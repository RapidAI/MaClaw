package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

func TestAppendPendingSteerInjections_GuideLaunchAsUserRole(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/remote-demo"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "coding system"},
		map[string]string{"role": "user", "content": "写一个硬件检查工具"},
	}
	out, injected, _ := h.appendPendingSteerInjections(userID, conversation, 1)
	if injected != "使用 c++ 开发" {
		t.Fatalf("injected text = %q, want user-facing guide only", injected)
	}
	if len(out) != 3 {
		t.Fatalf("conversation len = %d, want 3 after guide append", len(out))
	}
	last, ok := out[2].(map[string]string)
	if !ok {
		t.Fatalf("last message type = %T, want map[string]string", out[2])
	}
	if last["role"] != "user" {
		t.Fatalf("guide role = %q, want user", last["role"])
	}
	if !strings.Contains(last["content"], "使用 c++ 开发") {
		t.Fatalf("guide content missing user text: %q", last["content"])
	}
	if !strings.Contains(last["content"], "The user spoke while you were working") {
		t.Fatalf("guide content missing conversational steer context: %q", last["content"])
	}
	if !strings.Contains(last["content"], "Do not emit a canned receipt") {
		t.Fatalf("guide content missing natural-response instruction: %q", last["content"])
	}
	// Pending must be consumed once.
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("pendingInjection should be drained after append")
	}
}

func TestAppendPendingSteerInjections_PreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/local-demo"
	h.accumulatePreLoopGuide(userID, "改用 TypeScript")

	out, injected, _ := h.appendPendingSteerInjections(userID, nil, 0)
	if injected == "" || !strings.Contains(injected, "改用 TypeScript") {
		t.Fatalf("pre-loop guide not applied: %q", injected)
	}
	if len(out) != 1 {
		t.Fatalf("conversation len = %d, want 1", len(out))
	}
	msg := out[0].(map[string]string)
	if msg["role"] != "user" || !strings.Contains(msg["content"], "改用 TypeScript") {
		t.Fatalf("unexpected pre-loop message: %#v", msg)
	}
	if !strings.Contains(msg["content"], "Do not emit a canned receipt") {
		t.Fatalf("pre-loop guide should use conversational steer semantics: %#v", msg)
	}
}

func TestAppendPendingSteerInjections_DiscardsStalePreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/stale"
	h.pendingPreLoopGuide.Store(userID, &preLoopGuideEntry{
		Text:      "too old",
		CreatedAt: time.Now().Add(-preLoopGuideMaxAge - time.Second),
	})
	out, injected, _ := h.appendPendingSteerInjections(userID, nil, 0)
	if injected != "" {
		t.Fatalf("stale pre-loop guide should be discarded, got %q", injected)
	}
	if len(out) != 0 {
		t.Fatalf("stale guide should not append messages, got %#v", out)
	}
}

func TestCodingSubAgentHooks_TransformConversationInjectsGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/hook-demo"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))

	hooks := &codingSubAgentHooks{handler: h, userID: userID}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "hardware tool"},
	}
	next := hooks.TransformConversation(conversation)
	if next == nil {
		t.Fatal("TransformConversation returned nil, want guide-injected conversation")
	}
	if len(next) != 2 {
		t.Fatalf("len = %d, want 2", len(next))
	}
	// Second call with empty pending should not force a change (compaction nil).
	if again := hooks.TransformConversation(next); again != nil {
		t.Fatalf("second transform should be no-op without pending inject, got %#v", again)
	}
}

func TestInjectGuideReference_ActivePureCodingUsesPendingInjection(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/remote-coding"
	ctx := NewLoopContext("chat", 10, nil)
	// Simulate pure coding registration (beginPureCodingRuntime).
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop hardware tool")
	defer cleanup()

	if !h.InjectGuideReference(userID, "使用 c++ 开发") {
		t.Fatal("active pure-coding session should accept guide reference")
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("active loop must not store guide as pre-loop")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("active loop should store guide in pendingInjection")
	}
	text, _ := raw.(string)
	if !isGuideLaunchReferenceInjection(text) {
		t.Fatalf("pending injection is not a guide launch: %q", text)
	}
	if !strings.Contains(stripInjectionPrefix(text), "使用 c++ 开发") {
		t.Fatalf("pending guide missing user text: %q", text)
	}
}

