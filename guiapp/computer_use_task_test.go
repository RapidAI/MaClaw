package guiapp

import (
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func seedComputerUseObserve(t *testing.T, ocrText, elementName string) {
	t.Helper()
	els := []taskengine.UIElement{}
	if elementName != "" {
		els = append(els, taskengine.UIElement{
			Type: "button", Name: elementName, BBox: [4]int{1, 1, 20, 20}, Interactable: true,
		})
	}
	ocr := []taskengine.OCRResult{}
	if ocrText != "" {
		ocr = append(ocr, taskengine.OCRResult{Text: ocrText, Confidence: 1, BBox: [4]int{1, 1, 20, 20}})
	}
	cuSession().CommitObserve(computeruse.ScreenMeta{Width: 100, Height: 100, ScaleFactor: 1}, nil, els, ocr, "")
}

func lastComputerUseAction(t *testing.T) computeruse.ActionRecord {
	t.Helper()
	audit := cuSession().Audit()
	if len(audit) == 0 {
		t.Fatal("expected action audit")
	}
	return audit[len(audit)-1]
}

func TestComputerUseOwnerFromLoopPrefersSessionKey(t *testing.T) {
	ctx := &LoopContext{
		UserID: "desktop-user",
		Runtime: RuntimeContext{
			RequestID:    "req-1",
			Conversation: RuntimeConversationRef{SessionKey: "im:desktop:desktop-user:actor"},
		},
	}
	if got := computerUseOwnerFromLoop(ctx, "desktop-user"); got != "im:desktop:desktop-user:actor" {
		t.Fatalf("owner=%q", got)
	}
}

func TestComputerUseAuditCorpusIncludesOCRElementsAndText(t *testing.T) {
	obs := &computeruse.ObserveResult{
		OCRExcerpt:   "ocr-only-token",
		TextForModel: "perception=llm_vision",
		Elements:     []computeruse.MarkedElement{{Name: "UniqueSaveAsDialog", Value: "value-token"}},
	}
	got := computerUseAuditCorpus(obs)
	for _, want := range []string{"ocr-only-token", "perception=llm_vision", "UniqueSaveAsDialog", "value-token"} {
		if !strings.Contains(got, want) {
			t.Fatalf("corpus missing %q: %q", want, got)
		}
	}
}

func TestComputerUseProbeDigestKeepsElementsAheadOfLongText(t *testing.T) {
	obs := &computeruse.ObserveResult{
		OCRExcerpt:   "ocr-keep-token",
		TextForModel: strings.Repeat("x", 8000),
		Elements:     []computeruse.MarkedElement{{Name: "UniqueSaveAsDialog"}},
	}
	raw := computerUseProbeDigest(obs)
	head := raw
	if len(head) > 200 {
		head = head[:200]
	}
	if !strings.Contains(head, "ocr-keep-token") || !strings.Contains(head, "UniqueSaveAsDialog") {
		t.Fatalf("OCR/elements must lead probe digest: %q", head)
	}
	clipped := longhorizon.Clip(raw, 4000)
	if !strings.Contains(clipped, "ocr-keep-token") || !strings.Contains(clipped, "UniqueSaveAsDialog") {
		t.Fatal("4000-rune probe clip must keep OCR and element names")
	}
}

func TestComputerUseProbeDigestKeepsTextWhenManyElements(t *testing.T) {
	els := make([]computeruse.MarkedElement, 400)
	for i := range els {
		els[i] = computeruse.MarkedElement{Name: "ElName" + strings.Repeat("Z", 40)}
	}
	obs := &computeruse.ObserveResult{
		OCRExcerpt:   "ocr-keep-token",
		TextForModel: "perception=llm_vision",
		Elements:     els,
	}
	clipped := longhorizon.Clip(computerUseProbeDigest(obs), 4000)
	if !strings.Contains(clipped, "ocr-keep-token") {
		t.Fatal("huge element list must not drop OCR")
	}
	if !strings.Contains(clipped, "perception=llm_vision") {
		t.Fatal("huge element list must not drop TextForModel")
	}
}

func TestComputerUseProbeDigestKeepsElementsWhenOCRIsHuge(t *testing.T) {
	obs := &computeruse.ObserveResult{
		OCRExcerpt:   strings.Repeat("o", 8000),
		TextForModel: "perception=llm_vision",
		Elements:     []computeruse.MarkedElement{{Name: "UniqueSaveAsDialog"}},
	}
	clipped := longhorizon.Clip(computerUseProbeDigest(obs), 4000)
	if !strings.Contains(clipped, "UniqueSaveAsDialog") {
		t.Fatal("huge OCR must not drop element names")
	}
	if !strings.Contains(clipped, "perception=llm_vision") {
		t.Fatal("huge OCR must not drop TextForModel")
	}
}

func TestComputerUseProbeDigestSkipsHugeElementKeepsLater(t *testing.T) {
	obs := &computeruse.ObserveResult{
		OCRExcerpt: "ocr-keep-token",
		Elements: []computeruse.MarkedElement{
			{Name: strings.Repeat("H", 2000)},
			{Name: "LaterSaveButton"},
		},
	}
	got := computerUseProbeDigest(obs)
	if !strings.Contains(got, "LaterSaveButton") {
		t.Fatalf("later compact element must survive a huge earlier name: %q", got)
	}
	if strings.Contains(got, strings.Repeat("H", 2000)) {
		t.Fatal("oversized element name must not consume the probe budget")
	}
}

func TestComputerUseProbeDigestClipsHugeElementWhenAlone(t *testing.T) {
	token := "UniqueHugeWindowTitle"
	obs := &computeruse.ObserveResult{
		OCRExcerpt: "ocr-keep-token",
		Elements:   []computeruse.MarkedElement{{Name: token + strings.Repeat("H", 2000)}},
	}
	got := computerUseProbeDigest(obs)
	if !strings.Contains(got, token) {
		t.Fatalf("huge-only element must still contribute a prefix: %q", got)
	}
	obs.Elements = []computeruse.MarkedElement{{Name: strings.Repeat("H", 2000) + token}}
	got = computerUseProbeDigest(obs)
	if !strings.Contains(got, token) {
		t.Fatalf("huge-only element must keep a tail token: %q", got)
	}
}

func TestComputerUseProbeDigestKeepsOCRTailToken(t *testing.T) {
	token := "UniqueOCRTailToken"
	obs := &computeruse.ObserveResult{
		OCRExcerpt: strings.Repeat("o", 2000) + token,
		Elements:   []computeruse.MarkedElement{{Name: "LaterSaveButton"}},
	}
	got := computerUseProbeDigest(obs)
	if !strings.Contains(got, token) {
		t.Fatalf("OCR tail token must survive head-tail clip: %q", got)
	}
	if !strings.Contains(got, "LaterSaveButton") {
		t.Fatalf("compact element must still follow clipped OCR: %q", got)
	}
}

func TestComputerUseProbeDigestSamplesHugeElementAndKeepsLater(t *testing.T) {
	token := "UniqueHugeWindowTitle"
	obs := &computeruse.ObserveResult{
		OCRExcerpt: "ocr-keep-token",
		Elements: []computeruse.MarkedElement{
			{Name: strings.Repeat("H", 2000) + token},
			{Name: "LaterSaveButton"},
		},
	}
	got := computerUseProbeDigest(obs)
	if !strings.Contains(got, token) {
		t.Fatalf("huge window title tail must be sampled: %q", got)
	}
	if !strings.Contains(got, "LaterSaveButton") {
		t.Fatalf("later compact element must survive huge-title sample: %q", got)
	}
	if strings.Contains(got, strings.Repeat("H", 2000)) {
		t.Fatal("oversized element name must not consume the probe budget")
	}
}

func TestComputerUseDoneEmptyAcceptanceMatchesToday(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-empty")
	markComputerUseSessionActive()
	beginComputerUseTask("sk-empty", "req-1", "看看屏幕", nil)

	got := cuHandleDone("all good")
	if !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("got %q", got)
	}
	st := computerUseTaskStateFor("sk-empty")
	if st == nil || st.LastAudit != computerUseAuditSkipped {
		t.Fatalf("LastAudit=%v", st)
	}
	if last := lastComputerUseAction(t); !last.OK || last.Action != "done" {
		t.Fatalf("action=%+v", last)
	}
	if computerUseSessionActive() {
		t.Fatal("empty-contract done must clear sticky")
	}
}

func TestComputerUseDoneRejectsUnmetAcceptance(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	markComputerUseSessionActive()
	setComputerUseOwner("sk-fail")
	beginComputerUseTask("sk-fail", "req-1", "保存文档", []string{"已保存"})
	seedComputerUseObserve(t, "草稿未保存", "")

	got := cuHandleDone("done")
	if !strings.Contains(got, "computer_done rejected") || !strings.Contains(got, "computer_observe") {
		t.Fatalf("got %q", got)
	}
	st := computerUseTaskStateFor("sk-fail")
	if st == nil || st.LastAudit != computerUseAuditFailed || st.FailedDone != 1 {
		t.Fatalf("state=%+v", st)
	}
	if last := lastComputerUseAction(t); last.OK {
		t.Fatal("failed done must RecordAction ok=false")
	}
	if !computerUseSessionActive() {
		t.Fatal("rejected done must keep sticky")
	}
}

func computerUseControlActions(t *testing.T) *[]string {
	t.Helper()
	prev := computerUseEventEmitter
	actions := []string{}
	computerUseEventEmitter = func(name string, data interface{}) {
		if name != EventComputerUseControl {
			return
		}
		payload, _ := data.(map[string]interface{})
		action, _ := payload["action"].(string)
		if action != "" {
			actions = append(actions, action)
		}
	}
	t.Cleanup(func() { computerUseEventEmitter = prev })
	return &actions
}

