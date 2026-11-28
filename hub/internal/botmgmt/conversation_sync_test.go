package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func convService(t *testing.T) (*Service, *memSettings) {
	t.Helper()
	settings := &memSettings{}
	seedRecord(t, settings, "t", record{
		Bots: []Bot{
			{ID: "bot_a", Name: "a", InstanceID: "i", OwnerUserID: "alice"},
			{ID: "bot_b", Name: "b", InstanceID: "j", OwnerUserID: "alice"},
			{ID: "bot_c", Name: "c", InstanceID: "k", OwnerUserID: "bob"},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}, {ID: "g2", Scope: ScopeUser, TargetID: "bob"}},
	})
	return NewService(settings), settings
}

func TestEarlierDesktopLinesOmitsTheMessageJustSent(t *testing.T) {
	svc, _ := convService(t)
	ctx := context.Background()
	_, err := svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-1", `{"role":"user","content":"每5分钟向我报告下天气情况"}`),
		convMsg("bot_a", "m-2", `{"role":"assistant","content":"好的，我会定时向你报告天气情况。"}`),
		convMsg("bot_a", "m-3", `{"role":"user","content":"北京，下次再生成pdf文件"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := svc.earlierDesktopLines(ctx, "t", "alice", "bot_a", "北京，下次再生成pdf文件")
	if len(lines) != 2 || !strings.Contains(lines[0], "报告下天气") || !strings.Contains(lines[1], "定时向你报告") {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestConversationAcceptsBotInstanceID(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	if _, err := svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("i", "m-instance", `{"role":"user","content":"instance id"}`),
	}); err != nil {
		t.Fatalf("instance id write: %v", err)
	}
	page, err := svc.ReadConversation(ctx, "t", "alice", "i", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Entries {
		if e.ID == "m-instance" && e.BotID == "bot_a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("instance write was not canonicalized: %+v", page.Entries)
	}
}

func TestConversationStaleBotDoesNotPoisonSyncBatch(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	saved, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_deleted_local", "old-1", `{"role":"user","content":"old"}`),
		convMsg("bot_a", "new-1", `{"role":"user","content":"new"}`),
	})
	if err != nil {
		t.Fatalf("mixed stale/current batch: %v", err)
	}
	if len(saved.Assigned) != 1 || saved.Assigned[0].BotID != "bot_a" || saved.Assigned[0].ID != "new-1" {
		t.Fatalf("assigned %+v", saved.Assigned)
	}
	page, err := svc.ReadConversation(ctx, "t", "alice", "bot_a", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := convIDs(page.Entries); got != "bot_a/new-1@A" {
		t.Fatalf("valid entry missing after stale skip: %s", got)
	}

	// A batch made only of stale local entries is a successful no-op. This
	// lets an old GUI mark that cache as offered instead of retrying it forever.
	if _, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_deleted_local", "old-1", `{"role":"user","content":"old"}`),
	}); err != nil {
		t.Fatalf("stale-only batch: %v", err)
	}
}

func TestConversationForeignBotStillRejectsWholeBatch(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	if _, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "new-1", `{"role":"user","content":"new"}`),
		convMsg("bot_c", "foreign-1", `{"role":"user","content":"foreign"}`),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign mixed batch: %v", err)
	}
	page, err := svc.ReadConversation(ctx, "t", "alice", "bot_a", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("valid write was applied before foreign rejection: %+v", page.Entries)
	}
}

func TestConversationSaveKeepsCreatedSeq(t *testing.T) {
	svc, _ := convService(t)
	ctx := context.Background()
	first, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-1", `{"role":"user","content":"先"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveConversation(ctx, "t", "alice", "B", []ConversationWrite{
		convMsg("bot_a", "m-2", `{"role":"user","content":"后"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Head != 1 || second.Head != 2 || len(first.Assigned) != 1 || len(second.Assigned) != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if first.Assigned[0].CreatedSeq != 1 || second.Assigned[0].CreatedSeq != 2 {
		t.Fatalf("created seq first=%d second=%d", first.Assigned[0].CreatedSeq, second.Assigned[0].CreatedSeq)
	}
	// The same text again takes no new sequence, and still reports the first one.
	again, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-1", `{"role":"user","content":"先"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Head != 2 || len(again.Assigned) != 1 || again.Assigned[0].CreatedSeq != 1 || again.Assigned[0].Seq != 1 {
		t.Fatalf("rewrite %+v", again)
	}
	// An edit keeps the original display sequence.
	edited, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-1", `{"role":"user","content":"先，改过"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Head != 3 || edited.Assigned[0].CreatedSeq != 1 || edited.Assigned[0].Seq != 3 {
		t.Fatalf("edit %+v", edited)
	}
}

func TestConversationOldJournalKeepsDisplayOrder(t *testing.T) {
	ctx := context.Background()
	svc, settings := convService(t)
	raw := `{"head":3,"entries":[` +
		`{"seq":3,"bot_id":"bot_a","id":"m-1","kind":"message","origin":"A","by":"A","data":{"role":"user","content":"xian"},"at":"2026-11-01T00:00:03Z"},` +
		`{"seq":2,"bot_id":"bot_a","id":"m-2","kind":"message","origin":"B","by":"B","data":{"role":"user","content":"hou"},"at":"2026-11-01T00:00:02Z"}` +
		`]}`
	if err := settings.Set(ctx, storageKey("t")+convSettingsTag+"alice", raw); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ReadConversation(ctx, "t", "alice", "bot_a", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, e := range page.Entries {
		got[e.ID] = e.CreatedSeq
	}
	if got["m-1"] != 3 || got["m-2"] != 2 {
		t.Fatalf("backfill %+v", got)
	}
	edited, err := svc.SaveConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-2", `{"role":"user","content":"hou-edited"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edited.Assigned) != 1 || edited.Assigned[0].CreatedSeq != 2 || edited.Assigned[0].Seq == 2 {
		t.Fatalf("edit moved the old line: %+v", edited)
	}
}

func TestConversationPruneKeepsNewestDisplayOrder(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	for i := 0; i < convPerBotKeep; i++ {
		_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{
			convMsg("bot_a", fmt.Sprintf("m-%03d", i), `{"role":"user","content":"x"}`),
		})
	}
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-000", `{"role":"user","content":"edited"}`),
	})
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{
		convMsg("bot_a", "m-new", `{"role":"user","content":"new"}`),
	})
	page, _ := svc.ReadConversation(ctx, "t", "alice", "bot_a", 0, 0, 0)
	ids := map[string]bool{}
	for _, e := range page.Entries {
		if e.Kind == convKindMessage {
			ids[e.ID] = true
		}
	}
	if ids["m-000"] || !ids["m-001"] || !ids["m-new"] || len(ids) != convPerBotKeep {
		t.Fatalf("kept %d m-000=%v m-001=%v m-new=%v", len(ids), ids["m-000"], ids["m-001"], ids["m-new"])
	}
}

func convMsg(bot, id, body string) ConversationWrite {
	return ConversationWrite{BotID: bot, ID: id, Kind: convKindMessage, Data: json.RawMessage(body)}
}

func convIDs(entries []ConversationEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.BotID+"/"+e.ID+"@"+e.Origin)
	}
	return strings.Join(parts, ",")
}

