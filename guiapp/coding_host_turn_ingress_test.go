package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// newHostTurnIngressHandler builds a foreground handler whose owner is a local
// coding workbench session, the exact shape every host-authored coding turn
// (goal round, session wave, degraded retry) enters with.
func newHostTurnIngressHandler(t *testing.T) (*App, *IMMessageHandler, string) {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closeCodingTaskRelationService() })
	handler := &IMMessageHandler{app: app, memory: agent.NewConversationMemory()}
	t.Cleanup(handler.memory.Stop)
	app.imHandler = handler
	owner := projectSessionOwnerID("C:/workspace/host-turn")
	handler.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})
	return app, handler, owner
}

// assertNoOwnerCodingCapabilities proves the turn-end sweep removed every
// credential of one owner: live ingress tokens, the continuation slot, the
// chain proof, the mint budget and the workspace handles. The owner's semantic
// session handle deliberately survives; that is the next turn's continuation.
func assertNoOwnerCodingCapabilities(t *testing.T, app *App, owner string) {
	t.Helper()
	app.codingCaps.mu.Lock()
	defer app.codingCaps.mu.Unlock()
	for token, ingress := range app.codingCaps.ingress {
		if ingress.ownerID == owner {
			t.Fatalf("turn end left ingress %q for owner %q", token, owner)
		}
	}
	for handle, workspace := range app.codingCaps.staticWorkspaces {
		if workspace.ownerID == owner {
			t.Fatalf("turn end left workspace handle %q for owner %q", handle, owner)
		}
	}
	if _, ok := app.codingCaps.continuationTokens[owner]; ok {
		t.Fatalf("turn end left a continuation slot for owner %q", owner)
	}
	if consumed := app.codingCaps.chainConsumed[owner]; len(consumed) != 0 {
		t.Fatalf("turn end left %d chain proofs for owner %q", len(consumed), owner)
	}
	if _, ok := app.codingCaps.continuationBudget[owner]; ok {
		t.Fatalf("turn end left a continuation budget for owner %q", owner)
	}
}

// A host turn has no user send behind it, so the turn boundary itself must
// mint the desktop Coding credential. It must also sweep the whole chain when
// the turn ends: a continuation token minted by the last step bind is still a
// live capability to run implementation work.
func TestHostTurnBoundarySweepsCodingChainAtTurnEnd(t *testing.T) {
	app, handler, owner := newHostTurnIngressHandler(t)

	boundary := handler.enterIMMessageSerializationBoundary(IMUserMessage{
		RequestID: "session-wave", UserID: owner, Platform: sessionRoundContinuationPlatform,
		Text: sessionRoundContinuationPrompt("继续把推理程序补完"),
	}, nil, nil, nil, explicitTaskSlotDecision{})
	token := boundary.CodingIngressToken
	if token == "" {
		boundary.Unlock()
		t.Fatal("host turn reached the loop without a desktop Coding ingress")
	}
	if _, _, handle, _, ok := app.nextDesktopCodingRootStepRelation(token, owner); !ok || !handle.complete() {
		boundary.Unlock()
		t.Fatal("host turn ingress did not bind a desktop relation")
	}
	// Binding re-armed exactly one continuation token on this owner's chain.
	// The turn-end sweep must close it, or a later turn could replay the proof.
	boundary.Unlock()
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(token, owner); ok {
		t.Fatal("turn end left the chained continuation bindable")
	}
	assertNoOwnerCodingCapabilities(t, app, owner)
}

// A message that still carries a token from an earlier boundary is not
// trusted: the turn mints its own credential inside its window, and the
// turn-end sweep closes every token of the owner, including the carried one.
func TestHostTurnBoundaryReplacesStaleCarriedToken(t *testing.T) {
	app, handler, owner := newHostTurnIngressHandler(t)
	stale := app.beginDesktopCodingTaskIngressForRequest(owner, false)
	if stale == "" {
		t.Fatal("request boundary did not mint a token")
	}

	boundary := handler.enterIMMessageSerializationBoundary(IMUserMessage{
		RequestID: "desktop-send", UserID: owner, Platform: desktopPlatform, Text: "继续",
		CodingTaskIngressToken: stale,
	}, nil, nil, nil, explicitTaskSlotDecision{})
	token := boundary.CodingIngressToken
	if token == "" || token == stale {
		boundary.Unlock()
		t.Fatal("turn must mint its own credential, not trust one minted before its window")
	}
	if _, _, handle, _, ok := app.nextDesktopCodingRootStepRelation(token, owner); !ok || !handle.complete() {
		boundary.Unlock()
		t.Fatal("turn credential did not bind a desktop relation")
	}
	boundary.Unlock()
	if _, _, _, _, ok := app.nextDesktopCodingRootStepRelation(stale, owner); ok {
		t.Fatal("turn-end sweep left the carried token bindable")
	}
}