func TestTerminalDoneKeepsAFailedAudit(t *testing.T) {
	t.Run("input denied", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		markComputerUseSessionActive()
		setComputerUseOwner("sk-denied")
		beginComputerUseTask("sk-denied", "req-1", "保存文档", []string{"已保存"})
		seedComputerUseObserve(t, "草稿未保存", "")
		sess := cuSession()
		sess.NoteInputDenied()
		actions := computerUseControlActions(t)
		text, ok := cuDoneResult("could not type")
		if ok || strings.Contains(text, "computer_observe") || !strings.Contains(text, "refused input") || !strings.Contains(text, "Do not observe again.") {
			t.Fatalf("ok=%v text=%q", ok, text)
		}
		if !sess.InputDenied() {
			t.Fatal("refusal cleared the latch")
		}
		st := computerUseTaskStateFor("sk-denied")
		if st == nil || st.LastAudit != computerUseAuditFailed || st.FailedDone != 1 || len(st.Acceptance) == 0 {
			t.Fatalf("state=%+v", st)
		}
		if !computerUseSessionActive() {
			t.Fatal("refusal cleared the session")
		}
		if len(*actions) != 0 {
			t.Fatalf("refusal emitted %v", *actions)
		}
	})
	t.Run("steps exhausted", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		markComputerUseSessionActive()
		setComputerUseOwner("sk-spent")
		beginComputerUseTask("sk-spent", "req-1", "保存文档", []string{"已保存"})
		seedComputerUseObserve(t, "草稿未保存", "")
		sess := cuSession()
		sess.Policy().MaxSteps = 1
		if err := sess.BeginAction("click", "spend"); err != nil {
			t.Fatal(err)
		}
		if err := sess.BeginAction("click", "over"); err == nil {
			t.Fatal("expected the budget to be spent")
		}
		actions := computerUseControlActions(t)
		text, ok := cuDoneResult("could not finish")
		if ok || strings.Contains(text, "computer_observe") || !strings.Contains(text, "max steps exceeded (1)") {
			t.Fatalf("ok=%v text=%q", ok, text)
		}
		st := computerUseTaskStateFor("sk-spent")
		if st == nil || st.LastAudit != computerUseAuditFailed || len(st.Acceptance) == 0 {
			t.Fatalf("state=%+v", st)
		}
		if !computerUseSessionActive() {
			t.Fatal("refusal cleared the session")
		}
		if len(*actions) != 0 {
			t.Fatalf("refusal emitted %v", *actions)
		}
	})
	t.Run("audit already satisfied", func(t *testing.T) {
		resetComputerUseRuntimeForTest(t)
		resetComputerUseSessionForTest(t)
		setComputerUseOwner("sk-met")
		markComputerUseSessionActive()
		beginComputerUseTask("sk-met", "req-1", "保存文档", []string{"已保存"})
		seedComputerUseObserve(t, "文件已保存", "")
		sess := cuSession()
		sess.Policy().MaxSteps = 1
		if err := sess.BeginAction("click", "spend"); err != nil {
			t.Fatal(err)
		}
		if err := sess.BeginAction("click", "over"); err == nil {
			t.Fatal("expected the budget to be spent")
		}
		sess.NoteInputDenied()
		actions := computerUseControlActions(t)
		text, ok := cuDoneResult("saved")
		if !ok || !strings.HasPrefix(text, "computer_done:") || strings.Contains(text, "rejected") {
			t.Fatalf("ok=%v text=%q", ok, text)
		}
		if !sess.InputDenied() {
			t.Fatal("a satisfied completion cleared the latch")
		}
		if _, exhausted := sess.StepsExhausted(); !exhausted {
			t.Fatal("a satisfied completion reset the step budget")
		}
		st := computerUseTaskStateFor("sk-met")
		if st == nil || st.LastAudit != computerUseAuditPassed || len(st.Acceptance) != 0 {
			t.Fatalf("state=%+v", st)
		}
		if computerUseSessionActive() {
			t.Fatal("a satisfied completion left the session active")
		}
		if len(*actions) != 1 || (*actions)[0] != "done" {
			t.Fatalf("control=%v", *actions)
		}
	})
}

func TestBeginComputerUseTaskStartsAFreshAttempt(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-fresh")
	sess := cuSession()
	sess.Policy().MaxSteps = 1
	if err := sess.BeginAction("click", "spend"); err != nil {
		t.Fatal(err)
	}
	if err := sess.BeginAction("click", "over"); err == nil {
		t.Fatal("expected the budget to be spent")
	}
	sess.NoteInputDenied()
	sess.Pause()
	beginComputerUseTask("sk-fresh", "req-2", "下一句", nil)
	if sess.InputDenied() {
		t.Fatal("a new task kept the input denial")
	}
	if sess.Policy().StepCount() != 0 {
		t.Fatalf("steps=%d", sess.Policy().StepCount())
	}
	if _, exhausted := sess.StepsExhausted(); exhausted {
		t.Fatal("a new task kept the spent step budget")
	}
	paused, stopped := sess.ControlState()
	if !paused || stopped {
		t.Fatalf("pause=%v stopped=%v", paused, stopped)
	}
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		t.Fatalf("observe after a new task: %s", msg)
	}
}

func TestComputerUseDoneRetryAfterReject(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	markComputerUseSessionActive()
	setComputerUseOwner("sk-retry")
	beginComputerUseTask("sk-retry", "req-1", "save the file", []string{"file saved"})
	seedComputerUseObserve(t, "draft not saved", "")

	got := cuHandleDone("done")
	if !strings.Contains(got, "computer_done rejected") {
		t.Fatalf("first done = %q", got)
	}
	if !computerUseSessionActive() {
		t.Fatal("rejected done must keep sticky for retry")
	}

	seedComputerUseObserve(t, "file saved", "")
	got = cuHandleDone("saved now")
	if !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("retry done = %q", got)
	}
	st := computerUseTaskStateFor("sk-retry")
	if st == nil || st.LastAudit != computerUseAuditPassed {
		t.Fatalf("retry state=%+v", st)
	}
}

func TestComputerUseDonePassesWhenCorpusMatches(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-pass")
	markComputerUseSessionActive()
	beginComputerUseTask("sk-pass", "req-1", "保存文档", []string{"已保存"})
	seedComputerUseObserve(t, "文件已保存", "")

	got := cuHandleDone("saved")
	if !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("got %q", got)
	}
	st := computerUseTaskStateFor("sk-pass")
	if st == nil || st.LastAudit != computerUseAuditPassed {
		t.Fatalf("state=%+v", st)
	}
	if computerUseSessionActive() {
		t.Fatal("passed done must clear sticky")
	}
}

func TestComputerUseDoneDoesNotAuditGoal(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-goal")
	beginComputerUseTask("sk-goal", "req-1", "已保存", []string{"已保存"})
	seedComputerUseObserve(t, "hello world", "")

	got := cuHandleDone("done")
	if !strings.Contains(got, "rejected") {
		t.Fatalf("goal leaked into audit: %q", got)
	}
}

func TestComputerUseDoneRequiresObserveWhenContracted(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-obs")
	beginComputerUseTask("sk-obs", "req-1", "goal", []string{"已保存"})

	got := cuHandleDone("done")
	if !strings.Contains(got, "observe required") {
		t.Fatalf("got %q", got)
	}
}

func TestComputerUseLastObserveIsolatedByOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	setComputerUseOwner("tab-a")
	seedComputerUseObserve(t, "alpha-ocr", "AlphaBtn")
	setComputerUseOwner("tab-b")
	seedComputerUseObserve(t, "beta-ocr", "BetaBtn")

	setComputerUseOwner("tab-a")
	a := cuSession().LastObserve()
	setComputerUseOwner("tab-b")
	b := cuSession().LastObserve()
	if a == nil || b == nil || a.OCRExcerpt == b.OCRExcerpt {
		t.Fatalf("a=%v b=%v", a, b)
	}
	if !strings.Contains(a.OCRExcerpt, "alpha") || !strings.Contains(b.OCRExcerpt, "beta") {
		t.Fatalf("a=%q b=%q", a.OCRExcerpt, b.OCRExcerpt)
	}
}

func TestComputerUseBeginLatchedPerRequest(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	ctx := &LoopContext{
		UserID: "u1",
		Runtime: RuntimeContext{
			RequestID:    "req-same",
			Conversation: RuntimeConversationRef{SessionKey: "sk-latch"},
		},
		ComputerUseFresh: true,
	}
	maybeBeginComputerUseTask(ctx, "u1", "first")
	updateComputerUseTaskAudit("sk-latch", computerUseAuditFailed, true)
	st := computerUseTaskStateFor("sk-latch")
	if st == nil || st.FailedDone != 1 {
		t.Fatalf("setup FailedDone: %+v", st)
	}
	ctx.ComputerUseBegun = false
	maybeBeginComputerUseTask(ctx, "u1", "second rebuild")
	st2 := computerUseTaskStateFor("sk-latch")
	if st2 == nil || st2.FailedDone != 1 || st2.Goal != "first" {
		t.Fatalf("same RequestID must not replace TaskState: %+v", st2)
	}
}

func TestHelperContinuationKeepsTheSpentAttempt(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	goal := "给蓝信中的王占一发消息，告诉它，码卡龙可以顺利发送消息给蓝信用户了。"
	ctx := &LoopContext{
		ComputerHelper:         true,
		ComputerUseFresh:       true,
		ComputerUseRoutingText: goal,
		Runtime: RuntimeContext{
			RequestID:    "chat-task-ct-1-0",
			Conversation: RuntimeConversationRef{SessionKey: "helper-owner"},
		},
	}
	maybeBeginComputerUseTask(ctx, "u1", goal)
	setComputerUseOwner("helper-owner")
	sess := cuSession()
	sess.Policy().MaxSteps = 1
	if err := sess.BeginAction("click", "spend"); err != nil {
		t.Fatal(err)
	}
	if err := sess.BeginAction("click", "over"); err == nil {
		t.Fatal("expected the budget to be spent")
	}
	sess.NoteInputDenied()
	noteDesktopMessageTyped("码卡龙可以顺利发送消息给蓝信用户了。")
	if !noteDesktopMessageSubmitted("enter") {
		t.Fatal("send did not latch")
	}

	next := &LoopContext{
		ComputerHelper:         true,
		ComputerUseFresh:       true,
		ComputerUseRoutingText: goal,
		Runtime: RuntimeContext{
			RequestID:    "chat-task-ct-1-1",
			Conversation: RuntimeConversationRef{SessionKey: "helper-owner"},
		},
	}
	maybeBeginComputerUseTask(next, "u1", "[系统续轮] 继续完成同一任务")
	if !next.ComputerUseBegun {
		t.Fatal("continuation did not attach")
	}
	if !sess.InputDenied() {
		t.Fatal("continuation cleared the input denial")
	}
	if _, exhausted := sess.StepsExhausted(); !exhausted || sess.Policy().StepCount() == 0 {
		t.Fatalf("steps=%d", sess.Policy().StepCount())
	}
	st := computerUseTaskStateFor("helper-owner")
	if st == nil || !st.MessageSubmitted || st.Goal != goal || st.RequestID != "chat-task-ct-1-0" {
		t.Fatalf("state=%+v", st)
	}
	if msg := cuGuardDisabled("computer_click"); !strings.Contains(msg, "already submitted") {
		t.Fatalf("click=%s", msg)
	}
}

