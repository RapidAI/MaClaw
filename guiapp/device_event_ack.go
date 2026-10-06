package guiapp

import (
	"log"
	"strings"
	"sync"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// deviceApprovalApproveActionID is the action id that means "approve".
//
// It is a constant rather than a literal in two places because the two places
// have to agree: buildToolApprovalDeviceEvent offers the button under this id,
// and the ack path only treats a decision as an approval when it names this
// exact id. Anything else -- including a status/action mismatch a confused or
// hostile device could produce -- falls through to "not approved", which is the
// fail-closed direction D5-A requires.
const deviceApprovalApproveActionID = "approve"

// registeredToolApprovalDeviceSourceField carries the decision's provenance
// into handleRegisteredToolApprovalAgentViewSubmit, so the audit entry written
// by the shared approval path can say *how* the user answered.
const registeredToolApprovalDeviceSourceField = "_device_decision_source"

// deviceEventApprovalLinkCapacity bounds the eventId -> approval id map.
//
// The window is short (the approval event's own ttlSec) and the volume is a
// handful of high-risk confirmations, so the bound only exists to make a
// pathological producer -- or a stuck retry loop -- unable to grow it forever.
const deviceEventApprovalLinkCapacity = 64

// deviceEventApprovalLinkTTL mirrors the desktop pending-approval prune window.
// Keeping the link alive at least as long as the approval it points at means a
// late ack still resolves instead of being logged as unknown.
const deviceEventApprovalLinkTTL = 30 * time.Minute

type deviceEventApprovalLink struct {
	approvalID string
	expiresAt  time.Time
}

// deviceEventApprovalStore is the only way a device decision finds its approval.
//
// The terminal never learns the approval id: it is shown a generated `eventId`
// (buildToolApprovalDeviceEvent) and echoes that back. The correlation therefore
// has to live here, on the producer's side, and it is deliberately in memory
// only -- a Hub-mode GUI that restarts has no pending approval to resolve
// either, so a persisted link would only ever point at nothing.
var deviceEventApprovalStore = struct {
	sync.Mutex
	entries map[string]deviceEventApprovalLink
}{entries: make(map[string]deviceEventApprovalLink)}

// rememberDeviceEventApproval records which pending approval an event card
// answers. Called right after the card is handed to the Hub, so the window in
// which a decision could arrive unmatched is the same window in which the card
// does not exist yet.
func rememberDeviceEventApproval(eventID, approvalID string) {
	eventID = strings.TrimSpace(eventID)
	approvalID = strings.TrimSpace(approvalID)
	if eventID == "" || approvalID == "" {
		return
	}
	now := time.Now()
	deviceEventApprovalStore.Lock()
	defer deviceEventApprovalStore.Unlock()
	if deviceEventApprovalStore.entries == nil {
		deviceEventApprovalStore.entries = make(map[string]deviceEventApprovalLink)
	}
	pruneDeviceEventApprovalLinksLocked(now)
	deviceEventApprovalStore.entries[eventID] = deviceEventApprovalLink{
		approvalID: approvalID,
		expiresAt:  now.Add(deviceEventApprovalLinkTTL),
	}
	for len(deviceEventApprovalStore.entries) > deviceEventApprovalLinkCapacity {
		dropOldestDeviceEventApprovalLinkLocked()
	}
}

// lookupDeviceEventApproval resolves an eventId to its approval. The link is
// kept after a decision rather than consumed: `expired` must leave the desktop
// prompt answerable, and a second ack on the same event should be recognised as
// "already handled" rather than as an unknown event.
func lookupDeviceEventApproval(eventID string) (string, bool) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", false
	}
	now := time.Now()
	deviceEventApprovalStore.Lock()
	defer deviceEventApprovalStore.Unlock()
	link, ok := deviceEventApprovalStore.entries[eventID]
	if !ok {
		return "", false
	}
	if !link.expiresAt.IsZero() && now.After(link.expiresAt) {
		delete(deviceEventApprovalStore.entries, eventID)
		return "", false
	}
	return link.approvalID, true
}

func resetDeviceEventApprovalStore() {
	deviceEventApprovalStore.Lock()
	defer deviceEventApprovalStore.Unlock()
	deviceEventApprovalStore.entries = make(map[string]deviceEventApprovalLink)
}

// pruneDeviceEventApprovalLinksLocked drops expired links. Must be called with
// the store lock held.
func pruneDeviceEventApprovalLinksLocked(now time.Time) {
	for eventID, link := range deviceEventApprovalStore.entries {
		if !link.expiresAt.IsZero() && now.After(link.expiresAt) {
			delete(deviceEventApprovalStore.entries, eventID)
		}
	}
}

// dropOldestDeviceEventApprovalLinkLocked evicts one entry, oldest deadline
// first, so the bound is respected even when every link is still live. Must be
// called with the store lock held.
func dropOldestDeviceEventApprovalLinkLocked() {
	oldestID := ""
	var oldest time.Time
	for eventID, link := range deviceEventApprovalStore.entries {
		if oldestID == "" || link.expiresAt.Before(oldest) {
			oldestID, oldest = eventID, link.expiresAt
		}
	}
	if oldestID != "" {
		delete(deviceEventApprovalStore.entries, oldestID)
	}
}

