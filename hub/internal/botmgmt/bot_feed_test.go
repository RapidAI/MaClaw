package botmgmt

import (
	"context"
	"testing"
)

// A server-side wake acks the main bot's mail, but the GUI that opens later
// still shows the whole exchange (delegate, the teammate's file message and
// result, and which wake run took them) until it marks them shown.
func TestServerWakeDoesNotHideMessagesFromTheGUI(t *testing.T) {
	svc, f := dispatchFixture(t)
	ctx := context.Background()
	d, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "发布员", Text: "写个文件", Key: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(f.list()) == 1 })
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		n := findCrossNote(notes, d.ID)
		return n != nil && n.RunID != ""
	})
	svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_xdel-"+d.ID, "succeeded", Reply{Text: "写好了"}, nil)
	waitFor(t, func() bool { return len(f.list()) == 2 })
	var result *BotCrossNote
	waitFor(t, func() bool {
		notes, _ := svc.ListBotCrossNotes(ctx, "tenant-a", "alice")
		for i := range notes {
			if notes[i].ReplyTo == d.ID && notes[i].WakeRunID != "" {
				result = &notes[i]
				return true
			}
		}
		return false
	})
	// The server-side main run takes (acks) its mail.
	if _, err := svc.BotMailbox(ctx, "tenant-a", "alice", "main", []string{result.ID}, false); err != nil {
		t.Fatal(err)
	}
	feed, err := svc.BotFeed(ctx, "tenant-a", "alice", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]BotCrossNote{}
	for _, n := range feed {
		seen[n.ID] = n
	}
	if _, ok := seen[d.ID]; !ok {
		t.Fatalf("delegate missing from the GUI feed: %+v", feed)
	}
	if r, ok := seen[result.ID]; !ok || r.AckedAt == "" || r.WakeRunID == "" {
		t.Fatalf("acked result must still be shown, with its wake run: %+v", r)
	}
	ids := []string{}
	for _, n := range feed {
		ids = append(ids, n.ID)
	}
	if rest, _ := svc.BotFeed(ctx, "tenant-a", "alice", ids, false); len(rest) != 0 {
		t.Fatalf("shown notes came back: %+v", rest)
	}
	if hist, _ := svc.BotFeed(ctx, "tenant-a", "alice", nil, true); len(hist) != len(feed) {
		t.Fatalf("history = %d, want %d", len(hist), len(feed))
	}
}
