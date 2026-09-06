package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/alfredxw/denova/agent"
)

func TestMain(m *testing.M) {
	if os.Getenv("DENOVA_TEST_RIPGREP_HELPER") == "1" {
		for _, arg := range os.Args[1:] {
			if arg == "--no-config" {
				fmt.Fprint(os.Stdout, os.Getenv("DENOVA_TEST_RIPGREP_OUTPUT"))
				os.Exit(0)
			}
		}
		fmt.Fprintln(os.Stderr, "missing --no-config")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestLineNumbersUsesCompactPrefixAndPreservesIndentation(t *testing.T) {
	got := lineNumbers("  first\n\tsecond\n", 9)
	if want := "9\t  first\n10\t\tsecond\n"; got != want {
		t.Fatalf("lineNumbers() = %q, want %q", got, want)
	}
}

func TestReadRoutesLocalTextAndDirectoryWithAdapterSpecificArguments(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, root, "chapters/one.md", "first\nsecond\nthird\n")
	mustWriteTestFile(t, root, "chapters/.hidden", "secret")
	mustWriteTestFile(t, root, "chapters/nested/two.md", "two")
	workspace := mustOpenTestWorkspace(t, root)
	textAdapter, err := LocalTextAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	directoryAdapter, err := DirectoryAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{textAdapter, directoryAdapter})
	if err != nil {
		t.Fatal(err)
	}
	if definition.Descriptor.ResultRecoveryKind != agent.ToolResultRecoveryRead {
		t.Fatalf("read result recovery = %q", definition.Descriptor.ResultRecoveryKind)
	}
	info, err := definition.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "read" || len(parameters.AnyOf) != 0 || parameters.Properties.Len() != 6 {
		t.Fatalf("read schema = %#v", info)
	}

	fileResult, err := definition.Tool.Run(context.Background(), `{"path":"chapters/one.md","offset":2,"limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fileResult.ModelContent, `"kind":"local_text"`) ||
		!strings.Contains(fileResult.ModelContent, "2\tsecond") ||
		strings.Contains(fileResult.ModelContent, "first") {
		t.Fatalf("file result = %q", fileResult.ModelContent)
	}

	directoryResult, err := definition.Tool.Run(context.Background(), `{"path":"chapters","depth":2,"limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(directoryResult.ModelContent, `"kind":"directory"`) ||
		!strings.Contains(directoryResult.ModelContent, "chapters/nested/two.md") ||
		strings.Contains(directoryResult.ModelContent, ".hidden") {
		t.Fatalf("directory result = %q", directoryResult.ModelContent)
	}

	if _, err := definition.Tool.Run(context.Background(), `{"path":"chapters","offset":1}`); err != nil {
		t.Fatalf("directory rejected harmless local_text-only parameter: %v", err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"chapters/one.md","depth":2}`); err != nil {
		t.Fatalf("local text rejected harmless directory-only parameter: %v", err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"https://example.com"}`); err == nil || !strings.Contains(err.Error(), "web_fetch") {
		t.Fatalf("read accepted HTTP resource: %v", err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"chapters/one.md","mystery":true}`); err != nil {
		t.Fatalf("read rejected harmless unknown parameter: %v", err)
	}
}

func TestReadReportsMissingResourcesAsSuccessfulObservations(t *testing.T) {
	root := t.TempDir()
	workspace := mustOpenTestWorkspace(t, root)
	textAdapter, err := LocalTextAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	directoryAdapter, err := DirectoryAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{textAdapter, directoryAdapter})
	if err != nil {
		t.Fatal(err)
	}

	_, expectedErr := workspace.resolveReadPath("missing.md", false)
	if expectedErr == nil {
		t.Fatal("missing workspace path unexpectedly exists")
	}
	result, err := definition.Tool.Run(context.Background(), `{"path":"missing.md"}`)
	if err != nil {
		t.Fatalf("missing read returned a tool error: %v", err)
	}
	if result.Status != agent.ToolResultSuccess || result.IsError() {
		t.Fatalf("missing read status = %q", result.Status)
	}
	var envelope readEnvelope
	if err := json.Unmarshal(result.Details, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "not_found" || envelope.Source.Path != "missing.md" || envelope.Source.Kind != "unresolved" {
		t.Fatalf("missing read envelope = %#v", envelope)
	}
	if !strings.Contains(result.ModelContent, expectedErr.Error()) {
		t.Fatalf("missing read did not preserve the filesystem diagnostic: %q", result.ModelContent)
	}
}

func TestReadReportsAdapterNotFoundWithoutMaskingOtherFailures(t *testing.T) {
	type referenceInput struct {
		Path string `json:"path"`
	}
	missingErr := fmt.Errorf("read missing reference: %w", os.ErrNotExist)
	missingAdapter, err := NewReadAdapter(
		agent.CapabilityIdentity{Kind: "test.read.reference", Version: 1},
		"reference",
		func(context.Context, string) (bool, error) { return true, nil },
		func(context.Context, referenceInput) (ReadResult, error) { return ReadResult{}, missingErr },
	)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{missingAdapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := definition.Tool.Run(context.Background(), `{"path":"reference://missing"}`)
	if err != nil {
		t.Fatalf("adapter absence returned a tool error: %v", err)
	}
	if result.Status != agent.ToolResultSuccess || !strings.Contains(result.ModelContent, missingErr.Error()) ||
		!strings.Contains(result.ModelContent, `"status":"not_found"`) {
		t.Fatalf("adapter absence result = %#v", result)
	}
	boundedDefinition, err := Read([]ReadAdapter{missingAdapter}, WithMaxResultBytes(16))
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := boundedDefinition.Tool.Run(context.Background(), `{"path":"reference://missing"}`)
	if err != nil || bounded.Status != agent.ToolResultSuccess || len(bounded.ModelContent) > 16 {
		t.Fatalf("bounded adapter absence result = %#v, err=%v", bounded, err)
	}

	permissionErr := errors.New("permission denied")
	brokenAdapter, err := NewReadAdapter(
		agent.CapabilityIdentity{Kind: "test.read.broken", Version: 1},
		"broken",
		func(context.Context, string) (bool, error) { return false, permissionErr },
		func(context.Context, referenceInput) (ReadResult, error) { return ReadResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	missingMatcher, err := NewReadAdapter(
		agent.CapabilityIdentity{Kind: "test.read.missing", Version: 1},
		"missing",
		func(context.Context, string) (bool, error) { return false, os.ErrNotExist },
		func(context.Context, referenceInput) (ReadResult, error) { return ReadResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	definition, err = Read([]ReadAdapter{missingMatcher, brokenAdapter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"reference://blocked"}`); !errors.Is(err, permissionErr) {
		t.Fatalf("missing matcher masked inspection failure: %v", err)
	}
}

func TestReadRejectsAmbiguousAdapters(t *testing.T) {
	type input struct {
		Path string `json:"path"`
	}
	makeAdapter := func(name string) ReadAdapter {
		adapter, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read." + name, Version: 1}, name, func(context.Context, string) (bool, error) { return true, nil }, func(_ context.Context, in input) (ReadResult, error) {
			return ReadResult{Path: in.Path, Kind: name, Content: name}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return adapter
	}
	definition, err := Read([]ReadAdapter{makeAdapter("one"), makeAdapter("two")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"resource://one"}`); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous adapters error = %v", err)
	}
}

func TestReadRejectsConflictingSharedAdapterParameters(t *testing.T) {
	type firstInput struct {
		Path  string `json:"path"`
		Limit int    `json:"limit,omitempty" jsonschema:"minimum=1"`
	}
	type secondInput struct {
		Path  string `json:"path"`
		Limit string `json:"limit,omitempty"`
	}
	first, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read.first", Version: 1}, "first", func(context.Context, string) (bool, error) { return true, nil }, func(context.Context, firstInput) (ReadResult, error) {
		return ReadResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read.second", Version: 1}, "second", func(context.Context, string) (bool, error) { return false, nil }, func(context.Context, secondInput) (ReadResult, error) {
		return ReadResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Read([]ReadAdapter{first, second}); err == nil || !strings.Contains(err.Error(), "conflicting contracts") {
		t.Fatalf("conflicting adapter parameters were accepted: %v", err)
	}
}

func TestReadUsesWorkspacePolicyAndReturnsContinuation(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, root, "chapter.md", "one\nfive\nnine\n")
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
		Root: root, Limits: WorkspaceLimits{MaxResultBytes: 9, DefaultReadLines: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := LocalTextAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	result, err := definition.Tool.Run(context.Background(), `{"path":"chapter.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ModelContent, `"status":"partial"`) ||
		!strings.Contains(result.ModelContent, `"next_offset":3`) ||
		!strings.Contains(result.ModelContent, "2\tfive") ||
		strings.Contains(result.ModelContent, "nine") {
		t.Fatalf("bounded read result = %q", result.ModelContent)
	}
}

func TestReadProjectionBudgetsEnvelopeAndAdvancesOnlyVisibleLines(t *testing.T) {
	result, err := projectReadResult(ReadResult{
		Path: "chapter.md", Kind: "local_text", Offset: 10, Limit: 3,
		Content: strings.Repeat("a", 180) + "\n" + strings.Repeat("b", 180) + "\n" + strings.Repeat("c", 180) + "\n",
	}, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ModelContent) > 512 {
		t.Fatalf("projected read has %d bytes", len(result.ModelContent))
	}
	parts := strings.Split(strings.TrimSuffix(result.ModelContent, "\n"), "\n")
	visible := len(parts) - 1
	if visible <= 0 || visible >= 3 {
		t.Fatalf("visible lines = %d, result=%q", visible, result.ModelContent)
	}
	var envelope readEnvelope
	if err := json.Unmarshal([]byte(parts[0]), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Limits.Returned != visible || envelope.Limits.NextOffset != 10+visible || !envelope.Limits.Truncated {
		t.Fatalf("read envelope = %#v, visible=%d", envelope, visible)
	}
}

func TestReadSupportsExternalPathsAndRejectsBinaryContent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteTestFile(t, outside, "escape.txt", "external through symlink\n")
	siblingPath := filepath.Join(filepath.Dir(root), "external.txt")
	if err := os.WriteFile(siblingPath, []byte("external through parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustWriteTestFile(t, root, "long.txt", strings.Repeat("x", defaultResultBytes+1))
	if err := os.WriteFile(filepath.Join(root, "binary.txt"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
			t.Fatal(err)
		}
	}
	workspace := mustOpenTestWorkspace(t, root)
	adapter, err := LocalTextAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Read([]ReadAdapter{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definition.Tool.Run(context.Background(), `{"path":"binary.txt"}`); err == nil {
		t.Fatal("binary read succeeded")
	}
	parentRead, err := definition.Tool.Run(context.Background(), `{"path":"../external.txt"}`)
	if err != nil || !strings.Contains(parentRead.ModelContent, "external through parent") {
		t.Fatalf("parent external read = %#v, %v", parentRead, err)
	}
	var parentEnvelope readEnvelope
	if err := json.Unmarshal(parentRead.Details, &parentEnvelope); err != nil {
		t.Fatalf("decode parent external read envelope: %v", err)
	}
	reportedInfo, err := os.Stat(filepath.FromSlash(parentEnvelope.Source.Path))
	if err != nil {
		t.Fatalf("stat reported parent external path %q: %v", parentEnvelope.Source.Path, err)
	}
	expectedInfo, err := os.Stat(siblingPath)
	if err != nil {
		t.Fatalf("stat expected parent external path %q: %v", siblingPath, err)
	}
	if !os.SameFile(reportedInfo, expectedInfo) {
		t.Fatalf("parent external read source = %q, want file %q", parentEnvelope.Source.Path, siblingPath)
	}
	first, err := definition.Tool.Run(context.Background(), `{"path":"long.txt"}`)
	if err != nil {
		t.Fatalf("long-line read should return a byte continuation: %v", err)
	}
	var firstEnvelope readEnvelope
	if err := json.Unmarshal(first.Details, &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if firstEnvelope.Limits.NextOffset != 1 || firstEnvelope.Limits.NextByteOffset <= 0 || !firstEnvelope.Limits.Truncated {
		t.Fatalf("long-line continuation = %#v", firstEnvelope.Limits)
	}
	continued, err := definition.Tool.Run(context.Background(), fmt.Sprintf(
		`{"path":"long.txt","offset":1,"byte_offset":%d}`, firstEnvelope.Limits.NextByteOffset,
	))
	if err != nil {
		t.Fatalf("continue long line: %v", err)
	}
	var continuedEnvelope readEnvelope
	if err := json.Unmarshal(continued.Details, &continuedEnvelope); err != nil {
		t.Fatal(err)
	}
	if continuedEnvelope.Limits.ByteOffset != firstEnvelope.Limits.NextByteOffset {
		t.Fatalf("continued byte offset = %#v", continuedEnvelope.Limits)
	}
	if runtime.GOOS != "windows" {
		symlinkRead, err := definition.Tool.Run(context.Background(), `{"path":"outside/escape.txt"}`)
		if err != nil || !strings.Contains(symlinkRead.ModelContent, "external through symlink") ||
			!strings.Contains(symlinkRead.ModelContent, filepath.ToSlash(filepath.Join(outside, "escape.txt"))) {
			t.Fatalf("symlink external read = %#v, %v", symlinkRead, err)
		}
	}
}

type recordingMutationAdapter struct {
	write WriteRequest
	edit  EditRequest
}

func (*recordingMutationAdapter) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.recording-mutation", Version: 1}
}

func (adapter *recordingMutationAdapter) Write(_ context.Context, request WriteRequest) (agent.ToolResult, error) {
	adapter.write = request
	return agent.TextToolResult(`{"schema":"mutation.test"}`), nil
}

func (adapter *recordingMutationAdapter) Edit(_ context.Context, request EditRequest) (agent.ToolResult, error) {
	adapter.edit = request
	return agent.TextToolResult(`{"schema":"mutation.test"}`), nil
}

func TestWriteAndEditPublishSmallExactInterfaces(t *testing.T) {
	adapter := &recordingMutationAdapter{}
	writeDefinition, err := Write(adapter)
	if err != nil {
		t.Fatal(err)
	}
	editDefinition, err := Edit(adapter)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		definition agent.ToolDefinition
		name       string
		arguments  string
	}{
		{writeDefinition, "write", `{"path":"ideas.md","content":"new"}`},
		{editDefinition, "edit", `{"path":"ideas.md","edits":[{"old_string":"old","new_string":"new","replace_all":true},{"old_string":"ending","new_string":"finale"}]}`},
	} {
		info, infoErr := test.definition.Tool.Info(context.Background())
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if info.Name != test.name {
			t.Fatalf("tool name = %q, want %q", info.Name, test.name)
		}
		if _, runErr := test.definition.Tool.Run(context.Background(), test.arguments); runErr != nil {
			t.Fatal(runErr)
		}
	}
	if adapter.write.Path != "ideas.md" || adapter.write.Content != "new" {
		t.Fatalf("write request = %#v", adapter.write)
	}
	if adapter.edit.Path != "ideas.md" || len(adapter.edit.Edits) != 2 ||
		adapter.edit.Operation != EditOperationReplace ||
		adapter.edit.Edits[0].OldString != "old" || adapter.edit.Edits[0].NewString != "new" || !adapter.edit.Edits[0].ReplaceAll ||
		adapter.edit.Edits[1].OldString != "ending" || adapter.edit.Edits[1].NewString != "finale" || adapter.edit.Edits[1].ReplaceAll {
		t.Fatalf("edit request = %#v", adapter.edit)
	}
	info, err := editDefinition.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties.Get("old_string"); ok {
		t.Fatalf("edit root must not expose the removed single-replacement fields: %#v", schema)
	}
	editsSchema, ok := schema.Properties.Get("edits")
	if !ok || editsSchema.Items == nil || editsSchema.MinItems == nil || *editsSchema.MinItems != 1 ||
		editsSchema.MaxItems == nil || *editsSchema.MaxItems != maxMutationEdits {
		t.Fatalf("edit batch schema = %#v", editsSchema)
	}
	for _, required := range []string{"old_string", "new_string"} {
		if !containsTestString(editsSchema.Items.Required, required) {
			t.Fatalf("edit item must require %q: %#v", required, editsSchema.Items)
		}
	}
	operationSchema, ok := schema.Properties.Get("operation")
	if !ok || len(operationSchema.Enum) != 2 || containsTestString(schema.Required, "operation") || containsTestString(schema.Required, "edits") {
		t.Fatalf("edit operation schema = %#v; required=%#v", operationSchema, schema.Required)
	}
	if _, err := editDefinition.Tool.Run(context.Background(), `{"path":"ideas.md","edits":[]}`); err == nil {
		t.Fatal("edit accepted an empty batch")
	}
	if _, err := editDefinition.Tool.Run(context.Background(), `{"path":"ideas.md","operation":"delete"}`); err != nil {
		t.Fatalf("explicit delete failed: %v", err)
	}
	if adapter.edit.Operation != EditOperationDelete || len(adapter.edit.Edits) != 0 {
		t.Fatalf("delete request = %#v", adapter.edit)
	}
	if _, err := editDefinition.Tool.Run(context.Background(), `{"path":"ideas.md","operation":"delete","edits":[{"old_string":"old","new_string":"new"}]}`); err != nil {
		t.Fatalf("delete with ignored edits failed: %v", err)
	}
	if adapter.edit.Operation != EditOperationDelete || len(adapter.edit.Edits) != 0 || adapter.edit.IgnoredEditCount != 1 {
		t.Fatalf("tolerant delete request = %#v", adapter.edit)
	}
	for _, invalid := range []string{
		`{"path":"ideas.md"}`,
		`{"path":"ideas.md","operation":"replace"}`,
		`{"path":"ideas.md","operation":"remove"}`,
	} {
		if _, err := editDefinition.Tool.Run(context.Background(), invalid); err == nil {
			t.Fatalf("edit accepted invalid operation shape: %s", invalid)
		}
	}
}

func TestLocalWorkspaceGlobAndGrep(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is unavailable")
	}
	root := t.TempDir()
	mustWriteTestFile(t, root, "chapters/one.md", "opening\n-dragon wakes\n")
	mustWriteTestFile(t, root, "chapters/two.md", "second dragon\n")
	mustWriteTestFile(t, root, "chapters/ignored.md", "ignored dragon\n")
	mustWriteTestFile(t, root, ".hidden.md", "hidden dragon\n")
	external := mustCanonicalTestPath(t, t.TempDir())
	mustWriteTestFile(t, external, "references/style.md", "external dragon style\n")
	if err := os.MkdirAll(filepath.Join(external, "empty-reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty-visible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty-ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteTestFile(t, root, ".gitignore", "chapters/ignored.md\nempty-ignored/\n")
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
		Root: root,
		Limits: WorkspaceLimits{
			DefaultDirectoryItems: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := workspace.Glob(context.Background(), GlobRequest{
		Paths: []string{"**/*.md", "empty-*"}, Hidden: true, Gitignore: true, Limit: 20,
	})
	if err != nil || strings.Contains(strings.Join(matches.Entries, "\n"), "ignored.md") ||
		strings.Contains(strings.Join(matches.Entries, "\n"), "empty-ignored") ||
		!containsTestString(matches.Entries, ".hidden.md") || !containsTestString(matches.Entries, "chapters/one.md") ||
		!containsTestString(matches.Entries, "empty-visible/") {
		t.Fatalf("glob = %#v, %v", matches, err)
	}
	ignored, err := workspace.Glob(context.Background(), GlobRequest{
		Paths: []string{"**/*.md"}, Hidden: true, Gitignore: false, Limit: 20,
	})
	if err != nil || !containsTestString(ignored.Entries, "chapters/ignored.md") {
		t.Fatalf("glob without gitignore = %#v, %v", ignored, err)
	}
	firstGlobPage, err := workspace.Glob(context.Background(), GlobRequest{
		Paths: []string{"**/*.md", "empty-*"}, Hidden: true, Gitignore: true, Limit: 1,
	})
	if err != nil || len(firstGlobPage.Entries) != 1 || !firstGlobPage.Truncated || firstGlobPage.NextCursor == "" {
		t.Fatalf("first glob page = %#v, %v", firstGlobPage, err)
	}
	secondGlobPage, err := workspace.Glob(context.Background(), GlobRequest{
		Paths: []string{"**/*.md", "empty-*"}, Hidden: true, Gitignore: true, Limit: 1, Cursor: firstGlobPage.NextCursor,
	})
	if err != nil || len(secondGlobPage.Entries) != 1 || secondGlobPage.Entries[0] == firstGlobPage.Entries[0] {
		t.Fatalf("second glob page = %#v, %v", secondGlobPage, err)
	}
	manyPaths := make([]string, 300)
	for index := range manyPaths {
		manyPaths[index] = "chapters/one.md"
	}
	if result, err := workspace.Glob(context.Background(), GlobRequest{Paths: manyPaths, Hidden: true, Gitignore: true}); err != nil || len(result.Entries) != 1 {
		t.Fatalf("glob should accept more than 256 requested paths: result=%#v err=%v", result, err)
	}
	externalPattern := filepath.ToSlash(filepath.Join(external, "**", "*.md"))
	externalMatches, err := workspace.Glob(context.Background(), GlobRequest{
		Paths: []string{externalPattern, filepath.Join(external, "empty-reference")}, Hidden: true, Gitignore: true, Limit: 20,
	})
	wantExternalFile := filepath.ToSlash(filepath.Join(external, "references", "style.md"))
	wantExternalDirectory := filepath.ToSlash(filepath.Join(external, "empty-reference")) + "/"
	if err != nil || !containsTestString(externalMatches.Entries, wantExternalFile) ||
		!containsTestString(externalMatches.Entries, wantExternalDirectory) {
		t.Fatalf("external glob = %#v, %v", externalMatches, err)
	}
	grepCommand := "rg -C 1 dragon"
	content, err := workspace.Grep(context.Background(), GrepRequest{Command: grepCommand})
	if err != nil || len(content.Entries) != 1 || !content.Truncated || content.NextCursor == "" {
		t.Fatalf("first grep page = %#v, %v", content, err)
	}
	if !strings.Contains(content.Entries[0], "dragon") || !strings.Contains(content.Entries[0], "opening") {
		t.Fatalf("grep split a logical context block: %#v", content.Entries)
	}
	next, err := workspace.Grep(context.Background(), GrepRequest{Command: grepCommand, Cursor: content.NextCursor})
	if err != nil || len(next.Entries) != 1 || next.Entries[0] == content.Entries[0] {
		t.Fatalf("second grep page = %#v, %v", next, err)
	}
	if _, err := workspace.Grep(context.Background(), GrepRequest{Command: "rg different", Cursor: content.NextCursor}); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("cursor was accepted for a different query: %v", err)
	}
	hidden, err := workspace.Grep(context.Background(), GrepRequest{Command: "rg 'hidden dragon'"})
	if err != nil || len(hidden.Entries) != 0 {
		t.Fatalf("grep should use native hidden-file defaults: result=%#v err=%v", hidden, err)
	}
	hidden, err = workspace.Grep(context.Background(), GrepRequest{Command: "rg --hidden 'hidden dragon'"})
	if err != nil || len(hidden.Entries) != 1 || !strings.Contains(hidden.Entries[0], ".hidden.md") {
		t.Fatalf("grep --hidden = %#v, %v", hidden, err)
	}
	unpaged := mustOpenTestWorkspace(t, root)
	files, err := unpaged.Grep(context.Background(), GrepRequest{Command: "rg -l dragon"})
	if err != nil || strings.Join(files.Entries, ",") != "chapters/one.md,chapters/two.md" {
		t.Fatalf("grep files mode = %#v, %v", files, err)
	}
	scoped, err := unpaged.Grep(context.Background(), GrepRequest{Command: "rg dragon chapters"})
	// Like native rg, an explicit path overrides ignore rules within that path.
	if err != nil || len(scoped.Entries) != 3 {
		t.Fatalf("grep native positional path = %#v, %v", scoped, err)
	}
	leadingDash, err := unpaged.Grep(context.Background(), GrepRequest{Command: "rg -- '-dragon' chapters"})
	if err != nil || len(leadingDash.Entries) != 1 || !strings.Contains(leadingDash.Entries[0], "-dragon") {
		t.Fatalf("grep delimiter pattern = %#v, %v", leadingDash, err)
	}
	counts, err := unpaged.Grep(context.Background(), GrepRequest{Command: "rg --count-matches dragon"})
	if err != nil || strings.Join(counts.Entries, ",") != "chapters/one.md:1,chapters/two.md:1" {
		t.Fatalf("grep count mode = %#v, %v", counts, err)
	}
	externalGrep, err := unpaged.Grep(context.Background(), GrepRequest{
		Command: "rg -n dragon -- " + quoteCanonicalGrepWord(filepath.Join(external, "references")),
	})
	if err != nil || len(externalGrep.Entries) != 1 || !strings.Contains(externalGrep.Entries[0], wantExternalFile) ||
		!strings.Contains(externalGrep.Entries[0], "external dragon style") {
		t.Fatalf("external grep = %#v, %v", externalGrep, err)
	}
}

func TestLocalWorkspaceGrepUsesConfiguredRipgrep(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DENOVA_TEST_RIPGREP_HELPER", "1")
	t.Setenv("DENOVA_TEST_RIPGREP_OUTPUT", "chapters/one.md\n")
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{Root: t.TempDir(), RipgrepExecutable: os.Args[0]})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := workspace.Grep(context.Background(), GrepRequest{Command: "rg dragon"})
	if err != nil || strings.Join(matches.Entries, "\n") != "chapters/one.md" {
		t.Fatalf("grep = %#v, %v", matches, err)
	}
}

func TestGrepCursorSupportsLargeContinuationOffsets(t *testing.T) {
	request := GrepRequest{Command: "rg dragon"}
	cursor, err := encodeGrepCursor(grepCursorState{Offset: 50_000, Prefix: "prefix"}, request)
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeGrepCursor(cursor, request)
	if err != nil || state.Offset != 50_000 || state.Prefix != "prefix" {
		t.Fatalf("large continuation cursor state=%#v err=%v", state, err)
	}
}

func TestGrepUsesDeterministicPathOrdering(t *testing.T) {
	workspace := mustOpenTestWorkspace(t, t.TempDir())
	command, err := workspace.compileGrepCommand("rg dragon")
	if err != nil {
		t.Fatal(err)
	}
	args := grepArguments(command.stages[0], false)
	if !containsTestString(args, "--sort=path") || !containsTestString(args, "--path-separator=/") {
		t.Fatalf("grep args are not deterministically sorted: %v", args)
	}
	if containsTestString(args, "--hidden") {
		t.Fatalf("grep should preserve native hidden-file defaults: %v", args)
	}
}

func TestSearchProjectionBudgetsEnvelopeAndRewritesCursor(t *testing.T) {
	request := normalizeGrepRequest(GrepRequest{Command: "rg dragon"})
	initial := grepCursorState{Offset: 7, Prefix: "previous-prefix"}
	entries := []string{
		strings.Repeat("a", 220), strings.Repeat("b", 220), strings.Repeat("c", 220),
		strings.Repeat("d", 220), strings.Repeat("e", 220),
	}
	result, err := searchToolResult("grep", SearchResult{Entries: entries}, 900, func(returned int) (string, error) {
		return encodeGrepCursor(grepCursorState{
			Offset: initial.Offset + returned,
			Prefix: advanceGrepPrefix(initial.Prefix, entries[:returned]),
		}, request)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ModelContent) > 900 {
		t.Fatalf("projected search has %d bytes", len(result.ModelContent))
	}
	parts := strings.Split(result.ModelContent, "\n")
	visible := len(parts) - 1
	if visible <= 0 || visible >= len(entries) {
		t.Fatalf("visible entries = %d, result=%q", visible, result.ModelContent)
	}
	var envelope searchEnvelope
	if err := json.Unmarshal([]byte(parts[0]), &envelope); err != nil {
		t.Fatal(err)
	}
	state, err := decodeGrepCursor(envelope.Limits.NextCursor, request)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Limits.Returned != visible || envelope.Limits.Unit != "output_entries" || state.Offset != 7+visible || !envelope.Limits.Truncated {
		t.Fatalf("search envelope = %#v cursor=%#v visible=%d", envelope, state, visible)
	}
}

type recordingSearcher struct {
	glob GlobRequest
	grep GrepRequest
}

func (*recordingSearcher) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.recording-searcher", Version: 1}
}

func (searcher *recordingSearcher) Glob(_ context.Context, request GlobRequest) (SearchResult, error) {
	searcher.glob = request
	return SearchResult{Entries: []string{"chapter.md"}}, nil
}

func (searcher *recordingSearcher) Grep(_ context.Context, request GrepRequest) (SearchResult, error) {
	searcher.grep = request
	return SearchResult{Entries: []string{"chapter.md:1:dragon"}}, nil
}

func TestSearchToolsPublishNewStrictInterfaces(t *testing.T) {
	searcher := &recordingSearcher{}
	glob, err := Glob(searcher)
	if err != nil {
		t.Fatal(err)
	}
	grep, err := Grep(searcher)
	if err != nil {
		t.Fatal(err)
	}
	grepInfo, err := grep.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	grepSchema, err := grepInfo.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := grepSchema.Properties.Get("command"); !ok || !containsTestString(grepSchema.Required, "command") {
		t.Fatalf("grep command schema = %#v", grepSchema)
	}
	if _, ok := grepSchema.Properties.Get("cursor"); !ok {
		t.Fatalf("grep cursor schema = %#v", grepSchema)
	}
	descriptionSchema, ok := grepSchema.Properties.Get("description")
	if !ok || containsTestString(grepSchema.Required, "description") ||
		!strings.Contains(descriptionSchema.Description, "same language as the user's current input") {
		t.Fatalf("grep description schema = %#v; required=%#v", descriptionSchema, grepSchema.Required)
	}
	commandSchema, _ := grepSchema.Properties.Get("command")
	if !strings.Contains(commandSchema.Description, "Always use rg, not grep") ||
		!strings.Contains(commandSchema.Description, "never add head, tail") {
		t.Fatalf("grep command guidance = %q", commandSchema.Description)
	}
	for _, removed := range []string{"pattern", "paths", "mode", "case_sensitive", "gitignore", "context_before", "context_after", "limit"} {
		if _, ok := grepSchema.Properties.Get(removed); ok {
			t.Fatalf("grep schema still exposes removed field %q: %#v", removed, grepSchema)
		}
	}
	if glob.Descriptor.ResultRecoveryKind != agent.ToolResultRecoveryRerun || grep.Descriptor.ResultRecoveryKind != agent.ToolResultRecoveryRerun {
		t.Fatalf("search result recovery = glob:%q grep:%q", glob.Descriptor.ResultRecoveryKind, grep.Descriptor.ResultRecoveryKind)
	}
	if grep.Descriptor.Presentation != agent.UniformToolPresentation(agent.ToolPresentationSearch) {
		t.Fatalf("grep presentation = %#v", grep.Descriptor.Presentation)
	}
	legacyIdentity := toolsetIdentity("tools.grep", searcher.Identity())
	if grep.ImplementationIdentity.ConfigHash == legacyIdentity.ConfigHash {
		t.Fatal("grep implementation identity did not rotate with the command contract")
	}
	if _, err := glob.Tool.Run(context.Background(), `{"paths":["chapters/**/*.md"],"hidden":false,"gitignore":false,"limit":5}`); err != nil {
		t.Fatal(err)
	}
	if searcher.glob.Hidden || searcher.glob.Gitignore || searcher.glob.Limit != 5 {
		t.Fatalf("glob request = %#v", searcher.glob)
	}
	if _, err := grep.Tool.Run(context.Background(), `{"command":"rg -i -l dragon -- chapters"}`); err != nil {
		t.Fatal(err)
	}
	if searcher.grep.Command != "rg -i -l dragon -- chapters" {
		t.Fatalf("grep request = %#v", searcher.grep)
	}
	if _, err := glob.Tool.Run(context.Background(), `{"pattern":"legacy"}`); err == nil {
		t.Fatal("glob accepted the removed pattern/path interface")
	}
	if _, err := grep.Tool.Run(context.Background(), `{"pattern":"legacy"}`); err == nil {
		t.Fatal("grep accepted the removed structured interface")
	}
	if _, err := grep.Tool.Run(context.Background(), `{"command":"rg dragon","output_mode":"content"}`); err != nil {
		t.Fatalf("grep rejected a harmless unknown parameter: %v", err)
	}
}

func TestCommandToolsPublishOptionalUserFacingDescription(t *testing.T) {
	for _, build := range []struct {
		name string
		tool func(CommandRunner, ...DefinitionOption) (agent.ToolDefinition, error)
	}{
		{name: "bash", tool: Bash},
		{name: "pwsh", tool: Pwsh},
	} {
		t.Run(build.name, func(t *testing.T) {
			definition, err := build.tool(fakeCommandRunner{})
			if err != nil {
				t.Fatal(err)
			}
			info, err := definition.Tool.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			schema, err := info.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			descriptionSchema, ok := schema.Properties.Get("description")
			if !ok || containsTestString(schema.Required, "description") ||
				!strings.Contains(descriptionSchema.Description, "user-facing description") {
				t.Fatalf("%s description schema = %#v; required=%#v", build.name, descriptionSchema, schema.Required)
			}
		})
	}
}

func TestBashReturnsOutputExitMetadataAndUsesGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash process assertion is Unix-specific")
	}
	workspace := mustOpenTestWorkspace(t, t.TempDir())
	var guarded atomic.Int32
	runner, err := NewLocalCommandRunner(CommandRunnerOptions{
		Workspace: workspace, Shell: ShellBash,
		Guard: func(ctx context.Context, run func() error) error {
			guarded.Add(1)
			return run()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Bash(runner)
	if err != nil {
		t.Fatal(err)
	}
	result, err := definition.Tool.Run(context.Background(), `{"command":"printf reusable"}`)
	if err != nil {
		t.Fatal(err)
	}
	if guarded.Load() != 1 || result.Status != agent.ToolResultSuccess ||
		!strings.Contains(result.ModelContent, "reusable") || !strings.Contains(result.ModelContent, `"exit_code":0`) {
		t.Fatalf("bash result = %#v guarded=%d", result, guarded.Load())
	}
	failed, err := definition.Tool.Run(context.Background(), `{"command":"printf diagnostic >&2; exit 7"}`)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != agent.ToolResultSuccess || !strings.Contains(failed.ModelContent, `"status":"failed"`) ||
		!strings.Contains(failed.ModelContent, `"exit_code":7`) || !strings.Contains(failed.ModelContent, "diagnostic") {
		t.Fatalf("failed bash result = %#v", failed)
	}
}

func TestBashUsesCapturedBaseEnvironmentAndExplicitOverrides(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash process assertion is Unix-specific")
	}
	workspace := mustOpenTestWorkspace(t, t.TempDir())
	runner, err := NewLocalCommandRunner(CommandRunnerOptions{
		Workspace: workspace,
		Shell:     ShellBash,
		BaseEnvironment: []string{
			"PATH=" + os.Getenv("PATH"),
			"DENOVA_PROFILE_VALUE=from-login-shell",
			"PWD=/stale/profile/directory",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), CommandRequest{
		Command: `printf '%s|%s' "$DENOVA_PROFILE_VALUE" "$PWD"`,
		Env:     map[string]string{"DENOVA_PROFILE_VALUE": "explicit-override"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "explicit-override|" + workspace.Root()
	if result.Output != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestBashStoresCompleteOutputArtifactWhileKeepingBoundedModelProjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash process assertion is Unix-specific")
	}
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
		Root: t.TempDir(), Limits: WorkspaceLimits{MaxResultBytes: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewLocalCommandRunner(CommandRunnerOptions{Workspace: workspace, Shell: ShellBash})
	if err != nil {
		t.Fatal(err)
	}
	artifactStore := &memoryToolArtifactStore{}
	ctx := agent.ContextWithToolArtifactStore(context.Background(), artifactStore)
	result, err := runner.Run(ctx, CommandRequest{
		Command: `for ((i=0;i<200;i++)); do printf 'line-%03d-abcdefghijklmnopqrstuvwxyz\n' "$i"; done`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OutputTruncated || result.Artifact == nil ||
		result.Artifact.Purpose != agent.ToolArtifactPurposeCompleteToolOutput || result.ArtifactError != "" ||
		result.OutputBytes != int64(len(artifactStore.content.String())) || len(artifactStore.content.String()) <= len(result.Output) {
		t.Fatalf("command artifact result = %#v artifact_bytes=%d", result, len(artifactStore.content.String()))
	}
	if !strings.Contains(artifactStore.content.String(), "line-199-abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("complete artifact lost output tail: %q", artifactStore.content.String())
	}
	projected, err := commandToolResult(result, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.ModelContent) > 1024 || len(projected.Artifacts) != 1 ||
		projected.Artifacts[0].Purpose != agent.ToolArtifactPurposeCompleteToolOutput ||
		!strings.Contains(projected.ModelContent, `"artifact"`) || !strings.Contains(projected.ModelContent, processTruncatedMarker) {
		t.Fatalf("bounded process projection = %#v", projected)
	}
	if strings.Contains(projected.ModelContent, result.Artifact.SHA256) || strings.Contains(string(projected.Details), `"sha256"`) ||
		strings.Contains(string(projected.Details), `"uri"`) || strings.Contains(string(projected.Details), `"byte_size"`) {
		t.Fatalf("process model projection leaked internal artifact metadata: %s", projected.ModelContent)
	}
}

func TestBashReportsSafeArtifactFailureAndLossyProjectionMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash process assertion is Unix-specific")
	}
	for _, test := range []struct {
		name        string
		failureMode string
		wantFailure string
	}{
		{name: "write", failureMode: "write", wantFailure: agent.ToolArtifactFailureWrite},
		{name: "commit", failureMode: "commit", wantFailure: agent.ToolArtifactFailureCommit},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
				Root: t.TempDir(), Limits: WorkspaceLimits{MaxResultBytes: 512},
			})
			if err != nil {
				t.Fatal(err)
			}
			runner, err := NewLocalCommandRunner(CommandRunnerOptions{Workspace: workspace, Shell: ShellBash})
			if err != nil {
				t.Fatal(err)
			}
			store := &failingToolArtifactStore{mode: test.failureMode}
			ctx := agent.ContextWithToolArtifactStore(context.Background(), store)
			result, err := runner.Run(ctx, CommandRequest{
				Command: `for ((i=0;i<200;i++)); do printf 'line-%03d-abcdefghijklmnopqrstuvwxyz\n' "$i"; done`,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !result.OutputTruncated || result.Artifact != nil || result.ArtifactError != test.wantFailure ||
				result.OutputBytes <= int64(len(result.Output)) || strings.Contains(result.ArtifactError, "credential") {
				t.Fatalf("lossy command result = %#v", result)
			}

			projected, err := commandToolResult(result, 1024)
			if err != nil {
				t.Fatal(err)
			}
			persistence := projected.Metadata.ArtifactPersistence
			if len(projected.ModelContent) > 1024 || !projected.Metadata.ModelTruncated ||
				projected.Metadata.OriginalModelBytes <= int(result.OutputBytes) ||
				persistence == nil || !persistence.Attempted || persistence.Complete ||
				persistence.FailureReason != test.wantFailure {
				t.Fatalf("projected artifact failure = %#v", projected)
			}
			var envelope processEnvelope
			if err := json.Unmarshal(projected.Details, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.ArtifactError != test.wantFailure || !envelope.OutputTruncated ||
				strings.Contains(string(projected.Details), "credential") {
				t.Fatalf("unsafe process envelope = %#v", envelope)
			}
		})
	}
}

func TestBashDoesNotExposeArtifactBeginFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash process assertion is Unix-specific")
	}
	workspace, err := OpenWorkspaceWithOptions(WorkspaceOptions{
		Root: t.TempDir(), Limits: WorkspaceLimits{MaxResultBytes: 512},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewLocalCommandRunner(CommandRunnerOptions{Workspace: workspace, Shell: ShellBash})
	if err != nil {
		t.Fatal(err)
	}
	ctx := agent.ContextWithToolArtifactStore(context.Background(), &failingToolArtifactStore{mode: "begin"})
	_, err = runner.Run(ctx, CommandRequest{Command: `printf should-not-start`}, nil)
	if err == nil || !strings.Contains(err.Error(), agent.ToolArtifactFailureBegin) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("unsafe artifact begin error = %v", err)
	}
}

func TestProcessProjectionKeepsMandatoryMetadataWithinBudget(t *testing.T) {
	result, err := commandToolResult(CommandResult{
		Status: ProcessStatusFailed, Shell: ShellBash, Engine: "bash", ExitCode: 7,
		Output: strings.Repeat("diagnostic", 200), Cwd: ".",
	}, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ModelContent) > 512 {
		t.Fatalf("projected process has %d bytes", len(result.ModelContent))
	}
	first, _, found := strings.Cut(result.ModelContent, "\n")
	if !found {
		t.Fatalf("missing projected output: %q", result.ModelContent)
	}
	var envelope processEnvelope
	if err := json.Unmarshal([]byte(first), &envelope); err != nil {
		t.Fatalf("metadata must be the complete first line: %v", err)
	}
	if envelope.ExitCode != 7 || envelope.Status != ProcessStatusFailed || !envelope.OutputTruncated {
		t.Fatalf("process envelope = %#v", envelope)
	}
}

func TestShellDescriptorDeclaresHostExternalEffects(t *testing.T) {
	definition, err := Bash(fakeCommandRunner{result: CommandResult{Shell: ShellBash, Engine: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	if definition.Descriptor.MutationScope != agent.ToolMutationExternal ||
		definition.Descriptor.PostCheck != agent.ToolPostCheckExternalReceipt ||
		definition.Descriptor.Recovery != agent.ToolRecoveryNonIdempotent {
		t.Fatalf("shell descriptor = %#v", definition.Descriptor)
	}
}

func TestBashSupportsCwdEnvTimeoutMergedOrderAndPTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash assertion is Unix-specific")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := mustOpenTestWorkspace(t, root)
	runner, err := NewLocalCommandRunner(CommandRunnerOptions{Workspace: workspace, Shell: ShellBash})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), CommandRequest{
		Command: `printf '%s:%s:' "$NOVA_TEST_VALUE" "$PWD"; printf out; printf err >&2; printf tail`,
		Cwd:     "sub", Env: map[string]string{"NOVA_TEST_VALUE": "env-ok"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ProcessStatusSuccess || result.Cwd != "sub" ||
		!strings.Contains(result.Output, "env-ok:"+filepath.Join(workspace.Root(), "sub")) ||
		!strings.Contains(result.Output, "outerrtail") {
		t.Fatalf("rich bash result = %#v", result)
	}
	ptyResult, err := runner.Run(context.Background(), CommandRequest{Command: `test -t 1 && printf pty-ok`, PTY: true}, nil)
	if err != nil || ptyResult.Status != ProcessStatusSuccess || !strings.Contains(ptyResult.Output, "pty-ok") {
		t.Fatalf("PTY result = %#v err=%v", ptyResult, err)
	}
	started := time.Now()
	timedOut, err := runner.Run(context.Background(), CommandRequest{Command: "sleep 5", TimeoutSeconds: 1}, nil)
	if err != nil || timedOut.Status != ProcessStatusTimedOut || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout result = %#v err=%v elapsed=%s", timedOut, err, time.Since(started))
	}
}

type fakeCommandRunner struct{ result CommandResult }

func (fakeCommandRunner) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{Kind: "test.fake-command-runner", Version: 1}
}

func (runner fakeCommandRunner) Run(context.Context, CommandRequest, func(string)) (CommandResult, error) {
	return runner.result, nil
}

type zeroIdentityReadAdapter struct{ ReadAdapter }

func (zeroIdentityReadAdapter) Identity() agent.CapabilityIdentity { return agent.CapabilityIdentity{} }

type zeroIdentitySearcher struct{ *recordingSearcher }

func (zeroIdentitySearcher) Identity() agent.CapabilityIdentity { return agent.CapabilityIdentity{} }

type zeroIdentityMutationAdapter struct{ *recordingMutationAdapter }

func (zeroIdentityMutationAdapter) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{}
}

type zeroIdentityCommandRunner struct{ fakeCommandRunner }

func (zeroIdentityCommandRunner) Identity() agent.CapabilityIdentity {
	return agent.CapabilityIdentity{}
}

type zeroIdentityTodoStore struct{}

func (zeroIdentityTodoStore) Identity() agent.CapabilityIdentity { return agent.CapabilityIdentity{} }
func (zeroIdentityTodoStore) Load(context.Context) ([]TodoItem, uint64, error) {
	return nil, 0, nil
}
func (zeroIdentityTodoStore) Apply(context.Context, TodoApplyRequest) (TodoApplyResult, error) {
	return TodoApplyResult{}, nil
}

func TestBuiltInToolFactoriesRejectInvalidAdapterIdentity(t *testing.T) {
	validRead, err := NewReadAdapter(agent.CapabilityIdentity{Kind: "test.read.identity-probe", Version: 1}, "identity-probe", func(context.Context, string) (bool, error) {
		return true, nil
	}, func(context.Context, struct {
		Path string `json:"path"`
	}) (ReadResult, error) {
		return ReadResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		err  error
	}{
		{"read", func() error { _, err := Read([]ReadAdapter{zeroIdentityReadAdapter{validRead}}); return err }()},
		{"glob", func() error { _, err := Glob(zeroIdentitySearcher{&recordingSearcher{}}); return err }()},
		{"grep", func() error { _, err := Grep(zeroIdentitySearcher{&recordingSearcher{}}); return err }()},
		{"write", func() error { _, err := Write(zeroIdentityMutationAdapter{&recordingMutationAdapter{}}); return err }()},
		{"edit", func() error { _, err := Edit(zeroIdentityMutationAdapter{&recordingMutationAdapter{}}); return err }()},
		{"bash", func() error { _, err := Bash(zeroIdentityCommandRunner{}); return err }()},
		{"todo", func() error {
			_, err := Todo(zeroIdentityTodoStore{}).PrepareTools(context.Background(), agent.ToolRequest{})
			return err
		}()},
	}
	for _, test := range tests {
		if test.err == nil {
			t.Fatalf("%s accepted an invalid adapter identity", test.name)
		}
	}
}

func TestSingleRequiresExplicitStableIdentity(t *testing.T) {
	tool, err := agent.InferTool("single_identity", "identity fixture", func(context.Context, struct{}) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := agent.ToolDefinition{Tool: tool, Descriptor: readDescriptor()}
	set := Single(agent.CapabilityIdentity{}, definition)
	if _, err := set.PrepareTools(context.Background(), agent.ToolRequest{}); err == nil {
		t.Fatalf("Single accepted invalid identity: error=%v", err)
	}
	identity := agent.CapabilityIdentity{Kind: "test.single-identity", Version: 1, ConfigHash: "v1"}
	set = Single(identity, definition)
	if _, err := set.PrepareTools(context.Background(), agent.ToolRequest{}); err != nil {
		t.Fatal(err)
	}
	if set.Identity().Kind != identity.Kind || set.Identity().Version != identity.Version || set.Identity().ConfigHash == "" {
		t.Fatalf("Single identity = %#v", set.Identity())
	}
}

type memoryToolArtifactStore struct {
	content strings.Builder
	purpose agent.ToolArtifactPurpose
}

func (store *memoryToolArtifactStore) BeginToolArtifact(_ context.Context, request agent.ToolArtifactRequest) (agent.ToolArtifactWriter, error) {
	store.content.Reset()
	store.purpose = request.Purpose
	return &memoryToolArtifactWriter{store: store}, nil
}

type memoryToolArtifactWriter struct {
	store    *memoryToolArtifactStore
	terminal bool
}

func (writer *memoryToolArtifactWriter) Write(data []byte) (int, error) {
	if writer.terminal {
		return 0, fmt.Errorf("artifact writer is closed")
	}
	return writer.store.content.Write(data)
}

func (writer *memoryToolArtifactWriter) Commit() (agent.ToolArtifactRef, error) {
	writer.terminal = true
	return agent.ToolArtifactRef{
		ID: "memory-artifact", Purpose: writer.store.purpose,
		ReadablePath: ".denova/artifacts/test/memory.log",
		ContentType:  "text/plain; charset=utf-8", EstimatedBytes: int64(writer.store.content.Len()),
		EstimatedTokens: (writer.store.content.Len() + 3) / 4, Complete: true, SHA256: strings.Repeat("0", 64),
	}, nil
}

func (writer *memoryToolArtifactWriter) Abort() error {
	writer.terminal = true
	return nil
}

type failingToolArtifactStore struct{ mode string }

func (store *failingToolArtifactStore) BeginToolArtifact(context.Context, agent.ToolArtifactRequest) (agent.ToolArtifactWriter, error) {
	if store.mode == "begin" {
		return nil, fmt.Errorf("credential=must-not-leak")
	}
	return &failingToolArtifactWriter{mode: store.mode}, nil
}

type failingToolArtifactWriter struct {
	mode     string
	terminal bool
}

func (writer *failingToolArtifactWriter) Write(data []byte) (int, error) {
	if writer.terminal {
		return 0, fmt.Errorf("artifact writer is closed")
	}
	if writer.mode == "write" {
		return 0, fmt.Errorf("credential=must-not-leak")
	}
	return len(data), nil
}

func (writer *failingToolArtifactWriter) Commit() (agent.ToolArtifactRef, error) {
	writer.terminal = true
	if writer.mode == "commit" {
		return agent.ToolArtifactRef{}, fmt.Errorf("credential=must-not-leak")
	}
	return agent.ToolArtifactRef{}, fmt.Errorf("unexpected commit")
}

func (writer *failingToolArtifactWriter) Abort() error {
	writer.terminal = true
	return nil
}

func TestPlatformShellToolsHaveDistinctNames(t *testing.T) {
	bash, err := Bash(fakeCommandRunner{result: CommandResult{Shell: ShellBash, Engine: "bash"}})
	if err != nil {
		t.Fatal(err)
	}
	pwsh, err := Pwsh(fakeCommandRunner{result: CommandResult{Shell: ShellPwsh, Engine: "pwsh"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		definition agent.ToolDefinition
		name       string
	}{{bash, "bash"}, {pwsh, "pwsh"}} {
		info, infoErr := test.definition.Tool.Info(context.Background())
		if infoErr != nil || info.Name != test.name {
			t.Fatalf("shell info = %#v err=%v", info, infoErr)
		}
	}
	_, args := shellCommand(ShellPwsh, "powershell.exe", "Get-ChildItem")
	if !containsTestString(args, "-ExecutionPolicy") {
		t.Fatalf("Windows PowerShell fallback args = %v", args)
	}
}

func TestOpenWorkspaceCanonicalizesAndRejectsInvalidRoots(t *testing.T) {
	root := t.TempDir()
	workspace := mustOpenTestWorkspace(t, filepath.Join(root, "."))
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Root() != filepath.Clean(canonical) {
		t.Fatalf("root = %q, want %q", workspace.Root(), canonical)
	}
	if _, err := OpenWorkspace(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing workspace should be rejected")
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWorkspace(file); err == nil {
		t.Fatal("file workspace should be rejected")
	}
}

func TestWorkspaceFailsClosedWhenRootPathIsReplaced(t *testing.T) {
	container := t.TempDir()
	root := filepath.Join(container, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := mustOpenTestWorkspace(t, root)
	if err := os.Rename(root, filepath.Join(container, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	opened, err := workspace.openRoot()
	if opened != nil {
		opened.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("replaced workspace root was accepted: %v", err)
	}
}

func TestWorkspaceDirectoryAndIgnoreScansEnforceAggregateBudgets(t *testing.T) {
	rootPath := t.TempDir()
	mustWriteTestFile(t, rootPath, "one.txt", "one")
	mustWriteTestFile(t, rootPath, ".gitignore", "*.tmp\n")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readFilesystemDirectory(context.Background(), root, ".", &filesystemScanBudget{entries: maxFilesystemScanEntries}); err == nil {
		t.Fatal("directory scan exceeded its aggregate entry budget")
	}
	if _, err := readFilesystemIgnorePatterns(context.Background(), root, ".", nil, &filesystemIgnoreBudget{bytes: maxFilesystemIgnoreBytes}); err == nil {
		t.Fatal("ignore scan exceeded its aggregate byte budget")
	}
}

func mustOpenTestWorkspace(t *testing.T, root string) *LocalWorkspace {
	t.Helper()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func mustCanonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func mustWriteTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsTestString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
