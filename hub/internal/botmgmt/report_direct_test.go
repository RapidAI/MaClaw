package botmgmt

import (
	"context"
	"testing"
)

// The person asked bot_a in its own chat: its file report for the main bot
// is direct, so MaClawSrv attaches the file in bot_a's chat instead.
func TestReportRunIsDirectForPersonStartedRun(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	direct, err := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "run_person", "")
	if err != nil || !direct {
		t.Fatalf("direct=%v err=%v", direct, err)
	}
	for _, target := range []string{"main", "主"} {
		if d, _ := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "run_person", target); !d {
			t.Fatalf("target %q (the main bot) not direct", target)
		}
	}
	if d, _ := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "run_person", "B"); d {
		t.Fatal("a named peer is a real handoff, never direct")
	}
	if d, _ := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_main", "run_main", ""); d {
		t.Fatal("the main bot itself is never redirected")
	}
	if d, _ := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "", ""); d {
		t.Fatal("no run id: Hub cannot place the run")
	}
}

// A run the main bot delegated (its run id stored on the delegate note)
// keeps reporting files to the main bot.
func TestReportRunIsDirectFalseForDelegatedRun(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "写简历"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_a", d.ID); err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if _, err := svc.NoteBotCrossRun(ctx, "tenant-a", "alice", "bot_a", d.ID, "run_delegated"); err != nil {
		t.Fatal(err)
	}
	if direct, err := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "run_delegated", ""); err != nil || direct {
		t.Fatalf("delegated run direct=%v err=%v", direct, err)
	}
	if direct, _ := svc.ReportRunIsDirect(ctx, "tenant-a", "alice", "inst_a", "run_other", ""); !direct {
		t.Fatal("another run of the same bot the person started is direct")
	}
}

func TestReportRunIsDirectFalseForWakeRun(t *testing.T) {
	notes := []BotCrossNote{{Owner: "alice", From: "bot_main", Target: "bot_a", Kind: "report", WakeRunID: "run_wake"}}
	if reportRunIsDirect(notes, "alice", "bot_a", "run_wake") {
		t.Fatal("a mailbox wake is not the person's own request")
	}
	if !reportRunIsDirect(notes, "alice", "bot_a", "run_person") {
		t.Fatal("unrelated run should be direct")
	}
}
