package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeProviderAdapter struct {
	cmd      CommandSpec
	buildErr error
	lastSpec LaunchSpec
}

func (f *fakeProviderAdapter) ProviderName() string { return "claude" }
func (f *fakeProviderAdapter) ExecutionMode() ExecutionMode { return ExecModePTY }
func (f *fakeProviderAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	f.lastSpec = spec
	if f.buildErr != nil {
		return CommandSpec{}, f.buildErr
	}
	return f.cmd, nil
}

type fakePTYSession struct {
	pid         int
	startErr    error
	outputCh    chan []byte
	exitCh      chan PTYExit
	writes      [][]byte
	interrupted bool
	killed      bool
}

func newFakePTYSession(pid int) *fakePTYSession {
	return &fakePTYSession{
		pid:      pid,
		outputCh: make(chan []byte, 8),
		exitCh:   make(chan PTYExit, 1),
	}
}

func (f *fakePTYSession) Start(cmd CommandSpec) (int, error) {
	if f.startErr != nil {
		return 0, f.startErr
	}
	return f.pid, nil
}
func (f *fakePTYSession) Write(data []byte) error {
	copied := append([]byte(nil), data...)
	f.writes = append(f.writes, copied)
	return nil
}
func (f *fakePTYSession) Interrupt() error            { f.interrupted = true; return nil }
func (f *fakePTYSession) Kill() error                 { f.killed = true; return nil }
func (f *fakePTYSession) Resize(cols, rows int) error { return nil }
func (f *fakePTYSession) Close() error                { return nil }
func (f *fakePTYSession) Output() <-chan []byte       { return f.outputCh }
func (f *fakePTYSession) Exit() <-chan PTYExit        { return f.exitCh }

type fakeExecutionStrategy struct {
	handle   ExecutionHandle
	startErr error
}

func (f *fakeExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f.handle, nil
}

type fakeExecutionHandle struct {
	pid            int
	outputCh       chan []byte
	exitCh         chan PTYExit
	writes         [][]byte
	interruptCalls int
	killCalls      int
}

func newFakeExecutionHandle(pid int) *fakeExecutionHandle {
	return &fakeExecutionHandle{
		pid:      pid,
		outputCh: make(chan []byte, 8),
		exitCh:   make(chan PTYExit, 1),
	}
}

func (f *fakeExecutionHandle) PID() int { return f.pid }
func (f *fakeExecutionHandle) Write(data []byte) error {
	copied := append([]byte(nil), data...)
	f.writes = append(f.writes, copied)
	return nil
}
func (f *fakeExecutionHandle) Interrupt() error      { f.interruptCalls++; return nil }
func (f *fakeExecutionHandle) Kill() error           { f.killCalls++; return nil }
func (f *fakeExecutionHandle) Output() <-chan []byte { return f.outputCh }
func (f *fakeExecutionHandle) Exit() <-chan PTYExit  { return f.exitCh }
func (f *fakeExecutionHandle) Close() error          { return nil }

type fakeWorkspacePreparer struct {
	workspace    *PreparedWorkspace
	prepareErr   error
	prepareCalls int
	lastSpec     LaunchSpec
}

func (f *fakeWorkspacePreparer) Prepare(sessionID string, spec LaunchSpec) (*PreparedWorkspace, error) {
	f.prepareCalls++
	f.lastSpec = spec
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	if f.workspace == nil {
		return &PreparedWorkspace{
			ProjectPath: spec.ProjectPath,
			RootPath:    spec.ProjectPath,
			Mode:        WorkspaceModeDirect,
		}, nil
	}
	return f.workspace, nil
}

type stubPreviewBuffer struct {
	delta *SessionPreviewDelta
}

func (s *stubPreviewBuffer) Append(sessionID string, lines []string) *SessionPreviewDelta {
	if s.delta == nil {
		return nil
	}
	clone := *s.delta
	clone.SessionID = sessionID
	return &clone
}

type stubEventExtractor struct {
	events []ImportantEvent
}

func (s *stubEventExtractor) Consume(session *RemoteSession, lines []string) []ImportantEvent {
	out := make([]ImportantEvent, len(s.events))
	copy(out, s.events)
	for i := range out {
		out[i].SessionID = session.ID
	}
	return out
}

