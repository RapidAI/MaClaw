package im

import (
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// eventPushCapabilities is the contract a companion terminal declares once it
// can render structured events.
func eventPushCapabilities(maxChars int) agent.ClientCapabilities {
	return agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{
			Modalities: []string{"text"},
			Text:       &agent.ClientTextCapabilities{MaxChars: maxChars},
		},
		Features: agent.ClientFeatureCapabilities{EventPush: true},
	})
}

func newEventPushGateway(t *testing.T, clientID string, capabilities agent.ClientCapabilities) *DeviceGateway {
	t.Helper()
	gateway := NewDeviceGateway(nil)
	gateway.mu.Lock()
	state := gateway.clientLocked(clientID)
	state.capabilities = capabilities
	gateway.mu.Unlock()
	return gateway
}

func queuedGatewayMessages(t *testing.T, gateway *DeviceGateway, clientID string) []map[string]any {
	t.Helper()
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return append([]map[string]any(nil), gateway.clients[clientID].messages...)
}

func queuedEvent(t *testing.T, gateway *DeviceGateway, clientID string) map[string]any {
	t.Helper()
	messages := queuedGatewayMessages(t, gateway, clientID)
	if len(messages) == 0 {
		t.Fatal("expected one queued event, got none")
	}
	if len(messages) != 1 {
		t.Fatalf("expected exactly one queued message, got %d: %#v", len(messages), messages)
	}
	event, ok := messages[0]["event"].(map[string]any)
	if !ok {
		t.Fatalf("queued message carries no event payload: %#v", messages[0])
	}
	return event
}

// fullEventReply mirrors plan section 4.1's approval example: the shape a GUI
// producer must emit for a high-risk confirmation.
func fullEventReply(overrides map[string]any) map[string]any {
	event := map[string]any{
		"eventId":   "evt_01H8ZK",
		"category":  DeviceEventCategoryApproval,
		"severity":  DeviceEventSeverityInterrupt,
		"title":     "需要你确认",
		"summary":   "要删除 ~/Downloads/report-final-v2.xlsx 吗？",
		"ttlSec":    int64(300),
		"dedupeKey": "approval:inst-123:step-2",
		"actions": []any{
			map[string]any{"id": "approve", "label": "同意", "kind": DeviceEventActionKindPrimary, "risk": DeviceEventActionRiskHigh},
			map[string]any{"id": "reject", "label": "拒绝", "kind": DeviceEventActionKindSecondary},
		},
		"requiresAck": true,
		"persist":     true,
	}
	for key, value := range overrides {
		if value == nil {
			delete(event, key)
			continue
		}
		event[key] = value
	}
	return map[string]any{"type": "event", "event": event}
}

func TestDeviceGatewayEventPushDeliversStructuredEvent(t *testing.T) {
	gateway := newEventPushGateway(t, "pet-events", eventPushCapabilities(240))
	gateway.EnqueueReply("pet-events", "system", fullEventReply(nil))

	event := queuedEvent(t, gateway, "pet-events")
	if event["eventId"] != "evt_01H8ZK" || event["category"] != DeviceEventCategoryApproval {
		t.Fatalf("identity fields=%#v", event)
	}
	if event["severity"] != DeviceEventSeverityInterrupt {
		t.Fatalf("severity=%#v", event["severity"])
	}
	if event["title"] != "需要你确认" {
		t.Fatalf("title=%#v", event["title"])
	}
	if event["summary"] != "要删除 ~/Downloads/report-final-v2.xlsx 吗？" {
		t.Fatalf("summary=%#v", event["summary"])
	}
	if event["ttlSec"] != int64(300) {
		t.Fatalf("ttlSec=%#v", event["ttlSec"])
	}
	if event["dedupeKey"] != "approval:inst-123:step-2" {
		t.Fatalf("dedupeKey=%#v", event["dedupeKey"])
	}
	if event["requiresAck"] != true || event["persist"] != true {
		t.Fatalf("audit flags=%#v", event)
	}
	actions, ok := event["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions=%#v", event["actions"])
	}
	if actions[0]["kind"] != DeviceEventActionKindPrimary || actions[0]["risk"] != DeviceEventActionRiskHigh {
		t.Fatalf("approve action=%#v", actions[0])
	}
	// An action that never declared a risk must not be silently upgraded to
	// low: D5-A gates the whole flow on a positive risk=high test.
	if _, present := actions[1]["risk"]; present {
		t.Fatalf("undeclared risk must stay absent: %#v", actions[1])
	}
	if actions[1]["kind"] != DeviceEventActionKindSecondary {
		t.Fatalf("reject action=%#v", actions[1])
	}
}

