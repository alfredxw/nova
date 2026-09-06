package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"denova/config"
	"denova/internal/agents/canonicalstore"
	agentexecution "denova/internal/agents/execution"
	"denova/internal/agents/session"
	"denova/internal/agents/trajectory"
	activityapp "denova/internal/app/activity"
	agentchatapp "denova/internal/app/agentchat"
	appagentruntime "denova/internal/app/agentruntime"
	automationapp "denova/internal/app/automation"
	bookapp "denova/internal/app/book"
	imageapp "denova/internal/app/image"
	loreapp "denova/internal/app/lore"
	modelsapp "denova/internal/app/models"
	projectbookapp "denova/internal/app/projectbook"
	projectfilesapp "denova/internal/app/projectfiles"
	resourcecatalogapp "denova/internal/app/resourcecatalog"
	settingsapp "denova/internal/app/settings"
	apptask "denova/internal/app/task"
	"denova/internal/book"
	"denova/internal/concurrency"
	"denova/internal/interactive"
	"denova/internal/localfs"
	"denova/internal/portablepath"
	projectdomain "denova/internal/project"
	"denova/internal/terminal"
	"denova/internal/workspace/filewatch"
)

// App 是 API 层使用的应用门面；具体业务由领域应用服务承接。
type App struct {
	cfg *config.Config

	workspace                       string
	bookState                       *book.State
	bookService                     *book.Service
	interactive                     *interactive.Store
	sessionStore                    *session.Store
	session                         *session.Session
	executionRuntime                *agentexecution.Runtime
	projectRegistry                 *projectdomain.Registry
	bookMetaStore                   *book.MetaStore
	versionService                  *book.VersionService
	activeTask                      *apptask.Task
	activeWritingRun                *writingTaskRun
	activeInteractiveRun            *interactiveTaskRun
	workspaceTasks                  map[*apptask.Task]string
	workspaceTaskLeases             map[*apptask.Task]*concurrency.Lease
	workspaceTaskStops              map[*apptask.Task]func() bool
	workspaceTaskReplayReservations map[*apptask.Task]*apptask.ReplayReservation
	workspaceTransition             bool
	workspaceTransitionTargets      map[string]struct{}
	versionSummaryGenerator         versionSummaryGeneratorFunc
	workspaceFiles                  *filewatch.Service
	rootScope                       *concurrency.Scope
	workspaceScopes                 map[string]*concurrency.Scope
	workspaceScopeSequence          uint64
	projectScopes                   map[string]*concurrency.Scope
	projectScopeSequence            uint64
	projectTransitions              map[string]struct{}
	projectTasks                    map[*apptask.Task]*projectTaskRegistration
	workspaceGeneration             uint64
	closed                          bool
	closeOnce                       sync.Once
	activeTaskReplay                apptask.ReplayAdmission

	// terminals owns the pty sessions behind the AgentChat terminal tabs. They are decoupled from
	// the workspace: each session keeps its own cwd, so switching books never kills a running command.
	terminals *terminal.Manager

	workspaceApp       *workspaceService
	chatApp            *ChatAppService
	agentChatApp       *agentchatapp.Service
	interactiveApp     *InteractiveAppService
	loreApp            *loreapp.Service
	trajectoryOutcomes *trajectory.OutcomeStore
	automationApp      *automationapp.Service
	activityApp        *activityapp.Service
	bookApp            *bookapp.Service
	resourceCatalog    *resourcecatalogapp.Service
	settingsApp        *settingsapp.Service
	modelsApp          *modelsapp.Service
	imageApp           *imageapp.Service
	projectBook        *projectbookapp.Service
	projectFiles       *projectfilesapp.Service
	servicesOnce       sync.Once

	mu sync.RWMutex
	// Keep concurrent manual selections ordered across their conversation and
	// user-default writes, including selections made in different Projects.
	modelSelectionMu sync.Mutex
}