type stubSummaryReducer struct {
	summary SessionSummary
}

func (s *stubSummaryReducer) Apply(current SessionSummary, events []ImportantEvent, lines []string) SessionSummary {
	out := s.summary
	out.SessionID = current.SessionID
	return out
}

func TestRemoteSessionManagerCreateUsesFactoriesAndStoresSession(t *testing.T) {
	app := &App{}
	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	execHandle := newFakeExecutionHandle(42)
	released := 0
	workspacePreparer := &fakeWorkspacePreparer{
		workspace: &PreparedWorkspace{
			ProjectPath: `D:\workprj\demo-workspace`,
			RootPath:    `D:\workprj\demo-root`,
			Mode:        WorkspaceModeDirect,
			Release: func() {
				released++
			},
		},
	}
	manager := NewRemoteSessionManager(app)
	manager.workspacePreparer = workspacePreparer
	manager.providerFactory = func(tool string) (ProviderAdapter, error) {
		if tool != "claude" {
			return nil, fmt.Errorf("unexpected tool: %s", tool)
		}
		return provider, nil
	}
	manager.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: execHandle}, nil
	}

	session, err := manager.Create(LaunchSpec{Tool: "claude", Title: "demo", ProjectPath: `D:\workprj\demo`, ModelID: "m1"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if session.PID != 42 {
		t.Fatalf("session pid = %d, want 42", session.PID)
	}
	if workspacePreparer.prepareCalls != 1 {
		t.Fatalf("prepare calls = %d, want 1", workspacePreparer.prepareCalls)
	}
	if provider.cmd.Command != "claude.exe" {
		t.Fatalf("provider cmd command = %q, want %q", provider.cmd.Command, "claude.exe")
	}
	if provider.lastSpec.ProjectPath != `D:\workprj\demo-workspace` {
		t.Fatalf("provider project path = %q, want prepared workspace path", provider.lastSpec.ProjectPath)
	}
	if session.ProjectPath != `D:\workprj\demo` {
		t.Fatalf("session project path = %q, want original project path", session.ProjectPath)
	}
	if session.WorkspacePath != `D:\workprj\demo-workspace` {
		t.Fatalf("workspace path = %q, want %q", session.WorkspacePath, `D:\workprj\demo-workspace`)
	}
	if session.WorkspaceRoot != `D:\workprj\demo-root` {
		t.Fatalf("workspace root = %q, want %q", session.WorkspaceRoot, `D:\workprj\demo-root`)
	}
	if session.Exec == nil {
		t.Fatal("expected execution handle to be assigned")
	}
	if session.Provider != provider {
		t.Fatalf("expected provider to be assigned")
	}
	if _, ok := manager.Get(session.ID); !ok {
		t.Fatalf("expected created session to be stored")
	}

	if err := manager.WriteInput(session.ID, "continue\n"); err != nil {
		t.Fatalf("WriteInput() error = %v", err)
	}
	// PTY mode normalizes \n → \r\n for ConPTY compatibility.
	if len(execHandle.writes) != 1 || string(execHandle.writes[0]) != "continue\r\n" {
		t.Fatalf("unexpected execution writes: %#v", execHandle.writes)
	}

	if err := manager.Interrupt(session.ID); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if execHandle.interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", execHandle.interruptCalls)
	}

	if err := manager.Kill(session.ID); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if execHandle.killCalls != 1 {
		t.Fatalf("kill calls = %d, want 1", execHandle.killCalls)
	}
	if released != 0 {
		t.Fatalf("release count before exit = %d, want 0", released)
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsOpencode(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("opencode")
	if err != nil {
		t.Fatalf("providerFactory(opencode) error = %v", err)
	}
	if provider.ProviderName() != "opencode" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "opencode")
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsIFlow(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("iflow")
	if err != nil {
		t.Fatalf("providerFactory(iflow) error = %v", err)
	}
	if provider.ProviderName() != "iflow" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "iflow")
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsKilo(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("kilo")
	if err != nil {
		t.Fatalf("providerFactory(kilo) error = %v", err)
	}
	if provider.ProviderName() != "kilo" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "kilo")
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsGemini(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("gemini")
	if err != nil {
		t.Fatalf("providerFactory(gemini) error = %v", err)
	}
	if provider.ProviderName() != "gemini" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "gemini")
	}
	if provider.ExecutionMode() != ExecModeGeminiACP {
		t.Fatalf("provider.ExecutionMode() = %q, want %q", provider.ExecutionMode(), ExecModeGeminiACP)
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsClaudeSDK(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("claude")
	if err != nil {
		t.Fatalf("providerFactory(claude) error = %v", err)
	}
	if provider.ProviderName() != "claude" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "claude")
	}
	if provider.ExecutionMode() != ExecModeSDK {
		t.Fatalf("provider.ExecutionMode() = %q, want %q", provider.ExecutionMode(), ExecModeSDK)
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsCursor(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("cursor")
	if err != nil {
		t.Fatalf("providerFactory(cursor) error = %v", err)
	}
	if provider.ProviderName() != "cursor" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "cursor")
	}
	if provider.ExecutionMode() != ExecModeSDK {
		t.Fatalf("provider.ExecutionMode() = %q, want %q", provider.ExecutionMode(), ExecModeSDK)
	}
}