func TestInjectGuideReferenceRejectsReplacementTurn(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/replacement-turn"
	ctx := NewLoopContext("chat", 10, nil)
	ctx.Runtime.RequestID = "request-new"
	cleanup := h.beginPureCodingRuntime(ctx, userID, "replacement task")
	defer cleanup()

	if h.InjectGuideReferenceWithID(userID, "belongs to old turn", "buf-delayed", "request-old") {
		t.Fatal("delayed guide must not attach to a replacement turn")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("request-mismatched guide must remain outside the active loop")
	}
	if !h.InjectGuideReferenceWithID(userID, "belongs to current turn", "buf-current", "request-new") {
		t.Fatal("guide bound to the published request should be accepted")
	}
}

func TestInjectGuideReferenceRejectsAfterFinalResponseSealed(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/final-sealed"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	if !ctx.TrySealReplans(ctx.ReplanRevision()) {
		t.Fatal("failed to seal clean finalization boundary")
	}
	if h.InjectGuideReference(userID, "too late for this turn") {
		t.Fatal("sealed loop must reject guide so GUI retains it for next turn")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("rejected guide must not be stranded in pending injection")
	}
}

func TestLoopContextCancelAndGuideAcceptanceAreAtomic(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx := NewLoopContext("cancel-steer-race", 10, nil)
		start := make(chan struct{})
		accepted := make(chan bool, 1)
		go func() {
			<-start
			_, ok := ctx.TryRequestReplan(nil)
			accepted <- ok
		}()
		cancelled := make(chan struct{})
		go func() {
			<-start
			ctx.Cancel()
			close(cancelled)
		}()
		close(start)
		ok := <-accepted
		<-cancelled
		if !ok && ctx.ReplanRevision() != 0 {
			t.Fatalf("rejected steer advanced revision on iteration %d", i)
		}
		if ctx.AcceptingReplans() {
			t.Fatalf("cancelled loop still advertises steer acceptance on iteration %d", i)
		}
	}
}

func TestInjectSupplementaryRejectsAfterFinalResponseSealedWithoutQueueing(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/supplement-final-sealed"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	if !ctx.TrySealReplans(ctx.ReplanRevision()) {
		t.Fatal("failed to seal clean finalization boundary")
	}
	if h.InjectSupplementary(userID, "too late for this turn") {
		t.Fatal("sealed loop must reject supplementary text")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("rejected supplementary text must not be queued without a consumer")
	}
}

func TestInjectSupplementaryAtomicallyQueuesAndRequestsReplan(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/supplement-replan"
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "develop tool")
	defer cleanup()
	revision := ctx.ReplanRevision()
	if !h.InjectSupplementary(userID, "keep backwards compatibility") {
		t.Fatal("active loop should accept supplementary text")
	}
	if !ctx.ReplanRequestedSince(revision) {
		t.Fatal("accepted supplementary text did not request replan")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok || !strings.Contains(raw.(string), "keep backwards compatibility") {
		t.Fatalf("accepted supplementary text missing from queue: %#v", raw)
	}
}

func TestBeginPureCodingRuntime_NilSafe(t *testing.T) {
	h := &IMMessageHandler{}
	cleanup := h.beginPureCodingRuntime(nil, "u", "task")
	cleanup() // must not panic
	var nilHandler *IMMessageHandler
	cleanup = nilHandler.beginPureCodingRuntime(NewLoopContext("chat", 1, nil), "u", "task")
	cleanup()
}

func TestBeginPureCodingRuntime_PromotesPreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/promote-guide"
	// Simulate guide fired after previous turn ended (pre-loop bag).
	h.accumulatePreLoopGuide(userID, "使用 rust 实现")
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "continue coding")
	defer cleanup()

	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("pre-loop guide should be promoted away")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("expected pendingInjection after promote")
	}
	text, _ := raw.(string)
	if !isGuideLaunchReferenceInjection(text) {
		t.Fatalf("promoted injection is not guide format: %q", text)
	}
	if !strings.Contains(stripInjectionPrefix(text), "使用 rust 实现") {
		t.Fatalf("promoted guide missing user text: %q", text)
	}
}