// New creates the application runtime. When neither an explicit nor resumable
// workspace exists, App stays unbound until the user selects or creates a Book.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	dataDir := ""
	if cfg != nil {
		dataDir = strings.TrimSpace(cfg.DataDir())
	}
	if dataDir == "" {
		return nil, ErrAgentDataDirRequired
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("initialize Denova data directory: %w", err)
	}
	releaseStartupLease, err := localfs.AcquireLease(ctx, filepath.Join(dataDir, "runtime", "portable-startup.lock"))
	if err != nil {
		return nil, fmt.Errorf("acquire Denova data migration lock: %w", err)
	}
	defer func() {
		if releaseErr := releaseStartupLease(); releaseErr != nil {
			slog.ErrorContext(context.Background(), "[internal/app/app.go] release portable startup lock failed", "error", releaseErr)
		}
	}()
	if err := portablepath.PreflightTree(dataDir); err != nil {
		return nil, fmt.Errorf("Denova data directory is not portable across Windows, WSL, Linux, and macOS: %w", err)
	}
	if _, err := backupUnreleasedAgentTranscripts(dataDir); err != nil {
		return nil, err
	}
	migrationBackups, err := preparePortableDataMigration(dataDir)
	if err != nil {
		return nil, fmt.Errorf("prepare portable Denova data migration: %w", err)
	}
	if err := migratePresetLayout(dataDir); err != nil {
		return nil, fmt.Errorf("migrate Denova preset layout: %w", err)
	}
	if err := config.EnsureAgentProfiles(dataDir); err != nil {
		return nil, fmt.Errorf("initialize Agents Project profiles: %w", err)
	}
	registry := projectdomain.NewRegistry(dataDir)
	agentsRecord, err := registry.EnsureAgents(config.AgentProfilesRoot(dataDir))
	if err != nil {
		return nil, fmt.Errorf("register Agents Project: %w", err)
	}
	agentsLayout, err := registry.EnsureStore(agentsRecord)
	if err != nil {
		return nil, fmt.Errorf("initialize Agents Project Store: %w", err)
	}
	trajectoryOutcomes, err := trajectory.NewOutcomeStore(agentsLayout.StoreRoot)
	if err != nil {
		return nil, fmt.Errorf("initialize trajectory outcomes: %w", err)
	}
	bookMetaStore := book.NewMetaStore(dataDir)
	registeredProjects, err := registry.List(true)
	if err != nil {
		return nil, fmt.Errorf("load registered Projects for metadata migration: %w", err)
	}
	for _, record := range registeredProjects {
		if record.Type != projectdomain.TypeBook || record.Status != projectdomain.StatusAvailable {
			continue
		}
		layout, layoutErr := registry.EnsureStore(record)
		if layoutErr != nil {
			return nil, fmt.Errorf("prepare Book Project Store for metadata migration: %w", layoutErr)
		}
		if migrationErr := bookMetaStore.MigrateLegacy(layout.ContentRoot, layout.StoreRoot); migrationErr != nil {
			return nil, fmt.Errorf("migrate Book metadata: %w", migrationErr)
		}
	}
	if err := migratePortableApprovalSettings(dataDir, registry); err != nil {
		return nil, fmt.Errorf("migrate Agent approval rules: %w", err)
	}
	if rules, changed := portableApprovalRules(registeredProjects, cfg.AgentApprovalRules); changed {
		cfg.AgentApprovalRules = rules
	}
	if err := completePortableDataMigration(dataDir, migrationBackups, len(registeredProjects)); err != nil {
		return nil, fmt.Errorf("complete portable Denova data migration: %w", err)
	}
	app := &App{
		cfg:                cfg,
		projectRegistry:    registry,
		bookMetaStore:      bookMetaStore,
		trajectoryOutcomes: trajectoryOutcomes,
		workspaceFiles:     filewatch.NewService(),
		terminals:          terminal.NewManager(terminalConfigFromAppConfig(cfg)),
	}
	app.automationApp = automationapp.NewService(automationHost{app: app})
	canonicalSessions, err := canonicalstore.New(dataDir, registry)
	if err != nil {
		return nil, fmt.Errorf("initialize canonical Agent Session Store: %w", err)
	}
	executionRuntime, err := agentexecution.NewAgentRuntime(
		ctx,
		dataDir,
		agentexecution.WithProfiles(app.executionProfiles()...),
		agentexecution.WithSessionStore(canonicalSessions),
		agentexecution.WithChildDefinitionResolver(agentexecution.ChildDefinitionResolverFunc(app.prepareChildDefinition)),
		agentexecution.WithToolMutationApplier(app.automationApp.ApplyToolMutation),
		agentexecution.WithPermissionRuleStore(agentexecution.PermissionRuleStore{
			Load: func(context.Context) ([]config.AgentApprovalRule, error) {
				layered, err := app.SettingsService().Snapshot(settingsapp.Global())
				if err != nil {
					return nil, err
				}
				return config.NormalizeAgentApprovalRules(layered.Effective.AgentApprovalRules), nil
			},
			Persist: func(ctx context.Context, rule config.AgentApprovalRule) error {
				rule = canonicalApprovalRuleForPersistence(app.projectRegistry, rule)
				_, err := app.SettingsService().EnsureAgentApprovalRule(rule)
				return err
			},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize Agent runtime: %w", err)
	}
	app.executionRuntime = executionRuntime
	workspace := cfg.Workspace
	if workspace == "" && cfg.ResumeLastWorkspace {
		if lastWorkspace := registry.CurrentBookPath(); lastWorkspace != "" {
			workspace = lastWorkspace
		}
	}

	app.mu.Lock()
	if err := app.initializeLifecycleLocked(); err != nil {
		app.mu.Unlock()
		_ = executionRuntime.Close(context.Background())
		return nil, fmt.Errorf("initialize app lifecycle: %w", err)
	}
	app.mu.Unlock()
	app.ensureServices()

	if workspace == "" {
		slog.InfoContext(ctx, "[app] No workspace or previously opened book at startup; waiting for frontend selection")
		cfg.Workspace = ""
		app.Automation().StartScheduler(ctx)
		return app, nil
	}

	projectRecord, err := app.projectRegistry.EnsureBook(workspace)
	if err != nil {
		app.Close()
		return nil, err
	}
	layout, err := app.projectRegistry.EnsureStore(projectRecord)
	if err != nil {
		app.Close()
		return nil, err
	}
	runtime, err := buildRuntimeExclusively(ctx, cfg, layout)
	if err != nil {
		app.Close()
		return nil, err
	}
	cfg.Workspace = runtime.workspace
	_, _ = registry.TouchBook(runtime.workspace)

	app.mu.Lock()
	if err := app.replaceWorkspaceScopeLocked(runtime.workspace); err != nil {
		app.mu.Unlock()
		app.Close()
		return nil, err
	}
	app.applyRuntime(runtime)
	app.mu.Unlock()
	app.Automation().StartScheduler(ctx)
	return app, nil
}

// ErrNoWorkspace 表示当前 App 尚未绑定任何书籍 workspace。
var ErrNoWorkspace = appagentruntime.ErrNoWorkspace

// ErrAgentDataDirRequired prevents production App instances from silently
// falling back to a process-local journal that cannot survive restart.
var ErrAgentDataDirRequired = errors.New("agent runtime data directory is required")

// ErrNoWorkspaceOpen means a request requires an open workspace.
var ErrNoWorkspaceOpen = settingsapp.ErrProjectRequired

// ErrAgentOperationActive rejects implicit replacement. Callers must target
// the running operation with Follow Up, Steer, or Abort before starting a new
// root operation.
var ErrAgentOperationActive = appagentruntime.ErrOperationActive

// ErrWorkspaceTransition prevents a task from binding half to an old
// workspace and half to a newly constructed runtime.
var ErrWorkspaceTransition = appagentruntime.ErrWorkspaceTransition

// ErrAgentContextChanged means preparation completed against a workspace,
// session, story, or branch that is no longer current at atomic registration.
var ErrAgentContextChanged = appagentruntime.ErrContextChanged

func (a *App) ensureServices() {
	a.servicesOnce.Do(func() {
		a.workspaceApp = &workspaceService{app: a}
		a.chatApp = &ChatAppService{
			app: a, starts: apptask.NewStartRegistry(apptask.StartRegistryOptions{Label: "Writing"}),
		}
		a.agentChatApp = agentchatapp.NewService(agentChatHost{app: a}, a.projectRegistry)
		a.interactiveApp = &InteractiveAppService{app: a}
		if a.automationApp == nil {
			a.automationApp = automationapp.NewService(automationHost{app: a})
		}
		dataDir := ""
		if a.cfg != nil {
			dataDir = a.cfg.DataDir()
		}
		a.activityApp = activityapp.NewService(dataDir, a.automationApp)
		a.bookApp = bookapp.NewService(dataDir, a.projectRegistry, a.bookMetaStore)
		a.resourceCatalog = resourcecatalogapp.NewService(dataDir, resourceCatalogHost{app: a})
		a.settingsApp = settingsapp.NewService(settingsHost{app: a})
		a.modelsApp = modelsapp.NewService(modelHost{app: a})
		a.imageApp = imageapp.NewService(imageHost{app: a})
		a.loreApp = loreapp.NewService(loreHost{app: a}, a.imageApp)
		a.projectBook = projectbookapp.NewService(a.projectRegistry)
		if a.workspaceFiles != nil {
			a.workspaceFiles.SetObserver(a.observeProjectFileChange)
		}
		projectFileOptions := []projectfilesapp.ServiceOption(nil)
		if a.cfg != nil {
			projectFileOptions = append(projectFileOptions, projectfilesapp.WithTreeEntryLimit(a.cfg.ProjectFileTreeEntryLimit))
		}
		a.projectFiles = projectfilesapp.NewServiceWithVersioning(a.projectRegistry, a, projectFileOptions...)
	})
}

func (a *App) workspaceService() *workspaceService {
	a.ensureServices()
	return a.workspaceApp
}

func (a *App) chat() *ChatAppService {
	a.ensureServices()
	return a.chatApp
}

// AgentChat exposes the cohesive project-scoped conversation service.
func (a *App) AgentChat() *agentchatapp.Service {
	a.ensureServices()
	return a.agentChatApp
}

// ProjectBook exposes Book resources through stable Project identity without
// changing the foreground Writing workspace.
func (a *App) ProjectBook() *projectbookapp.Service {
	a.ensureServices()
	return a.projectBook
}

// ProjectFiles exposes Project-scoped file browsing and editing without
// changing the foreground Writing workspace.
func (a *App) ProjectFiles() *projectfilesapp.Service {
	a.ensureServices()
	return a.projectFiles
}

func (a *App) interactiveService() *InteractiveAppService {
	a.ensureServices()
	return a.interactiveApp
}

// Lore exposes the cohesive lore application service.
func (a *App) Lore() *loreapp.Service {
	a.ensureServices()
	return a.loreApp
}

// Images exposes shared image generation for writing and game modes.
func (a *App) Images() *imageapp.Service {
	a.ensureServices()
	return a.imageApp
}

// TrajectoryOutcomes returns the read-only evidence feedback store owned by
// the Agents Project Store. Profile content and feedback never share a directory.
func (a *App) TrajectoryOutcomes() *trajectory.OutcomeStore { return a.trajectoryOutcomes }

// Automation exposes the automation domain service without duplicating its API
// on the root composition type.
func (a *App) Automation() *automationapp.Service {
	a.ensureServices()
	return a.automationApp
}

// Activity exposes the unified notification and badge projection.
func (a *App) Activity() *activityapp.Service {
	a.ensureServices()
	return a.activityApp
}

// BookAssets exposes book cover and export operations.
func (a *App) BookAssets() *bookapp.Service {
	a.ensureServices()
	return a.bookApp
}

// ResourceCatalog exposes reusable creator resources shared by writing and
// game modes without duplicating their API on the root composition type.
func (a *App) ResourceCatalog() *resourcecatalogapp.Service {
	a.ensureServices()
	return a.resourceCatalog
}

// SettingsService exposes layered settings persistence while App retains only
// the process-local refresh effects.
func (a *App) SettingsService() *settingsapp.Service {
	a.ensureServices()
	return a.settingsApp
}

// Models exposes provider discovery and connection validation shared by all
// writing and game model configuration surfaces.
func (a *App) Models() *modelsapp.Service {
	a.ensureServices()
	return a.modelsApp
}

func (a *App) applyRuntime(runtime *runtimeState) {
	a.workspace = runtime.workspace
	a.bookState = runtime.bookState
	a.bookService = runtime.bookService
	a.interactive = runtime.interactive
	a.sessionStore = runtime.sessionStore
	a.session = runtime.session
	a.versionService = runtime.versionService
	if a.cfg != nil {
		a.cfg.ProjectID = runtime.projectID
		a.cfg.ProjectStoreDir = runtime.projectStoreRoot
	}
	a.activeTask = nil
	a.activeWritingRun = nil
	a.activeInteractiveRun = nil
}

func (a *App) clearRuntime() {
	a.workspace = ""
	a.cfg.Workspace = ""
	a.cfg.ProjectID = ""
	a.cfg.ProjectStoreDir = ""
	a.bookState = nil
	a.bookService = nil
	a.interactive = nil
	a.sessionStore = nil
	a.session = nil
	a.versionService = nil
	a.activeTask = nil
	a.activeWritingRun = nil
	a.activeInteractiveRun = nil
}

// Close stops background work owned by the current workspace runtime.
func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		a.ensureServices()
		a.mu.Lock()
		a.closed = true
		rootScope := a.rootScope
		versionService := a.versionService
		workspaceFiles := a.workspaceFiles
		interactiveStore := a.interactive
		sessionStore := a.sessionStore
		a.mu.Unlock()

		if workspaceFiles != nil {
			workspaceFiles.Close()
		}
		if a.terminals != nil {
			a.terminals.CloseAll()
		}
		// Admission closes before cancellation so no task can slip between the
		// final registry snapshot and the resource barrier.
		if rootScope != nil {
			rootScope.BeginClose()
		}
		if a.automationApp != nil {
			if err := a.automationApp.Close(context.Background()); err != nil {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[app] close automation service failed: %v", err))
			}
		}
		if a.agentChatApp != nil {
			a.agentChatApp.Close(context.Background())
		}
		if a.projectFiles != nil {
			a.projectFiles.Close()
		}
		a.abortOwnedAgentTasks(context.Background())
		if rootScope != nil {
			if err := rootScope.Wait(context.Background()); err != nil {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[app] wait lifecycle scope failed: %v", err))
			}
		}
		if interactiveStore != nil {
			if err := interactiveStore.Close(); err != nil {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[app] flush interactive conversation indexes failed: %v", err))
			}
		}
		if sessionStore != nil {
			if err := sessionStore.Close(); err != nil {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[app] flush Agent conversation indexes failed: %v", err))
			}
		}
		if versionService != nil {
			versionService.Close()
		}
		if a.executionRuntime != nil {
			if err := a.executionRuntime.Close(context.Background()); err != nil {
				slog.ErrorContext(context.Background(), fmt.Sprintf("[app] close durable agent runtime failed: %v", err))
			}
		}
	})
}

