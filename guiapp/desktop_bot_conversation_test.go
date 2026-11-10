package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeConvHub is the Hub journal as two GUIs of one person see it: one
// sequence per change, upsert by bot+kind+id, origin kept from the first write.
type fakeConvHub struct {
	mu      sync.Mutex
	head    int64
	entries []desktopBotConversationEntry
	posts   int
	missing bool
}

func (h *fakeConvHub) exchange(t *testing.T) func(context.Context, string, string, any, any, time.Duration, map[string]string) (int, error) {
	return func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.missing {
			return http.StatusNotFound, nil
		}
		switch {
		case method == http.MethodPost && path == "/api/v1/bots/conversation":
			h.posts++
			raw, _ := json.Marshal(body)
			var in struct {
				ClientID string                        `json:"client_id"`
				Entries  []DesktopBotConversationWrite `json:"entries"`
			}
			_ = json.Unmarshal(raw, &in)
			for _, w := range in.Entries {
				found := -1
				for i, e := range h.entries {
					if e.BotID == w.BotID && e.ID == w.ID && e.Kind == w.Kind {
						found = i
					}
				}
				h.head++
				e := desktopBotConversationEntry{Seq: h.head, CreatedSeq: h.head, BotID: w.BotID, ID: w.ID, Kind: w.Kind, Origin: in.ClientID, By: in.ClientID, Data: w.Data, At: time.Now().UTC().Format(time.RFC3339Nano)}
				if found >= 0 {
					e.CreatedSeq, e.Origin = h.entries[found].CreatedSeq, h.entries[found].Origin
					h.entries = append(h.entries[:found], h.entries[found+1:]...)
				}
				h.entries = append(h.entries, e)
			}
			return http.StatusOK, nil
		case method == http.MethodGet && strings.HasPrefix(path, "/api/v1/bots/conversation?"):
			q := path[strings.Index(path, "?")+1:]
			after := int64(0)
			for _, part := range strings.Split(q, "&") {
				if v, ok := strings.CutPrefix(part, "after="); ok {
					after, _ = strconv.ParseInt(v, 10, 64)
				}
			}
			page := desktopBotConversationPage{Head: h.head, Next: after, Entries: []desktopBotConversationEntry{}}
			for _, e := range h.entries {
				if e.Seq > after {
					page.Entries = append(page.Entries, e)
					page.Next = e.Seq
				}
			}
			raw, _ := json.Marshal(page)
			_ = json.Unmarshal(raw, dest)
			return http.StatusOK, nil
		}
		t.Errorf("unexpected hub call %s %s", method, path)
		return http.StatusNotFound, nil
	}
}

// writeAs posts entries as another client (client B's process).
func (h *fakeConvHub) writeAs(origin string, entries ...DesktopBotConversationWrite) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, w := range entries {
		h.head++
		h.entries = append(h.entries, desktopBotConversationEntry{Seq: h.head, CreatedSeq: h.head, BotID: w.BotID, ID: w.ID, Kind: w.Kind, Origin: origin, By: origin, Data: w.Data, At: time.Now().UTC().Format(time.RFC3339Nano)})
	}
}

type convEvents struct {
	mu   sync.Mutex
	list []string
	sync []map[string]any
}

func setupConvTest(t *testing.T, account string) (*fakeConvHub, *convEvents) {
	t.Helper()
	hub := &fakeConvHub{}
	events := &convEvents{}
	prevUser := shareAccessRemoteUserOverride
	shareAccessRemoteUserOverride = account
	desktopBotExchangeOverride = hub.exchange(t)
	desktopConvTestPublish = true
	desktopConvGo = func(f func()) { f() }
	desktopConvEmitSink = func(event string, payload map[string]any) {
		events.mu.Lock()
		defer events.mu.Unlock()
		raw, _ := json.Marshal(payload)
		events.list = append(events.list, event+" "+string(raw))
		if event == "desktop-bot-sync" {
			events.sync = append(events.sync, payload)
		}
	}
	var busy []string
	desktopBotBusySink = func(botID string, on bool) { busy = append(busy, botID+":"+strconv.FormatBool(on)) }
	desktopConv = &desktopConvSyncer{kick: make(chan struct{}, 1), remote: map[string]*desktopConvRemoteRun{}, lastAct: map[string]string{}}
	t.Cleanup(func() {
		shareAccessRemoteUserOverride = prevUser
		desktopBotExchangeOverride = nil
		desktopConvTestPublish = false
		desktopConvGo = func(f func()) { go f() }
		desktopConvEmitSink = nil
		desktopBotBusySink = nil
		desktopConv = &desktopConvSyncer{kick: make(chan struct{}, 1), remote: map[string]*desktopConvRemoteRun{}, lastAct: map[string]string{}}
		desktopBotBusyMu.Lock()
		desktopBotBusyCount = map[string]int{}
		desktopBotBusyMu.Unlock()
	})
	return hub, events
}

