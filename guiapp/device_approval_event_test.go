package guiapp

import (
	"strings"
	"testing"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func deviceApprovalTestNow() time.Time {
	return time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)
}

func highRiskApproval(tool string, args map[string]any) registeredToolPendingApproval {
	return registeredToolPendingApproval{
		ID:       "tool-approval-7",
		ToolName: tool,
		Args:     args,
		Risk: security.RiskAssessment{
			Level:  security.RiskHigh,
			Reason: "matched a high-risk pattern",
		},
		CreatedAt: deviceApprovalTestNow(),
	}
}

// D5-A is a *positive* test on the declared level. An approval whose risk was
// never assessed must not reach the device: treating a missing level as "safe
// enough to confirm from across the room" is exactly the failure mode the
// contract's "risk absent != risk=low" rule exists to prevent.
func TestBuildToolApprovalDeviceEventRequiresAnExplicitHighRisk(t *testing.T) {
	for _, level := range []security.RiskLevel{
		security.RiskLow, security.RiskMedium,
		security.RiskLevel("none"), security.RiskLevel(""),
	} {
		approval := highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"})
		approval.Risk.Level = level
		if _, ok := buildToolApprovalDeviceEvent(approval, deviceApprovalTestNow()); ok {
			t.Fatalf("level=%q must not become a device card", level)
		}
	}
	for _, level := range []security.RiskLevel{security.RiskHigh, security.RiskCritical} {
		approval := highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"})
		approval.Risk.Level = level
		if _, ok := buildToolApprovalDeviceEvent(approval, deviceApprovalTestNow()); !ok {
			t.Fatalf("level=%q must become a device card", level)
		}
	}
}

// The Hub rejects an approval event outright unless all three audit fields are
// present, and each omission fails silently. This test is the producer-side half
// of that contract.
func TestBuildToolApprovalDeviceEventSatisfiesTheApprovalAuditContract(t *testing.T) {
	event, ok := buildToolApprovalDeviceEvent(
		highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"}),
		deviceApprovalTestNow())
	if !ok {
		t.Fatal("a high-risk approval must produce an event")
	}
	if event["category"] != coreim.DeviceEventCategoryApproval {
		t.Fatalf("category=%#v", event["category"])
	}
	// interrupt is the reason the admission matrix exists: a high-risk approval
	// is the one case where waiting for the user to reach their laptop is worse
	// than interrupting them. The device downgrades it during quiet hours and
	// during a recording, so the producer does not need to know the time of day.
	if event["severity"] != coreim.DeviceEventSeverityInterrupt {
		t.Fatalf("severity=%#v", event["severity"])
	}
	if event["requiresAck"] != true {
		t.Fatalf("requiresAck=%#v: without it nobody learns the card never arrived", event["requiresAck"])
	}
	if event["persist"] != true {
		t.Fatalf("persist=%#v: without it a pending approval vanishes on reboot", event["persist"])
	}
	ttl, ok := event["ttlSec"].(int64)
	if !ok || ttl <= 0 {
		t.Fatalf("ttlSec=%#v: the wire contract forces a positive window", event["ttlSec"])
	}
	if ttl < coreim.DeviceEventMinTTLSec || ttl > coreim.DeviceEventMaxTTLSec {
		t.Fatalf("ttlSec=%d is outside the protocol window", ttl)
	}
	dedupe, _ := event["dedupeKey"].(string)
	if dedupe == "" || len(dedupe) > coreim.DeviceEventMaxDedupeKeyLen {
		t.Fatalf("dedupeKey=%q (%d bytes)", dedupe, len(dedupe))
	}
	id, _ := event["eventId"].(string)
	if id == "" || len(id) > coreim.DeviceEventMaxIDLen {
		t.Fatalf("eventId=%q (%d bytes)", id, len(id))
	}
	if _, leaked := event["expiresAtUnixMs"]; leaked {
		t.Fatal("the desktop must not mint the Hub's absolute expiry")
	}
}

