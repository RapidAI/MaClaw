package botmgmt

import "strings"

func desktopAttentionKey(tenantID, userID, botID string) string {
	return desktopViewKey(tenantID, userID) + "\x00" + strings.TrimSpace(botID)
}

func (s *Service) noteDesktopAttention(tenantID, userID, botID, reason string) {
	if s == nil {
		return
	}
	botID = strings.TrimSpace(botID)
	reason = strings.TrimSpace(reason)
	if botID == "" || reason == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopAttention == nil {
		s.desktopAttention = map[string]string{}
	}
	s.desktopAttention[desktopAttentionKey(tenantID, userID, botID)] = reason
}

func (s *Service) forgetDesktopAttentionLocked(tenantID, userID, botID string) {
	if s == nil || s.desktopAttention == nil {
		return
	}
	delete(s.desktopAttention, desktopAttentionKey(tenantID, userID, botID))
}

// DesktopAttention is the screen-handoff reason for this bot, if one is current.
func (s *Service) DesktopAttention(tenantID, userID, botID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktopAttention == nil {
		return ""
	}
	return strings.TrimSpace(s.desktopAttention[desktopAttentionKey(tenantID, userID, botID)])
}
