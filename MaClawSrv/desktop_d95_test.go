package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// run_5ecb9d6d (papers 11-11): every transport failure is infrastructure.
func TestDesktopD95TransportErrorsAreInfra(t *testing.T) {
	for _, msg := range []string{
		"desktop browser: CDP WebSocket connection failed: cdp dial: websocket: bad handshake",
		"desktop browser: CDP WebSocket connection failed: cdp dial: read tcp 172.21.0.4:44922->152.136.22.50:443: i/o timeout",
		"discover targets: Get \"http://x/json\": EOF",
	} {
		err := errors.New(msg)
		if got := desktopErrorClass("", err); got != "infra_timeout" {
			t.Errorf("%q: class %q, want infra_timeout", msg, got)
		}
		if !desktopTransportFailure(err) {
			t.Errorf("%q: not a transport failure", msg)
		}
	}
	if desktopTransportFailure(errors.New("element e12 not found")) {
		t.Fatal("a page error is not a transport failure")
	}
}

func TestDesktopD95HealAllowedAndBudget(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-d95-heal"}
	bad := errors.New("cdp dial: websocket: bad handshake")
	if desktopHealAllowed(scope, "bash", bad) {
		t.Fatal("bash does not reach the browser over CDP")
	}
	if desktopHealAllowed(scope, "probe", errors.New("element not found")) {
		t.Fatal("a page error is not healed")
	}
	n := 0
	for desktopHealAllowed(scope, "probe", bad) {
		n++
		if n > 50 {
			break
		}
	}
	if n != desktopHealPerRun {
		t.Fatalf("heals %d, want %d", n, desktopHealPerRun)
	}
	if desktopHealTake(scope.RunID) {
		t.Fatal("no let-through after the budget is spent")
	}
	// A publish whose run already has a record (it may have submitted) is
	// never re-run.
	pub := agentruntime.Scope{RunID: "run-d95-heal-pub"}
	desktopPublishRecordUpdate(pub, func(r *desktopPublishRecord) { r.State = browser.PublishSubmitted })
	defer desktopPublishRunTake(pub.RunID)
	if desktopHealAllowed(pub, "publish", bad) {
		t.Fatal("publish with a record must not be re-run")
	}
}

func TestDesktopD95HealResetsRouteAndAsksSession(t *testing.T) {
	oldPause, oldSession := desktopHealPause, desktopHealSession
	defer func() { desktopHealPause, desktopHealSession = oldPause, oldSession }()
	desktopHealPause = 0
	asked := 0
	desktopHealSession = func(context.Context, agentruntime.Scope, string) error { asked++; return nil }
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-heal", RunID: "run-d95-heal-reset"}
	key, _ := desktopUserKey(scope)
	desktopBindings.Store(key, &desktopBinding{addr: "x"})
	desktopRoundWakes.Store(scope.RunID, &desktopRoundWake{have: true})
	defer desktopRoundWakes.Delete(scope.RunID)
	did := desktopHealTransport(context.Background(), scope)
	if _, still := desktopBindings.Load(key); still {
		t.Fatal("binding kept")
	}
	v, _ := desktopRoundWakes.Load(scope.RunID)
	if v.(*desktopRoundWake).have {
		t.Fatal("cached endpoint kept")
	}
	if asked != 1 || !strings.Contains(did, "asked for the desktop session again") {
		t.Fatalf("session not asked again: %d %q", asked, did)
	}
}

func TestDesktopD95InfraBlockClearsAndLetsThrough(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-block", InstanceID: "i", RunID: "run-d95-block"}
	bad := errors.New("desktop browser: CDP WebSocket connection failed: cdp dial: websocket: bad handshake")
	args := map[string]any{"action": "publish", "title": "x"}
	desktopFailClassRecord(scope, "publish", args, "", bad)
	desktopFailClassRecord(scope, "publish", args, "", bad)
	desktopNoteRoute(scope.RunID, "publish", bad)
	// Blocked by infrastructure alone: one call is let through on the
	// run's heal budget instead of refusing for the rest of the run.
	if err := desktopFailClassCheck(scope, "publish", args); err != nil {
		t.Fatalf("infra block must let one call through: %v", err)
	}
	if !desktopClearInfraBlocks(desktopOwner(scope)) {
		t.Fatal("nothing cleared")
	}
	st := desktopRunFor(desktopOwner(scope))
	st.mu.Lock()
	n := len(st.blocked)
	st.mu.Unlock()
	if n != 0 {
		t.Fatalf("blocks left: %d", n)
	}
}

