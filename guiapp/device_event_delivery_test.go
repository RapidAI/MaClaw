package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestBuildScheduledTaskDeviceEventSplitsTheTaskNameOutOfTheSummary(t *testing.T) {
	d := &scheduler.TaskDelivery{}
	now := time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)
	event := buildScheduledTaskDeviceEvent(d.FormatBody("每日站会", "10 点开始", nil), now)

	if event["category"] != coreim.DeviceEventCategorySchedule {
		t.Fatalf("category=%#v", event["category"])
	}
	// soft is what lets the device queue the reminder behind an active utterance
	// and stay silent during do-not-disturb. interrupt is reserved for "waiting
	// would be worse than interrupting".
	if event["severity"] != coreim.DeviceEventSeveritySoft {
		t.Fatalf("severity=%#v", event["severity"])
	}
	if event["title"] != "每日站会" {
		t.Fatalf("title=%#v", event["title"])
	}
	summary, _ := event["summary"].(string)
	if strings.Contains(summary, "【定时任务】") {
		t.Fatalf("the summary must not repeat the title: %q", summary)
	}
	if !strings.Contains(summary, "10 点开始") {
		t.Fatalf("summary=%q", summary)
	}
	if event["persist"] != true {
		t.Fatalf("a reminder must survive a terminal reboot: %#v", event["persist"])
	}
	if event["requiresAck"] != false {
		t.Fatalf("nothing waits on a reminder receipt: %#v", event["requiresAck"])
	}
	ttl, ok := event["ttlSec"].(int64)
	if !ok {
		t.Fatalf("ttlSec=%#v", event["ttlSec"])
	}
	if ttl < coreim.DeviceEventMinTTLSec || ttl > coreim.DeviceEventMaxTTLSec {
		t.Fatalf("ttlSec=%d is outside the protocol window", ttl)
	}
	// A reminder has no decision to make. Attaching a button would push a
	// non-decision into the audit path D5-A reserves for high-risk approvals.
	if _, hasActions := event["actions"]; hasActions {
		t.Fatalf("a reminder must carry no actions: %#v", event["actions"])
	}
	// The absolute expiry is Hub-side snapshot bookkeeping. Producing it here
	// would be an undeclared wire field.
	if _, leaked := event["expiresAtUnixMs"]; leaked {
		t.Fatal("the desktop must not mint the Hub's absolute expiry")
	}
}

func TestBuildScheduledTaskDeviceEventUsesANeutralTitleWithoutATaskLine(t *testing.T) {
	// An im_message push has no task name, and the managed dispatch path sends
	// the raw result. The card must not name a task that does not exist.
	event := buildScheduledTaskDeviceEvent("楼下快递到了", time.Now())
	if event["title"] != "提醒" {
		t.Fatalf("title=%#v", event["title"])
	}
	if event["summary"] != "楼下快递到了" {
		t.Fatalf("summary=%#v", event["summary"])
	}
}

func TestBuildScheduledTaskDeviceEventMintsADistinctIDPerEvent(t *testing.T) {
	base := time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)
	first := buildScheduledTaskDeviceEvent("同一个任务", base)
	second := buildScheduledTaskDeviceEvent("同一个任务", base)
	if first["eventId"] == second["eventId"] {
		t.Fatalf("two fires in the same clock tick collided on %#v", first["eventId"])
	}
	// The Hub backfills dedupeKey from eventId, so an id that exceeds the
	// protocol bound would get the whole event rejected.
	id, _ := first["eventId"].(string)
	if id == "" || len(id) > coreim.DeviceEventMaxIDLen {
		t.Fatalf("eventId=%q (%d bytes)", id, len(id))
	}
}

func TestDeliverDeviceScheduledTargetRejectsNonOwnerTargets(t *testing.T) {
	// The Hub fans a device event out to every terminal bound to this machine,
	// so there is no per-device id a caller could legitimately name. Anything
	// else must be refused rather than silently resolved to the owner.
	app := &App{}
	for _, target := range []scheduler.DeliveryTarget{
		{Kind: scheduler.DeliveryKindGroup, GroupID: "ops"},
		{Kind: scheduler.DeliveryKindUser, UserID: "someone-else"},
	} {
		if _, err := app.deliverDeviceScheduledTarget(context.Background(), target, "hi"); err == nil {
			t.Fatalf("target=%#v must be rejected", target)
		}
	}
}

func TestDeliverDeviceScheduledTargetReportsTheTransportFailure(t *testing.T) {
	// The owner target is valid, so the failure has to be the transport. A device
	// push must never fall back to another channel: silently posting the reminder
	// somewhere else would be worse than reporting it undelivered.
	app := &App{}
	_, err := app.deliverDeviceScheduledTarget(context.Background(), scheduler.DeliveryTarget{
		Kind: scheduler.DeliveryKindUser, UserID: "self",
	}, "hi")
	if err == nil || !strings.Contains(err.Error(), "Hub not connected") {
		t.Fatalf("err=%v", err)
	}
}

func TestListDeviceDeliveryTargetsExposesOnlyTheOwner(t *testing.T) {
	app := &App{}
	refs, err := app.listDeviceDeliveryTargets(context.Background(), "")
	if err != nil {
		t.Fatalf("listDeviceDeliveryTargets: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs=%#v", refs)
	}
	if refs[0].Kind != scheduler.DeliveryKindUser || refs[0].ID != "self" {
		t.Fatalf("ref=%#v", refs[0])
	}
	if refs[0].Channel != scheduler.DeliveryChannelDevice {
		t.Fatalf("channel=%q", refs[0].Channel)
	}
}