// deviceDecisionAuditSource spells the provenance of a device-made decision for
// security.AuditEntry.Source, e.g. "device/voice".
//
// The plan's third N1-6 acceptance requirement is that a high-risk approval be
// traceable to *how* it was given, and the audit trail already answers "who"
// (UserID) and "when" (Timestamp). Only the modality was missing, and folding
// it into the free-text Result would have made the machine-readable result
// field start carrying prose.
func deviceDecisionAuditSource(decidedBy string) string {
	switch strings.ToLower(strings.TrimSpace(decidedBy)) {
	case coreim.DeviceEventDecidedByVoice:
		return "device/voice"
	case coreim.DeviceEventDecidedByButton:
		return "device/button"
	case coreim.DeviceEventDecidedByTimeout:
		return "device/timeout"
	default:
		return "device"
	}
}

// applyDeviceEventAck turns one ack from a paired terminal into a decision on
// the pending approval it answers (plan N1-6).
//
// The rules are deliberately narrow:
//
//   - `received` is a transport receipt, not a decision, and touches nothing;
//   - only the button the card actually offered under `approve` can approve --
//     every other combination is "not approved", never "approved by default";
//   - `expired` records that the terminal gave up but leaves the desktop prompt
//     open, because the user is still sitting in front of it.
//
// The actual approval is delegated to handleRegisteredToolApprovalAgentViewSubmit
// so the device path reuses the desktop path's single landing point: the same
// audit entries, the same session grant, the same execution token semantics.
func (h *IMMessageHandler) applyDeviceEventAck(eventID, status, actionID, decidedBy string) {
	eventID = strings.TrimSpace(eventID)
	status = strings.ToLower(strings.TrimSpace(status))
	if eventID == "" {
		return
	}
	if status == coreim.DeviceEventAckStatusReceived {
		// Nothing to do: the card arrived. This is the only signal that the
		// device actually showed the prompt, so it is logged for link health
		// but must not consume the approval.
		log.Printf("[device-approval] event %s acknowledged by the terminal", eventID)
		return
	}
	approvalID, ok := lookupDeviceEventApproval(eventID)
	if !ok {
		log.Printf("[device-approval] ack for unknown event %s status=%s", eventID, status)
		return
	}
	switch status {
	case coreim.DeviceEventAckStatusApproved, coreim.DeviceEventAckStatusRejected:
		approve := status == coreim.DeviceEventAckStatusApproved &&
			strings.TrimSpace(actionID) == deviceApprovalApproveActionID
		if status == coreim.DeviceEventAckStatusApproved && !approve {
			log.Printf("[device-approval] event %s claimed approval with action %q; treating as not approved",
				eventID, strings.TrimSpace(actionID))
		}
		h.resolveDeviceApproval(approvalID, approve, decidedBy, eventID)
	case coreim.DeviceEventAckStatusExpired:
		h.recordDeviceApprovalTimeout(approvalID, eventID)
	default:
		log.Printf("[device-approval] event %s carried unknown status %q", eventID, status)
	}
}

func (h *IMMessageHandler) resolveDeviceApproval(approvalID string, approve bool, decidedBy, eventID string) {
	if h == nil {
		return
	}
	data := map[string]interface{}{
		registeredToolApprovalIDField:           approvalID,
		"approved":                              approve,
		registeredToolApprovalDeviceSourceField: deviceDecisionAuditSource(decidedBy),
	}
	response := h.handleRegisteredToolApprovalAgentViewSubmit(data)
	if response != nil && response.Error != "" {
		// Not fatal: the approval may already have been answered at the desk,
		// which is exactly the race the shared path resolves by refusing the
		// second answer.
		log.Printf("[device-approval] event %s decision not applied: %s", eventID, response.Error)
	}
}

// recordDeviceApprovalTimeout writes the "the terminal never got an answer"
// audit line. It deliberately does not reject the pending approval: the
// timeout is the device's, and the desktop prompt is still answerable. What it
// buys is the plan's "超时走不做而非放行" guarantee being visible in the trail
// rather than inferred from an absence.
func (h *IMMessageHandler) recordDeviceApprovalTimeout(approvalID, eventID string) {
	if h == nil || h.firewall == nil {
		return
	}
	approval, ok := getRegisteredToolPendingApproval(approvalID)
	if !ok {
		return
	}
	h.firewall.recordAuditFromSource(approval.ToolName, approval.Args, approval.Risk, security.PolicyAsk,
		"device_approval_timed_out", approval.SessionID, approval.trustedAuditPrincipal(),
		deviceDecisionAuditSource(coreim.DeviceEventDecidedByTimeout))
	log.Printf("[device-approval] event %s expired on the terminal; approval %s stays pending at the desk",
		eventID, approvalID)
}
