package botmgmt

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"
)

// BotRoom is a group conversation between the person and some of their
// bots. Attention per member: "all" (wake on every message), "mentions"
// (only when @named; the default for sub-bots) or "off".
type BotRoom struct {
	ID        string            `json:"id"`
	Owner     string            `json:"owner"`
	Name      string            `json:"name"`
	Members   []string          `json:"members"`
	Attention map[string]string `json:"attention,omitempty"`
	CreatedAt string            `json:"created_at"`
}

func (r *BotRoom) hasMember(bot string) bool {
	for _, m := range r.Members {
		if m == bot {
			return true
		}
	}
	return false
}

func roomOf(rec record, userID, id string) *BotRoom {
	id = strings.TrimPrefix(strings.TrimSpace(id), "room:")
	if r := rec.Rooms[id]; r != nil && r.Owner == userID {
		return r
	}
	return nil
}

// Rooms (S5). The person writes in a room; Hub wakes the members whose
// attention lets them hear it ("all", or "mentions" when @named). Each
// woken bot gets the message with the recent room transcript and a list of
// who already answered, and replies with say room=<id>; Say refuses a
// repeat. MACLAW_BOT_ROOMS=off turns rooms off.

const (
	roomsMax          = 20
	roomMembersMax    = 8
	roomTranscriptMax = 8
)

func botRoomsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_BOT_ROOMS"))) {
	case "off", "0", "false", "no":
		return false
	}
	return botSayEnabled()
}

// RoomInput creates or changes a room; nil fields stay unchanged.
type RoomInput struct {
	Name      *string           `json:"name"`
	Members   []string          `json:"members"`
	Attention map[string]string `json:"attention"`
}

var roomAttention = map[string]bool{"all": true, "mentions": true, "off": true}

func (r *BotRoom) attention(bot, primary string) string {
	if a := r.Attention[bot]; roomAttention[a] {
		return a
	}
	if bot == primary {
		return "all"
	}
	return "mentions"
}