func TestDesktopD95RouteDownNoteNotDirtyForm(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", "bot_d95"), RunID: "run-d95-route"}
	desktopNoteRoute(scope.RunID, "probe", errors.New("cdp dial: websocket: bad handshake"))
	got := desktopUnstickAfterTimeout(scope, "probe", nil)
	if got != desktopRouteDownNote || strings.Contains(got, "holds content you entered") {
		t.Fatalf("note: %q", got)
	}
	desktopNoteRoute(scope.RunID, "probe", nil)
	if desktopRouteDown(scope.RunID) {
		t.Fatal("a step that reached the browser ends route-down")
	}
}

// A URL in a shell transcript is not the page the browser is on: it must
// not move the publish plan to compose.
func TestDesktopD95PhaseNotFromBash(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-phase", InstanceID: "i", RunID: "run-d95-phase"}
	desktopProgressNote(scope, "bash", `[{"url": "https://editor.csdn.net/md?articleId=170404442"}]`, nil)
	_, p := desktopProgressFor(scope)
	if p != nil && p.lastURL != "" {
		t.Fatalf("bash output set lastURL %q", p.lastURL)
	}
	desktopProgressNote(scope, "navigate", "https://editor.csdn.net/md/", nil)
	_, p = desktopProgressFor(scope)
	if p == nil || p.lastURL == "" {
		t.Fatal("navigate must set the page")
	}
	desktopProgressNote(scope, "probe", "", errors.New("cdp dial: websocket: bad handshake"))
	_, p = desktopProgressFor(scope)
	if p.lastURL != "" {
		t.Fatalf("transport failure must forget the page, got %q", p.lastURL)
	}
}

func TestDesktopD95PageListNeedsAttachedTarget(t *testing.T) {
	targets := []browser.TargetInfo{{ID: "A", Type: "page", URL: "https://zhihu.com"}}
	if desktopPagesBelongToAttached(targets, "") {
		t.Fatal("no attached id: list unproven")
	}
	if desktopPagesBelongToAttached(targets, "B") {
		t.Fatal("another display's list")
	}
	if !desktopPagesBelongToAttached(targets, "A") {
		t.Fatal("own list")
	}
}

func d95Ledger(t *testing.T) {
	dir := t.TempDir()
	old := desktopLedgerDir
	desktopLedgerDir = func() string { return dir }
	t.Cleanup(func() { desktopLedgerDir = old })
}

// The bot published through bash; the success page in its transcript is
// the proof: ledger, outcome and a correction of a reply that denies it.
func TestDesktopD95EvidenceFromBashSettlesOutcome(t *testing.T) {
	d95Ledger(t)
	run := "run-d95-evidence"
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-ev", InstanceID: "i", RunID: run}
	desktopNoteTaskSpec(run, "搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn")
	defer desktopTaskSpecs.Delete(run)
	out := `{"url": "https://mp.csdn.net/mp_blog/creation/success/170476928"} public https://blog.csdn.net/daniel/article/details/170476928`
	link := desktopObservePublishEvidence(scope, "bash", out)
	if link != "https://blog.csdn.net/daniel/article/details/170476928" {
		t.Fatalf("link %q", link)
	}
	if got := desktopObservePublishEvidence(scope, "screenshot", out); got != "" {
		t.Fatal("screenshot text is not evidence")
	}
	outcome, why := desktopReconcileOutcome(run, agentservice.TaskOutcomeBlocked, "publish blocked")
	if outcome != agentservice.TaskOutcomeDone || !strings.Contains(why, "170476928") {
		t.Fatalf("outcome %q %q", outcome, why)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "本次没有发布或验证，浏览器控制通道断了。"})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeDone {
		t.Fatalf("metadata %v", md)
	}
	if n := md[agentservice.TaskOutcomeNoticeKey]; !strings.Contains(n, "更正：已发布并确认") || !strings.Contains(n, "170476928") {
		t.Fatalf("notice %q", n)
	}
	// Another run does not take this post as its own.
	other := agentruntime.Scope{TenantID: "t", UserID: "u-d95-ev", InstanceID: "i", RunID: "run-d95-evidence-2"}
	desktopNoteTaskSpec(other.RunID, "发到csdn")
	defer desktopTaskSpecs.Delete(other.RunID)
	if got := desktopObservePublishEvidence(other, "bash", out); got != "" {
		t.Fatalf("other run took the post: %q", got)
	}
}

