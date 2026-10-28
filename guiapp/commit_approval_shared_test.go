package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// testAppForCommitApproval isolates the approval store and audit log, and
// stops the memory store so the temp dir can be removed.
func testAppForCommitApproval(t *testing.T) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
		if app.auditLog != nil {
			_ = app.auditLog.Close()
		}
	})
	return app
}

// A desktop turn resolves a human-class call through the same dialog the
// coding hosts use. Allow runs the blocked call in place: the tool name and
// arguments come from this call, so the resumed model turn cannot re-plan into
// a different mechanism or lose the tool from the surface.
func TestSharedCommitAskRunsTheCallThroughTheDesktopDialog(t *testing.T) {
	app := testAppForCommitApproval(t)
	handler := &IMMessageHandler{app: app}
	previous := desktopApprovalDialogAvailable
	desktopApprovalDialogAvailable = func(*App) bool { return true }
	t.Cleanup(func() { desktopApprovalDialogAvailable = previous })

	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &sharedAgentLoopCallbacks{
		handler:  handler,
		loopCtx:  loopCtx,
		userID:   "desktop-user:" + t.TempDir(),
		platform: desktopPlatform,
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	t.Cleanup(func() {
		pendingScopeApprovals.Range(func(key, value any) bool {
			if pending, ok := value.(*pendingScopeApproval); ok && pending.Request.Kind == commitShapeApprovalKind {
				pendingScopeApprovals.Delete(key)
			}
			return true
		})
	})

	first := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c","body":"secret"}`)
		first <- commitGateResult{res, blocked}
	}()
	ResolveScopeApproval(waitCommitShapePending(t, cb.CommitWorkspaceRoot()), "本任务内允许")
	if got := <-first; got.blocked || got.res.Result != "" {
		t.Fatalf("approved call result=%+v blocked=%v", got.res, got.blocked)
	}
	// 本任务内允许 stores the shape, so the next call must not open a dialog.
	if again, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"z@y.x","body":"other"}`); blocked {
		t.Fatalf("same shape asked again: %+v", again)
	}
}

// Without the desktop event bus no dialog can appear. The gate must fall back
// to the ask_user card instead of blocking a turn nobody can answer.
func TestSharedCommitAskKeepsTheCardWithoutDesktopDialog(t *testing.T) {
	app := testAppForCommitApproval(t)
	handler := &IMMessageHandler{app: app}
	previous := desktopApprovalDialogAvailable
	desktopApprovalDialogAvailable = func(*App) bool { return false }
	t.Cleanup(func() { desktopApprovalDialogAvailable = previous })

	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &sharedAgentLoopCallbacks{
		handler:  handler,
		loopCtx:  loopCtx,
		userID:   "desktop-user:" + t.TempDir(),
		platform: desktopPlatform,
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })

	res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c"}`)
	if !blocked || !agent.IsAskUserResult(res.Result) {
		t.Fatalf("expected the ask card, got result=%+v blocked=%v", res, blocked)
	}
}

// The commit card still carries its three actions for channels that answer by
// text. The desktop dialog is not the only consumer of buildAskUserResponse.
func TestCommitAskCardResponseCarriesApprovalActions(t *testing.T) {
	handler := &IMMessageHandler{app: testAppForCommitApproval(t)}
	ask := &agent.AskUserRequest{
		Question:  agent.CommitAskQuestion("mailbox_send") + "\na@b.com · subj",
		Options:   []string{"仅这一次", "本任务内允许", "拒绝"},
		InputType: "choice",
		Context:   agent.CommitApprovalContext("desktop\nuser\nD:/work", "mailbox_send\nto\nnone", "nonce-1"),
	}
	out := handler.handleAgentLoopAskUserToolResult(
		"desktop-user:test", desktopPlatform, "",
		agent.AskUserResultMarker(ask), false, "call-1",
		nil, nil, nil, nil, false,
	)
	if out.Response == nil || len(out.Response.Actions) != 3 {
		t.Fatalf("ask response = %+v", out.Response)
	}
	payload, err := json.Marshal(out.Response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(payload), `"actions":[`) || !strings.Contains(string(payload), "本任务内允许") {
		t.Fatalf("serialized card lost its actions: %s", payload)
	}
}

// 完全控制 authorizes this task tab, so a human-class call under it must not
// open another card. The skip is recorded on the same audit trail as a
// person's allow.
func TestSharedCommitFullControlRunsTheCallWithoutACard(t *testing.T) {
	app := testAppForCommitApproval(t)
	handler := &IMMessageHandler{app: app}
	userID := "desktop-user:" + t.TempDir()
	handler.setStickyCodingSessionPermissionMode(userID, "full", "", "")
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(userID) })

	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &sharedAgentLoopCallbacks{
		handler:  handler,
		loopCtx:  loopCtx,
		userID:   userID,
		platform: desktopPlatform,
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })

	if !cb.CommitFullControlGranted() {
		t.Fatal("完全控制 tier was not read for this task tab")
	}
	res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c"}`)
	if blocked || res.Result != "" {
		t.Fatalf("full-control call result=%+v blocked=%v", res, blocked)
	}

	app.ensureAuditLog()
	if app.auditLog == nil {
		t.Fatal("audit log was not created")
	}
	entries, err := app.auditLog.Query(security.AuditFilter{})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	sawFullControl := false
	for _, entry := range entries {
		if entry.ToolName == "mailbox_send" && entry.Arguments["source"] == "full_control" {
			sawFullControl = true
		}
	}
	if !sawFullControl {
		t.Fatalf("full-control skip was not audited: %+v", entries)
	}
}

