package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type workingStateHolderCB struct {
	*mockCallbacks
	loaded     *WorkingState
	saved      *WorkingState
	saveCalled bool
}

func (c *workingStateHolderCB) LoadWorkingState() *WorkingState { return c.loaded }
func (c *workingStateHolderCB) SaveWorkingState(s *WorkingState) {
	c.saveCalled = true
	c.saved = s
}

func TestRunLoop_WorkingStateAfterToolAndNoHistoryLeak(t *testing.T) {
	var secondSystem string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 {
			secondSystem = systemContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		} else {
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "ok",
	}
	result := RunLoop(cb, "fix compile error please", nil, nil)
	if result.Error != "" {
		t.Fatalf("error: %s", result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.Goal == "" {
		t.Fatalf("expected working state, got %#v", result.WorkingState)
	}
	if result.WorkingState.LastAction != ActionTrust {
		t.Fatalf("LastAction=%q", result.WorkingState.LastAction)
	}
	if !strings.Contains(secondSystem, WorkingStateMarker) || !strings.Contains(secondSystem, result.WorkingState.Goal) {
		t.Fatalf("second system missing section: %q", secondSystem)
	}
	for _, entry := range result.HistoryDelta {
		if text, ok := entry.Content.(string); ok && strings.Contains(text, WorkingStateMarker) {
			t.Fatalf("HistoryDelta leaked marker: %+v", entry)
		}
	}
}

func TestRunLoop_WorkingStateOffLeavesNil(t *testing.T) {
	t.Setenv(WorkingStateEnvKey, "off")
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		} else {
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "sys",
		toolResult: "ok",
	}
	result := RunLoop(cb, "fix it", nil, nil)
	if result.WorkingState != nil {
		t.Fatalf("off should leave nil, got %#v", result.WorkingState)
	}
}

func TestRunLoop_WorkingStateNotesPolicyRejectAfterTool(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		case 2:
			resp = toolCallResponse("bash", `{"command":"true"}`)
		default:
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "sys",
		toolResult: "ok",
		allowed:    map[string]bool{"write_file": true},
	}
	result := RunLoop(cb, "fix compile error please", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.LastAction != ActionRetryDiagnose {
		t.Fatalf("denied bash after a file ok should open: %#v", result.WorkingState)
	}
	if UnclosedOpenCount(result.WorkingState) == 0 {
		t.Fatalf("expected open from policy deny: %+v", result.WorkingState.Open)
	}
}

func TestRunLoop_WorkingStateAskUserCarriesAndHolderSaves(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolCallResponse("ask_user", `{"question":"Choose one"}`))
	}))
	defer server.Close()
	cb := &workingStateHolderCB{
		mockCallbacks: &mockCallbacks{
			config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:    10,
			sysPrompt:  "sys",
			toolResult: `__ASK_USER__{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
		},
	}
	result := RunLoop(cb, "need a choice", nil, nil)
	if result.AskUser == nil {
		t.Fatal("expected ask_user")
	}
	if result.WorkingState == nil || result.WorkingState.LastAction != ActionSeekUser {
		t.Fatalf("ask_user should carry state: %#v", result.WorkingState)
	}
	if result.WorkingState.Next != nextSeekUser() {
		t.Fatalf("ask_user Next=%q", result.WorkingState.Next)
	}
	if !cb.saveCalled || cb.saved == nil || cb.saved.LastAction != ActionSeekUser {
		t.Fatalf("holder save = called=%v saved=%#v", cb.saveCalled, cb.saved)
	}
}

func TestRunLoop_WorkingStateAskUserResumeKeepsGoal(t *testing.T) {
	var firstSystem string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if firstSystem == "" {
			firstSystem = systemContentFromRequest(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse("ok"))
	}))
	defer server.Close()
	carried := NewWorkingState("keep goal")
	carried.Next = "ask user"
	carried.LastAction = ActionSeekUser
	cb := &workingStateHolderCB{
		mockCallbacks: &mockCallbacks{
			config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:   5,
			sysPrompt: "sys",
		},
		loaded: carried,
	}
	result := RunLoop(cb, "A", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !strings.Contains(firstSystem, WorkingStateMarker) || !strings.Contains(firstSystem, "keep goal") {
		t.Fatalf("resume missing goal: %q", firstSystem)
	}
}

func TestRunLoop_WorkingStateHolderClearedOnNormalFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse("hello"))
	}))
	defer server.Close()
	cb := &workingStateHolderCB{
		mockCallbacks: &mockCallbacks{
			config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:   5,
			sysPrompt: "sys",
		},
		loaded: NewWorkingState("old"),
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !cb.saveCalled || cb.saved != nil {
		t.Fatalf("normal finish should Save(nil), saved=%#v", cb.saved)
	}
}

type workingStateGoalSourceCB struct {
	*mockCallbacks
	goal string
}

func (c *workingStateGoalSourceCB) ActiveWorkingStateGoal() string { return c.goal }

func TestRunLoop_WorkingStateGoalSourceAttachesFirstRequest(t *testing.T) {
	var firstSystem string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if firstSystem == "" {
			firstSystem = systemContentFromRequest(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse("ok"))
	}))
	defer server.Close()
	cb := &workingStateGoalSourceCB{
		mockCallbacks: &mockCallbacks{
			config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:   5,
			sysPrompt: "sys",
		},
		goal: "horizon projected",
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.Goal != "horizon projected" {
		t.Fatalf("state=%#v", result.WorkingState)
	}
	if !strings.Contains(firstSystem, WorkingStateMarker) || !strings.Contains(firstSystem, "horizon projected") {
		t.Fatalf("first request missing projection: %q", firstSystem)
	}
}

func TestRunLoop_WorkingStateStripsVisibleMarker(t *testing.T) {
	leaked := "user answer\n" + WorkingStateMarker + "\n目标: leaked"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse(leaked))
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   5,
		sysPrompt: "sys",
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if strings.Contains(result.Text, WorkingStateMarker) {
		t.Fatalf("Text leaked marker: %q", result.Text)
	}
	if !strings.Contains(result.Text, "user answer") {
		t.Fatalf("stripped too much: %q", result.Text)
	}
	for _, entry := range result.HistoryDelta {
		if text, ok := entry.Content.(string); ok && strings.Contains(text, WorkingStateMarker) {
			t.Fatalf("HistoryDelta leaked marker: %+v", entry)
		}
	}
}

func TestRunLoop_WorkingStateLightDoesNotSplice(t *testing.T) {
	var secondSystem string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 {
			secondSystem = systemContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		} else {
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &lightProfileCallbacks{
		mockCallbacks: &mockCallbacks{
			config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:    10,
			sysPrompt:  "LIGHT",
			toolResult: "ok",
			allowed:    map[string]bool{"write_file": true},
		},
		profile: PromptProfileLight,
	}
	result := RunLoop(cb, "write a file", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if strings.Contains(secondSystem, WorkingStateMarker) {
		t.Fatalf("light spliced section: %q", secondSystem)
	}
}

type wipeSystemAfterToolHooks struct {
	DefaultLoopHooks
}

func (wipeSystemAfterToolHooks) TransformConversation(conversation []interface{}) []interface{} {
	if len(conversation) < 3 {
		return nil
	}
	out := append([]interface{}{}, conversation...)
	out[0] = map[string]interface{}{"role": "system", "content": "compacted"}
	return out
}

type consumeSteerCB struct {
	*mockCallbacks
	DefaultLoopHooks
	consumed bool
}

func (c *consumeSteerCB) LLMReplanRequested() bool {
	return len(c.toolCalls) > 0 && !c.consumed
}

func (c *consumeSteerCB) TransformConversation(conversation []interface{}) []interface{} {
	if len(c.toolCalls) > 0 {
		c.consumed = true
	}
	return nil
}

func TestRunLoop_WorkingStateSteerClearsLiveKeepsGoal(t *testing.T) {
	var secondSystem string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 {
			secondSystem = systemContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		} else {
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &consumeSteerCB{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "sys",
		toolResult: "ok",
	}}
	result := RunLoop(cb, "fix compile error please", nil, nil, cb)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.Goal == "" {
		t.Fatalf("state=%#v", result.WorkingState)
	}
	if !strings.Contains(secondSystem, WorkingStateMarker) || !strings.Contains(secondSystem, result.WorkingState.Goal) {
		t.Fatalf("steer dropped goal: %q", secondSystem)
	}
	if strings.Contains(secondSystem, "台上:") {
		t.Fatalf("steer must clear live: %q", secondSystem)
	}
	if len(result.WorkingState.Live) != 0 {
		t.Fatalf("live after steer: %+v", result.WorkingState.Live)
	}
	if strings.Contains(result.WorkingState.Next, "main.go") {
		t.Fatalf("Next still names evicted live: %q", result.WorkingState.Next)
	}
	if result.WorkingState.LastAction != "" {
		t.Fatalf("LastAction still %q after steer", result.WorkingState.LastAction)
	}
	if len(result.WorkingState.Settled) != 1 || result.WorkingState.Settled[0].Label != "main.go" {
		t.Fatalf("steer must keep settled from the steered batch: %+v", result.WorkingState.Settled)
	}
}

func TestRunLoop_WorkingStateDoneCheckNudgesWhenIterationsRemain(t *testing.T) {
	var thirdUser string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 3 {
			thirdUser = lastUserContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("bash", `{"command":"true"}`)
		} else {
			resp = textResponse("final answer")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     3,
		sysPrompt:   "sys",
		toolResult:  "fail",
		toolOutcome: ToolExecutionOutcomeError,
	}
	result := RunLoop(cb, "run it", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !strings.Contains(thirdUser, "还有未关闭问题") {
		t.Fatalf("expected done-check nudge: %q", thirdUser)
	}
}

func TestRunLoop_WorkingStateDoneCheckDoesNotEatLastIteration(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("bash", `{"command":"true"}`)
		} else {
			resp = textResponse("final answer")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     2,
		sysPrompt:   "sys",
		toolResult:  "fail",
		toolOutcome: ToolExecutionOutcomeError,
	}
	result := RunLoop(cb, "run it", nil, nil)
	if result.Error != "" {
		t.Fatalf("last-iter done-check must not error: %s", result.Error)
	}
	if !strings.Contains(result.Text, "final answer") {
		t.Fatalf("text=%q", result.Text)
	}
	if result.WorkingState == nil || UnclosedOpenCount(result.WorkingState) == 0 {
		t.Fatalf("expected open items: %#v", result.WorkingState)
	}
}

func TestRunLoop_WorkingStateSurvivesCompactAfterTool(t *testing.T) {
	var secondSystem string
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 {
			secondSystem = systemContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"main.go","content":"x"}`)
		} else {
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "ok",
	}
	result := RunLoop(cb, "fix compile error please", nil, nil, wipeSystemAfterToolHooks{})
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.Goal == "" {
		t.Fatalf("state=%#v", result.WorkingState)
	}
	if !strings.Contains(secondSystem, WorkingStateMarker) || !strings.Contains(secondSystem, result.WorkingState.Goal) {
		t.Fatalf("compact wiped goal: %q", secondSystem)
	}
	if !strings.Contains(secondSystem, "compacted") {
		t.Fatalf("expected compacted body: %q", secondSystem)
	}
}

