package botmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// The conversation journal is what keeps every GUI of one person on the same
// chat with a bot. Each client still writes its own transcript; what it shows
// (the person's message, the instant acknowledgement, the settled reply, an
// ask card and whether it was answered) is also upserted here under the
// client's own message id. Every entry change takes the next sequence number
// of that person, so another client asks for "everything after N" and lands
// the same state, live or after being closed. Hub keeps the entries opaque;
// it only orders, deduplicates and isolates them.
//
// Isolation: one journal per (tenant, user); an entry names its bot, and only
// a bot this user owns is accepted. Another user never reads it.

const (
	convKindMessage  = "message"
	convKindRun      = "run"
	convKindActivity = "activity"

	convPerBotKeep  = 200
	convPerBotState = 20
	convMaxAge      = 14 * 24 * time.Hour
	convDataMax     = 32 << 10
	convIDMax       = 160
	convPageMax     = 500
	convWaitMax     = 25 * time.Second
	convBatchMax    = 50
	convOriginMax   = 80
	convSettingsTag = ":conv:"
	// Activity entries are not written to settings, so the stored head can
	// trail the live one by those. A reload starts past that gap, and the gap
	// is never allowed to grow beyond it, so a seq a client already holds is
	// never handed out again.
	convHeadGap = 1000
)

// ConversationEntry is one synced item. ID is stable inside the bot: the
// client's message id, or the client's request for a run state. Seq is the
// change number; CreatedSeq stays the number of the first write so a client
// can keep the original order when an older message is updated.
type ConversationEntry struct {
	Seq        int64  `json:"seq"`
	CreatedSeq int64  `json:"created_seq"`
	BotID      string `json:"bot_id"`
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin,omitempty"`
	// By is the client that made this change. A flag set on a client's own
	// message by another client comes back to it; its own writes do not.
	By   string          `json:"by,omitempty"`
	Data json.RawMessage `json:"data"`
	At   string          `json:"at"`
}

// ConversationWrite is what a client posts. Patch merges the true booleans of
// Data into an existing message (answered card, used options) without taking
// it over: the message keeps the client that wrote it.
type ConversationWrite struct {
	BotID string          `json:"bot_id"`
	ID    string          `json:"id"`
	Kind  string          `json:"kind"`
	Patch bool            `json:"patch,omitempty"`
	Data  json.RawMessage `json:"data"`
}

// ConversationPage answers one read. Head is the newest seq; a client whose
// cursor is past Head (a Hub that lost its journal) starts over from 0.
type ConversationPage struct {
	Entries []ConversationEntry `json:"entries"`
	Next    int64               `json:"next"`
	Head    int64               `json:"head"`
	Reset   bool                `json:"reset,omitempty"`
	More    bool                `json:"more,omitempty"`
}

type convLog struct {
	loaded  bool
	head    int64
	stored  int64
	entries []ConversationEntry
	wake    chan struct{}
}

type convStored struct {
	Head    int64               `json:"head"`
	Entries []ConversationEntry `json:"entries"`
}

type convHub struct {
	mu   sync.Mutex
	logs map[string]*convLog
}

func (s *Service) conv() *convHub {
	s.convOnce.Do(func() { s.convJournal = &convHub{logs: map[string]*convLog{}} })
	return s.convJournal
}

func convKey(tenantID, userID string) string {
	return storageKey(tenantID) + convSettingsTag + userID
}

// convLogLocked returns the person's journal, reading it once from settings.
// A failed read leaves it unloaded: writing then would replace the stored
// journal with an empty one.
func (s *Service) convLogLocked(ctx context.Context, h *convHub, tenantID, userID string) (*convLog, error) {
	key := convKey(tenantID, userID)
	lg := h.logs[key]
	if lg == nil {
		lg = &convLog{wake: make(chan struct{})}
		h.logs[key] = lg
	}
	if lg.loaded {
		return lg, nil
	}
	if s.System == nil {
		return lg, ErrSettingsUnavailable
	}
	raw, err := s.System.Get(ctx, key)
	if err != nil && !errors.Is(err, io.EOF) {
		return lg, fmt.Errorf("%w: %v", ErrSettingsUnavailable, err)
	}
	if strings.TrimSpace(raw) != "" {
		var st convStored
		if json.Unmarshal([]byte(raw), &st) == nil {
			lg.head = st.Head + convHeadGap
			lg.entries = st.Entries
		}
	}
	lg.stored = lg.head
	lg.loaded = true
	return lg, nil
}

