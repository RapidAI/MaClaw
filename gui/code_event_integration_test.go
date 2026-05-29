package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// extractFilePathFromSummary tests
// ---------------------------------------------------------------------------

func TestExtractFilePathFromSummary(t *testing.T) {
	cases := []struct {
		summary string
		want    string
	}{
		{"Inspected /project/main.go", "/project/main.go"},
		{"Modified /project/src/app.ts", "/project/src/app.ts"},
		{"Read /project/config.yaml", "/project/config.yaml"},
		{"Edited /project/lib.rs", "/project/lib.rs"},
		{"Created /project/new_file.py", "/project/new_file.py"},
		{"Wrote /project/output.json", "/project/output.json"},
		{"Some random summary", ""},
		{"", ""},
		{"Tool: Bash", ""},
	}
	for _, tc := range cases {
		t.Run(tc.summary, func(t *testing.T) {
			got := extractFilePathFromSummary(tc.summary)
			if got != tc.want {
				t.Errorf("extractFilePathFromSummary(%q) = %q, want %q", tc.summary, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// maxCodeFileSize constant test
// ---------------------------------------------------------------------------

func TestMaxCodeFileSize(t *testing.T) {
	if maxCodeFileSize != 1<<20 {
		t.Errorf("maxCodeFileSize = %d, want %d (1 MB)", maxCodeFileSize, 1<<20)
	}
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — nil safety tests
// ---------------------------------------------------------------------------

func TestEmitCodeFileEvents_NilEmitter(t *testing.T) {
	m := &RemoteSessionManager{
		app: &App{}, // codeEventEmitter is nil
	}
	s := &RemoteSession{ID: "test-session"}
	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "main.go"},
	}
	// Should not panic when codeEventEmitter is nil
	m.emitCodeFileEvents(s, events)
}

func TestEmitCodeFileEvents_NilApp(t *testing.T) {
	m := &RemoteSessionManager{
		app: nil,
	}
	s := &RemoteSession{ID: "test-session"}
	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "main.go"},
	}
	// Should not panic when app is nil
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — event filtering tests
// ---------------------------------------------------------------------------

func TestEmitCodeFileEvents_SkipsNonFileEvents(t *testing.T) {
	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session"}

	// These event types should be skipped
	events := []ImportantEvent{
		{Type: "command.started", Summary: "Running: go test"},
		{Type: "command.success", Summary: "Tests passed"},
		{Type: "session.error", Summary: "Error detected"},
		{Type: "task.completed", Summary: "Task completed"},
		{Type: "tool.use", Summary: "Tool: Bash"},
	}
	// Should not panic and should skip all events (no file path to read)
	m.emitCodeFileEvents(s, events)
}

func TestEmitCodeFileEvents_SkipsEmptyRelatedFile(t *testing.T) {
	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session"}

	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "", Summary: ""},
	}
	// Should not panic — event has no file path
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — file size guard test
// ---------------------------------------------------------------------------

func TestEmitCodeFileEvents_SkipsLargeFiles(t *testing.T) {
	// Create a temp file larger than 1MB
	tmpDir := t.TempDir()
	largePath := filepath.Join(tmpDir, "large.go")
	data := make([]byte, maxCodeFileSize+1)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(largePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session", ProjectPath: tmpDir}

	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "large.go"},
	}
	// Should not panic — file is too large, event should be skipped
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — reads file content from disk
// ---------------------------------------------------------------------------

func TestEmitCodeFileEvents_ReadsFileContent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "hello.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session", ProjectPath: tmpDir}

	events := []ImportantEvent{
		{Type: "file.read", RelatedFile: "hello.go"},
	}
	// Should not panic — reads file content from disk
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — file.change determines opType as "modify"
// ---------------------------------------------------------------------------

func TestEmitCodeFileEvents_FileChangeOpType(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "app.ts")
	if err := os.WriteFile(testFile, []byte("const x = 1;"), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session", ProjectPath: tmpDir}

	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "app.ts"},
	}
	// Should not panic — processes file.change event
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// emitCodeFileEvents — extracts file path from SDK summary
// ---------------------------------------------------------------------------

func TestBuildCodingSubAgentCodeFileEvents(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	modifiedPath := filepath.Join(tmpDir, "src", "app.ts")
	createdPath := filepath.Join(tmpDir, "src", "new.go")
	if err := os.WriteFile(modifiedPath, []byte("export const n = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(createdPath, []byte("package src\n"), 0644); err != nil {
		t.Fatal(err)
	}

	events := buildCodingSubAgentCodeFileEvents(
		"subagent-test",
		tmpDir,
		[]string{"src/app.ts", filepath.Join(tmpDir, "src", "new.go")},
		[]string{"src/new.go"},
	)

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(events), events)
	}
	if events[0].SessionID != "subagent-test" || events[0].FilePath != "src/app.ts" || events[0].FileName != "app.ts" || events[0].OpType != "modify" || events[0].Language != "typescript" {
		t.Fatalf("modified event = %#v", events[0])
	}
	if !events[0].ForceOpen {
		t.Fatalf("modified event ForceOpen = false, want true")
	}
	if events[0].Content != "export const n = 1\n" {
		t.Fatalf("modified content = %q", events[0].Content)
	}
	if events[1].FilePath != "src/new.go" || events[1].FileName != "new.go" || events[1].OpType != "create" || events[1].Language != "go" {
		t.Fatalf("created event = %#v", events[1])
	}
	if events[1].Original != "" {
		t.Fatalf("created original = %q, want empty", events[1].Original)
	}
}

