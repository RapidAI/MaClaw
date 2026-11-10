package guiapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Multi-client conversation sync.
//
// Each GUI keeps its own transcript of a bot chat. Without this, a second
// GUI of the same person saw none of it: the person's message, the instant
// acknowledgement, the reply, an ask card and its answer, the run's progress
// all lived only where they happened, and the finished-run delivery is
// consumed by the first GUI that acks it. Hub now keeps one journal per
// person (GET/POST /api/v1/bots/conversation, a sequence per change). This
// process writes what its page shows and replays what other clients wrote.
//
// Entry kinds:
//   message  one chat bubble, keyed by its message id; the page merges it.
//   run      a turn this process is relaying (running / done), so another
//            client shows the bot as busy while the desktop is in use.
//   activity the step the run is in, shown on the working row.
//
// A Hub without the route answers 404; the loop then waits and tries again
// later, and nothing else changes for that Hub.

// DesktopBotConversationWrite is one entry the page publishes.
type DesktopBotConversationWrite struct {
	BotID string          `json:"bot_id"`
	ID    string          `json:"id"`
	Kind  string          `json:"kind,omitempty"`
	Patch bool            `json:"patch,omitempty"`
	Data  json.RawMessage `json:"data"`
}

type desktopBotConversationEntry struct {
	Seq        int64           `json:"seq"`
	CreatedSeq int64           `json:"created_seq"`
	BotID      string          `json:"bot_id"`
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Origin     string          `json:"origin,omitempty"`
	By         string          `json:"by,omitempty"`
	Data       json.RawMessage `json:"data"`
	At         string          `json:"at"`
}

type desktopBotConversationPage struct {
	Entries []desktopBotConversationEntry `json:"entries"`
	Next    int64                         `json:"next"`
	Head    int64                         `json:"head"`
	Reset   bool                          `json:"reset"`
	More    bool                          `json:"more"`
}

const (
	desktopConvWaitSeconds = 20
	desktopConvRemoteStale = 35 * time.Minute
	desktopConvBatch       = 50
)

var (
	desktopConvClientOnce sync.Once
	desktopConvClient     string

	// Tests shorten these.
	desktopConvRetryMin    = 3 * time.Second
	desktopConvRetryMax    = 30 * time.Second
	desktopConvMissingWait = 5 * time.Minute
	// desktopConvTestPublish lets a test that replaces the Hub exchange see
	// the run and activity writes. Other tests that replace it never asked
	// for them, so they stay off.
	desktopConvTestPublish = false
)

// desktopConvGo runs a background write; tests run it inline.
var desktopConvGo = func(f func()) { go f() }

func desktopConvAutoPublish() bool {
	if desktopBotExchangeOverride != nil || desktopBotRelayOverride != nil {
		return desktopConvTestPublish && desktopBotExchangeOverride != nil
	}
	return true
}

// desktopBotClientID names this process to Hub. It is new per start, so two
// windows on one machine are two clients too; a restarted client reads its
// own earlier lines back as already known ids, which the page ignores.
func desktopBotClientID() string {
	desktopConvClientOnce.Do(func() {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			desktopConvClient = "gui-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			return
		}
		desktopConvClient = "gui-" + hex.EncodeToString(b[:])
	})
	return desktopConvClient
}

type desktopConvRemoteRun struct {
	botID string
	owner string
	seen  time.Time
}

type desktopConvSyncer struct {
	mu      sync.Mutex
	started bool
	reset   bool
	kick    chan struct{}
	cursor  int64
	account string
	remote  map[string]*desktopConvRemoteRun // origin \x00 bot \x00 id
	lastAct map[string]string
}

var desktopConv = &desktopConvSyncer{kick: make(chan struct{}, 1), remote: map[string]*desktopConvRemoteRun{}, lastAct: map[string]string{}}

// desktopConvEmitSink is set by tests to read what the page would be sent.
var desktopConvEmitSink func(event string, payload map[string]any)

