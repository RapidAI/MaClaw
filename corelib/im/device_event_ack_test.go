package im

import (
	"strings"
	"testing"
)

func validEventAck() ThirdPartyEventAckRequest {
	return ThirdPartyEventAckRequest{
		ClientID:  "client-a",
		EventID:   "evt_1",
		Status:    DeviceEventAckStatusApproved,
		ActionID:  "approve",
		DecidedBy: DeviceEventDecidedByButton,
	}
}

func TestNormalizeEventAckAcceptsAReceipt(t *testing.T) {
	req := ThirdPartyEventAckRequest{
		ClientID: "client-a",
		EventID:  "evt_1",
		Status:   DeviceEventAckStatusReceived,
	}
	if err := NormalizeThirdPartyEventAckRequest(&req); err != nil {
		t.Fatalf("a pure receipt must be accepted: %v", err)
	}
	if req.DecidedBy != "" {
		t.Fatalf("a receipt must not gain a modality, got %q", req.DecidedBy)
	}
}

func TestNormalizeEventAckAcceptsADecision(t *testing.T) {
	for _, modality := range []string{DeviceEventDecidedByButton, DeviceEventDecidedByVoice} {
		for _, status := range []string{DeviceEventAckStatusApproved, DeviceEventAckStatusRejected} {
			req := validEventAck()
			req.Status = status
			req.DecidedBy = modality
			if err := NormalizeThirdPartyEventAckRequest(&req); err != nil {
				t.Fatalf("decision %s/%s must be accepted: %v", status, modality, err)
			}
		}
	}
}

// A timeout has no decider.  Defaulting the modality keeps "nobody answered" a
// single spelling instead of making every firmware send the obvious value.
func TestNormalizeEventAckDefaultsExpiredModalityToTimeout(t *testing.T) {
	req := ThirdPartyEventAckRequest{
		ClientID: "client-a",
		EventID:  "evt_1",
		Status:   DeviceEventAckStatusExpired,
	}
	if err := NormalizeThirdPartyEventAckRequest(&req); err != nil {
		t.Fatalf("an expired ack must be accepted: %v", err)
	}
	if req.DecidedBy != DeviceEventDecidedByTimeout {
		t.Fatalf("expired must default to timeout, got %q", req.DecidedBy)
	}
}

// The audit rules: each violation would produce a record that looks complete
// but says the wrong thing.
func TestNormalizeEventAckEnforcesTheAuditContract(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ThirdPartyEventAckRequest)
		wantErr string
	}{
		{
			name:    "a decision must name the action",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.ActionID = "" },
			wantErr: "actionId is required",
		},
		{
			name:    "a decision must record the modality",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.DecidedBy = "" },
			wantErr: "decidedBy must be voice or button",
		},
		{
			name:    "a timeout is not a button press",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.Status = DeviceEventAckStatusExpired },
			wantErr: "expired must not carry an actionId",
		},
		{
			name: "a receipt must not carry a decision",
			mutate: func(r *ThirdPartyEventAckRequest) {
				r.Status = DeviceEventAckStatusReceived
			},
			wantErr: "must not carry a decision",
		},
		{
			name:    "an unknown status is rejected",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.Status = "maybe" },
			wantErr: "status must be received",
		},
		{
			name:    "an unknown modality is rejected",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.DecidedBy = "telepathy" },
			wantErr: "decidedBy must be voice or button",
		},
		{
			name:    "the event id is required",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.EventID = "" },
			wantErr: "eventId is required",
		},
		{
			name:    "the client id is required",
			mutate:  func(r *ThirdPartyEventAckRequest) { r.ClientID = "" },
			wantErr: "clientId is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validEventAck()
			tc.mutate(&req)
			err := NormalizeThirdPartyEventAckRequest(&req)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// The four tuples the paired terminal can actually emit, and nothing else.
//
// The firmware's decision module (iot-agentos/main/services/event_decision.h)
// answers exactly three questions -- status, actionId, decidedBy -- and
// deliberately answers "none" for two of them: a receipt carries neither an
// action nor a decider, and a timeout carries no action because nobody pressed
// anything.  Pinning the tuples here is what keeps the two ends from drifting:
// a firmware that started sending decidedBy on a receipt is refused by a unit
// test rather than by a fleet of devices.
//
// The wire strings themselves are mirrored on the device side and guarded by
// iot-agentos/tools/check-event-decision.ps1, which compares them against this
// file value by value.
func TestNormalizeEventAckAcceptsEveryTupleTheTerminalEmits(t *testing.T) {
	cases := []struct {
		name          string
		status        string
		actionID      string
		decidedBy     string
		wantDecidedBy string
	}{
		// The card reached the screen. Nothing was decided yet.
		{name: "receipt", status: DeviceEventAckStatusReceived, wantDecidedBy: ""},
		// The confirm gesture. Modality is `button`, never `voice`: the
		// terminal has no local ASR, speech is uploaded and transcribed on the
		// Hub, so a spoken answer is not something firmware can report.
		{
			name: "approved", status: DeviceEventAckStatusApproved, actionID: "approve",
			decidedBy: DeviceEventDecidedByButton, wantDecidedBy: DeviceEventDecidedByButton,
		},
		{
			name: "rejected", status: DeviceEventAckStatusRejected, actionID: "reject",
			decidedBy: DeviceEventDecidedByButton, wantDecidedBy: DeviceEventDecidedByButton,
		},
		// The window closed. The device sends no modality at all and relies on
		// this default, which is what keeps "nobody answered" one spelling
		// across firmware versions.
		{name: "expired", status: DeviceEventAckStatusExpired, wantDecidedBy: DeviceEventDecidedByTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ThirdPartyEventAckRequest{
				ClientID:  "client-a",
				EventID:   "evt_1",
				Status:    tc.status,
				ActionID:  tc.actionID,
				DecidedBy: tc.decidedBy,
			}
			if err := NormalizeThirdPartyEventAckRequest(&req); err != nil {
				t.Fatalf("the terminal's %s tuple must be accepted: %v", tc.name, err)
			}
			if req.DecidedBy != tc.wantDecidedBy {
				t.Fatalf("%s: DecidedBy = %q, want %q", tc.name, req.DecidedBy, tc.wantDecidedBy)
			}
		})
	}
}

