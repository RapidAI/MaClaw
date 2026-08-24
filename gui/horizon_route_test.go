package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestHandleHorizonIMRouteIgnoresPlainChat(t *testing.T) {
	h := &IMMessageHandler{}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Text: "hello"}, "hello")
	if handled || resp != nil {
		t.Fatalf("plain chat must not enter horizon handled=%v resp=%v", handled, resp)
	}
}

func TestHandleHorizonIMRouteSkipsCancelledSession(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{
		ownerID:   "u1",
		lang:      "en",
		cancelled: true,
		notify:    make(chan struct{}, 1),
		state:     &longhorizon.TaskState{TaskID: "t-cancelled"},
	}
	h.storeHorizonSession(sess)
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "also format"}, "also format")
	if handled || resp != nil {
		t.Fatalf("cancelled session must not intercept chat handled=%v resp=%+v", handled, resp)
	}
	if h.loadHorizonSession("u1") != nil {
		t.Fatal("cancelled leftover must be dropped from the live map")
	}
	if len(sess.inbox) != 0 {
		t.Fatalf("cancelled session must not record follow-up inbox=%v", sess.inbox)
	}
}

func TestHandleHorizonIMRouteAdmitAndFollowUp(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	var started atomic.Int32
	h.horizonStartSupervisor = func(sess *horizonSession) {
		started.Add(1)
		if sess == nil || sess.state == nil || sess.state.Policy.ProjectRoot == "" {
			t.Errorf("supervisor started without project root")
		}
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests")
	if !handled || resp == nil || !strings.Contains(resp.Text, "started") {
		t.Fatalf("admit resp=%+v handled=%v", resp, handled)
	}
	if started.Load() != 1 {
		t.Fatalf("supervisor starts = %d", started.Load())
	}
	if !h.horizonActive("u1") {
		t.Fatal("session should be active")
	}

	resp, handled = h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "also format"}, "also format")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "recorded") {
		t.Fatalf("follow-up resp=%+v", resp)
	}
	if started.Load() != 1 {
		t.Fatal("follow-up must not start a second supervisor")
	}
	sess := h.loadHorizonSession("u1")
	if sess == nil || len(sess.drainInbox()) == 0 {
		t.Fatal("follow-up should land in inbox")
	}

	resp, handled = h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon other"}, "@horizon other")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "already running") {
		t.Fatalf("second @horizon while active should refuse, got %+v", resp)
	}
}

func TestHandleHorizonIMRouteRequiresGoalAndProject(t *testing.T) {
	h := &IMMessageHandler{}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon"}, "@horizon")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "what to do") {
		t.Fatalf("empty body resp=%+v", resp)
	}
	h.horizonProjectPathFn = func(string) string { return "" }
	resp, handled = h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "workspace") {
		t.Fatalf("missing project resp=%+v", resp)
	}
}

func TestCancelSessionForUserCancelsHorizon(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	h.horizonStartSupervisor = func(*horizonSession) {}
	_, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests")
	if !handled {
		t.Fatal("expected admit")
	}
	got, err := h.CancelSessionForUser("u1")
	if err != nil || got != "horizon" {
		t.Fatalf("cancel = %q err=%v", got, err)
	}
	if h.horizonActive("u1") {
		t.Fatal("horizon session still active")
	}
}

func TestFinishHorizonCancelledClearsGUIClaimOnly(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	cuOwner := "im:desktop:u1:actor"
	setComputerUseOwner(cuOwner)
	seedComputerUseObserve(t, "notepad", "Notepad")
	setHorizonComputerUseClaimOnly(cuOwner, true)
	markComputerUseSessionActive()
	sess := &horizonSession{ownerID: "u1", computerUseOwner: cuOwner, state: &longhorizon.TaskState{TaskID: "t1"}}
	(&IMMessageHandler{}).finishHorizonCancelled(sess, "en")
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("cancelled Horizon must drop leftover GUI claim-only")
	}
	if !computerUseSessionActive() {
		t.Fatal("CLI/cancel path must not drop CU sticky; GUI probe-release owns that")
	}
	if cuSessionForOwner(cuOwner) == nil || cuSessionForOwner(cuOwner).LastValidObserve() == nil {
		t.Fatal("cancel must not evict the GUI observe snapshot")
	}
}

func TestHorizonPostureExecuteToolRejectsComputerUse(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	sa.SetHorizonPosture(longhorizon.CLIExecutorTools)
	cb := &codingSubAgentCallbacks{
		subagent:    sa,
		task:        &TaskItem{Title: "add tests"},
		prevOutputs: []string{"round 1 audit: missing coverage on parse"},
	}
	got := cb.executeToolWithOutcome("computer_observe", `{}`)
	if got.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("outcome=%s text=%s", got.Outcome, got.Text)
	}
	prompt := cb.BuildSystemPrompt("add tests", true)
	if !strings.Contains(prompt, "round 1 audit: missing coverage on parse") {
		t.Fatalf("related audits missing from horizon prompt: %s", prompt)
	}
	if longhorizon.ContainsForbiddenPrompt(prompt) {
		t.Fatalf("horizon prompt polluted: %s", prompt)
	}
	for _, tool := range cb.BuildTools("add tests") {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !longhorizon.ToolAllowed(longhorizon.CLIExecutorTools, name) {
			t.Fatalf("tool %q escaped horizon surface", name)
		}
	}
	if cb.matchedSkillsSelected || len(cb.matchedSkills) != 0 || cb.matchedMCPToolsSelected || len(cb.matchedMCPTools) != 0 {
		t.Fatal("horizon CLI must not match skills or MCP tools")
	}
}

func TestHorizonBuildToolsAreSurfaceFirst(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"computer_observe", "computer_click", "computer_done", "browser_session_start", "browser_observe", "bash"} {
		if err := reg.Register(RegisteredTool{Name: name, Description: name, InputSchema: map[string]interface{}{}}); err != nil {
			t.Fatal(err)
		}
	}
	h := &IMMessageHandler{registry: reg}

	guiSA := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	guiSA.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{}))
	guiCB := &codingSubAgentCallbacks{subagent: guiSA, task: &TaskItem{Title: "open notepad"}}
	guiNames := map[string]int{}
	for _, tool := range guiCB.BuildTools("open notepad") {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		guiNames[name]++
		if name == "bash" || name == "write_file" || strings.HasPrefix(name, "browser_") {
			t.Fatalf("GUI surface leaked %q", name)
		}
	}
	if guiNames["computer_observe"] != 1 || guiNames["computer_done"] != 1 {
		t.Fatalf("GUI tools = %#v", guiNames)
	}
	for name, n := range guiNames {
		if n != 1 {
			t.Fatalf("duplicate GUI tool %s count=%d", name, n)
		}
	}

	cliSA := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	cliSA.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleCLIExecutor, longhorizon.ManagerPlan{Goal: "add tests"}, nil, "", longhorizon.PolicySnapshot{}))
	cliCB := &codingSubAgentCallbacks{subagent: cliSA, task: &TaskItem{Title: "add tests"}}
	cliNames := map[string]int{}
	for _, tool := range cliCB.BuildTools("add tests") {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		cliNames[name]++
		if strings.HasPrefix(name, "computer_") || strings.HasPrefix(name, "browser_") {
			t.Fatalf("CLI surface leaked %q", name)
		}
	}
	if cliNames["bash"] != 1 || cliNames["todo_write"] != 1 {
		t.Fatalf("CLI tools = %#v", cliNames)
	}
}

