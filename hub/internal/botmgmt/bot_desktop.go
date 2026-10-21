package botmgmt

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopBotMetadataKey on a MaClawSrv instance names the bot whose own
// display this instance drives (desktop_mode=isolated). Absent means the
// owner's desktop.
const desktopBotMetadataKey = "hub_desktop_bot"

// desktopMaterialsMetadataKey names the bot's folder under ~/素材 on the
// desktop (desktop.MaterialsFolder), for MaClawSrv's run trajectories.
const desktopMaterialsMetadataKey = "hub_materials_folder"

func botIsolated(bot Bot) bool {
	return strings.TrimSpace(bot.DesktopMode) == desktop.BotDesktopModeIsolated
}

// desktopUserForBot is the desktop user whose state this bot's commands use:
// the owner for a shared bot, owner+bot for an isolated one.
func desktopUserForBot(userID string, bot Bot) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	if botIsolated(bot) {
		return desktop.BotDesktopUser(owner, bot.ID)
	}
	return owner
}

// desktopUserOf is desktopUserForBot when only the bot id is at hand.
func (s *Service) desktopUserOf(tenantID, userID, botID string) string {
	if s == nil || strings.TrimSpace(botID) == "" {
		return userID
	}
	rec, err := s.load(context.Background(), tenantID)
	if err != nil {
		return userID
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		return userID
	}
	return desktopUserForBot(userID, rec.Bots[index])
}

// NormalizeDesktopMode accepts "", "shared" and "isolated".
func NormalizeDesktopMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", desktop.BotDesktopModeShared:
		return "", nil
	case desktop.BotDesktopModeIsolated:
		return desktop.BotDesktopModeIsolated, nil
	}
	return "", fmt.Errorf("%w: desktop_mode must be shared or isolated", ErrInvalidInput)
}

// SetBotDesktopMode switches one bot between the owner's desktop (shared)
// and its own display (isolated). The instance learns it on its next
// command, when Hub rewrites the instance's desktop identity.
func (s *Service) SetBotDesktopMode(ctx context.Context, tenantID, botID, mode string) (Bot, error) {
	mode, err := NormalizeDesktopMode(mode)
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
	if index < 0 {
		return Bot{}, ErrNotFound
	}
	rec.Bots[index].DesktopMode = mode
	if err := s.save(ctx, tenantID, rec); err != nil {
		return Bot{}, err
	}
	return rec.Bots[index], nil
}

// BotDesktopMode reports a bot's desktop mode ("shared" or "isolated").
func (s *Service) BotDesktopMode(ctx context.Context, tenantID, userID, botID string) (string, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return "", err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != strings.TrimSpace(userID) {
		return "", ErrNotFound
	}
	if botIsolated(rec.Bots[index]) {
		return desktop.BotDesktopModeIsolated, nil
	}
	return desktop.BotDesktopModeShared, nil
}

// IsolatedDesktopUser is the desktop user for (owner, bot) when that bot is
// isolated and belongs to the owner, else the owner. Hub's desktop-service
// API uses it for an explicit bot_id.
func (s *Service) IsolatedDesktopUser(ctx context.Context, tenantID, userID, botID string) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	if s == nil || strings.TrimSpace(botID) == "" {
		return owner
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return owner
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		// MaClawSrv may name the bot by its instance id.
		for i, bot := range rec.Bots {
			if bot.InstanceID == strings.TrimSpace(botID) {
				index = i
				break
			}
		}
	}
	if index < 0 || rec.Bots[index].OwnerUserID != owner {
		return owner
	}
	return desktopUserForBot(owner, rec.Bots[index])
}

// DesktopCtl is the optional control channel of DesktopControl (Hub's
// desktop pool): one supervisor control op on a desktop user's display.
type DesktopCtl interface {
	Ctl(ctx context.Context, tenantID, userID string, request map[string]any) (map[string]any, error)
}

// HandBackDesktop ends a person's takeover of an isolated bot's display: the
// bot may drive it again. The keyboard pin of a login handoff is released the
// same way the next command releases it. Shared bots have no takeover state
// on the display; for them this only clears the pin.
func (s *Service) HandBackDesktop(ctx context.Context, tenantID, userID, botID string) error {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != strings.TrimSpace(userID) {
		return ErrNotFound
	}
	bot := rec.Bots[index]
	dUser := desktopUserForBot(userID, bot)
	if botIsolated(bot) {
		if ctl, ok := s.Desktop.(DesktopCtl); ok && ctl != nil {
			if _, err := ctl.Ctl(ctx, tenantID, dUser, map[string]any{"op": desktop.CtlTakeoverEnd}); err != nil {
				return fmt.Errorf("%w: %s", ErrSrv, err.Error())
			}
		}
	}
	s.releaseDesktopKeyboard(tenantID, dUser, bot.ID)
	return nil
}

// withDesktopModeView spells out the default for API responses: a stored
// empty mode is "shared".
func withDesktopModeView(bot Bot) Bot {
	if botIsolated(bot) {
		bot.DesktopMode = desktop.BotDesktopModeIsolated
	} else {
		bot.DesktopMode = desktop.BotDesktopModeShared
	}
	return bot
}

// SetBotDesktopModeForUser is SetBotDesktopMode for the bot's own owner.
// Another user's bot is not found.
func (s *Service) SetBotDesktopModeForUser(ctx context.Context, tenantID, userID, botID, mode string) (Bot, error) {
	if _, err := NormalizeDesktopMode(mode); err != nil {
		return Bot{}, err
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return Bot{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != strings.TrimSpace(userID) || strings.TrimSpace(userID) == "" {
		return Bot{}, ErrNotFound
	}
	bot, err := s.SetBotDesktopMode(ctx, tenantID, botID, mode)
	if err != nil {
		return bot, err
	}
	return withDesktopModeView(bot), nil
}

// ownerDefaultDesktopMode is the mode a new bot of ownerUserID starts with.
func ownerDefaultDesktopMode(rec record, ownerUserID string) string {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return ""
	}
	mode, err := NormalizeDesktopMode(rec.OwnerDesktopModeDefaults[ownerUserID])
	if err != nil {
		return ""
	}
	return mode
}

// SetOwnerDesktopModeDefault sets the desktop_mode new bots of one owner
// start with. "shared" or "" removes the default. Existing bots keep theirs.
func (s *Service) SetOwnerDesktopModeDefault(ctx context.Context, tenantID, ownerUserID, mode string) error {
	mode, err := NormalizeDesktopMode(mode)
	if err != nil {
		return err
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if mode == "" {
		delete(rec.OwnerDesktopModeDefaults, ownerUserID)
	} else {
		if rec.OwnerDesktopModeDefaults == nil {
			rec.OwnerDesktopModeDefaults = map[string]string{}
		}
		rec.OwnerDesktopModeDefaults[ownerUserID] = mode
	}
	return s.save(ctx, tenantID, rec)
}
