package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestGroupDiscussionHistoryStoreCacheSummariesNormalizesRelationAndReadonly(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{
		{ID: "role-review", Role: "review", Status: "open", Readonly: false},
		{ID: "closed-mine", LocalRelation: "initiated_by_me", Status: "closed", Readonly: false},
	}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	cached, err := store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries: %v", err)
	}
	byID := map[string]a2a.HubDiscussionSummary{}
	for _, summary := range cached {
		byID[summary.ID] = summary
	}
	if got := byID["role-review"]; got.LocalRelation != "owned_ve_invited" || !got.Readonly {
		t.Fatalf("role-review not normalized: %+v", got)
	}
	if got := byID["closed-mine"]; got.LocalRelation != "initiated_by_me" || !got.Readonly {
		t.Fatalf("closed-mine not normalized: %+v", got)
	}
}
func TestGroupDiscussionHistoryStoreNormalizesReadonlyOnCachedRead(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "invited-read", LocalRelation: "owned_ve_invited", Status: "open", Readonly: false}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE group_discussion_summaries SET summary_json = ? WHERE discussion_id = ?`, `{"id":"invited-read","local_relation":"owned_ve_invited","status":"open","readonly":false}`, "invited-read"); err != nil {
		t.Fatalf("force stale summary_json: %v", err)
	}
	cached, err := store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries: %v", err)
	}
	if len(cached) != 1 || !cached[0].Readonly {
		t.Fatalf("cached invited summary should be readonly after read normalization: %+v", cached)
	}
}

func TestGroupDiscussionHistoryStoreNormalizesReadonlyOnCachedDetailRead(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO group_discussion_details (discussion_id, detail_json) VALUES (?, ?)`, "invited-detail", `{"discussion":{"id":"invited-detail","local_relation":"owned_ve_invited","status":"open","readonly":false}}`); err != nil {
		t.Fatalf("seed stale detail_json: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "invited-detail")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if !cached.Discussion.Readonly {
		t.Fatalf("cached invited detail should be readonly after read normalization: %+v", cached.Discussion)
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

func TestGroupDiscussionHistoryStoreMaterializesInlineTextAttachments(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "attachments")
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-text", LocalRelation: "owned_ve_invited", Status: "open"},
		Messages: []a2a.Message{{
			ID:      "msg-1",
			FromID:  "ve-1",
			Kind:    a2a.MessageStatement,
			Content: "inline attachment",
			TextAttachments: []a2a.TextAttachment{{
				Filename: "notes.txt",
				MimeType: "text/plain",
				Content:  base64.StdEncoding.EncodeToString([]byte("review notes")),
			}},
		}},
	}
	if err := store.CacheDetail(ctx, detail, func(string) string { return root }); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "disc-text")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	localPath := cached.Messages[0].TextAttachments[0].LocalPath
	if localPath == "" {
		t.Fatalf("text attachment local path was not materialized: %+v", cached.Messages[0].TextAttachments[0])
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read materialized text attachment: %v", err)
	}
	if string(data) != "review notes" {
		t.Fatalf("materialized text content = %q", string(data))
	}
	records, err := store.DownloadedAttachments(ctx, "disc-text")
	if err != nil {
		t.Fatalf("DownloadedAttachments: %v", err)
	}
	if len(records) != 1 || records[0].Kind != "text" || records[0].LocalPath != localPath {
		t.Fatalf("downloaded text record = %+v", records)
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

func TestGroupDiscussionHistoryStoreEnrichesAttachmentPathsByIDBeforeFilename(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-same-name", LocalRelation: "initiated_by_me", Status: "open"},
		Messages: []a2a.Message{{
			ID:        "m1",
			SessionID: "disc-same-name",
			FromID:    "me",
			Kind:      a2a.MessageStatement,
			FileAttachments: []a2a.FileAttachment{
				{FileURL: "/api/ve/files/download/file-1?session_id=disc-same-name", Filename: "report.pdf"},
				{FileURL: "/api/ve/files/download/file-2?session_id=disc-same-name", Filename: "report.pdf"},
			},
		}},
	}
	if err := store.CacheDetail(ctx, detail, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: "file-1", DiscussionID: "disc-same-name", Filename: "report.pdf", HubURL: "/api/ve/files/file-1", LocalPath: filepath.Join("local", "first.pdf"), DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment file-1: %v", err)
	}
	if err := store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: "file-2", DiscussionID: "disc-same-name", Filename: "report.pdf", HubURL: "/api/ve/files/file-2", LocalPath: filepath.Join("local", "second.pdf"), DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment file-2: %v", err)
	}

	cached, ok, err := store.CachedDetail(ctx, "disc-same-name")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if got := cached.Messages[0].FileAttachments[0].LocalPath; got != filepath.Join("local", "first.pdf") {
		t.Fatalf("first attachment local path = %q", got)
	}
	if got := cached.Messages[0].FileAttachments[1].LocalPath; got != filepath.Join("local", "second.pdf") {
		t.Fatalf("second attachment local path = %q", got)
	}
}

