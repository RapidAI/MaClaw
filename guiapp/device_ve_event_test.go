package guiapp

import (
	"strings"
	"testing"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func workflowStatusTestPayload(overrides map[string]any) map[string]any {
	payload := map[string]any{
		"event":         "blocked",
		"status":        "blocked",
		"urgency":       "attention",
		"instance_id":   "wf-inst-9",
		"reason":        "等待财务审批",
		"workflow_name": "报销流程",
		"current_node":  "expense.approval",
	}
	for key, value := range overrides {
		payload[key] = value
	}
	return payload
}

// The Hub only sends blocked/escalation notifications on this channel, so the
// mapping encodes how much the user's attention is worth, not whether something
// is wrong.
func TestWorkflowStatusSeverityFollowsUrgency(t *testing.T) {
	cases := map[string]string{
		"critical":   coreim.DeviceEventSeverityInterrupt,
		"overdue":    coreim.DeviceEventSeveritySoft,
		"attention":  coreim.DeviceEventSeveritySilent,
		"CRITICAL":   coreim.DeviceEventSeverityInterrupt,
		" critical ": coreim.DeviceEventSeverityInterrupt,
		// The default bucket is the least specific classification, so an
		// unrecognised urgency must not buy the device a voice.
		"":      coreim.DeviceEventSeveritySilent,
		"weird": coreim.DeviceEventSeveritySilent,
	}
	for urgency, want := range cases {
		if got := workflowStatusSeverity(urgency); got != want {
			t.Fatalf("urgency=%q got %q want %q", urgency, got, want)
		}
	}
}

// A blocked workflow re-notifies every time the Hub re-evaluates it, and the
// user needs to learn about it once -- not once per re-evaluation.
func TestBuildWorkflowStatusDeviceEventKeysOnTheInstanceAndEvent(t *testing.T) {
	base := workflowStatusTestPayload(nil)
	first, ok := buildWorkflowStatusDeviceEvent(base, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	again, ok := buildWorkflowStatusDeviceEvent(base, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if first["dedupeKey"] != again["dedupeKey"] {
		t.Fatalf("re-notification changed the key: %#v vs %#v", first["dedupeKey"], again["dedupeKey"])
	}
	if first["eventId"] == again["eventId"] {
		t.Fatalf("two pushes collided on eventId %#v", first["eventId"])
	}

	escalated, ok := buildWorkflowStatusDeviceEvent(
		workflowStatusTestPayload(map[string]any{"event": "escalation"}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if escalated["dedupeKey"] == first["dedupeKey"] {
		t.Fatalf("a different event on the same instance must be a different card: %#v", escalated["dedupeKey"])
	}

	otherInstance, ok := buildWorkflowStatusDeviceEvent(
		workflowStatusTestPayload(map[string]any{"instance_id": "wf-inst-10"}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if otherInstance["dedupeKey"] == first["dedupeKey"] {
		t.Fatalf("a different instance must be a different card: %#v", otherInstance["dedupeKey"])
	}
}

// The decision this event implies is taken in the desktop ops panel. A device
// button nobody can act on would push a non-decision into the audit path D5-A
// reserves for high-risk approvals.
func TestBuildWorkflowStatusDeviceEventCarriesNoActions(t *testing.T) {
	event, ok := buildWorkflowStatusDeviceEvent(
		workflowStatusTestPayload(map[string]any{"urgency": "critical"}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if _, has := event["actions"]; has {
		t.Fatalf("a workflow notification must carry no actions: %#v", event["actions"])
	}
	// The desktop projection is authoritative and updates independently, so
	// nothing waits on a receipt from the device.
	if event["requiresAck"] != false {
		t.Fatalf("requiresAck=%#v", event["requiresAck"])
	}
}

// Without the instance id there is no stable identity, so every re-notification
// would look like a brand new problem.
func TestBuildWorkflowStatusDeviceEventRejectsUnaddressablePushes(t *testing.T) {
	if _, ok := buildWorkflowStatusDeviceEvent(nil, deviceApprovalTestNow()); ok {
		t.Fatal("an empty payload must not become a card")
	}
	if _, ok := buildWorkflowStatusDeviceEvent(map[string]any{}, deviceApprovalTestNow()); ok {
		t.Fatal("an empty payload must not become a card")
	}
	for _, missing := range []any{"", "   ", nil} {
		payload := workflowStatusTestPayload(map[string]any{"instance_id": missing})
		if _, ok := buildWorkflowStatusDeviceEvent(payload, deviceApprovalTestNow()); ok {
			t.Fatalf("instance_id=%#v must be rejected", missing)
		}
	}
}

func TestBuildWorkflowStatusDeviceEventFallsBackToTheNodeForTheSummary(t *testing.T) {
	payload := workflowStatusTestPayload(map[string]any{"reason": "", "details": ""})
	event, ok := buildWorkflowStatusDeviceEvent(payload, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	summary, _ := event["summary"].(string)
	if !strings.Contains(summary, "expense.approval") {
		t.Fatalf("summary=%q must name the blocking node", summary)
	}

	// Even with nothing to describe, the card must still say something.
	bare := workflowStatusTestPayload(map[string]any{"reason": "", "details": "", "current_node": ""})
	event, ok = buildWorkflowStatusDeviceEvent(bare, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if summary, _ := event["summary"].(string); strings.TrimSpace(summary) == "" {
		t.Fatal("a blocked workflow must never produce an empty summary")
	}
}

func TestBuildWorkflowStatusDeviceEventStaysWithinTheContract(t *testing.T) {
	event, ok := buildWorkflowStatusDeviceEvent(
		workflowStatusTestPayload(map[string]any{"urgency": "overdue"}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	if event["category"] != coreim.DeviceEventCategoryVE {
		t.Fatalf("category=%#v", event["category"])
	}
	if event["severity"] != coreim.DeviceEventSeveritySoft {
		t.Fatalf("severity=%#v", event["severity"])
	}
	if event["persist"] != true {
		t.Fatalf("persist=%#v", event["persist"])
	}
	if ttl, _ := event["ttlSec"].(int64); ttl < coreim.DeviceEventMinTTLSec || ttl > coreim.DeviceEventMaxTTLSec {
		t.Fatalf("ttlSec=%#v is outside the protocol window", event["ttlSec"])
	}
	if title, _ := event["title"].(string); title == "" || len([]rune(title)) > coreim.DeviceEventMaxTitleRunes {
		t.Fatalf("title=%q", title)
	}
	if summary, _ := event["summary"].(string); len([]rune(summary)) > coreim.DeviceEventMaxSummaryRunes {
		t.Fatalf("summary is %d runes, over the protocol bound", len([]rune(summary)))
	}
	if id, _ := event["eventId"].(string); id == "" || len(id) > coreim.DeviceEventMaxIDLen {
		t.Fatalf("eventId=%q (%d bytes)", id, len(id))
	}
	if key, _ := event["dedupeKey"].(string); key == "" || len(key) > coreim.DeviceEventMaxDedupeKeyLen {
		t.Fatalf("dedupeKey=%q (%d bytes)", key, len(key))
	}
	if _, leaked := event["expiresAtUnixMs"]; leaked {
		t.Fatal("the desktop must not mint the Hub's absolute expiry")
	}
}

// A long workflow name must be cut on a rune boundary, or the device font
// renderer receives invalid UTF-8.
func TestBuildWorkflowStatusDeviceEventTruncatesOnARuneBoundary(t *testing.T) {
	payload := workflowStatusTestPayload(map[string]any{
		"workflow_name": strings.Repeat("流", 400),
		"reason":        strings.Repeat("理", 400),
	})
	event, ok := buildWorkflowStatusDeviceEvent(payload, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	for _, key := range []string{"title", "summary"} {
		value, _ := event[key].(string)
		if strings.ContainsRune(value, '\uFFFD') {
			t.Fatalf("%s split a rune: %q", key, value)
		}
	}
}

// A GUI running without a Hub must not fail the workflow path.
func TestPushWorkflowStatusToDevicesIsSafeWithoutAHub(t *testing.T) {
	client := &RemoteHubClient{app: &App{}}
	client.pushWorkflowStatusToDevices(workflowStatusTestPayload(nil))
	var nilClient *RemoteHubClient
	nilClient.pushWorkflowStatusToDevices(workflowStatusTestPayload(nil))
	(&RemoteHubClient{}).pushWorkflowStatusToDevices(workflowStatusTestPayload(nil))
}
