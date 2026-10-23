package botmgmt

import (
	"context"
	"testing"
	"time"
)

// A GUI-hosted local bot is a full mailbox participant: the server-side main
// bot can delegate to it by name (it lands as a task in its Hub mailbox, Hub
// never admits a run for it), and it answers the main bot through Hub.
func TestLocalBotIsAMailboxParticipant(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	lb, err := svc.RegisterLocalBot(ctx, "tenant-a", "alice", LocalBot{Name: "整理员", Device: "PC-1", Capabilities: []string{"file_read", "file_write"}})
	if err != nil || lb.ID == "" {
		t.Fatalf("register: %+v %v", lb, err)
	}
	again, _ := svc.RegisterLocalBot(ctx, "tenant-a", "alice", LocalBot{Name: "整理员", Device: "PC-1"})
	if again.ID != lb.ID {
		t.Fatal("heartbeat made a second bot")
	}
	if _, err := svc.RegisterLocalBot(ctx, "tenant-a", "alice", LocalBot{Name: "发布员"}); err == nil {
		t.Fatal("local bot took a Hub bot's name")
	}
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "整理员", Text: "把 D:/稿子 里的周报整理好", Key: "lk"})
	if err != nil || d.Kind != "report" || d.Topic != "task" || d.Target != lb.ID {
		t.Fatalf("delegate to local: %+v %v", d, err)
	}
	mail, err := svc.BotMailbox(ctx, "tenant-a", "alice", lb.ID, nil, false)
	if err != nil || len(mail) != 1 || mail[0].ID != d.ID {
		t.Fatalf("local mailbox: %+v %v", mail, err)
	}
	time.Sleep(80 * time.Millisecond)
	if len(f.list()) != 0 {
		t.Fatalf("Hub admitted a run for a local bot: %v", f.list())
	}
	// The local bot answers through the user route; it lands in main's mailbox.
	r, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", lb.ID, BotCrossNote{Kind: "report", Target: "main", Topic: "done", Text: "整理好了", ReplyTo: d.ID})
	if err != nil || r.Target != "bot_main" {
		t.Fatalf("local report: %+v %v", r, err)
	}
	if _, err := svc.BotMailbox(ctx, "tenant-a", "alice", lb.ID, []string{d.ID}, false); err != nil {
		t.Fatal(err)
	}
	team, _ := svc.BotTeam(ctx, "tenant-a", "alice")
	if len(team) != 3 || team[2]["location"] != "local" {
		t.Fatalf("team = %v", team)
	}
	if err := svc.RemoveLocalBot(ctx, "tenant-a", "alice", lb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BotMailbox(ctx, "tenant-a", "alice", lb.ID, nil, false); err == nil {
		t.Fatal("removed local bot still has a mailbox")
	}
}
