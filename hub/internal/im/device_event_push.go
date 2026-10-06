package im

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// Device event push validation.
//
// The wire contract itself -- the closed-set values, the field bounds and the
// wire shape -- lives in corelib/im/device_event.go, because the desktop
// producers have to spell those values correctly too. What stays here is the
// *validator*: this package is the authority that decides whether an untrusted
// payload is deliverable at all.
//
// Persist TTL is Hub policy rather than wire contract: it bounds how long a
// persist=true event that declared no ttlSec of its own stays replayable from
// the handshake snapshot. Only approvals are required to carry a ttl, but
// "survive a reboot" without any deadline would let a days-old task_done card
// reappear on every handshake.
const deviceEventPersistTTLSec = 3600

// storedDeviceEvent is the durable snapshot entry for one machine's most recent
// persist=true event.
//
// The wire event and its absolute expiry are deliberately kept apart. ttlSec is
// a *relative* duration, so replaying it verbatim after a reboot would hand a
// three-hour-old approval a fresh five-minute window -- contradicting "timed
// out means not approved" (plan section 4.1, finding C34). expiresAtUnixMs is
// therefore stamped once at dispatch time and never crosses the wire: the
// device always receives a recomputed ttlSec.
type storedDeviceEvent struct {
	Event           map[string]any `json:"event"`
	ExpiresAtUnixMs int64          `json:"expiresAtUnixMs,omitempty"`
}

// deviceEventPush is the outcome of validating one raw event for delivery: the
// wire form, plus the snapshot entry to keep when persist=true.
type deviceEventPush struct {
	Event   map[string]any
	Persist bool
	Stored  storedDeviceEvent
}

// prepareDeviceEventPush validates a raw event and stamps the absolute expiry
// that lets a later replay stay honest about how much of the original window is
// left. Events without persist=true are never snapshotted, so they need no
// expiry and Stored is left zero.
//
// The normalized event is treated as immutable from here on, which is why the
// wire form and the snapshot entry may share one map: the queued message and
// the snapshot are both read-only, and every write to durable storage goes
// through cloneStoredDeviceEvent first.
func prepareDeviceEventPush(raw any, now time.Time) (deviceEventPush, bool) {
	normalized, ok := normalizeDeviceEventPush(raw)
	if !ok {
		return deviceEventPush{}, false
	}
	push := deviceEventPush{Event: normalized}
	if persist, _ := normalized["persist"].(bool); !persist {
		return push, true
	}
	windowSec := int64(deviceEventPersistTTLSec)
	if ttl, ok := normalized["ttlSec"].(int64); ok && ttl > 0 {
		windowSec = ttl
	}
	push.Persist = true
	push.Stored = storedDeviceEvent{
		Event:           normalized,
		ExpiresAtUnixMs: now.Add(time.Duration(windowSec) * time.Second).UnixMilli(),
	}
	return push, true
}

// replayDeviceEventPush rebuilds the device-facing event from a snapshot entry.
//
// It returns false when the window has already closed. A stale approval must be
// dropped rather than delivered with a refreshed deadline, so the recomputed
// ttlSec can only shrink. It is also clamped to the protocol maximum, so a
// hand-edited snapshot claiming a far-future expiry cannot pin a card on the
// device forever.
func replayDeviceEventPush(stored storedDeviceEvent, now time.Time) (map[string]any, bool) {
	if stored.Event == nil {
		return nil, false
	}
	normalized, ok := normalizeDeviceEventPush(stored.Event)
	if !ok {
		return nil, false
	}
	if stored.ExpiresAtUnixMs <= 0 {
		if _, hasWindow := normalized["ttlSec"].(int64); hasWindow {
			// An event that declared a window must have had its absolute expiry
			// recorded alongside it. Without one there is no way to tell whether
			// the window is still open, and guessing would refresh a stale
			// decision prompt.
			return nil, false
		}
		return normalized, true
	}
	remainingMs := stored.ExpiresAtUnixMs - now.UnixMilli()
	if remainingMs <= 0 {
		return nil, false
	}
	ttlSec := (remainingMs + 999) / 1000
	if ttlSec > coreim.DeviceEventMaxTTLSec {
		ttlSec = coreim.DeviceEventMaxTTLSec
	}
	if ttlSec < coreim.DeviceEventMinTTLSec {
		ttlSec = coreim.DeviceEventMinTTLSec
	}
	normalized["ttlSec"] = ttlSec
	return normalized, true
}