// Two clients of one person: what A writes shows on B once, in order, with
// A as its origin, and B's cursor then sees nothing new.
func TestConversationTwoClientsSeeOneTranscript(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	if _, err := svc.WriteConversation(ctx, "t", "alice", "client-A", []ConversationWrite{
		convMsg("bot_a", "m-1", `{"role":"user","content":"发一篇文章"}`),
		convMsg("bot_a", "m-2", `{"role":"assistant","content":"好的，我来发。","ack":true}`),
	}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ReadConversation(ctx, "t", "alice", "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := convIDs(page.Entries); got != "bot_a/m-1@client-A,bot_a/m-2@client-A" || page.Next != 2 || page.Head != 2 {
		t.Fatalf("B backfill = %s next=%d head=%d", got, page.Next, page.Head)
	}
	again, _ := svc.ReadConversation(ctx, "t", "alice", "", page.Next, 0, 0)
	if len(again.Entries) != 0 || again.Next != 2 {
		t.Fatalf("second read must be empty: %+v", again)
	}
	// The reply settles the ack's turn; the card is answered on B. A's
	// cursor sees the reply once and the answered flag once.
	if _, err := svc.WriteConversation(ctx, "t", "alice", "client-A", []ConversationWrite{
		convMsg("bot_a", "m-3", `{"role":"assistant","content":"要发到哪个账号？","askUser":{"question":"要发到哪个账号？","options":["A","B"]},"runId":"run_1","taskOutcome":"awaiting_user"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteConversation(ctx, "t", "alice", "client-B", []ConversationWrite{
		{BotID: "bot_a", ID: "m-3", Kind: convKindMessage, Patch: true, Data: json.RawMessage(`{"askResolved":true,"content":"ignored"}`)},
		convMsg("bot_a", "m-4", `{"role":"user","content":"A"}`),
	}); err != nil {
		t.Fatal(err)
	}
	fromA, _ := svc.ReadConversation(ctx, "t", "alice", "", 2, 0, 0)
	if got := convIDs(fromA.Entries); got != "bot_a/m-3@client-A,bot_a/m-4@client-B" {
		t.Fatalf("A sees = %s", got)
	}
	var card map[string]any
	_ = json.Unmarshal(fromA.Entries[0].Data, &card)
	if fromA.Entries[0].By != "client-B" || fromA.Entries[1].By != "client-B" {
		t.Fatalf("by = %+v", fromA.Entries)
	}
	if card["askResolved"] != true || card["content"] != "要发到哪个账号？" || card["taskOutcome"] != "awaiting_user" {
		t.Fatalf("patched card = %v", card)
	}
	if fromA.Entries[0].CreatedSeq != 3 {
		t.Fatalf("patched card keeps its place: %+v", fromA.Entries[0])
	}
}

// Resending the same entry (a client restart, a retried POST) takes no new
// seq; a real change does, and keeps the first writer as origin.
func TestConversationDedupAndUpdate(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	w := convMsg("bot_a", "m-1", `{"role":"assistant","content":"好的","ack":true}`)
	h1, _ := svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{w})
	h2, _ := svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-1", `{"ack":true, "content":"好的","role":"assistant"}`)})
	if h1 != 1 || h2 != 1 {
		t.Fatalf("resend bumped seq: %d %d", h1, h2)
	}
	h3, _ := svc.WriteConversation(ctx, "t", "alice", "B", []ConversationWrite{convMsg("bot_a", "m-1", `{"role":"assistant","content":"好的，正在写稿。","ack":true}`)})
	page, _ := svc.ReadConversation(ctx, "t", "alice", "", 1, 0, 0)
	if h3 != 2 || len(page.Entries) != 1 || page.Entries[0].Origin != "A" || page.Entries[0].CreatedSeq != 1 {
		t.Fatalf("update: head=%d %+v", h3, page.Entries)
	}
	// A patch that turns nothing on is not a change.
	h4, _ := svc.WriteConversation(ctx, "t", "alice", "B", []ConversationWrite{{BotID: "bot_a", ID: "m-1", Patch: true, Data: json.RawMessage(`{"ack":true}`)}})
	h5, _ := svc.WriteConversation(ctx, "t", "alice", "B", []ConversationWrite{{BotID: "bot_a", ID: "missing", Patch: true, Data: json.RawMessage(`{"askResolved":true}`)}})
	if h4 != 2 || h5 != 2 {
		t.Fatalf("no-op patch bumped: %d %d", h4, h5)
	}
}

// Another person and another person's bot stay out; a bot filter narrows.
func TestConversationIsolation(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	if _, err := svc.WriteConversation(ctx, "t", "bob", "X", []ConversationWrite{convMsg("bot_a", "m-1", `{"role":"user","content":"x"}`)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob writing alice's bot: %v", err)
	}
	if _, err := svc.ReadConversation(ctx, "t", "bob", "bot_a", 0, 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob reading alice's bot: %v", err)
	}
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-1", `{"role":"user","content":"a"}`), convMsg("bot_b", "m-2", `{"role":"user","content":"b"}`)})
	_, _ = svc.WriteConversation(ctx, "t", "bob", "C", []ConversationWrite{convMsg("bot_c", "m-1", `{"role":"user","content":"c"}`)})
	bob, _ := svc.ReadConversation(ctx, "t", "bob", "", 0, 0, 0)
	if got := convIDs(bob.Entries); got != "bot_c/m-1@C" || bob.Head != 1 {
		t.Fatalf("bob sees %s head=%d", got, bob.Head)
	}
	onlyB, _ := svc.ReadConversation(ctx, "t", "alice", "bot_b", 0, 0, 0)
	if got := convIDs(onlyB.Entries); got != "bot_b/m-2@A" {
		t.Fatalf("bot filter = %s", got)
	}
	if _, err := svc.ReadConversation(ctx, "other", "alice", "", 0, 0, 0); err == nil {
		// another tenant has no grant for alice
		page, _ := svc.ReadConversation(ctx, "other", "alice", "", 0, 0, 0)
		if len(page.Entries) != 0 {
			t.Fatalf("tenant leak: %+v", page.Entries)
		}
	}
}

// A Hub restart keeps the journal and its seq; a client whose cursor is past
// the head (journal lost) is told to start over.
func TestConversationSurvivesRestartAndResets(t *testing.T) {
	ctx := context.Background()
	svc, settings := convService(t)
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-1", `{"role":"user","content":"a"}`)})
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{{BotID: "bot_a", ID: "req-1", Kind: convKindActivity, Data: json.RawMessage(`{"activity":"正在写稿"}`)}})
	again := NewService(settings)
	page, err := again.ReadConversation(ctx, "t", "alice", "", 0, 0, 0)
	if err != nil || convIDs(page.Entries) != "bot_a/m-1@A" || page.Head != 1+convHeadGap {
		t.Fatalf("after restart: %v %s head=%d (activity is live only)", err, convIDs(page.Entries), page.Head)
	}
	h, _ := again.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-2", `{"role":"user","content":"b"}`)})
	if h <= 2 {
		t.Fatalf("seq went back after restart: %d", h)
	}
	// A client that read the activity (seq 2) before the restart still gets m-2.
	after, _ := again.ReadConversation(ctx, "t", "alice", "", 2, 0, 0)
	if convIDs(after.Entries) != "bot_a/m-2@A" {
		t.Fatalf("cursor 2 after restart: %s", convIDs(after.Entries))
	}
	reset, _ := again.ReadConversation(ctx, "t", "alice", "", 99999, 0, 0)
	if !reset.Reset || len(reset.Entries) != 2 {
		t.Fatalf("cursor past head must reset and backfill: %+v", reset)
	}
}

// A waiting read returns as soon as another client writes.
func TestConversationLongPollWakes(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	done := make(chan ConversationPage, 1)
	go func() {
		page, _ := svc.ReadConversation(ctx, "t", "alice", "", 0, 0, 10*time.Second)
		done <- page
	}()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-1", `{"role":"user","content":"a"}`)})
	select {
	case page := <-done:
		if convIDs(page.Entries) != "bot_a/m-1@A" || time.Since(start) > 2*time.Second {
			t.Fatalf("woke with %+v", page)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long poll did not wake")
	}
	// Bob's write does not wake or feed alice.
	quiet, _ := svc.ReadConversation(ctx, "t", "alice", "", 1, 0, 200*time.Millisecond)
	if len(quiet.Entries) != 0 {
		t.Fatalf("idle read: %+v", quiet)
	}
}

// Bad writes are refused whole; the window keeps the newest per bot.
func TestConversationLimits(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	for _, w := range []ConversationWrite{
		{BotID: "bot_a", ID: "", Data: json.RawMessage(`{}`)},
		{BotID: "bot_a", ID: "x", Data: json.RawMessage(`[1]`)},
		{BotID: "bot_a", ID: "x", Kind: "nope", Data: json.RawMessage(`{}`)},
		{BotID: "bot_a", ID: "x", Data: json.RawMessage(`{"c":"` + strings.Repeat("x", convDataMax) + `"}`)},
	} {
		if _, err := svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{w}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%+v accepted: %v", w.ID, err)
		}
	}
	for i := 0; i < convPerBotKeep+5; i++ {
		_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", fmt.Sprintf("m-%03d", i), `{"role":"user","content":"x"}`)})
	}
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_b", "keep", `{"role":"user","content":"x"}`)})
	page, _ := svc.ReadConversation(ctx, "t", "alice", "", 0, 0, 0)
	count := map[string]int{}
	for _, e := range page.Entries {
		count[e.BotID]++
	}
	if count["bot_a"] != convPerBotKeep || count["bot_b"] != 1 || page.Entries[0].ID != "m-005" {
		t.Fatalf("window: %v first=%s", count, page.Entries[0].ID)
	}
	small, _ := svc.ReadConversation(ctx, "t", "alice", "", 0, 3, 0)
	if len(small.Entries) != 3 || !small.More || small.Next != small.Entries[2].Seq {
		t.Fatalf("paging: %+v", small)
	}
}

// Run states do not push chat lines out of a bot's window.
func TestConversationRunStatesKeepTheirOwnWindow(t *testing.T) {
	ctx := context.Background()
	svc, _ := convService(t)
	_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{convMsg("bot_a", "m-keep", `{"role":"user","content":"x"}`)})
	for i := 0; i < convPerBotKeep+10; i++ {
		_, _ = svc.WriteConversation(ctx, "t", "alice", "A", []ConversationWrite{{BotID: "bot_a", ID: fmt.Sprintf("run:%d", i), Kind: convKindRun, Data: json.RawMessage(`{"state":"done"}`)}})
	}
	page, _ := svc.ReadConversation(ctx, "t", "alice", "", 0, 0, 0)
	runs := 0
	for _, e := range page.Entries {
		if e.Kind == convKindRun {
			runs++
		}
	}
	if page.Entries[0].ID != "m-keep" || runs != convPerBotState {
		t.Fatalf("first=%s runs=%d", page.Entries[0].ID, runs)
	}
}
