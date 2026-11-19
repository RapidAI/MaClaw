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

// speechKind keeps a sender-supplied kind only when it is one of the
// three speech kinds. Anything else is an ordinary say.
func speechKind(raw string) string {
	switch strings.TrimSpace(raw) {
	case "process", "needs_user", "outcome":
		return strings.TrimSpace(raw)
	default:
		return ""
	}
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

// SayFilesFromInstance is SayFromInstance with files already stored by
// StoreBotMailFile: a turn MaClawSrv started itself (a background-job check
// or resume) has no chat reply, so its deliverables ride on the said note
// and the GUI fetches them like a teammate's shared files.
func (s *Service) SayFilesFromInstance(ctx context.Context, tenantID, userID, instanceID, room, text, key string, files []BotMailFile) (BotCrossNote, error) {
	return s.SayFilesKindFromInstance(ctx, tenantID, userID, instanceID, room, text, key, files, "")
}

// SayFilesKindFromInstance is SayFilesFromInstance with the speech kind
// the sender already decided: process, needs_user, or outcome.
func (s *Service) SayFilesKindFromInstance(ctx context.Context, tenantID, userID, instanceID, room, text, key string, files []BotMailFile, messageKind string) (BotCrossNote, error) {
	from, err := s.botForInstance(ctx, tenantID, userID, instanceID)
	if err != nil {
		return BotCrossNote{}, err
	}
	return s.sayWith(ctx, tenantID, userID, from, room, text, key, files, messageKind)
}

// Say records one message from bot from to the person or a room.
func (s *Service) Say(ctx context.Context, tenantID, userID, from, room, text, key string) (BotCrossNote, error) {
	return s.sayWith(ctx, tenantID, userID, from, room, text, key, nil, "")
}

func (s *Service) sayWith(ctx context.Context, tenantID, userID, from, room, text, key string, files []BotMailFile, messageKind string) (BotCrossNote, error) {
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
	// A deleted id is not a speaker. Drop its lines before the repeat check
	// and the cap, so a new bot that reused the display name is not told
	// "already said" and an older live note is not evicted to make room.
	rec.CrossNotes = dropOrphanSays(rec.CrossNotes, rec, userID)
	target := "human"
	if room == "" {
		if a := openWorkerAssignment(rec.CrossNotes, userID, from, s.now()); a != nil {
			return BotCrossNote{}, &WorkerSayError{AssignmentID: a.ID, Router: a.From}
		}
	}
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
	note := BotCrossNote{ID: newCrossNoteID(), Owner: userID, From: from, Target: target, Kind: "say", MessageKind: speechKind(messageKind), Key: strings.TrimSpace(key), Text: text, Files: files, CreatedAt: now.UTC().Format(time.RFC3339)}
	var answered []string
	if room != "" {
		answered = RoomAnsweredBy(rec.CrossNotes, userID, target)
	}
	rec.CrossNotes = append(rec.CrossNotes, note)
	rec.CrossNotes = capCrossNotes(rec.CrossNotes, rec, userID)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotCrossNote{}, err
	}
	note.AnsweredBy = answered
	return note, nil
}

// noteLeftByGoneBot is a note that names a bot id this person no longer has.
// A say belongs to its speaker. A delegate or result belongs to the bot it
// was sent to. Reports and the person's room messages stay.
func noteLeftByGoneBot(rec record, userID string, n BotCrossNote) bool {
	if n.Owner != userID {
		return false
	}
	switch n.Kind {
	case "say":
		return strings.TrimSpace(n.From) != "human" && !participantOwned(rec, userID, n.From)
	case "delegate", "result":
		target := strings.TrimSpace(n.Target)
		return target != "" && !participantOwned(rec, userID, target)
	default:
		return false
	}
}

// dropOrphanSays removes notes left by a bot id that is gone. "Bot 1" is a
// display name; identity is the id. The same slice is returned when nothing changes.
func dropOrphanSays(notes []BotCrossNote, rec record, userID string) []BotCrossNote {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(notes) == 0 {
		return notes
	}
	dropped := false
	out := make([]BotCrossNote, 0, len(notes))
	for _, n := range notes {
		if noteLeftByGoneBot(rec, userID, n) {
			dropped = true
			continue
		}
		out = append(out, n)
	}
	if !dropped {
		return notes
	}
	return out
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
		if n.Owner != userID || n.Target != target || (n.Kind != "say" && n.Kind != "room") {
			continue
		}
		// A say belongs to the speaker's bot id. "Bot 1" is only the name the
		// person sees; a new bot that reuses that name has a new id and does
		// not inherit a deleted id's lines (2026-11-14).
		if n.Kind == "say" && n.From != "human" && !participantOwned(rec, userID, n.From) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}
