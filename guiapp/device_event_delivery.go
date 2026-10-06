package guiapp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// deviceScheduleEventTTLSec bounds how long a scheduler reminder survives a
// device reboot.
//
// Fifteen minutes covers "the terminal happened to be restarting", which is the
// case worth recovering, and stays far away from resurfacing a reminder the user
// already saw hours ago. It must stay within coreim's [MinTTLSec, MaxTTLSec].
const deviceScheduleEventTTLSec = 900

// deviceEventSequence disambiguates two events minted within the same clock
// tick. Wall-clock resolution alone is not enough: two tasks due at the same
// moment can be delivered back to back, and because the Hub backfills dedupeKey
// from eventId, a shared id would make the device treat the second reminder as a
// duplicate of the first and silently drop it.
var deviceEventSequence atomic.Uint64

// deliverDeviceScheduledTarget pushes one scheduled-task result to the companion
// hardware terminals paired with this GUI (plan N1-4).
//
// This is the scheduler's device channel, not an IM channel. The Hub fans the
// payload out to every device bound to this machine, so the only addressable
// target is the owner; anything else is rejected rather than resolved, matching
// the weixin channel's owner-only rule. There is no per-device id a caller could
// legitimately name, and inventing one would imply a routing precision that does
// not exist.
//
// The push is fire-and-forget by construction: the Hub validates the event and
// drops a malformed one without reporting back, and it also no-ops when no
// paired device declared event support. A delivery error here therefore means
// "the Hub link is down", never "a specific device refused it".
func (a *App) deliverDeviceScheduledTarget(_ context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return "", fmt.Errorf("device delivery only supports kind=user (the owner's paired terminals)")
	}
	if !scheduler.IsSelfPeerID(target.UserID) {
		return "", fmt.Errorf("device: only the owner's paired terminals are addressable (use user_id=self); got %q", target.UserID)
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return "", fmt.Errorf("device: Hub not connected")
	}
	if err := hub.SendDeviceGatewayEvent(buildScheduledTaskDeviceEvent(text, time.Now())); err != nil {
		return "", err
	}
	// The fan-out is machine-scoped, so there is no per-device durable peer id
	// to remember for later user_id=self resolution.
	return "", nil
}

// buildScheduledTaskDeviceEvent shapes one scheduler result as a structured
// `schedule` event (plan section 4.1).
//
// severity=soft is the point of the admission matrix: a reminder may speak when
// the device is idle, queue behind an active utterance, and stay silent during
// do-not-disturb. Escalating a *failed* task to interrupt was deliberately not
// done -- interrupt is reserved for "waiting for the user would be worse than
// interrupting them", which a late notification never is.
//
// persist=true with a bounded ttl means a terminal that is rebooting still
// learns about the reminder shortly after it returns, while one that was off for
// hours does not resurface a stale card. No dedupeKey is set on purpose: every
// scheduler fire is a distinct occurrence, so the Hub's eventId backfill is
// exactly the right idempotency key and two genuinely separate reminders both
// get through.
//
// No actions are attached. A reminder has no decision to make, and inventing a
// button here would push a non-decision into the same audit path that D5-A
// reserves for high-risk approvals.
func buildScheduledTaskDeviceEvent(body string, now time.Time) map[string]any {
	name, rest := scheduler.SplitScheduledTaskBody(body)
	title := name
	if title == "" {
		// An immediate im_message push carries no task name, and the managed
		// dispatch path sends the raw result. A neutral title keeps the card
		// honest instead of naming a task that does not exist.
		title = "提醒"
	}
	summary := rest
	if summary == "" {
		summary = strings.TrimSpace(body)
	}
	return map[string]any{
		"eventId":  fmt.Sprintf("sched_%d_%d", now.UnixNano(), deviceEventSequence.Add(1)),
		"category": coreim.DeviceEventCategorySchedule,
		"severity": coreim.DeviceEventSeveritySoft,
		"title":    title,
		"summary":  summary,
		"ttlSec":   int64(deviceScheduleEventTTLSec),
		// A reminder is not a decision, so nothing waits on a receipt.
		"requiresAck": false,
		"persist":     true,
	}
}
