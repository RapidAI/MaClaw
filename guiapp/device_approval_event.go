package guiapp

import (
	"fmt"
	"log"
	"strings"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// deviceApprovalEventTTLSec bounds how long a high-risk approval card survives
// on the terminal.
//
// Five minutes is the plan's own example and it is the right order of magnitude:
// a confirmation that has been waiting longer than that is stale enough that
// acting on it is worse than re-asking. It must stay within coreim's
// [MinTTLSec, MaxTTLSec]; the Hub also force-requires ttlSec>0 for
// category=approval, so this is a contract obligation, not a preference.
const deviceApprovalEventTTLSec = 300

// deviceApprovalMaxSummaryRunes keeps the spoken/shown context inside what a
// small round screen and a one-breath utterance can carry. The wire contract
// allows 400; the device is the binding constraint here.
const deviceApprovalMaxSummaryRunes = 240

// shouldPushApprovalToDevice implements D5-A: only high-risk approvals become
// device cards.
//
// The threshold is a positive test on the *declared* risk level, never a
// fallback: an approval whose risk was never assessed must not be treated as
// high, and must not be treated as low either -- it simply does not become a
// device card, and the desktop remains the only place it is answered. This is
// why the plan keeps `risk` absent != `risk=low`: deriving the decision from
// anything but an explicit level would make a missing field silently mean
// "safe enough to confirm from across the room".
func shouldPushApprovalToDevice(level security.RiskLevel) bool {
	switch level {
	case security.RiskHigh, security.RiskCritical:
		return true
	default:
		return false
	}
}

// deviceApprovalRiskLabel maps the local risk level onto the wire's closed set
// so the device can style the action without inferring severity from a colour.
func deviceApprovalRiskLabel(level security.RiskLevel) string {
	switch level {
	case security.RiskCritical, security.RiskHigh:
		return coreim.DeviceEventActionRiskHigh
	case security.RiskMedium:
		return coreim.DeviceEventActionRiskMedium
	default:
		return coreim.DeviceEventActionRiskLow
	}
}

// deviceApprovalToolLabel names the action in the user's terms.
//
// The point of the whole D5-A flow is that a confirmation carries *what* is
// about to happen. "run bash" is not an answer to that question; "run a shell
// command" at least tells the user which class of thing they are approving.
func deviceApprovalToolLabel(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "bash":
		return "执行命令"
	case "database", "database_query":
		return "数据库操作"
	case "archive":
		return "解压外部程序"
	case "write", "file_write", "edit", "file_edit":
		return "写入文件"
	case "delete", "file_delete", "remove":
		return "删除文件"
	case "call_mcp_tool":
		return "调用外部工具"
	default:
		name := strings.TrimSpace(toolName)
		if name == "" {
			return "执行操作"
		}
		return name
	}
}

// toolApprovalDeviceTarget extracts the concrete object of the pending action.
//
// "Blind confirmation is no confirmation" (plan D5-C15), so the goal is the
// most specific thing available, in this order:
//
//  1. the tool's own primary operand (a shell command, a SQL statement, a path);
//  2. the assessed risk reason, which the analyzer writes in concrete terms;
//  3. the bare tool label, which is a last resort and is deliberately honest
//     about being one.
//
// Arguments are passed through redactAgentViewSubmitSecrets first so the device
// card obeys exactly the same masking policy as the desktop review panel
// instead of inventing a second one.
func toolApprovalDeviceTarget(toolName string, args map[string]any) string {
	safe := redactAgentViewSubmitSecrets(args)
	tool := strings.TrimSpace(toolName)
	label := deviceApprovalToolLabel(tool)

	switch tool {
	case "bash":
		if cmd := strings.TrimSpace(nonEmptyStringFromAny(safe["command"])); cmd != "" {
			return label + "：" + cmd
		}
	case "database", "database_query":
		action := strings.TrimSpace(nonEmptyStringFromAny(safe["action"]))
		sql := strings.TrimSpace(nonEmptyStringFromAny(safe["sql"]))
		switch {
		case action != "" && sql != "":
			return fmt.Sprintf("%s：%s %s", label, action, sql)
		case sql != "":
			return label + "：" + sql
		case action != "":
			return label + "：" + action
		}
	}

	// Tools whose operand is a location rather than a verb.
	for _, key := range []string{
		"path", "file_path", "filepath", "target", "destination", "url", "query",
	} {
		if value := strings.TrimSpace(nonEmptyStringFromAny(safe[key])); value != "" {
			return label + "：" + value
		}
	}

	return label
}