func openDrivingComputerUse(t *testing.T, owner, goal string) (*LoopContext, *computeruse.Session) {
	t.Helper()
	loop := NewLoopContext("cu-"+owner, config.MaxAgentIterationsCap, nil)
	loop.ComputerHelper = true
	loop.ComputerUseActive = true
	loop.ComputerUseFresh = true
	loop.ComputerUseRoutingText = goal
	loop.UserID = owner
	loop.Runtime.RequestID = "req-" + owner
	loop.Runtime.Conversation.SessionKey = owner
	maybeBeginComputerUseTask(loop, owner, goal)
	setComputerUseOwner(owner)
	return loop, cuSession()
}

func spendComputerUseBudget(t *testing.T, sess *computeruse.Session) {
	t.Helper()
	sess.Policy().MaxSteps = 1
	if err := sess.BeginAction("click", "spend"); err != nil {
		t.Fatal(err)
	}
	if err := sess.BeginAction("click", "over"); err == nil {
		t.Fatal("expected the budget to be spent")
	}
}

func TestTerminalComputerUseAttemptDoesNotRefill(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)

	spentOwner := "cu-spent-refill"
	spentLoop, spentSess := openDrivingComputerUse(t, spentOwner, "打开记事本")
	spendComputerUseBudget(t, spentSess)
	spent := &sharedAgentLoopCallbacks{loopCtx: spentLoop, maxIter: config.MaxAgentIterationsCap, userID: spentOwner}
	if n, ok := spent.TurnIterationCeiling(40); !ok || n != 42 {
		t.Fatalf("spent ceiling=%d ok=%v, want 42", n, ok)
	}
	if n, ok := spent.TurnIterationCeiling(41); !ok || n != 42 {
		t.Fatalf("second ceiling=%d ok=%v, want the same 42", n, ok)
	}
	if got := spent.ExtendIterationBudget(config.MaxAgentIterationsCap, 8); got != 0 || spentLoop.iterationExtensions != 0 {
		t.Fatalf("refill=%d extensions=%d", got, spentLoop.iterationExtensions)
	}

	deniedOwner := "cu-denied-refill"
	deniedLoop, deniedSess := openDrivingComputerUse(t, deniedOwner, "打开记事本")
	deniedSess.NoteInputDenied()
	if _, exhausted := deniedSess.StepsExhausted(); exhausted {
		t.Fatal("input denial spent the step budget")
	}
	denied := &sharedAgentLoopCallbacks{loopCtx: deniedLoop, maxIter: config.MaxAgentIterationsCap, userID: deniedOwner}
	if n, ok := denied.TurnIterationCeiling(10); !ok || n != 12 {
		t.Fatalf("denied ceiling=%d ok=%v, want 12", n, ok)
	}
	if got := denied.ExtendIterationBudget(config.MaxAgentIterationsCap, 3); got != 0 {
		t.Fatalf("denied refill=%d", got)
	}

	plain := NewLoopContext("plain-owner", config.MaxAgentIterationsCap, nil)
	plain.UserID = spentOwner
	plain.Runtime.Conversation.SessionKey = spentOwner
	plainCB := &sharedAgentLoopCallbacks{loopCtx: plain, maxIter: config.MaxAgentIterationsCap, userID: spentOwner}
	if got := plainCB.ExtendIterationBudget(config.MaxAgentIterationsCap, 4); got != config.MaxAgentIterationsCap {
		t.Fatalf("a loop that is not driving the desktop refilled by %d", got)
	}

	spentLoop.computerUseTerminalCeiling = 42
	spentLoop.releaseTurnIterationExtension(config.MaxAgentIterationsCap)
	if spentLoop.computerUseTerminalCeiling != 0 {
		t.Fatal("the next message kept the computer-use ceiling")
	}

	h := &IMMessageHandler{}
	legacyOwner := "cu-legacy-spent"
	legacy, legacySess := openDrivingComputerUse(t, legacyOwner, "打开记事本")
	spendComputerUseBudget(t, legacySess)
	mid := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           legacy,
		UserID:            legacyOwner,
		Iteration:         40,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      func(string) {},
	})
	if mid.Stop || mid.EffectiveMax != 40 {
		t.Fatalf("mid stop=%v effective=%d, want the finish rounds at 40", mid.Stop, mid.EffectiveMax)
	}
	if _, exhausted := legacySess.StepsExhausted(); !exhausted {
		t.Fatal("preparing the round cleared the spent budget")
	}
	end := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           legacy,
		UserID:            legacyOwner,
		Iteration:         42,
		EffectiveMax:      mid.EffectiveMax,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      func(string) {},
	})
	if !end.Stop || end.EffectiveMax >= config.MaxAgentIterationsCap {
		t.Fatalf("end stop=%v effective=%d, want the iteration limit without another wave", end.Stop, end.EffectiveMax)
	}

	nearOwner := "cu-legacy-near-cap"
	near, nearSess := openDrivingComputerUse(t, nearOwner, "打开记事本")
	spendComputerUseBudget(t, nearSess)
	nearResult := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           near,
		UserID:            nearOwner,
		Iteration:         config.MaxAgentIterationsCap - 2,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      func(string) {},
	})
	if nearResult.Stop || nearResult.EffectiveMax != config.MaxAgentIterationsCap-2 {
		t.Fatalf("near stop=%v effective=%d, want %d without an auto-extend", nearResult.Stop, nearResult.EffectiveMax, config.MaxAgentIterationsCap-2)
	}
}

func TestTerminalComputerUseKeepsFinishRoundsAfterAutoExtend(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	h := &IMMessageHandler{}
	owner := "cu-extend-then-terminal"
	loop, sess := openDrivingComputerUse(t, owner, "打开记事本")
	// The helper loop is created at the default cap. This case is a chat
	// budget of 30 that already auto-extended, so the stored limit has to
	// match the value passed in. A stored 300 would skip the extend.
	loop.SetMaxIterations(30)
	if got := h.refreshAgentLoopEffectiveMax(loop, 28, 30, 0, nil, nil); got != 60 || !loop.iterationCeilingRaised {
		t.Fatalf("auto-extend=%d raised=%v", got, loop.iterationCeilingRaised)
	}
	spendComputerUseBudget(t, sess)
	progress := func(string) {}
	first := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         40,
		EffectiveMax:      60,
		ConfigMax:         30,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if first.Stop || first.EffectiveMax != 40 || loop.iterationCeilingRaised {
		t.Fatalf("first stop=%v effective=%d raised=%v", first.Stop, first.EffectiveMax, loop.iterationCeilingRaised)
	}
	second := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         41,
		EffectiveMax:      first.EffectiveMax,
		ConfigMax:         30,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if second.Stop || second.EffectiveMax != 40 {
		t.Fatalf("second stop=%v effective=%d", second.Stop, second.EffectiveMax)
	}
	third := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         42,
		EffectiveMax:      second.EffectiveMax,
		ConfigMax:         30,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if !third.Stop || third.EffectiveMax >= 60 {
		t.Fatalf("third stop=%v effective=%d", third.Stop, third.EffectiveMax)
	}
}

func TestTerminalComputerUseDoesNotRefillWhileCoding(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	h := &IMMessageHandler{}
	owner := "cu-coding-terminal"
	loop, sess := openDrivingComputerUse(t, owner, "打开记事本")
	spendComputerUseBudget(t, sess)
	loop.setCodeSessionID("coding-session")
	progress := func(string) {}
	mid := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         40,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if mid.Stop || mid.EffectiveMax != 40 || loop.iterationExtensions != 0 {
		t.Fatalf("mid stop=%v effective=%d extensions=%d", mid.Stop, mid.EffectiveMax, loop.iterationExtensions)
	}
	end := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         42,
		EffectiveMax:      mid.EffectiveMax,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if !end.Stop || end.EffectiveMax >= config.MaxAgentIterationsCap || loop.iterationExtensions != 0 {
		t.Fatalf("end stop=%v effective=%d extensions=%d", end.Stop, end.EffectiveMax, loop.iterationExtensions)
	}
}

func TestTerminalComputerUseRestartKeepsFinishRounds(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	owner := "cu-restart-ceiling"
	loop, sess := openDrivingComputerUse(t, owner, "打开记事本")
	spendComputerUseBudget(t, sess)
	cb := &sharedAgentLoopCallbacks{loopCtx: loop, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if n, ok := cb.TurnIterationCeiling(40); !ok || n != 42 {
		t.Fatalf("first ceiling=%d ok=%v", n, ok)
	}
	if n, ok := cb.TurnIterationCeiling(41); !ok || n != 42 {
		t.Fatalf("same run ceiling=%d ok=%v", n, ok)
	}
	if n, ok := cb.TurnIterationCeiling(0); !ok || n != 2 {
		t.Fatalf("restart ceiling=%d ok=%v", n, ok)
	}
	if n, ok := cb.TurnIterationCeiling(1); !ok || n != 2 {
		t.Fatalf("restart next ceiling=%d ok=%v", n, ok)
	}
	if got := cb.ExtendIterationBudget(2, 4); got != 0 {
		t.Fatalf("restart refill=%d", got)
	}

	h := &IMMessageHandler{}
	legacyOwner := "cu-terminal-at-zero"
	legacy, legacySess := openDrivingComputerUse(t, legacyOwner, "打开记事本")
	spendComputerUseBudget(t, legacySess)
	progress := func(string) {}
	effective := config.MaxAgentIterationsCap
	for _, iteration := range []int{0, 1} {
		got := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
			Context:           legacy,
			UserID:            legacyOwner,
			Iteration:         iteration,
			EffectiveMax:      effective,
			ConfigMax:         config.MaxAgentIterationsCap,
			ChatFinalizeGrace: 2,
			ToolCalls:         8,
			SendProgress:      progress,
		})
		if got.Stop || legacy.iterationExtensions != 0 {
			t.Fatalf("iter %d stop=%v extensions=%d", iteration, got.Stop, legacy.iterationExtensions)
		}
		effective = got.EffectiveMax
	}
	end := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           legacy,
		UserID:            legacyOwner,
		Iteration:         2,
		EffectiveMax:      effective,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if !end.Stop || end.EffectiveMax >= config.MaxAgentIterationsCap || legacy.iterationExtensions != 0 {
		t.Fatalf("end stop=%v effective=%d extensions=%d", end.Stop, end.EffectiveMax, legacy.iterationExtensions)
	}
}

