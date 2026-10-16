package guiapp

import (
	"fmt"
	"log"
	"strings"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// deviceVEEventTTLSec bounds how long a workflow attention card survives a
// terminal reboot. Same order of magnitude as the approval card: long enough to
// cover "the terminal was restarting", short enough that a stale blocked
// workflow does not resurface hours later.
const deviceVEEventTTLSec = 900

// deviceVEEventMaxSummaryRunes keeps the reason/details inside what the small
// screen can show. The wire contract allows 400; the device is the constraint.
const deviceVEEventMaxSummaryRunes = 240

// workflowStatusSeverity maps the Hub's urgency classification onto the event
// severity closed set.
//
// The Hub only ever sends blocked/escalation notifications on this channel
// (`classifyWorkflowStatusEvent`), so every one of them is a *problem* report.
// The mapping therefore encodes how much the user's attention is worth, not
// whether anything is wrong:
//
//   - critical: the workflow cannot proceed at all -- nobody is left to approve,
//     or a dependency is unavailable. Waiting for the user to look at their
//     laptop is worse than interrupting them.
//   - overdue: a timeout or a failed escalation. Worth announcing when the
//     device is idle, but it can queue behind whatever is already being said.
//   - attention: the default bucket, i.e. what a plain "blocked" falls into. It
//     is the least specific classification, so it gets no sound at all -- the
//     card still appears, but the device does not become chatty about routine
//     blocks. This is the cheap half of the W4 "events become noise" mitigation;
//     the frequency budget (N4-5) is the other half and does not exist yet.
func workflowStatusSeverity(urgency string) string {
	switch strings.ToLower(strings.TrimSpace(urgency)) {
	case "critical":
		return coreim.DeviceEventSeverityInterrupt
	case "overdue":
		return coreim.DeviceEventSeveritySoft
	default:
		return coreim.DeviceEventSeveritySilent
	}
}

// buildWorkflowStatusDeviceEvent shapes one Hub workflow status push as a
// structured `ve` event (plan section 4.1), or reports that it is not
// addressable.
//
// No actions are attached. The decision this event implies ("go unblock the
// workflow") is taken in the desktop ops panel; there is no button a device
// could offer that the user could actually press. Inventing one would push a
// non-decision into the audit path D5-A reserves for high-risk approvals.
//
// persist is on with a bounded ttl, and dedupeKey is scoped to
// (instance, event) rather than to the push. A blocked workflow re-notifies
// whenever the Hub re-evaluates it, and the user needs to learn about it once,
// not once per re-evaluation -- that is precisely what a stable key buys.
func buildWorkflowStatusDeviceEvent(payload map[string]any, now time.Time) (map[string]any, bool) {
	if len(payload) == 0 {
		return nil, false
	}
	instanceID := strings.TrimSpace(fmt.Sprint(payload["instance_id"]))
	if instanceID == "" || instanceID == "<nil>" {
		// Without the instance id there is no stable identity for the card, so
		// re-notifications would each look like a brand new problem.
		return nil, false
	}
	event := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["event"])))
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["status"])))
	if event == "" || event == "<nil>" {
		event = status
	}
	if event == "" || event == "<nil>" {
		event = "blocked"
	}

	workflowName := strings.TrimSpace(fmt.Sprint(payload["workflow_name"]))
	if workflowName == "" || workflowName == "<nil>" {
		workflowName = "数字员工流程"
	}
	reason := strings.TrimSpace(fmt.Sprint(payload["reason"]))
	if reason == "<nil>" {
		reason = ""
	}
	details := strings.TrimSpace(fmt.Sprint(payload["details"]))
	if details == "<nil>" {
		details = ""
	}

	summary := reason
	if summary == "" {
		summary = details
	}
	if summary == "" {
		// A blocked workflow with no reason is still worth a card; the node is
		// the most specific thing left.
		summary = "流程需要处理"
		if node := strings.TrimSpace(fmt.Sprint(payload["current_node"])); node != "" && node != "<nil>" {
			summary = summary + "（" + node + "）"
		}
	}

	return map[string]any{
		"eventId":   fmt.Sprintf("ve_%d_%d", now.UnixNano(), deviceEventSequence.Add(1)),
		"title":     truncateDeviceApprovalText(workflowName, coreim.DeviceEventMaxTitleRunes),
		"summary":   truncateDeviceApprovalText(summary, deviceVEEventMaxSummaryRunes),
		"category":  coreim.DeviceEventCategoryVE,
		"severity":  workflowStatusSeverity(fmt.Sprint(payload["urgency"])),
		"ttlSec":    int64(deviceVEEventTTLSec),
		"dedupeKey": "ve-workflow:" + instanceID + ":" + event,
		// Nothing waits on a receipt: the desktop projection is the
		// authoritative view and it is updated independently of the device.
		"requiresAck": false,
		"persist":     true,
	}, true
}

// pushWorkflowStatusToDevices delivers one workflow attention card to the paired
// terminals (plan N1-5).
//
// Fire-and-forget, for the same reason the approval push is: the desktop
// projection is the authoritative surface and must keep working when no
// companion terminal is reachable.
func (c *RemoteHubClient) pushWorkflowStatusToDevices(payload map[string]any) {
	if c == nil || c.app == nil {
		return
	}
	event, ok := buildWorkflowStatusDeviceEvent(payload, time.Now())
	if !ok {
		return
	}
	hub := c.app.hubClient()
	if hub == nil || !hub.IsConnected() {
		return
	}
	if err := hub.SendDeviceGatewayEvent(event); err != nil {
		log.Printf("[device-ve] push failed for instance %v: %v", payload["instance_id"], err)
	}
}