func TestDeviceGatewayEventPushRequiresDeclaredCapability(t *testing.T) {
	// A client that accepts text but never declared event support must not
	// receive an actionable card it has no way to present.
	textOnly := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{
			Modalities: []string{"text"},
			Text:       &agent.ClientTextCapabilities{MaxChars: 240},
		},
	})
	gateway := newEventPushGateway(t, "pet-text-only", textOnly)
	gateway.EnqueueReply("pet-text-only", "system", fullEventReply(nil))
	if messages := queuedGatewayMessages(t, gateway, "pet-text-only"); len(messages) != 0 {
		t.Fatalf("event must be dropped without the capability: %#v", messages)
	}
	// The same client still receives ordinary text, so adding the capability
	// gate cannot regress existing reply paths.
	gateway.EnqueueReply("pet-text-only", "default", map[string]any{"type": "text", "text": "hello"})
	messages := queuedGatewayMessages(t, gateway, "pet-text-only")
	if len(messages) != 1 || messages[0]["text"] != "hello" {
		t.Fatalf("text reply regressed: %#v", messages)
	}
}

func TestDeviceGatewayEventPushRejectsMalformedClassification(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
	}{
		{"unknown category", map[string]any{"category": "gossip"}},
		{"missing category", map[string]any{"category": nil}},
		{"unknown severity", map[string]any{"severity": "screaming"}},
		{"missing severity", map[string]any{"severity": nil}},
		{"missing event id", map[string]any{"eventId": nil}},
		{"blank event id", map[string]any{"eventId": "   "}},
		{"missing title", map[string]any{"title": nil}},
		{"blank title", map[string]any{"title": "\t"}},
		{"zero ttl", map[string]any{"ttlSec": int64(0)}},
		{"negative ttl", map[string]any{"ttlSec": int64(-1)}},
		{"ttl beyond the bound", map[string]any{"ttlSec": int64(deviceEventMaxTTLSec + 1)}},
		{"oversized dedupe key", map[string]any{"dedupeKey": string(make([]byte, deviceEventMaxDedupeKeyLen+1))}},
		{"unknown action kind", map[string]any{"actions": []any{map[string]any{"id": "a", "label": "A", "kind": "ghost"}}}},
		{"unknown action risk", map[string]any{"actions": []any{map[string]any{"id": "a", "label": "A", "risk": "apocalyptic"}}}},
		{"action without id", map[string]any{"actions": []any{map[string]any{"label": "A"}}}},
		{"action without label", map[string]any{"actions": []any{map[string]any{"id": "a"}}}},
		{"actions not a list", map[string]any{"actions": "reject"}},
		{"action entry not an object", map[string]any{"actions": []any{"reject"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clientID := "pet-reject"
			gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
			gateway.EnqueueReply(clientID, "system", fullEventReply(testCase.overrides))
			if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 0 {
				t.Fatalf("malformed event must be rejected, got %#v", messages)
			}
		})
	}
}

// TestDeviceGatewayEventPushApprovalRequiresAuditFlags pins the one policy the
// wire contract enforces on top of pure schema validation: an approval without
// requiresAck or without persist can be silently lost, and D5-A requires the
// decision path to be auditable.
func TestDeviceGatewayEventPushApprovalRequiresAuditFlags(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
	}{
		{"approval without requiresAck", map[string]any{"requiresAck": false}},
		{"approval without persist", map[string]any{"persist": false}},
		{"approval without ttl", map[string]any{"ttlSec": nil}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clientID := "pet-approval-audit"
			gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
			gateway.EnqueueReply(clientID, "system", fullEventReply(testCase.overrides))
			if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 0 {
				t.Fatalf("approval missing its audit contract must be rejected: %#v", messages)
			}
		})
	}

	// A non-approval event is free to be ephemeral and unacked: that is the
	// "正在输入" case plan section 4.1 calls out as persist=false.
	clientID := "pet-ephemeral"
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
	gateway.EnqueueReply(clientID, "system", map[string]any{"type": "event", "event": map[string]any{
		"eventId": "evt-typing", "category": DeviceEventCategorySystem,
		"severity": DeviceEventSeveritySilent, "title": "正在输入",
	}})
	event := queuedEvent(t, gateway, clientID)
	if event["requiresAck"] != false || event["persist"] != false {
		t.Fatalf("ephemeral event flags=%#v", event)
	}
	if _, present := event["ttlSec"]; present {
		t.Fatalf("ephemeral event must omit ttl: %#v", event)
	}
	// dedupeKey defaults to eventId so idempotency stays available.
	if event["dedupeKey"] != "evt-typing" {
		t.Fatalf("dedupeKey default=%#v", event["dedupeKey"])
	}
}