func TestHandleHorizonIMRouteSlashCommandsPassThrough(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	h.horizonStartSupervisor = func(*horizonSession) {}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests"); !handled {
		t.Fatal("expected admit")
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "/help"}, "/help")
	if handled || resp != nil {
		t.Fatalf("/help during horizon must pass through, handled=%v resp=%+v", handled, resp)
	}
	if !h.horizonActive("u1") {
		t.Fatal("slash pass-through must not cancel horizon")
	}
	resp, handled = h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "/clear"}, "/clear")
	if handled || resp != nil {
		t.Fatalf("/clear must pass through after cancelling horizon, handled=%v resp=%+v", handled, resp)
	}
	if h.horizonActive("u1") {
		t.Fatal("/clear should cancel horizon")
	}
}

func TestJoinHorizonInboxKeepsAllItems(t *testing.T) {
	if got := joinHorizonInbox([]string{"a", "", "b"}); got != "a\nb" {
		t.Fatalf("join = %q", got)
	}
}

func TestWaitHorizonInboxDrainsAll(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{notify: make(chan struct{}, 1)}
	sess.enqueue("one")
	sess.enqueue("two")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := h.waitHorizonInbox(ctx, sess)
	if !ok || got != "one\ntwo" {
		t.Fatalf("inbox = %q ok=%v", got, ok)
	}
	if leftover := sess.drainInbox(); len(leftover) != 0 {
		t.Fatalf("leftover inbox=%v", leftover)
	}
}

func TestLaunchHorizonSupervisorIsIdempotent(t *testing.T) {
	h := &IMMessageHandler{}
	var started atomic.Int32
	h.horizonStartSupervisor = func(*horizonSession) { started.Add(1) }
	sess := &horizonSession{ownerID: "u1", notify: make(chan struct{}, 1)}
	h.launchHorizonSupervisor(sess)
	h.launchHorizonSupervisor(sess)
	if started.Load() != 1 {
		t.Fatalf("started = %d", started.Load())
	}
}

func TestHandleHorizonIMRouteIgnoresSlashForResumeHijack(t *testing.T) {
	h := &IMMessageHandler{}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Text: "/help"}, "/help")
	if handled || resp != nil {
		t.Fatalf("slash commands must not enter resume offer handled=%v resp=%+v", handled, resp)
	}
}

func TestEnqueuePersistsCarryover(t *testing.T) {
	root := t.TempDir()
	sess := &horizonSession{
		storeRoot: root,
		notify:    make(chan struct{}, 1),
		state: &longhorizon.TaskState{
			TaskID: "hz-1",
			Policy: longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-1"},
		},
	}
	sess.enqueue("keep me")
	loaded, err := longhorizon.LoadTaskState(root, "hz-1")
	if err != nil || loaded == nil || len(loaded.Carryover) != 1 || loaded.Carryover[0] != "keep me" {
		t.Fatalf("carryover persist loaded=%+v err=%v", loaded, err)
	}
}

func TestMaybeOfferHorizonResumePersistsTrigger(t *testing.T) {
	tmp := t.TempDir()
	h := &IMMessageHandler{app: &App{testHomeDir: tmp}}
	root := h.horizonStoreRoot()
	state := &longhorizon.TaskState{
		TaskID:     "hz-resume",
		Status:     longhorizon.StatusAsking,
		RoundIndex: 1,
		MaxRounds:  longhorizon.DefaultMaxRounds,
		Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-resume"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "also cover edge cases"}, "also cover edge cases")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "resume") {
		t.Fatalf("resume offer resp=%+v handled=%v", resp, handled)
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-resume")
	if err != nil || loaded == nil || len(loaded.Carryover) != 1 || loaded.Carryover[0] != "also cover edge cases" {
		t.Fatalf("resume trigger not persisted loaded=%+v err=%v", loaded, err)
	}
}

func TestDropHorizonSessionIfKeepsReplacement(t *testing.T) {
	h := &IMMessageHandler{}
	old := &horizonSession{ownerID: "u1"}
	h.storeHorizonSession(old)
	next := &horizonSession{ownerID: "u1"}
	h.storeHorizonSession(next)
	h.dropHorizonSessionIf(old)
	if h.loadHorizonSession("u1") != next {
		t.Fatal("old supervisor must not drop the replacement session")
	}
}

func TestHorizonAdmitRefusedWhileSupervisorRunning(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	h.horizonStartSupervisor = func(*horizonSession) {}
	sess := &horizonSession{ownerID: "u1"}
	h.markHorizonRunning(sess)
	defer h.clearHorizonRunning(sess)
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "stopping") {
		t.Fatalf("admit while stopping resp=%+v handled=%v", resp, handled)
	}
	resp, handled = h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "hello"}, "hello")
	if handled || resp != nil {
		t.Fatalf("plain chat while stopping must pass through handled=%v resp=%+v", handled, resp)
	}
}

func TestResumeAskHorizonStartsNewTask(t *testing.T) {
	tmp := t.TempDir()
	h := &IMMessageHandler{app: &App{testHomeDir: tmp}}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	var started int32
	h.horizonStartSupervisor = func(*horizonSession) { started++ }
	root := h.horizonStoreRoot()
	state := &longhorizon.TaskState{
		TaskID:     "hz-old",
		Status:     longhorizon.StatusAsking,
		RoundIndex: 1,
		MaxRounds:  longhorizon.DefaultMaxRounds,
		Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-old"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "keep going"}, "keep going"); !handled {
		t.Fatal("expected resume offer")
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon do something else"}, "@horizon do something else")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "started") {
		t.Fatalf("new @horizon during resume ask resp=%+v handled=%v", resp, handled)
	}
	if started != 1 {
		t.Fatalf("new supervisor starts = %d", started)
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-old")
	if err != nil || loaded == nil || loaded.Status != longhorizon.StatusCancelled {
		t.Fatalf("old task should be cancelled loaded=%+v err=%v", loaded, err)
	}
}

func TestResumeAskEmptyHorizonKeepsOldTask(t *testing.T) {
	tmp := t.TempDir()
	h := &IMMessageHandler{app: &App{testHomeDir: tmp}}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	var started int32
	h.horizonStartSupervisor = func(*horizonSession) { started++ }
	root := h.horizonStoreRoot()
	state := &longhorizon.TaskState{
		TaskID:     "hz-keep",
		Status:     longhorizon.StatusAsking,
		RoundIndex: 1,
		MaxRounds:  longhorizon.DefaultMaxRounds,
		Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-keep"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "keep going"}, "keep going"); !handled {
		t.Fatal("expected resume offer")
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon"}, "@horizon")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "what to do") {
		t.Fatalf("empty @horizon during resume ask resp=%+v handled=%v", resp, handled)
	}
	if started != 0 {
		t.Fatalf("empty @horizon must not start a supervisor, started=%d", started)
	}
	sess := h.loadHorizonSession("u1")
	if sess == nil {
		t.Fatal("resume-ask session should remain")
	}
	sess.mu.Lock()
	resumeAsk := sess.resumeAsk
	sess.mu.Unlock()
	if !resumeAsk {
		t.Fatal("empty @horizon must not clear resume-ask")
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-keep")
	if err != nil || loaded == nil || loaded.Status == longhorizon.StatusCancelled {
		t.Fatalf("old task must remain resumable loaded=%+v err=%v", loaded, err)
	}
}

func TestResumeAskAbandonNotGenericCancel(t *testing.T) {
	tmp := t.TempDir()
	h := &IMMessageHandler{app: &App{testHomeDir: tmp}}
	root := h.horizonStoreRoot()
	state := &longhorizon.TaskState{
		TaskID:     "hz-abandon",
		Status:     longhorizon.StatusAsking,
		RoundIndex: 1,
		MaxRounds:  longhorizon.DefaultMaxRounds,
		Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-abandon"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "keep going"}, "keep going"); !handled {
		t.Fatal("expected resume offer")
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "\u653e\u5f03"}, "\u653e\u5f03")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "abandoned") {
		t.Fatalf("resume-ask abandon resp=%+v handled=%v", resp, handled)
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-abandon")
	if err != nil || loaded == nil || loaded.Status != longhorizon.StatusCancelled {
		t.Fatalf("abandoned task loaded=%+v err=%v", loaded, err)
	}
}

func TestHorizonPersistAfterCancelStaysCancelled(t *testing.T) {
	root := t.TempDir()
	sess := &horizonSession{
		storeRoot: root,
		state: &longhorizon.TaskState{
			TaskID: "hz-cancel-persist",
			Status: longhorizon.StatusManaging,
			Policy: longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-cancel-persist"},
		},
	}
	sess.mu.Lock()
	sess.cancelled = true
	sess.state.Status = longhorizon.StatusCancelled
	sess.persistLocked()
	sess.state.Status = longhorizon.StatusManaging
	sess.persistLocked()
	sess.mu.Unlock()
	loaded, err := longhorizon.LoadTaskState(root, "hz-cancel-persist")
	if err != nil || loaded == nil || loaded.Status != longhorizon.StatusCancelled {
		t.Fatalf("cancelled persist overwritten loaded=%+v err=%v", loaded, err)
	}
}

func TestHorizonStopRoundLimitNotResumable(t *testing.T) {
	root := t.TempDir()
	h := &IMMessageHandler{}
	sess := &horizonSession{
		storeRoot: root,
		lang:      "en",
		state: &longhorizon.TaskState{
			TaskID:     "hz-limit",
			Status:     longhorizon.StatusAsking,
			RoundIndex: 0,
			MaxRounds:  longhorizon.DefaultMaxRounds,
			Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-limit"},
		},
	}
	h.horizonStopRoundLimit(sess, "en")
	if sess.state == nil || sess.state.RoundIndex != longhorizon.DefaultMaxRounds || sess.state.Status != longhorizon.StatusBlocked {
		t.Fatalf("limit state=%+v", sess.state)
	}
	if longhorizon.Resumable(sess.state) {
		t.Fatal("ask-limit blocked task must not be resumable")
	}
}

func TestHorizonConcurrentAdmitClaimsOnce(t *testing.T) {
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	var started atomic.Int32
	h.horizonStartSupervisor = func(*horizonSession) { started.Add(1) }
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon add tests"}, "@horizon add tests")
		}()
	}
	wg.Wait()
	if started.Load() != 1 {
		t.Fatalf("concurrent admit started %d supervisors", started.Load())
	}
	if got := h.loadHorizonSession("u1"); got == nil {
		t.Fatal("expected one active session")
	}
}