func TestBuildCodingSubAgentCodeFileEventsSkipsMissingAndLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	largePath := filepath.Join(tmpDir, "large.go")
	if err := os.WriteFile(largePath, make([]byte, maxCodeFileSize+1), 0644); err != nil {
		t.Fatal(err)
	}

	events := buildCodingSubAgentCodeFileEvents("subagent-test", tmpDir, []string{"missing.go", "large.go"}, nil)
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
}

func TestBuildCodingSubAgentCodeFileEventsSkipsBinaryFiles(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "asset.bin")
	if err := os.WriteFile(binaryPath, []byte{'p', 'k', 0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	invalidUTF8Path := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(invalidUTF8Path, []byte{0xff, 0xfe, 0xfd}, 0644); err != nil {
		t.Fatal(err)
	}

	events := buildCodingSubAgentCodeFileEvents("subagent-test", tmpDir, []string{"asset.bin", "invalid.txt"}, nil)
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
}

func TestBuildCodingSubAgentCodeFileEventsSkipsOutsideProject(t *testing.T) {
	projectDir := t.TempDir()
	outsideDir := t.TempDir()
	insidePath := filepath.Join(projectDir, "inside.go")
	outsidePath := filepath.Join(outsideDir, "outside.go")
	if err := os.WriteFile(insidePath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("package outside\n"), 0644); err != nil {
		t.Fatal(err)
	}

	events := buildCodingSubAgentCodeFileEvents("subagent-test", projectDir, []string{"inside.go", outsidePath}, nil)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(events), events)
	}
	if events[0].FilePath != "inside.go" {
		t.Fatalf("event path = %q, want inside.go", events[0].FilePath)
	}
}

func TestIsCodePreviewTextContentAllowsUTF8Text(t *testing.T) {
	if !isCodePreviewTextContent([]byte("package main\n// 中文\n")) {
		t.Fatal("valid UTF-8 source text should be allowed")
	}
}

func TestCodingSubAgentCodeSessionID(t *testing.T) {
	if got := codingSubAgentCodeSessionID(" delegate ", " user-1 "); got != "delegate:user-1" {
		t.Fatalf("session id = %q, want delegate:user-1", got)
	}
	if got := codingSubAgentCodeSessionID("", ""); got != "subagent" {
		t.Fatalf("empty session id = %q, want subagent", got)
	}
}

func TestNewCodingSubAgentCodeSessionIDIsUniquePerRun(t *testing.T) {
	first := newCodingSubAgentCodeSessionID("delegate", "user-1")
	second := newCodingSubAgentCodeSessionID("delegate", "user-1")
	if first == second {
		t.Fatalf("session ids should be unique per run, both were %q", first)
	}
	if !strings.HasPrefix(first, "delegate:user-1:") || !strings.HasPrefix(second, "delegate:user-1:") {
		t.Fatalf("session ids should keep base prefix, got %q and %q", first, second)
	}
}

func TestEmitCodeFileEvents_ExtractsFromSummary(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	emitter := NewCodeEventEmitter(app)
	app.codeEventEmitter = emitter

	m := &RemoteSessionManager{app: app}
	s := &RemoteSession{ID: "test-session", ProjectPath: tmpDir}

	// SDK events may not have RelatedFile set, but have file path in Summary
	absPath := filepath.Join(tmpDir, "main.go")
	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "", Summary: "Modified " + absPath},
	}
	// Should not panic — extracts file path from summary
	m.emitCodeFileEvents(s, events)
}

// ---------------------------------------------------------------------------
// gitShowOriginal tests
// ---------------------------------------------------------------------------

func TestGitShowOriginal_EmptyProjectPath(t *testing.T) {
	result := gitShowOriginal("", "/some/file.go")
	if result != "" {
		t.Errorf("gitShowOriginal with empty project path should return empty string, got %q", result)
	}
}

func TestGitShowOriginal_NonGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	// tmpDir is not a git repo, so git show should fail gracefully
	result := gitShowOriginal(tmpDir, testFile)
	if result != "" {
		t.Errorf("gitShowOriginal in non-git dir should return empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// App.codeEventEmitter initialization test
// ---------------------------------------------------------------------------

func TestApp_CodeEventEmitter_InitializedInStartup(t *testing.T) {
	// Verify the field exists on App struct and can be set
	app := &App{}
	app.codeEventEmitter = NewCodeEventEmitter(app)
	if app.codeEventEmitter == nil {
		t.Fatal("codeEventEmitter should not be nil after initialization")
	}
	if app.codeEventEmitter.app != app {
		t.Error("codeEventEmitter.app should reference the App instance")
	}
}