func TestWorkingStateBatchGeneratePDFFailDoesNotOpenDiagnose(t *testing.T) {
	state := NewWorkingState("南京天气，生成pdf报告")
	var b workingStateBatch
	b.note(state, "generate_pdf", `{"query":"南京天气报告"}`, ToolExecutionOutcomeError)
	b.note(state, "generate_pdf", `{"content":"南京天气报告"}`, ToolExecutionOutcomeError)
	if inject := b.apply(state); inject != "" {
		t.Fatalf("generate_pdf intake fail must not inject: %q", inject)
	}
	if UnclosedOpenCount(state) != 0 || b.lastFail != nil {
		t.Fatalf("generate_pdf fail must not open diagnose: open=%+v lastFail=%+v", state.Open, b.lastFail)
	}
}

func TestWorkingStateBatchGeneratePDFSuccessDropsSiblingFail(t *testing.T) {
	state := NewWorkingState("南京天气，生成pdf报告")
	if err := AddOpen(state, OpenItem{Tool: "generate_pdf", Question: "error", SettleBy: "将改范围"}); err != nil {
		t.Fatal(err)
	}
	var b workingStateBatch
	b.note(state, "generate_pdf", `{"content":"南京天气报告","output_path":"n.pdf"}`, ToolExecutionOutcomeOK)
	b.note(state, "generate_pdf", `{"content":"南京今日天气：小雨","title":"南京天气报告"}`, ToolExecutionOutcomeError)
	if inject := b.apply(state); inject != "" {
		t.Fatalf("sibling generate fail must not inject: %q", inject)
	}
	if UnclosedOpenCount(state) != 0 {
		t.Fatalf("published generate_pdf must close diagnose opens: %+v", state.Open)
	}
}

