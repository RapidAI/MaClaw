package botmgmt

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

// DesktopQuota is how many desktop bots one user may own, and how many they
// already own. Limit nil means unlimited.
type DesktopQuota struct {
	Limit *int `json:"limit"`
	Used  int  `json:"used"`
	// CloudDesktop reports whether the owner has an assigned cloud computer.
	// It is true when no check is wired so a hub without the pool never hides
	// the option. LAN nodes are a separate path the client lists itself.
	CloudDesktop bool `json:"cloud_desktop"`
	// LanDesktop reports whether the owner's pinned bot node is a usable LAN
	// computer (srv plus a registered desktopd). A srv-only node is not one:
	// desktop:true creation falls back to the cloud instead of landing there.
	// Clients that cannot pick a node read cloud_desktop || lan_desktop as
	// "a computer bot is possible".
	LanDesktop bool `json:"lan_desktop"`
}

// desktopBotLimit resolves the user's effective cap as a cascade: the user
// grant first, then department grants from nearest to root, then the global
// grant, then the tenant default. A blank (nil) limit inherits the next
// level; every level blank is unlimited. No covering grant is also
// unlimited — access itself is decided elsewhere.
func desktopBotLimit(grants []Grant, defaultLimit *int, userID string, departments []string) *int {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		for _, item := range grants {
			if item.Scope == ScopeUser && strings.TrimSpace(item.TargetID) == userID && item.DesktopBotLimit != nil {
				return item.DesktopBotLimit
			}
		}
	}
	for _, groupID := range departments {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		for _, item := range grants {
			if item.Scope == ScopeDepartment && strings.TrimSpace(item.TargetID) == groupID && item.DesktopBotLimit != nil {
				return item.DesktopBotLimit
			}
		}
	}
	for _, item := range grants {
		if item.Scope == ScopeGlobal && item.DesktopBotLimit != nil {
			return item.DesktopBotLimit
		}
	}
	return defaultLimit
}

func countDesktopBots(bots []Bot, userID string) int {
	userID = strings.TrimSpace(userID)
	n := 0
	for _, bot := range bots {
		if strings.TrimSpace(bot.OwnerUserID) != userID {
			continue
		}
		if botNone(bot) {
			continue
		}
		n++
	}
	return n
}

// desktopQuotaAllows reports ErrDesktopQuota when ownerUserID already owns
// as many desktop bots as the matching grant allows. departments is the
// caller's lookup: this does not call the directory, so it can run while
// the bot lock is held. A nil limit or an empty owner allows the bot.
func desktopQuotaAllows(rec record, ownerUserID string, departments []string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil
	}
	limit := desktopBotLimit(rec.Grants, rec.DefaultDesktopBotLimit, ownerUserID, departments)
	if limit == nil {
		return nil
	}
	if countDesktopBots(rec.Bots, ownerUserID) >= *limit {
		return ErrDesktopQuota
	}
	return nil
}

// MarkDesktopOver copies bots and marks each desktop bot whose owner is
// already past the effective limit. A nil resolved limit is not over. none
// bots are not marked. The returned slice is not the stored record.
func (s *Service) MarkDesktopOver(ctx context.Context, tenantID string, bots []Bot, grants []Grant, defaultLimit *int) []Bot {
	if len(bots) == 0 {
		return bots
	}
	out := make([]Bot, len(bots))
	copy(out, bots)
	ctx = directoryContext(ctx, tenantID)
	over := map[string]bool{}
	memo := &parentMemo{}
	for i := range out {
		out[i].DesktopOver = false
		owner := strings.TrimSpace(out[i].OwnerUserID)
		if owner == "" || botNone(out[i]) {
			continue
		}
		flag, ok := over[owner]
		if !ok {
			limit := desktopBotLimit(grants, defaultLimit, owner, s.departmentChainCached(ctx, owner, "", memo))
			used := countDesktopBots(bots, owner)
			flag = limit != nil && used > *limit
			over[owner] = flag
		}
		out[i].DesktopOver = flag
	}
	return out
}

const desktopOverWarning = "这个用户名下要电脑的 bot 已经超过额度。这次设置仍然生效。"

// DesktopOverWarning is the sentence an admin response includes when saving
// a desktop mode leaves the owner past the grant. A none bot and an owner
// inside the grant return an empty string. The save itself is not rejected.
func (s *Service) DesktopOverWarning(ctx context.Context, tenantID, userID, mode string) string {
	mode, err := NormalizeDesktopMode(mode)
	if s == nil || err != nil || mode == desktop.BotDesktopModeNone || strings.TrimSpace(userID) == "" {
		return ""
	}
	quota, err := s.DesktopQuotaForUser(ctx, tenantID, userID)
	if err != nil || quota.Limit == nil || quota.Used <= *quota.Limit {
		return ""
	}
	return desktopOverWarning
}

// DesktopQuotaForUser is the quota the create-bot UI shows.
func (s *Service) DesktopQuotaForUser(ctx context.Context, tenantID, userID string) (DesktopQuota, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return DesktopQuota{}, err
	}
	userID = strings.TrimSpace(userID)
	departments := s.departmentChain(security.WithTenant(ctx, tenantID), userID)
	quota := DesktopQuota{
		Limit:        desktopBotLimit(rec.Grants, rec.DefaultDesktopBotLimit, userID, departments),
		Used:         countDesktopBots(rec.Bots, userID),
		CloudDesktop: true,
	}
	if s.CloudDesktopAssigned != nil {
		quota.CloudDesktop = s.CloudDesktopAssigned(ctx, tenantID, userID, departments) == nil
	}
	if pin := ownerNodeID(rec, userID); pin != "" && s.LanDesktopAvailable != nil {
		quota.LanDesktop = s.LanDesktopAvailable(ctx, tenantID, pin)
	}
	return quota, nil
}

// UpdateGrant replaces one grant's desktop-bot limit. Other fields stay.
// A nil limit clears the cap.
func (s *Service) UpdateGrant(ctx context.Context, tenantID, grantID string, limit *int) (Grant, error) {
	if limit != nil && *limit < 0 {
		return Grant{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return Grant{}, err
	}
	grantID = strings.TrimSpace(grantID)
	for i := range rec.Grants {
		if rec.Grants[i].ID != grantID {
			continue
		}
		rec.Grants[i].DesktopBotLimit = limit
		if err := s.save(ctx, tenantID, rec); err != nil {
			return Grant{}, err
		}
		return rec.Grants[i], nil
	}
	return Grant{}, ErrNotFound
}
