package botmgmt

import (
	"context"
	"testing"
)

func TestRunEndKeepsWaitingHumanOpen(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_pub", Name: "发布员", OwnerUserID: "alice", InstanceID: "inst_pub", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	ctx := context.Background()
	d, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", "bot_main", BotCrossNote{Kind: "delegate", Target: "bot_pub", Text: "写介绍", Order: "写产品介绍"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_pub", d.ID); err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if _, err := svc.NoteBotCrossRun(ctx, "tenant-a", "alice", "bot_pub", d.ID, "run_q"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_pub", BotCrossNote{Topic: "question", State: "waiting_human", ExpectsReply: true, Text: "中文还是英文？", ReplyTo: d.ID, RunID: "run_q"}); err != nil {
		t.Fatal(err)
	}
	svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_q", "succeeded", Reply{Text: "中文还是英文？"}, nil)
	list, err := svc.ListAssignments(ctx, "tenant-a", "alice")
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if list[0].State != "waiting_human" || list[0].OpenQuestion == "" {
		t.Fatalf("assignment = %#v", list[0])
	}
}
