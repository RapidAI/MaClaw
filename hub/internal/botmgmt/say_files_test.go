package botmgmt

import (
	"context"
	"testing"
)

func TestListSaidOmitsADeletedBot(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	if _, err := svc.Say(ctx, "tenant-a", "alice", "bot_a", "", "早上的训练报告", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Say(ctx, "tenant-a", "alice", "bot_main", "", "主 bot 还在", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "写稿"}); err != nil {
		t.Fatal(err)
	}
	// A delete from before this fix left the say in the log and removed the bot.
	svc.mu.Lock()
	rec, err := svc.load(ctx, "tenant-a")
	if err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	kept := make([]Bot, 0, len(rec.Bots))
	for _, bot := range rec.Bots {
		if bot.ID != "bot_a" {
			kept = append(kept, bot)
		}
	}
	rec.Bots = kept
	if err := svc.save(ctx, "tenant-a", rec); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()
	said, err := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(said) != 1 || said[0].From != "bot_main" || said[0].Text != "主 bot 还在" {
		t.Fatalf("said = %+v", said)
	}
	// Listing does not rewrite the record. The task aimed at the deleted id
	// is still stored until the next note is saved.
	svc.mu.Lock()
	rec, err = svc.load(ctx, "tenant-a")
	svc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	delegates := 0
	for _, n := range rec.CrossNotes {
		if n.Kind == "delegate" && n.Text == "写稿" {
			delegates++
		}
	}
	if delegates != 1 {
		t.Fatalf("list rewrote the task: %+v", rec.CrossNotes)
	}
	// The mnemonic "A" can be reused. The new id does not receive bot_a's say.
	svc.mu.Lock()
	rec, err = svc.load(ctx, "tenant-a")
	if err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	rec.Bots = append(rec.Bots, Bot{ID: "bot_a_new", Name: "A", OwnerUserID: "alice", InstanceID: "inst_a_new", CreatedAt: "2026-11-14T12:00:00Z"})
	if err := svc.save(ctx, "tenant-a", rec); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()
	if _, err := svc.Say(ctx, "tenant-a", "alice", "bot_a_new", "", "新的 A", "new"); err != nil {
		t.Fatal(err)
	}
	said, err = svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(said) != 2 || said[0].From != "bot_main" || said[1].From != "bot_a_new" || said[1].Text != "新的 A" {
		t.Fatalf("same name, new id said = %+v", said)
	}
	svc.mu.Lock()
	rec, err = svc.load(ctx, "tenant-a")
	svc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	delegates = 0
	for _, n := range rec.CrossNotes {
		if n.Kind == "delegate" && n.Text == "写稿" {
			delegates++
		}
	}
	if delegates != 0 {
		t.Fatalf("task for the deleted id still stored: %+v", rec.CrossNotes)
	}

	// Put the bot back, say again, and delete through the real path.
	svc.mu.Lock()
	rec, err = svc.load(ctx, "tenant-a")
	if err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	rec.Bots = append(rec.Bots, Bot{ID: "bot_a", Name: "A", OwnerUserID: "alice", InstanceID: "inst_a", CreatedAt: "2026-02-01T00:00:00Z"})
	rec.Rooms = map[string]*BotRoom{
		"r1": {ID: "r1", Owner: "alice", Name: "群", Members: []string{"bot_main", "bot_a"}, Attention: map[string]string{"bot_a": "all"}},
		"r2": {ID: "r2", Owner: "alice", Name: "只剩它", Members: []string{"bot_a"}},
		"rb": {ID: "rb", Owner: "bob", Name: "别人", Members: []string{"bot_a"}},
	}
	if err := svc.save(ctx, "tenant-a", rec); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()
	if err := svc.DeleteBot(ctx, "tenant-a", "bot_a"); err != nil {
		t.Fatal(err)
	}
	said, err = svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(said) != 2 || said[0].From != "bot_main" || said[1].From != "bot_a_new" {
		t.Fatalf("after delete said = %+v", said)
	}
	svc.mu.Lock()
	rec, err = svc.load(ctx, "tenant-a")
	svc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rec.CrossNotes {
		if n.From == "bot_a" && n.Kind == "say" {
			t.Fatalf("say still stored: %+v", n)
		}
	}
	if rec.Rooms["r1"] == nil || len(rec.Rooms["r1"].Members) != 1 || rec.Rooms["r1"].Members[0] != "bot_main" || rec.Rooms["r1"].Attention["bot_a"] != "" {
		t.Fatalf("room r1 = %+v", rec.Rooms["r1"])
	}
	if rec.Rooms["r2"] != nil {
		t.Fatalf("empty room kept: %+v", rec.Rooms["r2"])
	}
	if rec.Rooms["rb"] == nil || len(rec.Rooms["rb"].Members) != 1 {
		t.Fatalf("other owner's room = %+v", rec.Rooms["rb"])
	}
}

func TestDeletedBotSayDoesNotBlockTheNewSpeaker(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	if _, err := svc.Say(ctx, "tenant-a", "alice", "bot_a", "", "早上的训练报告", "old"); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	rec, err := svc.load(ctx, "tenant-a")
	if err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	kept := make([]Bot, 0, len(rec.Bots))
	for _, bot := range rec.Bots {
		if bot.ID != "bot_a" {
			kept = append(kept, bot)
		}
	}
	rec.Bots = kept
	if err := svc.save(ctx, "tenant-a", rec); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()
	note, err := svc.Say(ctx, "tenant-a", "alice", "bot_main", "", "早上的训练报告", "new")
	if err != nil || note.From != "bot_main" {
		t.Fatalf("note = %+v %v", note, err)
	}
}