func TestGuideRejectsUnpublishedPreLoopWindowAndAcceptsAfterPublication(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/start-window-handoff"
	state := h.getSessionLoop(userID)
	state.mu.Lock()
	if h.InjectGuideReference(userID, "arrived during preflight") {
		state.mu.Unlock()
		t.Fatal("unpublished pre-loop work must not claim a guide consumer")
	}
	state.mu.Unlock()
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("rejected pre-loop guide must remain solely in the durable GUI queue")
	}

	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginAgentLoopRuntime(ctx, userID, "task", "desktop")
	defer cleanup()
	if !h.InjectGuideReference(userID, "arrived after publication") {
		t.Fatal("published active loop should accept guide")
	}
	raw, ok := h.pendingInjection.Load(userID)
	if !ok || !strings.Contains(raw.(string), "arrived after publication") {
		t.Fatalf("accepted published guide did not reach active loop: %#v", raw)
	}
}

func TestBeginPureCodingRuntime_DropsVeryStalePreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/stale-promote"
	h.pendingPreLoopGuide.Store(userID, &preLoopGuideEntry{
		Text:      "ancient",
		CreatedAt: time.Now().Add(-3 * time.Minute),
	})
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "task")
	defer cleanup()
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("very stale pre-loop guide must not be promoted")
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("stale pre-loop guide should be deleted on promote attempt")
	}
}

func TestAppendPendingSteerInjections_ReturnsUserFacingGuideText(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/guide-text"
	h.accumulateInjection(userID, buildGuideLaunchInjection("使用 c++ 开发"))
	_, injected, _ := h.appendPendingSteerInjections(userID, nil, 0)
	if injected != "使用 c++ 开发" {
		t.Fatalf("injected text = %q, want user-facing guide only", injected)
	}
}

func TestCodingSubAgentHooks_IgnoresEmptyGuideWrapper(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/empty-guide"
	// Marker + instruction with no user text after strip yields no conversation growth.
	h.accumulateInjection(userID, guideLaunchReferenceMarker+"\n"+guideLaunchReferenceInstruction+"\n")
	hooks := &codingSubAgentHooks{handler: h, userID: userID}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "task"},
	}
	if next := hooks.TransformConversation(conversation); next != nil {
		t.Fatalf("empty guide wrapper should not rewrite conversation, got %#v", next)
	}
}

func TestAppendPendingSteerInjections_PreservesMixedInjectionRolesAndOrder(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/mixed-steer"
	h.accumulateInjection(userID, "[用户补充] keep the public API")
	h.accumulateInjection(userID, buildGuideLaunchInjection("switch storage to SQLite"))
	h.accumulateInjection(userID, "[用户补充] retain migration support")

	out, injected, _ := h.appendPendingSteerInjections(userID, nil, 2)
	if len(out) != 3 {
		t.Fatalf("conversation len = %d, want 3 distinct injections: %#v", len(out), out)
	}
	wantRoles := []string{"system", "user", "system"}
	for i, want := range wantRoles {
		msg, ok := out[i].(map[string]string)
		if !ok || msg["role"] != want {
			t.Fatalf("message %d = %#v, want role %q", i, out[i], want)
		}
	}
	if !strings.Contains(out[1].(map[string]string)["content"], "switch storage to SQLite") {
		t.Fatalf("guide content missing: %#v", out[1])
	}
	if strings.Contains(out[1].(map[string]string)["content"], "retain migration support") {
		t.Fatalf("adjacent system supplement leaked into user-role guide: %#v", out[1])
	}
	if !strings.Contains(injected, "keep the public API") ||
		!strings.Contains(injected, "switch storage to SQLite") ||
		!strings.Contains(injected, "retain migration support") {
		t.Fatalf("combined injected text lost an item: %q", injected)
	}
}

