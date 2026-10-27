package botmgmt

import (
	"context"
	"errors"
	"testing"
)

func TestPublishDelegateGetsPeerReviewer(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "把这篇文章发布到网站"})
	if err != nil || d.Reviewer != "bot_b" {
		t.Fatalf("d = %+v %v", d, err)
	}
	plain, _ := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "整理一下资料"})
	if plain.Reviewer != "" {
		t.Fatalf("plain task got reviewer %q", plain.Reviewer)
	}
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_a", BotCrossNote{State: "completed", Text: "已发布", ReplyTo: d.ID, Artifacts: []string{"/tmp/shot.png"}}); err != nil {
		t.Fatal(err)
	}
	box, err := svc.BotMailbox(ctx, "tenant-a", "alice", "bot_b", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range box {
		if n.Topic == "question" && n.Target == "bot_b" && len(n.Artifacts) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("reviewer not asked: %+v", box)
	}
	as, _ := svc.ListAssignments(ctx, "tenant-a", "alice")
	for _, a := range as {
		if a.ID == d.ID && a.State != "awaiting_review" {
			t.Fatalf("state = %s", a.State)
		}
	}
	if a, err := svc.ReviewAssignmentFromInstance(ctx, "tenant-a", "alice", "inst_b", d.ID, "accepted", "链接可打开"); err != nil || a.State != "accepted" {
		t.Fatalf("review %+v %v", a, err)
	}
}

func TestPeerReviewOffAndTwoBotFallback(t *testing.T) {
	ctx := context.Background()
	two := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_a", Name: "A", OwnerUserID: "alice", InstanceID: "inst_a", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	d, _ := two.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "checkout and pay"})
	if d.Reviewer != "bot_main" {
		t.Fatalf("fallback reviewer = %q", d.Reviewer)
	}
	t.Setenv("MACLAW_PEER_REVIEW", "off")
	d2, _ := two.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "publish it"})
	if d2.Reviewer != "" {
		t.Fatalf("off reviewer = %q", d2.Reviewer)
	}
}

func TestSayDedupesAcrossBots(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	if _, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_main", "", "文章已经发布好了，链接在这里 https://x/1", "k1"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "文章已经发布好了 链接在这里 https://x/1", "k2")
	var said *AlreadySaidError
	if !errors.As(err, &said) || said.By != "bot_main" {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "另外：封面图我换成了横版。", "k3"); err != nil {
		t.Fatal(err)
	}
	items, _ := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if len(items) != 2 || items[1].From != "bot_a" {
		t.Fatalf("said = %+v", items)
	}
	t.Setenv("MACLAW_BOT_SAY", "off")
	if _, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "x", ""); err == nil {
		t.Fatal("say while off")
	}
}