func TestGroupDiscussionHistoryStoreKeepsHiddenStateAcrossRefresh(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	first := a2a.HubDiscussionSummary{ID: "disc-hidden", LocalRelation: "owned_ve_invited", Readonly: true, Status: "open", Topic: "First", UpdatedAt: time.Now().UTC()}
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{first}, nil); err != nil {
		t.Fatalf("CacheSummaries first: %v", err)
	}
	if err := store.SetHidden(ctx, "disc-hidden", true); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	refreshed := first
	refreshed.Topic = "Refreshed"
	refreshed.UpdatedAt = refreshed.UpdatedAt.Add(time.Minute)
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{refreshed}, nil); err != nil {
		t.Fatalf("CacheSummaries refresh: %v", err)
	}
	visible, err := store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries visible: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("hidden discussion should stay hidden after refresh: %+v", visible)
	}
	hidden, err := store.HiddenSummaries(ctx)
	if err != nil {
		t.Fatalf("HiddenSummaries: %v", err)
	}
	if len(hidden) != 1 || hidden[0].Topic != "Refreshed" {
		t.Fatalf("hidden summaries = %+v", hidden)
	}
	if err := store.SetHidden(ctx, "disc-hidden", false); err != nil {
		t.Fatalf("restore SetHidden: %v", err)
	}
	visible, err = store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries restored: %v", err)
	}
	if len(visible) != 1 || visible[0].ID != "disc-hidden" {
		t.Fatalf("restored visible summaries = %+v", visible)
	}
}

func TestGroupDiscussionHistoryStoreSeparatesAttachmentsByDiscussion(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	for _, record := range []GroupDiscussionAttachmentRecord{
		{AttachmentID: "same-file", DiscussionID: "disc-a", Filename: "report.pdf", HubURL: "/api/ve/files/a", LocalPath: filepath.Join("local", "a.pdf"), DownloadState: "downloaded"},
		{AttachmentID: "same-file", DiscussionID: "disc-b", Filename: "report.pdf", HubURL: "/api/ve/files/b", LocalPath: filepath.Join("local", "b.pdf"), DownloadState: "downloaded"},
	} {
		if err := store.UpsertDownloadedAttachment(ctx, record); err != nil {
			t.Fatalf("UpsertDownloadedAttachment(%s): %v", record.DiscussionID, err)
		}
	}
	aRecords, err := store.DownloadedAttachments(ctx, "disc-a")
	if err != nil {
		t.Fatalf("DownloadedAttachments disc-a: %v", err)
	}
	bRecords, err := store.DownloadedAttachments(ctx, "disc-b")
	if err != nil {
		t.Fatalf("DownloadedAttachments disc-b: %v", err)
	}
	if len(aRecords) != 1 || aRecords[0].LocalPath != filepath.Join("local", "a.pdf") {
		t.Fatalf("disc-a attachments = %+v", aRecords)
	}
	if len(bRecords) != 1 || bRecords[0].LocalPath != filepath.Join("local", "b.pdf") {
		t.Fatalf("disc-b attachments = %+v", bRecords)
	}
}