func TestPendingGuideLaunchUserText_ExcludesAdjacentSupplementaryInjection(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/mixed-guide-entry"
	h.accumulateInjection(userID, buildGuideLaunchInjection("use Rust"))
	h.accumulateInjection(userID, "[用户补充] internal system supplement")
	if got := h.pendingGuideLaunchUserText(userID); got != "use Rust" {
		t.Fatalf("pending guide text = %q, want only guide payload", got)
	}
}

func TestClearNonGuidePendingInjection_PreservesEveryGuideBoundary(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/cleanup-mixed-guides"
	h.accumulateInjection(userID, "[用户补充] stale ordinary supplement")
	h.accumulateInjection(userID, buildGuideLaunchInjection("first guide"))
	h.accumulateInjection(userID, "[用户补充] another ordinary supplement")
	h.accumulateInjection(userID, buildGuideLaunchInjection("second guide"))

	h.clearNonGuidePendingInjection(userID)
	raw, ok := h.pendingInjection.Load(userID)
	if !ok {
		t.Fatal("cleanup removed queued guides")
	}
	text, _ := raw.(string)
	parts := splitPendingInjections(text)
	if len(parts) != 2 {
		t.Fatalf("guide count after cleanup = %d, want 2: %q", len(parts), text)
	}
	if got := stripInjectionPrefix(parts[0]); got != "first guide" {
		t.Fatalf("first guide = %q", got)
	}
	if got := stripInjectionPrefix(parts[1]); got != "second guide" {
		t.Fatalf("second guide = %q", got)
	}
	if strings.Contains(text, "ordinary supplement") {
		t.Fatalf("cleanup retained ordinary supplement: %q", text)
	}
}

func TestInjectGuideReference_GoalCommandDoesNotSteerLiveLoop(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	userID := "desktop-user:C:/tasks/goal-beside"
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	ctx := NewLoopContext("chat", 10, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "keep working")
	defer cleanup()

	if !h.InjectGuideReferenceWithID(userID, "/goal 优化四台设备上的推理性能", "launch-goal") {
		t.Fatal("live /goal must be accepted as a host command")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("/goal must not be stored as a steer")
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); ok {
		t.Fatal("/goal must not be stored as a pre-loop guide")
	}
	if ctx.IsCancelled() || ctx.ReplanRevision() != 0 {
		t.Fatalf("live /goal replanned the loop: cancelled=%v revision=%d", ctx.IsCancelled(), ctx.ReplanRevision())
	}
	g := store.Get(userID)
	if g == nil || g.Status != goal.StatusActive || g.Objective != "优化四台设备上的推理性能" {
		t.Fatalf("goal = %+v", g)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("goal continuation must wait until the live coding turn ends")
	}
	if !h.InjectGuideReferenceWithID(userID, "/goal 优化四台设备上的推理性能", "launch-goal") {
		t.Fatal("same launch id should stay accepted")
	}
	if again := store.Get(userID); again == nil || again.GoalID != g.GoalID {
		t.Fatalf("retry created another goal: %+v", again)
	}

	fullID := "desktop-user:C:/tasks/goal-fullwidth"
	t.Cleanup(func() { engine.CancelPending(fullID) })
	fullCtx := NewLoopContext("chat", 10, nil)
	fullCleanup := h.beginPureCodingRuntime(fullCtx, fullID, "keep working")
	defer fullCleanup()
	if !h.InjectGuideReference(fullID, "／goal\u3000优化四台设备上的推理性能") {
		t.Fatal("fullwidth /goal must be accepted")
	}
	fg := store.Get(fullID)
	if fg == nil || fg.Objective != "优化四台设备上的推理性能" || fg.Status != goal.StatusActive {
		t.Fatalf("fullwidth goal = %+v", fg)
	}
	if _, ok := h.pendingInjection.Load(fullID); ok {
		t.Fatal("fullwidth /goal was steered")
	}

	if h.InjectGuideReference("desktop-user:C:/tasks/goal-idle", "/goal 优化四台设备上的推理性能") {
		t.Fatal("idle session must leave /goal for the next real turn")
	}
	if store.Get("desktop-user:C:/tasks/goal-idle") != nil {
		t.Fatal("idle /goal must not create a goal before the GUI sends it")
	}

	sealedID := "desktop-user:C:/tasks/goal-sealed"
	sealedCtx := NewLoopContext("chat", 10, nil)
	sealedCleanup := h.beginPureCodingRuntime(sealedCtx, sealedID, "finishing")
	defer sealedCleanup()
	if !sealedCtx.TrySealReplans(sealedCtx.ReplanRevision()) {
		t.Fatal("seal failed")
	}
	if h.InjectGuideReference(sealedID, "/goal 优化四台设备上的推理性能") {
		t.Fatal("sealed loop must not run /goal; the GUI sends it next")
	}
	if store.Get(sealedID) != nil {
		t.Fatal("sealed /goal created a goal")
	}

	if h.InjectGuideReference(userID, "/help") {
		t.Fatal("a host command other than a live /goal must stay queued for the next turn")
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("/help was steered into the live loop")
	}

	if !h.InjectGuideReference(userID, "继续检查当前实现") {
		t.Fatal("ordinary steer should still enter the live loop")
	}
	raw, ok := h.pendingInjection.Load(userID)
	steered, _ := raw.(string)
	if !ok || !strings.Contains(steered, "继续检查当前实现") || strings.Contains(steered, "/goal") {
		t.Fatalf("ordinary steer = %q", steered)
	}
	if ctx.ReplanRevision() == 0 {
		t.Fatal("ordinary steer should request a replan")
	}

	resumeID := "desktop-user:C:/tasks/goal-resume"
	t.Cleanup(func() { engine.CancelPending(resumeID) })
	created, err := store.Set(resumeID, "先把当前回合做完")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(resumeID, created.GoalID) {
		t.Fatal("pause failed")
	}
	resumeCtx := NewLoopContext("chat", 10, nil)
	resumeCleanup := h.beginPureCodingRuntime(resumeCtx, resumeID, "still working")
	defer resumeCleanup()
	if !h.InjectGuideReference(resumeID, "/goal resume") {
		t.Fatal("resume beside the live loop should be accepted")
	}
	resumed := store.Get(resumeID)
	if resumed == nil || resumed.Status != goal.StatusActive || resumed.GoalID != created.GoalID {
		t.Fatalf("resumed = %+v", resumed)
	}
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[resumeID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("resume must not schedule while the coding turn is live")
	}
	if _, ok := h.pendingInjection.Load(resumeID); ok {
		t.Fatal("resume was steered")
	}
}

