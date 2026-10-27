package botmgmt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
)

// botStyleKeep is how many earlier versions a bot's style keeps.
const botStyleKeep = 20

// BotStyleVersion is one saved state of a bot's style.
type BotStyleVersion struct {
	Version   int               `json:"version"`
	Soul      string            `json:"soul,omitempty"`
	Memory    string            `json:"memory,omitempty"`
	Parts     map[string]string `json:"parts,omitempty"`
	BaseHash  string            `json:"base_hash,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
	UpdatedBy string            `json:"updated_by,omitempty"`
}

type botStyleRecord struct {
	Current BotStyleVersion   `json:"current"`
	History []BotStyleVersion `json:"history,omitempty"`
}

// BotStyleView is what the API returns: the stored style, the defaults it
// falls back to and whether the defaults changed since it was written.
type BotStyleView struct {
	BotID        string            `json:"bot_id"`
	Current      BotStyleVersion   `json:"current"`
	History      []BotStyleVersion `json:"history,omitempty"`
	DefaultSoul  string            `json:"default_soul"`
	DefaultParts map[string]string `json:"default_parts"`
	BaseHash     string            `json:"base_hash"`
	BaseChanged  bool              `json:"base_changed"`
	Enabled      bool              `json:"enabled"`
}

// BotStyleInput changes a style. Nil leaves that field as it is; Parts
// replaces the whole override map when set.
type BotStyleInput struct {
	Soul   *string            `json:"soul"`
	Memory *string            `json:"memory"`
	Parts  *map[string]string `json:"parts"`
}

// botStyleEnabled is the Hub side of the style kill switch.
func botStyleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_STYLE_LAYER"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

func (s *Service) ownedBot(rec *record, userID, botID string) bool {
	i := indexOf(rec.Bots, botID)
	if i >= 0 && rec.Bots[i].OwnerUserID == userID {
		return true
	}
	for _, lb := range rec.LocalBots {
		if lb.ID == botID && lb.Owner == userID {
			return true
		}
	}
	return false
}

func botStyleView(botID string, r *botStyleRecord) BotStyleView {
	v := BotStyleView{BotID: botID, DefaultSoul: promptparts.DefaultSoul, DefaultParts: map[string]string{}, BaseHash: promptparts.BaseHash(), Enabled: botStyleEnabled()}
	for _, id := range promptparts.IDs() {
		v.DefaultParts[string(id)] = promptparts.Default(id)
	}
	if r != nil {
		v.Current = r.Current
		v.History = append([]BotStyleVersion(nil), r.History...)
		v.BaseChanged = len(r.Current.Parts) > 0 && r.Current.BaseHash != "" && r.Current.BaseHash != v.BaseHash
	}
	return v
}

// GetBotStyleForUser returns one owned bot's style.
func (s *Service) GetBotStyleForUser(ctx context.Context, tenantID, userID, botID string) (BotStyleView, error) {
	if s == nil {
		return BotStyleView{}, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return BotStyleView{}, err
	}
	if !s.ownedBot(&rec, userID, botID) {
		return BotStyleView{}, ErrNotFound
	}
	return botStyleView(botID, rec.Styles[botID]), nil
}

func normalizeStyleParts(in map[string]string) (map[string]string, error) {
	known := map[string]bool{}
	for _, id := range promptparts.IDs() {
		known[string(id)] = true
	}
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		if !known[k] {
			return nil, fmt.Errorf("%w: unknown prompt part %q", ErrInvalidInput, k)
		}
		if v = strings.TrimSpace(v); v != "" {
			out[k] = promptparts.Clip(v, promptparts.MaxPartRunes)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SetBotStyleForUser saves a new version of one owned bot's style; the old
// one goes to history.
func (s *Service) SetBotStyleForUser(ctx context.Context, tenantID, userID, botID string, in BotStyleInput) (BotStyleView, error) {
	if s == nil {
		return BotStyleView{}, ErrSettingsUnavailable
	}
	var parts map[string]string
	if in.Parts != nil {
		var err error
		if parts, err = normalizeStyleParts(*in.Parts); err != nil {
			return BotStyleView{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return BotStyleView{}, err
	}
	if !s.ownedBot(&rec, userID, botID) {
		return BotStyleView{}, ErrNotFound
	}
	r := rec.Styles[botID]
	if r == nil {
		r = &botStyleRecord{}
	}
	next := r.Current
	if in.Soul != nil {
		next.Soul = promptparts.Clip(*in.Soul, promptparts.SoulMaxRunes)
	}
	if in.Memory != nil {
		next.Memory = promptparts.Clip(*in.Memory, promptparts.MemoryMaxRunes)
	}
	if in.Parts != nil {
		next.Parts = parts
	}
	s.pushStyle(r, next, userID)
	if rec.Styles == nil {
		rec.Styles = map[string]*botStyleRecord{}
	}
	rec.Styles[botID] = r
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotStyleView{}, err
	}
	return botStyleView(botID, r), nil
}

// RollbackBotStyleForUser makes an earlier version current again, as a new
// version, so the rollback itself can be undone.
func (s *Service) RollbackBotStyleForUser(ctx context.Context, tenantID, userID, botID string, version int) (BotStyleView, error) {
	if s == nil {
		return BotStyleView{}, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return BotStyleView{}, err
	}
	if !s.ownedBot(&rec, userID, botID) {
		return BotStyleView{}, ErrNotFound
	}
	r := rec.Styles[botID]
	if r == nil {
		return BotStyleView{}, ErrNotFound
	}
	var found *BotStyleVersion
	for i := range r.History {
		if r.History[i].Version == version {
			found = &r.History[i]
		}
	}
	if found == nil {
		return BotStyleView{}, fmt.Errorf("%w: no style version %d", ErrInvalidInput, version)
	}
	s.pushStyle(r, *found, userID)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return BotStyleView{}, err
	}
	return botStyleView(botID, r), nil
}

func (s *Service) pushStyle(r *botStyleRecord, next BotStyleVersion, userID string) {
	if r.Current.Version > 0 {
		r.History = append(r.History, r.Current)
		if len(r.History) > botStyleKeep {
			r.History = r.History[len(r.History)-botStyleKeep:]
		}
	}
	next.Version = r.Current.Version + 1
	next.BaseHash = promptparts.BaseHash()
	next.UpdatedAt = time.Now().UTC()
	next.UpdatedBy = userID
	r.Current = next
}

// botStyleMeta is the style a dispatched turn carries ("" map when the
// layer is off or nothing is stored, so the run uses the defaults).
func botStyleMeta(rec *record, botID string) map[string]string {
	if !botStyleEnabled() || rec == nil {
		return nil
	}
	r := rec.Styles[botID]
	if r == nil {
		return nil
	}
	return promptparts.Style{Soul: r.Current.Soul, Memory: r.Current.Memory, Parts: r.Current.Parts}.Meta()
}
