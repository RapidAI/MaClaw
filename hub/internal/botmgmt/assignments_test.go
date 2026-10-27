package botmgmt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func assignFixture(t *testing.T) *Service {
	return primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_a", Name: "A", OwnerUserID: "alice", InstanceID: "inst_a", CreatedAt: "2026-02-01T00:00:00Z"},
		{ID: "bot_b", Name: "B", OwnerUserID: "alice", InstanceID: "inst_b", CreatedAt: "2026-03-01T00:00:00Z"},
	})
}

func TestSubBotMayNotDelegate(t *testing.T) {
	svc := assignFixture(t)
	_, err := svc.PostBotDelegateFromInstance(context.Background(), "tenant-a", "alice", "inst_a", BotCrossNote{Target: "B", Text: "x"})
	if !errors.Is(err, ErrNotPrimary) {
		t.Fatalf("err = %v", err)
	}
}

func TestReportStateCausationAndReview(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "写稿"})
	if err != nil {
		t.Fatal(err)
	}
	if d.RootID != d.ID || d.Hop != 0 {
		t.Fatalf("root/hop = %q %d", d.RootID, d.Hop)
	}
	q, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "waiting_human", Text: "用哪个标题?", ExpectsReply: true, ReplyTo: d.ID})
	if err != nil || q.Topic != "question" || q.RootID != d.ID || q.Hop != 1 {
		t.Fatalf("question = %+v %v", q, err)
	}
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "bogus", Text: "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad state err = %v", err)
	}
	as, _ := svc.ListAssignments(ctx, "tenant-a", "alice")
	if len(as) != 1 || as[0].State != "waiting_human" || as[0].OpenQuestion != q.ID {
		t.Fatalf("assignments = %+v", as)
	}
	r, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "completed", Text: "写好了", ReplyTo: d.ID, Artifacts: []string{"/tmp/a.md"}})
	if err != nil || r.Topic != "done" {
		t.Fatalf("result = %+v %v", r, err)
	}
	if _, err := svc.ReviewAssignmentFromInstance(ctx, "tenant-a", "alice", "inst_b", d.ID, "accepted", ""); !errors.Is(err, ErrNotPrimary) {
		t.Fatalf("peer review without being reviewer: %v", err)
	}
	a, err := svc.ReviewAssignmentFromInstance(ctx, "tenant-a", "alice", "inst_main", d.ID, "returned", "标题太长")
	if err != nil || a.State != "returned" {
		t.Fatalf("review = %+v %v", a, err)
	}
	as, _ = svc.ListAssignments(ctx, "tenant-a", "alice")
	if len(as) != 2 || as[0].ParentID != d.ID || as[0].RootID != d.ID || !strings.Contains(as[0].Task, "标题太长") {
		t.Fatalf("returned child = %+v", as)
	}
}

func TestNamedReviewerMayReview(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "整理", Reviewer: "B"})
	if err != nil || d.Reviewer != "bot_b" {
		t.Fatalf("d = %+v %v", d, err)
	}
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "completed", Text: "好了", ReplyTo: d.ID}); err != nil {
		t.Fatal(err)
	}
	if a, err := svc.ReviewAssignmentFromInstance(ctx, "tenant-a", "alice", "inst_b", d.ID, "accepted", "ok"); err != nil || a.State != "accepted" {
		t.Fatalf("review = %+v %v", a, err)
	}
}

func TestAssignmentsKillSwitch(t *testing.T) {
	t.Setenv("MACLAW_ASSIGNMENTS", "off")
	svc := assignFixture(t)
	ctx := context.Background()
	// Unknown states are ignored, not rejected, when off.
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "bogus", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewAssignmentFromInstance(ctx, "tenant-a", "alice", "inst_main", "x", "accepted", ""); err == nil {
		t.Fatal("review allowed while off")
	}
}

// Each bot has its own desktop: one busy bot must not hold the others.
func TestParallelDispatchSkipsOnlyTheBusyBot(t *testing.T) {
	svc := assignFixture(t)
	var mu sync.Mutex
	var got []string
	busyA := true
	svc.BotDispatch = true
	svc.dispatchAdmit = func(ctx context.Context, tenantID, userID, botID, content, phase, key string) (BotAdmission, error) {
		mu.Lock()
		defer mu.Unlock()
		if botID == "bot_a" && busyA {
			return BotAdmission{}, ErrDesktopBusy
		}
		got = append(got, botID)
		return BotAdmission{Accepted: true, RunID: "run_" + key}, nil
	}
	ctx := context.Background()
	for _, tgt := range []string{"A", "B"} {
		if _, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: tgt, Text: "任务" + tgt}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 1 && got[0] == "bot_b" })
	mu.Lock()
	busyA = false
	mu.Unlock()
	svc.kickDispatch("tenant-a", "alice")
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 && got[1] == "bot_a" })
}