func TestGoalPauseAndCancelBesideDesktopTurnDoNotStopIt(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	app := &App{goalContinuation: engine}
	h := &IMMessageHandler{app: app, goalStore: store}
	app.imHandler = h

	pauseID := "desktop-user:C:/tasks/goal-pause"
	t.Cleanup(func() { engine.CancelPending(pauseID) })
	created, err := store.Set(pauseID, "先把当前回合做完")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(pauseID, created.GoalID)
	pauseCtx := NewLoopContext("chat", 10, nil)
	pauseCleanup := h.beginPureCodingRuntime(pauseCtx, pauseID, "keep working")
	defer pauseCleanup()
	h.pendingInjection.Store(pauseID, "steer stays")
	h.pendingAskUser.Store(pauseID, "which file?")
	if !h.InjectGuideReference(pauseID, "/goal pause") {
		t.Fatal("pause beside the live coding turn should be accepted")
	}
	if pauseCtx.IsCancelled() || pauseCtx.ReplanRevision() != 0 {
		t.Fatalf("pause stopped the coding turn: cancelled=%v revision=%d", pauseCtx.IsCancelled(), pauseCtx.ReplanRevision())
	}
	paused := store.Get(pauseID)
	if paused == nil || paused.Status != goal.StatusPaused || paused.GoalID != created.GoalID {
		t.Fatalf("paused = %+v", paused)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[pauseID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("pause left the continuation timer")
	}
	if raw, _ := h.pendingInjection.Load(pauseID); raw != "steer stays" {
		t.Fatalf("pause dropped the coding steer: %v", raw)
	}
	if _, ok := h.pendingAskUser.Load(pauseID); !ok {
		t.Fatal("pause dropped the coding turn's question")
	}

	cancelID := "desktop-user:C:/tasks/goal-cancel"
	t.Cleanup(func() { engine.CancelPending(cancelID) })
	if _, err := store.Set(cancelID, "先把当前回合做完"); err != nil {
		t.Fatal(err)
	}
	cancelCtx := NewLoopContext("chat", 10, nil)
	cancelCleanup := h.beginPureCodingRuntime(cancelCtx, cancelID, "keep working")
	defer cancelCleanup()
	if !h.InjectGuideReference(cancelID, "/goal cancel") {
		t.Fatal("cancel beside the live coding turn should be accepted")
	}
	if cancelCtx.IsCancelled() {
		t.Fatal("cancel stopped the coding turn")
	}
	if store.Get(cancelID) != nil {
		t.Fatal("cancel left the goal")
	}

	statusID := "desktop-user:C:/tasks/goal-status"
	t.Cleanup(func() { engine.CancelPending(statusID) })
	if _, err := store.Set(statusID, "先把当前回合做完"); err != nil {
		t.Fatal(err)
	}
	statusCtx := NewLoopContext("chat", 10, nil)
	statusCleanup := h.beginPureCodingRuntime(statusCtx, statusID, "keep working")
	defer statusCleanup()
	if h.InjectGuideReference(statusID, "/goal status") {
		t.Fatal("status must stay queued so the next turn can show it")
	}
	if h.InjectGuideReference(statusID, "/goal") {
		t.Fatal("help must stay queued so the next turn can show it")
	}
	if _, ok := h.pendingInjection.Load(statusID); ok {
		t.Fatal("status was steered")
	}
	if got := store.Get(statusID); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("status changed the goal: %+v", got)
	}

	dupID := "desktop-user:C:/tasks/goal-dup"
	t.Cleanup(func() { engine.CancelPending(dupID) })
	existing, err := store.Set(dupID, "先把当前回合做完")
	if err != nil {
		t.Fatal(err)
	}
	dupCtx := NewLoopContext("chat", 10, nil)
	dupCleanup := h.beginPureCodingRuntime(dupCtx, dupID, "keep working")
	defer dupCleanup()
	if h.InjectGuideReference(dupID, "/goal 另一件工作") {
		t.Fatal("a refused create must stay queued so the person sees the refusal")
	}
	if got := store.Get(dupID); got == nil || got.GoalID != existing.GoalID || got.Objective != existing.Objective {
		t.Fatalf("refused create changed the goal: %+v", got)
	}
	if _, ok := h.pendingInjection.Load(dupID); ok {
		t.Fatal("refused create was steered")
	}
}

