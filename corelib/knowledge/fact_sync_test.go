package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyVerifiedFactRewritesSavedTextAndSearch(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	saved, err := store.SaveText(ctx, TextSaveRequest{
		Title:   "jump-host",
		Text:    "跳板机 10.9.8.7 当前可达，SSH 端口 22",
		OwnerID: "desktop-user:proj",
		Kind:    SourceKindText,
	})
	if err != nil {
		t.Fatalf("SaveText: %v", err)
	}

	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity:  "ip:10.9.8.7",
		Claim:   "10.9.8.7 当前不可达",
		Evidence: "bash: 100% packet loss",
		OwnerID: "desktop-user:proj",
	})
	if err != nil {
		t.Fatalf("ApplyVerifiedFact: %v", err)
	}
	if got.Rewritten < 1 && got.Suppressed < 1 && !got.Inserted {
		t.Fatalf("expected a knowledge mutation, got %#v", got)
	}

	source, err := store.GetSource(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	body, err := store.sourcePlainText(ctx, source.ID)
	if err != nil {
		t.Fatalf("sourcePlainText: %v", err)
	}
	if strings.Contains(body, "可达") && !strings.Contains(body, "不可达") {
		t.Fatalf("stale reachable text still stored: %q", body)
	}

	results, err := store.Search(ctx, SearchOptions{Query: "10.9.8.7", OwnerID: "desktop-user:proj", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		results, err = store.Search(ctx, SearchOptions{Query: "当前不可达", OwnerID: "desktop-user:proj", Limit: 10})
		if err != nil {
			t.Fatalf("Search fallback: %v", err)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected verified claim to be searchable")
	}
	for _, result := range results {
		blob := result.Claim + " " + result.Summary + " " + result.Snippet + " " + result.Object
		if strings.Contains(blob, "可达") && !strings.Contains(blob, "不可达") {
			t.Fatalf("stale reachable claim still retrieved: %#v", result)
		}
	}
}

func TestApplyVerifiedFactStrictOwnerSkipsSharedSource(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "shared-up",
		Text:  "跳板机 10.0.0.9 当前可达",
		Kind:  SourceKindText,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity:      "ip:10.0.0.9",
		Claim:       "10.0.0.9 当前不可达",
		OwnerID:     "desktop-user:proj",
		StrictOwner: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten != 0 || got.Suppressed != 0 || got.Inserted {
		t.Fatalf("isolated write must not mutate shared knowledge: %#v", got)
	}
}

func TestApplyVerifiedFactStrictOwnerSkipsOtherTenant(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title:    "tenant-a-up",
		Text:     "跳板机 10.0.0.5 当前可达",
		Kind:     SourceKindText,
		OwnerID:  "user-a",
		TenantID: "tenant-a",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity:      "ip:10.0.0.5",
		Claim:       "10.0.0.5 当前不可达",
		OwnerID:     "user-b",
		TenantID:    "tenant-b",
		StrictOwner: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten != 0 || got.Suppressed != 0 || got.Inserted {
		t.Fatalf("tenant-b must not mutate tenant-a knowledge: %#v", got)
	}
}

func TestSaveTextWithTenantDoesNotMutateSharedSource(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	shared, err := store.SaveText(ctx, TextSaveRequest{
		Title: "shared-up",
		Text:  "跳板机 10.0.0.4 当前可达",
		Kind:  SourceKindText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title:    "tenant-b-down",
		Text:     "跳板机 10.0.0.4 当前不可达",
		Kind:     SourceKindText,
		OwnerID:  "user-b",
		TenantID: "tenant-b",
	}); err != nil {
		t.Fatal(err)
	}
	body, err := store.sourcePlainText(ctx, shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "可达") || strings.Contains(body, "不可达") {
		t.Fatalf("tenant save must not rewrite shared knowledge: %q", body)
	}
}

func TestApplyVerifiedFactInsertsEvenIfSamePolarityCardExists(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "already-down",
		Text:  "10.4.4.4 当前不可达",
		Kind:  SourceKindText,
	}); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("手册\n跳板机 10.4.4.4 当前可达。\n")
	for i := 0; i < 40; i++ {
		body.WriteString("部署、监控、回滚与值班说明，保留原文。\n")
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "manual-up",
		Text:  body.String(),
		Kind:  SourceKindText,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity: "ip:10.4.4.4",
		Claim:  "10.4.4.4 当前不可达",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten != 0 {
		t.Fatalf("manual must not be rewritten: %#v", got)
	}
}

func TestApplyVerifiedFactUpdatesAddressClaim(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	saved, err := store.SaveText(ctx, TextSaveRequest{
		Title: "api-url",
		Text:  "API 地址是 https://old.example.com",
		Kind:  SourceKindText,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Claim: "API 地址是 https://new.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten < 1 && got.Suppressed < 1 && !got.Inserted {
		t.Fatalf("address fact not updated: %#v", got)
	}
	if got.Rewritten > 0 {
		body, err := store.sourcePlainText(ctx, saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "old.example.com") && !strings.Contains(body, "new.example.com") {
			t.Fatalf("stale address still stored: %q", body)
		}
	}
}

func TestApplyVerifiedFactAliasesRewriteResolvedIPSource(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	saved, err := store.SaveText(ctx, TextSaveRequest{
		Title: "ip-up",
		Text:  "服务器 10.1.2.3 当前可达",
		Kind:  SourceKindText,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity:  "host:jump.example.com",
		Claim:   "jump.example.com 当前不可达",
		Aliases: []string{"ip:10.1.2.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten < 1 && got.Suppressed < 1 && !got.Inserted {
		t.Fatalf("resolved IP knowledge not updated: %#v", got)
	}
	body, err := store.sourcePlainText(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "可达") && !strings.Contains(body, "不可达") {
		t.Fatalf("stale IP fact still stored: %q", body)
	}
}

func TestApplyVerifiedFactDoesNotInsertWhenNothingStored(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity: "ip:10.0.0.1",
		Claim:  "10.0.0.1 当前不可达",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten != 0 || got.Suppressed != 0 || got.Inserted {
		t.Fatalf("empty store must not be seeded: %#v", got)
	}
}

func TestApplyVerifiedFactDoesNotRewriteLongDocument(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	var body strings.Builder
	body.WriteString("运维手册\n跳板机 10.8.7.6 当前可达，SSH 端口 22。\n")
	for i := 0; i < 40; i++ {
		body.WriteString("后续章节包含部署、监控、回滚与值班说明，不能整篇替换成单行结论。\n")
	}
	saved, err := store.SaveText(ctx, TextSaveRequest{
		Title: "ops-manual",
		Text:  body.String(),
		Kind:  SourceKindText,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ApplyVerifiedFact(ctx, VerifiedFactRequest{
		Entity: "ip:10.8.7.6",
		Claim:  "10.8.7.6 当前不可达",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rewritten != 0 {
		t.Fatalf("long document must not be rewritten in place: %#v", got)
	}
	kept, err := store.sourcePlainText(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kept, "运维手册") || !strings.Contains(kept, "部署、监控") {
		t.Fatalf("long document body was destroyed: %q", kept[:min(len(kept), 80)])
	}
}

func TestSaveTextSyncsContradictingVerifiedFact(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	old, err := store.SaveText(ctx, TextSaveRequest{
		Title: "server-up",
		Text:  "服务器 172.16.1.4 当前可达",
		Kind:  SourceKindText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveText(ctx, TextSaveRequest{
		Title: "server-down",
		Text:  "服务器 172.16.1.4 当前不可达",
		Kind:  SourceKindText,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetSource(ctx, old.ID); err != nil {
		return
	}
	body, err := store.sourcePlainText(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "不可达") {
		return
	}
	items, err := store.ListSuppressedCards(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.SourceID == old.ID {
			return
		}
	}
	t.Fatalf("old reachable source still live; body=%q suppressions=%d", body, len(items))
}
