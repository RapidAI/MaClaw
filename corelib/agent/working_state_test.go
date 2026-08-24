package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWorkingStateRenderEmpty(t *testing.T) {
	if got := RenderWorkingState(nil); got != "" {
		t.Fatalf("nil render = %q", got)
	}
	if got := RenderWorkingState(&WorkingState{}); got != "" {
		t.Fatalf("empty render = %q", got)
	}
}

func TestWorkingStateRenderOmitsEmptyFields(t *testing.T) {
	got := RenderWorkingState(&WorkingState{Goal: "修编译", Next: "读 main.go"})
	if !strings.HasPrefix(got, WorkingStateMarker+"\n") {
		t.Fatalf("missing marker: %q", got)
	}
	if !strings.Contains(got, "目标: 修编译") || !strings.Contains(got, "下一步: 读 main.go") {
		t.Fatalf("missing fields: %q", got)
	}
	if strings.Contains(got, "台上:") || strings.Contains(got, "未决:") || strings.Contains(got, "已证实:") || strings.Contains(got, "动作:") {
		t.Fatalf("empty fields should be omitted: %q", got)
	}
}

func TestWorkingStateRenderKeepsCoreUnderBudget(t *testing.T) {
	state := &WorkingState{
		Goal:       "目标必须留下",
		Next:       "下一步必须留下",
		LastAction: ActionTrust,
		Live: []FocusItem{{
			Label: strings.Repeat("L", 40),
			Fact:  strings.Repeat("F", 200),
		}},
		Settled: []Settled{{Claim: strings.Repeat("S", 200), Verifier: "v", Coverage: "c", Label: "x"}},
		Open:    []OpenItem{{Tool: "bash", Question: strings.Repeat("Q", 80)}},
	}
	got := RenderWorkingState(state)
	if utf8.RuneCountInString(got) > workingStateMaxRunes {
		t.Fatalf("render is %d runes, want <= %d\n%s", utf8.RuneCountInString(got), workingStateMaxRunes, got)
	}
	if !strings.Contains(got, "目标必须留下") || !strings.Contains(got, "下一步必须留下") || !strings.Contains(got, string(ActionTrust)) {
		t.Fatalf("core fields dropped: %q", got)
	}
}

func TestApplyWorkingStateSectionTwoMapTypes(t *testing.T) {
	state := &WorkingState{Goal: "G", Next: "N"}
	for _, conv := range [][]interface{}{
		{map[string]string{"role": "system", "content": "policy"}},
		{map[string]interface{}{"role": "system", "content": "policy"}},
	} {
		got := ApplyWorkingStateSection(conv, state, true)
		_, content, _ := systemPromptContent(got[0])
		if !strings.Contains(content, "policy") || !strings.Contains(content, WorkingStateMarker) || !strings.Contains(content, "目标: G") {
			t.Fatalf("splice failed: %q", content)
		}
		again := ApplyWorkingStateSection(got, state, true)
		_, content2, _ := systemPromptContent(again[0])
		if strings.Count(content2, WorkingStateMarker) != 1 {
			t.Fatalf("not idempotent: %q", content2)
		}
	}
}

func TestApplyWorkingStateSectionNoOpNonSystem(t *testing.T) {
	conv := []interface{}{map[string]string{"role": "user", "content": "hi"}}
	got := ApplyWorkingStateSection(conv, &WorkingState{Goal: "G"}, true)
	if got[0].(map[string]string)["content"] != "hi" {
		t.Fatalf("touched non-system: %#v", got[0])
	}
}

func TestApplyWorkingStateSectionStripsWhenDetached(t *testing.T) {
	conv := []interface{}{map[string]string{"role": "system", "content": "policy\n\n" + WorkingStateMarker + "\n目标: old"}}
	got := ApplyWorkingStateSection(conv, nil, false)
	_, content, _ := systemPromptContent(got[0])
	if strings.Contains(content, WorkingStateMarker) || strings.Contains(content, "old") {
		t.Fatalf("should strip: %q", content)
	}
	if content != "policy" {
		t.Fatalf("content = %q", content)
	}
}

