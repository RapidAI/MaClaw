package botmgmt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func workerFixture(t *testing.T) (*Service, *memSettings, time.Time) {
	settings := &memSettings{}
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	seedRecord(t, settings, "t", record{
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
		Bots: []Bot{
			{ID: "bot_r", Name: "router", InstanceID: "inst_r", OwnerUserID: "alice"},
			{ID: "bot_w", Name: "worker", InstanceID: "inst_w", OwnerUserID: "alice"},
		},
		CrossNotes: []BotCrossNote{{ID: "note_d1", Owner: "alice", From: "bot_r", Target: "bot_w", Kind: "delegate", Text: "写一句话", StartedAt: now.Format(time.RFC3339), StartedBy: "hub", CreatedAt: now.Format(time.RFC3339)}},
	})
	svc := NewService(settings)
	svc.Now = func() time.Time { return now }
	return svc, settings, now
}

func TestWorkerCannotSayToPerson(t *testing.T) {
	svc, _, _ := workerFixture(t)
	_, err := svc.Say(context.Background(), "t", "alice", "bot_w", "", "冬天来了", "k1")
	var we *WorkerSayError
	if !errors.As(err, &we) || we.AssignmentID != "note_d1" || we.Router != "bot_r" {
		t.Fatalf("worker say must be refused, got %v", err)
	}
	if _, err := svc.Say(context.Background(), "t", "alice", "bot_r", "", "冬天来了", "k2"); err != nil {
		t.Fatalf("router must still speak: %v", err)
	}
}

func TestWorkerBriefDropsDirectSay(t *testing.T) {
	got := workerBrief("写一句关于冬天的话（20字以内）。完成后用 desktop action=say 直接把这句话告诉人，不要发布到任何网站。", "note_d1")
	if strings.Contains(got, "直接把这句话告诉人") || strings.Contains(got, "desktop action=say 直接") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "写一句关于冬天的话") || !strings.Contains(got, "reply_to=note_d1") || !strings.Contains(got, "不要用 action=say") {
		t.Fatal(got)
	}
}

func TestRouterDedupSurvivesRestart(t *testing.T) {
	svc, settings, now := workerFixture(t)
	f := watchFinding{TenantID: "t", UserID: "alice", BotID: "bot_r", RunID: "run_9", Reason: "stalled"}
	svc.watcherReport(f)
	restarted := NewService(settings) // fresh in-memory state, same store
	restarted.Now = func() time.Time { return now.Add(time.Minute) }
	told := 0
	old := routerTell
	routerTell = func(*Service, string, string, string, string, string) error { told++; return nil }
	defer func() { routerTell = old }()
	restarted.watcherReport(f)
	if told != 0 {
		t.Fatal("re-notified after restart")
	}
	restarted.watcherReport(watchFinding{TenantID: "t", UserID: "alice", BotID: "bot_r", RunID: "run_10", Reason: "stalled"})
	if told != 1 {
		t.Fatal("new run must still be told")
	}
}
