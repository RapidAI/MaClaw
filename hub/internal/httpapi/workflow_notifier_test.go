package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	"pgregory.net/rapid"
)

// Compile-time assertion that *HubNotifier satisfies the existing
// workflow.HubInAppNotifier interface (Req 1.1 / 8.1). This mirrors the
// production assertion in workflow_notifier.go; the blank identifier never
// collides, so declaring it here keeps the example self-contained.
var _ workflow.HubInAppNotifier = (*HubNotifier)(nil)

// countingMachineSender is the property-test machine sender. Unlike the
// existing capturingMachineSender (which records only on the success path),
// it records EVERY SendToMachine call — machine id + message — regardless of
// the configured outcome, and can be configured to return nil,
// device.ErrMachineOffline, or an arbitrary error. This lets the property
// tests assert both delivery attempts and return-value mirroring on the
// failure paths. Used by all property tests in this file.
type countingMachineSender struct {
	err   error
	calls []capturedNotify
}

type capturedNotify struct {
	machineID string
	msg       map[string]any
}

func (s *countingMachineSender) SendToMachine(machineID string, msg any) error {
	typed, _ := msg.(map[string]any)
	s.calls = append(s.calls, capturedNotify{machineID: machineID, msg: typed})
	return s.err
}

// fakePresence is a deterministic machinePresenceChecker for the WithPresence
// example. It returns its configured bool for any machine id.
type fakePresence struct {
	online bool
}

func (f *fakePresence) IsMachineOnline(machineID string) bool {
	return f.online
}

// allNotifTypes covers every notification type that flows through
// HubNotifier.Send: terminal-node completions (result_executor, notifier),
// confirmation reminders/escalations (reminder, escalation), and withdrawals.
var allNotifTypes = []workflow.NotifType{
	workflow.NotifTypeResultExecutor,
	workflow.NotifTypeNotifier,
	workflow.NotifTypeReminder,
	workflow.NotifTypeEscalation,
	workflow.NotifTypeWithdrawal,
}

// whitespacePads is the set of leading/trailing whitespace fragments used to
// build recipient ids that exercise the TrimSpace guard.
var whitespacePads = []string{"", " ", "  ", "\t", "\n", " \t\n "}

// drawNotification generates an arbitrary non-nil *InAppNotification across all
// notification types and arbitrary title/body/url fields.
func drawNotification(t *rapid.T) *workflow.InAppNotification {
	typ := rapid.SampledFrom(allNotifTypes).Draw(t, "type")
	return &workflow.InAppNotification{
		Title: rapid.String().Draw(t, "title"),
		Body:  rapid.String().Draw(t, "body"),
		URL:   rapid.String().Draw(t, "url"),
		Type:  string(typ),
	}
}

// drawRecipient generates a recipient id whose trimmed form is non-empty,
// optionally padded with whitespace (incl. unicode core runes). It returns the
// raw id passed to Send and the expected trimmed id Send delivers to. Both the
// generator and the implementation use strings.TrimSpace, so the expected
// value is computed identically to the production behavior.
func drawRecipient(t *rapid.T) (raw, trimmed string) {
	core := rapid.StringMatching(`[-a-zA-Z0-9_\x{4e00}-\x{9fff}]{1,12}`).Draw(t, "core")
	left := rapid.SampledFrom(whitespacePads).Draw(t, "left")
	right := rapid.SampledFrom(whitespacePads).Draw(t, "right")
	raw = left + core + right
	return raw, strings.TrimSpace(raw)
}

// --- Task 1.2: example / edge tests -----------------------------------------

// TestHubNotifier_SatisfiesInterface is the runtime companion to the static
// assertion: a *HubNotifier is assignable to workflow.HubInAppNotifier
// (Req 1.1 / 8.1).
func TestHubNotifier_SatisfiesInterface(t *testing.T) {
	var n workflow.HubInAppNotifier = NewHubNotifier(&countingMachineSender{})
	if n == nil {
		t.Fatal("HubNotifier should satisfy workflow.HubInAppNotifier")
	}
}

// TestHubNotifier_NilSender_ReturnsErrorZeroDelivery verifies a notifier built
// without a machine sender returns an error from Send and never reaches a
// transport (Req 1.5). With no sender there is nothing to call, so delivery is
// necessarily zero.
func TestHubNotifier_NilSender_ReturnsErrorZeroDelivery(t *testing.T) {
	n := NewHubNotifier(nil)
	err := n.Send(context.Background(), "machine-1", &workflow.InAppNotification{Type: "reminder"})
	if err == nil {
		t.Fatal("expected error from Send when no machine sender is configured")
	}
}