func TestCodexAdapterBuildCommand(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.cmd"
	}
	binaryPath := filepath.Join(toolsDir, binaryName)
	if err := os.WriteFile(binaryPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(codex) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	adapter := NewCodexAdapter(&App{})

	// Verify SDK execution mode
	if adapter.ExecutionMode() != ExecModeCodexSDK {
		t.Fatalf("ExecutionMode() = %q, want %q", adapter.ExecutionMode(), ExecModeCodexSDK)
	}

	cmd, err := adapter.BuildCommand(LaunchSpec{
		Tool:        "codex",
		ProjectPath: projectDir,
		ModelID:     "gpt-5.2-codex",
		Env:         map[string]string{"OPENAI_API_KEY": "test-key"},
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	if cmd.Command == "" {
		t.Fatal("BuildCommand() command is empty")
	}
	if cmd.Cwd != projectDir {
		t.Fatalf("cmd.Cwd = %q, want %q", cmd.Cwd, projectDir)
	}
	// Verify exec --json args are present
	argsStr := strings.Join(cmd.Args, " ")
	if !strings.Contains(argsStr, "exec") {
		t.Fatalf("Args = %v, want 'exec' sub-command", cmd.Args)
	}
	if !strings.Contains(argsStr, "--json") {
		t.Fatalf("Args = %v, want '--json' flag", cmd.Args)
	}
	if !strings.Contains(argsStr, "--model") {
		t.Fatalf("Args = %v, want '--model' flag", cmd.Args)
	}
	if cmd.Env["OPENAI_MODEL"] != "gpt-5.2-codex" {
		t.Fatalf("OPENAI_MODEL = %q, want %q", cmd.Env["OPENAI_MODEL"], "gpt-5.2-codex")
	}
	if cmd.Env["WIRE_API"] != "responses" {
		t.Fatalf("WIRE_API = %q, want %q", cmd.Env["WIRE_API"], "responses")
	}
	if !strings.Contains(cmd.Env["PATH"], toolsDir) {
		t.Fatalf("PATH = %q, want it to contain %q", cmd.Env["PATH"], toolsDir)
	}
}

func TestCodexAdapterBuildCommandYoloMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.cmd"
	}
	binaryPath := filepath.Join(toolsDir, binaryName)
	if err := os.WriteFile(binaryPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(codex) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	adapter := NewCodexAdapter(&App{})
	cmd, err := adapter.BuildCommand(LaunchSpec{
		Tool:        "codex",
		ProjectPath: projectDir,
		ModelID:     "gpt-5.2-codex",
		YoloMode:    true,
		Env:         map[string]string{"OPENAI_API_KEY": "test-key"},
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	argsStr := strings.Join(cmd.Args, " ")
	if !strings.Contains(argsStr, "--full-auto") {
		t.Fatalf("Args = %v, want '--full-auto' flag in yolo mode", cmd.Args)
	}
}

func TestCodexAdapterBuildCommandOriginalMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.cmd"
	}
	binaryPath := filepath.Join(toolsDir, binaryName)
	if err := os.WriteFile(binaryPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(codex) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	adapter := NewCodexAdapter(&App{})
	cmd, err := adapter.BuildCommand(LaunchSpec{
		Tool:        "codex",
		ProjectPath: projectDir,
		ModelName:   "original",
		ModelID:     "gpt-5.2-codex",
		Env:         map[string]string{"OPENAI_API_KEY": "test-key"},
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	// In original mode, --model should NOT be in args
	argsStr := strings.Join(cmd.Args, " ")
	if strings.Contains(argsStr, "--model") {
		t.Fatalf("Args = %v, should NOT contain '--model' in original mode", cmd.Args)
	}
	// OPENAI_MODEL should NOT be set in original mode
	if cmd.Env["OPENAI_MODEL"] != "" {
		t.Fatalf("OPENAI_MODEL = %q, want empty in original mode", cmd.Env["OPENAI_MODEL"])
	}
}

func TestRemoteSessionManagerDefaultProviderFactorySupportsCodexSDK(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})

	provider, err := manager.providerFactory("codex")
	if err != nil {
		t.Fatalf("providerFactory(codex) error = %v", err)
	}
	if provider.ProviderName() != "codex" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "codex")
	}
	if provider.ExecutionMode() != ExecModeCodexSDK {
		t.Fatalf("provider.ExecutionMode() = %q, want %q", provider.ExecutionMode(), ExecModeCodexSDK)
	}
}