func TestBuildToolApprovalDeviceEventOffersApproveAndReject(t *testing.T) {
	event, ok := buildToolApprovalDeviceEvent(
		highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"}),
		deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	actions, ok := event["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions=%#v", event["actions"])
	}
	if actions[0]["id"] != "approve" || actions[0]["kind"] != coreim.DeviceEventActionKindPrimary {
		t.Fatalf("the affirmative action must be the primary one: %#v", actions[0])
	}
	// kind is presentation emphasis; risk is the consequence. D5-A reads risk,
	// so a styling change must not be able to alter the safety decision.
	if actions[0]["risk"] != coreim.DeviceEventActionRiskHigh {
		t.Fatalf("approve.risk=%#v", actions[0]["risk"])
	}
	if actions[1]["id"] != "reject" || actions[1]["kind"] != coreim.DeviceEventActionKindSecondary {
		t.Fatalf("actions[1]=%#v", actions[1])
	}
	// Only the affirmative action carries a risk: rejecting is always safe.
	if _, hasRisk := actions[1]["risk"]; hasRisk {
		t.Fatalf("reject must not carry a risk label: %#v", actions[1])
	}
}

// "Blind confirmation is no confirmation" (D5-C15): the card has to name what is
// about to happen, or the user is being asked to approve a mystery.
func TestBuildToolApprovalDeviceEventNamesTheConcreteObject(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		args     map[string]any
		mustHave string
	}{
		{"shell command", "bash", map[string]any{"command": "rm -rf ~/Downloads"}, "rm -rf ~/Downloads"},
		{"database write", "database", map[string]any{"action": "delete", "sql": "DELETE FROM t"}, "DELETE FROM t"},
		{"file path", "write", map[string]any{"path": "/tmp/report.xlsx"}, "/tmp/report.xlsx"},
		{"archive target", "archive", map[string]any{"path": "/tmp/a.zip"}, "/tmp/a.zip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, ok := buildToolApprovalDeviceEvent(
				highRiskApproval(tc.tool, tc.args), deviceApprovalTestNow())
			if !ok {
				t.Fatal("expected an event")
			}
			summary, _ := event["summary"].(string)
			if !strings.Contains(summary, tc.mustHave) {
				t.Fatalf("summary=%q must name %q", summary, tc.mustHave)
			}
			// The title stays neutral; the specificity belongs in summary, and
			// duplicating it in both would make the two drift.
			if event["title"] != "需要你确认" {
				t.Fatalf("title=%#v", event["title"])
			}
		})
	}
}

