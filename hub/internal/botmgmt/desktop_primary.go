package botmgmt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

const desktopCrossNoteCap = 50

// BotListItem is one bot on GET /api/v1/bots. Primary is computed for the
// response. It is not a field of Bot, so save cannot write it back onto the row.
type BotListItem struct {
	Bot
	Primary bool `json:"primary,omitempty"`
}

// BotCrossNote is one message between bots of the same account. It is not a run.
type BotCrossNote struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	From      string `json:"from"`
	Target    string `json:"target"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Order     string `json:"order,omitempty"`
	CreatedAt string `json:"created_at"`
	// StartedAt is the single claim that a client may admit this note.
	// Empty means no client has started it. RunID is filled after 202.
	StartedAt string `json:"started_at,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	// ReplyTo is the delegate note a result answers. One delegate is answered once.
	ReplyTo string `json:"reply_to,omitempty"`
	// NoteID is the request alias for ReplyTo. It is not stored on the note.
	NoteID string `json:"note_id,omitempty"`
}

// ListBotsForUser returns this user's bots. Exactly one of them is primary
// when the user has any bot: the stored id when it still exists, otherwise
// the earliest parseable CreatedAt, then bot id ascending.
func (s *Service) ListBotsForUser(ctx context.Context, tenantID, userID string) ([]BotListItem, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	bots := botsOwnedBy(rec.Bots, userID)
	primary := primaryBotID(rec, userID)
	out := make([]BotListItem, 0, len(bots))
	for _, bot := range bots {
		out = append(out, BotListItem{Bot: bot, Primary: primary != "" && bot.ID == primary})
	}
	return out, nil
}

// SetPrimaryBot stores this bot as the account's only primary. The previous
// id is replaced in the same write. Creating a bot does not call this.
func (s *Service) SetPrimaryBot(ctx context.Context, tenantID, userID, botID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !botOwned(rec.Bots, userID, botID) {
		return ErrNotFound
	}
	if rec.PrimaryBot == nil {
		rec.PrimaryBot = map[string]string{}
	}
	rec.PrimaryBot[userID] = botID
	return s.save(ctx, tenantID, rec)
}

func botsOwnedBy(bots []Bot, userID string) []Bot {
	userID = strings.TrimSpace(userID)
	out := make([]Bot, 0)
	for _, bot := range bots {
		if strings.TrimSpace(bot.OwnerUserID) == userID {
			out = append(out, bot)
		}
	}
	return out
}

func botOwned(bots []Bot, userID, botID string) bool {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return false
	}
	for _, bot := range botsOwnedBy(bots, userID) {
		if bot.ID == botID {
			return true
		}
	}
	return false
}

func primaryBotID(rec record, userID string) string {
	userID = strings.TrimSpace(userID)
	owned := botsOwnedBy(rec.Bots, userID)
	if len(owned) == 0 {
		return ""
	}
	if rec.PrimaryBot != nil {
		marked := strings.TrimSpace(rec.PrimaryBot[userID])
		if marked != "" && botOwned(owned, userID, marked) {
			return marked
		}
	}
	return earliestBotID(owned)
}

func earliestBotID(bots []Bot) string {
	if len(bots) == 0 {
		return ""
	}
	best := 0
	for i := 1; i < len(bots); i++ {
		if botCreatedBefore(bots[i], bots[best]) {
			best = i
		}
	}
	return bots[best].ID
}

func botCreatedBefore(a, b Bot) bool {
	ta, aErr := time.Parse(time.RFC3339, strings.TrimSpace(a.CreatedAt))
	tb, bErr := time.Parse(time.RFC3339, strings.TrimSpace(b.CreatedAt))
	aOK, bOK := aErr == nil, bErr == nil
	if aOK != bOK {
		return aOK
	}
	if aOK && !ta.Equal(tb) {
		return ta.Before(tb)
	}
	return a.ID < b.ID
}

// PostBotCrossTalk appends one note. notify and delegate require the caller
// to be the primary bot and do not admit a run. result is accepted from the
// delegate note's stored sender, including after that bot is no longer primary
// or has been deleted.
func (s *Service) PostBotCrossTalk(ctx context.Context, tenantID, userID, fromBotID string, in BotCrossNote) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	fromBotID = strings.TrimSpace(fromBotID)
	kind := strings.TrimSpace(in.Kind)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	rec.CrossNotes = pruneCrossNotes(rec.CrossNotes, s.now())
	switch kind {
	case "notify", "delegate":
		if !botOwned(rec.Bots, userID, fromBotID) {
			return BotCrossNote{}, ErrNotFound
		}
		if primaryBotID(rec, userID) != fromBotID {
			return BotCrossNote{}, ErrNotPrimary
		}
		target := strings.TrimSpace(in.Target)
		if target == "" || target == fromBotID || !botOwned(rec.Bots, userID, target) {
			return BotCrossNote{}, ErrNoSuchBot
		}
		note := BotCrossNote{
			ID:        newCrossNoteID(),
			Owner:     userID,
			From:      fromBotID,
			Target:    target,
			Kind:      kind,
			Text:      strings.TrimSpace(in.Text),
			Order:     strings.TrimSpace(in.Order),
			CreatedAt: s.now().UTC().Format(time.RFC3339),
		}
		rec.CrossNotes = append(rec.CrossNotes, note)
		rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
		if err := s.save(ctx, tenantID, rec); err != nil {
			return BotCrossNote{}, err
		}
		return note, nil
	case "result":
		parentID := strings.TrimSpace(in.ReplyTo)
		if parentID == "" {
			parentID = strings.TrimSpace(in.NoteID)
		}
		if parentID == "" {
			parentID = strings.TrimSpace(in.ID)
		}
		parent := findCrossNote(rec.CrossNotes, parentID)
		if parent == nil || parent.Kind != "delegate" || parent.Owner != userID || parent.Target != fromBotID {
			return BotCrossNote{}, ErrNotFound
		}
		if existing := resultFor(rec.CrossNotes, parent.ID); existing != nil {
			return *existing, nil
		}
		note := BotCrossNote{
			ID:        newCrossNoteID(),
			Owner:     userID,
			From:      parent.From,
			Target:    parent.Target,
			Kind:      "result",
			Text:      strings.TrimSpace(in.Text),
			CreatedAt: s.now().UTC().Format(time.RFC3339),
			ReplyTo:   parent.ID,
		}
		rec.CrossNotes = append(rec.CrossNotes, note)
		rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
		if err := s.save(ctx, tenantID, rec); err != nil {
			return BotCrossNote{}, err
		}
		return note, nil
	default:
		return BotCrossNote{}, ErrInvalidInput
	}
}