func (s *Service) convPersistLocked(ctx context.Context, tenantID, userID string, lg *convLog) error {
	if s.System == nil {
		return ErrSettingsUnavailable
	}
	kept := make([]ConversationEntry, 0, len(lg.entries))
	for _, e := range lg.entries {
		// The step a run is in changes every few seconds; it is live state,
		// not history, and is not worth a settings write.
		if e.Kind == convKindActivity {
			continue
		}
		kept = append(kept, e)
	}
	raw, err := json.Marshal(convStored{Head: lg.head, Entries: kept})
	if err != nil {
		return err
	}
	if err := s.System.Set(ctx, convKey(tenantID, userID), string(raw)); err != nil {
		return err
	}
	lg.stored = lg.head
	return nil
}

func convOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if len(origin) > convOriginMax {
		origin = origin[:convOriginMax]
	}
	return origin
}

func validConvWrite(w *ConversationWrite) error {
	w.BotID = strings.TrimSpace(w.BotID)
	w.ID = strings.TrimSpace(w.ID)
	w.Kind = strings.TrimSpace(w.Kind)
	if w.Kind == "" {
		w.Kind = convKindMessage
	}
	if w.BotID == "" || w.ID == "" || len(w.ID) > convIDMax {
		return fmt.Errorf("%w: bot_id and id are required", ErrInvalidInput)
	}
	switch w.Kind {
	case convKindMessage, convKindRun, convKindActivity:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidInput, w.Kind)
	}
	data := bytes.TrimSpace(w.Data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("%w: data must be an object", ErrInvalidInput)
	}
	if len(data) > convDataMax {
		return fmt.Errorf("%w: data too large", ErrInvalidInput)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("%w: data must be an object", ErrInvalidInput)
	}
	w.Data = data
	return nil
}

// mergeConvPatch sets each true boolean of patch on base. Only a flag that
// turns on counts: an answered card is never un-answered by a late client.
func mergeConvPatch(base, patch json.RawMessage) (json.RawMessage, bool) {
	var into map[string]json.RawMessage
	var from map[string]json.RawMessage
	if json.Unmarshal(base, &into) != nil || json.Unmarshal(patch, &from) != nil {
		return base, false
	}
	changed := false
	for k, v := range from {
		if string(bytes.TrimSpace(v)) != "true" {
			continue
		}
		if string(bytes.TrimSpace(into[k])) == "true" {
			continue
		}
		into[k] = json.RawMessage("true")
		changed = true
	}
	if !changed {
		return base, false
	}
	raw, err := json.Marshal(into)
	if err != nil {
		return base, false
	}
	return raw, true
}

func convSameData(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ra, _ := json.Marshal(x)
	rb, _ := json.Marshal(y)
	return bytes.Equal(ra, rb)
}