func TestRemoteSessionManagerCreateStoresFailedSessionWhenBuildCommandFails(t *testing.T) {
	app := &App{}
	provider := &fakeProviderAdapter{buildErr: fmt.Errorf("claude launch config missing")}
	manager := NewRemoteSessionManager(app)
	manager.workspacePreparer = &fakeWorkspacePreparer{
		workspace: &PreparedWorkspace{
			ProjectPath: `D:\workprj\demo`,
			RootPath:    `D:\workprj\demo`,
			Mode:        WorkspaceModeDirect,
		},
	}
	manager.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}

	session, err := manager.Create(LaunchSpec{Tool: "claude", ProjectPath: `D:\workprj\demo`})
	if err == nil {
		t.Fatal("Create() error = nil, want build command error")
	}
	if session == nil {
		t.Fatal("Create() session = nil, want failed session")
	}
	if session.Status != SessionError {
		t.Fatalf("session status = %q, want %q", session.Status, SessionError)
	}
	if session.Summary.LastResult != "claude launch config missing" {
		t.Fatalf("summary last result = %q", session.Summary.LastResult)
	}
	if len(session.Preview.PreviewLines) != 1 {
		t.Fatalf("preview lines = %#v, want launch failure line", session.Preview.PreviewLines)
	}
	if len(session.Events) != 1 || session.Events[0].Type != "session.failed" {
		t.Fatalf("events = %#v, want one session.failed event", session.Events)
	}
	if _, ok := manager.Get(session.ID); !ok {
		t.Fatal("failed session was not stored")
	}
}

func TestRemoteSessionManagerCreateStoresFailedSessionWhenPTYStartFails(t *testing.T) {
	app := &App{}
	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	manager := NewRemoteSessionManager(app)
	manager.workspacePreparer = &fakeWorkspacePreparer{
		workspace: &PreparedWorkspace{
			ProjectPath: `D:\workprj\demo`,
			RootPath:    `D:\workprj\demo`,
			Mode:        WorkspaceModeDirect,
		},
	}
	manager.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	manager.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{startErr: fmt.Errorf("conpty unavailable")}, nil
	}

	session, err := manager.Create(LaunchSpec{Tool: "claude", ProjectPath: `D:\workprj\demo`, Title: "demo"})
	if err == nil {
		t.Fatal("Create() error = nil, want start error")
	}
	if session == nil {
		t.Fatal("Create() session = nil, want failed session")
	}
	if session.Status != SessionError {
		t.Fatalf("session status = %q, want %q", session.Status, SessionError)
	}
	if session.Summary.CurrentTask != "Starting Claude session" {
		t.Fatalf("summary current task = %q", session.Summary.CurrentTask)
	}
	if session.Preview.OutputSeq != 1 {
		t.Fatalf("preview output seq = %d, want 1", session.Preview.OutputSeq)
	}
}

