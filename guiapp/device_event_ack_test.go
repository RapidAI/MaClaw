package guiapp

import (
	"fmt"
	"testing"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func deviceAckTestApproval(t *testing.T) registeredToolPendingApproval {
	t.Helper()
	approval := storeRegisteredToolPendingApproval(
		"bash",
		map[string]any{"command": "rm -rf /tmp/x"},
		"session-1",
		"owner-1",
		security.RiskAssessment{Level: security.RiskHigh, Reason: "high-risk pattern"},
	)
	t.Cleanup(func() { deleteRegisteredToolPendingApproval(approval.ID) })
	return approval
}

func pendingApprovalExists(id string) bool {
	_, ok := getRegisteredToolPendingApproval(id)
	return ok
}

// A receipt only proves the card arrived. Treating it as a decision would let a
// device approve a high-risk action merely by reporting that it displayed the
// prompt.
func TestApplyDeviceEventAckIgnoresAReceipt(t *testing.T) {
	resetDeviceEventApprovalStore()
	approval := deviceAckTestApproval(t)
	rememberDeviceEventApproval("evt_receipt", approval.ID)

	handler := &IMMessageHandler{}
	handler.applyDeviceEventAck("evt_receipt", coreim.DeviceEventAckStatusReceived, "", "")

	if !pendingApprovalExists(approval.ID) {
		t.Fatal("a receipt must not consume the pending approval")
	}
}

func TestApplyDeviceEventAckForAnUnknownEventIsANoOp(t *testing.T) {
	resetDeviceEventApprovalStore()
	handler := &IMMessageHandler{}
	// No link was recorded, so there is nothing this could approve. The point is
	// that it is dropped rather than guessed at.
	handler.applyDeviceEventAck("evt_never_pushed", coreim.DeviceEventAckStatusApproved, deviceApprovalApproveActionID, coreim.DeviceEventDecidedByButton)
}

// D5-A is fail-closed: only the button the card actually offered under
// `approve` approves. Any other pairing of status and action -- a confused
// firmware, or a hostile device inventing an action id -- is "not approved".
func TestApplyDeviceEventAckTreatsEveryOtherActionAsNotApproved(t *testing.T) {
	for _, actionID := range []string{"", "reject", "approve_everything", "APPROVE"} {
		t.Run("action="+actionID, func(t *testing.T) {
			resetDeviceEventApprovalStore()
			approval := deviceAckTestApproval(t)
			rememberDeviceEventApproval("evt_mismatch", approval.ID)

			handler := &IMMessageHandler{}
			handler.applyDeviceEventAck("evt_mismatch", coreim.DeviceEventAckStatusApproved, actionID, coreim.DeviceEventDecidedByVoice)

			if pendingApprovalExists(approval.ID) {
				t.Fatalf("action %q must not approve; the approval is still pending", actionID)
			}
		})
	}
}

func TestApplyDeviceEventAckRejectsOnARejection(t *testing.T) {
	resetDeviceEventApprovalStore()
	approval := deviceAckTestApproval(t)
	rememberDeviceEventApproval("evt_reject", approval.ID)

	handler := &IMMessageHandler{}
	handler.applyDeviceEventAck("evt_reject", coreim.DeviceEventAckStatusRejected, "reject", coreim.DeviceEventDecidedByButton)

	if pendingApprovalExists(approval.ID) {
		t.Fatal("a rejection must consume the pending approval")
	}
}

// A terminal timeout means "nothing happened", not "rejected" and definitely
// not "approved". The desktop prompt must stay answerable: the user is still
// sitting in front of it, and closing it would turn a device-side hiccup into a
// lost decision.
func TestApplyDeviceEventAckExpiryLeavesTheDesktopPromptAnswerable(t *testing.T) {
	resetDeviceEventApprovalStore()
	approval := deviceAckTestApproval(t)
	rememberDeviceEventApproval("evt_expired", approval.ID)

	handler := &IMMessageHandler{}
	handler.applyDeviceEventAck("evt_expired", coreim.DeviceEventAckStatusExpired, "", coreim.DeviceEventDecidedByTimeout)

	if !pendingApprovalExists(approval.ID) {
		t.Fatal("a device timeout must not close the desktop approval")
	}
}

func TestDeviceDecisionAuditSourceNamesTheModality(t *testing.T) {
	cases := map[string]string{
		coreim.DeviceEventDecidedByVoice:   "device/voice",
		coreim.DeviceEventDecidedByButton:  "device/button",
		coreim.DeviceEventDecidedByTimeout: "device/timeout",
		"":                                 "device",
		"telepathy":                        "device",
	}
	for decidedBy, want := range cases {
		if got := deviceDecisionAuditSource(decidedBy); got != want {
			t.Fatalf("decidedBy=%q got %q want %q", decidedBy, got, want)
		}
	}
}

func TestDeviceEventApprovalLinkResolvesAndExpires(t *testing.T) {
	resetDeviceEventApprovalStore()
	rememberDeviceEventApproval("evt_lookup", "tool-approval-42")

	got, ok := lookupDeviceEventApproval("evt_lookup")
	if !ok || got != "tool-approval-42" {
		t.Fatalf("lookup=%q ok=%v", got, ok)
	}
	if _, ok := lookupDeviceEventApproval("evt_other"); ok {
		t.Fatal("an unrecorded event must not resolve")
	}
	// An empty event id cannot come off the wire, but it must not match either.
	if _, ok := lookupDeviceEventApproval("   "); ok {
		t.Fatal("a blank event id must not resolve")
	}

	// Age the link past its TTL rather than sleeping: the expiry is what keeps
	// a long-lived GUI from accumulating links for approvals that are gone.
	deviceEventApprovalStore.Lock()
	link := deviceEventApprovalStore.entries["evt_lookup"]
	link.expiresAt = time.Now().Add(-time.Second)
	deviceEventApprovalStore.entries["evt_lookup"] = link
	deviceEventApprovalStore.Unlock()

	if _, ok := lookupDeviceEventApproval("evt_lookup"); ok {
		t.Fatal("an expired link must not resolve")
	}
}

func TestDeviceEventApprovalLinksAreBounded(t *testing.T) {
	resetDeviceEventApprovalStore()
	// Fill to the bound with strictly increasing deadlines, so which link is
	// "oldest" is a fact rather than a map-iteration coin flip. The deadlines
	// start a minute out so the insert's own prune pass cannot remove one of
	// them -- this test is about eviction, not expiry.
	base := time.Now().Add(time.Minute)
	deviceEventApprovalStore.Lock()
	for i := 0; i < deviceEventApprovalLinkCapacity; i++ {
		deviceEventApprovalStore.entries[fmt.Sprintf("evt_%d", i)] = deviceEventApprovalLink{
			approvalID: fmt.Sprintf("tool-approval-%d", i),
			expiresAt:  base.Add(time.Duration(i) * time.Second),
		}
	}
	deviceEventApprovalStore.Unlock()

	rememberDeviceEventApproval("evt_newest", "tool-approval-newest")

	deviceEventApprovalStore.Lock()
	size := len(deviceEventApprovalStore.entries)
	_, oldestStillPresent := deviceEventApprovalStore.entries["evt_0"]
	deviceEventApprovalStore.Unlock()
	if size > deviceEventApprovalLinkCapacity {
		t.Fatalf("link store holds %d entries, over the %d bound", size, deviceEventApprovalLinkCapacity)
	}
	if oldestStillPresent {
		t.Fatal("the bound must evict the oldest link, not an arbitrary one")
	}
	// The newest link must survive, or the bound would break the very
	// correlation it is meant to protect.
	if got, ok := lookupDeviceEventApproval("evt_newest"); !ok || got != "tool-approval-newest" {
		t.Fatalf("the newest link was evicted: %q ok=%v", got, ok)
	}
}

func TestRememberDeviceEventApprovalIgnoresUnusableInput(t *testing.T) {
	resetDeviceEventApprovalStore()
	rememberDeviceEventApproval("", "tool-approval-1")
	rememberDeviceEventApproval("evt_x", "")
	rememberDeviceEventApproval("  ", "  ")
	deviceEventApprovalStore.Lock()
	size := len(deviceEventApprovalStore.entries)
	deviceEventApprovalStore.Unlock()
	if size != 0 {
		t.Fatalf("unusable input created %d links", size)
	}
}