// Background work is never serialized as a turn and must never hold write
// authority for a coding workbench.
func TestHostTurnIngressSkipsBackgroundWork(t *testing.T) {
	app, handler, owner := newHostTurnIngressHandler(t)
	if token := app.attachDesktopCodingIngressForTurn(&IMUserMessage{
		RequestID: "background", UserID: owner, IsBackground: true, Text: "摘要",
	}); token != "" {
		t.Fatalf("background work received a coding ingress token %q", token)
	}
	if token := app.attachDesktopCodingIngressForTurn(&IMUserMessage{
		RequestID: "chat", UserID: "desktop-user", Text: "北京天气",
	}); token != "" {
		t.Fatalf("chat turn received a coding ingress token %q", token)
	}
	if handler == nil {
		t.Fatal("handler not built")
	}
}

// A plan-approval command executes a plan, so it must run inside the owner's
// turn window like any other dispatching turn. This asserts the command waits
// for the window instead of racing it. No pending plan is stored, so the
// command returns its local "no pending plan" answer without dispatching.
func TestCodingPlanApproveTakesOwnerTurnWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
		app.closeCodingTaskRelationService()
	})
	h := &IMMessageHandler{app: app}
	app.imHandler = h
	owner := projectSessionOwnerID("C:/workspace/slash-window")
	h.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})

	window, timeoutResponse := h.beginOwnerTurnWindow(&IMUserMessage{
		RequestID: "holder", UserID: owner, Platform: desktopPlatform, Text: "占住窗口",
	})
	if timeoutResponse != nil {
		t.Fatalf("holder window failed: %+v", timeoutResponse)
	}

	handledC := make(chan bool, 1)
	go func() {
		_, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{
			RequestID: "slash", UserID: owner, Platform: desktopPlatform, Text: "/plan approve",
		}, "/plan approve", nil, nil, nil)
		handledC <- handled
	}()
	select {
	case <-handledC:
		window.Close()
		t.Fatal("plan approval ran outside the owner turn window")
	case <-time.After(150 * time.Millisecond):
	}
	window.Close()
	select {
	case handled := <-handledC:
		if !handled {
			t.Fatal("plan approval command was not handled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("plan approval command did not proceed after the window closed")
	}
}

// A local workbench command must stay immediate: it answers from local state
// and must not queue behind a long-running turn for the same owner.
func TestCodingWorkbenchLocalCommandStaysImmediate(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
		app.closeCodingTaskRelationService()
	})
	h := &IMMessageHandler{app: app}
	app.imHandler = h
	owner := projectSessionOwnerID("C:/workspace/slash-immediate")
	h.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})

	window, timeoutResponse := h.beginOwnerTurnWindow(&IMUserMessage{
		RequestID: "holder", UserID: owner, Platform: desktopPlatform, Text: "占住窗口",
	})
	if timeoutResponse != nil {
		window.Close()
		t.Fatalf("holder window failed: %+v", timeoutResponse)
	}
	defer window.Close()

	done := make(chan bool, 1)
	go func() {
		_, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{
			RequestID: "slash", UserID: owner, Platform: desktopPlatform, Text: "/coding-help",
		}, "/coding-help", nil, nil, nil)
		done <- handled
	}()
	select {
	case handled := <-done:
		if !handled {
			t.Fatal("coding help was not handled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local workbench command waited for the owner turn window")
	}
}

// A command that already runs inside an internal loop (background chat task)
// must not open a second window for the same owner, and must not be handed
// write authority. It proceeds immediately and stays fail-closed.
func TestCodingPlanApproveInsideInternalLoopSkipsOwnerWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
		app.closeCodingTaskRelationService()
	})
	h := &IMMessageHandler{app: app}
	app.imHandler = h
	owner := projectSessionOwnerID("C:/workspace/slash-internal")
	h.storeStickyCodingWorkbenchMemory(owner, stickyCodingWorkbenchMemory{Kind: "local"})

	window, timeoutResponse := h.beginOwnerTurnWindow(&IMUserMessage{
		RequestID: "holder", UserID: owner, Platform: desktopPlatform, Text: "占住窗口",
	})
	if timeoutResponse != nil {
		t.Fatalf("holder window failed: %+v", timeoutResponse)
	}
	defer window.Close()

	internal := NewLoopContext("background-chat-task", 10, nil)
	t.Cleanup(func() {
		internal.Cancel()
		internal.Done()
	})
	done := make(chan bool, 1)
	go func() {
		_, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{
			RequestID: "internal", UserID: owner, Platform: desktopPlatform, Text: "/plan approve",
		}, "/plan approve", internal, nil, nil)
		done <- handled
	}()
	select {
	case handled := <-done:
		if !handled {
			t.Fatal("internal-loop plan approval was not handled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("internal-loop plan approval waited for the owner turn window")
	}
}
