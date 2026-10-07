package im

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Device event push contract (plan section 4.1).
//
// An `event` reply is a *structured* message. Unlike `text` it carries a
// category, a severity that decides how loudly the device presents it, and an
// optional set of decisions the user can take without touching a computer.
//
// Downstream tasks all depend on this one wire shape:
//
//	N1-2 renders it (severity -> silent / soft / interrupt)
//	N1-3 fans it out to every online device under a machine and replays
//	     persist=true events from the handshake snapshot
//	N1-4 delivers scheduler reminders through it
//	N1-5 wires the first producers (approval / task_done / ve)
//	N1-6 turns `approval` into an auditable decision loop via `event-ack`
//
// so this file is the single place where that shape is defined.
//
// It lives in corelib/im rather than in the Hub because *both* sides of the
// process boundary need it: the Hub validates the payload, and the desktop
// producers (scheduler delivery, approval prompts) have to spell the closed-set
// values correctly. A producer that guessed a severity would not get a compile
// error -- the Hub would silently drop the whole event -- so the values must not
// be duplicated per side.
//
// Wire shape (payload nested under the reply's own key, matching the existing
// `ambient` convention rather than plan section 4.1's flat sketch -- the
// envelope `ThirdPartyOutgoingMessage` is type-agnostic and must not grow
// event-only fields):
//
//	{
//	  "type": "event",
//	  "conversationId": "system",
//	  "event": {
//	    "eventId": "evt_01H...",
//	    "category": "approval",
//	    "severity": "interrupt",
//	    "title": "需要你确认",
//	    "summary": "要删除 ~/Downloads/report-final-v2.xlsx 吗？",
//	    "ttlSec": 300,
//	    "dedupeKey": "approval:inst-123:step-2",
//	    "actions": [
//	      {"id": "approve", "label": "同意", "kind": "primary", "risk": "high"},
//	      {"id": "reject",  "label": "拒绝", "kind": "secondary"}
//	    ],
//	    "requiresAck": true,
//	    "persist": true
//	  }
//	}
//
// There is deliberately no separate `speak` field: plan section 4.1's own
// example writes `summary` as the spoken question, so the device speaks
// `summary` and falls back to `title`. Two text fields that must stay in sync
// would drift.
const (
	// DeviceEventReplyType is the `reply_type` a producer sets when it wants the
	// Hub to route the payload as a structured event.
	DeviceEventReplyType = "event"
	// DeviceEventReplyKey is the nested key carrying the event object inside the
	// reply envelope.
	DeviceEventReplyKey = "event"

	DeviceEventCategoryApproval = "approval"
	DeviceEventCategoryTaskDone = "task_done"
	DeviceEventCategorySchedule = "schedule"
	DeviceEventCategoryVE       = "ve"
	DeviceEventCategorySystem   = "system"

	// Severity is the only thing that decides how the device interrupts the
	// user, so it is a closed set: firmware switches on it exhaustively and
	// cannot invent a rendering for an unknown level. The准入 matrix in plan
	// section 4.2 is keyed on exactly these three values.
	DeviceEventSeveritySilent    = "silent"
	DeviceEventSeveritySoft      = "soft"
	DeviceEventSeverityInterrupt = "interrupt"

	// Kind is presentation emphasis; risk is the consequence of taking the
	// action. They are separate on purpose: D5-A gates the whole approval flow
	// on risk=high, so risk must carry meaning rather than be inferred from a
	// button's colour.
	DeviceEventActionKindPrimary   = "primary"
	DeviceEventActionKindSecondary = "secondary"

	DeviceEventActionRiskLow    = "low"
	DeviceEventActionRiskMedium = "medium"
	DeviceEventActionRiskHigh   = "high"
)

// Event-ack (plan section 4.3): the device's receipt or decision for one event.
//
// This is deliberately not `/tool-result`.  That endpoint means "the device
// executed a tool and is returning its result"; a decision means "the user
// answered".  Merging them would let a button press masquerade as a tool
// execution in the server's state machine.
//
// The decision values are a closed set because the audit trail is built from
// them: `approved` and `rejected` are *user* decisions, `expired` is the
// absence of one, and `received` is not a decision at all.  Collapsing
// `expired` into `rejected` would erase the distinction the plan's "timed out
// means not approved, not approved-by-default" rule depends on.
const (
	DeviceEventAckStatusReceived = "received"
	DeviceEventAckStatusApproved = "approved"
	DeviceEventAckStatusRejected = "rejected"
	DeviceEventAckStatusExpired  = "expired"

	// How the decision was made.  `timeout` is not a user action; it exists so
	// the audit record can say "nobody answered" without inventing a decider.
	DeviceEventDecidedByVoice   = "voice"
	DeviceEventDecidedByButton  = "button"
	DeviceEventDecidedByTimeout = "timeout"
)

// DeviceEventAckIsDecision reports whether a status carries a user decision.
// `received` is a pure receipt (section 4.1's `requiresAck`) and `expired` is
// a non-decision, so neither may be routed as "the user chose X".
func DeviceEventAckIsDecision(status string) bool {
	switch status {
	case DeviceEventAckStatusApproved, DeviceEventAckStatusRejected:
		return true
	default:
		return false
	}
}