func TestResumeAskFollowUpIsRecorded(t *testing.T) {
	tmp := t.TempDir()
	h := &IMMessageHandler{app: &App{testHomeDir: tmp}}
	root := h.horizonStoreRoot()
	state := &longhorizon.TaskState{
		TaskID:     "hz-resume-follow",
		Status:     longhorizon.StatusAsking,
		RoundIndex: 1,
		MaxRounds:  longhorizon.DefaultMaxRounds,
		Policy:     longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-resume-follow"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "keep going"}, "keep going"); !handled {
		t.Fatal("expected resume offer")
	}
	resp, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "also cover retries"}, "also cover retries")
	if !handled || resp == nil || !strings.Contains(strings.ToLower(resp.Text), "resume") {
		t.Fatalf("follow-up during resume ask resp=%+v handled=%v", resp, handled)
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-resume-follow")
	if err != nil || loaded == nil || len(loaded.Carryover) < 2 {
		t.Fatalf("resume follow-up not persisted loaded=%+v err=%v", loaded, err)
	}
}

func TestHorizonProjectPathSkipsGlobalWorkspace(t *testing.T) {
	h := &IMMessageHandler{}
	if got := h.horizonProjectPath("u1", IMUserMessage{}); got != "" {
		t.Fatalf("unbound owner must not use global workspace, got %q", got)
	}
}

func TestHorizonProjectPathUsesSessionOwnerPath(t *testing.T) {
	dir := t.TempDir()
	owner := projectSessionOwnerID(dir)
	h := &IMMessageHandler{}
	got := h.horizonProjectPath(owner, IMUserMessage{})
	if got != normalizeProjectSessionPath(dir) {
		t.Fatalf("session owner path = %q, want %q", got, normalizeProjectSessionPath(dir))
	}
}

func TestHorizonProjectPathUsesAssistantBinding(t *testing.T) {
	dir := t.TempDir()
	h := &IMMessageHandler{}
	got := h.horizonProjectPath("u1", IMUserMessage{
		AssistantBinding: &agent.AssistantBinding{WorkingDirectory: dir},
	})
	if got != normalizeProjectSessionPath(dir) {
		t.Fatalf("binding path = %q, want %q", got, normalizeProjectSessionPath(dir))
	}
}

func TestHorizonProjectPathPrefersBoundWorkingDir(t *testing.T) {
	projectDir := t.TempDir()
	overrideDir := t.TempDir()
	owner := projectSessionOwnerID(projectDir)
	app := &App{}
	app.assistantSessionWorkingDirs.Store(owner, normalizeProjectSessionPath(overrideDir))
	h := &IMMessageHandler{app: app}
	got := h.horizonProjectPath(owner, IMUserMessage{})
	if got != normalizeProjectSessionPath(overrideDir) {
		t.Fatalf("bound override = %q, want %q not session path %q", got, normalizeProjectSessionPath(overrideDir), normalizeProjectSessionPath(projectDir))
	}
}

