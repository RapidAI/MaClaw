package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRemoteSDKBusyIdleTimeoutIsConservative(t *testing.T) {
	if remoteSDKBusyIdleTimeout < 10*time.Minute {
		t.Fatalf("remoteSDKBusyIdleTimeout = %s, want at least 10m", remoteSDKBusyIdleTimeout)
	}
}

func TestBuildGuideLaunchInjectionMarksReferenceOnly(t *testing.T) {
	got := buildGuideLaunchInjection(" saved context ")
	if !strings.Contains(got, "saved context") || !strings.Contains(got, "Guide launch reference") {
		t.Fatalf("reference injection missing marker or content: %q", got)
	}
	if !strings.Contains(got, "Do not treat it as a new user turn") {
		t.Fatalf("reference injection should prevent treating context as user input: %q", got)
	}
	if buildGuideLaunchInjection("   ") != "" {
		t.Fatalf("empty guide launch text should not create an injection")
	}
}

func TestNormalizeAIAssistantSessionUserID(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty falls back", input: " ", want: ""},
		{name: "local", input: desktopUserID, want: desktopUserID},
		{name: "project", input: desktopUserID + ":D:/work/project", want: desktopUserID + ":D:/work/project"},
		{name: "project path is trimmed", input: desktopUserID + ":  D:/work/project  ", want: desktopUserID + ":D:/work/project"},
		{name: "empty project rejected", input: desktopUserID + ":", wantErr: true},
		{name: "foreign user rejected", input: "weixin:user", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAIAssistantSessionUserID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeAIAssistantSessionUserID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestActiveAIAssistantLoopUserIDIgnoresNonDesktopUsers(t *testing.T) {
	h := &IMMessageHandler{lastUserID: "weixin:user"}
	if got := activeAIAssistantLoopUserID(h); got != desktopUserID {
		t.Fatalf("activeAIAssistantLoopUserID() = %q, want %q", got, desktopUserID)
	}

	h.lastUserID = desktopUserID + ":D:/work/project"
	if got := activeAIAssistantLoopUserID(h); got != h.lastUserID {
		t.Fatalf("activeAIAssistantLoopUserID() = %q, want project userID", got)
	}
}

func TestInjectGuideReferenceTargetsOnlyExplicitActiveSession(t *testing.T) {
	h := &IMMessageHandler{}
	projectUserID := desktopUserID + ":D:/work/project"

	if h.InjectGuideReference(projectUserID, "project guide") {
		t.Fatalf("inactive project session should reject guide reference")
	}
	h.setSessionLoopCtx(desktopUserID, NewLoopContext("local", 1, nil))
	if !h.InjectGuideReference(desktopUserID, "local guide") {
		t.Fatalf("active local session should accept guide reference")
	}
	if h.InjectGuideReference(projectUserID, "project guide") {
		t.Fatalf("inactive project session should still reject guide reference")
	}

	h.setSessionLoopCtx(projectUserID, NewLoopContext("project", 1, nil))
	if !h.InjectGuideReference(projectUserID, "project guide") {
		t.Fatalf("active project session should accept guide reference")
	}

	localRaw, ok := h.pendingInjection.Load(desktopUserID)
	if !ok || !strings.Contains(localRaw.(string), "local guide") || strings.Contains(localRaw.(string), "project guide") {
		t.Fatalf("local pending injection not isolated: %#v", localRaw)
	}
	projectRaw, ok := h.pendingInjection.Load(projectUserID)
	if !ok || !strings.Contains(projectRaw.(string), "project guide") || strings.Contains(projectRaw.(string), "local guide") {
		t.Fatalf("project pending injection not isolated: %#v", projectRaw)
	}
}

func TestStripInjectionPrefixKeepsOnlyGuideLaunchUserText(t *testing.T) {
	injected := buildGuideLaunchInjection("use ssh next")
	if got := stripInjectionPrefix(injected); got != "use ssh next" {
		t.Fatalf("stripInjectionPrefix() = %q, want fired text only", got)
	}

	combined := buildGuideLaunchInjection("use ssh next") + "\n" + buildGuideLaunchInjection("then inspect logs")
	if got := stripInjectionPrefix(combined); got != "use ssh next\nthen inspect logs" {
		t.Fatalf("stripInjectionPrefix() = %q, want both fired texts only", got)
	}

	literalMarker := guideLaunchReferenceMarker + "\nkeep this user text"
	if got := stripInjectionPrefix(literalMarker); got != literalMarker {
		t.Fatalf("stripInjectionPrefix() = %q, want literal user marker preserved", got)
	}
}

func TestGuideLaunchPendingReferenceExtendsFinalizationBoundary(t *testing.T) {
	if got := extendEffectiveMaxForPendingGuideReference(3, 3, true); got != 4 {
		t.Fatalf("extendEffectiveMaxForPendingGuideReference() = %d, want 4", got)
	}
	if got := extendEffectiveMaxForPendingGuideReference(2, 3, true); got != 3 {
		t.Fatalf("early guide reference should not change effective max, got %d", got)
	}
	if got := extendEffectiveMaxForPendingGuideReference(3, 3, false); got != 3 {
		t.Fatalf("without guide reference effective max changed to %d", got)
	}
}

func TestPendingGuideReferenceDetectionRequiresInstructionWrapper(t *testing.T) {
	h := &IMMessageHandler{}
	h.accumulateInjection(desktopUserID, buildGuideLaunchInjection("steer next round"))
	if !h.hasPendingGuideReferenceInjection(desktopUserID) {
		t.Fatal("expected pending guide reference to be detected")
	}

	h.pendingInjection.Delete(desktopUserID)
	h.accumulateInjection(desktopUserID, guideLaunchReferenceMarker+"\nliteral user text")
	if h.hasPendingGuideReferenceInjection(desktopUserID) {
		t.Fatal("literal marker without instruction wrapper should not be treated as guide reference")
	}
}

type fakeProviderAdapter struct {
	cmd      CommandSpec
	buildErr error
	lastSpec LaunchSpec
}

func (f *fakeProviderAdapter) ProviderName() string         { return "claude" }
func (f *fakeProviderAdapter) ExecutionMode() ExecutionMode { return ExecModeSDK }
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

type fakeSDKExecutionHandle struct {
	*fakeExecutionHandle
	messages chan SDKMessage
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (n nopWriteCloser) Close() error { return nil }

func newFakeSDKExecutionHandle(pid int) *fakeSDKExecutionHandle {
	return &fakeSDKExecutionHandle{
		fakeExecutionHandle: newFakeExecutionHandle(pid),
		messages:            make(chan SDKMessage, 8),
	}
}

func (f *fakeSDKExecutionHandle) Messages() <-chan SDKMessage {
	return f.messages
}

func (f *fakeSDKExecutionHandle) ControlRequests() <-chan SDKControlRequest {
	return nil
}

type fakeAskUserResponderHandle struct {
	*fakeExecutionHandle
	lastPending *PendingToolUse
	lastText    string
	respondErr  error
}

func newFakeAskUserResponderHandle(pid int) *fakeAskUserResponderHandle {
	return &fakeAskUserResponderHandle{fakeExecutionHandle: newFakeExecutionHandle(pid)}
}

func (f *fakeAskUserResponderHandle) WriteAskUserQuestionAnswer(pending *PendingToolUse, text string) error {
	f.lastPending = pending
	f.lastText = text
	return f.respondErr
}

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

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !check() {
		t.Fatal("condition not met before timeout")
	}
}

func TestRemoteSessionManagerWriteInputAnswersPendingQuestion(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	handle := newFakeAskUserResponderHandle(99)
	session := &RemoteSession{
		ID:          "sess_pending",
		Tool:        "claude",
		Title:       "pending",
		ProjectPath: `D:\\workprj\\demo`,
		Exec:        handle,
		Status:      SessionWaitingInput,
		Summary: SessionSummary{
			Status:          string(SessionWaitingInput),
			WaitingForUser:  true,
			PendingQuestion: &PendingQuestionView{Question: "Choose one"},
		},
		PendingUserQuestion: &PendingToolUse{
			ToolUseID: "call_1",
			ToolName:  "AskUserQuestion",
			Question:  &PendingQuestionView{Question: "Choose one"},
		},
	}
	manager.sessions[session.ID] = session

	if err := manager.WriteInput(session.ID, "continue\n"); err != nil {
		t.Fatalf("WriteInput() error = %v", err)
	}
	if handle.lastPending == nil || handle.lastPending.ToolUseID != "call_1" {
		t.Fatal("expected pending AskUserQuestion to be routed to responder")
	}
	if got := handle.lastText; got != "continue" {
		t.Fatalf("responder text = %q, want %q", got, "continue")
	}
	if session.PendingUserQuestion != nil {
		t.Fatal("expected pending question to be cleared after successful submit")
	}
	if session.Summary.PendingQuestion != nil {
		t.Fatal("expected summary pending question to be cleared after successful submit")
	}
	if session.Status != SessionBusy {
		t.Fatalf("session status = %q, want %q", session.Status, SessionBusy)
	}
	if session.Summary.WaitingForUser {
		t.Fatal("expected waiting_for_user to be false after answering pending question")
	}
}

func TestRemoteSessionManagerWriteInputMarksSDKSessionBusyAfterWrite(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	stdin := &bytes.Buffer{}
	handle := &SDKExecutionHandle{
		stdin:    nopWriteCloser{stdin},
		outputCh: make(chan []byte, 1),
		exitCh:   make(chan PTYExit, 1),
		msgCh:    make(chan SDKMessage, 1),
	}
	session := &RemoteSession{
		ID:          "sess_sdk_busy",
		Tool:        "claude",
		Title:       "sdk busy",
		ProjectPath: `D:\\workprj\\demo`,
		Exec:        handle,
		Status:      SessionWaitingInput,
		Summary: SessionSummary{
			Status:         string(SessionWaitingInput),
			WaitingForUser: true,
			CurrentTask:    "Waiting for input",
		},
	}
	manager.sessions[session.ID] = session

	if err := manager.WriteInput(session.ID, "build a tiny python project\n"); err != nil {
		t.Fatalf("WriteInput() error = %v", err)
	}
	payload := stdin.String()
	if !strings.Contains(payload, "\"type\":\"user\"") {
		t.Fatalf("write payload = %q, want sdk user message json", payload)
	}
	if !strings.Contains(payload, "build a tiny python project") {
		t.Fatalf("write payload = %q, want prompt text", payload)
	}
	if session.Status != SessionBusy {
		t.Fatalf("session status = %q, want %q", session.Status, SessionBusy)
	}
	if session.Summary.Status != string(SessionBusy) {
		t.Fatalf("summary status = %q, want %q", session.Summary.Status, SessionBusy)
	}
	if session.Summary.WaitingForUser {
		t.Fatal("expected waiting_for_user to be false after successful SDK write")
	}
	if session.Summary.CurrentTask != "Processing your input" {
		t.Fatalf("current task = %q, want %q", session.Summary.CurrentTask, "Processing your input")
	}
}

func TestRemoteSessionManagerWriteInputKeepsPendingQuestionOnResponderError(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	handle := newFakeAskUserResponderHandle(100)
	handle.respondErr = fmt.Errorf("submit failed")
	pending := &PendingToolUse{ToolUseID: "call_2", ToolName: "AskUserQuestion", Question: &PendingQuestionView{Question: "Still waiting"}}
	session := &RemoteSession{
		ID:                  "sess_pending_error",
		Tool:                "claude",
		Title:               "pending error",
		ProjectPath:         `D:\\workprj\\demo`,
		Exec:                handle,
		Status:              SessionWaitingInput,
		Summary:             SessionSummary{Status: string(SessionWaitingInput), WaitingForUser: true, PendingQuestion: &PendingQuestionView{Question: "Still waiting"}},
		PendingUserQuestion: pending,
	}
	manager.sessions[session.ID] = session

	err := manager.WriteInput(session.ID, "retry\n")
	if err == nil {
		t.Fatal("expected responder error")
	}
	if session.PendingUserQuestion == nil || session.PendingUserQuestion.ToolUseID != pending.ToolUseID {
		t.Fatal("expected pending question to be preserved on responder error")
	}
	if session.Summary.PendingQuestion == nil {
		t.Fatal("expected summary pending question to remain on responder error")
	}
}

func TestRemoteSessionManagerSDKLoopReturnsToWaitingInputAfterToolResult(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	handle := &SDKExecutionHandle{
		outputCh:  make(chan []byte, 8),
		exitCh:    make(chan PTYExit, 1),
		msgCh:     make(chan SDKMessage, 8),
		ctrlReqCh: make(chan SDKControlRequest, 1),
	}
	session := &RemoteSession{
		ID:          "sess_sdk_loop",
		Tool:        "claude",
		Title:       "sdk loop",
		ProjectPath: `D:\\workprj\\demo`,
		Exec:        handle,
		Status:      SessionStarting,
		CreatedAt:   time.Now(),
		Summary: SessionSummary{
			Status: string(SessionStarting),
		},
		Preview: SessionPreview{SessionID: "sess_sdk_loop"},
	}
	manager.sessions[session.ID] = session

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	handle.msgCh <- SDKMessage{Type: "system", Subtype: "init", SessionID: "sdk-session-1"}
	waitForCondition(t, time.Second, func() bool {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.Status == SessionWaitingInput && session.Summary.WaitingForUser
	})

	handle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{{
				Type:  "tool_use",
				ID:    "toolu_1",
				Name:  "Write",
				Input: map[string]interface{}{"file_path": "D:/workprj/aicoder/TODO.md"},
			}},
		},
	}
	waitForCondition(t, time.Second, func() bool {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.Status == SessionBusy && !session.Summary.WaitingForUser
	})

	handle.msgCh <- SDKMessage{
		Type: "user",
		Message: &SDKAssistantPayload{
			Role: "user",
			Content: []SDKContentBlock{{
				Type:      "tool_result",
				ToolUseID: "toolu_1",
				Content:   "ok",
			}},
		},
	}
	handle.msgCh <- SDKMessage{Type: "result", Result: &SDKResultPayload{Duration: 1200, NumTurns: 1}}
	waitForCondition(t, time.Second, func() bool {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.Status == SessionWaitingInput && session.Summary.WaitingForUser && !session.Summary.Thinking
	})

	close(handle.msgCh)
	close(handle.outputCh)
	close(handle.ctrlReqCh)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runSDKOutputLoop did not exit")
	}
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