// ListBotCrossNotes returns this user's notes that are still inside the
// seven-day window. It does not consume them.
func (s *Service) ListBotCrossNotes(ctx context.Context, tenantID, userID string) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	notes := pruneCrossNotes(rec.CrossNotes, s.now())
	out := make([]BotCrossNote, 0)
	userID = strings.TrimSpace(userID)
	for _, note := range notes {
		if note.Owner == userID {
			out = append(out, note)
		}
	}
	return out, nil
}

// ClaimBotCrossStart lets one client start a delegate. A second claim while
// the first is still fresh, or after a run id is stored, does not win.
// An empty claim older than 30 seconds can be taken again.
func (s *Service) ClaimBotCrossStart(ctx context.Context, tenantID, userID, botID, noteID string) (BotCrossNote, bool, error) {
	if s == nil {
		return BotCrossNote{}, false, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	botID = strings.TrimSpace(botID)
	noteID = strings.TrimSpace(noteID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, false, err
	}
	note := findCrossNote(rec.CrossNotes, noteID)
	if note == nil || note.Owner != userID || note.Kind != "delegate" || note.Target != botID {
		return BotCrossNote{}, false, ErrNotFound
	}
	now := s.now()
	if strings.TrimSpace(note.RunID) != "" || crossClaimFresh(note.StartedAt, now) {
		return *note, false, nil
	}
	note.StartedAt = now.UTC().Format(time.RFC3339)
	note.RunID = ""
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, false, err
	}
	return *note, true, nil
}

// NoteBotCrossRun stores the admitted run on a claim this target already won.
func (s *Service) NoteBotCrossRun(ctx context.Context, tenantID, userID, botID, noteID, runID string) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	note := findCrossNote(rec.CrossNotes, strings.TrimSpace(noteID))
	if note == nil || note.Owner != strings.TrimSpace(userID) || note.Target != strings.TrimSpace(botID) || note.Kind != "delegate" {
		return BotCrossNote{}, ErrNotFound
	}
	if strings.TrimSpace(note.StartedAt) == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	if existing := strings.TrimSpace(note.RunID); existing != "" && existing != runID {
		return *note, nil
	}
	note.RunID = runID
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, err
	}
	return *note, nil
}

func crossClaimFresh(started string, now time.Time) bool {
	started = strings.TrimSpace(started)
	if started == "" {
		return false
	}
	stamp, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return true
	}
	return now.Sub(stamp) <= desktopAdmitAbandonAfter
}

func findCrossNote(notes []BotCrossNote, id string) *BotCrossNote {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for i := range notes {
		if notes[i].ID == id {
			return &notes[i]
		}
	}
	return nil
}

func resultFor(notes []BotCrossNote, delegateID string) *BotCrossNote {
	for i := range notes {
		if notes[i].Kind == "result" && notes[i].ReplyTo == delegateID {
			return &notes[i]
		}
	}
	return nil
}

func pruneCrossNotes(notes []BotCrossNote, now time.Time) []BotCrossNote {
	if len(notes) == 0 {
		return notes
	}
	out := make([]BotCrossNote, 0, len(notes))
	for _, note := range notes {
		stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(note.CreatedAt))
		if err == nil && now.Sub(stamp) > desktopTaskUndeliveredKeep {
			continue
		}
		out = append(out, note)
	}
	return out
}

func capCrossNotes(notes []BotCrossNote, userID string) []BotCrossNote {
	userID = strings.TrimSpace(userID)
	owned := 0
	for _, note := range notes {
		if note.Owner == userID {
			owned++
		}
	}
	if owned <= desktopCrossNoteCap {
		return notes
	}
	drop := owned - desktopCrossNoteCap
	out := make([]BotCrossNote, 0, len(notes)-drop)
	for _, note := range notes {
		if drop > 0 && note.Owner == userID {
			drop--
			continue
		}
		out = append(out, note)
	}
	return out
}

func newCrossNoteID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "note_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return "note_" + hex.EncodeToString(buf[:])
}