func TestHealthyComputerUseAttemptStillRefills(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	owner := "cu-healthy-refill"
	loop, sess := openDrivingComputerUse(t, owner, "打开记事本")
	if sess.InputDenied() {
		t.Fatal("a fresh attempt started denied")
	}
	if _, exhausted := sess.StepsExhausted(); exhausted {
		t.Fatal("a fresh attempt started exhausted")
	}
	cb := &sharedAgentLoopCallbacks{loopCtx: loop, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if _, ok := cb.TurnIterationCeiling(40); ok {
		t.Fatal("a healthy attempt installed a computer-use ceiling")
	}
	if got := cb.ExtendIterationBudget(config.MaxAgentIterationsCap, 6); got != config.MaxAgentIterationsCap {
		t.Fatalf("healthy refill=%d, want %d", got, config.MaxAgentIterationsCap)
	}

	legacy, _ := openDrivingComputerUse(t, "cu-healthy-legacy", "打开记事本")
	legacy.Kind = LoopKindBackground
	result := (&IMMessageHandler{}).prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:      legacy,
		UserID:       "cu-healthy-legacy",
		Iteration:    config.MaxAgentIterationsCap,
		EffectiveMax: config.MaxAgentIterationsCap,
		ConfigMax:    config.MaxAgentIterationsCap,
		ToolCalls:    4,
	})
	if result.Stop || result.EffectiveMax != config.MaxAgentIterationsCap*2 {
		t.Fatalf("healthy legacy stop=%v effective=%d, want %d", result.Stop, result.EffectiveMax, config.MaxAgentIterationsCap*2)
	}
}

func TestSentMessageStillContinuesTheIteration(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	goal := "给蓝信中的王占一发消息，告诉它，码卡龙可以顺利发送消息给蓝信用户了。"
	owner := "cu-sent-refill"
	loop, sess := openDrivingComputerUse(t, owner, goal)
	noteDesktopMessageTyped("码卡龙可以顺利发送消息给蓝信用户了。")
	if !noteDesktopMessageSubmitted("enter") {
		t.Fatal("send did not latch")
	}
	if sess.InputDenied() {
		t.Fatal("sending the message latched input denial")
	}
	if _, exhausted := sess.StepsExhausted(); exhausted {
		t.Fatal("sending the message spent the step budget")
	}
	cb := &sharedAgentLoopCallbacks{loopCtx: loop, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if _, ok := cb.TurnIterationCeiling(15); ok {
		t.Fatal("a submitted message installed a computer-use ceiling")
	}
	if got := cb.ExtendIterationBudget(config.MaxAgentIterationsCap, 5); got != config.MaxAgentIterationsCap {
		t.Fatalf("submitted-message refill=%d, want %d", got, config.MaxAgentIterationsCap)
	}
}

func TestCompletedComputerUseDoesNotOperateAgain(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	owner := "cu-done-stop"
	summary := "1. 已发出给马勇\n2. 无\n3. 无\n文件：无"
	loop, _ := openDrivingComputerUse(t, owner, "给蓝信中的马勇发一条消息")
	text, ok := cuDoneResult(summary)
	if !ok || !strings.HasPrefix(text, "computer_done:") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if computerUseSessionActive() {
		t.Fatal("a finished attempt left the session active")
	}
	for _, tool := range []string{"computer_focus", "computer_observe", "computer_type", "computer_key", "computer_click", "bash", "goal"} {
		msg := computerUseCompletedToolRefusal(tool)
		if msg == "" || strings.Contains(msg, "computer_observe and retry") || strings.Contains(msg, "Call computer_done") {
			t.Fatalf("%s refusal=%q", tool, msg)
		}
		if strings.HasPrefix(tool, "computer_") {
			if guarded := cuGuardDisabled(tool); guarded == "" {
				t.Fatalf("%s was allowed after computer_done", tool)
			}
		}
	}
	if msg := cuGuardDisabled("computer_done"); msg != "" {
		t.Fatalf("computer_done after success: %s", msg)
	}
	if got := computerUseFinishedReply(loop, owner, ""); got != summary {
		t.Fatalf("reply=%q", got)
	}
	if got := computerUseFinishedReply(loop, owner, "1. 已发出"); got != "1. 已发出" {
		t.Fatalf("model text replaced: %q", got)
	}

	cb := &sharedAgentLoopCallbacks{loopCtx: loop, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if n, ceilingOK := cb.TurnIterationCeiling(64); !ceilingOK || n != 64 {
		t.Fatalf("ceiling=%d ok=%v, want 64 with no finish rounds", n, ceilingOK)
	}
	if n, ceilingOK := cb.TurnIterationCeiling(65); !ceilingOK || n != 64 {
		t.Fatalf("later ceiling=%d ok=%v, want the same 64", n, ceilingOK)
	}
	if got := cb.ExtendIterationBudget(64, 8); got != 0 {
		t.Fatalf("finished refill=%d", got)
	}

	h := &IMMessageHandler{}
	progress := func(string) {}
	stopped := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           loop,
		UserID:            owner,
		Iteration:         64,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         8,
		SendProgress:      progress,
	})
	if !stopped.Stop {
		t.Fatal("the iteration after computer_done was allowed to start")
	}

	plain := NewLoopContext("plain-after-done", config.MaxAgentIterationsCap, nil)
	plain.UserID = owner
	plain.Runtime.Conversation.SessionKey = owner
	plainCB := &sharedAgentLoopCallbacks{loopCtx: plain, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if _, ceilingOK := plainCB.TurnIterationCeiling(8); ceilingOK {
		t.Fatal("a loop that is not driving the desktop stopped for a finished attempt")
	}
	if got := plainCB.ExtendIterationBudget(config.MaxAgentIterationsCap, 4); got != config.MaxAgentIterationsCap {
		t.Fatalf("plain refill=%d", got)
	}

	next := &LoopContext{
		ComputerHelper:         true,
		ComputerUseActive:      true,
		ComputerUseFresh:       true,
		ComputerUseRoutingText: "给蓝信中的马勇发一条消息",
		UserID:                 owner,
		Runtime: RuntimeContext{
			RequestID:    "chat-task-ct-1-1",
			Conversation: RuntimeConversationRef{SessionKey: owner},
		},
	}
	maybeBeginComputerUseTask(next, owner, "[系统续轮] 继续完成同一任务")
	if msg := cuGuardDisabled("computer_focus"); msg == "" {
		t.Fatal("a helper continuation cleared the finished attempt")
	}
	nextCB := &sharedAgentLoopCallbacks{loopCtx: next, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if n, ceilingOK := nextCB.TurnIterationCeiling(0); !ceilingOK || n != 0 {
		t.Fatalf("continuation ceiling=%d ok=%v, want 0", n, ceilingOK)
	}
	wave := h.prepareAgentLoopRound(agentLoopRoundPrepOptions{
		Context:           next,
		UserID:            owner,
		Iteration:         0,
		EffectiveMax:      config.MaxAgentIterationsCap,
		ConfigMax:         config.MaxAgentIterationsCap,
		ChatFinalizeGrace: 2,
		ToolCalls:         1,
		SendProgress:      progress,
	})
	if !wave.Stop {
		t.Fatal("a new helper wave started after computer_done")
	}

	beginComputerUseTask(owner, "req-next", "下一句", nil)
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		t.Fatalf("a new attempt stayed finished: %s", msg)
	}
	if _, ceilingOK := cb.TurnIterationCeiling(3); ceilingOK {
		t.Fatal("a new attempt kept the finished ceiling")
	}
}

func TestFinishedLoopDropsARimDrawnAfterComputerDone(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	owner := "cu-done-rim"
	loop, _ := openDrivingComputerUse(t, owner, "给蓝信中的马勇发一条消息")
	if _, ok := cuDoneResult("1. 已发出给马勇\n2. 无\n3. 无\n文件：无"); !ok {
		t.Fatal("computer_done was not accepted")
	}
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(42)
	if !computerUseSessionActive() || computerUseHaloTarget() != 42 {
		t.Fatalf("active=%v halo=%d", computerUseSessionActive(), computerUseHaloTarget())
	}
	actions := computerUseControlActions(t)
	retireFinishedComputerUseAttempt(loop, owner)
	if computerUseSessionActive() {
		t.Fatal("the finished loop left the session active")
	}
	if computerUseHaloTarget() != 0 {
		t.Fatal("the finished loop left the operated-window rim")
	}
	if len(*actions) != 1 || (*actions)[0] != "done" {
		t.Fatalf("control=%v", *actions)
	}

	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(7)
	plain := NewLoopContext("plain-rim", config.MaxAgentIterationsCap, nil)
	plain.UserID = owner
	retireFinishedComputerUseAttempt(plain, owner)
	if !computerUseSessionActive() || computerUseHaloTarget() != 7 {
		t.Fatal("a loop that is not driving the desktop cleared the rim")
	}
	if len(*actions) != 1 {
		t.Fatalf("control=%v", *actions)
	}

	driving, _ := openDrivingComputerUse(t, "cu-not-done", "打开记事本")
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(9)
	retireFinishedComputerUseAttempt(driving, "cu-not-done")
	if !computerUseSessionActive() || computerUseHaloTarget() != 9 {
		t.Fatal("an unfinished attempt dropped the rim when its loop returned")
	}
	if len(*actions) != 1 {
		t.Fatalf("control=%v", *actions)
	}
}

func TestFinishedLoopLeavesAnotherOwnersSession(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	loop, _ := openDrivingComputerUse(t, "owner-a", "给蓝信中的马勇发一条消息")
	if _, ok := cuDoneResult("1. 已发出给马勇\n2. 无\n3. 无\n文件：无"); !ok {
		t.Fatal("computer_done was not accepted")
	}
	setComputerUseOwner("owner-b")
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(77)
	actions := computerUseControlActions(t)
	retireFinishedComputerUseAttempt(loop, "owner-a")
	if !computerUseSessionActive() || computerUseHaloTarget() != 77 {
		t.Fatalf("active=%v halo=%d", computerUseSessionActive(), computerUseHaloTarget())
	}
	globalComputerUse.mu.Lock()
	held := globalComputerUse.activatedOwner
	globalComputerUse.mu.Unlock()
	if held != "owner-b" {
		t.Fatalf("owner=%s", held)
	}
	if len(*actions) != 0 {
		t.Fatalf("control=%v", *actions)
	}

	globalComputerUse.mu.Lock()
	changed := clearComputerUseStickyLocked()
	globalComputerUse.mu.Unlock()
	if !changed {
		t.Fatal("the other owner's session was already clear")
	}
	noteComputerUseOperatedWindow(81)
	retireFinishedComputerUseAttempt(loop, "owner-a")
	if computerUseSessionActive() {
		t.Fatal("a cleared session became active")
	}
	if computerUseHaloTarget() != 81 {
		t.Fatalf("halo=%d", computerUseHaloTarget())
	}
	if len(*actions) != 0 {
		t.Fatalf("control=%v", *actions)
	}
}

func TestComputerDoneLeavesAnotherOwnersSession(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("owner-b")
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(77)
	setComputerUseOwner("owner-a")
	beginComputerUseTask("owner-a", "req-a", "给蓝信中的马勇发一条消息", nil)
	actions := computerUseControlActions(t)
	text, ok := cuDoneResult("1. 已发出给马勇\n2. 无\n3. 无\n文件：无")
	if !ok || !strings.HasPrefix(text, "computer_done:") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if !computerUseSessionActive() || computerUseHaloTarget() != 77 {
		t.Fatalf("active=%v halo=%d", computerUseSessionActive(), computerUseHaloTarget())
	}
	globalComputerUse.mu.Lock()
	held := globalComputerUse.activatedOwner
	globalComputerUse.mu.Unlock()
	if held != "owner-b" {
		t.Fatalf("owner=%s", held)
	}
	if len(*actions) != 0 {
		t.Fatalf("control=%v", *actions)
	}

	setComputerUseOwner("owner-a")
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(42)
	text, ok = cuDoneResult("再次完成")
	if !ok || !strings.HasPrefix(text, "computer_done:") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if computerUseSessionActive() || computerUseHaloTarget() != 0 {
		t.Fatalf("active=%v halo=%d", computerUseSessionActive(), computerUseHaloTarget())
	}
	if len(*actions) != 1 || (*actions)[0] != "done" {
		t.Fatalf("control=%v", *actions)
	}
}

func TestComputerUseCallOwnerSurvivesGlobalSteal(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	loop, _ := openDrivingComputerUse(t, "owner-a", "给蓝信中的马勇发一条消息")
	callWithComputerUseOwner("owner-a", func() {
		setComputerUseOwner("owner-b")
		if computerUseOwnerKey() != "owner-b" {
			t.Fatalf("global=%s", computerUseOwnerKey())
		}
		if computerUseCallOwner() != "owner-a" {
			t.Fatalf("call=%s", computerUseCallOwner())
		}
		markComputerUseSessionActive()
		noteComputerUseOperatedWindow(77)
	})
	globalComputerUse.mu.Lock()
	held := globalComputerUse.activatedOwner
	globalComputerUse.mu.Unlock()
	if held != "owner-a" || !computerUseSessionActive() || computerUseHaloTarget() != 77 {
		t.Fatalf("owner=%s active=%v halo=%d", held, computerUseSessionActive(), computerUseHaloTarget())
	}

	setComputerUseOwner("owner-b")
	beginComputerUseTask("owner-b", "req-b", "另一件事", nil)
	actions := computerUseControlActions(t)
	text, ok := cuDoneResult("1. 已发出给别人\n2. 无\n3. 无\n文件：无")
	if !ok || !strings.HasPrefix(text, "computer_done:") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	globalComputerUse.mu.Lock()
	held = globalComputerUse.activatedOwner
	globalComputerUse.mu.Unlock()
	if held != "owner-a" || !computerUseSessionActive() || computerUseHaloTarget() != 77 || len(*actions) != 0 {
		t.Fatalf("owner=%s active=%v halo=%d control=%v", held, computerUseSessionActive(), computerUseHaloTarget(), *actions)
	}

	setComputerUseOwner("owner-b")
	callWithComputerUseOwner("owner-a", func() {
		text, ok = cuDoneResult("1. 已发出给马勇\n2. 无\n3. 无\n文件：无")
	})
	if !ok || !strings.HasPrefix(text, "computer_done:") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if computerUseSessionActive() || computerUseHaloTarget() != 0 || len(*actions) != 1 || (*actions)[0] != "done" {
		t.Fatalf("active=%v halo=%d control=%v", computerUseSessionActive(), computerUseHaloTarget(), *actions)
	}

	callWithComputerUseOwner("owner-a", func() {
		setComputerUseOwner("owner-b")
		markComputerUseSessionActive()
		noteComputerUseOperatedWindow(81)
	})
	setComputerUseOwner("owner-b")
	retireFinishedComputerUseAttempt(loop, "owner-a")
	if computerUseSessionActive() || computerUseHaloTarget() != 0 || len(*actions) != 2 || (*actions)[1] != "done" {
		t.Fatalf("active=%v halo=%d control=%v", computerUseSessionActive(), computerUseHaloTarget(), *actions)
	}

	left := make(chan string, 1)
	right := make(chan string, 1)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		callWithComputerUseOwner("owner-a", func() {
			<-start
			left <- computerUseCallOwner()
		})
	}()
	go func() {
		defer wg.Done()
		callWithComputerUseOwner("owner-b", func() {
			<-start
			right <- computerUseCallOwner()
		})
	}()
	close(start)
	wg.Wait()
	if got := <-left; got != "owner-a" {
		t.Fatalf("left=%s", got)
	}
	if got := <-right; got != "owner-b" {
		t.Fatalf("right=%s", got)
	}
}

