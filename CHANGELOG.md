# Changelog

Denova 仅在此记录用户可感知的重大功能、重要不兼容或数据变更、安全更新，以及影响核心流程的关键修复。内部重构、测试调整、文案修改和细节级 UI 优化不再逐项列出，完整记录可查看 [Git history](https://github.com/alfredxw/denova/commits/master)。

Denova records only major user-visible features, important compatibility or data changes, security updates, and fixes affecting core workflows. Internal refactors, test changes, copy edits, and minor UI polish are omitted; see the [Git history](https://github.com/alfredxw/denova/commits/master) for full details.

`Unreleased` 以最近一个已发布版本（当前为 v0.4.2）为比较基线，只描述升级用户最终可感知的净变化；内部接口、实现重构和 v0.4.2 后从未发布的中间格式不计入。

`Unreleased` compares against the latest release (currently v0.4.2) and describes only the final user-visible delta. Internal APIs, implementation refactors, and intermediate formats never released after v0.4.2 are excluded.

格式参考 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)。

## [Unreleased]

## [v0.4.2] - 2026-09-06

### Brief / 简要说明

#### 中文

- 改善本地推理服务的内置工具调用兼容性，保留完整参数说明和执行校验。
- 记住手动选择的模型与思考强度，供后续同类对话和新故事使用。

#### English

- Improved built-in tool compatibility with local inference services while retaining full parameter guidance and execution validation.
- Remembered manually selected models and thinking levels for subsequent conversations of the same kind and new stories.

### Changed / 调整

- 手动调整模型或思考强度后，后续同类对话和新故事会沿用该选择，减少重复配置。
- Subsequent conversations of the same kind and new stories reuse manually selected models and thinking levels, reducing repeated setup.

### Fixed / 修复

- 简化内置工具的参数 Schema，改善本地推理服务的工具调用兼容性，同时保留参数校验和状态提交约束。
- Simplified built-in tool parameter schemas for local inference compatibility while preserving argument validation and state submission guarantees.

## [v0.4.1] - 2026-09-06

### Brief / 简要说明

#### 中文

- 修复工作台变更审阅页面崩溃、刷新后仍反复报错的问题。
- 明确 Agent 资料库写入规则：按人物、地点等独立实体或主题分条维护，更新时保留已有有效设定。

#### English

- Fixed workbench change-review crashes that could recur after refreshing.
- Clarified Agent lore-writing guidance to maintain separate entities or topics and preserve valid existing canon during updates.

### Fixed / 修复

- 修复正式构建中打开或恢复工作台变更审阅标签时出现 React 渲染异常、刷新后仍无法恢复的问题。
- Fixed a React rendering crash when opening or restoring workbench change-review tabs in production builds, including crashes that persisted after refreshing.

## [v0.4.0] - 2026-09-05

### Brief / 简要说明

#### 中文

v0.4.0 的核心是重建创作工作台与 Agent 执行底座：围绕 Project 统一创作资源，让持续对话、任务执行与恢复使用一致的会话机制。

- **统一 Project 工作台**：新增通用 Agent，书籍与本地目录都可作为项目；写作、游戏与共通工具采用并列导航，共享文件、资料库、终端和版本历史，支持多会话并行。
- **重建 Agent 运行与恢复**：统一保存对话、工具结果与任务状态，支持运行中追加指令、中断后继续及刷新或重启后恢复；新增自定义主 Agent、Goal 和脚本编排，统一管理 Skills、协作与权限。
- **重组游戏创作流程**：从组合预设改为故事自身配置，规划模板独立复用；单页完成开局，在故事控制台调整规划、事件与状态，并可从任意已保存回复创建分支。
- **统一模型接入**：语言与图像模型采用可复用的“连接 + 模型”配置，扩展 Responses、Anthropic、Gemini Image、Seedream 与 ComfyUI 等接入。
- **项目数据可整体迁移**：受管项目使用稳定身份和相对路径，退出应用后可将完整 `.denova` 目录跨 Windows、WSL、Linux 与 macOS 搬迁，保留会话、版本和附件。

**升级注意**：v0.3.3 数据按支持范围保留或先备份再迁移；旧全局 Automation 须在项目内重新创建，旧 Config Manager 历史不再展示，部分模型与上下文选项需重新设置。旧游戏预设中的自定义规划 Markdown 保留在备份中，不自动转换。

#### English

v0.4.0 rebuilds the creation workbench and Agent runtime around Projects, giving ongoing conversations, task execution, and recovery a consistent session model.

- **Unified Project workbench**: General Agents, Books, and local directories share project tools and parallel conversations; Writing, Game, and shared tools use peer navigation.
- **Rebuilt Agent runtime**: Conversations, tool results, and task state persist together, with follow-ups, resumable interruptions, restart recovery, custom main Agents, Goals, and scripting.
- **Restructured Game creation**: Stories own their settings; reusable Planning Templates, single-page setup, the Story Console, and branching from any saved reply reshape the workflow.
- **Unified model setup**: Reusable connections and model profiles expand language and image-provider support.
- **Portable project data**: Managed Projects retain history, versions, and attachments when moving the complete `.denova` directory across supported systems after shutdown.

**Upgrade note**: Supported v0.3.3 data is retained or migrated after backup. Recreate global Automations within Projects; old Config Manager history is no longer displayed, and some model/context options need reselection. Custom planning Markdown from old Game Presets remains in backups and is not converted automatically.

### Major changes / 重大变更

#### 中文

- **以 Project 统一创作工作台与资源。** 从写作、游戏模式切换改为并列一级导航；新增通用 Agent，书籍和任意本地目录可运行多个通用或写作会话。Files、资料库、阅读器、终端与版本历史在工作台内共享，配置对话与 Automation 也归入 Project 会话。双栏布局可持久保存，文件按类型使用文档、源码或图片视图，并共享自动保存、冲突保护与版本恢复。
- **以持久会话重建 Agent 执行。** 写作、通用对话、图像、自动化与游戏复用统一运行机制；每个逻辑会话以一份日志保存对话、工具结果、Goal、待办和上下文压缩等恢复状态，减少刷新、重连和重启后的丢失或重复。运行中可追加 Follow Up，中断后可继续；会话独立保存模型与权限。新增自定义主 Agent、Goal、JavaScript `script` 编排和 Project `AGENTS.md`，既有 Skills 与 SubAgent 协作纳入统一配置和执行机制，支持 Ask、Write、Full access 权限模式。主 Agent 默认自行处理任务，仅在用户或 Skill 明确要求时委派。Agent 配置本身也作为受管 Project 管理，可通过会话、Files 与版本历史维护。
- **将游戏规划与故事配置分开。** v0.3.3 由组合预设提供的叙事、事件、检定、状态与图像选择，现在直接保存在故事上；规划独立为五种内置方式及可自定义的规划模板。创建故事采用单页开局，故事控制台集中管理规划、事件与状态，并支持从任意已保存 AI 回复创建和管理分支。创作者可以分别调整故事的运行配置与后续剧情规划。
- **将模型服务连接与模型选项分开。** 同一连接可复用到多个模型，支持语言模型发现、批量添加和连接测试；协议覆盖 OpenAI Chat Completions、Responses、Anthropic Messages 及自定义兼容端点。图像接入扩展为 OpenAI Images、xAI/Grok、火山方舟 Seedream、Google Gemini Image 与 ComfyUI Workflow，多图任务保留部分成功结果。思考强度与单模型输出上限统一配置，改善长会话、工具调用和不同服务商之间的兼容性。
- **让项目数据脱离本机绝对路径。** 受管 Project 使用稳定 Project ID，数据目录内引用改为规范相对路径；退出 Denova 后，完整 `.denova` 目录可跨 Windows、WSL、Linux 与 macOS 移动或复制，会话、游戏、版本、附件、工具产物和自动化随项目保留。v0.3.3 数据按支持范围先备份再迁移；外部 Project 的内容不包含在数据目录内，不承诺随目录跨系统迁移。

#### English

- **One Project workbench for creation and resources.** Peer top-level navigation replaces Writing/Game mode switching. General Agents are added, and Books or arbitrary local directories can run multiple General or Writing conversations. Files, Lore, Reader, terminals, and version history share the workbench; configuration conversations and Automations also belong to Project sessions. Persistent split layouts and file-appropriate document, source, or image views share autosave, conflict protection, and version restoration.
- **An Agent runtime built around durable sessions.** Writing, General chat, Image, Automation, and Game share execution mechanisms. Each logical session keeps conversations, tool results, Goals, todos, and context-compaction state in one journal, reducing loss or duplication after refresh, reconnect, or restart. Runs accept follow-ups and support resumable interruptions; sessions retain their own model and permissions. Custom main Agents, Goals, JavaScript scripting, and Project instructions extend the existing Skills and SubAgent capabilities under shared configuration and execution, with Ask, Write, and Full access modes. Delegation remains opt-in through user or Skill instructions. Agent configuration is itself managed as a Project with conversations, Files, and version history.
- **Game planning separated from story settings.** Narrative, event, check, state, and image selections previously supplied by composition presets now belong directly to each story. Planning becomes independently reusable through five built-in approaches and custom Planning Templates. Single-page setup, a consolidated Story Console, and branching from any saved AI reply let creators adjust story settings and future plot plans separately.
- **Model connections separated from model options.** Multiple models reuse one connection, with language-model discovery, batch addition, and connection tests. Protocols include OpenAI Chat Completions, Responses, Anthropic Messages, and custom-compatible endpoints. Image support covers OpenAI Images, xAI/Grok, Volcengine Ark Seedream, Google Gemini Image, and ComfyUI Workflow, preserving partial success in multi-image jobs. Unified thinking settings and per-model output caps improve long-session, tool-call, and provider compatibility.
- **Project data independent of host paths.** Managed Projects use stable IDs and normalized relative references. After Denova exits, the complete `.denova` directory can move or be copied across Windows, WSL, Linux, and macOS with sessions, Game state, versions, attachments, tool artifacts, and Automations intact. Supported v0.3.3 data migrates after backup. External Project contents are outside the data directory and are not covered by this cross-system portability guarantee.

### Incompatible data changes / 用户数据不兼容变更

- v0.3.3 的游戏预设不再作为运行时组合配置。故事首次打开时会先备份原故事日志和引用的旧预设，再保留当时实际生效的叙事、事件、检定、状态、图像及规则展示配置，并切换到默认规划模板；旧预设中的自定义规划 Markdown 不自动转换，原内容保留在迁移备份中。
- v0.3.3 Game Presets no longer act as runtime composition settings. When a story is first opened, Denova backs up its journal and referenced legacy preset, preserves the effective narrative, event, check, state, image, and rule-visibility selections on the story, and selects the default Planning Template. Custom planning Markdown is not converted automatically and remains available in the migration backup.
- v0.3.3 的独立 Config Manager 会话、专属设置和用户级全局 Automation 任务文件会保留，但不再展示、触发或参与运行；后续配置对话使用对应 Project 的统一会话列表，需要继续使用的 Automation 须在对应 Project 下重新创建。
- Standalone v0.3.3 Config Manager sessions, dedicated settings, and user-level global Automation files are retained but no longer displayed, triggered, or used at runtime. Future configuration conversations use the Project's shared conversation list, and Automations that remain needed must be recreated in that Project.
- v0.3.3 的旧思考、输出上限、工具结果保留、低层 Cleanup 与 `labs.continual_learning` 设置不再生效；升级后需重新选择当前选项。有效用户级 Agent 设置会在备份后迁入 `.denova/agents` Profile，Project 覆盖继续保留。
- Legacy v0.3.3 thinking, output-limit, tool-result retention, low-level Cleanup, and `labs.continual_learning` settings no longer take effect and must be reselected where applicable. Active user-level Agent settings are backed up before migration into `.denova/agents` Profiles, while Project overrides remain intact.

### Major fixes / 重要修复

- 修复 Agent 流式输出和子 Agent 写作时界面明显卡顿的问题。
- Fixed severe UI stalls during Agent streaming and delegated writing.
- 修复子 Agent 成功写入文件后被错误标记为失败、导致主 Agent 重复写入的问题。
- Fixed delegated Agents reporting failure after successful file writes, which could cause the parent Agent to repeat the writes.

- Agent 首轮上下文现在直接包含当前可用的 Skill 目录，按名称加载只需一次工具调用；显式 `/<skill-name>` 仍会直接预载，减少等待和无效上下文消耗。
- The first Agent context now includes the available Skill catalog, so loading by name takes one tool call; explicit `/<skill-name>` invocations remain preloaded, reducing latency and unnecessary context usage.

- 修复首次切换 v0.3.3 书籍时工作区路径与 Project ID 短暂不同步而报错的问题；切换现在会一次发布完整 Project 身份，无需刷新恢复。
- Fixed first-switch errors for v0.3.3 Books caused by the workspace path and Project ID updating separately; switches now publish one complete Project identity without requiring a refresh.
- 修复数据目录绝对路径参与 Project 与会话身份，以及部分 v0.3.3 游戏故事含旧上下文事件的问题；搬迁或升级后不再出现历史消失、重复 Project 或故事无法打开，迁移会先备份原始数据再原子更新。
- Fixed absolute data-directory paths participating in Project and session identity, along with obsolete context events in some v0.3.3 Game stories. Relocation or upgrade no longer causes missing history, duplicate Projects, or stories that fail to open, and migrations back up source data before atomic updates.
- 修复跨轮、工具调用、刷新、重连、中断、继续或重启后推理上下文、消息、工具结果和已提交内容丢失或重复的问题；主动中断现在进入可恢复暂停，Token 校准不会再下调保守上下文估算。
- Fixed reasoning context, messages, tool results, or committed content being lost or duplicated across turns, tool calls, refresh, reconnect, interruption, continuation, or restart. User interruption now creates a resumable pause, and token calibration no longer lowers conservative context estimates.
- 修复写作与资料编辑中的自动保存、外部修改合并、评论消费和版本恢复问题，避免草稿被覆盖或已提交反馈重复出现。
- Fixed autosave, external-edit merging, comment consumption, and version restoration in Writing and Lore, preventing draft overwrites and submitted feedback from reappearing.
- 修复游戏检定重复应用故事修正、重生成重试丢失原回合、生成中调校丢弃正文，以及空回合崩溃、历史跳转和刷新后重复回放等问题；规则模板修正由后端解析，大成功与大失败仅由自然 20/1 触发。
- Fixed Game checks applying story tuning twice, regeneration retries losing their target, tuning during generation discarding prose, empty-turn crashes, incorrect history navigation, and settled content replaying after refresh. Rule-template modifiers are resolved by the backend, and critical success or failure requires a natural 20 or 1.
- 修复内置终端退出、重连、主题和 PTY 生命周期问题，交互式 CLI 退出后可可靠返回原工作目录的 Shell。
- Fixed embedded-terminal exit, reconnect, theme, and PTY lifecycle issues so interactive tools reliably return to the shell in the original working directory.
- 修复 DeepSeek、MiniMax、Claude 等模型及游戏回合输出上限过低，以及 DeepSeek 思考模式在工具结果投影或启动 SubAgent 后未回传 `reasoning_content` 而失败的问题。
- Fixed incorrectly low output limits for DeepSeek, MiniMax, Claude, and Game turns, plus DeepSeek thinking-mode failures when `reasoning_content` was not replayed after tool-result projection or SubAgent startup.
- 模型回复达到输出或上下文限制、被内容过滤或被服务商标记未完成时，会保留已生成内容、按真实原因提示并阻止执行可能残缺的工具参数；模型最大输出 Token 现在同时参与上下文预算，压缩摘要使用独立的有界输出。
- Model responses that reach output or context limits, are content-filtered, or are marked incomplete now retain generated content, report the actual reason, and block potentially partial tool arguments. Model output caps now also participate in context budgeting, while checkpoint summaries use their own bounded output.
- 应用内更新在 Release 缺少 `checksums.txt` 时会拒绝安装，不再跳过完整性校验。
- In-app updates now refuse releases without `checksums.txt` instead of skipping integrity verification.
- 升级 Go、`go-git` 与 `x/image` 安全基线，修复标准库、Git 路径/符号链接和 WebP 解码相关漏洞。
- Upgraded Go, `go-git`, and `x/image` security baselines to address standard-library, Git path/symlink, and WebP decoding vulnerabilities.

## [v0.3.3] - 2026-07-25

- 修复写作编辑器与对话框切换时输入法被重置的问题。
- Fixed IME resets when switching between the Writing editor and chat.
- 每个 Markdown 文件使用独立撤销历史，跨标签页撤销不再串改内容。
- Each Markdown file now has an isolated undo history, preventing cross-tab edits during undo.

## [v0.3.2] - 2026-07-23

- Agent 对话按 Run 聚合为可折叠的执行过程；工作台支持跨 Project、多会话真正并行运行。
- Agent conversations are grouped into collapsible run histories, while Workspace supports true concurrent conversations across Projects.
- Skills 新增分类与能力声明，工具参数支持安全规范化，网页搜索支持可替换 Provider 与抓取回退。
- Skills gained categories and capability declarations, tool arguments gained safe normalization, and web access gained replaceable search providers and fetch fallbacks.
- 编辑器自动保存和 Git 自动版本改为修改后延迟执行，并用 revision/CAS 与三方合并保护并发编辑。
- Editor autosave and Git versions now run after edit idle periods, with revision/CAS checks and three-way merging for concurrent changes.
- 游戏历史、模型上下文和 UI 展示拆分为独立投影，并强化最新回合编辑、重生成与恢复的一致性。
- Game history, model context, and UI rendering now use separate projections with safer latest-turn editing, regeneration, and recovery.

## [v0.3.1] - 2026-07-23

- Agent Trace 支持复制 Run ID 和导出完整 JSONL；前端独立启动可显式指定后端端口。
- Agent Trace can copy Run IDs and export complete JSONL traces; standalone frontend startup can target an explicit backend port.
- 修复端口冲突、Windows 设置自动保存和游戏正文候选重复等关键问题。
- Fixed important port-conflict, Windows settings-autosave, and duplicate Game narrative issues.

## [v0.3.0] - 2026-07-22

- 写作模式新增持久化 Change Review、正文评论、跨重启 Undo/Redo、正则替换和带备份的全局替换。
- Writing added durable Change Review, document comments, restart-safe undo/redo, regex replacement, and backed-up workspace-wide replacement.
- 游戏模式新增导演运行策略、故事级状态结构、Actor 归档/恢复、状态布局、回复修正和全屏导演台。
- Game added Director policies, story-scoped state schemas, Actor archive/restore, state layouts, response correction, and a full-screen Director Desk.
- 自动保存、三方合并、工作区变更账本、崩溃恢复与活动任务重连统一为更可靠的数据保护链路。
- Unified autosave, three-way merging, workspace change journals, crash recovery, and active-task reconnection into a safer data pipeline.
- 新增上下文检查、完整 Trace、更平滑的流式展示和 Unicode 规范化安全升级。
- Added context inspection, complete traces, smoother streaming, and a Unicode normalization security update.

## [v0.2.0] - 2026-07-15

- 写作工作台与 AI 互动叙事统一到长期创作架构，明确 Story Director、Actor State、事件、TRPG、资料与历史边界。
- Unified the Writing workbench and interactive narrative around clear Story Director, Actor State, event, TRPG, Lore, and history boundaries.
- Agent/Subagent 新增可配置 Skills、上下文压缩、工具结果策略、运行计划、自动化和本地 Trace。
- Agent and Subagent workflows gained configurable Skills, context compaction, tool-result policies, plans, automation, and local traces.
- 游戏当前状态改为摘要优先的自适应布局，并支持安全恢复无效的旧预设覆盖。
- Game Current State moved to an adaptive summary-first layout with safe recovery for invalid legacy preset overrides.
- 资料库新增按需加载、批量读取、revision 保护和 Tavern 角色卡/世界书导入。
- Lore gained on-demand loading, batch reads, revision protection, and Tavern character-card/world-book imports.

## [v0.1.18] - 2026-07-01

- 完成 Denova 品牌与分发命名切换，Release 包只提供 `denova` / `denova.exe`。
- Completed the Denova branding and distribution rename; release packages now use only `denova` / `denova.exe`.
- 新增新用户引导、消息中心、PWA/移动端主屏和前端内嵌自托管能力。
- Added onboarding, a message center, PWA/mobile home-screen support, and embedded-web self-hosting.
- 完善移动端写作与游戏的输入、弹窗、文件操作、故事记忆与分支导航。
- Expanded mobile Writing and Game input, dialogs, file actions, story memory, and branch navigation.
- 改进书籍封面、互动图像、Plan Mode 和资源保存冲突保护。
- Improved book covers, interactive images, Plan Mode, and resource-save conflict protection.

## [v0.1.17] - 2026-06-27

- 新增章节插图与互动图像生成工作流，并完善模型设置和故事开局交互。
- Added chapter-illustration and interactive-image workflows, with improved model settings and story-opening controls.

## [v0.1.16] - 2026-06-27

- 新增图像生成配置和默认模型别名支持。
- Added image-generation settings and default model aliases.

## [v0.1.15] - 2026-06-27

- 完善 Writing Agent 配置、运行控制和应用内更新体验。
- Improved Writing Agent configuration, run controls, and in-app updates.

## [v0.1.14] - 2026-06-26

- 新增写作 Skill 预设、配置工具和 Subagent 会话详情，并强化 Agent 启动与更新稳定性。
- Added Writing Skill presets, configuration tools, and Subagent session details, while improving Agent startup and updater reliability.

## [v0.1.13] - 2026-06-24

- 改进 Agent 上下文缓存、配置资源管理和移动端适配，并隔离配置管理会话。
- Improved Agent context caching, configuration resources, mobile layouts, and Config Manager session isolation.

## [v0.1.12] - 2026-06-20

- 重构互动故事记忆与状态 Schema，并补充上下文控制和失败重试。
- Reworked interactive-story memory and state schemas with stronger context controls and retry behavior.

## [v0.1.11] - 2026-06-18

- 新增故事开局、资料启用状态、Tavern 卡导入与记忆召回。
- Added story openings, Lore enablement, Tavern-card import, and memory recall.
- 新增自动化触发、消息收件箱、GitHub Release 更新器和深浅主题。
- Added automation triggers, an inbox, a GitHub Release updater, and light/dark themes.

## [v0.1.10] - 2026-06-12

- 改进工作区删除与恢复安全性。
- Improved workspace deletion and recovery safety.

## [v0.1.9] - 2026-06-12

- 修复空工作区启动、Skills 与互动叙事回归，完成核心架构稳定性清理。
- Fixed empty-workspace startup, Skills, and interactive-story regressions, with core architecture stabilization.

## [v0.1.8] - 2026-06-11

- 新增网页搜索和自定义 Skills。
- Added web search and custom Skills.

## [v0.1.7] - 2026-06-10

- 新增现有小说导入、自动化、工作区版本历史、中英文本地化和全局字号设置。
- Added existing-novel import, automation, workspace version history, Chinese/English localization, and global font sizing.

## [v0.1.6] - 2026-06-05

- 新增角色状态跟踪，增强资料管理，并将版本能力收敛为本地快照。
- Added character-state tracking, improved Lore management, and consolidated versioning around local snapshots.

## [v0.1.5] - 2026-06-02

- 资料库迁移为结构化数据并支持渐进加载与批量操作；资料、导演和写作工作区升级为完整 IDE 页面。
- Migrated Lore to structured storage with progressive loading and batch operations, and promoted Lore, Director, and Writing surfaces to full IDE pages.
- 互动故事新增回合版本切换、行动候选和按 Agent 配置的模型 Profile。
- Interactive stories gained turn versioning, action choices, and per-Agent model Profiles.

## [v0.1.4] - 2026-05-29

- 建立可玩的互动故事工作台，支持流式回合、状态、分支路线、行动候选和可中断生成。
- Introduced the playable interactive-story workbench with streaming turns, state, branch routes, action choices, and interruptible generation.
- 资料库升级为结构化 Lore Item，并新增资料 Agent、导演配置和 Tavern v2 角色卡导入。
- Upgraded Lore to structured items and added a Lore Agent, Director configuration, and Tavern v2 character-card import.
- 写作工作台新增作品统计，并统一写作与游戏的紧凑导航、字体和布局设置。
- Added manuscript statistics and unified compact navigation, typography, and layout controls across Writing and Game.

## [v0.1.3] - 2026-05-24

- 工作区新增多 Tab、分层设置、无工作区引导和 `brainstorm.md` 创作流程。
- Added multi-tab workspaces, layered settings, empty-workspace onboarding, and the `brainstorm.md` creation flow.
- Agent 新增异常中断恢复、上下文边界和场景化风格规则。
- Added Agent interruption recovery, context boundaries, and scene-specific style rules.

## [v0.1.2] - 2026-05-18

- 新增多会话管理、`/clear` 上下文分界、可调工作区布局和章节版本 Diff/回滚界面。
- Added multi-session management, `/clear` context boundaries, resizable workspace layouts, and chapter version diff/rollback views.

## [v0.1.1] - 2026-05-17

- 发布基于 React、TipTap 与 Go/Hertz 的首个 Web 小说 IDE，包含文件树、Markdown 编辑、自动保存和三栏工作区。
- Released the first web novel IDE based on React, TipTap, and Go/Hertz, with a file tree, Markdown editing, autosave, and a three-pane workspace.
- 创作 Agent 支持 SSE 流式输出、思考与工具时间线、中断、风格引用和 `CREATOR.md` 指令。
- The Writing Agent supports SSE streaming, reasoning/tool timelines, interruption, style references, and `CREATOR.md` instructions.
- 新增本地版本管理、书籍切换和基础会话持久化，并移除旧 TUI。
- Added local version management, book switching, and basic session persistence, and removed the former TUI.
