package botmgmt

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var (
	botColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	botShapePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
)

// NormalizeBotAppearance validates a badge look. Empty values clear that part.
// Colors are #rrggbb; shapes are short lowercase identifiers so a newer GUI
// can add a shape without a Hub change.
func NormalizeBotAppearance(color, shape string) (string, string, error) {
	color = strings.ToLower(strings.TrimSpace(color))
	shape = strings.ToLower(strings.TrimSpace(shape))
	if color != "" && !botColorPattern.MatchString(color) {
		return "", "", fmt.Errorf("%w: color must be #rrggbb", ErrInvalidInput)
	}
	if shape != "" && !botShapePattern.MatchString(shape) {
		return "", "", fmt.Errorf("%w: invalid shape", ErrInvalidInput)
	}
	return color, shape, nil
}

// SetBotAppearanceForUser stores the owner's badge look for one bot. Nil
// leaves that part unchanged. It is Hub-only data: MaClawSrv is not called.
func (s *Service) SetBotAppearanceForUser(ctx context.Context, tenantID, userID, botID string, color, shape *string) (Bot, error) {
	if s == nil {
		return Bot{}, ErrSettingsUnavailable
	}
	var c, sh string
	if color != nil {
		c = *color
	}
	if shape != nil {
		sh = *shape
	}
	c, sh, err := NormalizeBotAppearance(c, sh)
	if err != nil {
		return Bot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return Bot{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return Bot{}, ErrNotFound
	}
	bot := rec.Bots[index]
	if color != nil {
		bot.Color = c
	}
	if shape != nil {
		bot.Shape = sh
	}
	if bot == rec.Bots[index] {
		return withDesktopModeView(bot), nil
	}
	rec.Bots[index] = bot
	if err := s.save(ctx, tenantID, rec); err != nil {
		return Bot{}, err
	}
	return withDesktopModeView(bot), nil
}