func TestWaitHorizonAskIgnoresStaleInbox(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{notify: make(chan struct{}, 1)}
	sess.enqueue("stale")
	sess.discardPendingInbox()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	gotCh := make(chan string, 1)
	go func() {
		got, ok := h.waitHorizonInbox(ctx, sess)
		if !ok {
			gotCh <- ""
			return
		}
		gotCh <- got
	}()
	sess.enqueue("fresh")
	select {
	case got := <-gotCh:
		if got != "fresh" {
			t.Fatalf("ask wait = %q, want fresh", got)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for fresh inbox item")
	}
}

func TestHorizonEpisodeAskQuestion(t *testing.T) {
	if q := horizonEpisodeAskQuestion("status=failed\n{\"status\":\"ask\"}"); q != "" {
		t.Fatalf("json mention must not pause outer loop, got %q", q)
	}
	if q := horizonEpisodeAskQuestion("status=passed\nclicked status=ask button"); q != "" {
		t.Fatalf("substring status=ask must not pause outer loop, got %q", q)
	}
	if q := horizonEpisodeAskQuestion("status=ask\nwhich window?"); q != "which window?" {
		t.Fatalf("first-line status=ask = %q", q)
	}
	marker := `__ASK_USER__{"question":"which window?"}`
	if q := horizonEpisodeAskQuestion(marker); q != "which window?" {
		t.Fatalf("ask_user = %q", q)
	}
}

func TestFormatHorizonEpisodeResultAsk(t *testing.T) {
	got := formatHorizonEpisodeResult(&CodingSubAgentResult{
		Status:      TaskExecSkipped,
		Summary:     "which window?",
		AskQuestion: "which window?",
	})
	if got != "status=ask\nwhich window?" {
		t.Fatalf("episode result = %q", got)
	}
	if q := horizonEpisodeAskQuestion(got); q != "which window?" {
		t.Fatalf("supervisor ask = %q", q)
	}
}

func TestFinishCodingSubAgentAskSkipsQuality(t *testing.T) {
	sa := &CodingSubAgent{horizonPosture: true}
	got := finishCodingSubAgentAsk(sa, agent.LoopResult{
		AskUser:    &agent.AskUserRequest{Question: "solve captcha"},
		Iterations: 2,
		ToolCalls:  1,
	})
	if got == nil || got.AskQuestion != "solve captcha" || got.Status != TaskExecSkipped {
		t.Fatalf("ask result = %+v", got)
	}
	if got.QualityStatus != codingSubAgentQualityNotNeeded || !got.HorizonOwned {
		t.Fatalf("quality/horizon = %+v", got)
	}
	if horizonEpisodeAskQuestion(formatHorizonEpisodeResult(got)) != "solve captcha" {
		t.Fatalf("outer loop missed ask: %q", formatHorizonEpisodeResult(got))
	}
}

func TestHorizonAuditEvidenceKeepsProbe(t *testing.T) {
	claim := strings.Repeat("x", 20000)
	got := horizonAuditEvidence(claim, "ocr=notepad visible")
	if !strings.HasPrefix(got, "Probe:\nocr=notepad visible\nClaim:\n") {
		t.Fatalf("probe must lead evidence: %q", got[:80])
	}
	if strings.Contains(got, strings.Repeat("x", 8001)) {
		t.Fatal("claim was not clipped")
	}
}

func TestHorizonGUIProbeIgnoresStaleObserve(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	setComputerUseOwner("other-owner")
	setComputerUseOwner("u1")
	seedComputerUseObserve(t, "notepad", "Notepad")
	cuSession().InvalidateRefs()
	setComputerUseOwner("other-owner")
	probe := horizonGUIProbe("u1")
	if probe.OK || strings.TrimSpace(probe.Digest) != "" {
		t.Fatalf("stale observe must not be used: %+v", probe)
	}
	if computerUseOwnerKey() != "other-owner" {
		t.Fatalf("gui probe mutated active owner: %s", computerUseOwnerKey())
	}
}

func TestStripHorizonProbeImagesKeepsText(t *testing.T) {
	got := stripHorizonProbeImages("ocr=notepad data:image/png;base64," + strings.Repeat("A", 40) + " title=Notes")
	if !strings.Contains(got, "ocr=notepad") || !strings.Contains(got, "title=Notes") {
		t.Fatalf("probe text dropped: %q", got)
	}
	if strings.Contains(got, strings.Repeat("A", 40)) || strings.Contains(got, "data:image/png") {
		t.Fatalf("image payload leaked: %q", got)
	}
}

func TestHorizonManagerSearchIncludesCandidate(t *testing.T) {
	opts := horizonManagerSearchOptions("open notepad", "/tmp/proj")
	found := false
	for _, st := range opts.Status {
		if st == knowledge.CodingStatusCandidate {
			found = true
			break
		}
	}
	if !found || opts.Limit != longhorizon.MaxExperiencePerTask {
		t.Fatalf("search opts = %+v", opts)
	}
}

func TestHorizonPostureGUIInjectsHostTools(t *testing.T) {
	reg := NewToolRegistry()
	if err := reg.Register(RegisteredTool{
		Name:        "computer_observe",
		Description: "observe desktop",
		InputSchema: map[string]interface{}{},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: reg}
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad", Acceptance: "visible"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	prompt := cb.BuildSystemPrompt("open notepad", true)
	if prompt == longhorizon.CLIExecutorSystemPrompt() || !strings.Contains(prompt, "computer_done") {
		t.Fatalf("gui prompt missing: %s", prompt)
	}
	foundObserve := false
	for _, tool := range cb.BuildTools("open notepad") {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == "bash" {
			t.Fatal("bash escaped gui surface")
		}
		if name == "computer_observe" {
			foundObserve = true
		}
	}
	if !foundObserve {
		t.Fatal("computer_observe missing from gui horizon tools")
	}
	got := cb.executeToolWithOutcome("bash", `{}`)
	if got.Outcome != codingToolOutcomeBlocked {
		t.Fatalf("bash outcome=%s text=%s", got.Outcome, got.Text)
	}
	host := cb.executeToolWithOutcome("computer_observe", `{}`)
	if strings.Contains(host.Text, "unknown tool:") && strings.Contains(host.Text, "coding SubAgent supports") {
		t.Fatalf("gui host tool treated as unknown coding tool: %s", host.Text)
	}
}

func TestFormatHorizonManagerEvidenceDropsImages(t *testing.T) {
	got := formatHorizonManagerEvidence([]knowledge.CodingExperience{
		{Title: "bad", Content: "data:image/png;base64,AAAA"},
		{Title: "keep", Content: "prefer a real audit before done"},
	})
	if strings.Contains(got, "base64") || !strings.Contains(got, "prefer a real audit") {
		t.Fatalf("evidence = %q", got)
	}
}

func TestHorizonRolesForNext(t *testing.T) {
	execRole, auditRole, label := horizonRolesForNext(longhorizon.NextBrowser)
	if execRole != longhorizon.RoleBrowserExecutor || auditRole != longhorizon.RoleBrowserAuditor || label != "Browser" {
		t.Fatalf("browser roles = %s %s %s", execRole, auditRole, label)
	}
	execRole, auditRole, label = horizonRolesForNext(longhorizon.NextGUI)
	if execRole != longhorizon.RoleGUIExecutor || auditRole != longhorizon.RoleGUIAuditor || label != "GUI" {
		t.Fatalf("gui roles = %s %s %s", execRole, auditRole, label)
	}
	if r, _, _ := horizonRolesForNext(longhorizon.NextAsk); r != "" {
		t.Fatal("ask is not an executor next")
	}
}

func TestHorizonEpisodeMaxIterationsGUIDefault(t *testing.T) {
	gui := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{})
	if got := horizonEpisodeMaxIterations(gui); got != longhorizon.GUIMaxIterations {
		t.Fatalf("assembled gui budget = %d", got)
	}
	if got := horizonEpisodeMaxIterations(longhorizon.EpisodeContext{Role: longhorizon.RoleGUIExecutor}); got != longhorizon.GUIMaxIterations {
		t.Fatalf("missing gui budget fell back to %d, want %d", got, longhorizon.GUIMaxIterations)
	}
	if got := horizonEpisodeMaxIterations(longhorizon.EpisodeContext{Role: longhorizon.RoleCLIExecutor}); got != longhorizon.CLIMaxIterations {
		t.Fatalf("missing cli budget = %d", got)
	}
}

func TestAccelerateHorizonGUIAuditPass(t *testing.T) {
	if !accelerateHorizonGUIAuditPass("window visible\nfile saved", "window visible\nfile saved") {
		t.Fatal("short bullets present in observe must accelerate pass")
	}
	if accelerateHorizonGUIAuditPass("window visible\nfile saved", "window visible") {
		t.Fatal("partial short match must not accelerate (and must not fail alone)")
	}
	paragraph := "The notepad window should be in the foreground with hello.txt saved to disk."
	if accelerateHorizonGUIAuditPass(paragraph, paragraph) {
		t.Fatal("manager paragraph must not be a mechanical pass; auditor LLM decides")
	}
	if accelerateHorizonGUIAuditPass(paragraph, "Notepad - Untitled") {
		t.Fatal("paragraph miss must not mechanical-fail; accelerate stays false so LLM runs")
	}
	if accelerateHorizonGUIAuditPass("window visible", "") {
		t.Fatal("empty probe must not accelerate")
	}
	if !accelerateHorizonGUIAuditPass("- window visible\n- file saved", "window visible\nfile saved") {
		t.Fatal("markdown bullets must strip before matching observe")
	}
}

func TestHorizonEpisodeUncertain(t *testing.T) {
	if !horizonEpisodeUncertain("status=interrupted\nuncertain abort") {
		t.Fatal("interrupted must be uncertain")
	}
	if horizonEpisodeUncertain("status=passed\nopened notepad") {
		t.Fatal("passed executor is not uncertain")
	}
	if horizonEpisodeUncertain("status=passed\nfixed uncertain abort handling") {
		t.Fatal("passed summary must not match uncertain by substring")
	}
}

func TestComputerUseResetDoesNotCancelHorizon(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	h.horizonProjectPathFn = func(string) string { return t.TempDir() }
	h.horizonStartSupervisor = func(*horizonSession) {}
	if _, handled := h.handleHorizonIMRoute(IMUserMessage{UserID: "u1", Lang: "en", Text: "@horizon open notepad"}, "@horizon open notepad"); !handled {
		t.Fatal("expected admit")
	}
	if !h.horizonActive("u1") {
		t.Fatal("horizon should be active")
	}
	app := &App{}
	if err := app.ComputerUseReset(); err != nil {
		t.Fatal(err)
	}
	if !h.horizonActive("u1") {
		t.Fatal("operator CU reset must not cancel Horizon")
	}
}

func TestHorizonGUIMaxIterationsNotFloored(t *testing.T) {
	loopCtx := NewLoopContext("horizon-test", longhorizon.GUIMaxIterations, nil)
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	if got := cb.GetMaxIterations(); got != longhorizon.GUIMaxIterations {
		t.Fatalf("gui iterations = %d, want %d", got, longhorizon.GUIMaxIterations)
	}
}

func TestHorizonGUIMaxIterationsFallsBackWithoutLoopCtx(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	if got := cb.GetMaxIterations(); got != longhorizon.GUIMaxIterations {
		t.Fatalf("gui fallback iterations = %d, want %d not CLI %d", got, longhorizon.GUIMaxIterations, longhorizon.CLIMaxIterations)
	}
}

func TestHorizonDefaultSystemPromptIsNotCLIForGUI(t *testing.T) {
	got := horizonDefaultSystemPrompt(longhorizon.RoleGUIExecutor)
	if !strings.Contains(got, "computer_") || strings.Contains(got, "Do not call computer-use") {
		t.Fatalf("GUI default prompt leaked CLI: %q", got)
	}
}

func TestStripHorizonAcceptanceBulletTab(t *testing.T) {
	if got := stripHorizonAcceptanceBullet("-\twindow visible"); got != "window visible" {
		t.Fatalf("tab bullet = %q", got)
	}
}

func TestHorizonPostureAllowsHostToolsWhenInquiryKindSet(t *testing.T) {
	reg := NewToolRegistry()
	if err := reg.Register(RegisteredTool{
		Name:        "computer_observe",
		Description: "observe desktop",
		InputSchema: map[string]interface{}{},
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: reg}
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "what window is focused"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{
		subagent: sa,
		task:     &TaskItem{Title: "what window is focused", RequestKind: codingRequestInquiry},
	}
	got := cb.ExecuteToolStructured("computer_observe", `{}`)
	if strings.Contains(got.Result, "unavailable for a read-only repository inquiry") {
		t.Fatalf("horizon GUI tool blocked by inquiry filter: %s", got.Result)
	}
}

func TestInstallHorizonLoopCtxCLIDoesNotStealComputerUseOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	setComputerUseOwner("tab-a")
	h := &IMMessageHandler{}
	sentinel := NewLoopContext("im", 8, nil)
	h.currentLoopCtx = sentinel
	h.lastUserID = "im-user"
	loopCtx := NewLoopContext("h", 80, nil)
	loopCtx.UserID = "u1"
	loopCtx.HorizonRole = longhorizon.RoleCLIExecutor
	restore := h.installHorizonLoopCtx(loopCtx, "u1")
	if computerUseOwnerKey() != "tab-a" {
		t.Fatalf("CLI stole CU owner: %s", computerUseOwnerKey())
	}
	if h.currentLoopCtx != sentinel || h.lastUserID != "im-user" {
		t.Fatal("CLI must not steal currentLoopCtx")
	}
	restore()
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	restore = h.installHorizonLoopCtx(loopCtx, "u1")
	if computerUseOwnerKey() != "u1" {
		t.Fatalf("GUI should bind CU owner, got %s", computerUseOwnerKey())
	}
	if h.currentLoopCtx != loopCtx || h.lastUserID != "u1" {
		t.Fatal("GUI should bind currentLoopCtx")
	}
	restore()
	if computerUseOwnerKey() != "tab-a" {
		t.Fatalf("GUI restore failed: %s", computerUseOwnerKey())
	}
	if h.currentLoopCtx != sentinel || h.lastUserID != "im-user" {
		t.Fatal("GUI must restore currentLoopCtx")
	}
}

func TestHorizonBrowserHostToolDoesNotStealComputerUseOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	setComputerUseOwner("tab-a")
	h := &IMMessageHandler{}
	loopCtx := NewLoopContext("h", 20, nil)
	loopCtx.UserID = "u1"
	loopCtx.ComputerUseOwner = "u1"
	loopCtx.HorizonRole = longhorizon.RoleBrowserExecutor
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleBrowserExecutor, longhorizon.ManagerPlan{Goal: "open site"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa}
	_ = cb.executeHorizonHostTool("browser_observe", `{}`)
	if computerUseOwnerKey() != "tab-a" {
		t.Fatalf("browser host tool stole CU owner: %s", computerUseOwnerKey())
	}
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	_ = cb.executeHorizonHostTool("computer_observe", `{}`)
	if computerUseOwnerKey() != "u1" {
		t.Fatalf("GUI host tool should bind CU owner, got %s", computerUseOwnerKey())
	}
}

