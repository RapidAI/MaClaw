package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestGroupDiscussionHistoryStoreCachesSummariesAndHideState(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	summaries := []a2a.HubDiscussionSummary{{
		ID:             "disc-1",
		LocalRelation:  "initiated_by_me",
		Readonly:       false,
		Status:         "open",
		Topic:          "Plan",
		Question:       "How?",
		ParticipantIDs: []string{"me", "ve-a"},
		MessageCount:   2,
		UpdatedAt:      now,
	}}
	ctx := context.Background()
	if err := store.CacheSummaries(ctx, summaries, func(id string) string { return filepath.Join("root", id) }); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	cached, err := store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries: %v", err)
	}
	if len(cached) != 1 || cached[0].ID != "disc-1" || cached[0].LocalRelation != "initiated_by_me" {
		t.Fatalf("cached summaries = %+v", cached)
	}
	if err := store.SetHidden(ctx, "disc-1", true); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	cached, err = store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries hidden: %v", err)
	}
	if len(cached) != 0 {
		t.Fatalf("hidden discussion should be excluded: %+v", cached)
	}
	cached, err = store.CachedSummaries(ctx, true)
	if err != nil {
		t.Fatalf("CachedSummaries include hidden: %v", err)
	}
	if len(cached) != 1 {
		t.Fatalf("include hidden cached summaries = %+v", cached)
	}
}

func TestGroupDiscussionHistoryStoreCachesDetail(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-2", LocalRelation: "owned_ve_invited", Readonly: true, Status: "open"},
		Messages:   []a2a.Message{{ID: "m1", SessionID: "disc-2", FromID: "ve-a", Kind: a2a.MessageStatement, Content: "review", CreatedAt: time.Now().UTC()}},
	}
	if err := store.CacheDetail(ctx, detail, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "disc-2")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if cached.Discussion.ID != "disc-2" || len(cached.Messages) != 1 || cached.Messages[0].Content != "review" {
		t.Fatalf("cached detail = %+v", cached)
	}
}

func TestGroupDiscussionHistoryStoreCacheDetailPreservesCachedRelation(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "disc-3", LocalRelation: "owned_ve_invited", Readonly: true, Role: "review", Status: "open"}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	if err := store.CacheDetail(ctx, a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-3", Status: "open"}}, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "disc-3")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if cached.Discussion.LocalRelation != "owned_ve_invited" || !cached.Discussion.Readonly || cached.Discussion.Role != "review" {
		t.Fatalf("cached relation not preserved: %+v", cached.Discussion)
	}
}

func TestIsWritableHistoryDiscussionSummary(t *testing.T) {
	cases := []struct {
		name    string
		summary a2a.HubDiscussionSummary
		want    bool
	}{
		{name: "initiated open", summary: a2a.HubDiscussionSummary{ID: "mine", LocalRelation: "initiated_by_me", Status: "open"}, want: true},
		{name: "initiated closed", summary: a2a.HubDiscussionSummary{ID: "closed", LocalRelation: "initiated_by_me", Status: "closed"}, want: false},
		{name: "invited open readonly", summary: a2a.HubDiscussionSummary{ID: "invited", LocalRelation: "owned_ve_invited", Readonly: true, Status: "open"}, want: false},
		{name: "initiator role fallback", summary: a2a.HubDiscussionSummary{ID: "role", Role: "initiator", Status: "open"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWritableHistoryDiscussionSummary(tc.summary); got != tc.want {
				t.Fatalf("isWritableHistoryDiscussionSummary(%+v) = %v, want %v", tc.summary, got, tc.want)
			}
		})
	}
}

func TestGroupDiscussionHistoryStoreEnrichesCachedAttachmentPaths(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-4", LocalRelation: "initiated_by_me", Status: "open"},
		Messages: []a2a.Message{{
			ID:        "m1",
			SessionID: "disc-4",
			FromID:    "me",
			Kind:      a2a.MessageStatement,
			Content:   "see attached",
			FileAttachments: []a2a.FileAttachment{{
				FileURL:  "/api/ve/files/file-1",
				Filename: "report.pdf",
			}},
			ImageAttachments: []a2a.ImageAttachment{{
				FileURL:  "/api/ve/files/image-1",
				Filename: "chart.png",
			}},
		}},
	}
	if err := store.CacheDetail(ctx, detail, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: "file-1", DiscussionID: "disc-4", Filename: "report.pdf", HubURL: "/api/ve/files/file-1", LocalPath: filepath.Join("local", "report.pdf"), DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment file: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: "image-1", DiscussionID: "disc-4", Filename: "chart.png", HubURL: "/api/ve/files/image-1", LocalPath: filepath.Join("local", "chart.png"), DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment image: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "disc-4")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if got := cached.Messages[0].FileAttachments[0].LocalPath; got != filepath.Join("local", "report.pdf") {
		t.Fatalf("file local path = %q", got)
	}
	if got := cached.Messages[0].ImageAttachments[0].LocalPath; got != filepath.Join("local", "chart.png") {
		t.Fatalf("image local path = %q", got)
	}
}