func TestRemoteSessionManagerInterruptCancelsBackgroundAISession(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	loopCtx := NewBackgroundLoopContext("bg-1", SlotKindScheduled, "ai task", 4, nil, nil)
	session := manager.CreateAIBackgroundSession("AI task", `D:\workprj\demo`, loopCtx)
	if session == nil {
		t.Fatal("expected background AI session")
	}
	if err := manager.Interrupt(session.ID); err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	select {
	case <-loopCtx.CancelC:
	default:
		t.Fatal("expected loop context to be cancelled")
	}
}

func TestRemoteSessionManagerCreateAIBackgroundSessionStoresMetadata(t *testing.T) {
	manager := NewRemoteSessionManager(&App{})
	loopCtx := NewLoopContext("bg-2", 4, nil)
	loopCtx.JobID = "job-1"
	loopCtx.RunID = "run-1"
	session := manager.CreateAIBackgroundSession("Summarize repo", `D:\workprj\demo`, loopCtx)
	if session == nil {
		t.Fatal("expected session")
	}
	if session.LaunchSource != RemoteLaunchSourceAI {
		t.Fatalf("launch source = %q, want %q", session.LaunchSource, RemoteLaunchSourceAI)
	}
	if session.AgentLoop != loopCtx {
		t.Fatal("expected agent loop to be stored on session")
	}
	if session.Summary.Status != string(SessionBusy) {
		t.Fatalf("summary status = %q, want %q", session.Summary.Status, SessionBusy)
	}
	if session.JobID != "job-1" || session.RunID != "run-1" {
		t.Fatalf("trace ids = (%q, %q), want (job-1, run-1)", session.JobID, session.RunID)
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

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
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

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
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
	if !containsArg(cmd.Args, codexYoloModeFlag) {
		t.Fatalf("Args = %v, want %q flag in yolo mode", cmd.Args, codexYoloModeFlag)
	}
	if containsArg(cmd.Args, "--full-auto") {
		t.Fatalf("Args = %v, should not contain deprecated '--full-auto' flag", cmd.Args)
	}
}

func TestCodexAdapterBuildCommandResumeSession(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
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
		Tool:            "codex",
		ProjectPath:     projectDir,
		ModelID:         "gpt-5.2-codex",
		ResumeSessionID: "thread_123",
		Env:             map[string]string{"OPENAI_API_KEY": "test-key"},
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	wantArgs := []string{"exec", "resume", "--json", "--model", "gpt-5.2-codex", "thread_123", "-"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("Args len = %d, want %d: %v", len(cmd.Args), len(wantArgs), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("Args[%d] = %q, want %q (all args: %v)", i, cmd.Args[i], want, cmd.Args)
		}
	}
	if containsArg(cmd.Args, "-c") {
		t.Fatalf("Args = %v, should not contain model_provider override when ModelName is empty", cmd.Args)
	}
}

func TestShouldCreatePendingResumeSlotRequiresIncompleteCompletion(t *testing.T) {
	session := &RemoteSession{
		ResumeContext:   &SessionResumeContext{OriginalTask: "resume me"},
		CompletionLevel: CompletionCompleted,
	}
	if shouldCreatePendingResumeSlot(session) {
		t.Fatal("shouldCreatePendingResumeSlot() = true, want false for completed sessions")
	}

	session.CompletionLevel = CompletionIncomplete
	if !shouldCreatePendingResumeSlot(session) {
		t.Fatal("shouldCreatePendingResumeSlot() = false, want true for incomplete sessions")
	}
}

func TestRunExitLoopPersistsPendingResumeSlotTool(t *testing.T) {
	tempHome := t.TempDir()
	app := NewApp()
	app.testHomeDir = tempHome
	manager := NewRemoteSessionManager(app)
	execHandle := newFakeExecutionHandle(901)
	session := &RemoteSession{
		ID:              "sess-opencode",
		Tool:            "opencode",
		ProjectPath:     "D:/work/project",
		Exec:            execHandle,
		Status:          SessionBusy,
		CompletionLevel: CompletionIncomplete,
		ResumeContext: &SessionResumeContext{
			ProjectPath:     "D:/work/project",
			Tool:            "opencode",
			ResumeSessionID: "resume-xyz",
			OriginalTask:    "continue opencode task",
			LastProgress:    "halfway done",
		},
		Summary: SessionSummary{
			CurrentTask:     "continue opencode task",
			ProgressSummary: "halfway done",
		},
	}

	done := make(chan struct{})
	go func() {
		manager.runExitLoop(session)
		close(done)
	}()

	exitCode := 2
	execHandle.exitCh <- PTYExit{Code: &exitCode}
	close(execHandle.exitCh)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runExitLoop did not finish")
	}

	mem := app.ensureConversationMemory()
	slot := mem.GetUnfinishedSlot("desktop-user")
	if slot == nil {
		t.Fatal("expected pending resume slot")
	}
	if slot.Tool != "opencode" {
		t.Fatalf("slot.Tool = %q, want %q", slot.Tool, "opencode")
	}
	if slot.Source != "session_exit" {
		t.Fatalf("slot.Source = %q, want %q", slot.Source, "session_exit")
	}
}

