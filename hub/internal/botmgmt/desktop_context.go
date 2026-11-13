package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// DesktopConversation reads the conversation MaClawSrv stores for this bot.
// Hub does not keep a second copy. Every GUI reads this same snapshot.
func (s *Service) DesktopConversation(ctx context.Context, tenantID, userID, botID string) (json.RawMessage, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return nil, ErrNotFound
	}
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return nil, ErrNotFound
	}
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/bot-context?client_session_key=" + url.QueryEscape(strings.TrimSpace(botID))
	var out json.RawMessage
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
