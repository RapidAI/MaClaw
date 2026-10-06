package im

import (
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
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

// textOnlyCapabilities is a device that draws text but never declared event
// support. It is the case the fan-out must not silently downgrade a card into.
func textOnlyCapabilities(maxChars int) agent.ClientCapabilities {
	return agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{
			Modalities: []string{"text"},
			Text:       &agent.ClientTextCapabilities{MaxChars: maxChars},
		},
	})
}

// bindEventPushDevice registers a paired device under a machine. The fan-out
// resolves its targets from the token table and reads capabilities off the
// client state, so both must be present for a device to count as reachable.
func bindEventPushDevice(t *testing.T, gateway *DeviceGateway, clientID, machineID string, capabilities agent.ClientCapabilities) {
	t.Helper()
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.tokens["tok-"+clientID] = devicePrincipal{ClientID: clientID, MachineID: machineID}
	gateway.clientLocked(clientID).capabilities = capabilities
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
		"category":  coreim.DeviceEventCategoryApproval,
		"severity":  coreim.DeviceEventSeverityInterrupt,
		"title":     "需要你确认",
		"summary":   "要删除 ~/Downloads/report-final-v2.xlsx 吗？",
		"ttlSec":    int64(300),
		"dedupeKey": "approval:inst-123:step-2",
		"actions": []any{
			map[string]any{"id": "approve", "label": "同意", "kind": coreim.DeviceEventActionKindPrimary, "risk": coreim.DeviceEventActionRiskHigh},
			map[string]any{"id": "reject", "label": "拒绝", "kind": coreim.DeviceEventActionKindSecondary},
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
	if event["eventId"] != "evt_01H8ZK" || event["category"] != coreim.DeviceEventCategoryApproval {
		t.Fatalf("identity fields=%#v", event)
	}
	if event["severity"] != coreim.DeviceEventSeverityInterrupt {
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
	if actions[0]["kind"] != coreim.DeviceEventActionKindPrimary || actions[0]["risk"] != coreim.DeviceEventActionRiskHigh {
		t.Fatalf("approve action=%#v", actions[0])
	}
	// An action that never declared a risk must not be silently upgraded to
	// low: D5-A gates the whole flow on a positive risk=high test.
	if _, present := actions[1]["risk"]; present {
		t.Fatalf("undeclared risk must stay absent: %#v", actions[1])
	}
	if actions[1]["kind"] != coreim.DeviceEventActionKindSecondary {
		t.Fatalf("reject action=%#v", actions[1])
	}
}

func TestDeviceGatewayEventPushRequiresDeclaredCapability(t *testing.T) {
	// A client that accepts text but never declared event support must not
	// receive an actionable card it has no way to present.
	textOnly := textOnlyCapabilities(240)
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
		{"ttl beyond the bound", map[string]any{"ttlSec": int64(coreim.DeviceEventMaxTTLSec + 1)}},
		{"oversized dedupe key", map[string]any{"dedupeKey": string(make([]byte, coreim.DeviceEventMaxDedupeKeyLen+1))}},
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
		"eventId": "evt-typing", "category": coreim.DeviceEventCategorySystem,
		"severity": coreim.DeviceEventSeveritySilent, "title": "正在输入",
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
	longID := make([]byte, coreim.DeviceEventMaxIDLen+1)
	for index := range longID {
		longID[index] = 'a'
	}
	longActionID := make([]byte, coreim.DeviceEventMaxActionIDLen+1)
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

	longTitle := make([]rune, coreim.DeviceEventMaxTitleRunes+20)
	for index := range longTitle {
		longTitle[index] = '题'
	}
	longSummary := make([]rune, coreim.DeviceEventMaxSummaryRunes+20)
	for index := range longSummary {
		longSummary[index] = '要'
	}
	longLabel := make([]rune, coreim.DeviceEventMaxActionLabelRunes+10)
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
		{"title", coreim.DeviceEventMaxTitleRunes},
		{"summary", coreim.DeviceEventMaxSummaryRunes},
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
	if label := actions[0]["label"].(string); len([]rune(label)) != coreim.DeviceEventMaxActionLabelRunes {
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
	if event["eventId"] != "evt_01H8ZK" || event["category"] != coreim.DeviceEventCategoryApproval {
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
	longTitle := make([]rune, coreim.DeviceEventMaxTitleRunes+50)
	for index := range longTitle {
		longTitle[index] = '题'
	}
	longSummary := make([]rune, coreim.DeviceEventMaxSummaryRunes+50)
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
		{"tight budget wins", 240, coreim.DeviceEventMaxTitleRunes, 240},
		// Budget above the protocol cap: the protocol cap wins.
		{"protocol cap wins", 1000, coreim.DeviceEventMaxTitleRunes, coreim.DeviceEventMaxSummaryRunes},
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
	actions := make([]any, 0, coreim.DeviceEventMaxActions+1)
	for index := 0; index <= coreim.DeviceEventMaxActions; index++ {
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
	gateway.EnqueueReply(clientID, "system", fullEventReply(map[string]any{"actions": actions[:coreim.DeviceEventMaxActions]}))
	if event := queuedEvent(t, gateway, clientID); len(event["actions"].([]map[string]any)) != coreim.DeviceEventMaxActions {
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
	if actions[0]["kind"] != coreim.DeviceEventActionKindSecondary {
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
	if normalized["eventId"] != "evt-1" || normalized["category"] != coreim.DeviceEventCategoryTaskDone {
		t.Fatalf("normalized=%#v", normalized)
	}
	if normalized["severity"] != coreim.DeviceEventSeveritySoft || normalized["title"] != "跑完了" {
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

// ---------------------------------------------------------------------------
// N1-3 UpdateMachineEvent fan-out and the handshake replay snapshot.
// ---------------------------------------------------------------------------

// deviceEventPushTestNow is a fixed clock, so expiry arithmetic is asserted
// exactly instead of within a tolerance.
func deviceEventPushTestNow() time.Time {
	return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
}

// silentSystemEvent is the fire-and-forget shape: a notification with no
// decision to make and no window to respect.
func silentSystemEvent(overrides map[string]any) map[string]any {
	event := map[string]any{
		"eventId":  "evt_sys_01H8ZK",
		"category": coreim.DeviceEventCategorySystem,
		"severity": coreim.DeviceEventSeveritySilent,
		"title":    "正在输入",
	}
	for key, value := range overrides {
		if value == nil {
			delete(event, key)
			continue
		}
		event[key] = value
	}
	return event
}

func TestUpdateMachineEventFansOutToEveryCapableDeviceUnderTheMachine(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	bindEventPushDevice(t, gateway, "pet-a", "machine-1", eventPushCapabilities(240))
	bindEventPushDevice(t, gateway, "pet-b", "machine-1", eventPushCapabilities(240))
	bindEventPushDevice(t, gateway, "pet-c", "machine-1", eventPushCapabilities(240))
	// A device of a different GUI must never see this machine's events.
	bindEventPushDevice(t, gateway, "stranger", "machine-2", eventPushCapabilities(240))

	gateway.UpdateMachineEvent("machine-1", fullEventReply(nil)["event"].(map[string]any))

	for _, clientID := range []string{"pet-a", "pet-b", "pet-c"} {
		event := queuedEvent(t, gateway, clientID)
		if event["eventId"] != "evt_01H8ZK" {
			t.Fatalf("%s received %#v", clientID, event)
		}
		if event["category"] != coreim.DeviceEventCategoryApproval {
			t.Fatalf("%s category=%#v", clientID, event["category"])
		}
	}
	if messages := queuedGatewayMessages(t, gateway, "stranger"); len(messages) != 0 {
		t.Fatalf("another machine must not receive the event: %#v", messages)
	}
}

func TestUpdateMachineEventSkipsDevicesThatCannotRenderEvents(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	bindEventPushDevice(t, gateway, "capable", "machine-1", eventPushCapabilities(240))
	bindEventPushDevice(t, gateway, "text-only", "machine-1", textOnlyCapabilities(240))

	gateway.UpdateMachineEvent("machine-1", fullEventReply(nil)["event"].(map[string]any))

	if event := queuedEvent(t, gateway, "capable"); event["eventId"] != "evt_01H8ZK" {
		t.Fatalf("capable device=%#v", event)
	}
	// The fan-out builds its message directly and never passes through
	// adaptDeviceGatewayReply, so this skip is the only thing protecting a
	// text-only device from a card it would have to drop.
	if messages := queuedGatewayMessages(t, gateway, "text-only"); len(messages) != 0 {
		t.Fatalf("a text-only device must not receive a card: %#v", messages)
	}
	// The snapshot is kept regardless, so a device that later reconnects with
	// eventPush still learns about the pending event.
	gateway.mu.Lock()
	_, snapshotted := gateway.eventsByMachine["machine-1"]
	gateway.mu.Unlock()
	if !snapshotted {
		t.Fatal("a persist event must be snapshotted even with no capable device online")
	}
}

func TestUpdateMachineEventDoesNotSnapshotNonPersistEvent(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	bindEventPushDevice(t, gateway, "pet-a", "machine-1", eventPushCapabilities(240))

	gateway.UpdateMachineEvent("machine-1", silentSystemEvent(nil))

	if event := queuedEvent(t, gateway, "pet-a"); event["category"] != coreim.DeviceEventCategorySystem {
		t.Fatalf("event=%#v", event)
	}
	gateway.mu.Lock()
	_, snapshotted := gateway.eventsByMachine["machine-1"]
	gateway.mu.Unlock()
	if snapshotted {
		t.Fatal("persist=false must not be snapshotted: it would replay forever")
	}
}

func TestUpdateMachineEventDropsMalformedEvent(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	bindEventPushDevice(t, gateway, "pet-a", "machine-1", eventPushCapabilities(240))

	gateway.UpdateMachineEvent("machine-1", map[string]any{
		"eventId": "evt_bad", "category": "not_a_category",
		"severity": coreim.DeviceEventSeveritySoft, "title": "x",
	})

	if messages := queuedGatewayMessages(t, gateway, "pet-a"); len(messages) != 0 {
		t.Fatalf("a malformed event must not be delivered: %#v", messages)
	}
	gateway.mu.Lock()
	_, snapshotted := gateway.eventsByMachine["machine-1"]
	gateway.mu.Unlock()
	if snapshotted {
		t.Fatal("a rejected event must not enter the snapshot")
	}
}

func TestPrepareDeviceEventPushStampsAbsoluteExpiryForPersistEvents(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the section 4.1 approval fixture must validate")
	}
	if !push.Persist {
		t.Fatal("persist=true must be reported so the caller snapshots it")
	}
	if want := now.Add(300 * time.Second).UnixMilli(); push.Stored.ExpiresAtUnixMs != want {
		t.Fatalf("expiresAtUnixMs=%d want %d", push.Stored.ExpiresAtUnixMs, want)
	}
	// The absolute expiry is Hub bookkeeping. Sending it would be an undeclared
	// wire field, and the device is told the remaining window via ttlSec.
	if _, leaked := push.Event["expiresAtUnixMs"]; leaked {
		t.Fatal("the absolute expiry must not cross the wire")
	}
	if push.Event["ttlSec"] != int64(300) {
		t.Fatalf("ttlSec=%#v", push.Event["ttlSec"])
	}
}

func TestPrepareDeviceEventPushAppliesPersistCeilingWithoutDeclaredTTL(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(silentSystemEvent(map[string]any{"persist": true}), now)
	if !ok {
		t.Fatal("a silent system event with persist must validate")
	}
	if !push.Persist {
		t.Fatal("persist=true must be reported")
	}
	// Only approvals are required to declare a ttl, but "survive a reboot"
	// without any deadline would replay a days-old card forever.
	want := now.Add(deviceEventPersistTTLSec * time.Second).UnixMilli()
	if push.Stored.ExpiresAtUnixMs != want {
		t.Fatalf("expiresAtUnixMs=%d want %d", push.Stored.ExpiresAtUnixMs, want)
	}
	if _, declared := push.Event["ttlSec"]; declared {
		t.Fatal("the ceiling must not be written into the wire event")
	}
}

func TestReplayDeviceEventPushNeverRefreshesTheWindow(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	// Four minutes into a five-minute window: replaying ttlSec=300 verbatim
	// would hand a stale approval a brand-new five minutes (finding C34).
	replayed, ok := replayDeviceEventPush(push.Stored, now.Add(240*time.Second))
	if !ok {
		t.Fatal("the window is still open")
	}
	if replayed["ttlSec"] != int64(60) {
		t.Fatalf("ttlSec=%#v want 60", replayed["ttlSec"])
	}
	if replayed["eventId"] != "evt_01H8ZK" || replayed["category"] != coreim.DeviceEventCategoryApproval {
		t.Fatalf("replayed=%#v", replayed)
	}
	actions, ok := replayed["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions=%#v", replayed["actions"])
	}
}

func TestReplayDeviceEventPushDropsClosedWindow(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	// Exactly at the deadline and well past it: a timed-out approval means
	// "not approved", so it must not be delivered at all.
	for _, elapsed := range []time.Duration{300 * time.Second, time.Hour} {
		if _, ok := replayDeviceEventPush(push.Stored, now.Add(elapsed)); ok {
			t.Fatalf("elapsed=%s must not replay", elapsed)
		}
	}
}

func TestReplayDeviceEventPushClampsFarFutureExpiry(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	tampered := push.Stored
	tampered.ExpiresAtUnixMs = now.Add(365 * 24 * time.Hour).UnixMilli()
	replayed, ok := replayDeviceEventPush(tampered, now)
	if !ok {
		t.Fatal("a far-future expiry is still replayable")
	}
	if replayed["ttlSec"] != int64(coreim.DeviceEventMaxTTLSec) {
		t.Fatalf("ttlSec=%#v want %d", replayed["ttlSec"], coreim.DeviceEventMaxTTLSec)
	}
}

func TestReplayDeviceEventPushDropsWindowWithoutAbsoluteExpiry(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	// A snapshot that kept ttlSec but lost the absolute expiry cannot be judged:
	// replaying it would refresh the window, so it is dropped instead.
	stripped := storedDeviceEvent{Event: push.Stored.Event}
	if _, ok := replayDeviceEventPush(stripped, now); ok {
		t.Fatal("a windowed event without a recorded expiry must be dropped")
	}
}

func TestReplayDeviceEventPushPassesThroughEventWithoutWindow(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(silentSystemEvent(map[string]any{"persist": true}), now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	// Nothing declared a window, so there is nothing to expire.
	stripped := storedDeviceEvent{Event: push.Stored.Event}
	replayed, ok := replayDeviceEventPush(stripped, now.Add(10*time.Hour))
	if !ok {
		t.Fatal("an event without a window has no deadline to miss")
	}
	if _, declared := replayed["ttlSec"]; declared {
		t.Fatalf("no window must be invented: %#v", replayed)
	}
	if replayed["title"] != "正在输入" {
		t.Fatalf("replayed=%#v", replayed)
	}
}

func TestCloneStoredDeviceEventAcceptsNormalizedShapeAndDeepCopies(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	// Precondition for the shape tolerance added to normalizeDeviceEventActions:
	// an in-memory normalized event carries []map[string]any, while a JSON
	// round-trip yields []any. Re-validating must accept both.
	if _, isTyped := push.Event["actions"].([]map[string]any); !isTyped {
		t.Fatalf("precondition failed: actions is %T", push.Event["actions"])
	}
	copied, ok := cloneStoredDeviceEvent(push.Stored)
	if !ok {
		t.Fatal("re-validating the normalized shape must succeed")
	}
	if copied.ExpiresAtUnixMs != push.Stored.ExpiresAtUnixMs {
		t.Fatalf("expiresAtUnixMs=%d want %d", copied.ExpiresAtUnixMs, push.Stored.ExpiresAtUnixMs)
	}
	copied.Event["title"] = "mutated"
	actions, _ := copied.Event["actions"].([]map[string]any)
	actions[0]["label"] = "mutated"
	if push.Stored.Event["title"] == "mutated" {
		t.Fatal("the copy must not alias the source event map")
	}
	original, _ := push.Stored.Event["actions"].([]map[string]any)
	if original[0]["label"] == "mutated" {
		t.Fatal("the copy must not alias the source action maps")
	}
}

func TestCloneStoredDeviceEventRejectsInvalidSnapshot(t *testing.T) {
	if _, ok := cloneStoredDeviceEvent(storedDeviceEvent{}); ok {
		t.Fatal("a nil event must be rejected")
	}
	incomplete := storedDeviceEvent{Event: map[string]any{"eventId": "evt_x"}}
	if _, ok := cloneStoredDeviceEvent(incomplete); ok {
		t.Fatal("an incomplete event must be rejected")
	}
}

func TestStoredDeviceEventSurvivesJSONRoundTrip(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	raw, err := json.Marshal(persistedDeviceCredentials{
		EventsByMachine: map[string]storedDeviceEvent{"machine-1": push.Stored},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded persistedDeviceCredentials
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	copied, ok := cloneStoredDeviceEvent(decoded.EventsByMachine["machine-1"])
	if !ok {
		t.Fatal("a snapshot that survived JSON must still validate")
	}
	if copied.ExpiresAtUnixMs != push.Stored.ExpiresAtUnixMs {
		t.Fatalf("expiresAtUnixMs=%d want %d", copied.ExpiresAtUnixMs, push.Stored.ExpiresAtUnixMs)
	}
	replayed, ok := replayDeviceEventPush(copied, now.Add(60*time.Second))
	if !ok {
		t.Fatal("the window is still open")
	}
	if replayed["ttlSec"] != int64(240) {
		t.Fatalf("ttlSec=%#v want 240", replayed["ttlSec"])
	}
	actions, ok := replayed["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions=%#v", replayed["actions"])
	}
}

func TestMachineEventSnapshotSurvivesHubRestart(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	bindEventPushDevice(t, gateway, "pet-a", "machine-1", eventPushCapabilities(240))
	gateway.UpdateMachineEvent("machine-1", fullEventReply(nil)["event"].(map[string]any))

	gateway.mu.Lock()
	raw, err := gateway.marshalPersistedCredentialsLocked()
	gateway.mu.Unlock()
	if err != nil {
		t.Fatalf("marshal persisted credentials: %v", err)
	}

	restarted := NewDeviceGateway(nil)
	if err := restarted.RestorePersistedCredentials(raw); err != nil {
		t.Fatalf("RestorePersistedCredentials: %v", err)
	}
	restarted.mu.Lock()
	stored, ok := restarted.eventsByMachine["machine-1"]
	restarted.mu.Unlock()
	if !ok {
		t.Fatal("a pending persist event must survive a Hub restart")
	}
	if stored.ExpiresAtUnixMs <= 0 {
		t.Fatal("the absolute expiry must survive too, or the replay would refresh it")
	}
	if _, leaked := stored.Event["expiresAtUnixMs"]; leaked {
		t.Fatal("bookkeeping must not leak into the wire event")
	}
	// One minute left of the original window, counted from the restart.
	replayed, ok := replayDeviceEventPush(stored, time.UnixMilli(stored.ExpiresAtUnixMs).Add(-60*time.Second))
	if !ok {
		t.Fatal("the window is still open")
	}
	if replayed["ttlSec"] != int64(60) {
		t.Fatalf("ttlSec=%#v want 60", replayed["ttlSec"])
	}
}

func TestMachineEventSnapshotDropsUnreadableEntryWithoutFailingRestore(t *testing.T) {
	// A corrupt event cache must not cost the operator every device pairing.
	raw, err := json.Marshal(persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{"tok-a": {ClientID: "pet-a", MachineID: "machine-1"}},
		EventsByMachine: map[string]storedDeviceEvent{
			"machine-1": {Event: map[string]any{"eventId": "evt_x"}, ExpiresAtUnixMs: 1},
			"machine-2": {Event: nil},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	gateway := NewDeviceGateway(nil)
	if err := gateway.RestorePersistedCredentials(string(raw)); err != nil {
		t.Fatalf("RestorePersistedCredentials: %v", err)
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(gateway.eventsByMachine) != 0 {
		t.Fatalf("unreadable entries must be dropped: %#v", gateway.eventsByMachine)
	}
	if len(gateway.tokens) != 1 {
		t.Fatalf("the pairing must survive: %#v", gateway.tokens)
	}
}

func TestLatestDeviceEventForMachineRecomputesTTL(t *testing.T) {
	now := deviceEventPushTestNow()
	push, ok := prepareDeviceEventPush(fullEventReply(nil)["event"], now)
	if !ok {
		t.Fatal("the fixture must validate")
	}
	gateway := NewDeviceGateway(nil)
	gateway.mu.Lock()
	gateway.eventsByMachine["machine-1"] = push.Stored
	event, ok := gateway.latestDeviceEventForMachineLocked("machine-1", now.Add(240*time.Second))
	gateway.mu.Unlock()
	if !ok {
		t.Fatal("the window is still open")
	}
	if event["ttlSec"] != int64(60) {
		t.Fatalf("ttlSec=%#v want 60", event["ttlSec"])
	}

	gateway.mu.Lock()
	_, stillPending := gateway.latestDeviceEventForMachineLocked("machine-1", now.Add(time.Hour))
	_, unknownMachine := gateway.latestDeviceEventForMachineLocked("machine-9", now)
	gateway.mu.Unlock()
	if stillPending {
		t.Fatal("a stale approval must not be handed to a reconnecting device")
	}
	if unknownMachine {
		t.Fatal("a machine with no snapshot has nothing to replay")
	}
}