func TestRemoteSessionManagerCreateStoresFailedSessionWhenWorkspacePrepareFails(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.workspacePreparer = &fakeWorkspacePreparer{prepareErr: fmt.Errorf("workspace locked")}

	session, err := manager.Create(LaunchSpec{Tool: "claude", ProjectPath: `D:\workprj\demo`})
	if err == nil {
		t.Fatal("Create() error = nil, want workspace prepare error")
	}
	if session == nil {
		t.Fatal("Create() session = nil, want failed session")
	}
	if session.Status != SessionError {
		t.Fatalf("session status = %q, want %q", session.Status, SessionError)
	}
	if session.Summary.LastResult != "workspace locked" {
		t.Fatalf("summary last result = %q, want %q", session.Summary.LastResult, "workspace locked")
	}
}

func TestRemoteSessionManagerRunOutputLoopUpdatesSession(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	manager.pipelineFactory = func() *OutputPipeline {
		return &OutputPipeline{
			buffer:  &stubPreviewBuffer{delta: &SessionPreviewDelta{OutputSeq: 1, AppendLines: []string{"Running go test ./..."}, UpdatedAt: time.Now().Unix()}},
			extract: &stubEventExtractor{events: []ImportantEvent{{Type: "command.started", Title: "Running command", CreatedAt: time.Now().Unix()}}},
			reducer: &stubSummaryReducer{summary: SessionSummary{Status: string(SessionBusy), Severity: "info", CurrentTask: "Running tests", UpdatedAt: time.Now().Unix()}},
		}
	}

	execHandle := newFakeExecutionHandle(7)
	session := &RemoteSession{
		ID:      "session-1",
		Tool:    "claude",
		Title:   "demo",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "session-1", Status: string(SessionStarting)},
		Exec:    execHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runOutputLoop(session)
		close(done)
	}()

	execHandle.outputCh <- []byte("Running go test ./...\n")
	close(execHandle.outputCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runOutputLoop did not finish")
	}

	if session.Status != SessionBusy {
		t.Fatalf("session status = %q, want %q", session.Status, SessionBusy)
	}
	if session.Summary.CurrentTask != "Running tests" {
		t.Fatalf("summary current task = %q", session.Summary.CurrentTask)
	}
	if session.Preview.OutputSeq != 1 {
		t.Fatalf("preview output seq = %d, want 1", session.Preview.OutputSeq)
	}
	if len(session.Preview.PreviewLines) != 1 || session.Preview.PreviewLines[0] != "Running go test ./..." {
		t.Fatalf("unexpected preview lines: %#v", session.Preview.PreviewLines)
	}
}

func TestRemoteSessionManagerRunExitLoopUpdatesExitState(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	execHandle := newFakeExecutionHandle(9)
	released := 0
	session := &RemoteSession{
		ID:     "session-1",
		Exec:   execHandle,
		Status: SessionBusy,
		workspaceRelease: func() {
			released++
		},
	}

	done := make(chan struct{})
	go func() {
		manager.runExitLoop(session)
		close(done)
	}()

	exitCode := 0
	execHandle.exitCh <- PTYExit{Code: &exitCode}
	close(execHandle.exitCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runExitLoop did not finish")
	}

	if session.Status != SessionExited {
		t.Fatalf("session status = %q, want %q", session.Status, SessionExited)
	}
	if session.ExitCode == nil || *session.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %#v", session.ExitCode)
	}
	if released != 1 {
		t.Fatalf("release count = %d, want 1", released)
	}
	if session.Summary.Status != string(SessionExited) {
		t.Fatalf("summary status = %q, want %q", session.Summary.Status, SessionExited)
	}
	if session.Summary.LastResult != "Session exited with code 0" {
		t.Fatalf("summary last result = %q", session.Summary.LastResult)
	}
	if len(session.Events) != 1 || session.Events[0].Type != "session.closed" {
		t.Fatalf("events = %#v, want one session.closed event", session.Events)
	}
}

