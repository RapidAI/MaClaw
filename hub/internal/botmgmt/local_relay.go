package botmgmt

import (
	"context"
	"strings"
)

// BotByInstance returns the owner's bot whose MaClawSrv instance is
// instanceID (or whose id is instanceID). The local relay names the bot to
// the person by this record, never by what MaClawSrv claims.
func (s *Service) BotByInstance(ctx context.Context, tenantID, userID, instanceID string) (Bot, error) {
	if s == nil {
		return Bot{}, ErrSettingsUnavailable
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return Bot{}, ErrNotFound
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	s.mu.Unlock()
	if err != nil {
		return Bot{}, err
	}
	for _, bot := range botsOwnedBy(rec.Bots, strings.TrimSpace(userID)) {
		if strings.TrimSpace(bot.InstanceID) == instanceID || bot.ID == instanceID {
			return bot, nil
		}
	}
	return Bot{}, ErrNotFound
}