// buildToolApprovalDeviceEvent shapes a pending tool approval as a structured
// `approval` event (plan section 4.1), or reports that it must not go to the
// device at all.
//
// severity=interrupt is deliberate and is the whole reason the admission matrix
// exists: a high-risk approval is the one case where waiting for the user to
// look at their laptop is worse than interrupting them. Everything that makes
// interrupt safe is handled downstream rather than here -- the device downgrades
// it to display+vibrate during quiet hours and to display-only during a
// recording (plan section 4.2), so the producer does not need to know the time
// of day.
//
// The audit contract is satisfied in full: ttlSec>0, requiresAck=true and
// persist=true. The Hub rejects the event outright if any is missing, because
// each omission fails silently -- no receipt means nobody learns the card never
// arrived, and no persist means a pending high-risk approval disappears without
// a trace when the terminal reboots.
//
// dedupeKey is the approval's own identity, not a hash of its arguments. A
// retried tool call mints a *new* pending approval, and that is the correct
// behaviour: the user rejecting one instance does not authorise suppressing a
// later, separately-created request that merely looks identical. What the key
// must prevent is the same card being shown twice for the same pending
// approval, which is exactly what the id gives.
func buildToolApprovalDeviceEvent(approval registeredToolPendingApproval, now time.Time) (map[string]any, bool) {
	if !shouldPushApprovalToDevice(approval.Risk.Level) {
		return nil, false
	}
	approvalID := strings.TrimSpace(approval.ID)
	if approvalID == "" {
		// Without an id there is no way to correlate the decision back to the
		// pending approval, so the card would be a prompt the user can answer
		// but nobody can act on.
		return nil, false
	}

	summary := toolApprovalDeviceTarget(approval.ToolName, approval.Args)
	if reason := strings.TrimSpace(approval.Risk.Reason); reason != "" &&
		summary == deviceApprovalToolLabel(approval.ToolName) {
		// No concrete operand was found; the analyzer's reason is the most
		// specific thing left, and it is still better than a blind prompt.
		summary = summary + "：" + reason
	}
	summary = truncateDeviceApprovalText(summary, deviceApprovalMaxSummaryRunes)

	return map[string]any{
		"eventId": fmt.Sprintf("approval_%d_%d", now.UnixNano(), deviceEventSequence.Add(1)),
		// title stays neutral: the specificity lives in summary, and the two
		// must not drift into saying the same thing twice.
		"title":       "需要你确认",
		"summary":     summary,
		"category":    coreim.DeviceEventCategoryApproval,
		"severity":    coreim.DeviceEventSeverityInterrupt,
		"ttlSec":      int64(deviceApprovalEventTTLSec),
		"dedupeKey":   "approval:" + approvalID,
		"requiresAck": true,
		"persist":     true,
		"actions": []map[string]any{
			{
				"id":    deviceApprovalApproveActionID,
				"label": "同意",
				"kind":  coreim.DeviceEventActionKindPrimary,
				"risk":  deviceApprovalRiskLabel(approval.Risk.Level),
			},
			{
				"id":    "reject",
				"label": "拒绝",
				"kind":  coreim.DeviceEventActionKindSecondary,
			},
		},
	}, true
}

// truncateDeviceApprovalText cuts on a rune boundary so a CJK summary cannot be
// split into invalid UTF-8 on its way to the device font renderer.
func truncateDeviceApprovalText(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}

// pushToolApprovalToDevices delivers one high-risk approval card to the paired
// terminals (plan N1-5).
//
// Fire-and-forget on purpose. The desktop AgentView is the authoritative
// approval surface; the device is an additional, optional one. If the Hub link
// is down, or no paired terminal declared event support, the approval must still
// be answerable at the desk -- so a delivery failure is logged and never
// propagated into the approval flow. Failing the tool call because a companion
// device could not be reached would make the device a new single point of
// failure for the user's work.
func (h *IMMessageHandler) pushToolApprovalToDevices(approval registeredToolPendingApproval) {
	if h == nil || h.app == nil {
		return
	}
	event, ok := buildToolApprovalDeviceEvent(approval, time.Now())
	if !ok {
		return
	}
	hub := h.app.hubClient()
	if hub == nil || !hub.IsConnected() {
		// Not an error: a GUI running standalone has no Hub, and D5-A is
		// additive to the desktop flow.
		return
	}
	if err := hub.SendDeviceGatewayEvent(event); err != nil {
		log.Printf("[device-approval] push failed for %s: %v", approval.ID, err)
		return
	}
	// The terminal only ever sees the generated eventId, so this link is what
	// lets its answer find the approval again. Recorded after a successful
	// hand-off: a card that never reached the Hub can never be answered, and a
	// dangling link would only ever be pruned.
	rememberDeviceEventApproval(nonEmptyStringFromAny(event["eventId"]), approval.ID)
}