func TestBuildRecoverableSessionPayload(t *testing.T) {
	session := &RemoteSession{
		ID:          "sess-1",
		Tool:        "claude",
		Title:       "Resume Task",
		ProjectPath: "D:/work/project",
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask:     "继续 Daily Paper",
			ProgressSummary: "还差最后一轮整理",
		},
		ResumeContext: &SessionResumeContext{
			ProjectPath:     "D:/work/project",
			Tool:            "claude",
			ExitReason:      "token_limit",
			ResumeSessionID: "resume-123",
			ResumeCount:     2,
			LastProgress:    "还差最后一轮整理",
		},
	}

	payload := buildRecoverableSessionPayload(session)
	if payload == nil {
		t.Fatal("expected payload")
	}
	if payload.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", payload.SessionID)
	}
	if payload.ResumeSessionID != "resume-123" {
		t.Fatalf("ResumeSessionID = %q, want resume-123", payload.ResumeSessionID)
	}
	if payload.Tool != "claude" {
		t.Fatalf("Tool = %q, want claude", payload.Tool)
	}
	if len(payload.Actions) == 0 || payload.Actions[0].Command != "__resume_session__ sess-1" {
		t.Fatalf("Actions = %#v, want resume-session action", payload.Actions)
	}
}

func TestBuildResumeContextCapturesCodexThreadID(t *testing.T) {
	handle := &CodexSDKExecutionHandle{threadID: "thread_456"}
	session := &RemoteSession{
		ProjectPath: "D:/workprj/demo",
		Tool:        "codex",
		Exec:        handle,
		Summary: SessionSummary{
			ProgressSummary: "halfway",
			ImportantFiles:  []string{"a.go", "b.go"},
		},
		RawOutputLines: []string{"line1", "line2"},
	}

	rc := buildResumeContext(session, "token_limit")
	if rc == nil {
		t.Fatal("expected resume context")
	}
	if rc.ResumeSessionID != "thread_456" {
		t.Fatalf("ResumeSessionID = %q, want %q", rc.ResumeSessionID, "thread_456")
	}
	if rc.ClaudeSessionID != "" {
		t.Fatalf("ClaudeSessionID = %q, want empty", rc.ClaudeSessionID)
	}
	if rc.Tool != "codex" {
		t.Fatalf("Tool = %q, want %q", rc.Tool, "codex")
	}
	if rc.ProjectPath != "D:/workprj/demo" {
		t.Fatalf("ProjectPath = %q, want %q", rc.ProjectPath, "D:/workprj/demo")
	}
}

func TestCodexAdapterBuildCommandOriginalMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
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
		IsBuiltin:   true,
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

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
