package botmgmt

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ActiveDesktopRun is one ongoing piece of work for a bot's 任务 tab.
//
// Source "desktop" is a run this Hub admitted and still follows. Source
// "server" is a run MaClawSrv still records as running that this Hub does not
// follow, which is usually a run whose process ended (a restart) without
// closing the record. Status "queued" is an admission still being posted: it
// has no run id yet and is cancelled through the admit-cancel path.
type ActiveDesktopRun struct {
	RunID          string     `json:"run_id"`
	BotID          string     `json:"bot_id"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	Task           string     `json:"task,omitempty"`
	Step           string     `json:"step,omitempty"`
	NextStep       string     `json:"next_step,omitempty"`
	Phase          string     `json:"phase,omitempty"`
	WaitingForUser bool       `json:"waiting_for_user,omitempty"`
	Stale          bool       `json:"stale,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
}

type srvRunRow struct {
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	WaitingForUser bool              `json:"waiting_for_user"`
	StartedAt      time.Time         `json:"started_at"`
	Metadata       map[string]string `json:"metadata"`
}

// ActiveDesktopRuns lists the bot's ongoing runs: Hub-followed desktop runs,
// pending admissions, and MaClawSrv runs still marked running. A MaClawSrv
// read failure leaves the Hub rows in place.
func (s *Service) ActiveDesktopRuns(ctx context.Context, tenantID, userID, botID string) ([]ActiveDesktopRun, error) {
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
	botID = rec.Bots[index].ID
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)

	items := make([]ActiveDesktopRun, 0)
	seen := map[string]bool{}
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	now := s.now()
	s.dropAbandonedDesktopReserves(tenantID, userID, botID, now)
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		if state.done {
			state.mu.Unlock()
			continue
		}
		row := ActiveDesktopRun{
			RunID:    strings.TrimSpace(state.runID),
			BotID:    state.botID,
			Source:   "desktop",
			Status:   "running",
			Task:     desktopTaskBody(state.task),
			Step:     strings.TrimSpace(state.activity),
			NextStep: strings.TrimSpace(state.progressNext),
			Phase:    strings.TrimSpace(state.phase),
		}
		if row.Step == "" {
			row.Step = strings.TrimSpace(state.progressWhere)
		}
		if row.RunID == "" {
			row.Status = "queued"
		}
		if !state.started.IsZero() {
			t := state.started
			row.StartedAt = &t
		}
		state.mu.Unlock()
		if row.RunID != "" {
			seen[row.RunID] = true
		}
		items = append(items, row)
	}
	s.mu.Unlock()

	if instanceID != "" {
		if rows, err := s.srvRunningRuns(ctx, tenantID, userID, instanceID); err == nil {
			for _, run := range rows {
				id := strings.TrimSpace(run.ID)
				if id == "" {
					continue
				}
				if seen[id] {
					for i := range items {
						if items[i].RunID == id {
							items[i].WaitingForUser = run.WaitingForUser
							if items[i].StartedAt == nil && !run.StartedAt.IsZero() {
								t := run.StartedAt
								items[i].StartedAt = &t
							}
						}
					}
					continue
				}
				row := ActiveDesktopRun{
					RunID:          id,
					BotID:          botID,
					Source:         "server",
					Status:         "running",
					WaitingForUser: run.WaitingForUser,
					Phase:          strings.TrimSpace(run.Metadata["bot_phase"]),
					Stale:          !run.WaitingForUser,
				}
				if !run.StartedAt.IsZero() {
					t := run.StartedAt
					row.StartedAt = &t
				}
				items = append(items, row)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if (items[i].Status == "running") != (items[j].Status == "running") {
			return items[i].Status == "running"
		}
		var a, b time.Time
		if items[i].StartedAt != nil {
			a = *items[i].StartedAt
		}
		if items[j].StartedAt != nil {
			b = *items[j].StartedAt
		}
		return a.After(b)
	})
	return items, nil
}

func (s *Service) srvRunningRuns(ctx context.Context, tenantID, userID, instanceID string) ([]srvRunRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []srvRunRow `json:"items"`
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs?status=running&limit=50"
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	kept := out.Items[:0]
	for _, run := range out.Items {
		if strings.TrimSpace(run.Status) == "running" {
			kept = append(kept, run)
		}
	}
	return kept, nil
}