// pruneConvLocked drops entries older than the window and keeps the newest
// convPerBotKeep messages and convPerBotState run/activity entries of each bot.
func pruneConvLocked(lg *convLog, now time.Time) {
	perBot := map[string]int{}
	keep := make([]ConversationEntry, 0, len(lg.entries))
	for i := len(lg.entries) - 1; i >= 0; i-- {
		e := lg.entries[i]
		if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && now.Sub(at) > convMaxAge {
			continue
		}
		// Run and activity entries are live state: a few per bot are enough,
		// and they must not push chat lines out of the window.
		limit, bucket := convPerBotKeep, e.BotID
		if e.Kind != convKindMessage {
			limit, bucket = convPerBotState, e.BotID+"\x00state"
		}
		perBot[bucket]++
		if perBot[bucket] > limit {
			continue
		}
		keep = append(keep, e)
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	lg.entries = keep
}

// WriteConversation upserts a client's entries. An entry identical to the
// stored one takes no new seq, so a client resending its whole transcript
// after a restart is a no-op. It returns the person's head after the writes.
func (s *Service) WriteConversation(ctx context.Context, tenantID, userID, origin string, writes []ConversationWrite) (int64, error) {
	if s == nil {
		return 0, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	if len(writes) == 0 {
		return s.conversationHead(ctx, tenantID, userID)
	}
	if len(writes) > convBatchMax {
		return 0, fmt.Errorf("%w: too many entries", ErrInvalidInput)
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return 0, err
	}
	for i := range writes {
		if err := validConvWrite(&writes[i]); err != nil {
			return 0, err
		}
		if !s.ownedBot(&rec, userID, writes[i].BotID) {
			return 0, ErrNotFound
		}
	}
	origin = convOrigin(origin)
	h := s.conv()
	h.mu.Lock()
	defer h.mu.Unlock()
	lg, err := s.convLogLocked(ctx, h, tenantID, userID)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	changed, durable := false, false
	for _, w := range writes {
		at := -1
		for i := range lg.entries {
			if lg.entries[i].BotID == w.BotID && lg.entries[i].ID == w.ID && lg.entries[i].Kind == w.Kind {
				at = i
				break
			}
		}
		if w.Patch {
			if at < 0 {
				continue
			}
			merged, ok := mergeConvPatch(lg.entries[at].Data, w.Data)
			if !ok {
				continue
			}
			e := lg.entries[at]
			lg.head++
			e.Seq, e.Data, e.At, e.By = lg.head, merged, now.Format(time.RFC3339Nano), origin
			lg.entries = append(append(lg.entries[:at:at], lg.entries[at+1:]...), e)
			changed = true
			durable = durable || e.Kind != convKindActivity
			continue
		}
		if at >= 0 && convSameData(lg.entries[at].Data, w.Data) {
			continue
		}
		lg.head++
		e := ConversationEntry{Seq: lg.head, CreatedSeq: lg.head, BotID: w.BotID, ID: w.ID, Kind: w.Kind, Origin: origin, By: origin, Data: w.Data, At: now.Format(time.RFC3339Nano)}
		if at >= 0 {
			prev := lg.entries[at]
			e.CreatedSeq = prev.CreatedSeq
			// The writer of a message stays its origin, so that client does
			// not take its own message back as someone else's.
			if prev.Origin != "" {
				e.Origin = prev.Origin
			}
			lg.entries = append(lg.entries[:at:at], lg.entries[at+1:]...)
		}
		lg.entries = append(lg.entries, e)
		changed = true
		durable = durable || e.Kind != convKindActivity
	}
	if !changed {
		return lg.head, nil
	}
	pruneConvLocked(lg, now)
	if durable || lg.head-lg.stored >= convHeadGap/2 {
		if err := s.convPersistLocked(ctx, tenantID, userID, lg); err != nil {
			return 0, err
		}
	}
	close(lg.wake)
	lg.wake = make(chan struct{})
	return lg.head, nil
}

func (s *Service) conversationHead(ctx context.Context, tenantID, userID string) (int64, error) {
	if _, err := s.loadForUser(ctx, tenantID, userID); err != nil {
		return 0, err
	}
	h := s.conv()
	h.mu.Lock()
	defer h.mu.Unlock()
	lg, err := s.convLogLocked(ctx, h, tenantID, userID)
	if err != nil {
		return 0, err
	}
	return lg.head, nil
}

// ReadConversation returns the person's entries changed after the cursor,
// oldest change first. botID narrows to one bot. With wait > 0 and nothing
// new, it holds the request until an entry changes or wait passes.
func (s *Service) ReadConversation(ctx context.Context, tenantID, userID, botID string, after int64, limit int, wait time.Duration) (ConversationPage, error) {
	if s == nil {
		return ConversationPage{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return ConversationPage{}, err
	}
	if botID != "" && !s.ownedBot(&rec, userID, botID) {
		return ConversationPage{}, ErrNotFound
	}
	if limit <= 0 || limit > convPageMax {
		limit = convPageMax
	}
	if wait > convWaitMax {
		wait = convWaitMax
	}
	if after < 0 {
		after = 0
	}
	deadline := time.Now().Add(wait)
	h := s.conv()
	for {
		h.mu.Lock()
		lg, err := s.convLogLocked(ctx, h, tenantID, userID)
		if err != nil {
			h.mu.Unlock()
			return ConversationPage{}, err
		}
		page := ConversationPage{Entries: []ConversationEntry{}, Head: lg.head, Next: after}
		if after > lg.head {
			page.Reset = true
			after = 0
			page.Next = 0
		}
		for _, e := range lg.entries {
			if e.Seq <= after || (botID != "" && e.BotID != botID) {
				continue
			}
			// Keep only bots this user still owns; a deleted bot's lines stop.
			if !s.ownedBot(&rec, userID, e.BotID) {
				continue
			}
			page.Entries = append(page.Entries, e)
		}
		wake := lg.wake
		h.mu.Unlock()
		sort.SliceStable(page.Entries, func(i, j int) bool { return page.Entries[i].Seq < page.Entries[j].Seq })
		if len(page.Entries) > limit {
			page.Entries = page.Entries[:limit]
			page.More = true
		}
		if n := len(page.Entries); n > 0 {
			page.Next = page.Entries[n-1].Seq
		} else if !page.Reset && page.Head > page.Next && botID == "" {
			// Nothing of this user's is past the cursor any more (pruned or
			// a deleted bot): move the cursor up to the head.
			page.Next = page.Head
		}
		left := time.Until(deadline)
		if len(page.Entries) > 0 || page.Reset || wait <= 0 || left <= 0 {
			return page, nil
		}
		timer := time.NewTimer(left)
		select {
		case <-ctx.Done():
			timer.Stop()
			return page, nil
		case <-timer.C:
		case <-wake:
			timer.Stop()
		}
	}
}
