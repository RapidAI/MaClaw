package botmgmt

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BotSchedule is one timer on the MaClawSrv clock for a single bot id.
// Display names are not part of the key. A recreated "Bot 1" does not see
// a deleted id's timers.
type BotSchedule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Action          string `json:"action"`
	IntervalMinutes int    `json:"interval_minutes"`
	Hour            int    `json:"hour"`
	Minute          int    `json:"minute"`
	DayOfWeek       int    `json:"day_of_week"`
	NextRunAt       string `json:"next_run_at,omitempty"`
	Status          string `json:"status,omitempty"`
}

// ListBotSchedules reads the host clock for this bot. It does not write the
// tenant record. A bot with no instance has no timers.
func (s *Service) ListBotSchedules(ctx context.Context, tenantID, userID, botID string) ([]BotSchedule, error) {
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
	bot := rec.Bots[index]
	instanceID := strings.TrimSpace(bot.InstanceID)
	if instanceID == "" {
		return []BotSchedule{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []BotSchedule `json:"items"`
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/bot-schedules?bot_id=" + url.QueryEscape(bot.ID)
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []BotSchedule{}
	}
	// After the list is in hand. A retain failure must not hide the timers.
	s.sweepOrphanSchedules(ctx, tenantID, userID, fresh, token)
	return out.Items, nil
}

// scheduleSweepGap is how often one user's panel asks the host to drop
// timers whose bot id is no longer in the tenant. The panel polls faster
// than this.
const scheduleSweepGap = 10 * time.Minute

func (s *Service) claimScheduleSweep(tenantID, userID string) bool {
	if s == nil {
		return false
	}
	key := strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
	now := s.now()
	s.scheduleSweepMu.Lock()
	defer s.scheduleSweepMu.Unlock()
	if s.scheduleSweep == nil {
		s.scheduleSweep = map[string]time.Time{}
	}
	if last, ok := s.scheduleSweep[key]; ok && now.Sub(last) < scheduleSweepGap {
		return false
	}
	s.scheduleSweep[key] = now
	return true
}

// sweepOrphanSchedules asks the host to drop timers on this user's instances
// whose bot id is gone. The live set is every bot still stored for the
// tenant, so another person's bot on a shared instance is left in place.
// An empty set is not sent: that would be read as "keep nothing".
func (s *Service) sweepOrphanSchedules(ctx context.Context, tenantID, userID string, rec record, token string) {
	if s == nil || ctx.Err() != nil {
		return
	}
	live := make([]string, 0, len(rec.Bots))
	seen := map[string]bool{}
	instances := map[string]bool{}
	for _, bot := range rec.Bots {
		id := strings.TrimSpace(bot.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			live = append(live, id)
		}
		if bot.OwnerUserID == userID {
			if inst := strings.TrimSpace(bot.InstanceID); inst != "" {
				instances[inst] = true
			}
		}
	}
	if len(live) == 0 || len(instances) == 0 {
		return
	}
	if !s.claimScheduleSweep(tenantID, userID) {
		return
	}
	sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body := map[string]any{"bot_ids": live}
	okN, failN := 0, 0
	for inst := range instances {
		path := "/api/v1/instances/" + url.PathEscape(inst) + "/bot-schedules/retain"
		if err := s.doAuth(sweepCtx, rec, token, http.MethodPost, path, body, nil); err != nil {
			failN++
			log.Printf("[botmgmt] retain schedules instance=%s: %v", inst, err)
			continue
		}
		okN++
	}
	// A failed retain used to occupy the 10-minute gap, so a restart left
	// deleted bots' timers running until the next gap. Retry those sooner.
	if okN == 0 && failN > 0 {
		s.noteScheduleSweep(tenantID, userID, time.Minute)
	}
}

// noteScheduleSweep sets the next sweep to happen after gap. claimScheduleSweep
// stores the previous time and allows another sweep once scheduleSweepGap
// has passed, so a shorter gap is recorded as an older timestamp.
func (s *Service) noteScheduleSweep(tenantID, userID string, gap time.Duration) {
	if s == nil {
		return
	}
	if gap <= 0 || gap > scheduleSweepGap {
		gap = scheduleSweepGap
	}
	key := strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
	s.scheduleSweepMu.Lock()
	defer s.scheduleSweepMu.Unlock()
	if s.scheduleSweep == nil {
		s.scheduleSweep = map[string]time.Time{}
	}
	s.scheduleSweep[key] = s.now().Add(gap - scheduleSweepGap)
}

// DeleteBotSchedule removes one timer from the host clock. The id has to
// belong to this bot; the host refuses another bot's row.
func (s *Service) DeleteBotSchedule(ctx context.Context, tenantID, userID, botID, taskID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ErrNotFound
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	bot := rec.Bots[index]
	if strings.TrimSpace(bot.InstanceID) == "" {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	path := "/api/v1/instances/" + url.PathEscape(bot.InstanceID) + "/bot-schedules/" + url.PathEscape(taskID) + "?bot_id=" + url.QueryEscape(bot.ID)
	return s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodDelete, path, nil, nil)
}

// cancelBotSchedules stops every timer of a bot that is about to be deleted.
// A failure leaves DeleteBot free to continue: the instance is going away,
// and a later list for a new id cannot see these rows.
func (s *Service) cancelBotSchedules(ctx context.Context, rec record, token string, bot Bot) error {
	if strings.TrimSpace(bot.InstanceID) == "" || strings.TrimSpace(bot.ID) == "" {
		return nil
	}
	path := "/api/v1/instances/" + url.PathEscape(bot.InstanceID) + "/bot-schedules?bot_id=" + url.QueryEscape(bot.ID)
	return s.doAuth(ctx, rec, token, http.MethodDelete, path, nil, nil)
}