// TestHubNotifier_WithPresence_ReadsSourceBool verifies WithPresence attaches a
// presence source whose IsMachineOnline read returns the source's bool for a
// non-empty recipient (Req 3.4).
func TestHubNotifier_WithPresence_ReadsSourceBool(t *testing.T) {
	for _, want := range []bool{true, false} {
		src := &fakePresence{online: want}
		n := NewHubNotifier(&countingMachineSender{}).WithPresence(src)
		if n.presence == nil {
			t.Fatal("WithPresence should attach a presence source")
		}
		if got := n.presence.IsMachineOnline("machine-1"); got != want {
			t.Fatalf("presence read for non-empty recipient = %v, want %v", got, want)
		}
	}
}

// --- Task 2.1: Property 1 ----------------------------------------------------

// Feature: workflow-confirmation-notifier, Property 1: One Send invocation makes exactly one delivery to the recipient
func TestProp_HubNotifier_SingleDelivery(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sender := &countingMachineSender{}
		n := NewHubNotifier(sender)

		raw, trimmed := drawRecipient(rt)
		notif := drawNotification(rt)

		if err := n.Send(context.Background(), raw, notif); err != nil {
			rt.Fatalf("Send returned error for valid input: %v", err)
		}
		if len(sender.calls) != 1 {
			rt.Fatalf("expected exactly 1 SendToMachine call, got %d", len(sender.calls))
		}
		if sender.calls[0].machineID != trimmed {
			rt.Fatalf("expected delivery to trimmed recipient %q, got %q", trimmed, sender.calls[0].machineID)
		}
	})
}

// --- Task 2.2: Property 2 ----------------------------------------------------

// Feature: workflow-confirmation-notifier, Property 2: Delivered message uses the typed envelope with a faithful type discriminator
func TestProp_HubNotifier_EnvelopeAndDiscriminator(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sender := &countingMachineSender{}
		n := NewHubNotifier(sender)

		raw, _ := drawRecipient(rt)
		notif := drawNotification(rt)

		if err := n.Send(context.Background(), raw, notif); err != nil {
			rt.Fatalf("Send returned error for valid input: %v", err)
		}
		if len(sender.calls) != 1 {
			rt.Fatalf("expected exactly 1 SendToMachine call, got %d", len(sender.calls))
		}

		msg := sender.calls[0].msg
		if msg == nil {
			rt.Fatalf("captured message is not a map[string]any: %#v", sender.calls[0])
		}

		// Envelope shape is exactly {type, ts, payload} — identical across all
		// notification types (completions and reminders share one path).
		if len(msg) != 3 {
			rt.Fatalf("expected exactly 3 envelope keys {type, ts, payload}, got %d: %#v", len(msg), msg)
		}
		for _, k := range []string{"type", "ts", "payload"} {
			if _, ok := msg[k]; !ok {
				rt.Fatalf("envelope missing key %q: %#v", k, msg)
			}
		}

		// type == ve:workflow_notification
		if msg["type"] != workflowNotificationWireType {
			rt.Fatalf("expected type %q, got %v", workflowNotificationWireType, msg["type"])
		}

		// ts is an integer timestamp (device.Service.SendToMachine receives the
		// raw map; Send sets ts via time.Now().Unix(), an int64).
		if _, ok := msg["ts"].(int64); !ok {
			rt.Fatalf("expected integer ts, got %T (%v)", msg["ts"], msg["ts"])
		}

		payload, ok := msg["payload"].(map[string]any)
		if !ok {
			rt.Fatalf("payload is not a map[string]any: %T", msg["payload"])
		}
		nt, ok := payload["notification_type"].(string)
		if !ok {
			rt.Fatalf("notification_type is not a string: %T", payload["notification_type"])
		}
		if nt != notif.Type {
			rt.Fatalf("expected notification_type %q (== notif.Type), got %q", notif.Type, nt)
		}
		if _, ok := payload["notification"]; !ok {
			rt.Fatalf("payload missing marshaled notification body: %#v", payload)
		}
	})
}

// --- Task 2.3: Property 3 ----------------------------------------------------

