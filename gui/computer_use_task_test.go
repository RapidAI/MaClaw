package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
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
	markComputerUseSessionActive()
	setComputerUseOwner("sk-empty")
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
	if !strings.Contains(got, "computer_done rejected") {
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
	markComputerUseSessionActive()
	setComputerUseOwner("sk-pass")
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
	h := &IMMessageHandler{}
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			RequestID:    "req-sync",
			Conversation: RuntimeConversationRef{SessionKey: "sk-sync"},
		},
		ComputerUseRoutingText: "@computer 看屏幕",
	}
	syncComputerUseTurn(h, ctx, "desktop-user", "看屏幕")
	if !ctx.ComputerUseFresh || !ctx.ComputerUseBegun {
		t.Fatalf("fresh=%v begun=%v", ctx.ComputerUseFresh, ctx.ComputerUseBegun)
	}
	if computerUseTaskStateFor("sk-sync") == nil {
		t.Fatal("Begin must key TaskState by SessionKey")
	}
	if computerUseTaskStateFor("desktop-user") != nil {
		t.Fatal("must not key TaskState by UserID when SessionKey is set")
	}
}

func TestComputerUseGateUsesLoopRoutingText(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	h := &IMMessageHandler{}
	ctx := &LoopContext{ComputerUseRoutingText: "@computer 点一下确定"}
	activeCompact, _ := h.gateComputerUse("随便聊聊")
	activeShared, _ := recordComputerUseGate(h, ctx, ctx.ComputerUseRoutingText)
	if activeCompact {
		t.Fatal("compact/chat text should not open CU")
	}
	if !activeShared || !ctx.ComputerUseFresh {
		t.Fatalf("routing text must open CU fresh=%v active=%v", ctx.ComputerUseFresh, activeShared)
	}
	activeAgain, _ := recordComputerUseGate(h, ctx, "随便聊聊")
	if !activeAgain || !ctx.ComputerUseActive {
		t.Fatal("second gate on the same LoopContext must reuse the latched decision")
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