func TestDesktopD95EvidencePartialAcrossSites(t *testing.T) {
	d95Ledger(t)
	run := "run-d95-partial"
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-part", InstanceID: "i", RunID: run}
	desktopNoteTaskSpec(run, multiSiteReq)
	defer desktopTaskSpecs.Delete(run)
	desktopObservePublishEvidence(scope, "probe", "https://mp.csdn.net/mp_blog/creation/success/42")
	outcome, why := desktopReconcileOutcome(run, "", "")
	if outcome != agentservice.TaskOutcomePartial || !strings.Contains(why, "csdn") || !strings.Contains(why, "zhihu") {
		t.Fatalf("outcome %q %q", outcome, why)
	}
}

// A finished run that worked carries an outcome too (Bot 2 had none).
func TestDesktopD95SuccessHasOutcome(t *testing.T) {
	run := "run-d95-success"
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	defer func() { desktopRecStateMu.Lock(); delete(desktopRecRunSteps, run); desktopRecStateMu.Unlock() }()
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "done", "text": "贪吃蛇做好了，截图在这条消息里。", "note": "关于地址要求：没有代理替换。"}); err != nil {
		t.Fatal(err)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "贪吃蛇做好了，截图在这条消息里。\n关于地址要求：没有代理替换。"})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeDone {
		t.Fatalf("metadata %v", md)
	}
	if !strings.HasPrefix(md["send_to_user"], "做成了。") || !strings.Contains(md["send_to_user"], "贪吃蛇做好了，截图在这条消息里。") {
		t.Fatalf("sentence %v", md)
	}
}

func TestNoFinishSuccessfulRunIsDoneUndeclared(t *testing.T) {
	run := "run-no-finish"
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	defer func() { desktopRecStateMu.Lock(); delete(desktopRecRunSteps, run); desktopRecStateMu.Unlock() }()
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "贪吃蛇做好了。"})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeDone || md["task_outcome_evidence"] != "undeclared" {
		t.Fatalf("metadata %v", md)
	}
}

func TestDeclaredFailedIsBlocked(t *testing.T) {
	run := "run-finish-failed"
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	defer desktopRecoveryEndRun(run)
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "failed", "text": "这件没做成。"}); err != nil {
		t.Fatal(err)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "这件没做成。"})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeBlocked || !strings.Contains(md["task_outcome_evidence"], "declared failed") {
		t.Fatalf("metadata %v", md)
	}
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "ask_user", "text": "把密码给我。"}); err != nil {
		t.Fatal(err)
	}
	md = desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "把密码给我。"})
	if md[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeBlocked || !strings.Contains(md["task_outcome_evidence"], "ask_user") {
		t.Fatalf("ask without tool %v", md)
	}
}

func TestPlainChatIsNotForcedThroughFinish(t *testing.T) {
	run := "run-plain-chat"
	defer desktopRecoveryEndRun(run)
	if agentservice.DesktopTurnNeedsFinish(run) {
		t.Fatal("a reply with no desktop step was forced through finish")
	}
	if _, err := desktopFinishTurn(agentruntime.Scope{RunID: run}, map[string]any{"outcome": "done", "text": "你好"}); err != nil {
		t.Fatal(err)
	}
	if md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "你好"}); md != nil {
		t.Fatalf("plain chat outcome %#v", md)
	}
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	desktopClearFinish(run)
	if !agentservice.DesktopTurnNeedsFinish(run) {
		t.Fatal("a turn that did desktop work was not offered finish")
	}
}

func TestDeclaredDoneWithoutWorkIsNotUndeclared(t *testing.T) {
	run := "run-finish-idle"
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-finish-idle", RunID: run}
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	defer desktopRecoveryEndRun(run)
	desktopNoteStepResult(scope, fmt.Errorf("step failed"))
	if _, err := desktopFinishTurn(scope, map[string]any{"outcome": "done", "text": "做好了。"}); err != nil {
		t.Fatal(err)
	}
	md := desktopTaskOutcomeMetadata(run, agentservice.Message{Content: "做好了。"})
	if md[agentservice.TaskOutcomeKey] == agentservice.TaskOutcomeDone || strings.Contains(md["task_outcome_evidence"], "undeclared") || strings.Contains(md["task_outcome_evidence"], "without a declared outcome") {
		t.Fatalf("metadata %v", md)
	}
	if !strings.Contains(md["task_outcome_evidence"], "no successful desktop work") {
		t.Fatalf("metadata %v", md)
	}
}

