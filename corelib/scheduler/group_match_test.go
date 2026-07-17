package scheduler

import (
	"context"
	"strings"
	"testing"
)

func TestMatchGroupQuery(t *testing.T) {
	t.Parallel()
	groups := []GroupRef{
		{ID: "g1", Name: "产品讨论群"},
		{ID: "g2", Name: "产品运营群"},
		{ID: "g3", Name: "研发日报"},
	}
	if m := MatchGroupQuery("g1", groups); m.Status != GroupMatchUnique || m.Matches[0].ID != "g1" {
		t.Fatalf("id match: %+v", m)
	}
	if m := MatchGroupQuery("研发日报", groups); m.Status != GroupMatchUnique || m.Matches[0].ID != "g3" {
		t.Fatalf("exact name: %+v", m)
	}
	if m := MatchGroupQuery("产品", groups); m.Status != GroupMatchAmbiguous || len(m.Matches) != 2 {
		t.Fatalf("ambiguous: %+v", m)
	}
	if m := MatchGroupQuery("不存在", groups); m.Status != GroupMatchNone {
		t.Fatalf("none: %+v", m)
	}
}

func TestResolveDeliveryGroupNames(t *testing.T) {
	t.Parallel()
	groups := []GroupRef{{ID: "g3", Name: "研发日报"}}
	d := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupName: "研发日报"}},
	}
	if err := ResolveDeliveryGroupNames(d, groups); err != nil {
		t.Fatal(err)
	}
	if d.Targets[0].GroupID != "g3" {
		t.Fatalf("id=%q", d.Targets[0].GroupID)
	}
	if err := d.EnsureResolved(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetCatalogRegistry(t *testing.T) {
	t.Parallel()
	r := NewTargetCatalogRegistry()
	r.Register(TargetCatalogFunc{
		ChannelName: DeliveryChannelLansenger,
		List: func(ctx context.Context, query string) ([]TargetRef, error) {
			return []TargetRef{
				{Kind: DeliveryKindGroup, ID: "g1", Name: "Alpha 群"},
				{Kind: DeliveryKindGroup, ID: "g2", Name: "Beta"},
			}, nil
		},
	})
	list, err := r.ListTargets(context.Background(), "lansenger", "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	// Catalog returns all; Format/Filter applied by callers. ListTargets returns raw.
	if len(list) != 2 {
		t.Fatalf("len=%d", len(list))
	}
	filtered := FilterTargetRefs(list, "Alpha")
	if len(filtered) != 1 || filtered[0].ID != "g1" {
		t.Fatalf("%+v", filtered)
	}

	d := &TaskDelivery{
		Enabled: true,
		Channel: "lansenger",
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupName: "Alpha 群"}},
	}
	if err := r.ResolveDeliveryNames(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if d.Targets[0].GroupID != "g1" {
		t.Fatalf("resolved=%q", d.Targets[0].GroupID)
	}

	text := FormatTargetList("lansenger", list, "Beta")
	if !strings.Contains(text, "g2") || !strings.Contains(text, "group_id") {
		t.Fatalf("format=%q", text)
	}

	if _, err := r.ListTargets(context.Background(), "weixin", ""); err == nil {
		t.Fatal("expected unknown channel error")
	}
}