func TestRejectedComputerDoneStillAllowsObserve(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	markComputerUseSessionActive()
	owner := "cu-done-reject"
	loop := NewLoopContext("cu-"+owner, config.MaxAgentIterationsCap, nil)
	loop.ComputerHelper = true
	loop.ComputerUseActive = true
	loop.UserID = owner
	loop.Runtime.RequestID = "req-reject"
	loop.Runtime.Conversation.SessionKey = owner
	setComputerUseOwner(owner)
	beginComputerUseTask(owner, "req-reject", "save the file", []string{"file saved"})
	seedComputerUseObserve(t, "draft not saved", "")
	text, ok := cuDoneResult("not yet")
	if ok || !strings.Contains(text, "call computer_observe and retry") {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if !computerUseSessionActive() {
		t.Fatal("a rejected done cleared the session")
	}
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		t.Fatalf("observe refused: %s", msg)
	}
	if msg := computerUseCompletedToolRefusal("bash"); msg != "" {
		t.Fatalf("bash refused: %s", msg)
	}
	cb := &sharedAgentLoopCallbacks{loopCtx: loop, maxIter: config.MaxAgentIterationsCap, userID: owner}
	if _, ceilingOK := cb.TurnIterationCeiling(10); ceilingOK {
		t.Fatal("a rejected done installed a computer-use ceiling")
	}
	if got := cb.ExtendIterationBudget(config.MaxAgentIterationsCap, 3); got != config.MaxAgentIterationsCap {
		t.Fatalf("rejected refill=%d", got)
	}
	if got := computerUseFinishedReply(loop, owner, ""); got != "" {
		t.Fatalf("rejected reply=%q", got)
	}
}

func TestFreshRequestOnTheSameOwnerStartsANewAttempt(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	ctx := &LoopContext{
		ComputerUseFresh:       true,
		ComputerUseRoutingText: "第一件事",
		Runtime: RuntimeContext{
			RequestID:    "req-1",
			Conversation: RuntimeConversationRef{SessionKey: "owner-a"},
		},
	}
	maybeBeginComputerUseTask(ctx, "u1", "第一件事")
	setComputerUseOwner("owner-a")
	sess := cuSession()
	sess.Policy().MaxSteps = 1
	if err := sess.BeginAction("click", "spend"); err != nil {
		t.Fatal(err)
	}
	if err := sess.BeginAction("click", "over"); err == nil {
		t.Fatal("expected the budget to be spent")
	}
	sess.NoteInputDenied()

	next := &LoopContext{
		ComputerUseFresh:       true,
		ComputerUseRoutingText: "第二件事",
		Runtime: RuntimeContext{
			RequestID:    "req-2",
			Conversation: RuntimeConversationRef{SessionKey: "owner-a"},
		},
	}
	maybeBeginComputerUseTask(next, "u1", "第二件事")
	if sess.InputDenied() || sess.Policy().StepCount() != 0 {
		t.Fatalf("denied=%v steps=%d", sess.InputDenied(), sess.Policy().StepCount())
	}
	st := computerUseTaskStateFor("owner-a")
	if st == nil || st.Goal != "第二件事" || st.RequestID != "req-2" {
		t.Fatalf("state=%+v", st)
	}
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		t.Fatalf("observe=%s", msg)
	}
}

