package im

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// Device event push contract (plan N1-1, spec in plan section 4.1).
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

// Bounds are protocol-level, not display-level. The device's own text budget is
// applied separately by the caller, so a device that declares a small budget
// never receives more text than it said it could draw.
const (
	deviceEventMaxIDLen            = 64
	deviceEventMaxDedupeKeyLen     = 128
	deviceEventMaxActionIDLen      = 32
	deviceEventMaxActionLabelRunes = 24
	deviceEventMaxActions          = 4
	deviceEventMaxTitleRunes       = 120
	deviceEventMaxSummaryRunes     = 400
	// ttlSec is bounded so a producer cannot pin a decision prompt on the
	// device forever. Plan section 4.1 requires an approval to expire.
	deviceEventMinTTLSec = 1
	deviceEventMaxTTLSec = 3600
)

func knownDeviceEventCategory(value string) bool {
	switch value {
	case DeviceEventCategoryApproval, DeviceEventCategoryTaskDone, DeviceEventCategorySchedule,
		DeviceEventCategoryVE, DeviceEventCategorySystem:
		return true
	default:
		return false
	}
}

func knownDeviceEventSeverity(value string) bool {
	switch value {
	case DeviceEventSeveritySilent, DeviceEventSeveritySoft, DeviceEventSeverityInterrupt:
		return true
	default:
		return false
	}
}

func knownDeviceEventActionKind(value string) bool {
	switch value {
	case DeviceEventActionKindPrimary, DeviceEventActionKindSecondary:
		return true
	default:
		return false
	}
}

func knownDeviceEventActionRisk(value string) bool {
	switch value {
	case DeviceEventActionRiskLow, DeviceEventActionRiskMedium, DeviceEventActionRiskHigh:
		return true
	default:
		return false
	}
}

// DeviceEventPushSupported reports whether a device declared that it can render
// structured events.
//
// Fan-out code (N1-3 `UpdateMachineEvent`) must call this per device. Fan-out
// builds the outgoing message directly instead of routing it through
// adaptDeviceGatewayReply, so the capability gate that protects the reply path
// does not apply there on its own -- the ambient fan-out it is modelled on has
// no such check, and a device that can only draw text would silently receive a
// card it cannot render. Keeping the predicate here gives both paths one
// definition of "this device can receive events".
func DeviceEventPushSupported(capabilities agent.ClientCapabilities) bool {
	normalized := agent.NormalizeClientCapabilities(&capabilities)
	return normalized.Features.EventPush && normalized.SupportsOutput("text")
}

// normalizeDeviceEventPush validates and normalizes the untrusted `event` object
// carried by a reply. It fails closed: an event the device cannot render
// faithfully is dropped rather than delivered half-formed, because a
// half-rendered approval card is worse than no card at all.
//
// Free text is truncated to the protocol bound; identity and classification
// fields are rejected when malformed, because truncating an event id or a
// dedupe key would silently break the `event-ack` correlation and the
// "same dedupeKey is presented once" guarantee that N1-6 depends on.
func normalizeDeviceEventPush(raw any) (map[string]any, bool) {
	payload, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}

	eventID := deviceReplyString(payload, "eventId", "event_id")
	if eventID == "" || len(eventID) > deviceEventMaxIDLen {
		return nil, false
	}

	category := strings.ToLower(deviceReplyString(payload, "category"))
	if !knownDeviceEventCategory(category) {
		return nil, false
	}

	severity := strings.ToLower(deviceReplyString(payload, "severity"))
	if !knownDeviceEventSeverity(severity) {
		return nil, false
	}

	title := deviceReplyString(payload, "title")
	if title == "" {
		return nil, false
	}

	dedupeKey := deviceReplyString(payload, "dedupeKey", "dedupe_key")
	if len(dedupeKey) > deviceEventMaxDedupeKeyLen {
		return nil, false
	}
	if dedupeKey == "" {
		// Falling back to eventId keeps the idempotency guarantee available to
		// producers that do not distinguish a logical request from one attempt.
		dedupeKey = eventID
	}

	ttlSec := deviceReplyInt64(payload, "ttlSec", "ttl_sec")
	if ttlSec < 0 || (ttlSec > 0 && (ttlSec < deviceEventMinTTLSec || ttlSec > deviceEventMaxTTLSec)) {
		return nil, false
	}

	actions, ok := normalizeDeviceEventActions(payload["actions"])
	if !ok {
		return nil, false
	}

	requiresAck := deviceReplyBool(payload, "requiresAck", "requires_ack")
	persist := deviceReplyBool(payload, "persist")

	// Approval events carry the D5-A / section 4.3 audit contract, so the three
	// guarantees that make an approval auditable are enforced at the wire
	// boundary instead of trusted to every producer: without requiresAck the
	// Hub cannot know the card arrived, without persist a reboot silently drops
	// a pending high-risk decision, and without a ttl the prompt hangs forever.
	// All three failures are invisible in production, which is exactly why they
	// must not be optional here.
	if category == DeviceEventCategoryApproval {
		if ttlSec <= 0 || !requiresAck || !persist {
			return nil, false
		}
	}

	normalized := map[string]any{
		"eventId":     eventID,
		"category":    category,
		"severity":    severity,
		"title":       truncateDeviceEventRunes(title, deviceEventMaxTitleRunes),
		"dedupeKey":   dedupeKey,
		"requiresAck": requiresAck,
		"persist":     persist,
	}
	if summary := deviceReplyString(payload, "summary"); summary != "" {
		normalized["summary"] = truncateDeviceEventRunes(summary, deviceEventMaxSummaryRunes)
	}
	if ttlSec > 0 {
		normalized["ttlSec"] = ttlSec
	}
	if len(actions) > 0 {
		normalized["actions"] = actions
	}
	return normalized, true
}