func TestHorizonCLIProbeUsesWorkspaceListing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := (&IMMessageHandler{}).horizonProbe(context.Background(), dir)
	if !got.OK || !strings.Contains(got.Digest, "readme.txt") {
		t.Fatalf("cheap CLI probe = %+v", got)
	}
	if strings.Contains(strings.ToLower(got.Digest), "go test") || strings.Contains(strings.ToLower(got.Digest), "go build") {
		t.Fatalf("CLI probe must not run full verify: %q", got.Digest)
	}
}

func TestRecordHorizonExecutorAsk(t *testing.T) {
	sess := &horizonSession{state: &longhorizon.TaskState{}}
	recordHorizonExecutorAsk(sess, "which window?")
	if len(sess.state.Carryover) != 1 || !strings.Contains(sess.state.Carryover[0], "which window?") {
		t.Fatalf("carryover = %#v", sess.state.Carryover)
	}
}

func TestShouldAccelerateHorizonGUIAuditRejectsFailedClaim(t *testing.T) {
	probe := "window visible\nfile saved"
	acc := "window visible\nfile saved"
	if !shouldAccelerateHorizonGUIAudit("status=passed\nopened notepad", acc, probe) {
		t.Fatal("successful claim with matching observe must accelerate")
	}
	if shouldAccelerateHorizonGUIAudit("status=failed\ncould not find notepad", acc, probe) {
		t.Fatal("failed executor must not mechanical-pass")
	}
	if shouldAccelerateHorizonGUIAudit("status=ask\nwhich window?", acc, probe) {
		t.Fatal("ask must not mechanical-pass")
	}
	if shouldAccelerateHorizonGUIAudit("status=interrupted\nuncertain abort", acc, probe) {
		t.Fatal("interrupted must not mechanical-pass")
	}
	if shouldAccelerateHorizonGUIAudit("executor returned nil", acc, probe) {
		t.Fatal("unknown executor status must not mechanical-pass")
	}
	if shouldAccelerateHorizonGUIAudit("horizon episode: cancelled", acc, probe) {
		t.Fatal("cancelled episode must not mechanical-pass")
	}
}

func TestFormatHorizonEpisodeResultInterruptedBeatsAsk(t *testing.T) {
	got := formatHorizonEpisodeResult(&CodingSubAgentResult{
		Status:      TaskExecInterrupted,
		Summary:     "uncertain abort",
		AskQuestion: "which window?",
	})
	if !strings.HasPrefix(got, "status=interrupted") {
		t.Fatalf("cancel must beat ask: %q", got)
	}
	if q := horizonEpisodeAskQuestion(got); q != "" {
		t.Fatalf("interrupted result must not pause outer loop, got %q", q)
	}
}