// When no concrete operand can be found, the analyzer's reason is the most
// specific thing left. The card must still say *something* about the operation
// rather than degrading into a bare tool name.
func TestBuildToolApprovalDeviceEventFallsBackToTheRiskReason(t *testing.T) {
	approval := highRiskApproval("some_unknown_tool", map[string]any{"unrelated": 1})
	event, ok := buildToolApprovalDeviceEvent(approval, deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	summary, _ := event["summary"].(string)
	if !strings.Contains(summary, approval.Risk.Reason) {
		t.Fatalf("summary=%q must carry the assessed reason %q", summary, approval.Risk.Reason)
	}
}

// The card travels over the Hub and is persisted in the machine snapshot, so a
// secret that reaches it is written to disk outside the desktop's control.
func TestBuildToolApprovalDeviceEventNeverCarriesASecretValue(t *testing.T) {
	const secret = "sk-live-do-not-leak"
	event, ok := buildToolApprovalDeviceEvent(
		highRiskApproval("write", map[string]any{
			"path":      "/tmp/report.xlsx",
			"api_token": secret,
		}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	summary, _ := event["summary"].(string)
	if strings.Contains(summary, secret) {
		t.Fatalf("summary leaked a secret: %q", summary)
	}
	if !strings.Contains(summary, "/tmp/report.xlsx") {
		t.Fatalf("summary=%q lost the real operand", summary)
	}
}

// Without an id the decision cannot be correlated back to the pending approval,
// so the card would be a prompt the user can answer but nobody can act on.
func TestBuildToolApprovalDeviceEventRejectsAnApprovalWithoutAnID(t *testing.T) {
	approval := highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"})
	approval.ID = "   "
	if _, ok := buildToolApprovalDeviceEvent(approval, deviceApprovalTestNow()); ok {
		t.Fatal("an approval without an id must not become a card")
	}
}

// A long command must be cut on a rune boundary: splitting a CJK character
// produces invalid UTF-8 that the device font renderer cannot draw.
func TestBuildToolApprovalDeviceEventTruncatesOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("删", 400)
	event, ok := buildToolApprovalDeviceEvent(
		highRiskApproval("bash", map[string]any{"command": long}), deviceApprovalTestNow())
	if !ok {
		t.Fatal("expected an event")
	}
	summary, _ := event["summary"].(string)
	if len([]rune(summary)) > deviceApprovalMaxSummaryRunes+1 {
		t.Fatalf("summary is %d runes", len([]rune(summary)))
	}
	for _, r := range summary {
		if r == '\uFFFD' {
			t.Fatalf("truncation split a rune: %q", summary)
		}
	}
	// The wire contract caps summary at 400 runes; stay well inside it.
	if len([]rune(summary)) > coreim.DeviceEventMaxSummaryRunes {
		t.Fatalf("summary is %d runes, over the protocol bound", len([]rune(summary)))
	}
}

// The key is the pending approval's identity, not a hash of its arguments. Two
// separately-created approvals that happen to share arguments are two separate
// questions, and suppressing the second would let a rejection silently veto a
// later, unrelated request.
func TestBuildToolApprovalDeviceEventDedupeKeyFollowsTheApprovalID(t *testing.T) {
	args := map[string]any{"command": "rm -rf /tmp/x"}
	first := highRiskApproval("bash", args)
	first.ID = "tool-approval-1"
	second := highRiskApproval("bash", args)
	second.ID = "tool-approval-2"

	a, okA := buildToolApprovalDeviceEvent(first, deviceApprovalTestNow())
	b, okB := buildToolApprovalDeviceEvent(second, deviceApprovalTestNow())
	if !okA || !okB {
		t.Fatal("expected both events")
	}
	if a["dedupeKey"] == b["dedupeKey"] {
		t.Fatalf("two distinct approvals shared dedupeKey %#v", a["dedupeKey"])
	}
	if a["dedupeKey"] != "approval:tool-approval-1" {
		t.Fatalf("dedupeKey=%#v", a["dedupeKey"])
	}
	// Re-deriving the same approval must be idempotent for the device.
	again, _ := buildToolApprovalDeviceEvent(first, deviceApprovalTestNow())
	if again["dedupeKey"] != a["dedupeKey"] {
		t.Fatalf("the same approval produced two keys: %#v vs %#v", again["dedupeKey"], a["dedupeKey"])
	}
}

func TestDeviceApprovalRiskLabelMapsOntoTheWireSet(t *testing.T) {
	cases := map[security.RiskLevel]string{
		security.RiskCritical: coreim.DeviceEventActionRiskHigh,
		security.RiskHigh:     coreim.DeviceEventActionRiskHigh,
		security.RiskMedium:   coreim.DeviceEventActionRiskMedium,
		security.RiskLow:      coreim.DeviceEventActionRiskLow,
	}
	for level, want := range cases {
		if got := deviceApprovalRiskLabel(level); got != want {
			t.Fatalf("level=%q got %q want %q", level, got, want)
		}
	}
}

// The device push is additive to the desktop flow. A GUI running without a Hub
// must not fail the approval, and a delivery failure must never be propagated:
// making the companion device a new single point of failure for the user's work
// would be worse than not having the card at all.
func TestPushToolApprovalToDevicesIsSafeWithoutAHub(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	// Must not panic, and must not report anything to the caller: it returns
	// nothing by construction.
	handler.pushToolApprovalToDevices(highRiskApproval("bash", map[string]any{"command": "rm -rf /tmp/x"}))
	// A nil handler and a nil app are both no-ops.
	var nilHandler *IMMessageHandler
	nilHandler.pushToolApprovalToDevices(highRiskApproval("bash", nil))
	(&IMMessageHandler{}).pushToolApprovalToDevices(highRiskApproval("bash", nil))
}