func TestComputerUseStickyDoesNotBegin(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			RequestID:    "req-sticky",
			Conversation: RuntimeConversationRef{SessionKey: "sk-sticky"},
		},
		ComputerUseFresh: false,
	}
	maybeBeginComputerUseTask(ctx, "u1", "continue")
	if computerUseTaskStateFor("sk-sticky") != nil {
		t.Fatal("sticky continuation must not Begin")
	}
}

func TestComputerUseFreshBeginPlaybookIncludesContract(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			RequestID:    "req-pb",
			Conversation: RuntimeConversationRef{SessionKey: "sk-pb"},
		},
		ComputerUseFresh: true,
	}
	maybeBeginComputerUseTask(ctx, "u1", "保存")
	beginComputerUseTask("sk-pb", "req-pb", "保存", []string{"已保存"})
	setComputerUseOwner("sk-pb")
	section := computerUsePlaybookSection(true)
	if !strings.Contains(section, "已保存") {
		t.Fatalf("first-round playbook missing contract: %q", section)
	}
}

func TestComputerUseResetClearsAllTaskStates(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	beginComputerUseTask("sk-a", "r1", "g", []string{"已保存"})
	beginComputerUseTask("sk-b", "r2", "g", []string{"已保存"})
	app := &App{}
	if err := app.ComputerUseReset(); err != nil {
		t.Fatal(err)
	}
	if computerUseTaskStateFor("sk-a") != nil || computerUseTaskStateFor("sk-b") != nil {
		t.Fatal("reset must clear every CU TaskState")
	}
}

func TestComputerUseResetDoesNotClearHorizonTaskState(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	beginComputerUseTask("u1", "r1", "g", []string{"已保存"})
	root := t.TempDir()
	state := &longhorizon.TaskState{
		TaskID:   "hz-keep",
		Status:   longhorizon.StatusExecuting,
		UserGoal: "open notepad",
		Policy:   longhorizon.PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-keep"},
	}
	if err := longhorizon.SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	if err := app.ComputerUseReset(); err != nil {
		t.Fatal(err)
	}
	if computerUseTaskStateFor("u1") != nil {
		t.Fatal("reset must clear CU TaskState")
	}
	loaded, err := longhorizon.LoadTaskState(root, "hz-keep")
	if err != nil || loaded == nil || loaded.TaskID != "hz-keep" || loaded.Status != longhorizon.StatusExecuting {
		t.Fatalf("horizon task must survive CU reset: %+v err=%v", loaded, err)
	}
}

func TestComputerUseAcceptanceDenylist(t *testing.T) {
	got := normalizeComputerUseAcceptance([]string{"确定", "OK", "已保存", "保存", "  "})
	if len(got) != 1 || got[0] != "已保存" {
		t.Fatalf("got %#v", got)
	}
}

func TestSyncComputerUseTurnRecordsFreshWithoutToolsPrep(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	desktopbot.ResetComputerScreens()
	t.Cleanup(desktopbot.ResetComputerScreens)
	h := &IMMessageHandler{}
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			RequestID:    "req-sync",
			Conversation: RuntimeConversationRef{SessionKey: "sk-sync"},
		},
		ComputerUseRoutingText: "@computer 看屏幕",
	}
	syncComputerUseTurn(h, ctx, "desktop-user", "看屏幕")
	if ctx.ComputerUseActive || ctx.ComputerUseBegun {
		t.Fatalf("active=%v begun=%v", ctx.ComputerUseActive, ctx.ComputerUseBegun)
	}
	if !ctx.ComputerHelperDispatched || ctx.ComputerHelperGoal == "" {
		t.Fatalf("helper card missing: dispatched=%v goal=%q", ctx.ComputerHelperDispatched, ctx.ComputerHelperGoal)
	}
	if computerUseTaskStateFor("sk-sync") != nil {
		t.Fatal("the chat loop must not begin a computer-use task")
	}
	if computerUseTaskStateFor("desktop-user") != nil {
		t.Fatal("must not key TaskState by UserID when SessionKey is set")
	}
}

func TestDesktopMessageSendDispatchesHelperCard(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	desktopbot.ResetComputerScreens()
	t.Cleanup(desktopbot.ResetComputerScreens)
	h := &IMMessageHandler{unifiedClassifier: computerUseGateTestClassifier(t)}
	text := "给蓝信中的张勇用户发送消息，告诉它你是码卡龙。"
	ctx := &LoopContext{ComputerUseRoutingText: text, UserID: desktopUserID}
	active, _ := recordComputerUseGate(h, ctx, text)
	if active || ctx.ComputerUseActive {
		t.Fatalf("main loop became the operator active=%v flag=%v", active, ctx.ComputerUseActive)
	}
	if !ctx.ComputerHelperDispatched {
		t.Fatal("desktop IM send did not dispatch the helper")
	}
	if !strings.Contains(ctx.ComputerHelperGoal, "蓝信") || !strings.Contains(ctx.ComputerHelperGoal, "张勇") {
		t.Fatalf("goal = %q", ctx.ComputerHelperGoal)
	}
	if !strings.Contains(ctx.ComputerHelperLimits, "只发送") || strings.Contains(ctx.ComputerHelperLimits, "不发送") {
		t.Fatalf("limits = %q", ctx.ComputerHelperLimits)
	}
	if !strings.Contains(ctx.ComputerHelperSuccess, "没有文件也算完成") || !strings.Contains(ctx.ComputerHelperSuccess, "第 2 行只写无") {
		t.Fatalf("success = %q", ctx.ComputerHelperSuccess)
	}
	if !ctx.DesktopMessageHelperOwns {
		t.Fatal("desktop IM send did not mark the chat loop as not a second sender")
	}

	desktopbot.ResetComputerScreens()
	group := &LoopContext{
		ComputerUseRoutingText:    text,
		UserID:                    "lansenger:group",
		LansengerGroupPermissions: &lansengerGroupPermissionPolicy{},
	}
	if active, _ := recordComputerUseGate(h, group, text); active || group.ComputerHelperNoted || group.ComputerHelperDispatched {
		t.Fatalf("group chat started a helper active=%v noted=%v dispatched=%v", active, group.ComputerHelperNoted, group.ComputerHelperDispatched)
	}

	desktopbot.ResetComputerScreens()
	note := &LoopContext{ComputerUseRoutingText: "@computer 打开记事本", UserID: desktopUserID}
	recordComputerUseGate(h, note, note.ComputerUseRoutingText)
	if !strings.Contains(note.ComputerHelperLimits, "不发送") || strings.Contains(note.ComputerHelperLimits, "只发送") {
		t.Fatalf("default card changed: %q", note.ComputerHelperLimits)
	}
	if note.DesktopMessageHelperOwns {
		t.Fatal("opening Notepad is not a desktop message send")
	}
}

func TestStickySendDoesNotOpenFromThePhraseList(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	desktopbot.ResetComputerScreens()
	t.Cleanup(desktopbot.ResetComputerScreens)
	send := "给蓝信中的张勇用户发送消息，告诉它你是码卡龙。"

	markComputerUseSessionActive()
	closed := &IMMessageHandler{}
	if active, fresh := closed.gateComputerUse(send); active || fresh {
		t.Fatalf("phrase list kept sticky computer use active=%v fresh=%v", active, fresh)
	}
	if computerUseSessionActive() {
		t.Fatal("an unconfirmed send must clear sticky")
	}
	ctx := &LoopContext{ComputerUseRoutingText: send, UserID: desktopUserID}
	recordComputerUseGate(closed, ctx, send)
	if ctx.ComputerHelperNoted || ctx.ComputerHelperDispatched || ctx.DesktopMessageHelperOwns {
		t.Fatalf("unconfirmed send started a helper noted=%v dispatched=%v owns=%v limits=%q",
			ctx.ComputerHelperNoted, ctx.ComputerHelperDispatched, ctx.DesktopMessageHelperOwns, ctx.ComputerHelperLimits)
	}

	markComputerUseSessionActive()
	if active, fresh := closed.gateComputerUse("打开记事本"); !active || fresh {
		t.Fatalf("a launch follow-up active=%v fresh=%v", active, fresh)
	}

	markComputerUseSessionActive()
	ready := &IMMessageHandler{unifiedClassifier: computerUseGateTestClassifier(t)}
	active, fresh := ready.gateComputerUse(send)
	if !active || !fresh {
		t.Fatalf("classifier send during sticky active=%v fresh=%v", active, fresh)
	}
	desktopbot.ResetComputerScreens()
	confirmed := &LoopContext{ComputerUseRoutingText: send, UserID: desktopUserID}
	recordComputerUseGate(ready, confirmed, send)
	if !confirmed.ComputerHelperDispatched || !confirmed.DesktopMessageHelperOwns {
		t.Fatalf("dispatched=%v owns=%v", confirmed.ComputerHelperDispatched, confirmed.DesktopMessageHelperOwns)
	}
	if !strings.Contains(confirmed.ComputerHelperLimits, "只发送") || strings.Contains(confirmed.ComputerHelperLimits, "不发送") {
		t.Fatalf("limits = %q", confirmed.ComputerHelperLimits)
	}
}

func TestParentDesktopSendRefusesASecondSender(t *testing.T) {
	owned := &LoopContext{DesktopMessageHelperOwns: true}
	for _, tool := range []string{"craft_tool", "search_and_install_skill", "send_to_im", "send_file", "bash", "write_file", chatTaskToolDispatch} {
		if parentDesktopSendToolRefusal(owned, tool) == "" {
			t.Fatalf("%s was allowed", tool)
		}
	}
	if parentDesktopSendToolRefusal(owned, "read_file") != "" || parentDesktopSendToolRefusal(owned, "computer_click") != "" {
		t.Fatal("a read or a helper click was refused on the chat loop")
	}
	helper := &LoopContext{ComputerHelper: true, DesktopMessageHelperOwns: true}
	if parentDesktopSendToolRefusal(helper, "bash") != "" {
		t.Fatal("the helper loop was treated as a second sender")
	}
	if parentDesktopSendToolRefusal(&LoopContext{}, "craft_tool") != "" {
		t.Fatal("an ordinary chat was refused")
	}
	if desktopMessageHelperOwnsNote(owned) == "" || desktopMessageHelperOwnsNote(&LoopContext{}) != "" {
		t.Fatal("owns note")
	}
}

