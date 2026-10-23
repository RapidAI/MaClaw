package botmgmt

// bot_local.go: local sub-bots. A bot that runs inside the MaClaw GUI on the
// person's own machine (local files, local network) has no MaClawSrv
// instance, but it is a full mailbox participant on Hub: it is registered
// here by its GUI, has a Hub mailbox like any other bot, can be the target
// of a report or a delegated order (stored as a report with topic "task"),
// and sends reports through the normal user cross route. Hub never starts a
// run for it; its GUI reads its mailbox and wakes it (the dispatcher only
// admits runs for bots with an instance).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const localBotsPerUser = 20

// LocalBot is a GUI-hosted bot on one of the owner's machines.
type LocalBot struct {
	ID           string   `json:"id"`
	Owner        string   `json:"owner"`
	Name         string   `json:"name"`
	Device       string   `json:"device,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	CreatedAt    string   `json:"created_at"`
	SeenAt       string   `json:"seen_at,omitempty"`
}

func localBotID(userID, device, name string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(device) + "\x00" + strings.ToLower(strings.TrimSpace(name))))
	return "lb_" + hex.EncodeToString(sum[:6])
}

func localOwned(rec record, userID, id string) bool {
	id = strings.TrimSpace(id)
	for _, b := range rec.LocalBots {
		if b.ID == id && b.Owner == strings.TrimSpace(userID) {
			return true
		}
	}
	return false
}

// participantOwned: a Hub bot or a local bot of this user.
func participantOwned(rec record, userID, id string) bool {
	return botOwned(rec.Bots, userID, id) || localOwned(rec, userID, id)
}

// resolveParticipant turns a name into a Hub or local bot id of this user.
func resolveParticipant(rec record, userID, target string) string {
	t := strings.TrimSpace(target)
	if t == "" || participantOwned(rec, userID, t) {
		return t
	}
	for _, bot := range botsOwnedBy(rec.Bots, userID) {
		if strings.EqualFold(strings.TrimSpace(bot.Name), t) {
			return bot.ID
		}
	}
	for _, b := range rec.LocalBots {
		if b.Owner == strings.TrimSpace(userID) && strings.EqualFold(b.Name, t) {
			return b.ID
		}
	}
	return t
}

// RegisterLocalBot adds or refreshes one local bot (a GUI heartbeat).
func (s *Service) RegisterLocalBot(ctx context.Context, tenantID, userID string, in LocalBot) (LocalBot, error) {
	if s == nil {
		return LocalBot{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	name := strings.TrimSpace(in.Name)
	if userID == "" || name == "" || len([]rune(name)) > 40 {
		return LocalBot{}, ErrInvalidInput
	}
	device := strings.TrimSpace(in.Device)
	if r := []rune(device); len(r) > 60 {
		device = string(r[:60])
	}
	caps := []string{}
	for _, c := range in.Capabilities {
		if c = strings.TrimSpace(c); c != "" && len(caps) < 12 {
			caps = append(caps, c)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return LocalBot{}, err
	}
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		if strings.EqualFold(strings.TrimSpace(b.Name), name) {
			return LocalBot{}, ErrInvalidInput // a Hub bot already has this name
		}
	}
	now := s.now().UTC().Format(time.RFC3339)
	id := localBotID(userID, device, name)
	mine := 0
	for i := range rec.LocalBots {
		b := &rec.LocalBots[i]
		if b.Owner != userID {
			continue
		}
		mine++
		if b.ID == id {
			b.Capabilities, b.SeenAt = caps, now
			out := *b
			return out, s.save(ctx, tenantID, rec)
		}
	}
	if mine >= localBotsPerUser {
		return LocalBot{}, ErrInvalidInput
	}
	b := LocalBot{ID: id, Owner: userID, Name: name, Device: device, Capabilities: caps, CreatedAt: now, SeenAt: now}
	rec.LocalBots = append(rec.LocalBots, b)
	return b, s.save(ctx, tenantID, rec)
}

// ListLocalBots returns this user's local bots.
func (s *Service) ListLocalBots(ctx context.Context, tenantID, userID string) ([]LocalBot, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	out := []LocalBot{}
	for _, b := range rec.LocalBots {
		if b.Owner == strings.TrimSpace(userID) {
			out = append(out, b)
		}
	}
	return out, nil
}

// RemoveLocalBot unregisters one local bot. Its unread mail expires normally.
func (s *Service) RemoveLocalBot(ctx context.Context, tenantID, userID, id string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	kept := rec.LocalBots[:0]
	found := false
	for _, b := range rec.LocalBots {
		if b.ID == strings.TrimSpace(id) && b.Owner == strings.TrimSpace(userID) {
			found = true
			continue
		}
		kept = append(kept, b)
	}
	if !found {
		return ErrNoSuchBot
	}
	rec.LocalBots = kept
	return s.save(ctx, tenantID, rec)
}