func TestApplyThenSeekUserKeepsPriorFailOpen(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	b.note(state, "write_file", `{"path":"b.go","content":"y"}`, ToolExecutionOutcomeError)
	got := applyThenSeekUser(state, &b, "g", 2, "")
	if got == nil || got.LastAction != ActionSeekUser || got.Next != nextSeekUser() {
		t.Fatalf("pause: %+v", got)
	}
	if len(got.Settled) != 1 || got.Settled[0].Label != "a.go" {
		t.Fatalf("prior file ok must stay settled: %+v", got.Settled)
	}
	if UnclosedOpenCount(got) == 0 {
		t.Fatalf("prior file fail must still open: %+v", got.Open)
	}
	AdvanceWorkingStateAfterUserReply(got)
	if UnclosedOpenCount(got) == 0 {
		t.Fatalf("answer must not drop the fail open: %+v", got.Open)
	}
	if got.LastAction != "" || !strings.Contains(got.Next, "g") {
		t.Fatalf("after answer: action=%q next=%q", got.LastAction, got.Next)
	}
}

func TestWorkingStateBatchSettlesFocusOKBeforeLaterLiveEviction(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	b.note(state, "write_file", `{"path":"b.go","content":"y"}`, ToolExecutionOutcomeError)
	b.note(state, "write_file", `{"path":"c.go","content":"z"}`, ToolExecutionOutcomeError)
	_ = b.apply(state)
	found := false
	for _, s := range state.Settled {
		if s.Label == "a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("success evicted from Live must already be settled: live=%+v settled=%+v", state.Live, state.Settled)
	}
	ClearLiveAndOpen(state)
	found = false
	for _, s := range state.Settled {
		if s.Label == "a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steer dropped evicted success: %+v", state.Settled)
	}
}