func (a *App) emitDesktopConv(event string, payload map[string]any) {
	if desktopConvEmitSink != nil {
		desktopConvEmitSink(event, payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	a.emitEvent(event, string(raw))
}

// StartDesktopBotConversationSync starts reading the journal and returns this
// client's id. A second call (the page reloaded) reads from the start again,
// so the page rebuilds what it may have missed.
func (a *App) StartDesktopBotConversationSync() string {
	id := desktopBotClientID()
	if a == nil {
		return id
	}
	s := desktopConv
	s.mu.Lock()
	if s.started {
		s.reset = true
		s.mu.Unlock()
		s.wake()
		return id
	}
	s.started = true
	s.mu.Unlock()
	go a.runDesktopConvSync(desktopSurfaceContext())
	return id
}

func (s *desktopConvSyncer) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// PublishDesktopBotConversation writes the page's entries for owner. Only the
// account this GUI is signed in as is written: a transcript left in storage
// by another account never goes out under this login.
func (a *App) PublishDesktopBotConversation(owner string, entries []DesktopBotConversationWrite) error {
	owner = strings.TrimSpace(owner)
	if a == nil {
		return fmt.Errorf("AI assistant backend is unavailable")
	}
	account := strings.TrimSpace(a.desktopBotAccountUserID())
	if owner == "" || owner == "local" || account == "local" || owner != account {
		return nil
	}
	clean := make([]DesktopBotConversationWrite, 0, len(entries))
	for _, e := range entries {
		e.BotID = strings.TrimSpace(e.BotID)
		e.ID = strings.TrimSpace(e.ID)
		if e.BotID == "" || e.ID == "" || len(e.Data) == 0 {
			continue
		}
		if e.Kind == "" {
			e.Kind = "message"
		}
		clean = append(clean, e)
	}
	for len(clean) > 0 {
		n := len(clean)
		if n > desktopConvBatch {
			n = desktopConvBatch
		}
		if err := a.postDesktopConv(clean[:n]); err != nil {
			return err
		}
		clean = clean[n:]
	}
	return nil
}

func (a *App) postDesktopConv(entries []DesktopBotConversationWrite) error {
	body := map[string]any{"client_id": desktopBotClientID(), "entries": entries}
	status, err := a.desktopBotExchange(context.Background(), http.MethodPost, "/api/v1/bots/conversation", body, nil, 15*time.Second, nil)
	if status == http.StatusNotFound {
		// An older Hub, or a bot this account does not own on Hub (a local
		// bot). Neither is an error for the page.
		return nil
	}
	if status == http.StatusBadRequest {
		// Hub refused the entries as written (too large, malformed). Sending
		// the same entries again would be refused again.
		log.Printf("[desktop-bot-sync] hub refused %d entries: %v", len(entries), err)
		return nil
	}
	return err
}

// publishDesktopRunState tells other clients this turn is on the desktop.
func (a *App) publishDesktopRunState(botID, requestID, runID, state string) {
	botID = strings.TrimSpace(botID)
	requestID = strings.TrimSpace(requestID)
	if a == nil || botID == "" || requestID == "" || !desktopConvAutoPublish() {
		return
	}
	data, _ := json.Marshal(map[string]string{"state": state, "run_id": strings.TrimSpace(runID), "request_id": requestID})
	owner := a.desktopBotAccountUserID()
	desktopConvGo(func() {
		if err := a.PublishDesktopBotConversation(owner, []DesktopBotConversationWrite{{BotID: botID, ID: "run:" + requestID, Kind: "run", Data: data}}); err != nil {
			log.Printf("[desktop-bot-sync] run state bot=%s state=%s err=%v", botID, state, err)
		}
	})
}

// publishDesktopActivityRemote shares the working-row step of a run.
func (a *App) publishDesktopActivityRemote(botID, runID, activity string) {
	if a == nil || botID == "" || runID == "" || !desktopConvAutoPublish() {
		return
	}
	key := botID + "\x00" + runID
	desktopConv.mu.Lock()
	if desktopConv.lastAct[key] == activity {
		desktopConv.mu.Unlock()
		return
	}
	if activity == "" {
		delete(desktopConv.lastAct, key)
	} else {
		desktopConv.lastAct[key] = activity
	}
	desktopConv.mu.Unlock()
	data, _ := json.Marshal(map[string]string{"run_id": runID, "activity": activity})
	owner := a.desktopBotAccountUserID()
	desktopConvGo(func() {
		_ = a.PublishDesktopBotConversation(owner, []DesktopBotConversationWrite{{BotID: botID, ID: "act:" + runID, Kind: "activity", Data: data}})
	})
}

func (a *App) runDesktopConvSync(ctx context.Context) {
	wait := desktopConvRetryMin
	for {
		if ctx.Err() != nil {
			return
		}
		next, err := a.desktopConvSyncOnce(ctx, desktopConvWaitSeconds)
		if err == nil {
			wait = desktopConvRetryMin
			if next > 0 {
				continue
			}
		}
		pause := wait
		if err == errDesktopConvMissing {
			pause = desktopConvMissingWait
		} else if err != nil {
			wait *= 2
			if wait > desktopConvRetryMax {
				wait = desktopConvRetryMax
			}
		}
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-desktopConv.kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

var errDesktopConvMissing = fmt.Errorf("hub has no conversation journal")

// desktopConvSyncOnce reads one page and hands it out. It returns how many
// entries it read, so a full page is followed at once.
func (a *App) desktopConvSyncOnce(ctx context.Context, waitSeconds int) (int, error) {
	s := desktopConv
	account := strings.TrimSpace(a.desktopBotAccountUserID())
	s.mu.Lock()
	if s.reset || s.account != account {
		s.cursor = 0
		s.reset = false
		if s.account != account {
			a.releaseDesktopConvRemoteLocked("")
			s.account = account
		}
	}
	cursor := s.cursor
	s.mu.Unlock()
	if account == "" || account == "local" {
		return 0, fmt.Errorf("not signed in")
	}
	path := "/api/v1/bots/conversation?after=" + strconv.FormatInt(cursor, 10) + "&wait=" + strconv.Itoa(waitSeconds)
	var page desktopBotConversationPage
	status, err := a.desktopBotExchange(ctx, http.MethodGet, path, nil, &page, time.Duration(waitSeconds+15)*time.Second, nil)
	if status == http.StatusNotFound {
		return 0, errDesktopConvMissing
	}
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	if s.account != account || s.cursor != cursor || s.reset {
		// The account changed or the page asked for a rebuild meanwhile.
		s.mu.Unlock()
		return 0, nil
	}
	s.cursor = page.Next
	s.mu.Unlock()
	a.applyDesktopConvPage(account, page)
	if page.More {
		return len(page.Entries), nil
	}
	return 0, nil
}

func (a *App) applyDesktopConvPage(owner string, page desktopBotConversationPage) {
	self := desktopBotClientID()
	messages := make([]map[string]any, 0, len(page.Entries))
	for _, e := range page.Entries {
		// This client's own change is already on its page. Another client's
		// change to a bubble this client wrote (an answered card) is not.
		by := e.By
		if by == "" {
			by = e.Origin
		}
		if by == self || strings.TrimSpace(e.BotID) == "" {
			continue
		}
		switch e.Kind {
		case "message":
			if desktopConvOwnLiveRun(e.BotID, e.Data) {
				// Another client's bubble for a run this process is still
				// relaying. Its own reply lands here; taking the other one
				// too would show the result twice.
				continue
			}
			messages = append(messages, map[string]any{
				"bot_id":      e.BotID,
				"id":          e.ID,
				"seq":         e.Seq,
				"created_seq": e.CreatedSeq,
				"origin":      e.Origin,
				"by":          by,
				"data":        e.Data,
			})
		case "run":
			a.applyDesktopConvRun(owner, e)
		case "activity":
			var d struct {
				RunID    string `json:"run_id"`
				Activity string `json:"activity"`
			}
			if json.Unmarshal(e.Data, &d) != nil || d.RunID == "" {
				continue
			}
			if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && time.Since(at) > desktopConvRemoteStale {
				continue
			}
			a.emitDesktopConv("desktop-bot-activity", map[string]any{"bot_id": e.BotID, "run_id": d.RunID, "activity": d.Activity})
		}
	}
	a.expireDesktopConvRemote()
	if len(messages) > 0 || page.Reset {
		a.emitDesktopConv("desktop-bot-sync", map[string]any{
			"owner_id": owner,
			"entries":  messages,
			"head":     page.Head,
			"reset":    page.Reset,
		})
	}
}

func desktopConvOwnLiveRun(botID string, data json.RawMessage) bool {
	var d struct {
		RunID string `json:"runId"`
	}
	if json.Unmarshal(data, &d) != nil || strings.TrimSpace(d.RunID) == "" {
		return false
	}
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	for _, run := range activeDesktopRuns {
		if run != nil && run.BotID == botID && run.RunID == strings.TrimSpace(d.RunID) {
			return true
		}
	}
	return false
}

func (a *App) applyDesktopConvRun(owner string, e desktopBotConversationEntry) {
	var d struct {
		State string `json:"state"`
		RunID string `json:"run_id"`
	}
	if json.Unmarshal(e.Data, &d) != nil {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, e.At)
	if err != nil {
		at = time.Now()
	}
	key := e.Origin + "\x00" + e.BotID + "\x00" + e.ID
	s := desktopConv
	s.mu.Lock()
	held := s.remote[key]
	running := d.State == "running" && time.Since(at) < desktopConvRemoteStale
	switch {
	case running && held == nil:
		s.remote[key] = &desktopConvRemoteRun{botID: e.BotID, owner: owner, seen: at}
		s.mu.Unlock()
		a.noteDesktopBotBusy(e.BotID, owner, true)
		return
	case running:
		held.seen = at
	case held != nil:
		delete(s.remote, key)
		s.mu.Unlock()
		a.noteDesktopBotBusy(held.botID, held.owner, false)
		a.emitDesktopConv("desktop-bot-activity", map[string]any{"bot_id": e.BotID, "run_id": d.RunID, "activity": ""})
		return
	}
	s.mu.Unlock()
}

// expireDesktopConvRemote lets go of another client's run that stopped
// reporting (that client closed mid-run). The page then stops showing busy.
func (a *App) expireDesktopConvRemote() {
	s := desktopConv
	s.mu.Lock()
	var gone []*desktopConvRemoteRun
	for key, run := range s.remote {
		if time.Since(run.seen) > desktopConvRemoteStale {
			gone = append(gone, run)
			delete(s.remote, key)
		}
	}
	s.mu.Unlock()
	for _, run := range gone {
		a.noteDesktopBotBusy(run.botID, run.owner, false)
	}
}

// releaseDesktopConvRemoteLocked drops every remote busy mark (the account
// changed). s.mu is held.
func (a *App) releaseDesktopConvRemoteLocked(_ string) {
	s := desktopConv
	for key, run := range s.remote {
		delete(s.remote, key)
		go a.noteDesktopBotBusy(run.botID, run.owner, false)
	}
}
