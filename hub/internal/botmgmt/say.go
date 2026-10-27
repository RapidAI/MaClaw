package botmgmt

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"
	"unicode"
)

// Bot speech to the person (S4) and to rooms (S5). Any bot of the account,
// main or sub, may speak to the person directly. Hub keeps what was said
// as "say" notes (target "human" or "room:<id>") and refuses a message
// that repeats what another bot (the main bot included) already said
// there recently, so the person never hears the same thing twice.
// MACLAW_BOT_SAY=off turns it off.

const (
	sayWindow  = 30 * time.Minute
	sayPerHour = 12
	sayTextMax = 2000
)

// ErrAlreadySaid: another bot already told the person the same thing.
var ErrAlreadySaid = errors.New("already said")

// AlreadySaidError names who said it.
type AlreadySaidError struct{ By, NoteID string }

func (e *AlreadySaidError) Error() string { return "already said by " + e.By }
func (e *AlreadySaidError) Unwrap() error { return ErrAlreadySaid }

func botSayEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_BOT_SAY"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// sayNorm keeps letters and digits only, lower-cased.
func sayNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sayRepeats: same words, or one contains the other and is substantial.
func sayRepeats(a, b string) bool {
	na, nb := sayNorm(a), sayNorm(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	short, long := na, nb
	if len([]rune(short)) > len([]rune(long)) {
		short, long = long, short
	}
	return len([]rune(short)) >= 12 && strings.Contains(long, short)
}

// SayFromInstance is a server-side bot run speaking to the person (room
// empty) or into a room.
func (s *Service) SayFromInstance(ctx context.Context, tenantID, userID, instanceID, room, text, key string) (BotCrossNote, error) {
	from, err := s.botForInstance(ctx, tenantID, userID, instanceID)
	if err != nil {
		return BotCrossNote{}, err
	}
	return s.Say(ctx, tenantID, userID, from, room, text, key)
}

// Say records one message from bot from to the person or a room.
func (s *Service) Say(ctx context.Context, tenantID, userID, from, room, text, key string) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	if !botSayEnabled() {
		return BotCrossNote{}, ErrInvalidInput
	}
	userID, text, room = strings.TrimSpace(userID), strings.TrimSpace(text), strings.TrimSpace(room)
	if r := []rune(text); len(r) > sayTextMax {
		text = string(r[:sayTextMax])
	}
	if userID == "" || text == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotCrossNote{}, err
	}
	if !botOwned(rec.Bots, userID, from) {
		return BotCrossNote{}, ErrNotFound
	}
	target := "human"
	if room != "" {
		r := roomOf(rec, userID, room)
		if r == nil {
			return BotCrossNote{}, ErrNotFound
		}
		if !r.hasMember(from) {
			return BotCrossNote{}, ErrNotFound
		}
		target = "room:" + r.ID
	}
	if prior := keyedNote(rec.CrossNotes, userID, from, key); prior != nil {
		out := *prior
		out.Duplicate = true
		return out, nil
	}
	now := s.now()
	recent := 0
	for i := len(rec.CrossNotes) - 1; i >= 0; i-- {
		n := rec.CrossNotes[i]
		if n.Owner != userID || (n.Kind != "say" && n.Kind != "room") {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, n.CreatedAt)
		if err != nil {
			continue
		}
		age := now.Sub(stamp)
		if n.Kind == "say" && n.From == from && age <= time.Hour {
			recent++
		}
		// In a room, only what was said since the person last spoke counts.
		if n.Kind == "room" && n.Target == target {
			break
		}
		if n.Kind == "say" && n.Target == target && age <= sayWindow && sayRepeats(n.Text, text) {
			return n, &AlreadySaidError{By: n.From, NoteID: n.ID}
		}
	}
	if recent >= sayPerHour {
		return BotCrossNote{}, ErrRateLimited
	}
	note := BotCrossNote{ID: newCrossNoteID(), Owner: userID, From: from, Target: target, Kind: "say", Key: strings.TrimSpace(key), Text: text, CreatedAt: now.UTC().Format(time.RFC3339)}
	var answered []string
	if room != "" {
		answered = RoomAnsweredBy(rec.CrossNotes, userID, target)
	}
	rec.CrossNotes = append(rec.CrossNotes, note)
	rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, err
	}
	note.AnsweredBy = answered
	return note, nil
}

// ListSaid returns what bots told the person (room empty) or said in a
// room, oldest first, with the person's room messages interleaved.
func (s *Service) ListSaid(ctx context.Context, tenantID, userID, room string) ([]BotCrossNote, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	target := "human"
	if room = strings.TrimSpace(room); room != "" {
		r := roomOf(rec, userID, room)
		if r == nil {
			return nil, ErrNotFound
		}
		target = "room:" + r.ID
	}
	out := []BotCrossNote{}
	for _, n := range pruneCrossNotes(rec.CrossNotes, s.now()) {
		if n.Owner == userID && n.Target == target && (n.Kind == "say" || n.Kind == "room") {
			out = append(out, n)
		}
	}
	return out, nil
}