// DeviceEventAckIsTerminal reports whether a status closes the event's life.
// A terminal ack is what clears the Hub's pending-event snapshot (N1-6); a
// `received` receipt must not, because the card is still waiting for an answer.
func DeviceEventAckIsTerminal(status string) bool {
	switch status {
	case DeviceEventAckStatusApproved, DeviceEventAckStatusRejected, DeviceEventAckStatusExpired:
		return true
	default:
		return false
	}
}

// ThirdPartyEventAckRequest is the body of `POST /api/im-gateway/v1/event-ack`.
type ThirdPartyEventAckRequest struct {
	ClientID string `json:"clientId"`
	// EventID is the `eventId` of the event being acked.  It is the
	// correlation key: an ack naming an event the Hub never sent is rejected
	// rather than applied, so a device cannot inject a decision.
	EventID string `json:"eventId"`
	Status  string `json:"status"`
	// ActionID names which button the user pressed.  Required for a decision,
	// forbidden for a receipt or a timeout -- see the audit rules below.
	ActionID string `json:"actionId,omitempty"`
	// DecidedBy records the modality (voice/button/timeout).
	DecidedBy string `json:"decidedBy,omitempty"`
	// AckID is the device's idempotency key for this ack.
	AckID     string `json:"ackId,omitempty"`
	CreatedAt int64  `json:"createdAt,omitempty"`
}

// NormalizeThirdPartyEventAckRequest validates and normalizes an untrusted ack.
//
// It fails closed and enforces the *audit contract* rather than only the
// field shapes.  The rules exist because each violation produces an audit
// record that looks complete but says the wrong thing:
//
//   - a decision without an actionId cannot name what was approved;
//   - a decision without a modality cannot distinguish a spoken approval from
//     a stray button press;
//   - an `expired` carrying an actionId claims a timeout pressed a button;
//   - a `received` carrying a decision claims a receipt decided something.
func NormalizeThirdPartyEventAckRequest(req *ThirdPartyEventAckRequest) error {
	if req == nil {
		return errors.New("event ack request is required")
	}
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.EventID = NormalizeThirdPartyID(req.EventID)
	req.ActionID = NormalizeThirdPartyID(req.ActionID)
	req.AckID = NormalizeThirdPartyID(req.AckID)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.DecidedBy = strings.ToLower(strings.TrimSpace(req.DecidedBy))

	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.EventID == "" {
		return errors.New("eventId is required")
	}
	if utf8.RuneCountInString(req.EventID) > DeviceEventMaxIDLen {
		return fmt.Errorf("eventId exceeds %d characters", DeviceEventMaxIDLen)
	}
	if req.AckID != "" {
		if err := validateThirdPartyIdempotencyKey("ackId", req.AckID); err != nil {
			return err
		}
	}
	if req.ActionID != "" && utf8.RuneCountInString(req.ActionID) > DeviceEventMaxActionIDLen {
		return fmt.Errorf("actionId exceeds %d characters", DeviceEventMaxActionIDLen)
	}

	switch req.Status {
	case DeviceEventAckStatusReceived:
		if req.ActionID != "" || req.DecidedBy != "" {
			return errors.New("received is a receipt and must not carry a decision")
		}
	case DeviceEventAckStatusApproved, DeviceEventAckStatusRejected:
		if req.ActionID == "" {
			return errors.New("actionId is required for a decision")
		}
		switch req.DecidedBy {
		case DeviceEventDecidedByVoice, DeviceEventDecidedByButton:
		default:
			return errors.New("decidedBy must be voice or button for a decision")
		}
	case DeviceEventAckStatusExpired:
		// A timeout has no decider.  Defaulting the modality here (rather than
		// requiring the device to send it) keeps "nobody answered" a single
		// spelling across firmware versions.
		if req.ActionID != "" {
			return errors.New("expired must not carry an actionId")
		}
		if req.DecidedBy == "" {
			req.DecidedBy = DeviceEventDecidedByTimeout
		}
		if req.DecidedBy != DeviceEventDecidedByTimeout {
			return errors.New("decidedBy must be timeout for an expired ack")
		}
	default:
		return errors.New("status must be received, approved, rejected, or expired")
	}
	return nil
}

// ThirdPartyEventAckEventID is the Hub-side dedupe key for one ack.  It is
// keyed on (event, status, action) rather than the device's ackId so a device
// that retries with a fresh ackId -- or an older firmware that sends none --
// still cannot record the same decision twice.  Two *different* decisions on
// one event remain distinguishable, which is what the audit needs.
func ThirdPartyEventAckEventID(req ThirdPartyEventAckRequest) string {
	key := "event_ack:" + NormalizeThirdPartyID(req.EventID) + ":" +
		strings.ToLower(strings.TrimSpace(req.Status))
	if action := NormalizeThirdPartyID(req.ActionID); action != "" {
		key += ":" + action
	}
	return key
}

// Bounds are protocol-level, not display-level. The device's own text budget is
// applied separately by the Hub, so a device that declares a small budget never
// receives more text than it said it could draw.
const (
	DeviceEventMaxIDLen            = 64
	DeviceEventMaxDedupeKeyLen     = 128
	DeviceEventMaxActionIDLen      = 32
	DeviceEventMaxActionLabelRunes = 24
	DeviceEventMaxActions          = 4
	DeviceEventMaxTitleRunes       = 120
	DeviceEventMaxSummaryRunes     = 400
	// TTLSec is bounded so a producer cannot pin a decision prompt on the
	// device forever. Plan section 4.1 requires an approval to expire.
	DeviceEventMinTTLSec = 1
	DeviceEventMaxTTLSec = 3600
)