func TestComputerUseGateUsesLoopRoutingText(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	desktopbot.ResetComputerScreens()
	t.Cleanup(desktopbot.ResetComputerScreens)
	h := &IMMessageHandler{}
	ctx := &LoopContext{ComputerUseRoutingText: "@computer 点一下确定"}
	activeCompact, _ := h.gateComputerUse("随便聊聊")
	activeShared, _ := recordComputerUseGate(h, ctx, ctx.ComputerUseRoutingText)
	if activeCompact {
		t.Fatal("compact/chat text should not open CU")
	}
	if activeShared || ctx.ComputerUseActive || !ctx.ComputerHelperDispatched || ctx.ComputerHelperGoal == "" {
		t.Fatalf("main loop active=%v flag=%v dispatched=%v goal=%q", activeShared, ctx.ComputerUseActive, ctx.ComputerHelperDispatched, ctx.ComputerHelperGoal)
	}
	desktopbot.ResetComputerScreens()
	activeAgain, _ := recordComputerUseGate(h, ctx, "随便聊聊")
	if activeAgain || ctx.ComputerUseActive {
		t.Fatal("a settled chat loop must not become the operator")
	}
	if started, queued := desktopbot.ClaimComputerScreen("default", "probe"); !started || queued {
		t.Fatal("the settled loop claimed the screen again")
	}
}

func TestComputerUseAuditIgnoresNegatedSubstring(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-neg")
	beginComputerUseTask("sk-neg", "req-1", "goal", []string{"完成"})
	seedComputerUseObserve(t, "任务未完成", "")
	got := cuHandleDone("done")
	if !strings.Contains(got, "rejected") {
		t.Fatalf("negated substring must not pass: %q", got)
	}
}

func TestComputerUseAuditAcceptsPositiveMatchAfterNegation(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-pos")
	beginComputerUseTask("sk-pos", "req-1", "goal", []string{"完成"})
	seedComputerUseObserve(t, "先前未完成，现在已完成", "")
	got := cuHandleDone("done")
	if !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("positive match after negation should pass: %q", got)
	}
}

func TestComputerUseAuditIgnoresEnglishNegation(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-en")
	beginComputerUseTask("sk-en", "req-1", "goal", []string{"complete"})
	seedComputerUseObserve(t, "the task is incomplete", "")
	got := cuHandleDone("done")
	if !strings.Contains(got, "rejected") {
		t.Fatalf("incomplete must not satisfy complete: %q", got)
	}
}

func TestComputerUseAuditDoesNotTreatMainAsNegation(t *testing.T) {
	if computerUseNegatedMatch("main complete", strings.Index("main complete", "complete")) {
		t.Fatal("main must not count as an in- negation of complete")
	}
}

func TestComputerUseSessionEvictDropsTaskState(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	beginComputerUseTask("old-owner", "r1", "g", []string{"已保存"})
	globalComputerUse.mu.Lock()
	forgetComputerUseOwnerLocked("old-owner")
	globalComputerUse.mu.Unlock()
	if computerUseTaskStateFor("old-owner") != nil {
		t.Fatal("evicting a session must drop its CU TaskState")
	}
}

func TestComputerUseSessionEvictKeepsHorizonClaimOnly(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("old-owner")
	setHorizonComputerUseClaimOnly("old-owner", true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly("old-owner", false) })
	globalComputerUse.mu.Lock()
	forgetComputerUseOwnerLocked("old-owner")
	globalComputerUse.mu.Unlock()
	if !horizonComputerUseClaimOnly("old-owner") {
		t.Fatal("evicting a session must not drop in-flight GUI claim-only")
	}
	setComputerUseOwner("old-owner")
	got := cuHandleDone("opened")
	if !strings.HasPrefix(got, "computer_done claim:") {
		t.Fatalf("claim-only must survive eviction, got %q", got)
	}
}

func TestComputerUseEvictSkipsInFlightGUIObserve(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("gui-owner")
	seedComputerUseObserve(t, "notepad visible", "Notepad")
	setHorizonComputerUseClaimOnly("gui-owner", true)
	t.Cleanup(func() { setHorizonComputerUseClaimOnly("gui-owner", false) })
	for i := 0; i < computerUseMaxSessions+2; i++ {
		setComputerUseOwner("other-" + string(rune('A'+i)))
		_ = cuSession()
	}
	sess := cuSessionForOwner("gui-owner")
	if sess == nil || sess.LastValidObserve() == nil {
		t.Fatal("in-flight GUI observe must not be capacity-evicted")
	}
}

func TestComputerUseDonePassClearsPlaybookContract(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-clear")
	beginComputerUseTask("sk-clear", "req-1", "保存文档", []string{"已保存"})
	seedComputerUseObserve(t, "文件已保存", "")
	if got := cuHandleDone("saved"); !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("got %q", got)
	}
	st := computerUseTaskStateFor("sk-clear")
	if st == nil || st.LastAudit != computerUseAuditPassed || len(st.Acceptance) != 0 {
		t.Fatalf("state=%+v", st)
	}
	if strings.Contains(computerUsePlaybookSection(true), "已保存") {
		t.Fatal("passed done must stop injecting the contract into the playbook")
	}
}

func TestComputerUsePlaybookExtraUsesExplicitOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	beginComputerUseTask("sk-a", "r1", "g", []string{"已保存"})
	beginComputerUseTask("sk-b", "r2", "g", []string{"草稿已打开"})
	setComputerUseOwner("sk-b")
	section := computerUsePlaybookSectionFor(true, "sk-a")
	if !strings.Contains(section, "已保存") {
		t.Fatalf("explicit owner missing contract: %q", section)
	}
	if strings.Contains(section, "草稿已打开") {
		t.Fatalf("playbook used global owner instead of explicit owner: %q", section)
	}
}

func TestSyncComputerUseTurnSetsLoopOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			RequestID:    "req-owner",
			Conversation: RuntimeConversationRef{SessionKey: "sk-loop-owner"},
		},
		ComputerUseRoutingText: "@computer 看屏幕",
	}
	syncComputerUseTurn(h, ctx, "desktop-user", "看屏幕")
	if ctx.ComputerUseOwner != "sk-loop-owner" {
		t.Fatalf("ComputerUseOwner=%q", ctx.ComputerUseOwner)
	}
}

func TestComputerUseAuditRejectsEnglishNotComplete(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-not")
	beginComputerUseTask("sk-not", "req-1", "goal", []string{"complete"})
	seedComputerUseObserve(t, "the task is not complete", "")
	got := cuHandleDone("done")
	if !strings.Contains(got, "rejected") {
		t.Fatalf("not complete must not satisfy complete: %q", got)
	}
}

func TestComputerUseAuditAcceptsLoginComplete(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-login")
	beginComputerUseTask("sk-login", "req-1", "goal", []string{"complete"})
	seedComputerUseObserve(t, "login complete", "")
	got := cuHandleDone("done")
	if !strings.HasPrefix(got, "computer_done:") {
		t.Fatalf("login complete must not count as in- negation: %q", got)
	}
}

func TestComputerUseAuditRejectsMeiYouWancheng(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("sk-meiyou")
	beginComputerUseTask("sk-meiyou", "req-1", "goal", []string{"完成"})
	seedComputerUseObserve(t, "任务没有完成", "")
	got := cuHandleDone("done")
	if !strings.Contains(got, "rejected") {
		t.Fatalf("没有完成 must not satisfy 完成: %q", got)
	}
}

func TestComputerUseEpilogueFallbackDoesNotUsePolicyOwner(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	h := &IMMessageHandler{}
	ctx := &LoopContext{
		UserID: "desktop-user",
		Runtime: RuntimeContext{
			RequestID:     "req-epilogue-owner",
			PolicyOwnerID: "policy-owner",
			Conversation:  RuntimeConversationRef{SessionKey: "im:desktop:desktop-user:actor"},
		},
		ComputerUseRoutingText: "@computer 看屏幕",
	}
	syncComputerUseTurn(h, ctx, ctx.Runtime.PolicyOwnerID, "看屏幕")
	if ctx.ComputerUseOwner != "im:desktop:desktop-user:actor" {
		t.Fatalf("SessionKey must win over PolicyOwnerID fallback: %q", ctx.ComputerUseOwner)
	}
	ctx2 := &LoopContext{
		UserID: "desktop-user",
		Runtime: RuntimeContext{
			RequestID:     "req-epilogue-user",
			PolicyOwnerID: "policy-owner",
		},
		ComputerUseRoutingText: "@computer 看屏幕",
	}
	syncComputerUseTurn(h, ctx2, ctx2.UserID, "看屏幕")
	if ctx2.ComputerUseOwner != "desktop-user" {
		t.Fatalf("UserID must win over PolicyOwnerID when SessionKey is empty: %q", ctx2.ComputerUseOwner)
	}
	if computerUseTaskStateFor("policy-owner") != nil {
		t.Fatal("must not Begin TaskState under PolicyOwnerID")
	}
}

func TestComputerUseDoneHorizonClaimOnly(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("hz-owner")
	beginComputerUseTask("hz-owner", "req-hz", "open notepad", []string{"window visible"})
	seedComputerUseObserve(t, "notepad", "")
	setHorizonComputerUseClaimOnly("hz-owner", true)
	got := cuHandleDone("opened")
	if !strings.HasPrefix(got, "computer_done claim:") {
		t.Fatalf("got %q", got)
	}
	st := computerUseTaskStateFor("hz-owner")
	if st == nil || len(st.Acceptance) == 0 {
		t.Fatalf("claim-only must not clear Horizon/CU contract: %+v", st)
	}
	if st.LastAudit == computerUseAuditPassed {
		t.Fatal("claim-only must not mark CU audit passed")
	}
	hz := &longhorizon.TaskState{ManagerNext: longhorizon.NextDone}
	if longhorizon.MarkCompleted(hz) || hz.Completed {
		t.Fatal("computer_done claim must not complete the outer Horizon task")
	}
}

