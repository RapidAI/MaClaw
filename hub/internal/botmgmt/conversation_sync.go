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

// ConversationAssignment is the sequence a write landed on. CreatedSeq is the
// first write of that message and does not move when the message is edited,
// so every GUI of this person can show the same order.
type ConversationAssignment struct {
	BotID      string `json:"bot_id"`
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
	CreatedSeq int64  `json:"created_seq"`
}

// ConversationSave is the result of one write. Assigned lists each message
// the call touched, including one whose text did not change, so the writer
// learns the sequence it should display.
type ConversationSave struct {
	Head     int64                    `json:"head"`
	Assigned []ConversationAssignment `json:"assigned,omitempty"`
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
	// Rows written before display numbers existed have no created_seq. Stamp
	// the seq they already hold so a later edit cannot move them, and so a
	// GUI that just came online can sort the history it reads.
	if stampConvCreatedSeq(lg) {
		_ = s.convPersistLocked(ctx, tenantID, userID, lg)
	}
	return lg, nil
}

// convDisplaySeq is the order a chat line keeps. CreatedSeq is that number
// once it has been stamped; older rows still use the seq they were stored with.
func convDisplaySeq(e ConversationEntry) int64 {
	if e.CreatedSeq > 0 {
		return e.CreatedSeq
	}
	return e.Seq
}

func stampConvCreatedSeq(lg *convLog) bool {
	changed := false
	for i := range lg.entries {
		e := &lg.entries[i]
		if e.Kind != convKindMessage || e.CreatedSeq > 0 || e.Seq <= 0 {
			continue
		}
		e.CreatedSeq = e.Seq
		changed = true
	}
	return changed
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
// convPerBotKeep messages (by display sequence, not by last edit) and
// convPerBotState run/activity entries of each bot.
func pruneConvLocked(lg *convLog, now time.Time) {
	type ranked struct {
		e ConversationEntry
		i int
	}
	groups := map[string][]ranked{}
	var order []string
	for i, e := range lg.entries {
		if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && now.Sub(at) > convMaxAge {
			continue
		}
		key := e.BotID
		if e.Kind != convKindMessage {
			key = e.BotID + "\x00state"
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], ranked{e: e, i: i})
	}
	keep := make([]ranked, 0, len(lg.entries))
	for _, key := range order {
		items := groups[key]
		limit := convPerBotKeep
		if strings.Contains(key, "\x00state") {
			limit = convPerBotState
		}
		if len(items) > limit {
			sort.SliceStable(items, func(a, b int) bool {
				da, db := convDisplaySeq(items[a].e), convDisplaySeq(items[b].e)
				if da != db {
					return da > db
				}
				return items[a].i > items[b].i
			})
			items = items[:limit]
		}
		keep = append(keep, items...)
	}
	sort.SliceStable(keep, func(a, b int) bool { return keep[a].i < keep[b].i })
	out := make([]ConversationEntry, 0, len(keep))
	for _, item := range keep {
		out = append(out, item.e)
	}
	lg.entries = out
}

// WriteConversation upserts a client's entries. An entry identical to the
// stored one takes no new seq, so a client resending its whole transcript
// after a restart is a no-op. It returns the person's head after the writes.
func (s *Service) WriteConversation(ctx context.Context, tenantID, userID, origin string, writes []ConversationWrite) (int64, error) {
	saved, err := s.SaveConversation(ctx, tenantID, userID, origin, writes)
	return saved.Head, err
}

// SaveConversation is WriteConversation plus the sequence of each message,
// so the writing GUI can place its own bubble without waiting for another
// client to echo it.
func (s *Service) SaveConversation(ctx context.Context, tenantID, userID, origin string, writes []ConversationWrite) (ConversationSave, error) {
	if s == nil {
		return ConversationSave{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	if len(writes) == 0 {
		head, err := s.conversationHead(ctx, tenantID, userID)
		return ConversationSave{Head: head}, err
	}
	if len(writes) > convBatchMax {
		return ConversationSave{}, fmt.Errorf("%w: too many entries", ErrInvalidInput)
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return ConversationSave{}, err
	}
	for i := range writes {
		if err := validConvWrite(&writes[i]); err != nil {
			return ConversationSave{}, err
		}
		if !s.ownedBot(&rec, userID, writes[i].BotID) {
			return ConversationSave{}, ErrNotFound
		}
	}
	origin = convOrigin(origin)
	h := s.conv()
	h.mu.Lock()
	defer h.mu.Unlock()
	lg, err := s.convLogLocked(ctx, h, tenantID, userID)
	if err != nil {
		return ConversationSave{}, err
	}
	now := s.now().UTC()
	changed, durable := false, false
	assigned := make([]ConversationAssignment, 0, len(writes))
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
				assigned = noteConvAssignment(assigned, lg.entries[at])
				continue
			}
			e := lg.entries[at]
			lg.head++
			e.Seq, e.Data, e.At, e.By = lg.head, merged, now.Format(time.RFC3339Nano), origin
			lg.entries = append(append(lg.entries[:at:at], lg.entries[at+1:]...), e)
			assigned = noteConvAssignment(assigned, e)
			changed = true
			durable = durable || e.Kind != convKindActivity
			continue
		}
		if at >= 0 && convSameData(lg.entries[at].Data, w.Data) {
			assigned = noteConvAssignment(assigned, lg.entries[at])
			continue
		}
		lg.head++
		e := ConversationEntry{Seq: lg.head, CreatedSeq: lg.head, BotID: w.BotID, ID: w.ID, Kind: w.Kind, Origin: origin, By: origin, Data: w.Data, At: now.Format(time.RFC3339Nano)}
		if at >= 0 {
			prev := lg.entries[at]
			if prev.CreatedSeq <= 0 && prev.Seq > 0 {
				prev.CreatedSeq = prev.Seq
			}
			e.CreatedSeq = prev.CreatedSeq
			// The writer of a message stays its origin, so that client does
			// not take its own message back as someone else's.
			if prev.Origin != "" {
				e.Origin = prev.Origin
			}
			lg.entries = append(lg.entries[:at:at], lg.entries[at+1:]...)
		}
		lg.entries = append(lg.entries, e)
		assigned = noteConvAssignment(assigned, e)
		changed = true
		durable = durable || e.Kind != convKindActivity
	}
	saved := ConversationSave{Head: lg.head, Assigned: assigned}
	if !changed {
		return saved, nil
	}
	pruneConvLocked(lg, now)
	if durable || lg.head-lg.stored >= convHeadGap/2 {
		if err := s.convPersistLocked(ctx, tenantID, userID, lg); err != nil {
			return ConversationSave{}, err
		}
	}
	close(lg.wake)
	lg.wake = make(chan struct{})
	return saved, nil
}

// noteConvAssignment records a message's display sequence. Run and activity
// entries are live state, not chat lines, so they get no number.
func noteConvAssignment(out []ConversationAssignment, e ConversationEntry) []ConversationAssignment {
	if e.Kind != convKindMessage {
		return out
	}
	created := e.CreatedSeq
	if created <= 0 {
		created = e.Seq
	}
	return append(out, ConversationAssignment{BotID: e.BotID, ID: e.ID, Seq: e.Seq, CreatedSeq: created})
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
			if e.Kind == convKindMessage && e.CreatedSeq <= 0 {
				e.CreatedSeq = e.Seq
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