func TestReleaseHorizonGUIAfterProbeClearsSticky(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	setComputerUseOwner("u1")
	beginComputerUseTask("u1", "req-hz", "open notepad", nil)
	seedComputerUseObserve(t, "notepad", "Notepad")
	markComputerUseSessionActive()
	setHorizonComputerUseClaimOnly("u1", true)
	if !computerUseSessionActive() || computerUseTaskStateFor("u1") == nil {
		t.Fatal("precondition: sticky CU and task state")
	}
	releaseHorizonGUIAfterProbe("u1")
	if computerUseSessionActive() {
		t.Fatal("horizon GUI must not leave sticky CU for the next chat")
	}
	if computerUseTaskStateFor("u1") != nil {
		t.Fatal("horizon GUI must not leave a CU task contract behind")
	}
	if horizonComputerUseClaimOnly("u1") {
		t.Fatal("claim-only must clear only after the auditor probe")
	}
	if cuSessionForOwner("u1") == nil || cuSessionForOwner("u1").LastValidObserve() == nil {
		t.Fatal("probe snapshot must remain after sticky release")
	}
}

func TestHorizonGUIProbeUsesStoredComputerUseOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	cuOwner := "im:desktop:u1:actor"
	setComputerUseOwner(cuOwner)
	seedComputerUseObserve(t, "notepad visible", "Notepad")
	setHorizonComputerUseClaimOnly(cuOwner, true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly(cuOwner, false) })
	sess := &horizonSession{ownerID: "u1", computerUseOwner: cuOwner}
	probe := horizonGUIProbe(sess.computerUseOwnerOr("u1"))
	if !probe.OK || !strings.Contains(probe.Digest, "notepad visible") {
		t.Fatalf("probe must use stored CU owner, got %+v", probe)
	}
	if horizonGUIProbe("u1").OK {
		t.Fatal("UserID lookup must not see a SessionKey-keyed observe")
	}
}

func TestHorizonHostToolPinsComputerUseOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("tab-a")
	setHorizonComputerUseClaimOnly("u1", true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly("u1", false) })

	reg := NewToolRegistry()
	registerComputerUseTools(reg, nil)
	h := &IMMessageHandler{registry: reg}
	loopCtx := NewLoopContext("horizon-pin", 5, nil)
	loopCtx.UserID = "u1"
	loopCtx.ComputerUseOwner = "u1"
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	got := cb.executeToolWithOutcome("computer_done", `{"summary":"opened"}`)
	if !strings.Contains(got.Text, "computer_done claim:") {
		t.Fatalf("host tool must pin GUI owner for claim-only done, got %q owner=%s", got.Text, computerUseOwnerKey())
	}
	if computerUseOwnerKey() != "u1" {
		t.Fatalf("CU owner after host tool = %q, want u1", computerUseOwnerKey())
	}
}

func TestHorizonComputerToolPinsOwnerWithoutGUIRole(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("tab-a")
	setHorizonComputerUseClaimOnly("u1", true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly("u1", false) })

	reg := NewToolRegistry()
	registerComputerUseTools(reg, nil)
	h := &IMMessageHandler{registry: reg}
	loopCtx := NewLoopContext("horizon-pin-name", 5, nil)
	loopCtx.UserID = "u1"
	loopCtx.ComputerUseOwner = "u1"
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	ep := longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{})
	sa.SetHorizonEpisode(ep)
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	got := cb.executeToolWithOutcome("computer_done", `{"summary":"opened"}`)
	if !strings.Contains(got.Text, "computer_done claim:") {
		t.Fatalf("computer_* must pin owner even without HorizonRole, got %q", got.Text)
	}
}

func TestHorizonGUIHostToolIgnoresLeftoverLocalFileFence(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_observe",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop observed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	leftover := NewLoopContext("chat-attach", 1, nil)
	leftover.ComputerUseBlockedForLocalFileWork = true
	h.setSessionLoopCtx("u1", leftover)

	loopCtx := NewLoopContext("horizon-gui", 20, nil)
	loopCtx.UserID = "u1"
	loopCtx.ComputerUseOwner = "u1"
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	restore := h.installHorizonLoopCtx(loopCtx, "u1")
	defer restore()

	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	got := cb.executeHorizonHostTool("computer_observe", `{}`)
	if !called {
		t.Fatalf("Horizon GUI computer_observe blocked by leftover chat fence: %q", got.Text)
	}
	if strings.Contains(got.Text, "local attachment") {
		t.Fatalf("leftover fence leaked into Horizon GUI: %q", got.Text)
	}
}

func TestHorizonGUIFenceDoesNotUnblockOtherUser(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_observe",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop observed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	leftover := NewLoopContext("other-attach", 1, nil)
	leftover.ComputerUseBlockedForLocalFileWork = true
	h.setSessionLoopCtx("u2", leftover)

	loopCtx := NewLoopContext("horizon-gui", 20, nil)
	loopCtx.UserID = "u1"
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	restore := h.installHorizonLoopCtx(loopCtx, "u1")
	defer restore()

	got := h.executeToolDetailedWithRuntimeState("u2", true, "desktop", "computer_observe", `{}`, "", nil)
	if called {
		t.Fatal("other user's leftover local-file fence must still block computer_*")
	}
	if got.FailureKind != toolFailurePolicyRejected || !strings.Contains(got.Text, "local attachment") {
		t.Fatalf("result = %+v, want leftover fence rejection for u2", got)
	}
}

func TestInstallHorizonLoopCtxNestedGUIDoesNotClobberOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	setComputerUseOwner("tab-a")
	first := NewLoopContext("gui-a", 20, nil)
	first.UserID = "u1"
	first.HorizonRole = longhorizon.RoleGUIExecutor
	restoreFirst := h.installHorizonLoopCtx(first, "u1")
	second := NewLoopContext("gui-b", 20, nil)
	second.UserID = "u2"
	second.HorizonRole = longhorizon.RoleGUIExecutor
	restoreSecond := h.installHorizonLoopCtx(second, "u2")
	if computerUseOwnerKey() != "u2" {
		t.Fatalf("inner GUI owner = %s, want u2", computerUseOwnerKey())
	}
	restoreFirst()
	if computerUseOwnerKey() != "u2" {
		t.Fatalf("outer restore clobbered inner GUI owner: %s", computerUseOwnerKey())
	}
	if h.currentLoopCtx != second {
		t.Fatal("outer restore must not steal inner currentLoopCtx")
	}
	restoreSecond()
	if computerUseOwnerKey() != "u1" {
		t.Fatalf("inner restore = %s, want outer GUI owner u1", computerUseOwnerKey())
	}
}

func TestInstallHorizonLoopCtxNestedGUILIFORestoresOriginal(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	setComputerUseOwner("tab-a")
	first := NewLoopContext("gui-a", 20, nil)
	first.UserID = "u1"
	first.HorizonRole = longhorizon.RoleGUIExecutor
	restoreFirst := h.installHorizonLoopCtx(first, "u1")
	second := NewLoopContext("gui-b", 20, nil)
	second.UserID = "u2"
	second.HorizonRole = longhorizon.RoleGUIExecutor
	restoreSecond := h.installHorizonLoopCtx(second, "u2")
	restoreSecond()
	if computerUseOwnerKey() != "u1" || h.currentLoopCtx != first {
		t.Fatalf("LIFO inner restore owner=%s ctx=%v", computerUseOwnerKey(), h.currentLoopCtx == first)
	}
	restoreFirst()
	if computerUseOwnerKey() != "tab-a" {
		t.Fatalf("LIFO outer restore = %s, want tab-a", computerUseOwnerKey())
	}
}