func TestDeviceGatewayEventPushRejectsMissingPayload(t *testing.T) {
	gateway := newEventPushGateway(t, "pet-nopayload", eventPushCapabilities(240))
	gateway.EnqueueReply("pet-nopayload", "system", map[string]any{"type": "event"})
	if messages := queuedGatewayMessages(t, gateway, "pet-nopayload"); len(messages) != 0 {
		t.Fatalf("event without a payload must be rejected: %#v", messages)
	}
	gateway.EnqueueReply("pet-nopayload", "system", map[string]any{"type": "event", "event": "approve"})
	if messages := queuedGatewayMessages(t, gateway, "pet-nopayload"); len(messages) != 0 {
		t.Fatalf("non-object payload must be rejected: %#v", messages)
	}
}

func TestDeviceGatewayEventPushRejectsOversizedIdentityFields(t *testing.T) {
	// Identity fields are rejected rather than truncated: a shortened event id
	// or dedupe key would silently break event-ack correlation and the
	// "same dedupeKey is presented once" guarantee.
	longID := make([]byte, deviceEventMaxIDLen+1)
	for index := range longID {
		longID[index] = 'a'
	}
	longActionID := make([]byte, deviceEventMaxActionIDLen+1)
	for index := range longActionID {
		longActionID[index] = 'b'
	}
	cases := []struct {
		name      string
		overrides map[string]any
	}{
		{"oversized event id", map[string]any{"eventId": string(longID)}},
		{"oversized action id", map[string]any{"actions": []any{map[string]any{"id": string(longActionID), "label": "A"}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clientID := "pet-oversize"
			gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
			gateway.EnqueueReply(clientID, "system", fullEventReply(testCase.overrides))
			if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 0 {
				t.Fatalf("oversized identity field must be rejected, got %#v", messages)
			}
		})
	}
}

func TestDeviceGatewayEventPushTruncatesFreeTextOnly(t *testing.T) {
	clientID := "pet-truncate"
	// maxChars 0 means "transport default": the client declares no tighter
	// budget, so only the protocol caps apply and this test isolates them. The
	// declared-budget interaction is covered by
	// TestDeviceGatewayEventPushEffectiveCapIsTheSmallerOfTheTwo.
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(0))

	longTitle := make([]rune, deviceEventMaxTitleRunes+20)
	for index := range longTitle {
		longTitle[index] = '题'
	}
	longSummary := make([]rune, deviceEventMaxSummaryRunes+20)
	for index := range longSummary {
		longSummary[index] = '要'
	}
	longLabel := make([]rune, deviceEventMaxActionLabelRunes+10)
	for index := range longLabel {
		longLabel[index] = '按'
	}

	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{
		"title":   string(longTitle),
		"summary": string(longSummary),
		"actions": []any{map[string]any{"id": "ok", "label": string(longLabel)}},
	}))

	event := queuedEvent(t, gateway, clientID)
	checks := []struct {
		field string
		want  int
	}{
		{"title", deviceEventMaxTitleRunes},
		{"summary", deviceEventMaxSummaryRunes},
	}
	for _, check := range checks {
		value, ok := event[check.field].(string)
		if !ok {
			t.Fatalf("%s missing: %#v", check.field, event)
		}
		if !utf8.ValidString(value) {
			t.Fatalf("%s was cut mid-rune: %q", check.field, value)
		}
		if runes := []rune(value); len(runes) != check.want {
			t.Fatalf("%s rune count=%d want %d", check.field, len(runes), check.want)
		}
	}
	actions := event["actions"].([]map[string]any)
	if label := actions[0]["label"].(string); len([]rune(label)) != deviceEventMaxActionLabelRunes {
		t.Fatalf("action label rune count=%d", len([]rune(label)))
	}
	// Identity and timing survived untouched.
	if event["eventId"] != "evt_01H8ZK" || event["dedupeKey"] != "approval:inst-123:step-2" {
		t.Fatalf("identity was altered: %#v", event)
	}
	if event["ttlSec"] != int64(300) {
		t.Fatalf("ttl was altered: %#v", event["ttlSec"])
	}
}