func (a *App) abortOwnedAgentTasks(ctx context.Context) {
	if a == nil {
		return
	}
	a.mu.RLock()
	unique := make(map[*apptask.Task]struct{}, 3+len(a.workspaceTasks)+len(a.projectTasks))
	add := func(task *apptask.Task) {
		if task != nil {
			unique[task] = struct{}{}
		}
	}
	if a.activeTask != nil {
		add(a.activeTask)
	}
	if a.activeInteractiveRun != nil && a.activeInteractiveRun.task != nil {
		add(a.activeInteractiveRun.task)
	}
	for task := range a.workspaceTasks {
		add(task)
	}
	for task := range a.projectTasks {
		add(task)
	}
	a.mu.RUnlock()
	tasks := make([]*apptask.Task, 0, len(unique))
	for task := range unique {
		tasks = append(tasks, task)
	}
	if err := abortAndWaitTasks(ctx, tasks, "app_close"); err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("[app] wait for owned agent tasks failed: %v", err))
	}
}

// RemoteAccessConfig returns the current process-level access policy used by
// the HTTP gateway. Settings updates may change this before a full restart.
func (a *App) RemoteAccessConfig() config.RemoteAccessConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cfg == nil {
		return config.RemoteAccessConfig{}
	}
	return a.cfg.RemoteAccessConfig()
}