func TestApplyWorkingStateSectionLastLineStartMarker(t *testing.T) {
	body := "see " + WorkingStateMarker + " in prose\npolicy\n" + WorkingStateMarker + "\n目标: first\n" + WorkingStateMarker + "\n目标: last"
	conv := []interface{}{map[string]string{"role": "system", "content": body}}
	got := ApplyWorkingStateSection(conv, &WorkingState{Goal: "new"}, true)
	_, content, _ := systemPromptContent(got[0])
	if !strings.Contains(content, "see "+WorkingStateMarker+" in prose") {
		t.Fatalf("inline marker stripped: %q", content)
	}
	if !strings.Contains(content, WorkingStateMarker+"\n目标: first") {
		t.Fatalf("earlier line-start section should remain: %q", content)
	}
	if strings.Count(content, WorkingStateMarker) != 3 {
		t.Fatalf("marker count = %d, content=%q", strings.Count(content, WorkingStateMarker), content)
	}
	if !strings.HasSuffix(strings.TrimSpace(content), "目标: new") {
		t.Fatalf("last section not replaced: %q", content)
	}
}

func TestExtractFocusAllowlist(t *testing.T) {
	item, ok := ExtractFocus("write_file", `{"path":"D:\\src\\main.go","content":"x"}`, ToolExecutionOutcomeOK)
	if !ok || !strings.Contains(item.Fact, "path=") || item.Label == "" {
		t.Fatalf("write_file focus = %+v ok=%v", item, ok)
	}
	if _, ok := ExtractFocus("list_directory", `{"path":"D:\\src"}`, ToolExecutionOutcomeOK); ok {
		t.Fatal("list_directory must not admit")
	}
	if _, ok := ExtractFocus("write_file", `{"content":"x"}`, ToolExecutionOutcomeOK); ok {
		t.Fatal("missing path must fail")
	}
	if _, ok := ExtractFocus("write_file", `{"content":"see \"path\": \"leaked.go\""}`, ToolExecutionOutcomeOK); ok {
		t.Fatal("path inside content must not admit")
	}
	item, ok = ExtractFocus("write_file", `{"path":"partial.go"`, ToolExecutionOutcomeOK)
	if !ok || item.Label != "partial.go" {
		t.Fatalf("truncated JSON should still find path: %+v ok=%v", item, ok)
	}
	item, ok = ExtractFocus("read_file", `{"pathology":"x","path":"main.go"}`, ToolExecutionOutcomeOK)
	if !ok || item.Label != "main.go" {
		t.Fatalf("exact path field lost next to pathology: %+v ok=%v", item, ok)
	}
	item, ok = ExtractFocus("write_file", `{"file_path":"alias.go","content":"x"}`, ToolExecutionOutcomeOK)
	if !ok || item.Label != "alias.go" {
		t.Fatalf("file_path alias: %+v ok=%v", item, ok)
	}
	if _, ok := ExtractFocus("opaque_grant", `{"path":"D:\\src\\main.go"}`, ToolExecutionOutcomeOK); ok {
		t.Fatal("semantic grant must not admit")
	}
	long := strings.Repeat("x", 120) + ".go"
	item, ok = ExtractFocus("read_file", `{"path":"`+long+`"}`, ToolExecutionOutcomeOK)
	if !ok || utf8.RuneCountInString(item.Fact) > workingStateFactMaxRunes {
		t.Fatalf("ok fact must clip: %+v ok=%v", item, ok)
	}
}

func TestNormalizeFocusLabelMergesSamePath(t *testing.T) {
	a := NormalizeFocusLabel(`D:\src\main.go`)
	b := NormalizeFocusLabel(`D:/src/main.go`)
	if a == "" || a != b {
		t.Fatalf("labels %q vs %q", a, b)
	}
}