func TestWorkingStateBatchBashOnlyOKStillTrustsOnApply(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeOK)
	if state.LastAction == ActionTrust {
		t.Fatal("bash ok must not trust until apply")
	}
	_ = b.apply(state)
	if state.LastAction != ActionTrust {
		t.Fatalf("LastAction=%q want trust", state.LastAction)
	}
	if !strings.Contains(state.Next, "bash") && !strings.Contains(state.Next, "g") {
		t.Fatalf("bash-only Next=%q", state.Next)
	}
}

func TestWorkingStateBatchKeepsFileNextAfterLaterBashOK(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	if !strings.Contains(state.Next, "a.go") {
		t.Fatalf("file ok Next=%q", state.Next)
	}
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeOK)
	_ = b.apply(state)
	if state.LastAction != ActionTrust {
		t.Fatalf("LastAction=%q", state.LastAction)
	}
	if !strings.Contains(state.Next, "a.go") {
		t.Fatalf("later bash ok rewrote Next off the file: %q", state.Next)
	}
	if strings.Contains(state.Next, "bash") {
		t.Fatalf("Next should keep the file, got %q", state.Next)
	}
}

func TestWorkingStateBatchClosesNonFocusOKWhenFileIsLast(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "error"}); err != nil {
		t.Fatal(err)
	}
	var b workingStateBatch
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeOK)
	b.note(state, "write_file", `{"path":"main.go","content":"x"}`, ToolExecutionOutcomeOK)
	_ = b.apply(state)
	if UnclosedOpenCount(state) != 0 {
		t.Fatalf("bash ok then file ok left opens: %+v", state.Open)
	}
	if len(state.Settled) != 1 || state.Settled[0].Label != "main.go" {
		t.Fatalf("file ok must still settle: %+v", state.Settled)
	}
}

func TestWorkingStateBatchSettlesEveryFocusOK(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	b.note(state, "write_file", `{"path":"b.go","content":"y"}`, ToolExecutionOutcomeOK)
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeError)
	_ = b.apply(state)
	if len(state.Settled) != 2 {
		t.Fatalf("both file oks must settle: %+v", state.Settled)
	}
	got := map[string]bool{}
	for _, s := range state.Settled {
		got[s.Label] = true
	}
	if !got["a.go"] || !got["b.go"] {
		t.Fatalf("settled labels: %+v", state.Settled)
	}
	if state.LastAction != ActionRetryDiagnose {
		t.Fatalf("LastAction=%q want retry_diagnose", state.LastAction)
	}
	ClearLiveAndOpen(state)
	if len(state.Settled) != 2 {
		t.Fatalf("steer dropped an earlier write: %+v", state.Settled)
	}
	got = map[string]bool{}
	for _, s := range state.Settled {
		got[s.Label] = true
	}
	if !got["a.go"] || !got["b.go"] {
		t.Fatalf("steer settled labels: %+v", state.Settled)
	}
}

