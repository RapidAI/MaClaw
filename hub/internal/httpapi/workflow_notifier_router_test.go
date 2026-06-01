package httpapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// routerWiringDeviceStub mirrors *device.Service in the one respect that matters
// for the router wiring assertion: it satisfies BOTH machineCommandSender
// (SendToMachine) and machinePresenceChecker (IsMachineOnline). The production
// router builds the notifier with a single *device.Service passed to both
// constructor steps — NewHubNotifier(deviceSvc).WithPresence(deviceSvc) — so the
// test passes a single stub instance to both, faithfully reproducing that
// construction expression.
//
// Named distinctly (routerWiring*) to avoid duplicate-symbol collisions with the
// test doubles in workflow_notifier_test.go and workflow_notifier_dispatch_test.go,
// which are added concurrently in this same package.
type routerWiringDeviceStub struct {
	mu        sync.Mutex
	sendCalls []routerWiringSend
	online    bool
}

type routerWiringSend struct {
	machineID string
	msg       map[string]any
}

func (s *routerWiringDeviceStub) SendToMachine(machineID string, msg any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	typed, _ := msg.(map[string]any)
	s.sendCalls = append(s.sendCalls, routerWiringSend{machineID: machineID, msg: typed})
	return nil
}

func (s *routerWiringDeviceStub) IsMachineOnline(string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.online
}

func (s *routerWiringDeviceStub) sendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sendCalls)
}

// Compile-time proof the stub satisfies both abstractions the same way
// *device.Service does, so it is a faithful stand-in for deviceSvc.
var (
	_ machineCommandSender   = (*routerWiringDeviceStub)(nil)
	_ machinePresenceChecker = (*routerWiringDeviceStub)(nil)
)

// routerWiringAuditStore records Append calls so the test can prove the
// dispatcher was wired with auditStore as arg 3: the dispatcher appends an
// im_delivery_failed audit entry whenever IM push is unavailable, which is the
// case here because imPusher is nil. Distinctly named to avoid collision with
// dispatchNoopAuditStore in workflow_notifier_dispatch_test.go.
type routerWiringAuditStore struct {
	mu      sync.Mutex
	appends []*workflow.AuditEntry
}

var _ workflow.AuditStore = (*routerWiringAuditStore)(nil)

func (s *routerWiringAuditStore) Append(_ context.Context, e *workflow.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appends = append(s.appends, e)
	return nil
}

func (s *routerWiringAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *routerWiringAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *routerWiringAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *routerWiringAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (s *routerWiringAuditStore) appendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.appends)
}

// TestRouterWiring_NotifierConstructionExpression asserts the exact arg-1
// construction expression the production router uses in its workflow runtime
// wiring block —
//
//	hubNotifier := NewHubNotifier(deviceSvc).WithPresence(deviceSvc)
//
// produces a *HubNotifier that (a) satisfies the unchanged
// workflow.HubInAppNotifier interface (the nil-notifier gap this feature
// closes), (b) is backed by the machine sender passed to NewHubNotifier
// (Req 7.1), (c) has the presence source attached by WithPresence (Req 7.4), and
// (d) actually delivers — a single Send routes through the wired sender exactly
// once.
//
// HubNotifier lives in package httpapi, so its unexported sender/presence fields
// are inspectable here. The router passes the SAME deviceSvc to both steps,
// which the test mirrors by passing one stub to both.
//
// Requirements: 7.1, 7.2, 7.4.
func TestRouterWiring_NotifierConstructionExpression(t *testing.T) {
	dev := &routerWiringDeviceStub{}

	// Exactly the expression in router.go's workflow runtime wiring block.
	hubNotifier := NewHubNotifier(dev).WithPresence(dev)

	if hubNotifier == nil {
		t.Fatal("NewHubNotifier(deviceSvc).WithPresence(deviceSvc) returned nil")
	}

	// (a) arg 1 is a real, non-nil HubInAppNotifier — the gap this feature closes.
	var _ workflow.HubInAppNotifier = hubNotifier

	// (b) backed by the machine sender passed to NewHubNotifier (Req 7.1).
	if hubNotifier.sender == nil {
		t.Fatal("HubNotifier.sender is nil; NewHubNotifier did not wire the machine sender")
	}
	if hubNotifier.sender != dev {
		t.Fatal("HubNotifier.sender is not the machine sender passed to NewHubNotifier")
	}

	// (c) presence source attached by WithPresence(deviceSvc) (Req 7.4).
	if hubNotifier.presence == nil {
		t.Fatal("HubNotifier.presence is nil; WithPresence did not attach the presence source")
	}
	if hubNotifier.presence != dev {
		t.Fatal("HubNotifier.presence is not the presence source passed to WithPresence")
	}

	// The router passes the SAME deviceSvc to both steps — sender and presence
	// share one source, mirroring NewHubNotifier(deviceSvc).WithPresence(deviceSvc).
	if machineCommandSender(dev) != hubNotifier.sender || machinePresenceChecker(dev) != hubNotifier.presence {
		t.Fatal("sender and presence are not wired from the same source")
	}

	// (d) Send routes through the wired sender exactly once — arg 1 delivers.
	if err := hubNotifier.Send(context.Background(), "machine-1", &workflow.InAppNotification{Type: "reminder"}); err != nil {
		t.Fatalf("Send through wired notifier returned error: %v", err)
	}
	if got := dev.sendCount(); got != 1 {
		t.Fatalf("expected exactly 1 SendToMachine via wired notifier, got %d", got)
	}
}