func TestAdmitLiveRules(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitLive(state, FocusItem{Label: "a", Fact: ""}); err == nil {
		t.Fatal("empty fact should fail")
	}
	if err := AdmitLive(state, FocusItem{Label: "a", Fact: "fa"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLive(state, FocusItem{Label: "b", Fact: "fb"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLive(state, FocusItem{Label: "c", Fact: "fc"}); err != errLiveFull {
		t.Fatalf("full = %v", err)
	}
	if err := AdmitLive(state, FocusItem{Label: "a", Fact: "fa2"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Live) != 2 || state.Live[0].Fact != "fa2" {
		t.Fatalf("same label should update: %+v", state.Live)
	}
	if !strings.Contains(state.Next, "a") {
		t.Fatalf("next should name label: %q", state.Next)
	}
}

func TestAdmitLiveEvictOldestNamesOldest(t *testing.T) {
	state := NewWorkingState("g")
	_ = AdmitLive(state, FocusItem{Label: "old", Fact: "1"})
	_ = AdmitLive(state, FocusItem{Label: "keep", Fact: "2"})
	if err := AdmitLiveEvictOldest(state, FocusItem{Label: "new", Fact: "3"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Live) != 2 || state.Live[0].Label != "keep" || state.Live[1].Label != "new" {
		t.Fatalf("evict oldest: %+v", state.Live)
	}
	if err := SwapLive(state, "", FocusItem{Label: "x", Fact: "y"}); err != errSwapUnnamed {
		t.Fatalf("unnamed swap = %v", err)
	}
}

func TestSwapLiveRestoresOutgoingWhenIncomingRejected(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitLive(state, FocusItem{Label: "a.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLive(state, FocusItem{Label: "b.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := SwapLive(state, "a.go", FocusItem{Label: "c.go", Fact: ""}); err != errFocusRequired {
		t.Fatalf("err=%v", err)
	}
	if len(state.Live) != 2 || state.Live[0].Label != "a.go" || state.Live[1].Label != "b.go" {
		t.Fatalf("rejected swap punched Live: %+v", state.Live)
	}
}

func TestAdmitSettledRequiresLiveAndCoverage(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitSettled(state, Settled{Label: "p", Claim: "c", Verifier: "v", Coverage: "cov"}); err != errPremiseMissing {
		t.Fatalf("missing live = %v", err)
	}
	_ = AdmitLive(state, FocusItem{Label: "p", Fact: "f"})
	if err := AdmitSettled(state, Settled{Label: "p", Claim: "c", Verifier: "v", Coverage: ""}); err != errSettledIncomplete {
		t.Fatalf("missing coverage = %v", err)
	}
	if err := AdmitSettled(state, Settled{Label: "p", Claim: "c", Verifier: "v", Coverage: "cov"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "p", Claim: "p-updated", Verifier: "v2", Coverage: "cov2"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Settled) != 1 || state.Settled[0].Claim != "p-updated" || state.Settled[0].ID == "" {
		t.Fatalf("same label must update in place: %+v", state.Settled)
	}
}

func TestAdmitSettledRefreshKeepsRecent(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitLive(state, FocusItem{Label: "a.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLive(state, FocusItem{Label: "b.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "a.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "b.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "a.go", Claim: "a-again", Verifier: "v2", Coverage: "c2"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLiveEvictOldest(state, FocusItem{Label: "c.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "c.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Settled) != 2 || state.Settled[0].Label != "a.go" || state.Settled[1].Label != "c.go" {
		t.Fatalf("refreshed a.go must outrank stale b.go: %+v", state.Settled)
	}
}

func TestSelectActionTable(t *testing.T) {
	cases := []struct {
		name string
		sig  RoundSignal
		want ControlAction
	}{
		{"ok", RoundSignal{Kind: RoundToolOK}, ActionTrust},
		{"fail1", RoundSignal{Kind: RoundToolError, SameSigCount: 1}, ActionRetryDiagnose},
		{"fail2", RoundSignal{Kind: RoundToolError, SameSigCount: 2}, ActionReroute},
		{"fail3", RoundSignal{Kind: RoundToolError, SameSigCount: 3}, ActionSeekUser},
		{"empiric same", RoundSignal{Kind: RoundToolError, Prev: ActionEmpiric, SameSigCount: 2}, ActionSeekUser},
		{"open full", RoundSignal{Kind: RoundToolError, SameSigCount: 1, OpenCount: 2}, ActionSeekUser},
		{"empty1", RoundSignal{Kind: RoundLLMEmpty, EmptyCount: 1}, ActionEmpiric},
		{"empty2", RoundSignal{Kind: RoundLLMEmpty, EmptyCount: 2}, ActionSeekUser},
	}
	for _, tc := range cases {
		got, err := SelectAction(tc.sig)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q err=%v want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestAddOpenCapsUnclosed(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "c"}); err != nil {
		t.Fatal(err)
	}
	if UnclosedOpenCount(state) != workingStateMaxOpen {
		t.Fatalf("unclosed=%d want %d", UnclosedOpenCount(state), workingStateMaxOpen)
	}
}

func TestAddOpenRejectedOnTrust(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionTrust
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "x"}); err != errOpenOnTrust {
		t.Fatalf("trust open = %v", err)
	}
}

func TestApplyControlActionTrustSettlesOnlyFocusTool(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitLive(state, FocusItem{Label: "main.go", Fact: "path=main.go；结果=ok"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyControlAction(state, ActionTrust, RoundSignal{Kind: RoundToolOK, ToolName: "bash"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Settled) != 0 {
		t.Fatalf("bash trust must not settle unrelated live: %+v", state.Settled)
	}
	if strings.Contains(state.Next, "main.go") {
		t.Fatalf("bash trust Next must not point at leftover live: %q", state.Next)
	}
	if err := ApplyControlAction(state, ActionTrust, RoundSignal{Kind: RoundToolOK, ToolName: "write_file", FocusLabel: "main.go"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Settled) != 1 || state.Settled[0].Label != "main.go" {
		t.Fatalf("focus-tool trust should settle: %+v", state.Settled)
	}
}

func TestCloseOpenOnTrustWithoutSettled(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "error"}); err != nil {
		t.Fatal(err)
	}
	CloseOpenOnTrust(state, "bash", "")
	if UnclosedOpenCount(state) != 0 || state.Open[0].ClosedBy != "trust:bash" {
		t.Fatalf("close = %+v", state.Open)
	}
}

func TestShouldAttachAndBlockFinish(t *testing.T) {
	if ShouldAttachWorkingState(PromptProfileLight, false, &WorkingState{Goal: "g"}) {
		t.Fatal("light must not attach")
	}
	if ShouldAttachWorkingState(PromptProfileFull, true, &WorkingState{Goal: "g"}) {
		t.Fatal("env off must not attach")
	}
	if ShouldAttachWorkingState(PromptProfileFull, false, nil) {
		t.Fatal("nil state must not attach")
	}
	if !ShouldAttachWorkingState(PromptProfileFull, false, &WorkingState{Goal: "g"}) {
		t.Fatal("full + state should attach")
	}

	state := &WorkingState{Open: []OpenItem{{Tool: "bash", Question: "e"}}}
	if !ShouldBlockFinish(state, "继续", true) {
		t.Fatal("open should block once")
	}
	state.FinishNudges = 1
	if ShouldBlockFinish(state, "继续", true) {
		t.Fatal("nudged should pass")
	}
	state.FinishNudges = 0
	if ShouldBlockFinish(state, "就这样", true) {
		t.Fatal("allowlist should pass")
	}
	if ShouldBlockFinish(&WorkingState{}, "继续", true) {
		t.Fatal("no open should pass")
	}
	if ShouldBlockFinish(state, "继续", false) {
		t.Fatal("detached should pass")
	}
}

func TestClearLiveAndOpenResetsNextOffEvictedLive(t *testing.T) {
	state := NewWorkingState("修编译")
	if err := AdmitLive(state, FocusItem{Label: "main.go", Fact: "path=main.go；结果=ok"}); err != nil {
		t.Fatal(err)
	}
	state.LastAction = ActionRetryDiagnose
	state.FinishNudges = 1
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "error"}); err != nil {
		t.Fatal(err)
	}
	ClearLiveAndOpen(state)
	if len(state.Live) != 0 || UnclosedOpenCount(state) != 0 {
		t.Fatalf("live/open = %+v / %+v", state.Live, state.Open)
	}
	if state.Goal != "修编译" {
		t.Fatalf("goal changed: %q", state.Goal)
	}
	if strings.Contains(state.Next, "main.go") {
		t.Fatalf("Next still names evicted live: %q", state.Next)
	}
	if state.LastAction != "" {
		t.Fatalf("LastAction still %q after steer", state.LastAction)
	}
	if state.FinishNudges != 0 {
		t.Fatalf("FinishNudges still %d after steer", state.FinishNudges)
	}
}

func TestAccountToolSignatureOnlyIncrementsOnFailure(t *testing.T) {
	state := NewWorkingState("g")
	if n := AccountToolSignature(state, "read_file", `{"path":"a"}`, true); n != 1 {
		t.Fatalf("first fail = %d", n)
	}
	if n := AccountToolSignature(state, "read_file", `{"path":"a"}`, true); n != 2 {
		t.Fatalf("same fail = %d", n)
	}
	if n := AccountToolSignature(state, "read_file", `{"path":"a"}`, false); n != 0 {
		t.Fatalf("ok should reset = %d", n)
	}
}

func TestGoalFromUserText(t *testing.T) {
	if got := GoalFromUserText("修编译错误。然后提交"); got != "修编译错误" {
		t.Fatalf("got %q", got)
	}
	if got := GoalFromUserText(""); got != "当前任务" {
		t.Fatalf("empty = %q", got)
	}
	if got := GoalFromUserText("fix main.go then compile"); got != "fix main.go then compile" {
		t.Fatalf("filename split: %q", got)
	}
	if got := GoalFromUserText("Fix it. Then run"); got != "Fix it" {
		t.Fatalf("english sentence: %q", got)
	}
	if got := GoalFromUserText("修 main.go"); got != "修 main.go" {
		t.Fatalf("cjk filename: %q", got)
	}
}

func TestStripWorkingStateFromVisible(t *testing.T) {
	leaked := "hello\n" + WorkingStateMarker + "\n目标: leaked"
	got := StripWorkingStateFromVisible(leaked)
	if strings.Contains(got, WorkingStateMarker) || strings.Contains(got, "leaked") {
		t.Fatalf("section remained: %q", got)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
	inline := "see " + WorkingStateMarker + " inline"
	if StripWorkingStateFromVisible(inline) != inline {
		t.Fatalf("inline changed: %q", StripWorkingStateFromVisible(inline))
	}
}

func TestSanitizeLoopResultVisibleStripsMultimodalHistory(t *testing.T) {
	leaked := "answer\n" + WorkingStateMarker + "\n目标: leaked"
	r := &LoopResult{
		Text: leaked,
		HistoryDelta: []ConversationEntry{
			{Role: "assistant", Content: leaked, ReasoningContent: leaked},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": leaked},
			}},
			{Role: "assistant", Content: []map[string]interface{}{
				{"type": "text", "text": leaked},
			}},
		},
	}
	sanitizeLoopResultVisible(r)
	if strings.Contains(r.Text, WorkingStateMarker) {
		t.Fatalf("Text leaked: %q", r.Text)
	}
	if s, ok := r.HistoryDelta[0].Content.(string); !ok || strings.Contains(s, WorkingStateMarker) {
		t.Fatalf("string HistoryDelta leaked: %#v", r.HistoryDelta[0].Content)
	}
	if strings.Contains(r.HistoryDelta[0].ReasoningContent, WorkingStateMarker) {
		t.Fatalf("ReasoningContent leaked: %q", r.HistoryDelta[0].ReasoningContent)
	}
	blocks, ok := r.HistoryDelta[1].Content.([]interface{})
	if !ok || len(blocks) != 1 {
		t.Fatalf("multimodal HistoryDelta: %#v", r.HistoryDelta[1].Content)
	}
	block, _ := blocks[0].(map[string]interface{})
	text, _ := block["text"].(string)
	if strings.Contains(text, WorkingStateMarker) || !strings.Contains(text, "answer") {
		t.Fatalf("multimodal text: %q", text)
	}
	typed, ok := r.HistoryDelta[2].Content.([]map[string]interface{})
	if !ok || len(typed) != 1 {
		t.Fatalf("typed multimodal HistoryDelta: %#v", r.HistoryDelta[2].Content)
	}
	if t2, _ := typed[0]["text"].(string); strings.Contains(t2, WorkingStateMarker) || !strings.Contains(t2, "answer") {
		t.Fatalf("typed multimodal text: %q", t2)
	}
}

func TestSanitizeLoopResultVisibleKeepsToolFileBody(t *testing.T) {
	body := "intro\n" + WorkingStateMarker + "\n目标: keep this file body"
	r := &LoopResult{
		Text: "done",
		HistoryDelta: []ConversationEntry{
			{Role: "tool", ToolName: "read_file", Content: body},
			{Role: "assistant", Content: "see\n" + WorkingStateMarker + "\n目标: leaked"},
		},
	}
	sanitizeLoopResultVisible(r)
	got, _ := r.HistoryDelta[0].Content.(string)
	if got != body {
		t.Fatalf("tool body was stripped: %q", got)
	}
	if s, _ := r.HistoryDelta[1].Content.(string); strings.Contains(s, WorkingStateMarker) {
		t.Fatalf("assistant still leaked: %q", s)
	}
}

func TestPromptWorkingStateContractHasNoMarker(t *testing.T) {
	if strings.Contains(PromptWorkingStateContract, WorkingStateMarker) {
		t.Fatal("contract must not include the splice marker")
	}
}

func TestNewWorkingStateClipsGoal(t *testing.T) {
	long := strings.Repeat("目", workingStateGoalMaxRunes+10)
	got := NewWorkingState(long)
	if utf8.RuneCountInString(got.Goal) != workingStateGoalMaxRunes {
		t.Fatalf("goal runes=%d", utf8.RuneCountInString(got.Goal))
	}
}

func TestAppendNextHintDoesNotCopySection(t *testing.T) {
	state := &WorkingState{Goal: "G", Next: "读 main.go", LastAction: ActionTrust}
	got := AppendNextHint(FinishNudgeMessage(), state)
	if strings.Contains(got, WorkingStateMarker) || strings.Contains(got, "目标:") {
		t.Fatalf("inject copied section: %q", got)
	}
	if !strings.Contains(got, "下一步：读 main.go") {
		t.Fatalf("missing next: %q", got)
	}
}

func TestEnsureWorkingState(t *testing.T) {
	if EnsureWorkingState(nil, "hi", 0, "") != nil {
		t.Fatal("no tools no projection")
	}
	if got := EnsureWorkingState(nil, "修它。详述", 1, ""); got == nil || got.Goal != "修它" {
		t.Fatalf("after tool: %+v", got)
	}
	if got := EnsureWorkingState(nil, "hi", 0, "horizon"); got == nil || got.Goal != "horizon" {
		t.Fatalf("projection: %+v", got)
	}
	if got := EnsureWorkingState(nil, "user answer", 1, "horizon"); got == nil || got.Goal != "horizon" {
		t.Fatalf("projection must win over tool userText: %+v", got)
	}
	existing := NewWorkingState("keep")
	if got := EnsureWorkingState(existing, "other", 3, "proj"); got.Goal != "keep" {
		t.Fatalf("existing overwritten: %+v", got)
	}
}

func TestApplyWorkingStateEmptyUsesProjection(t *testing.T) {
	got, _ := applyWorkingStateEmpty(nil, "hi", "", 1, 0, "horizon goal")
	if got == nil || got.Goal != "horizon goal" {
		t.Fatalf("empty first round must use projection: %+v", got)
	}
}

func TestMaybeMarkSeekUserSetsNext(t *testing.T) {
	state := NewWorkingState("g")
	state.Next = "按 main.go 继续"
	got := maybeMarkSeekUser(state, "g", 1, "")
	if got == nil || got.LastAction != ActionSeekUser || got.Next != nextSeekUser() {
		t.Fatalf("seek_user Next stale: %+v", got)
	}
}

func TestAdvanceWorkingStateAfterUserReply(t *testing.T) {
	state := NewWorkingState("fix compile")
	state.LastAction = ActionSeekUser
	state.Next = nextSeekUser()
	AdvanceWorkingStateAfterUserReply(state)
	if state.LastAction != "" {
		t.Fatalf("LastAction=%q", state.LastAction)
	}
	if state.Next != nextContinue(state, "", "") {
		t.Fatalf("Next=%q", state.Next)
	}
	AdvanceWorkingStateAfterUserReply(nil)
	state.LastAction = ActionTrust
	state.Next = "keep"
	AdvanceWorkingStateAfterUserReply(state)
	if state.LastAction != ActionTrust || state.Next != "keep" {
		t.Fatalf("non-seek mutated: %+v", state)
	}
}

func TestAdvanceWorkingStateAfterUserReplyClosesSeekOpenOnly(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionRetryDiagnose
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "error", SettleBy: "将改范围"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyControlAction(state, ActionSeekUser, RoundSignal{Kind: RoundToolError, ToolName: "bash", SameSigCount: 3, OpenCount: 1}); err != nil {
		t.Fatal(err)
	}
	if UnclosedOpenCount(state) != 2 {
		t.Fatalf("setup opens: %+v", state.Open)
	}
	AdvanceWorkingStateAfterUserReply(state)
	if UnclosedOpenCount(state) != 1 {
		t.Fatalf("seek open should close, fail open stay: %+v", state.Open)
	}
	if state.Open[0].ClosedBy != "" || state.Open[0].SettleBy != "将改范围" {
		t.Fatalf("fail open mutated: %+v", state.Open[0])
	}
	if state.Open[1].ClosedBy != "user-reply" {
		t.Fatalf("seek open ClosedBy=%q", state.Open[1].ClosedBy)
	}
}

func TestAdvanceWorkingStateAfterUserReplyResetsFinishNudges(t *testing.T) {
	state := NewWorkingState("g")
	state.LastAction = ActionSeekUser
	state.FinishNudges = 1
	if err := AddOpen(state, OpenItem{Tool: "bash", Question: "error", SettleBy: "将改范围"}); err != nil {
		t.Fatal(err)
	}
	AdvanceWorkingStateAfterUserReply(state)
	if state.FinishNudges != 0 {
		t.Fatalf("FinishNudges=%d after answer", state.FinishNudges)
	}
	if !ShouldBlockFinish(state, "继续", true) {
		t.Fatal("resumed turn must still get one done-check for the fail open")
	}
}

func TestWorkingStateRenderShowsAllSettled(t *testing.T) {
	state := NewWorkingState("g")
	state.Settled = []Settled{
		{Label: "a.go", Claim: "a.go", Verifier: "v", Coverage: "c"},
		{Label: "b.go", Claim: "b.go", Verifier: "v", Coverage: "c"},
	}
	got := RenderWorkingState(state)
	if !strings.Contains(got, "- a.go") || !strings.Contains(got, "- b.go") {
		t.Fatalf("both settled must render: %q", got)
	}
}

func TestAdmitSettledIDsStayUniqueAfterCap(t *testing.T) {
	state := NewWorkingState("g")
	if err := AdmitLive(state, FocusItem{Label: "a.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLive(state, FocusItem{Label: "b.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "a.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "b.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLiveEvictOldest(state, FocusItem{Label: "c.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "c.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitLiveEvictOldest(state, FocusItem{Label: "d.go", Fact: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitSettled(state, Settled{Label: "d.go", Verifier: "v", Coverage: "c"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Settled) != workingStateMaxSettled {
		t.Fatalf("settled=%+v", state.Settled)
	}
	if state.Settled[0].ID == "" || state.Settled[0].ID == state.Settled[1].ID {
		t.Fatalf("duplicate settled IDs after cap: %+v", state.Settled)
	}
}