func TestDeviceGatewayEventPushHonoursDeclaredTextBudget(t *testing.T) {
	clientID := "pet-budget"
	// 12 runes of budget: short enough to prove the clamp runs, and the kind of
	// declaration a small screen would make.
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(12))
	gateway.EnqueueReply(clientID, "system", fullEventReply(nil))

	event := queuedEvent(t, gateway, clientID)
	for _, field := range []string{"title", "summary"} {
		value, ok := event[field].(string)
		if !ok {
			t.Fatalf("%s missing: %#v", field, event)
		}
		if runes := []rune(value); len(runes) > 12 {
			t.Fatalf("%s exceeded the declared budget: %q (%d runes)", field, value, len(runes))
		}
	}
	if event["eventId"] != "evt_01H8ZK" || event["category"] != DeviceEventCategoryApproval {
		t.Fatalf("clamping must not touch identity fields: %#v", event)
	}
	actions := event["actions"].([]map[string]any)
	if label := actions[0]["label"].(string); len([]rune(label)) > 12 {
		t.Fatalf("action label exceeded the declared budget: %q", label)
	}
}

// TestDeviceGatewayEventPushEffectiveCapIsTheSmallerOfTheTwo pins the
// interaction between the protocol bound and the declared display budget: the
// device must never receive more text than either allows.
func TestDeviceGatewayEventPushEffectiveCapIsTheSmallerOfTheTwo(t *testing.T) {
	longTitle := make([]rune, deviceEventMaxTitleRunes+50)
	for index := range longTitle {
		longTitle[index] = '题'
	}
	longSummary := make([]rune, deviceEventMaxSummaryRunes+50)
	for index := range longSummary {
		longSummary[index] = '要'
	}

	cases := []struct {
		name          string
		declaredChars int
		wantTitle     int
		wantSummary   int
	}{
		// Budget below the protocol cap: the budget wins.
		{"tight budget wins", 240, deviceEventMaxTitleRunes, 240},
		// Budget above the protocol cap: the protocol cap wins.
		{"protocol cap wins", 1000, deviceEventMaxTitleRunes, deviceEventMaxSummaryRunes},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			clientID := "pet-effective-cap"
			gateway := newEventPushGateway(t, clientID, eventPushCapabilities(testCase.declaredChars))
			gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{
				"title":   string(longTitle),
				"summary": string(longSummary),
			}))
			event := queuedEvent(t, gateway, clientID)
			if runes := []rune(event["title"].(string)); len(runes) != testCase.wantTitle {
				t.Fatalf("title runes=%d want %d", len(runes), testCase.wantTitle)
			}
			if runes := []rune(event["summary"].(string)); len(runes) != testCase.wantSummary {
				t.Fatalf("summary runes=%d want %d", len(runes), testCase.wantSummary)
			}
		})
	}
}

func TestDeviceGatewayEventPushCapsActionCount(t *testing.T) {
	clientID := "pet-many-actions"
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
	actions := make([]any, 0, deviceEventMaxActions+1)
	for index := 0; index <= deviceEventMaxActions; index++ {
		actions = append(actions, map[string]any{
			"id":    string(rune('a' + index)),
			"label": string(rune('A' + index)),
		})
	}
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{"actions": actions}))
	if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 0 {
		t.Fatalf("too many actions must be rejected: %#v", messages)
	}

	// Exactly the cap is still accepted.
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{"actions": actions[:deviceEventMaxActions]}))
	if event := queuedEvent(t, gateway, clientID); len(event["actions"].([]map[string]any)) != deviceEventMaxActions {
		t.Fatalf("actions at the cap must be delivered: %#v", event["actions"])
	}
}

func TestDeviceGatewayEventPushRejectsDuplicateActionIDs(t *testing.T) {
	clientID := "pet-dup-actions"
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{
		"actions": []any{
			map[string]any{"id": "approve", "label": "同意"},
			map[string]any{"id": "approve", "label": "也同意"},
		},
	}))
	// Two buttons sharing an id would make the audit trail ambiguous about what
	// the user actually approved, so this fails closed.
	if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 0 {
		t.Fatalf("duplicate action ids must be rejected: %#v", messages)
	}
}

func TestDeviceGatewayEventPushAcceptsSingleActionObject(t *testing.T) {
	clientID := "pet-single-action"
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{
		"actions": map[string]any{"id": "ack", "label": "知道了"},
	}))
	event := queuedEvent(t, gateway, clientID)
	actions, ok := event["actions"].([]map[string]any)
	if !ok || len(actions) != 1 || actions[0]["id"] != "ack" {
		t.Fatalf("single action object=%#v", event["actions"])
	}
	if actions[0]["kind"] != DeviceEventActionKindSecondary {
		t.Fatalf("kind default=%#v", actions[0]["kind"])
	}
}