func TestGoalPauseBesideItsContinuationStopsThatTurn(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	app := &App{goalContinuation: engine}
	h := &IMMessageHandler{app: app, goalStore: store}
	app.imHandler = h
	userID := "desktop-user:C:/tasks/goal-own-turn"
	t.Cleanup(func() { engine.CancelPending(userID) })
	if _, err := store.Set(userID, "先把当前回合做完"); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("goal", 4, nil)
	ctx.Platform = "goal-continuation"
	cleanup := h.beginPureCodingRuntime(ctx, userID, "[系统续接][execute] 当前步骤")
	defer cleanup()
	if !h.InjectGuideReference(userID, "/goal pause") {
		t.Fatal("pause of the goal's own turn should be accepted")
	}
	if !ctx.IsCancelled() {
		t.Fatal("pause should stop the goal continuation")
	}
	got := store.Get(userID)
	if got == nil || got.Status != goal.StatusPaused {
		t.Fatalf("paused = %+v", got)
	}
}

func TestGoalSlashSettledKeepsAFailedCreateAndARefusal(t *testing.T) {
	before := &goal.Goal{GoalID: "old", Status: goal.StatusActive}
	failed := &goal.Goal{GoalID: "new", Status: goal.StatusFailed}
	if !goalSlashSettled(goalSlashCreate, nil, failed) {
		t.Fatal("a failed create wrote a row and must not be sent again")
	}
	if goalSlashSettled(goalSlashCreate, before, before) {
		t.Fatal("a refusal must stay queued")
	}
	if goalSlashSettled(goalSlashCreate, nil, nil) {
		t.Fatal("a store miss must stay queued")
	}
	paused := &goal.Goal{GoalID: "old", Status: goal.StatusPaused}
	if !goalSlashSettled(goalSlashPause, before, paused) {
		t.Fatal("pause should count as settled")
	}
	if goalSlashSettled(goalSlashPause, paused, paused) {
		t.Fatal("pause of an already paused goal must stay queued")
	}
	if goalSlashSettled(goalSlashStatus, before, before) || goalSlashSettled(goalSlashHelp, nil, nil) {
		t.Fatal("status and help must stay queued")
	}
}