// normalizeDeviceEventActions validates the decision buttons. Duplicate ids are
// rejected rather than de-duplicated: the device echoes an action id back in
// `event-ack`, and two buttons sharing an id would make the audit trail
// ambiguous about what the user actually approved.
func normalizeDeviceEventActions(raw any) ([]map[string]any, bool) {
	if raw == nil {
		return nil, true
	}
	items, ok := raw.([]any)
	if !ok {
		// A single action object is accepted for hand-written payloads.
		if single, singleOK := raw.(map[string]any); singleOK {
			items = []any{single}
		} else {
			return nil, false
		}
	}
	if len(items) > deviceEventMaxActions {
		return nil, false
	}
	out := make([]map[string]any, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		id := deviceReplyString(entry, "id", "actionId", "action_id")
		if id == "" || len(id) > deviceEventMaxActionIDLen || seen[id] {
			return nil, false
		}
		label := deviceReplyString(entry, "label", "text")
		if label == "" {
			return nil, false
		}
		kind := strings.ToLower(deviceReplyString(entry, "kind"))
		if kind == "" {
			kind = DeviceEventActionKindSecondary
		}
		if !knownDeviceEventActionKind(kind) {
			return nil, false
		}
		risk := strings.ToLower(deviceReplyString(entry, "risk"))
		if risk != "" && !knownDeviceEventActionRisk(risk) {
			return nil, false
		}
		seen[id] = true
		action := map[string]any{
			"id":    id,
			"label": truncateDeviceEventRunes(label, deviceEventMaxActionLabelRunes),
			"kind":  kind,
		}
		// An absent risk means "not declared as risky", which is different from
		// an explicit low. Keeping it absent lets D5-A stay a positive test.
		if risk != "" {
			action["risk"] = risk
		}
		out = append(out, action)
	}
	return out, true
}

// truncateDeviceEventRunes bounds free text on a rune boundary so a multi-byte
// CJK title is never cut into an invalid UTF-8 sequence the device cannot draw.
func truncateDeviceEventRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

// clampDeviceEventText honours the client's declared text budget on every free
// text field of a normalized event. An event card is still text the device has
// to draw, so a client that declared a small budget must never receive more
// than it said it could render -- the same rule `meeting_result` already
// applies to reply text.
//
// Only free text is clamped. `eventId`, `dedupeKey`, `category`, `severity`,
// `ttlSec` and action ids are identity, classification or timing values;
// shortening them would break correlation instead of making anything fit.
func clampDeviceEventText(normalized map[string]any, maxChars int) {
	if normalized == nil || maxChars <= 0 {
		return
	}
	for _, key := range []string{"title", "summary"} {
		if value, ok := normalized[key].(string); ok {
			normalized[key] = truncateDeviceEventRunes(value, maxChars)
		}
	}
	actions, ok := normalized["actions"].([]map[string]any)
	if !ok {
		return
	}
	for _, action := range actions {
		if label, ok := action["label"].(string); ok {
			action["label"] = truncateDeviceEventRunes(label, maxChars)
		}
	}
}