// The terminal sends no ackId at all. That has to keep working: the dedupe key
// is derived from (eventId, status, actionId) precisely so a firmware that
// omits the field still cannot record the same decision twice.
//
// Only ackId is optional here. A decision still owes its modality -- "somebody
// pressed something" without saying which modality is exactly the audit record
// the contract refuses to write.
func TestNormalizeEventAckAcceptsATerminalPayloadWithNoAckID(t *testing.T) {
	req := ThirdPartyEventAckRequest{
		ClientID:  "client-a",
		EventID:   "evt_1",
		Status:    DeviceEventAckStatusApproved,
		ActionID:  "approve",
		DecidedBy: DeviceEventDecidedByButton,
	}
	if req.AckID != "" {
		t.Fatal("this case is about an omitted ackId")
	}
	if err := NormalizeThirdPartyEventAckRequest(&req); err != nil {
		t.Fatalf("a decision without ackId must be accepted: %v", err)
	}
	retry := req
	retry.AckID = "ack-generated-on-the-second-attempt"
	if ThirdPartyEventAckEventID(req) != ThirdPartyEventAckEventID(retry) {
		t.Fatal("a retry with a fresh ackId must collapse to the same dedupe key")
	}
}

func TestNormalizeEventAckRejectsAnOverlongEventID(t *testing.T) {
	req := validEventAck()
	req.EventID = strings.Repeat("e", DeviceEventMaxIDLen+1)
	err := NormalizeThirdPartyEventAckRequest(&req)
	if err == nil || !strings.Contains(err.Error(), "eventId exceeds") {
		t.Fatalf("an overlong eventId must be rejected, got %v", err)
	}
}

func TestNormalizeEventAckRejectsNil(t *testing.T) {
	if err := NormalizeThirdPartyEventAckRequest(nil); err == nil {
		t.Fatal("a nil request must be rejected")
	}
}

// The dedupe key must not depend on the device's ackId: a retry with a fresh
// ackId, or an older firmware that sends none, must still collapse to one
// recorded decision.
func TestEventAckDedupeKeyIgnoresTheDeviceAckID(t *testing.T) {
	first := validEventAck()
	first.AckID = "ack-1"
	second := validEventAck()
	second.AckID = "ack-2"
	third := validEventAck()
	third.AckID = ""
	if ThirdPartyEventAckEventID(first) != ThirdPartyEventAckEventID(second) ||
		ThirdPartyEventAckEventID(first) != ThirdPartyEventAckEventID(third) {
		t.Fatalf("the dedupe key must not vary with ackId: %q / %q / %q",
			ThirdPartyEventAckEventID(first), ThirdPartyEventAckEventID(second),
			ThirdPartyEventAckEventID(third))
	}
}

// Different decisions on one event must stay distinguishable, or the audit
// could not tell an approval from a rejection.
func TestEventAckDedupeKeyDistinguishesDecisions(t *testing.T) {
	approve := validEventAck()
	reject := validEventAck()
	reject.Status = DeviceEventAckStatusRejected
	reject.ActionID = "reject"
	receipt := ThirdPartyEventAckRequest{ClientID: "client-a", EventID: "evt_1", Status: DeviceEventAckStatusReceived}
	keys := map[string]bool{
		ThirdPartyEventAckEventID(approve): true,
		ThirdPartyEventAckEventID(reject):  true,
		ThirdPartyEventAckEventID(receipt): true,
	}
	if len(keys) != 3 {
		t.Fatalf("approve/reject/receipt must have distinct keys, got %v", keys)
	}
	if got := ThirdPartyEventAckEventID(approve); got != "event_ack:evt_1:approved:approve" {
		t.Fatalf("unexpected key shape: %q", got)
	}
}

func TestEventAckStatusPredicates(t *testing.T) {
	for status, want := range map[string]bool{
		DeviceEventAckStatusApproved: true,
		DeviceEventAckStatusRejected: true,
		DeviceEventAckStatusExpired:  false, // the absence of a decision
		DeviceEventAckStatusReceived: false, // a receipt, not a decision
		"":                           false,
	} {
		if got := DeviceEventAckIsDecision(status); got != want {
			t.Fatalf("DeviceEventAckIsDecision(%q) = %v, want %v", status, got, want)
		}
	}
	for status, want := range map[string]bool{
		DeviceEventAckStatusApproved: true,
		DeviceEventAckStatusRejected: true,
		DeviceEventAckStatusExpired:  true,
		DeviceEventAckStatusReceived: false, // the card is still waiting
		"":                           false,
	} {
		if got := DeviceEventAckIsTerminal(status); got != want {
			t.Fatalf("DeviceEventAckIsTerminal(%q) = %v, want %v", status, got, want)
		}
	}
}
