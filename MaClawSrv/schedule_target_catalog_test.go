package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestListSrvPeerDeliveryTargetsSelf(t *testing.T) {
	refs := listSrvPeerDeliveryTargets(scheduler.DeliveryChannelTelegram, "Telegram", "")
	if len(refs) < 1 || refs[0].ID != "self" {
		t.Fatalf("refs=%#v", refs)
	}
	if !strings.Contains(refs[0].Name, "Telegram") {
		t.Fatalf("name=%q", refs[0].Name)
	}
}

func TestListSrvPeerDeliveryTargetsQueryFilter(t *testing.T) {
	refs := listSrvPeerDeliveryTargets(scheduler.DeliveryChannelQQ, "QQ", "zzzz-no-match")
	if len(refs) != 0 {
		t.Fatalf("expected empty filter, got %#v", refs)
	}
}

func TestBuildAdminSchedulerDeliveryMetrics(t *testing.T) {
	// Lightweight unit via buildAdminSchedulerStatus with temp dir is covered in admin_runtime_test;
	// here we only guard Active/HasDeliveryWarning helpers used by metrics.
	d := &scheduler.TaskDelivery{
		Enabled: true,
		Channel: scheduler.DeliveryChannelLansenger,
		Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindGroup, GroupID: "g1"}},
	}
	if !d.Active() {
		t.Fatal("expected active delivery")
	}
	if !scheduler.HasDeliveryWarning("[投递警告] boom") {
		t.Fatal("expected warning detect")
	}
}
