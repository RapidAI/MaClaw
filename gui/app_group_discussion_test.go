package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestGroupDiscussionListMineFallsBackToLocalCacheWhenDisabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: false}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "cached-1", LocalRelation: "initiated_by_me", Status: "open", Topic: "Cached VE", ParticipantIDs: []string{"local", "ve-a"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	got, err := app.GroupDiscussionListMine("all")
	if err != nil {
		t.Fatalf("GroupDiscussionListMine: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cached-1" {
		t.Fatalf("cached discussions = %+v", got)
	}
}

func TestGroupDiscussionGetConsultationDetailFallsBackToLocalCacheWhenDisabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: false}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "cached-detail", LocalRelation: "initiated_by_me", Status: "open", Topic: "Cached detail", UpdatedAt: time.Now().UTC()},
		Messages:   []a2a.Message{{ID: "m1", FromID: "local", Content: "hello"}},
	}
	if err := store.CacheDetail(ctx, detail, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}

	got, err := app.GroupDiscussionGetConsultationDetail("cached-detail")
	if err != nil {
		t.Fatalf("GroupDiscussionGetConsultationDetail: %v", err)
	}
	if got.Discussion.ID != "cached-detail" || len(got.Messages) != 1 {
		t.Fatalf("cached detail = %+v", got)
	}
}
