package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	"pgregory.net/rapid"
)

// dispatchCountingSender is a thread-safe machineCommandSender that records
// every SendToMachine attempt (DispatchBatch fans out concurrently) and can be
// configured to return an error for a chosen subset of recipients. Unlike the
// existing capturingMachineSender (which returns its error BEFORE recording),
// this double records the attempt FIRST so a failing send still counts as an
// attempt — exactly what Property 5 (attempts == N, even on failure) requires.
//
// Named distinctly from any test double in workflow_notifier_test.go to avoid
// duplicate-symbol collisions within package httpapi.
type dispatchCountingSender struct {
	mu       sync.Mutex
	attempts []string        // machine IDs in attempt order
	failFor  map[string]bool // recipients whose send returns an error
}

func newDispatchCountingSender(failFor map[string]bool) *dispatchCountingSender {
	return &dispatchCountingSender{failFor: failFor}
}

func (s *dispatchCountingSender) SendToMachine(machineID string, msg any) error {
	s.mu.Lock()
	s.attempts = append(s.attempts, machineID)
	fail := s.failFor[machineID]
	s.mu.Unlock()
	if fail {
		return errors.New("simulated delivery failure for " + machineID)
	}
	return nil
}

func (s *dispatchCountingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attempts)
}

func (s *dispatchCountingSender) attemptCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.attempts))
	for _, id := range s.attempts {
		out[id]++
	}
	return out
}

// dispatchNoopAuditStore is a minimal no-op workflow.AuditStore so the
// NotificationDispatcher can be constructed exactly as the router does
// (hubNotifier, nil, auditStore, nil). Distinctly named to avoid collision with
// any audit-store double declared elsewhere in package httpapi.
type dispatchNoopAuditStore struct{}

var _ workflow.AuditStore = dispatchNoopAuditStore{}

func (dispatchNoopAuditStore) Append(_ context.Context, _ *workflow.AuditEntry) error { return nil }

func (dispatchNoopAuditStore) QueryByInstance(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (dispatchNoopAuditStore) QueryByApprover(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (dispatchNoopAuditStore) QueryByTimeRange(_ context.Context, _, _ time.Time, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

func (dispatchNoopAuditStore) QueryByDecision(_ context.Context, _ string, _, _ int) ([]workflow.AuditEntry, int, error) {
	return nil, 0, nil
}

// dispatchNotifTypes covers every WorkflowNotification.Type that flows through
// the single HubNotifier.Send delivery path.
var dispatchNotifTypes = []workflow.NotifType{
	workflow.NotifTypeResultExecutor,
	workflow.NotifTypeNotifier,
	workflow.NotifTypeReminder,
	workflow.NotifTypeEscalation,
	workflow.NotifTypeWithdrawal,
}

// Feature: workflow-confirmation-notifier, Property 5: Batch dispatch attempts delivery to every recipient (attempts == N)
//
// Validates: Requirements 6.1
//
// For any batch of N >= 1 distinct recipients and any subset whose sender
// returns an error, NotificationDispatcher.DispatchBatch (driven by a real
// HubNotifier) results in exactly N SendToMachine attempts — one per recipient —
// without aborting on the first failure.
func TestProp_HubNotifier_BatchAttemptsEqualN(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(rt, "n")

		recipients := make([]string, 0, n)
		failFor := make(map[string]bool, n)
		notifs := make([]*workflow.WorkflowNotification, 0, n)

		for i := 0; i < n; i++ {
			// The index prefix guarantees distinctness; the random suffix adds
			// variety without risking collisions across recipients.
			suffix := rapid.StringMatching(`[a-zA-Z0-9]{1,6}`).Draw(rt, fmt.Sprintf("suffix-%d", i))
			recipient := fmt.Sprintf("recipient-%02d-%s", i, suffix)
			recipients = append(recipients, recipient)

			if rapid.Bool().Draw(rt, fmt.Sprintf("fail-%d", i)) {
				failFor[recipient] = true
			}

			nt := dispatchNotifTypes[rapid.IntRange(0, len(dispatchNotifTypes)-1).Draw(rt, fmt.Sprintf("type-%d", i))]
			notifs = append(notifs, &workflow.WorkflowNotification{
				InstanceID:   fmt.Sprintf("inst-%d", i),
				Type:         nt,
				RecipientID:  recipient,
				WorkflowName: "Leave Request",
				InstanceURL:  "/instances/inst",
			})
		}

		sender := newDispatchCountingSender(failFor)
		notifier := NewHubNotifier(sender)
		dispatcher := workflow.NewNotificationDispatcher(notifier, nil, dispatchNoopAuditStore{}, nil)

		err := dispatcher.DispatchBatch(context.Background(), notifs)

		// Exactly N attempts — the batch fans out to every recipient.
		if got := sender.count(); got != n {
			rt.Fatalf("expected exactly %d SendToMachine attempts, got %d", n, got)
		}

		// Each distinct recipient is attempted exactly once; a failing send for
		// one recipient never aborts the batch or starves the others.
		counts := sender.attemptCounts()
		for _, r := range recipients {
			if counts[r] != 1 {
				rt.Fatalf("recipient %q attempted %d times, want exactly 1", r, counts[r])
			}
		}

		// DispatchBatch surfaces a combined error when any recipient fails and
		// nil when all succeed — but in both cases all N were attempted above.
		switch {
		case len(failFor) > 0 && err == nil:
			rt.Fatalf("expected combined error when %d recipients fail, got nil", len(failFor))
		case len(failFor) == 0 && err != nil:
			rt.Fatalf("expected nil error when all deliveries succeed, got %v", err)
		}
	})
}

// TestHubNotifier_DispatchSkipsIMPushWhenNilPusher is an example for Req 9.6:
// with imPusher == nil, Dispatch delivers through the HubNotifier and skips IM
// push without returning a delivery error (the dispatcher's existing optional-
// channel behavior). This mirrors the production router wiring
// NewNotificationDispatcher(hubNotifier, nil, auditStore, nil).
func TestHubNotifier_DispatchSkipsIMPushWhenNilPusher(t *testing.T) {
	sender := newDispatchCountingSender(nil)
	notifier := NewHubNotifier(sender)
	dispatcher := workflow.NewNotificationDispatcher(notifier, nil, dispatchNoopAuditStore{}, nil)

	notif := &workflow.WorkflowNotification{
		InstanceID:   "inst-1",
		Type:         workflow.NotifTypeResultExecutor,
		RecipientID:  "recipient-1",
		WorkflowName: "Leave Request",
		Result:       "approved",
		InstanceURL:  "/instances/inst-1",
	}

	if err := dispatcher.Dispatch(context.Background(), notif); err != nil {
		t.Fatalf("Dispatch returned error with nil imPusher, want nil: %v", err)
	}

	// Hub in-app delivery still happened exactly once.
	if got := sender.count(); got != 1 {
		t.Fatalf("expected exactly 1 hub delivery, got %d", got)
	}

	// IM push was skipped (not attempted), so the recorded channel is hub-only.
	if notif.DeliveryChannel != "hub_inapp" {
		t.Fatalf("expected delivery channel %q (IM skipped), got %q", "hub_inapp", notif.DeliveryChannel)
	}
}