func TestHorizonGUIHostToolIgnoresLeftoverGroupPermissions(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_observe",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop observed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	leftover := NewLoopContext("group-chat", 1, nil)
	leftover.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{}
	h.setSessionLoopCtx("u1", leftover)
	blocked := h.executeToolDetailedWithRuntimeState("u1", true, "desktop", "computer_observe", `{}`, "", nil)
	if called || blocked.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("leftover group loop must still block computer_* without Horizon GUI: %+v called=%v", blocked, called)
	}

	loopCtx := NewLoopContext("horizon-gui", 20, nil)
	loopCtx.UserID = "u1"
	loopCtx.ComputerUseOwner = "u1"
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	restore := h.installHorizonLoopCtx(loopCtx, "u1")
	defer restore()

	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	got := cb.executeHorizonHostTool("computer_observe", `{}`)
	if !called {
		t.Fatalf("Horizon GUI computer_observe blocked by leftover group permissions: %q", got.Text)
	}
}

func TestHorizonBrowserHostToolIgnoresLeftoverGroupPermissions(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "browser",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "browser observed"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := &IMMessageHandler{registry: registry}
	leftover := NewLoopContext("group-chat", 1, nil)
	leftover.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{}
	h.setSessionLoopCtx("u1", leftover)
	blocked := h.executeToolDetailedWithRuntimeState("u1", true, "desktop", "browser_observe", `{}`, "", nil)
	if called || blocked.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("leftover group loop must still block browser_* without Horizon host: %+v called=%v", blocked, called)
	}

	loopCtx := NewLoopContext("horizon-browser", 20, nil)
	loopCtx.UserID = "u1"
	loopCtx.HorizonRole = longhorizon.RoleBrowserExecutor
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleBrowserExecutor, longhorizon.ManagerPlan{Goal: "open site"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open site"}}
	got := cb.executeHorizonHostTool("browser_observe", `{}`)
	if !called {
		t.Fatalf("Horizon browser host tool blocked by leftover group permissions: %q", got.Text)
	}
}

func TestHorizonHostToolIgnoresWorkflowDocOnlyPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "horizon-wf-cu-" + t.Name()
	engine := handler.app.workflowEngine
	workflowType := v2.WorkflowType("horizon_host_doc_only_" + t.Name())
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "horizon host doc only",
		Description: "test",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{Category: workflowType, Summary: "plan"}); err != nil {
		t.Fatalf("StartWorkflow: %v", err)
	}
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_observe",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop observed"
		},
	}); err != nil {
		t.Fatalf("Register tool: %v", err)
	}
	handler.registry = registry
	blocked := handler.executeToolDetailedWithRuntimeState(userID, true, "desktop", "computer_observe", `{}`, "", nil)
	if called || blocked.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("doc-only workflow must still block computer_* without Horizon host: %+v called=%v", blocked, called)
	}

	loopCtx := NewLoopContext("horizon-gui", 20, nil)
	loopCtx.UserID = userID
	loopCtx.ComputerUseOwner = userID
	loopCtx.HorizonRole = longhorizon.RoleGUIExecutor
	restore := handler.installHorizonLoopCtx(loopCtx, userID)
	defer restore()
	sa := NewCodingSubAgent(handler, corelib.MaclawLLMConfig{}, nil, t.TempDir(), loopCtx)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleGUIExecutor, longhorizon.ManagerPlan{Goal: "open notepad"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open notepad"}}
	got := cb.executeHorizonHostTool("computer_observe", `{}`)
	if !called {
		t.Fatalf("Horizon GUI computer_observe blocked by leftover workflow policy: %q", got.Text)
	}
}

func TestParseHorizonBrowserSessionID(t *testing.T) {
	got := parseHorizonBrowserSessionID(`{"ok":true,"data":{"session_id":"sess-1"}}`)
	if got != "sess-1" {
		t.Fatalf("nested session id = %q", got)
	}
	if parseHorizonBrowserSessionID(`{"ok":false,"data":{"session_id":"sess-fail"}}`) != "sess-fail" {
		t.Fatal("OpenURL failure after create must still expose session_id for cleanup")
	}
	if parseHorizonBrowserSessionID(`tool execution interrupted: canceled; {"ok":true,"data":{"session_id":"sess-2"}}`) != "sess-2" {
		t.Fatal("prefixed JSON must still parse session_id")
	}
	if parseHorizonBrowserSessionID("not json") != "" {
		t.Fatal("garbage must not be tracked")
	}
}

func TestHorizonBrowserHostToolGetsDescription(t *testing.T) {
	reg := NewToolRegistry()
	if err := reg.Register(RegisteredTool{Name: "browser_session_start", Description: "", InputSchema: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: reg}
	sa := NewCodingSubAgent(h, corelib.MaclawLLMConfig{}, nil, t.TempDir(), nil)
	sa.SetHorizonEpisode(longhorizon.AssembleEpisodeContext(longhorizon.RoleBrowserExecutor, longhorizon.ManagerPlan{Goal: "open site"}, nil, "", longhorizon.PolicySnapshot{}))
	cb := &codingSubAgentCallbacks{subagent: sa, task: &TaskItem{Title: "open site"}}
	found := ""
	for _, tool := range cb.BuildTools("open site") {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name != "browser_session_start" {
			continue
		}
		found, _ = fn["description"].(string)
	}
	if strings.TrimSpace(found) == "" || found == "browser_session_start" {
		t.Fatalf("blank IM browser def must get a real Horizon description, got %q", found)
	}
}

func TestRememberHorizonBrowserSessionCreatedVsReused(t *testing.T) {
	sess := &horizonSession{}
	sess.rememberBrowserSession("reused", false)
	sess.rememberBrowserSession("created", true)
	sess.rememberBrowserSession("reused", false)
	if got := sess.latestBrowserSessionID(); got != "created" {
		t.Fatalf("latest = %q", got)
	}
	created := sess.takeCreatedBrowserSessionIDs()
	if len(created) != 1 || created[0] != "created" {
		t.Fatalf("created = %#v", created)
	}
	if extra := sess.takeCreatedBrowserSessionIDs(); len(extra) != 0 {
		t.Fatalf("second take = %#v", extra)
	}
}

func TestReleaseHorizonBrowserSessionsIgnoresUnknownIDs(t *testing.T) {
	sess := &horizonSession{}
	sess.rememberBrowserSession("missing-horizon-browser", true)
	(&IMMessageHandler{}).releaseHorizonBrowserSessions(sess)
	if extra := sess.takeCreatedBrowserSessionIDs(); len(extra) != 0 {
		t.Fatalf("release must consume created ids, leftover=%#v", extra)
	}
}

func TestRetrieveHorizonManagerEvidenceRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := (&IMMessageHandler{app: &App{}}).retrieveHorizonManagerEvidence(ctx, nil, &longhorizon.TaskState{UserGoal: "add tests"})
	if got != "" {
		t.Fatalf("cancelled retrieve = %q", got)
	}
}

func TestRecordHorizonExecutorStartLockedPersistsCarryover(t *testing.T) {
	root := t.TempDir()
	sess := &horizonSession{
		storeRoot: root,
		state: &longhorizon.TaskState{
			TaskID: "hz-start",
			Policy: longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-start"},
		},
	}
	sess.mu.Lock()
	recordHorizonExecutorStartLocked(sess, 2, longhorizon.NextCLI, "add tests")
	sess.persistLocked()
	sess.mu.Unlock()
	loaded, err := longhorizon.LoadTaskState(root, "hz-start")
	if err != nil || loaded == nil || len(loaded.Carryover) != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if !strings.Contains(loaded.Carryover[0], "Executor round 2 started (cli)") || !strings.Contains(loaded.Carryover[0], "add tests") {
		t.Fatalf("carryover = %#v", loaded.Carryover)
	}
}

