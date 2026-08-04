package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
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

func TestListSrvPeerDeliveryTargetsUsesProvidedPrincipalStore(t *testing.T) {
	root := t.TempDir()
	storeA := scheduler.NewDeliveryStateStore(filepath.Join(root, "a"))
	storeB := scheduler.NewDeliveryStateStore(filepath.Join(root, "b"))
	storeA.RememberPeer(scheduler.DeliveryChannelTelegram, "1001")
	storeB.RememberPeer(scheduler.DeliveryChannelTelegram, "2002")
	refsA := listSrvPeerDeliveryTargetsWithStore(storeA, scheduler.DeliveryChannelTelegram, "Telegram", "")
	refsB := listSrvPeerDeliveryTargetsWithStore(storeB, scheduler.DeliveryChannelTelegram, "Telegram", "")
	joined := func(refs []scheduler.TargetRef) string {
		var ids []string
		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}
		return strings.Join(ids, ",")
	}
	if got := joined(refsA); !strings.Contains(got, "1001") || strings.Contains(got, "2002") {
		t.Fatalf("principal A refs=%q", got)
	}
	if got := joined(refsB); !strings.Contains(got, "2002") || strings.Contains(got, "1001") {
		t.Fatalf("principal B refs=%q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a", scheduler.DeliveryStateFileName)); err != nil {
		t.Fatalf("principal A state not persisted: %v", err)
	}
}

func TestPrincipalTargetCatalogPassesRequestIdentityToLoader(t *testing.T) {
	want := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	called := false
	reg := newSrvScheduleTargetCatalogsForPrincipalWithLansengerLoader(nil, want, func(_ context.Context, _ *agentservice.Service, got agentservice.Principal, query string) ([]scheduler.TargetRef, error) {
		called = true
		if got.TenantID != want.TenantID || got.UserID != want.UserID || query != "研发" {
			t.Fatalf("principal=%#v query=%q", got, query)
		}
		return []scheduler.TargetRef{{Kind: scheduler.DeliveryKindGroup, ID: "g-9", Name: "研发群", Channel: scheduler.DeliveryChannelLansenger}}, nil
	})
	refs, err := reg.ListTargets(context.Background(), scheduler.DeliveryChannelLansenger, "研发")
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(refs) != 1 || refs[0].ID != "g-9" {
		t.Fatalf("called=%v refs=%#v", called, refs)
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