func TestDesktopMessageSubmitStopsFurtherActions(t *testing.T) {
	goal := "给蓝信中的王占一发消息，告诉它，码卡龙可以顺利发送消息给蓝信用户了。"
	if got := desktopMessagePayload(goal); got != "码卡龙可以顺利发送消息给蓝信用户了。" {
		t.Fatalf("payload=%q", got)
	}
	if desktopMessagePayload("打开记事本写一段话") != "" {
		t.Fatal("a non-send goal must not have a message payload")
	}

	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	computerUseForegroundTitleFn = func() string { return "蓝信" }
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("message-send test must not focus a real window")
		return nil
	}
	beginComputerUseTask(computerUseDefaultOwner, "req-lanxin", goal, nil)

	name := cuHandleType(map[string]interface{}{"text": "王占一"})
	searchEnter := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.types != 1 || in.keys != 1 || strings.Contains(name, "already submitted") || strings.Contains(searchEnter, "already submitted") {
		t.Fatalf("types=%d keys=%d name=%s enter=%s", in.types, in.keys, name, searchEnter)
	}

	typed := cuHandleType(map[string]interface{}{"text": "码卡龙可以顺利发送消息给蓝信用户了。"})
	newline := cuHandleKey(map[string]interface{}{"keys": "shift+enter"})
	if in.types != 2 || in.keys != 2 || !strings.Contains(typed, "press Enter once") || strings.Contains(typed, "already submitted") || strings.Contains(newline, "already submitted") {
		t.Fatalf("types=%d keys=%d typed=%s newline=%s", in.types, in.keys, typed, newline)
	}
	sent := cuHandleKey(map[string]interface{}{"keys": "return"})
	if in.types != 2 || in.keys != 3 || !strings.Contains(sent, "already submitted") {
		t.Fatalf("types=%d keys=%d sent=%s", in.types, in.keys, sent)
	}

	click := cuHandleClick(map[string]interface{}{"ref": "e0"})
	again := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.clicks != 0 || in.keys != 3 || !strings.Contains(click, "already submitted") || !strings.Contains(again, "already submitted") {
		t.Fatalf("clicks=%d keys=%d click=%s again=%s", in.clicks, in.keys, click, again)
	}
	if cuRefuseAfterDesktopMessageSent("bash") == "" {
		t.Fatal("bash must stop after the message is submitted")
	}
	done := cuHandleDone("已发给王占一")
	if !strings.HasPrefix(done, "computer_done:") {
		t.Fatalf("done=%s", done)
	}
}

func TestDesktopMessageSpeechSubmitHidesOnlyOwnHalo(t *testing.T) {
	const goal = "给终端安全的马勇发消息，选读它码卡龙现在很擅长给蓝信中的用户发消息了。"
	const body = "码卡龙现在很擅长给蓝信中的用户发消息了。"
	if got := desktopMessagePayload(goal); got != body {
		t.Fatalf("payload=%q", got)
	}

	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("speech-owner")
	beginComputerUseTask("speech-owner", "req-speech", goal, nil)
	if !noteDesktopMessageTyped(body) {
		t.Fatal("speech-transcribed body did not latch as typed")
	}
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(42)
	if !noteDesktopMessageSubmitted("enter") {
		t.Fatal("enter did not latch the send")
	}
	if computerUseHaloTarget() != 0 || !computerUseSessionActive() {
		t.Fatalf("halo=%d active=%v", computerUseHaloTarget(), computerUseSessionActive())
	}

	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner("speech-owner")
	beginComputerUseTask("speech-owner", "req-speech-2", goal, nil)
	noteDesktopMessageTyped(body)
	setComputerUseOwner("other-owner")
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(77)
	setComputerUseOwner("speech-owner")
	if !noteDesktopMessageSubmitted("enter") || computerUseHaloTarget() != 77 {
		t.Fatalf("another owner's halo=%d", computerUseHaloTarget())
	}
}

func TestDesktopMessageRejectedDoneAllowsAuditObserve(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner(computerUseDefaultOwner)
	goal := "给蓝信中的马勇发消息，告诉它，测试发送。"
	beginComputerUseTask(computerUseDefaultOwner, "req-audit", goal, []string{"已发送"})
	noteDesktopMessageTyped("测试发送。")
	if !noteDesktopMessageSubmitted("enter") {
		t.Fatal("enter did not latch the send")
	}

	done := cuHandleDone("已发送")
	if !strings.Contains(done, "computer_done rejected") || !strings.Contains(done, "computer_observe") {
		t.Fatalf("done=%s", done)
	}
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		t.Fatalf("audit observe blocked: %s", msg)
	}
	for _, tool := range []string{"computer_click", "computer_type", "computer_key", "computer_find", "bash"} {
		if msg := cuRefuseAfterDesktopMessageSent(tool); !strings.Contains(msg, "already submitted") {
			t.Fatalf("%s was not blocked: %s", tool, msg)
		}
	}
	seedComputerUseObserve(t, "已发送", "")
	if retry := cuHandleDone("已发送"); !strings.HasPrefix(retry, "computer_done:") {
		t.Fatalf("retry=%s", retry)
	}
}

func TestDesktopMessagePayloadDoesNotExcludeIntentWordsInBody(t *testing.T) {
	for goal, want := range map[string]string{
		"给蓝信中的马勇发消息，告诉它，介绍一下伴读功能。":   "介绍一下伴读功能。",
		"给蓝信中的马勇发消息，告诉它，在吗？":         "在吗？",
		"给蓝信上的终端安全的马勇发个消息，介绍一下伴读功能。": "介绍一下伴读功能。",
	} {
		if got := desktopMessagePayload(goal); got != want {
			t.Errorf("payload(%q)=%q, want %q", goal, got, want)
		}
	}
	if got := desktopMessagePayload("写一份蓝信发消息的说明，内容是介绍一下操作步骤"); got != "" {
		t.Fatalf("document request produced a payload: %q", got)
	}
	if got := desktopMessagePayload("怎么给蓝信发消息，再介绍一下操作步骤"); got != "" {
		t.Fatalf("inquiry produced a payload: %q", got)
	}
}

func TestDesktopMessageShortBodyLatchesWithoutSubstringMatch(t *testing.T) {
	const goal = "给蓝信中的马勇发消息，告诉它，好。"
	if got := desktopMessagePayload(goal); got != "好。" {
		t.Fatalf("payload=%q", got)
	}
	if got := desktopMessagePayload("给蓝信中的马勇发消息，告诉它，……"); got != "" {
		t.Fatalf("punctuation-only payload=%q", got)
	}
	if desktopMessageCovers("好", "马好") || !desktopMessageCovers("好", "好。") {
		t.Fatal("short body must require an exact compact match")
	}

	resetComputerUseRuntimeForTest(t)
	resetComputerUseSessionForTest(t)
	setComputerUseOwner(computerUseDefaultOwner)
	beginComputerUseTask(computerUseDefaultOwner, "req-short", goal, nil)
	if noteDesktopMessageTyped("马勇") {
		t.Fatal("the recipient search was mistaken for the short body")
	}
	if !noteDesktopMessageTyped("好。") || !noteDesktopMessageSubmitted("enter") {
		t.Fatal("short body after recipient search did not reach the submitted state")
	}
}

func TestDesktopMessageSendControlNames(t *testing.T) {
	for _, name := range []string{"发送", "发送消息", "发送(S)", "发送按钮", "發送", "發送(S)", "發送按鈕", "Send", "Send message", "Send now", "Send button", "the blue Send button"} {
		if !desktopMessageSendControlName(name) {
			t.Errorf("Send label %q was rejected", name)
		}
	}
	for _, name := range []string{"联系人", "发送文件", "发送文件按钮", "消息里提到发送", "sender", "已发送"} {
		if desktopMessageSendControlName(name) {
			t.Errorf("non-Send label %q was accepted", name)
		}
	}
}

func TestDesktopMessageSendClickLatchesAndHidesHalo(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "发送", BBox: [4]int{100, 40, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	computerUseForegroundTitleFn = func() string { return "蓝信" }
	computerUseForegroundIDFn = func() uint64 { return 42 }
	computerUseWindowAtPointFn = func(int, int) (uint64, string) { return 42, "蓝信" }
	computerUsePolicyWindowHook = func(int, int) string { return "蓝信" }
	setComputerUseOwner(computerUseDefaultOwner)
	beginComputerUseTask(computerUseDefaultOwner, "req-click", "给蓝信中的马勇发消息，告诉它，测试发送。", nil)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "发送", BBox: [4]int{100, 40, 40, 20}, Source: "yolo"},
	}, nil, "")
	if !noteDesktopMessageTyped("测试发送。") {
		t.Fatal("body did not latch as typed")
	}
	markComputerUseSessionActive()
	noteComputerUseOperatedWindow(42)
	for _, name := range []string{"联系人", "发送文件", "消息里提到发送"} {
		if noteDesktopMessageClickSubmitted(name) {
			t.Fatalf("non-Send label %q latched", name)
		}
	}

	right := cuHandleClick(map[string]interface{}{"ref": "e0", "button": "right"})
	if in.clicks != 1 || strings.Contains(right, "already submitted") || computerUseHaloTarget() != 42 {
		t.Fatalf("right clicks=%d halo=%d msg=%s", in.clicks, computerUseHaloTarget(), right)
	}
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "发送", BBox: [4]int{100, 40, 40, 20}, Source: "yolo"},
	}, nil, "")
	double := cuHandleClick(map[string]interface{}{"ref": "e0", "count": 2})
	if in.clicks != 2 || strings.Contains(double, "already submitted") || computerUseHaloTarget() != 42 {
		t.Fatalf("double clicks=%d halo=%d msg=%s", in.clicks, computerUseHaloTarget(), double)
	}
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "发送", BBox: [4]int{100, 40, 40, 20}, Source: "yolo"},
	}, nil, "")

	msg := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 3 || !strings.Contains(msg, "already submitted") || computerUseHaloTarget() != 0 {
		t.Fatalf("clicks=%d halo=%d msg=%s", in.clicks, computerUseHaloTarget(), msg)
	}
	again := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 3 || !strings.Contains(again, "already submitted") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, again)
	}
}