// cloneStoredDeviceEvent validates and copies a snapshot entry before it crosses
// an ownership boundary (persistence, recovery or a handshake response). It
// reuses the protocol validator so an old, corrupt or hand-edited snapshot
// cannot reintroduce an event the wire contract would have rejected.
func cloneStoredDeviceEvent(stored storedDeviceEvent) (storedDeviceEvent, bool) {
	normalized, ok := normalizeDeviceEventPush(stored.Event)
	if !ok {
		return storedDeviceEvent{}, false
	}
	copy := storedDeviceEvent{Event: normalized}
	if stored.ExpiresAtUnixMs > 0 {
		copy.ExpiresAtUnixMs = stored.ExpiresAtUnixMs
	}
	return copy, true
}

func knownDeviceEventCategory(value string) bool {
	switch value {
	case coreim.DeviceEventCategoryApproval, coreim.DeviceEventCategoryTaskDone, coreim.DeviceEventCategorySchedule,
		coreim.DeviceEventCategoryVE, coreim.DeviceEventCategorySystem:
		return true
	default:
		return false
	}
}

func knownDeviceEventSeverity(value string) bool {
	switch value {
	case coreim.DeviceEventSeveritySilent, coreim.DeviceEventSeveritySoft, coreim.DeviceEventSeverityInterrupt:
		return true
	default:
		return false
	}
}

func knownDeviceEventActionKind(value string) bool {
	switch value {
	case coreim.DeviceEventActionKindPrimary, coreim.DeviceEventActionKindSecondary:
		return true
	default:
		return false
	}
}

func knownDeviceEventActionRisk(value string) bool {
	switch value {
	case coreim.DeviceEventActionRiskLow, coreim.DeviceEventActionRiskMedium, coreim.DeviceEventActionRiskHigh:
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
	if eventID == "" || len(eventID) > coreim.DeviceEventMaxIDLen {
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
	if len(dedupeKey) > coreim.DeviceEventMaxDedupeKeyLen {
		return nil, false
	}
	if dedupeKey == "" {
		// Falling back to eventId keeps the idempotency guarantee available to
		// producers that do not distinguish a logical request from one attempt.
		dedupeKey = eventID
	}

	ttlSec := deviceReplyInt64(payload, "ttlSec", "ttl_sec")
	if ttlSec < 0 || (ttlSec > 0 && (ttlSec < coreim.DeviceEventMinTTLSec || ttlSec > coreim.DeviceEventMaxTTLSec)) {
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
	if category == coreim.DeviceEventCategoryApproval {
		if ttlSec <= 0 || !requiresAck || !persist {
			return nil, false
		}
	}

	normalized := map[string]any{
		"eventId":     eventID,
		"category":    category,
		"severity":    severity,
		"title":       truncateDeviceEventRunes(title, coreim.DeviceEventMaxTitleRunes),
		"dedupeKey":   dedupeKey,
		"requiresAck": requiresAck,
		"persist":     persist,
	}
	if summary := deviceReplyString(payload, "summary"); summary != "" {
		normalized["summary"] = truncateDeviceEventRunes(summary, coreim.DeviceEventMaxSummaryRunes)
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
		// The same payload has three shapes depending on where it came from: a
		// freshly decoded JSON body yields []any, an already-normalized event
		// carries []map[string]any, and hand-written payloads may send a single
		// object. All three describe the same list of actions.
		switch typed := raw.(type) {
		case []map[string]any:
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		case map[string]any:
			items = []any{typed}
		default:
			return nil, false
		}
	}
	if len(items) > coreim.DeviceEventMaxActions {
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
		if id == "" || len(id) > coreim.DeviceEventMaxActionIDLen || seen[id] {
			return nil, false
		}
		label := deviceReplyString(entry, "label", "text")
		if label == "" {
			return nil, false
		}
		kind := strings.ToLower(deviceReplyString(entry, "kind"))
		if kind == "" {
			kind = coreim.DeviceEventActionKindSecondary
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
			"label": truncateDeviceEventRunes(label, coreim.DeviceEventMaxActionLabelRunes),
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