func TestDesktopD95StaleAskAndTakeoverCleared(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-stale", InstanceID: "i-d95", SessionID: "s-d95", RunID: "run-d95-stale"}
	noteDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID, "浏览器控制通道断了")
	desktopbot.NoteDangerAsk(desktopDangerAskKeys(scope), desktopbot.ConnectorAsk{Text: "我想在桌面上跑一条命令…可以吗？"})
	desktopNoteAskOrTakeover(scope.RunID)
	desktopNoteProgressAfterAsk(scope, "bash", nil)
	if _, ok := desktopAttention.Load(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID)); !ok {
		t.Fatal("cleared after one step")
	}
	desktopNoteProgressAfterAsk(scope, "probe", errors.New("x"))
	desktopNoteProgressAfterAsk(scope, "screenshot", nil) // observing is not going on
	if _, ok := desktopAttention.Load(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID)); !ok {
		t.Fatal("observing must not clear the takeover")
	}
	desktopNoteProgressAfterAsk(scope, "script", nil)
	if _, ok := desktopAttention.Load(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID)); ok {
		t.Fatal("stale takeover text kept")
	}
	md := map[string]string{}
	desktopbot.ApplyDangerAsk(md, desktopDangerAskKeys(scope)...)
	if len(md) != 0 {
		t.Fatalf("stale command question kept: %v", md)
	}
}

func TestDesktopD95RecoverNothingWaiting(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-d95-rec", InstanceID: "i", RunID: "run-d95-rec"}
	desktopNoteRoute(scope.RunID, "publish", errors.New("cdp dial: websocket: bad handshake"))
	out := desktopRecoverNothingWaiting(scope, map[string]any{"goal": "publish CSDN article"})
	for _, w := range []string{"no publish goal", "did not reach", "reconnects", "action=publish"} {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q: %s", w, out)
		}
	}
}

func TestDesktopD95DeliverHintNamesBothCaps(t *testing.T) {
	err := desktopDeliverSizeHint(map[string]any{"action": "deliver"}, errors.New("HTTP 400: file is too large"))
	if !strings.Contains(err.Error(), "525000") || !strings.Contains(err.Error(), "8388608") {
		t.Fatalf("hint: %v", err)
	}
	if desktop.FileBytesMax < 300_000 {
		t.Fatalf("a 203 KB docx must fit: cap %d", desktop.FileBytesMax)
	}
}

// End to end through the desktop tool: a dial failure is healed and the
// step run again; while the route stays dead the publish block is lifted
// as soon as a step reaches the browser again.
func TestDesktopD95HealRetriesThroughTool(t *testing.T) {
	req, calls := deadlockEnv(t)
	oldPause, oldSession, oldOp := desktopHealPause, desktopHealSession, operateDesktopFunc
	defer func() { desktopHealPause, desktopHealSession, operateDesktopFunc = oldPause, oldSession, oldOp }()
	desktopHealPause = 0
	heals := 0
	desktopHealSession = func(context.Context, agentruntime.Scope, string) error { heals++; return nil }
	dead := true
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		a := desktopArg(args, "action")
		calls[a]++
		if dead && a != "screenshot" {
			return "", errors.New("desktop browser: CDP WebSocket connection failed: cdp dial: websocket: bad handshake")
		}
		if a == "publish" {
			return "", errors.New("publish step submit failed: test")
		}
		return "page https://huggingface.co/papers", nil
	}
	out, err := invoke(t, req, map[string]any{"action": "navigate", "url": "https://huggingface.co/papers"})
	if err == nil {
		t.Fatalf("dead route navigate ok: %s", out)
	}
	if calls["navigate"] != 2 || heals != 1 || !strings.Contains(err.Error(), "still fails") {
		t.Fatalf("navigate calls=%d heals=%d err=%v", calls["navigate"], heals, err)
	}
	dead = false
	out, err = invoke(t, req, map[string]any{"action": "probe"})
	if err != nil {
		t.Fatalf("probe after the route came back: %v", err)
	}
	if !desktopRouteEverFailed(req.Scope.RunID) || desktopRouteDown(req.Scope.RunID) {
		t.Fatal("route state")
	}
	_ = out
}