func (s *Service) ListRooms(ctx context.Context, tenantID, userID string) ([]BotRoom, error) {
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
	out := []BotRoom{}
	for _, r := range rec.Rooms {
		if r.Owner == userID {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// SaveRoom creates a room (id empty) or updates one.
func (s *Service) SaveRoom(ctx context.Context, tenantID, userID, id string, in RoomInput) (BotRoom, error) {
	if s == nil {
		return BotRoom{}, ErrSettingsUnavailable
	}
	if !botRoomsEnabled() {
		return BotRoom{}, ErrInvalidInput
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return BotRoom{}, err
	}
	var r *BotRoom
	if id = strings.TrimSpace(id); id != "" {
		if r = roomOf(rec, userID, id); r == nil {
			return BotRoom{}, ErrNotFound
		}
	} else {
		n := 0
		for _, x := range rec.Rooms {
			if x.Owner == userID {
				n++
			}
		}
		if n >= roomsMax {
			return BotRoom{}, ErrInvalidInput
		}
		r = &BotRoom{ID: strings.Replace(newCrossNoteID(), "note_", "room_", 1), Owner: userID, CreatedAt: s.now().UTC().Format(time.RFC3339)}
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if r := []rune(name); len(r) > 60 {
			name = string(r[:60])
		}
		r.Name = name
	}
	if in.Members != nil {
		seen := map[string]bool{}
		members := []string{}
		for _, m := range in.Members {
			id := resolveParticipant(rec, userID, strings.TrimSpace(m))
			if id == "" || !botOwned(rec.Bots, userID, id) {
				return BotRoom{}, ErrNoSuchBot
			}
			if !seen[id] {
				seen[id] = true
				members = append(members, id)
			}
		}
		if len(members) == 0 || len(members) > roomMembersMax {
			return BotRoom{}, ErrInvalidInput
		}
		r.Members = members
	}
	if len(r.Members) == 0 {
		return BotRoom{}, ErrInvalidInput
	}
	for k, v := range in.Attention {
		id := resolveParticipant(rec, userID, strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if !r.hasMember(id) || !roomAttention[v] {
			return BotRoom{}, ErrInvalidInput
		}
		if r.Attention == nil {
			r.Attention = map[string]string{}
		}
		r.Attention[id] = v
	}
	if r.Name == "" {
		r.Name = "群聊"
	}
	if rec.Rooms == nil {
		rec.Rooms = map[string]*BotRoom{}
	}
	rec.Rooms[r.ID] = r
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotRoom{}, err
	}
	return *r, nil
}

func (s *Service) DeleteRoom(ctx context.Context, tenantID, userID, id string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	r := roomOf(rec, userID, id)
	if r == nil {
		return ErrNotFound
	}
	delete(rec.Rooms, r.ID)
	return s.save(ctx, tenantID, rec)
}

// roomMentions returns the members @named in text (by name or id).
func roomMentions(rec record, userID string, r *BotRoom, text string) map[string]bool {
	out := map[string]bool{}
	low := strings.ToLower(text)
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		if !r.hasMember(b.ID) {
			continue
		}
		for _, h := range []string{b.Name, b.ID} {
			if h = strings.TrimSpace(h); h != "" && (strings.Contains(low, "@"+strings.ToLower(h)) || strings.Contains(low, "＠"+strings.ToLower(h))) {
				out[b.ID] = true
			}
		}
	}
	return out
}

// PostRoomMessage is the person writing in a room. It returns the stored
// message and the bots woken to hear it.
func (s *Service) PostRoomMessage(ctx context.Context, tenantID, userID, roomID, text string) (BotCrossNote, []string, error) {
	if s == nil {
		return BotCrossNote{}, nil, ErrSettingsUnavailable
	}
	if !botRoomsEnabled() {
		return BotCrossNote{}, nil, ErrInvalidInput
	}
	userID, text = strings.TrimSpace(userID), strings.TrimSpace(text)
	if r := []rune(text); len(r) > sayTextMax {
		text = string(r[:sayTextMax])
	}
	if userID == "" || text == "" {
		return BotCrossNote{}, nil, ErrInvalidInput
	}
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		s.mu.Unlock()
		return BotCrossNote{}, nil, err
	}
	r := roomOf(rec, userID, roomID)
	if r == nil {
		s.mu.Unlock()
		return BotCrossNote{}, nil, ErrNotFound
	}
	target := "room:" + r.ID
	now := s.now().UTC().Format(time.RFC3339)
	msg := BotCrossNote{ID: newCrossNoteID(), Owner: userID, From: "human", Target: target, Kind: "room", Text: text, CreatedAt: now}
	transcript := roomTranscript(rec, userID, target, botNames(rec, userID))
	rec.CrossNotes = append(rec.CrossNotes, msg)
	primary := primaryBotID(rec, userID)
	mentioned := roomMentions(rec, userID, r, text)
	var woken []string
	for _, m := range r.Members {
		att := r.attention(m, primary)
		if att == "off" && !mentioned[m] {
			continue
		}
		if len(mentioned) > 0 && !mentioned[m] {
			continue // the person addressed someone in particular
		}
		if len(mentioned) == 0 && att != "all" {
			continue
		}
		woken = append(woken, m)
	}
	if len(woken) == 0 && len(mentioned) == 0 {
		woken = append(woken, r.Members[0]) // someone always hears the person
	}
	for _, m := range woken {
		body := "[群聊 / room " + r.ID + " 「" + r.Name + "」] 用户说 / the person said: " + text
		if transcript != "" {
			body += "\n最近对话 / recent:\n" + transcript
		}
		if len(woken) > 1 {
			body += "\n同时听到的还有 / also hearing this: " + strings.Join(namesOf(rec, userID, woken, m), ", ") + "。只说自己独有的内容；别人已经回答的不要重复，没有新内容就保持沉默。"
		}
		body += "\n回复用 action=say room=" + r.ID + "。"
		rec.CrossNotes = append(rec.CrossNotes, BotCrossNote{ID: newCrossNoteID(), Owner: userID, From: "human", Target: m, Kind: "report", Topic: "task", Text: body, ReplyTo: msg.ID, Key: "room-" + msg.ID + "-" + m, CreatedAt: now})
	}
	rec.CrossNotes = capCrossNotes(rec.CrossNotes, userID)
	err = s.save(ctx, tenantID, rec)
	s.mu.Unlock()
	if err != nil {
		return BotCrossNote{}, nil, err
	}
	s.kickDispatch(tenantID, userID)
	return msg, woken, nil
}

func botNames(rec record, userID string) map[string]string {
	out := map[string]string{"human": "用户"}
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		out[b.ID] = firstNonEmptyString(b.Name, b.ID)
	}
	return out
}

func namesOf(rec record, userID string, ids []string, except string) []string {
	names := botNames(rec, userID)
	out := []string{}
	for _, id := range ids {
		if id != except {
			out = append(out, names[id])
		}
	}
	return out
}

// roomTranscript is the last few room lines, oldest first.
func roomTranscript(rec record, userID, target string, names map[string]string) string {
	var lines []string
	for i := len(rec.CrossNotes) - 1; i >= 0 && len(lines) < roomTranscriptMax; i-- {
		n := rec.CrossNotes[i]
		if n.Owner != userID || n.Target != target || (n.Kind != "say" && n.Kind != "room") {
			continue
		}
		t := []rune(n.Text)
		if len(t) > 200 {
			t = append(t[:200], '…')
		}
		lines = append(lines, firstNonEmptyString(names[n.From], n.From)+": "+string(t))
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}

// RoomAnsweredBy lists the bots that already spoke in a room since the
// person last wrote (the "already answered" hint).
func RoomAnsweredBy(notes []BotCrossNote, userID, target string) []string {
	var out []string
	for i := len(notes) - 1; i >= 0; i-- {
		n := notes[i]
		if n.Owner != userID || n.Target != target {
			continue
		}
		if n.Kind == "room" {
			break
		}
		if n.Kind == "say" {
			out = append(out, n.From)
		}
	}
	return out
}