// Deny is remembered: the blocked call never runs and the same shape is
// refused without opening a second dialog.
func TestSharedCommitDenyIsRememberedWithoutAnotherDialog(t *testing.T) {
	app := testAppForCommitApproval(t)
	handler := &IMMessageHandler{app: app}
	previous := desktopApprovalDialogAvailable
	desktopApprovalDialogAvailable = func(*App) bool { return true }
	t.Cleanup(func() { desktopApprovalDialogAvailable = previous })

	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &sharedAgentLoopCallbacks{
		handler:  handler,
		loopCtx:  loopCtx,
		userID:   "desktop-user:" + t.TempDir(),
		platform: desktopPlatform,
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })
	t.Cleanup(func() {
		pendingScopeApprovals.Range(func(key, value any) bool {
			if pending, ok := value.(*pendingScopeApproval); ok && pending.Request.Kind == commitShapeApprovalKind {
				pendingScopeApprovals.Delete(key)
			}
			return true
		})
	})

	denied := make(chan commitGateResult, 1)
	go func() {
		res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c","body":"secret"}`)
		denied <- commitGateResult{res, blocked}
	}()
	ResolveScopeApproval(waitCommitShapePending(t, cb.CommitWorkspaceRoot()), "拒绝")
	if got := <-denied; !got.blocked || !strings.Contains(got.res.Result, "已拒绝") {
		t.Fatalf("denied call result=%+v blocked=%v", got.res, got.blocked)
	}
	again, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"z@y.x","body":"other"}`)
	if !blocked || !strings.Contains(again.Result, "已拒绝") {
		t.Fatalf("remembered deny result=%+v blocked=%v", again, blocked)
	}
}

// An IM turn has no panel dialog and no composer tier. It keeps the ask card
// and must not inherit the desktop full-control skip.
func TestSharedCommitAskStaysOnTheCardForIMTurns(t *testing.T) {
	app := testAppForCommitApproval(t)
	handler := &IMMessageHandler{app: app}
	userID := "desktop-user:" + t.TempDir()
	handler.setStickyCodingSessionPermissionMode(userID, "full", "", "")
	t.Cleanup(func() { handler.clearStickyCodingWorkbenchMemory(userID) })
	previous := desktopApprovalDialogAvailable
	desktopApprovalDialogAvailable = func(*App) bool { return true }
	t.Cleanup(func() { desktopApprovalDialogAvailable = previous })

	loopCtx := &LoopContext{CancelC: make(chan struct{})}
	t.Cleanup(func() { closeLoopCtx(loopCtx) })
	cb := &sharedAgentLoopCallbacks{
		handler:  handler,
		loopCtx:  loopCtx,
		userID:   userID,
		platform: "weixin",
	}
	t.Cleanup(func() { forgetCommitLedger(cb.CommitTaskKey()) })

	if cb.CommitFullControlGranted() {
		t.Fatal("IM turn inherited the desktop full-control tier")
	}
	res, blocked := agent.ApplyCommitGate(cb, "mailbox_send", `{"to":"a@b.c"}`)
	if !blocked || !agent.IsAskUserResult(res.Result) {
		t.Fatalf("IM ask result=%+v blocked=%v", res, blocked)
	}
	pendingScopeApprovals.Range(func(key, value any) bool {
		if pending, ok := value.(*pendingScopeApproval); ok && pending.Request.Kind == commitShapeApprovalKind {
			t.Fatalf("IM turn opened a desktop dialog: %+v", pending.Request)
		}
		return true
	})
}

// A reloaded panel adopts whatever is still waiting. The list is FIFO, reports
// the human-call shape without a countdown, and drops an answered request.
func TestListPendingScopeApprovalsReportsOutstandingRequests(t *testing.T) {
	commitID := storePendingScopeApproval(nil, ScopeApprovalRequest{
		ToolName: "mailbox_send",
		Path:     "a@b.c",
		Kind:     commitShapeApprovalKind,
	}, make(chan ScopeApprovalDecision, 1))
	timedID := storePendingScopeApproval(nil, ScopeApprovalRequest{
		ToolName:  "bash",
		Path:      "git push",
		Kind:      localHighRiskApprovalKind,
		AutoAllow: true,
	}, make(chan ScopeApprovalDecision, 1))
	t.Cleanup(func() {
		pendingScopeApprovals.Delete(commitID)
		pendingScopeApprovals.Delete(timedID)
	})
	// Stored back-to-back: pin the order the test asserts.
	if item, ok := pendingScopeApprovals.Load(commitID); ok {
		item.(*pendingScopeApproval).CreatedAt = time.Now().Add(-2 * time.Second)
	}
	if item, ok := pendingScopeApprovals.Load(timedID); ok {
		item.(*pendingScopeApproval).CreatedAt = time.Now().Add(-time.Second)
	}

	views := (&App{}).ListPendingScopeApprovals()
	var commitView, timedView *ScopeApprovalView
	for i := range views {
		switch views[i].ID {
		case commitID:
			commitView = &views[i]
		case timedID:
			timedView = &views[i]
		}
	}
	if commitView == nil || timedView == nil {
		t.Fatalf("pending approvals missing: %+v", views)
	}
	if views[0].ID != commitID {
		t.Fatalf("oldest approval is not first: %+v", views)
	}
	if commitView.TimeoutSeconds != 0 || commitView.RemainingSeconds != 0 {
		t.Fatalf("human-call approval counts down: %+v", commitView)
	}
	if timedView.TimeoutSeconds != int(scopeApprovalTimeout/time.Second) ||
		timedView.RemainingSeconds <= 0 || timedView.RemainingSeconds > timedView.TimeoutSeconds {
		t.Fatalf("timed approval window = %+v", timedView)
	}

	ResolveScopeApproval(commitID, "deny")
	for _, view := range (&App{}).ListPendingScopeApprovals() {
		if view.ID == commitID {
			t.Fatalf("answered approval is still listed: %+v", view)
		}
	}
}