func TestGoalCreateWaitsForUnpublishedTurn(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	userID := "desktop-user:C:/tasks/goal-inflight"
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	ctx := NewLoopContext("chat", 4, nil)
	h.beginInFlightTurn(userID, "still starting", ctx)
	resp := h.handleGoalCommand(IMUserMessage{UserID: userID, Platform: "desktop", Text: "/goal 先等这一回合"}, "/goal 先等这一回合")
	if resp == nil || !strings.Contains(resp.Text, "目标已开始") {
		t.Fatalf("create = %+v", resp)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("continuation must wait for the unpublished turn")
	}
	h.endInFlightTurn(userID, ctx)
	g := store.Get(userID)
	if g == nil {
		t.Fatal("missing goal")
	}
	h.scheduleGoalContinuationUnlessLive(userID, g.GoalID)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("continuation should arm once the turn is gone")
	}
}

func TestGoalCreateWaitsForSealedTurn(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	userID := "desktop-user:C:/tasks/goal-sealed"
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	ctx := NewLoopContext("coding", 4, nil)
	cleanup := h.beginPureCodingRuntime(ctx, userID, "keep working")
	if !ctx.TrySealReplans(ctx.ReplanRevision()) {
		t.Fatal("seal failed")
	}
	if h.hasActiveLoopForUser(userID) {
		t.Fatal("a sealed loop accepts steering")
	}
	resp := h.handleGoalCommand(IMUserMessage{UserID: userID, Platform: "desktop", Text: "/goal 先等封板这一回合"}, "/goal 先等封板这一回合")
	if resp == nil || !strings.Contains(resp.Text, "目标已开始") {
		t.Fatalf("create = %+v", resp)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("continuation must wait while the sealed turn still holds the session")
	}
	cleanup()
	g := store.Get(userID)
	if g == nil {
		t.Fatal("missing goal")
	}
	h.scheduleGoalContinuationUnlessLive(userID, g.GoalID)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("continuation should arm once the sealed turn is gone")
	}
}

func TestGoalCreateWaitsForTheOwnerWindow(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	userID := "desktop-user:C:/tasks/goal-window"
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	h.setPendingForegroundText(userID, "keep working")
	resp := h.handleGoalCommand(IMUserMessage{UserID: userID, Platform: "desktop", Text: "/goal 先等这一窗口"}, "/goal 先等这一窗口")
	if resp == nil || !strings.Contains(resp.Text, "目标已开始") {
		t.Fatalf("create = %+v", resp)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if scheduled {
		t.Fatal("continuation must wait while the owner window holds the session")
	}
	h.setPendingForegroundText(userID, "")
	g := store.Get(userID)
	if g == nil {
		t.Fatal("missing goal")
	}
	h.scheduleGoalContinuationUnlessLive(userID, g.GoalID)
	engine.mu.Lock()
	_, scheduled = engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("continuation should arm once the owner window releases")
	}
}

func TestGoalResumeArmsWhileContinuationTextHoldsTheWindow(t *testing.T) {
	store := goal.NewStore("")
	engine := NewGoalContinuationEngine(store, nil)
	engine.cooldown = time.Hour
	userID := "desktop-user:C:/tasks/goal-resume-window"
	t.Cleanup(func() { engine.CancelPending(userID) })
	h := &IMMessageHandler{app: &App{goalContinuation: engine}, goalStore: store}
	g, err := store.Set(userID, "先把当前回合做完")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pause(userID, g.GoalID) {
		t.Fatal("pause")
	}
	h.setPendingForegroundText(userID, "[系统续接][execute] 当前步骤")
	resp := h.handleGoalCommand(IMUserMessage{UserID: userID, Platform: "desktop", Text: "/goal resume"}, "/goal resume")
	if resp == nil || !strings.Contains(resp.Text, "目标已恢复") {
		t.Fatalf("resume = %+v", resp)
	}
	engine.mu.Lock()
	_, scheduled := engine.scheduledTimers[userID]
	engine.mu.Unlock()
	if !scheduled {
		t.Fatal("resume must arm while a cancelled continuation still holds its text")
	}
}

