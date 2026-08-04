package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestResolveIMFileDeliveryTargetResolvesUniqueGroupName(t *testing.T) {
	app := &App{}
	app.scheduleTargetCatalogs = scheduler.NewTargetCatalogRegistry()
	app.scheduleTargetCatalogs.Register(scheduler.TargetCatalogFunc{
		ChannelName: scheduler.DeliveryChannelLansenger,
		List: func(context.Context, string) ([]scheduler.TargetRef, error) {
			return []scheduler.TargetRef{{Kind: scheduler.DeliveryKindGroup, ID: "g-1", Name: "研发群", Channel: scheduler.DeliveryChannelLansenger}}, nil
		},
	})
	// Prevent ensureScheduleTargetCatalogs from replacing the injected registry.
	app.scheduleTargetCatalogsOnce.Do(func() {})
	target := agent.IMFileDeliveryTarget{Channel: "lansenger", GroupName: "研发群"}
	if err := app.resolveIMFileDeliveryTarget(&target); err != nil {
		t.Fatalf("resolveIMFileDeliveryTarget: %v", err)
	}
	if target.GroupID != "g-1" || target.GroupName != "研发群" {
		t.Fatalf("target=%#v", target)
	}
}

func TestResolveIMFileDeliveryTargetRejectsAmbiguousGroupName(t *testing.T) {
	app := &App{}
	app.scheduleTargetCatalogs = scheduler.NewTargetCatalogRegistry()
	app.scheduleTargetCatalogs.Register(scheduler.TargetCatalogFunc{
		ChannelName: scheduler.DeliveryChannelLansenger,
		List: func(context.Context, string) ([]scheduler.TargetRef, error) {
			return []scheduler.TargetRef{
				{Kind: scheduler.DeliveryKindGroup, ID: "g-1", Name: "研发群"},
				{Kind: scheduler.DeliveryKindGroup, ID: "g-2", Name: "研发群"},
			}, nil
		},
	})
	app.scheduleTargetCatalogsOnce.Do(func() {})
	target := agent.IMFileDeliveryTarget{Channel: "lansenger", GroupName: "研发群"}
	err := app.resolveIMFileDeliveryTarget(&target)
	if err == nil || !strings.Contains(err.Error(), "研发群") {
		t.Fatalf("err=%v", err)
	}
}