func TestBuildSessionInitEvent(t *testing.T) {
	session := &RemoteSession{
		ID:          "sess-1",
		Tool:        "claude",
		ProjectPath: `D:\workprj\demo`,
	}

	event := buildSessionInitEvent(session)
	if event.Type != "session.init" {
		t.Fatalf("event.Type = %q, want %q", event.Type, "session.init")
	}
	if event.Count != 1 {
		t.Fatalf("event.Count = %d, want 1", event.Count)
	}
	if event.Title != "Session started" {
		t.Fatalf("event.Title = %q, want %q", event.Title, "Session started")
	}
	if event.SessionID != session.ID {
		t.Fatalf("event.SessionID = %q, want %q", event.SessionID, session.ID)
	}
}

func TestBuildSessionFailedEvent(t *testing.T) {
	session := &RemoteSession{ID: "sess-1"}

	event := buildSessionFailedEvent(session, fmt.Errorf("launch failed"))
	if event.Type != "session.failed" {
		t.Fatalf("event.Type = %q, want %q", event.Type, "session.failed")
	}
	if event.Summary != "launch failed" {
		t.Fatalf("event.Summary = %q, want %q", event.Summary, "launch failed")
	}
	if event.Count != 1 {
		t.Fatalf("event.Count = %d, want 1", event.Count)
	}
}

func TestBuildSessionClosedEventWithError(t *testing.T) {
	session := &RemoteSession{ID: "sess-1"}

	event := buildSessionClosedEvent(session, PTYExit{Err: fmt.Errorf("pty disconnected")})
	if event.Type != "session.closed" {
		t.Fatalf("event.Type = %q, want %q", event.Type, "session.closed")
	}
	if event.Severity != "error" {
		t.Fatalf("event.Severity = %q, want %q", event.Severity, "error")
	}
	if event.Title != "Session crashed" {
		t.Fatalf("event.Title = %q, want %q", event.Title, "Session crashed")
	}
	if event.Count != 1 {
		t.Fatalf("event.Count = %d, want 1", event.Count)
	}
}

func TestOutputPipelineDedupesRepeatedEvents(t *testing.T) {
	pipeline := NewOutputPipeline()
	events := []ImportantEvent{
		{Type: "command.started", Command: "go test ./...", Summary: "Running go test ./..."},
		{Type: "command.started", Command: "go test ./...", Summary: "Running go test ./..."},
		{Type: "file.change", RelatedFile: "main.go", Summary: "Modified main.go"},
		{Type: "file.change", RelatedFile: "main.go", Summary: "Modified main.go"},
	}

	filtered := pipeline.filterDuplicateEvents(events)
	if len(filtered) != 2 {
		t.Fatalf("filtered event count = %d, want 2", len(filtered))
	}
	if filtered[0].Type != "command.started" {
		t.Fatalf("filtered[0].Type = %q, want %q", filtered[0].Type, "command.started")
	}
	if filtered[1].Type != "file.change" {
		t.Fatalf("filtered[1].Type = %q, want %q", filtered[1].Type, "file.change")
	}
}

func TestOutputPipelineCoalescesFileChangeEvents(t *testing.T) {
	pipeline := NewOutputPipeline()
	events := []ImportantEvent{
		{Type: "file.change", RelatedFile: "a.go", Summary: "Modified a.go"},
		{Type: "file.change", RelatedFile: "b.go", Summary: "Modified b.go"},
		{Type: "file.change", RelatedFile: "c.go", Summary: "Modified c.go"},
		{Type: "command.started", Command: "go test ./...", Summary: "Running go test ./..."},
	}

	merged := pipeline.coalesceEvents(events)
	if len(merged) != 2 {
		t.Fatalf("merged event count = %d, want 2", len(merged))
	}
	if merged[0].Type != "file.change" {
		t.Fatalf("merged[0].Type = %q, want %q", merged[0].Type, "file.change")
	}
	if merged[0].Title != "Changed 3 files" {
		t.Fatalf("merged[0].Title = %q, want %q", merged[0].Title, "Changed 3 files")
	}
	if !merged[0].Grouped || merged[0].Count != 3 {
		t.Fatalf("merged[0] grouped/count = %v/%d, want true/3", merged[0].Grouped, merged[0].Count)
	}
	if merged[1].Type != "command.started" {
		t.Fatalf("merged[1].Type = %q, want %q", merged[1].Type, "command.started")
	}
}