func TestGroupDiscussionHistoryStoreMigratesLegacyAttachmentPrimaryKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE group_discussion_attachments (
    attachment_id TEXT PRIMARY KEY,
    discussion_id TEXT NOT NULL,
    message_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    hub_url TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    checksum TEXT NOT NULL DEFAULT '',
    download_state TEXT NOT NULL DEFAULT 'remote',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);`)
	if err != nil {
		t.Fatalf("create legacy attachments table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO group_discussion_attachments (attachment_id, discussion_id, filename, local_path, download_state) VALUES ('same-file', 'legacy-disc', 'old.pdf', 'old.pdf', 'downloaded')`); err != nil {
		t.Fatalf("insert legacy attachment: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewGroupDiscussionHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: "same-file", DiscussionID: "new-disc", Filename: "new.pdf", LocalPath: "new.pdf", DownloadState: "downloaded"}); err != nil {
		t.Fatalf("UpsertDownloadedAttachment after migration: %v", err)
	}
	legacy, err := store.DownloadedAttachments(ctx, "legacy-disc")
	if err != nil {
		t.Fatalf("DownloadedAttachments legacy-disc: %v", err)
	}
	newer, err := store.DownloadedAttachments(ctx, "new-disc")
	if err != nil {
		t.Fatalf("DownloadedAttachments new-disc: %v", err)
	}
	if len(legacy) != 1 || legacy[0].LocalPath != "old.pdf" {
		t.Fatalf("legacy attachments = %+v", legacy)
	}
	if len(newer) != 1 || newer[0].LocalPath != "new.pdf" {
		t.Fatalf("new attachments = %+v", newer)
	}
}

func TestGroupDiscussionHistoryStoreMigratesLegacySummaryColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE group_discussion_summaries (
    discussion_id TEXT PRIMARY KEY,
    summary_json TEXT NOT NULL
);`)
	if err != nil {
		t.Fatalf("create legacy summaries table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO group_discussion_summaries (discussion_id, summary_json) VALUES ('old-disc', '{"id":"old-disc"}')`); err != nil {
		t.Fatalf("insert legacy summary: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewGroupDiscussionHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "new-disc", LocalRelation: "initiated_by_me", Status: "open", Topic: "Migrated", UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries after migration: %v", err)
	}
	cached, err := store.CachedSummaries(ctx, false)
	if err != nil {
		t.Fatalf("CachedSummaries: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("cached summaries count = %d, summaries=%+v", len(cached), cached)
	}
	var foundNew bool
	for _, summary := range cached {
		if summary.ID == "new-disc" && summary.Topic == "Migrated" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("new migrated summary not found: %+v", cached)
	}
}

func TestFilterGroupDiscussionSummariesByRoleTreatsAllAsUnfiltered(t *testing.T) {
	summaries := []a2a.HubDiscussionSummary{{ID: "mine", Role: "initiator"}, {ID: "review", Role: "review"}}
	if got := filterGroupDiscussionSummariesByRole(append([]a2a.HubDiscussionSummary{}, summaries...), "all"); len(got) != 2 {
		t.Fatalf("filter all returned %d summaries: %+v", len(got), got)
	}
	if got := filterGroupDiscussionSummariesByRole(append([]a2a.HubDiscussionSummary{}, summaries...), "review"); len(got) != 1 || got[0].ID != "review" {
		t.Fatalf("filter review = %+v", got)
	}
}

func TestFilterGroupDiscussionSummariesByRoleUsesRelationWhenRoleMissing(t *testing.T) {
	summaries := []a2a.HubDiscussionSummary{
		{ID: "initiated", LocalRelation: "initiated_by_me"},
		{ID: "invited", LocalRelation: "owned_ve_invited"},
		{ID: "review", Role: "review", LocalRelation: "owned_ve_invited"},
	}
	initiated := filterGroupDiscussionSummariesByRole(append([]a2a.HubDiscussionSummary{}, summaries...), "initiator")
	if len(initiated) != 1 || initiated[0].ID != "initiated" {
		t.Fatalf("initiator filter = %+v", initiated)
	}
	review := filterGroupDiscussionSummariesByRole(append([]a2a.HubDiscussionSummary{}, summaries...), "review")
	if len(review) != 2 || review[0].ID != "invited" || review[1].ID != "review" {
		t.Fatalf("review filter = %+v", review)
	}
}
func TestMergeGroupDiscussionSummariesKeepsLocalInitiatedSessions(t *testing.T) {
	live := []a2a.HubDiscussionSummary{{ID: "invited", Role: "review", LocalRelation: "owned_ve_invited"}}
	cached := []a2a.HubDiscussionSummary{
		{ID: "local-only", Role: "initiator", LocalRelation: "initiated_by_me"},
		{ID: "invited", Role: "review", LocalRelation: "owned_ve_invited", Topic: "stale"},
	}
	got := mergeGroupDiscussionSummaries(live, cached, "all")
	if len(got) != 2 {
		t.Fatalf("merged summaries = %+v", got)
	}
	if got[0].ID != "invited" || got[0].Topic == "stale" {
		t.Fatalf("live summary should win for duplicate ids: %+v", got)
	}
	if got[1].ID != "local-only" || got[1].LocalRelation != "initiated_by_me" {
		t.Fatalf("local initiated summary missing: %+v", got)
	}
}