// TestRouterWiring_DispatcherArgsConstructedDeps reproduces the router's exact
// NewNotificationDispatcher construction —
//
//	notifDispatcher := workflow.NewNotificationDispatcher(hubNotifier, nil, auditStore, nil)
//
// and asserts the four argument positions by observable behavior.
//
// NotificationDispatcher lives in package workflow, so its unexported fields
// (hubNotifier, imPusher, auditStore, notifStore) are NOT accessible from
// package httpapi, and the production NewRouter is impractical to invoke here
// (it requires ~40 live dependencies). This test therefore asserts the wiring
// via the exact construction expression plus the dispatcher's observable
// behavior — the strongest assertion available without modifying production
// code or exposing dispatcher internals.
//
// Requirements: 7.1, 7.2, 7.3, 9.6 (and arg-4 notifStore nil per Req 7.6).
func TestRouterWiring_DispatcherArgsConstructedDeps(t *testing.T) {
	dev := &routerWiringDeviceStub{}
	audit := &routerWiringAuditStore{}

	// Exactly the expression in router.go.
	hubNotifier := NewHubNotifier(dev).WithPresence(dev)
	notifDispatcher := workflow.NewNotificationDispatcher(hubNotifier, nil, audit, nil)
	if notifDispatcher == nil {
		t.Fatal("NewNotificationDispatcher(hubNotifier, nil, auditStore, nil) returned nil")
	}

	notif := &workflow.WorkflowNotification{
		InstanceID:   "inst-1",
		Type:         workflow.NotifTypeReminder,
		RecipientID:  "machine-1",
		WorkflowName: "Leave Request",
		InstanceURL:  "/instances/inst-1",
	}

	if err := notifDispatcher.Dispatch(context.Background(), notif); err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	// arg 1 (hubNotifier): the dispatcher routed delivery through the wired
	// HubNotifier, which made exactly one SendToMachine call to the recipient.
	// A nil arg-1 would have short-circuited with "hub notifier not configured"
	// and zero deliveries (Req 7.1, 7.2).
	if got := dev.sendCount(); got != 1 {
		t.Fatalf("arg 1 (hubNotifier): expected exactly 1 delivery via wired notifier, got %d", got)
	}

	// arg 2 (imPusher == nil): IM push is skipped without a delivery error and
	// the recorded delivery channel is hub-only (Req 9.6).
	if notif.DeliveryChannel != "hub_inapp" {
		t.Fatalf("arg 2 (imPusher nil): expected delivery channel %q (IM skipped), got %q", "hub_inapp", notif.DeliveryChannel)
	}

	// arg 3 (auditStore): the dispatcher appended an audit entry through the
	// wired store — it records im_delivery_failed whenever IM push is
	// unavailable, which it is, since imPusher is nil. A non-wired (nil)
	// auditStore would have recorded nothing (Req 7.3).
	if got := audit.appendCount(); got == 0 {
		t.Fatal("arg 3 (auditStore): expected the dispatcher to append an audit entry through the wired store, got none")
	}

	// arg 4 (notifStore == nil): the dispatch above completed without panicking
	// despite a nil notification store — the dispatcher guards every notifStore
	// access with a nil check, so nil is the supported wired value the router
	// passes (Req 7.6). Reaching this line without a panic is the assertion.
}
