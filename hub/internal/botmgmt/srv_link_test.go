package botmgmt

import (
	"context"
	"strings"
	"testing"
)

func TestSaveSrvLinkRegistersKeepsSecretAndDeletes(t *testing.T) {
	settings := &memSettings{}
	svc := NewService(settings)
	ctx := context.Background()

	secret := "adm-secret-1"
	view, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{
		Name:        "srv-a",
		BaseURL:     "https://srv-a.example.com/",
		AdminSecret: &secret,
	})
	if err != nil {
		t.Fatalf("SaveSrvLink: %v", err)
	}
	if view.ID == "" || !strings.HasPrefix(view.ID, "srv_") {
		t.Fatalf("expected generated srv_ id, got %q", view.ID)
	}
	if view.BaseURL != "https://srv-a.example.com" {
		t.Fatalf("trailing slash not trimmed: %q", view.BaseURL)
	}
	if !view.AdminSecretSet {
		t.Fatalf("admin_secret_set should be true: %+v", view)
	}

	// An update with a nil secret keeps the stored one.
	updated, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{ID: view.ID, Name: "srv-a2", BaseURL: "https://srv-a2.example.com"})
	if err != nil {
		t.Fatalf("SaveSrvLink update: %v", err)
	}
	if updated.ID != view.ID || updated.Name != "srv-a2" || !updated.AdminSecretSet {
		t.Fatalf("update lost secret or id: %+v", updated)
	}
	rec := storedRecord(t, settings, "t1")
	if got := findSrvLink(rec.SrvLinks, view.ID); got == nil || got.AdminSecret != secret {
		t.Fatalf("stored secret changed on nil update: %+v", got)
	}

	// Second link lists alongside the first; the view exposes both.
	if _, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{BaseURL: "https://srv-b.example.com"}); err != nil {
		t.Fatalf("SaveSrvLink second: %v", err)
	}
	links := svc.SrvLinks(ctx, "t1")
	if len(links) != 2 {
		t.Fatalf("SrvLinks len=%d want 2", len(links))
	}
	settingsView, err := svc.View(ctx, "t1")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if len(settingsView.SrvLinks) != 2 {
		t.Fatalf("SettingsView.SrvLinks len=%d want 2", len(settingsView.SrvLinks))
	}

	if err := svc.DeleteSrvLink(ctx, "t1", view.ID); err != nil {
		t.Fatalf("DeleteSrvLink: %v", err)
	}
	if links := svc.SrvLinks(ctx, "t1"); len(links) != 1 {
		t.Fatalf("after delete len=%d want 1", len(links))
	}
	// Deleting a missing id is a no-op.
	if err := svc.DeleteSrvLink(ctx, "t1", "srv_missing"); err != nil {
		t.Fatalf("DeleteSrvLink missing: %v", err)
	}
}

func TestSaveSrvLinkValidatesURLAndNodeIDCollision(t *testing.T) {
	settings := &memSettings{}
	svc := NewService(settings)
	ctx := context.Background()
	seedRecord(t, settings, "t1", record{NodeLinks: []nodeLink{{NodeID: "node-9", BaseURL: "http://n9"}}})

	if _, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{BaseURL: ""}); err == nil {
		t.Fatalf("empty base_url should fail")
	}
	if _, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{BaseURL: "ftp://x"}); err == nil {
		t.Fatalf("non-http scheme should fail")
	}
	if _, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{BaseURL: "http://a.node.invalid"}); err == nil {
		t.Fatalf("node.invalid host should fail")
	}
	if _, err := svc.SaveSrvLink(ctx, "t1", SrvLinkInput{ID: "node-9", BaseURL: "https://x.example.com"}); err == nil {
		t.Fatalf("id collision with a node link should fail")
	}
}