func TestOutputPipelineCoalescesFileReadEvents(t *testing.T) {
	pipeline := NewOutputPipeline()
	events := []ImportantEvent{
		{Type: "file.read", RelatedFile: "a.go", Summary: "Read a.go"},
		{Type: "file.read", RelatedFile: "b.go", Summary: "Read b.go"},
	}

	merged := pipeline.coalesceEvents(events)
	if len(merged) != 1 {
		t.Fatalf("merged event count = %d, want 1", len(merged))
	}
	if merged[0].Title != "Inspected 2 files" {
		t.Fatalf("merged[0].Title = %q, want %q", merged[0].Title, "Inspected 2 files")
	}
	if !merged[0].Grouped || merged[0].Count != 2 {
		t.Fatalf("merged[0] grouped/count = %v/%d, want true/2", merged[0].Grouped, merged[0].Count)
	}
}

func TestOutputPipelineCoalescesFileChangesAcrossChunks(t *testing.T) {
	pipeline := NewOutputPipeline()
	session := &RemoteSession{ID: "session-1"}

	first := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "file.change", RelatedFile: "a.go", Summary: "Modified a.go"},
	})
	second := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "file.change", RelatedFile: "b.go", Summary: "Modified b.go"},
	})

	if len(first) != 1 {
		t.Fatalf("first event count = %d, want 1", len(first))
	}
	if len(second) != 1 {
		t.Fatalf("second event count = %d, want 1", len(second))
	}
	if second[0].Title != "Changed 2 files" {
		t.Fatalf("second[0].Title = %q, want %q", second[0].Title, "Changed 2 files")
	}
	if !second[0].Grouped || second[0].Count != 2 {
		t.Fatalf("second[0] grouped/count = %v/%d, want true/2", second[0].Grouped, second[0].Count)
	}
}

func TestOutputPipelineSuppressesDuplicateBurstFiles(t *testing.T) {
	pipeline := NewOutputPipeline()
	session := &RemoteSession{ID: "session-1"}

	first := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "file.read", RelatedFile: "a.go", Summary: "Read a.go"},
	})
	second := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "file.read", RelatedFile: "a.go", Summary: "Read a.go"},
	})

	if len(first) != 1 {
		t.Fatalf("first event count = %d, want 1", len(first))
	}
	if len(second) != 0 {
		t.Fatalf("second event count = %d, want 0", len(second))
	}
}

func TestOutputPipelineSuppressesDuplicateCommandsAcrossChunks(t *testing.T) {
	pipeline := NewOutputPipeline()
	session := &RemoteSession{ID: "session-1"}

	first := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "command.started", Command: "go test ./...", Summary: "Running go test ./..."},
	})
	second := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "command.started", Command: "go test ./...", Summary: "Running go test ./..."},
	})
	third := pipeline.coalesceAcrossBursts(session, []ImportantEvent{
		{Type: "command.started", Command: "go build ./...", Summary: "Running go build ./..."},
	})

	if len(first) != 1 {
		t.Fatalf("first event count = %d, want 1", len(first))
	}
	if len(second) != 0 {
		t.Fatalf("second event count = %d, want 0", len(second))
	}
	if len(third) != 1 {
		t.Fatalf("third event count = %d, want 1", len(third))
	}
	if third[0].Command != "go build ./..." {
		t.Fatalf("third[0].Command = %q, want %q", third[0].Command, "go build ./...")
	}
}

func TestAppendRecentEventsKeepsLatestItems(t *testing.T) {
	events := []ImportantEvent{
		{Type: "session.init", Summary: "Session started"},
		{Type: "file.read", Summary: "Read a.go"},
	}

	for i := 0; i < 5; i++ {
		events = appendRecentEvents(events, ImportantEvent{
			Type:    "command.started",
			Summary: fmt.Sprintf("Run %d", i),
		}, 5)
	}

	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if events[0].Summary != "Run 0" {
		t.Fatalf("events[0].Summary = %q, want %q", events[0].Summary, "Run 0")
	}
	if events[4].Summary != "Run 4" {
		t.Fatalf("events[4].Summary = %q, want %q", events[4].Summary, "Run 4")
	}
}