// Feature: workflow-confirmation-notifier, Property 3: Send's return value mirrors the sender outcome, with offline distinguishable
func TestProp_HubNotifier_ReturnMirrorsOutcome(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// outcome kind: 0 = nil, 1 = offline, 2 = arbitrary error
		kind := rapid.SampledFrom([]int{0, 1, 2}).Draw(rt, "outcome")

		var injected error
		switch kind {
		case 1:
			injected = device.ErrMachineOffline
		case 2:
			// A fresh error that is never errors.Is(ErrMachineOffline), so the
			// offline condition stays distinguishable.
			injected = errors.New("transient transport failure: " + rapid.String().Draw(rt, "errmsg"))
		}

		sender := &countingMachineSender{err: injected}
		n := NewHubNotifier(sender)

		raw, _ := drawRecipient(rt)
		notif := drawNotification(rt)

		err := n.Send(context.Background(), raw, notif)

		switch kind {
		case 0:
			if err != nil {
				rt.Fatalf("expected nil return for nil sender outcome, got %v", err)
			}
		case 1:
			if err == nil {
				rt.Fatal("expected non-nil return for offline outcome")
			}
			if !errors.Is(err, device.ErrMachineOffline) {
				rt.Fatalf("expected errors.Is(err, ErrMachineOffline) for offline outcome, got %v", err)
			}
		case 2:
			if err == nil {
				rt.Fatal("expected non-nil return for arbitrary error outcome")
			}
			if errors.Is(err, device.ErrMachineOffline) {
				rt.Fatalf("arbitrary error must not match the offline sentinel: %v", err)
			}
		}

		// Valid input always reaches the transport exactly once, regardless of
		// outcome — Send never reports success-without-attempt or vice versa.
		if len(sender.calls) != 1 {
			rt.Fatalf("expected exactly 1 transport attempt, got %d", len(sender.calls))
		}
	})
}

// --- Task 2.4: Property 4 ----------------------------------------------------

// Feature: workflow-confirmation-notifier, Property 4: Invalid input is rejected with an error and zero delivery attempts
func TestProp_HubNotifier_InvalidInputRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// invalid class: 0 = empty/whitespace recipient, 1 = nil payload, 2 = nil sender
		class := rapid.SampledFrom([]int{0, 1, 2}).Draw(rt, "class")

		sender := &countingMachineSender{}
		var n *HubNotifier
		var recipient string
		var notif *workflow.InAppNotification

		switch class {
		case 0: // empty or all-whitespace recipient, valid sender + payload
			n = NewHubNotifier(sender)
			recipient = rapid.SampledFrom(whitespacePads).Draw(rt, "ws")
			notif = drawNotification(rt)
		case 1: // nil payload, valid sender + recipient
			n = NewHubNotifier(sender)
			recipient, _ = drawRecipient(rt)
			notif = nil
		case 2: // nil sender, valid recipient + payload
			n = NewHubNotifier(nil)
			recipient, _ = drawRecipient(rt)
			notif = drawNotification(rt)
		}

		err := n.Send(context.Background(), recipient, notif)
		if err == nil {
			rt.Fatalf("expected non-nil error for invalid class %d", class)
		}
		if len(sender.calls) != 0 {
			rt.Fatalf("expected zero SendToMachine calls for invalid class %d, got %d", class, len(sender.calls))
		}
	})
}

// --- Task 2.5: Property 6 ----------------------------------------------------

// Feature: workflow-confirmation-notifier, Property 6: HubNotifier holds no cadence, counting, or dedup state across invocations
func TestProp_HubNotifier_NoInternalState(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sender := &countingMachineSender{}
		n := NewHubNotifier(sender)

		raw, trimmed := drawRecipient(rt)
		notif := drawNotification(rt)
		k := rapid.IntRange(1, 20).Draw(rt, "k")

		for i := 0; i < k; i++ {
			if err := n.Send(context.Background(), raw, notif); err != nil {
				rt.Fatalf("Send invocation %d returned error: %v", i, err)
			}
		}

		// k invocations for the same recipient + payload yield exactly k
		// deliveries — no suppression, collapse, dedup, or rate-limit.
		if len(sender.calls) != k {
			rt.Fatalf("expected exactly %d SendToMachine calls for %d Sends, got %d", k, k, len(sender.calls))
		}
		for i, c := range sender.calls {
			if c.machineID != trimmed {
				rt.Fatalf("call %d delivered to %q, want %q", i, c.machineID, trimmed)
			}
		}
	})
}