func TestWorkingStateBatchSettlesFocusOKWhenBatchAlsoFails(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "write_file", `{"path":"main.go","content":"x"}`, ToolExecutionOutcomeOK)
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeError)
	if inject := b.apply(state); inject != "" {
		t.Fatalf("mixed batch should not inject: %q", inject)
	}
	if len(state.Settled) != 1 || state.Settled[0].Label != "main.go" {
		t.Fatalf("successful file tool must settle: %+v", state.Settled)
	}
	if state.LastAction != ActionRetryDiagnose {
		t.Fatalf("LastAction=%q want retry_diagnose", state.LastAction)
	}
	ClearLiveAndOpen(state)
	if len(state.Live) != 0 || len(state.Settled) != 1 || state.Settled[0].Label != "main.go" {
		t.Fatalf("steer must keep settled write: live=%+v settled=%+v", state.Live, state.Settled)
	}
}

func TestWorkingStateBatchApplyUsesLiveOpenCount(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "write_file", Question: "e1", SettleBy: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := AddOpen(state, OpenItem{Tool: "write_file", Question: "e2", SettleBy: "x"}); err != nil {
		t.Fatal(err)
	}
	var b workingStateBatch
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeError)
	if b.lastFail == nil || b.lastFail.OpenCount < 2 {
		t.Fatalf("note-time OpenCount should capture prior opens: %+v", b.lastFail)
	}
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	if UnclosedOpenCount(state) != 0 {
		t.Fatalf("file ok should close write_file opens: %+v", state.Open)
	}
	_ = b.apply(state)
	if state.LastAction == ActionSeekUser {
		t.Fatalf("stale OpenCount sought user after opens closed: action=%q open=%+v", state.LastAction, state.Open)
	}
	if state.LastAction != ActionRetryDiagnose {
		t.Fatalf("LastAction=%q want retry_diagnose", state.LastAction)
	}
}

func TestWorkingStateBatchApplyUsesLivePrev(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionEmpiric
	args := `{"command":"true"}`
	var b workingStateBatch
	b.note(state, "bash", args, ToolExecutionOutcomeError)
	b.note(state, "bash", args, ToolExecutionOutcomeError)
	if b.lastFail == nil || b.lastFail.Prev != ActionEmpiric || b.lastFail.SameSigCount != 2 {
		t.Fatalf("note-time Prev should stay empiric: %+v", b.lastFail)
	}
	b.note(state, "write_file", `{"path":"a.go","content":"x"}`, ToolExecutionOutcomeOK)
	if state.LastAction != ActionTrust {
		t.Fatalf("file ok LastAction=%q", state.LastAction)
	}
	_ = b.apply(state)
	if state.LastAction == ActionSeekUser {
		t.Fatalf("stale empiric Prev sought user after file ok: action=%q", state.LastAction)
	}
	if state.LastAction != ActionReroute {
		t.Fatalf("LastAction=%q want reroute", state.LastAction)
	}
}

func TestWorkingStateBatchApplyIsIdempotent(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeError)
	_ = b.apply(state)
	_ = b.apply(state)
	if UnclosedOpenCount(state) != 1 {
		t.Fatalf("second apply added another open: %+v", state.Open)
	}
}

func TestWorkingStateBatchSeekUserWhenOpensRemain(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "e1", SettleBy: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "e2", SettleBy: "x"}); err != nil {
		t.Fatal(err)
	}
	var b workingStateBatch
	b.note(state, "bash", `{"command":"true"}`, ToolExecutionOutcomeError)
	_ = b.apply(state)
	if state.LastAction != ActionSeekUser {
		t.Fatalf("two live opens + fail should seek: action=%q", state.LastAction)
	}
	if UnclosedOpenCount(state) != 2 {
		t.Fatalf("seek-on-open-cap must not add a third: %+v", state.Open)
	}
}