func TestGoneBotIsNotATaskOrARoomMember(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	if _, err := svc.PostBotDelegateFromInstance(ctx, "tenant-a", "alice", "inst_main", BotCrossNote{Target: "A", Text: "写稿"}); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	rec, err := svc.load(ctx, "tenant-a")
	if err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	kept := make([]Bot, 0, len(rec.Bots))
	for _, bot := range rec.Bots {
		if bot.ID != "bot_a" {
			kept = append(kept, bot)
		}
	}
	rec.Bots = kept
	rec.Rooms = map[string]*BotRoom{
		"r1": {ID: "r1", Owner: "alice", Name: "群", Members: []string{"bot_main", "bot_a"}, CreatedAt: "2026-11-14T00:00:00Z"},
		"r2": {ID: "r2", Owner: "alice", Name: "只剩它", Members: []string{"bot_a"}, CreatedAt: "2026-11-14T00:00:01Z"},
	}
	if err := svc.save(ctx, "tenant-a", rec); err != nil {
		svc.mu.Unlock()
		t.Fatal(err)
	}
	svc.mu.Unlock()
	as, err := svc.ListAssignments(ctx, "tenant-a", "alice")
	if err != nil || len(as) != 0 {
		t.Fatalf("assignments = %+v %v", as, err)
	}
	rooms, err := svc.ListRooms(ctx, "tenant-a", "alice")
	if err != nil || len(rooms) != 1 || len(rooms[0].Members) != 1 || rooms[0].Members[0] != "bot_main" {
		t.Fatalf("rooms = %+v %v", rooms, err)
	}
}

func TestCapDropsSaysOfADeletedBotBeforeLiveNotes(t *testing.T) {
	rec := record{Bots: []Bot{{ID: "bot_main", OwnerUserID: "alice"}}}
	notes := []BotCrossNote{
		{Owner: "alice", From: "bot_main", Target: "bot_main", Kind: "delegate", Text: "旧任务", CreatedAt: "2026-11-01T00:00:00Z"},
		{Owner: "alice", From: "bot_main", Target: "bot_gone", Kind: "delegate", Text: "发给已删除的", CreatedAt: "2026-11-02T00:00:00Z"},
	}
	for i := 0; i < desktopCrossNoteCap; i++ {
		notes = append(notes, BotCrossNote{Owner: "alice", From: "bot_gone", Kind: "say", Text: "早上的训练报告", CreatedAt: "2026-11-14T00:00:00Z"})
	}
	out := capCrossNotes(notes, rec, "alice")
	if len(out) != 1 || out[0].Kind != "delegate" || out[0].Text != "旧任务" {
		t.Fatalf("out = %+v", out)
	}
}

// A server-started turn's files ride on its said note, are listed with it,
// and can be read back by the person's GUI.
func TestSayKeepsTheSpeechKind(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	note, err := svc.SayFilesKindFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "后台任务结束了，下面是最后的输出\ntrain=1", "k-process", nil, "process")
	if err != nil || note.MessageKind != "process" {
		t.Fatalf("note %+v %v", note, err)
	}
	said, err := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range said {
		if n.ID == note.ID && n.MessageKind == "process" && n.Kind == "say" {
			found = true
		}
	}
	if !found {
		t.Fatalf("said %+v", said)
	}
	plain, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "结果是 F1 0.81，建议停在这一轮。", "k-outcome")
	if err != nil || plain.MessageKind != "" {
		t.Fatalf("untagged say %+v %v", plain, err)
	}
	outcome, err := svc.SayFilesKindFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "做成了。\nF1 0.91", "k-outcome-kind", nil, "outcome")
	if err != nil || outcome.MessageKind != "outcome" {
		t.Fatalf("outcome note %+v %v", outcome, err)
	}
	said, err = svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	var outcomeKept bool
	for _, n := range said {
		if n.ID == outcome.ID && n.MessageKind == "outcome" {
			outcomeKept = true
		}
	}
	if !outcomeKept {
		t.Fatalf("outcome kind was not persisted: %+v", said)
	}
}

func TestSayFilesFromInstanceCarriesReadableFiles(t *testing.T) {
	svc := assignFixture(t)
	svc.MailFileDir = t.TempDir()
	ctx := context.Background()
	f, err := svc.StoreBotMailFile("tenant-a", "REPORT.md", "text/markdown", []byte("# report\nF1 0.79"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.SayFilesFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "训练结束，报告见附件。", "k1", []BotMailFile{f})
	if err != nil || len(note.Files) != 1 {
		t.Fatalf("note = %+v %v", note, err)
	}
	said, err := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range said {
		if n.ID == note.ID && len(n.Files) == 1 && n.Files[0].Name == "REPORT.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("said = %+v", said)
	}
	got, data, err := svc.ReadBotMailFile(ctx, "tenant-a", "alice", "", note.ID, "REPORT.md")
	if err != nil || got.Name != "REPORT.md" || string(data) != "# report\nF1 0.79" {
		t.Fatalf("read = %+v %q %v", got, data, err)
	}
}