func convWrite(bot, id, kind, data string) DesktopBotConversationWrite {
	return DesktopBotConversationWrite{BotID: bot, ID: id, Kind: kind, Data: json.RawMessage(data)}
}

func syncedIDs(events *convEvents) []string {
	events.mu.Lock()
	defer events.mu.Unlock()
	var out []string
	for _, p := range events.sync {
		list, _ := p["entries"].([]map[string]any)
		for _, e := range list {
			out = append(out, e["bot_id"].(string)+"/"+e["id"].(string))
		}
	}
	return out
}

// A message another client wrote reaches this page once; this client's own
// writes do not come back; a second read with the same cursor is empty.
func TestDesktopConvOtherClientMessageArrivesOnce(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	a := &App{}
	if err := a.PublishDesktopBotConversation("alice", []DesktopBotConversationWrite{convWrite("bot_1", "m-own", "", `{"role":"user","content":"我发的"}`)}); err != nil {
		t.Fatal(err)
	}
	hub.writeAs("client-B",
		convWrite("bot_1", "m-b1", "message", `{"role":"user","content":"B 发的"}`),
		convWrite("bot_1", "m-b2", "message", `{"role":"assistant","content":"好的，我来。","ack":true}`),
	)
	if _, err := a.desktopConvSyncOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(syncedIDs(events), ","); got != "bot_1/m-b1,bot_1/m-b2" {
		t.Fatalf("synced %s", got)
	}
	if _, err := a.desktopConvSyncOnce(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(syncedIDs(events), ","); got != "bot_1/m-b1,bot_1/m-b2" {
		t.Fatalf("second read repeated: %s", got)
	}
	// The page reloaded: the next read starts over, so it can rebuild.
	desktopConv.reset = true
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	if got := strings.Join(syncedIDs(events), ","); got != "bot_1/m-b1,bot_1/m-b2,bot_1/m-b1,bot_1/m-b2" {
		t.Fatalf("backfill after reload: %s", got)
	}
}

// Only the signed-in account's transcript is written.
func TestDesktopConvPublishOnlyForSignedInAccount(t *testing.T) {
	hub, _ := setupConvTest(t, "alice")
	a := &App{}
	_ = a.PublishDesktopBotConversation("bob", []DesktopBotConversationWrite{convWrite("bot_1", "m-1", "", `{"role":"user"}`)})
	_ = a.PublishDesktopBotConversation("local", []DesktopBotConversationWrite{convWrite("bot_1", "m-1", "", `{"role":"user"}`)})
	if hub.posts != 0 {
		t.Fatalf("another account's transcript went out: %d posts", hub.posts)
	}
	many := make([]DesktopBotConversationWrite, 0, 120)
	for i := 0; i < 120; i++ {
		many = append(many, convWrite("bot_1", "m-"+strconv.Itoa(i), "", `{"role":"user"}`))
	}
	if err := a.PublishDesktopBotConversation("alice", many); err != nil || hub.posts != 3 || hub.head != 120 {
		t.Fatalf("batches: posts=%d head=%d err=%v", hub.posts, hub.head, err)
	}
}

// Another client's run shows this bot busy and its step, and lets go when
// it ends; this process's own run states are not replayed to it.
func TestDesktopConvRemoteRunProgress(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	var busy []string
	desktopBotBusySink = func(botID string, on bool) { busy = append(busy, botID+":"+strconv.FormatBool(on)) }
	a := &App{}
	a.publishDesktopRunState("bot_1", "req-own", "", "running")
	hub.writeAs("client-B",
		convWrite("bot_1", "run:req-b", "run", `{"state":"running","request_id":"req-b"}`),
		convWrite("bot_1", "act:run_b", "activity", `{"run_id":"run_b","activity":"正在输入"}`),
	)
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	hub.writeAs("client-B", convWrite("bot_1", "run:req-b", "run", `{"state":"running","request_id":"req-b"}`))
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	hub.writeAs("client-B", convWrite("bot_1", "run:req-b", "run", `{"state":"done","run_id":"run_b","request_id":"req-b"}`))
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	if strings.Join(busy, ",") != "bot_1:true,bot_1:false" {
		t.Fatalf("busy marks = %v", busy)
	}
	joined := strings.Join(events.list, "\n")
	if !strings.Contains(joined, `desktop-bot-activity {"activity":"正在输入","bot_id":"bot_1","run_id":"run_b"}`) ||
		!strings.Contains(joined, `desktop-bot-activity {"activity":"","bot_id":"bot_1","run_id":"run_b"}`) {
		t.Fatalf("activity events:\n%s", joined)
	}
	if len(syncedIDs(events)) != 0 {
		t.Fatalf("run entries are not chat bubbles: %v", syncedIDs(events))
	}
	// A run state older than the stale window is not shown as busy.
	busy = nil
	hub.mu.Lock()
	hub.head++
	hub.entries = append(hub.entries, desktopBotConversationEntry{Seq: hub.head, CreatedSeq: hub.head, BotID: "bot_2", ID: "run:req-old", Kind: "run", Origin: "client-B", Data: json.RawMessage(`{"state":"running"}`), At: time.Now().Add(-2 * desktopConvRemoteStale).UTC().Format(time.RFC3339Nano)})
	hub.mu.Unlock()
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	if len(busy) != 0 {
		t.Fatalf("stale run marked busy: %v", busy)
	}
}

// The other client's bubble for a run this process is still relaying is
// left out: this process shows its own reply for it.
func TestDesktopConvSkipsCopyOfOwnLiveRun(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	a := &App{}
	run := a.registerActiveDesktopRun("bot_1", "req-1", "alice:bot_1", "写稿", "execute")
	a.setActiveDesktopRunID("bot_1", "req-1", "run_live")
	t.Cleanup(func() { unregisterActiveDesktopRun("bot_1", "req-1", run.RunID) })
	hub.writeAs("client-B",
		convWrite("bot_1", "m-d", "message", `{"role":"assistant","content":"写好了","runId":"run_live","requestId":"desktop-bot-deliver-run_live"}`),
		convWrite("bot_1", "m-other", "message", `{"role":"assistant","content":"另一件","runId":"run_other"}`),
	)
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	if got := strings.Join(syncedIDs(events), ","); got != "bot_1/m-other" {
		t.Fatalf("synced %s", got)
	}
}

// An older Hub has no journal: the loop is told to wait, nothing breaks.
func TestDesktopConvOldHub(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	hub.missing = true
	a := &App{}
	if _, err := a.desktopConvSyncOnce(context.Background(), 0); err != errDesktopConvMissing {
		t.Fatalf("err = %v", err)
	}
	if err := a.PublishDesktopBotConversation("alice", []DesktopBotConversationWrite{convWrite("bot_1", "m-1", "", `{"role":"user"}`)}); err != nil {
		t.Fatalf("publish to old hub: %v", err)
	}
	if len(events.list) != 0 {
		t.Fatalf("events: %v", events.list)
	}
}

// Switching account starts the cursor over and drops the old busy marks.
func TestDesktopConvAccountSwitchResetsCursor(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	a := &App{}
	hub.writeAs("client-B", convWrite("bot_1", "m-1", "message", `{"role":"user","content":"x"}`))
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	shareAccessRemoteUserOverride = "carol"
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.sync) != 2 || events.sync[1]["owner_id"] != "carol" {
		t.Fatalf("after switch: %v", events.sync)
	}
}

// Another client answering this client's card comes back here; this
// client's own unchanged writes do not.
func TestDesktopConvPatchOnOwnMessageArrives(t *testing.T) {
	hub, events := setupConvTest(t, "alice")
	a := &App{}
	self := desktopBotClientID()
	hub.mu.Lock()
	hub.head += 2
	hub.entries = append(hub.entries,
		desktopBotConversationEntry{Seq: 1, CreatedSeq: 1, BotID: "bot_1", ID: "m-mine", Kind: "message", Origin: self, By: self, Data: json.RawMessage(`{"role":"user"}`)},
		desktopBotConversationEntry{Seq: 2, CreatedSeq: 1, BotID: "bot_1", ID: "m-card", Kind: "message", Origin: self, By: "client-B", Data: json.RawMessage(`{"role":"assistant","askResolved":true}`)},
	)
	hub.mu.Unlock()
	_, _ = a.desktopConvSyncOnce(context.Background(), 0)
	if got := strings.Join(syncedIDs(events), ","); got != "bot_1/m-card" {
		t.Fatalf("synced %s", got)
	}
}
