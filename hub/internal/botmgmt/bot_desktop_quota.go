package botmgmt

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

// DesktopQuota is how many desktop bots one user may own, and how many they
// already own. Limit nil means unlimited.
type DesktopQuota struct {
	Limit *int `json:"limit"`
	Used  int  `json:"used"`
}

// desktopBotLimit is the limit on the most specific grant that covers the
// user: user, then department from nearest to root, then global. A matching
// grant with a nil limit is unlimited. No matching grant is also unlimited;
// access itself is decided elsewhere.
func desktopBotLimit(grants []Grant, userID string, departments []string) *int {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		for _, item := range grants {
			if item.Scope == ScopeUser && strings.TrimSpace(item.TargetID) == userID {
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
			if item.Scope == ScopeDepartment && strings.TrimSpace(item.TargetID) == groupID {
				return item.DesktopBotLimit
			}
		}
	}
	for _, item := range grants {
		if item.Scope == ScopeGlobal {
			return item.DesktopBotLimit
		}
	}
	return nil
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
	limit := desktopBotLimit(rec.Grants, ownerUserID, departments)
	if limit == nil {
		return nil
	}
	if countDesktopBots(rec.Bots, ownerUserID) >= *limit {
		return ErrDesktopQuota
	}
	return nil
}

// MarkDesktopOver copies bots and marks each desktop bot whose owner is
// already past the matching grant. A nil limit is not over. none bots are
// not marked. The returned slice is not the stored record.
func (s *Service) MarkDesktopOver(ctx context.Context, tenantID string, bots []Bot, grants []Grant) []Bot {
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
			limit := desktopBotLimit(grants, owner, s.departmentChainCached(ctx, owner, "", memo))
			used := countDesktopBots(bots, owner)
			flag = limit != nil && used > *limit
			over[owner] = flag
		}
		out[i].DesktopOver = flag
	}
	return out
}

// DesktopQuotaForUser is the quota the create-bot UI shows.
func (s *Service) DesktopQuotaForUser(ctx context.Context, tenantID, userID string) (DesktopQuota, error) {
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return DesktopQuota{}, err
	}
	userID = strings.TrimSpace(userID)
	departments := s.departmentChain(security.WithTenant(ctx, tenantID), userID)
	return DesktopQuota{
		Limit: desktopBotLimit(rec.Grants, userID, departments),
		Used:  countDesktopBots(rec.Bots, userID),
	}, nil
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