func TestHorizonLogClipQuoteAndIDs(t *testing.T) {
	if got := horizonLogClip("  hello   world  ", 80); got != "hello world" {
		t.Fatalf("clip fields = %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := horizonLogClip(long, 80); len([]rune(got)) > 80 {
		t.Fatalf("clip len = %d", len([]rune(got)))
	}
	if q := horizonLogQuote("say hi"); q != `"say hi"` {
		t.Fatalf("quote = %s", q)
	}
	sess := &horizonSession{
		ownerID: "u1",
		state:   &longhorizon.TaskState{TaskID: "hz-1", Status: longhorizon.StatusManaging, RoundIndex: 3},
	}
	task, owner, status, round := sess.horizonLogIDsLocked()
	if task != "hz-1" || owner != "u1" || status != longhorizon.StatusManaging || round != 3 {
		t.Fatalf("ids task=%s owner=%s status=%s round=%d", task, owner, status, round)
	}
	horizonLog(sess, "admit", "goal="+horizonLogQuote("add tests"))
}

func TestFinishHorizonCancelledIdempotent(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	cuOwner := "im:desktop:u1:actor"
	setHorizonComputerUseClaimOnly(cuOwner, true)
	sess := &horizonSession{ownerID: "u1", computerUseOwner: cuOwner, state: &longhorizon.TaskState{TaskID: "t1"}}
	h := &IMMessageHandler{}
	h.finishHorizonCancelled(sess, "en")
	h.finishHorizonCancelled(sess, "en")
	if !sess.finalized || !sess.cancelled {
		t.Fatal("expected finalized cancelled session")
	}
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("claim-only must stay cleared")
	}
}

func TestHorizonLogFieldQuotesAndOwner(t *testing.T) {
	if got := horizonLogField("project", `D:\work prj\app`); got != `project="D:\\work prj\\app"` {
		t.Fatalf("field = %s", got)
	}
	if got := horizonLogField("goal", "add tests"); got != `goal="add tests"` {
		t.Fatalf("goal field = %s", got)
	}
}

func TestCancelHorizonSessionClearsGUIClaimOnly(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	cuOwner := "im:desktop:u1:actor"
	setHorizonComputerUseClaimOnly(cuOwner, true)
	h := &IMMessageHandler{}
	sess := &horizonSession{
		ownerID:          "u1",
		computerUseOwner: cuOwner,
		notify:           make(chan struct{}, 1),
		state:            &longhorizon.TaskState{TaskID: "t1"},
	}
	h.storeHorizonSession(sess)
	if !h.cancelHorizonSessionWithReason("u1", "session") {
		t.Fatal("expected cancel")
	}
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("cancel must drop GUI claim-only immediately")
	}
	if !sess.cancelNotified {
		t.Fatal("stop-button cancel must suppress duplicate cancelled reply")
	}
}

func TestWaitHorizonInboxReturnsOnSessionCancel(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{notify: make(chan struct{}, 1)}
	gotCh := make(chan bool, 1)
	go func() {
		_, ok := h.waitHorizonInbox(context.Background(), sess)
		gotCh <- ok
	}()
	sess.mu.Lock()
	sess.cancelled = true
	sess.mu.Unlock()
	select {
	case sess.notify <- struct{}{}:
	default:
	}
	select {
	case ok := <-gotCh:
		if ok {
			t.Fatal("cancelled wait must return false")
		}
	case <-time.After(time.Second):
		t.Fatal("waitHorizonInbox did not observe session cancel")
	}
}

func TestWaitHorizonInboxCancelBeatsPendingItems(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{notify: make(chan struct{}, 1)}
	sess.enqueue("late")
	sess.mu.Lock()
	sess.cancelled = true
	sess.mu.Unlock()
	_, ok := h.waitHorizonInbox(context.Background(), sess)
	if ok {
		t.Fatal("cancel must beat leftover inbox items")
	}
}

func TestArmHorizonGUIEpisodeCancelledSkipsReset(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	cuOwner := "im:desktop:u1:actor"
	setComputerUseOwner(cuOwner)
	seedComputerUseObserve(t, "notepad", "Notepad")
	sess := &horizonSession{ownerID: "u1", cancelled: true}
	if armHorizonGUIEpisode(sess, nil, "u1", cuOwner, "open notepad") {
		t.Fatal("cancelled session must not arm GUI")
	}
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("cancelled GUI arm must not leave claim-only")
	}
	if cuSessionForOwner(cuOwner) == nil || cuSessionForOwner(cuOwner).LastValidObserve() == nil {
		t.Fatal("cancelled GUI arm must not reset an existing CU observe")
	}
}

func TestHorizonWaitAborted(t *testing.T) {
	if !horizonWaitAborted(context.Background(), nil) {
		t.Fatal("nil session is aborted")
	}
	sess := &horizonSession{}
	if horizonWaitAborted(context.Background(), sess) {
		t.Fatal("live session")
	}
	sess.cancelled = true
	if !horizonWaitAborted(context.Background(), sess) {
		t.Fatal("cancelled session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !horizonWaitAborted(ctx, &horizonSession{}) {
		t.Fatal("cancelled context")
	}
}

func TestCancelHorizonSessionFindsRunningAfterDrop(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	cuOwner := "im:desktop:u1:actor"
	setHorizonComputerUseClaimOnly(cuOwner, true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly(cuOwner, false) })
	sess := &horizonSession{
		ownerID:          "u1",
		computerUseOwner: cuOwner,
		notify:           make(chan struct{}, 1),
		state:            &longhorizon.TaskState{TaskID: "t-run"},
	}
	h.storeHorizonSession(sess)
	h.markHorizonRunning(sess)
	if !h.cancelHorizonSessionWithReason("u1", "session") {
		t.Fatal("first cancel")
	}
	if h.loadHorizonSession("u1") != nil {
		t.Fatal("cancelled session must be dropped from the live map")
	}
	if !h.horizonSupervisorRunning("u1") {
		t.Fatal("supervisor still stopping")
	}
	if !h.cancelHorizonSessionWithReason("u1", "session") {
		t.Fatal("stop must still find the supervisor while it is stopping")
	}
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("repeat stop must keep GUI claim-only cleared")
	}
}

func TestCancelSessionForUserWhileHorizonStopping(t *testing.T) {
	h := &IMMessageHandler{}
	sess := &horizonSession{
		ownerID: "u1",
		notify:  make(chan struct{}, 1),
		state:   &longhorizon.TaskState{TaskID: "t-stop"},
	}
	h.markHorizonRunning(sess)
	got, err := h.CancelSessionForUser("u1")
	if err != nil || got != "horizon" {
		t.Fatalf("cancel while stopping = %q err=%v", got, err)
	}
	if !sess.cancelled {
		t.Fatal("running supervisor must still be cancelled")
	}
}

func TestCancelAllSessionsForShutdownCancelsHorizon(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	cuOwner := "im:desktop:u1:actor"
	setHorizonComputerUseClaimOnly(cuOwner, true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly(cuOwner, false) })
	sess := &horizonSession{
		ownerID:          "u1",
		computerUseOwner: cuOwner,
		notify:           make(chan struct{}, 1),
		state:            &longhorizon.TaskState{TaskID: "t-shutdown"},
	}
	h.storeHorizonSession(sess)
	h.markHorizonRunning(sess)
	h.cancelAllSessionsForShutdown()
	if !sess.cancelled {
		t.Fatal("hardware/bot shutdown must cancel Horizon")
	}
	if h.loadHorizonSession("u1") != nil {
		t.Fatal("shutdown must drop the live Horizon session")
	}
	if !sess.cancelNotified {
		t.Fatal("shutdown must not emit a cancelled chat bubble")
	}
	if horizonComputerUseClaimOnly(cuOwner) {
		t.Fatal("shutdown must drop GUI claim-only")
	}
}