func TestGoalPauseStopsTheLiveSlashTwin(t *testing.T) {
	store := goal.NewStore("")
	app := &App{testHomeDir: t.TempDir()}
	engine := NewGoalContinuationEngine(store, app)
	engine.cooldown = time.Hour
	app.goalContinuation = engine
	h := &IMMessageHandler{app: app, goalStore: store}
	app.imHandler = h
	const front = `desktop-user:F:/bird2`
	const back = `desktop-user:F:\bird2`
	t.Cleanup(func() {
		engine.CancelPending(front)
		engine.CancelPending(back)
	})
	stale, err := store.Set(front, "旧的暂停")
	if err != nil {
		t.Fatal(err)
	}
	if !store.UpdateStatus(front, stale.GoalID, goal.StatusPaused, "旧的") {
		t.Fatal("pause stale")
	}
	g, err := store.Set(back, "正在做的小鸟")
	if err != nil {
		t.Fatal(err)
	}
	engine.scheduleDelayed(back, g.GoalID)
	refused := h.handleGoalCommand(IMUserMessage{UserID: front, Platform: "desktop", Text: "/goal 另一件工作"}, "/goal 另一件工作")
	if refused == nil || !strings.Contains(refused.Error, "已有活跃目标") {
		t.Fatalf("create = %+v", refused)
	}
	if got := store.Get(front); got == nil || got.GoalID != stale.GoalID || got.Status != goal.StatusPaused {
		t.Fatalf("create wrote over the other spelling: %+v", got)
	}

	ctx := NewLoopContext("coding", 4, nil)
	cleanup := h.beginPureCodingRuntime(ctx, front, "keep working")
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if !h.InjectGuideReference(front, "/goal pause") {
		t.Fatal("pause of the live slash twin should be accepted")
	}
	if ctx.IsCancelled() {
		t.Fatal("pause cancelled the desktop turn")
	}
	if got := store.Get(back); got == nil || got.Status != goal.StatusPaused || got.Objective != "正在做的小鸟" {
		t.Fatalf("live row = %+v", got)
	}
	if got := store.Get(front); got == nil || got.GoalID != stale.GoalID || got.Objective != "旧的暂停" {
		t.Fatalf("older twin changed: %+v", got)
	}
	engine.mu.Lock()
	_, backTimer := engine.scheduledTimers[back]
	engine.mu.Unlock()
	if backTimer {
		t.Fatal("pause left the live row's timer")
	}

	resumed := h.handleGoalCommand(IMUserMessage{UserID: front, Platform: "desktop", Text: "/goal resume"}, "/goal resume")
	if resumed == nil || !strings.Contains(resumed.Text, "目标已恢复") {
		t.Fatalf("resume = %+v", resumed)
	}
	if got := store.Get(back); got == nil || got.Status != goal.StatusActive {
		t.Fatalf("resumed row = %+v", got)
	}
	engine.mu.Lock()
	_, backTimer = engine.scheduledTimers[back]
	engine.mu.Unlock()
	if backTimer {
		t.Fatal("resume armed a continuation beside the live desktop turn")
	}
	cleanup()
	cleanup = nil
	kept := store.Get(back)
	if kept == nil {
		t.Fatal("missing goal")
	}
	h.scheduleGoalContinuationUnlessLive(kept.UserID, kept.GoalID)
	engine.mu.Lock()
	_, backTimer = engine.scheduledTimers[back]
	engine.mu.Unlock()
	if !backTimer {
		t.Fatal("continuation should arm once the desktop turn is gone")
	}
}