func TestDeviceGatewayEventPushReadsQuotedBooleanFlags(t *testing.T) {
	// Hand-written and LLM-produced payloads routinely quote booleans.
	clientID := "pet-quoted-flags"
	gateway := newEventPushGateway(t, clientID, eventPushCapabilities(240))
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{
		"requiresAck": "TRUE",
		"persist":     " true ",
	}))
	event := queuedEvent(t, gateway, clientID)
	if event["requiresAck"] != true || event["persist"] != true {
		t.Fatalf("quoted booleans were not read: %#v", event)
	}

	// A malformed flag must never be read as an affirmative.
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{"requiresAck": "yes", "persist": 1}))
	if messages := queuedGatewayMessages(t, gateway, clientID); len(messages) != 1 {
		t.Fatalf("malformed flags must not satisfy the approval contract: %#v", messages)
	}
}

func TestDeviceGatewayEventPushRejectsClientWithoutTextOutput(t *testing.T) {
	// An event card is still text the device has to draw. A client that cannot
	// render text cannot render an event either.
	audioOnly := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
		Output:   agent.ClientOutputCapabilities{Modalities: []string{"audio"}, Audio: &agent.ClientAudioCapabilities{Playback: true}},
		Features: agent.ClientFeatureCapabilities{EventPush: true},
	})
	gateway := newEventPushGateway(t, "pet-audio-only", audioOnly)
	gateway.EnqueueReply("pet-audio-only", "system", fullEventReply(nil))
	if messages := queuedGatewayMessages(t, gateway, "pet-audio-only"); len(messages) != 0 {
		t.Fatalf("event must require text output: %#v", messages)
	}
}

func TestDeviceEventPushSupportedRequiresBothDeclarations(t *testing.T) {
	cases := []struct {
		name         string
		capabilities agent.ClientCapabilities
		want         bool
	}{
		{"event push and text", eventPushCapabilities(240), true},
		{
			"event push without text",
			agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
				Output:   agent.ClientOutputCapabilities{Modalities: []string{"audio"}, Audio: &agent.ClientAudioCapabilities{Playback: true}},
				Features: agent.ClientFeatureCapabilities{EventPush: true},
			}),
			false,
		},
		{
			"text without event push",
			agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
				Output: agent.ClientOutputCapabilities{Modalities: []string{"text"}},
			}),
			false,
		},
		{"legacy zero value", agent.ClientCapabilities{}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := DeviceEventPushSupported(testCase.capabilities); got != testCase.want {
				t.Fatalf("DeviceEventPushSupported=%t want %t", got, testCase.want)
			}
		})
	}
}

func TestNormalizeDeviceEventPushTrimsAndLowercasesClassification(t *testing.T) {
	normalized, ok := normalizeDeviceEventPush(map[string]any{
		"eventId":  "  evt-1  ",
		"category": "  Task_Done  ",
		"severity": " SOFT ",
		"title":    "  跑完了  ",
	})
	if !ok {
		t.Fatal("a trimmed, case-insensitive payload must normalize")
	}
	if normalized["eventId"] != "evt-1" || normalized["category"] != DeviceEventCategoryTaskDone {
		t.Fatalf("normalized=%#v", normalized)
	}
	if normalized["severity"] != DeviceEventSeveritySoft || normalized["title"] != "跑完了" {
		t.Fatalf("normalized=%#v", normalized)
	}
}

func TestNormalizeDeviceEventPushNilAndNonObjectAreRejected(t *testing.T) {
	for _, raw := range []any{nil, "approval", 42, []any{}, map[string]any{}} {
		if _, ok := normalizeDeviceEventPush(raw); ok {
			t.Fatalf("raw=%#v must be rejected", raw)
		}
	}
}

func TestClampDeviceEventTextIgnoresNonPositiveBudget(t *testing.T) {
	normalized, ok := normalizeDeviceEventPush(fullEventReply(nil)["event"])
	if !ok {
		t.Fatal("fixture must normalize")
	}
	// A zero budget means "transport default", not "erase the text".
	clampDeviceEventText(normalized, 0)
	if normalized["title"] != "需要你确认" {
		t.Fatalf("zero budget must not clamp: %#v", normalized["title"])
	}
	// Nil maps and absent action lists must not panic.
	clampDeviceEventText(nil, 10)
	clampDeviceEventText(map[string]any{"title": "abc"}, 10)
}