func TestMergeGroupDiscussionSummariesSortsByUpdatedAt(t *testing.T) {
	base := time.Now().UTC()
	live := []a2a.HubDiscussionSummary{{ID: "older-live", Role: "review", Status: "open", UpdatedAt: base}}
	cached := []a2a.HubDiscussionSummary{{ID: "newer-cached", Role: "initiator", Status: "open", UpdatedAt: base.Add(time.Minute)}}
	got := mergeGroupDiscussionSummaries(live, cached, "all")
	if len(got) != 2 || got[0].ID != "newer-cached" || got[1].ID != "older-live" {
		t.Fatalf("merged summaries not sorted by updated_at desc: %+v", got)
	}
	if got[0].LocalRelation != "initiated_by_me" || got[1].LocalRelation != "owned_ve_invited" || !got[1].Readonly {
		t.Fatalf("merged summaries not normalized: %+v", got)
	}
}
func TestGroupDiscussionHistoryStoreCacheDetailDerivesRelationFromIncomingRole(t *testing.T) {
	store, err := NewGroupDiscussionHistoryStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "disc-role", LocalRelation: "owned_ve_invited", Readonly: true, Role: "review", Status: "open"}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	if err := store.CacheDetail(ctx, a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-role", Role: "initiator", Status: "open"}}, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}
	cached, ok, err := store.CachedDetail(ctx, "disc-role")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if cached.Discussion.LocalRelation != "initiated_by_me" || cached.Discussion.Readonly {
		t.Fatalf("incoming initiator role should override stale cached relation: %+v", cached.Discussion)
	}
}

func TestNormalizeHistorySummaryReadonlyForInvitedAndClosed(t *testing.T) {
	invited := normalizeHistorySummaryReadonly(a2a.HubDiscussionSummary{ID: "invited", LocalRelation: "owned_ve_invited", Status: "open"})
	if !invited.Readonly {
		t.Fatalf("owned_ve_invited open session should be readonly: %+v", invited)
	}
	closed := normalizeHistorySummaryReadonly(a2a.HubDiscussionSummary{ID: "closed", LocalRelation: "initiated_by_me", Status: "closed"})
	if !closed.Readonly {
		t.Fatalf("closed initiated session should be readonly: %+v", closed)
	}
}

func TestGroupDiscussionSendHistoryMessageUsesParticipantScopedDetail(t *testing.T) {
	detailHits := 0
	messageHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/consultations/disc-1/detail":
			detailHits++
			if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
				t.Fatalf("detail participant_id = %q raw=%q", got, r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: "open", LocalRelation: "initiated_by_me", Readonly: false, Role: "initiator"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-1/messages":
			messageHits++
			var msg a2a.GroupDiscussionMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			if msg.FromID != "machine-1" || msg.SessionID != "disc-1" || msg.Content != "continue" {
				t.Fatalf("message = %+v", msg)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}}
	if err := app.GroupDiscussionSendHistoryMessage("disc-1", a2a.GroupDiscussionMessage{FromID: "spoofed-sender", SessionID: "wrong-session", Content: "continue"}); err != nil {
		t.Fatalf("GroupDiscussionSendHistoryMessage: %v", err)
	}
	if detailHits != 2 || messageHits != 1 {
		t.Fatalf("hits detail=%d messages=%d", detailHits, messageHits)
	}
}

func TestGroupDiscussionSendHistoryMessageBlocksInvitedDetail(t *testing.T) {
	messageHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/consultations/disc-2/detail":
			_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-2", Status: "open", LocalRelation: "owned_ve_invited", Readonly: true, Role: "review"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-2/messages":
			messageHits++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}}
	err := app.GroupDiscussionSendHistoryMessage("disc-2", a2a.GroupDiscussionMessage{Content: "should not send"})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
	if messageHits != 0 {
		t.Fatalf("read-only history should not send messages, hits=%d", messageHits)
	}
}