func TestWorkingStateBatchKeepsFailSigAfterLaterSuccess(t *testing.T) {
	state := NewWorkingState("g")
	var b workingStateBatch
	args := `{"command":"true"}`
	b.note(state, "bash", args, ToolExecutionOutcomeError)
	b.note(state, "bash", args, ToolExecutionOutcomeError)
	b.note(state, "bash", args, ToolExecutionOutcomeOK)
	if inject := b.apply(state); inject != "" {
		t.Fatalf("mixed batch should not inject yet: %q", inject)
	}
	if state.LastAction != ActionReroute {
		t.Fatalf("LastAction=%q want reroute", state.LastAction)
	}
	if state.SigCount != 2 {
		t.Fatalf("SigCount=%d after fail+fail+ok, want 2", state.SigCount)
	}
	var next workingStateBatch
	next.note(state, "bash", args, ToolExecutionOutcomeError)
	inject := next.apply(state)
	if state.LastAction != ActionSeekUser || state.SigCount != 3 {
		t.Fatalf("third fail should escalate: action=%q count=%d", state.LastAction, state.SigCount)
	}
	if !strings.Contains(inject, "禁止再次使用相同参数") {
		t.Fatalf("inject=%q", inject)
	}
}

func TestRunLoop_WorkingStateSameSigInject(t *testing.T) {
	var secondUser string
	callCount := 0
	args := `{"command":"true"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 2 {
			secondUser = lastUserContentFromRequest(body)
		}
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallsResponse(
				[2]string{"bash", args},
				[2]string{"bash", args},
				[2]string{"bash", args},
			)
		} else {
			resp = textResponse("need user")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     10,
		sysPrompt:   "sys",
		toolResult:  "fail",
		toolOutcome: ToolExecutionOutcomeError,
	}
	result := RunLoop(cb, "run it", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.LastAction != ActionSeekUser {
		t.Fatalf("state=%#v", result.WorkingState)
	}
	if !strings.Contains(secondUser, "禁止再次使用相同参数") {
		t.Fatalf("missing same-sig inject: %q", secondUser)
	}
}

func TestRunLoop_WorkingStateSameSigInjectSkipsLastIteration(t *testing.T) {
	args := `{"command":"true"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toolCallsResponse(
			[2]string{"bash", args},
			[2]string{"bash", args},
			[2]string{"bash", args},
		))
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     1,
		sysPrompt:   "sys",
		toolResult:  "fail",
		toolOutcome: ToolExecutionOutcomeError,
	}
	result := RunLoop(cb, "run it", nil, nil)
	if result.Error != "max iterations reached" {
		t.Fatalf("error=%q", result.Error)
	}
	if result.WorkingState == nil || result.WorkingState.LastAction != ActionSeekUser {
		t.Fatalf("state=%#v", result.WorkingState)
	}
	for _, entry := range result.HistoryDelta {
		if text, ok := entry.Content.(string); ok && strings.Contains(text, "禁止再次使用相同参数") {
			t.Fatalf("last-iter same-sig inject leaked into history: %q", text)
		}
	}
}

func TestRunLoop_WorkingStateEmptySkipsLastIterationInject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse(""))
	}))
	defer server.Close()
	cb := &workingStateGoalSourceCB{
		mockCallbacks: &mockCallbacks{
			config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:   1,
			sysPrompt: "sys",
		},
		goal: "horizon projected",
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "max iterations reached" {
		t.Fatalf("error=%q", result.Error)
	}
	for _, entry := range result.HistoryDelta {
		if text, ok := entry.Content.(string); ok && (strings.Contains(text, "下一步") || strings.Contains(text, "列出不超过3个候选")) {
			t.Fatalf("last-iter empty inject leaked: %q", text)
		}
	}
}

func toolCallResponse(name, args string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]interface{}{
						{
							"id":   "call_1",
							"type": "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": args,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

func textResponse(text string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
	}
}

func toolCallsResponse(calls ...[2]string) map[string]interface{} {
	tcs := make([]map[string]interface{}, 0, len(calls))
	for i, call := range calls {
		tcs = append(tcs, map[string]interface{}{
			"id":   "call_" + strconv.Itoa(i+1),
			"type": "function",
			"function": map[string]interface{}{
				"name":      call[0],
				"arguments": call[1],
			},
		})
	}
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":       "assistant",
					"content":    "",
					"tool_calls": tcs,
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

func lastUserContentFromRequest(body []byte) string {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	msgs, _ := payload["messages"].([]interface{})
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, _ := msgs[i].(map[string]interface{})
		if role, _ := msg["role"].(string); role == "user" {
			content, _ := msg["content"].(string)
			return content
		}
	}
	return ""
}

func systemContentFromRequest(body []byte) string {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	msgs, _ := payload["messages"].([]interface{})
	if len(msgs) == 0 {
		return ""
	}
	first, _ := msgs[0].(map[string]interface{})
	content, _ := first["content"].(string)
	return content
}
