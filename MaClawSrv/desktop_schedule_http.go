package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// desktopBotScheduleView is one timer a client may show. The clock stays on
// this host. Every GUI, including mobile, reads this list through Hub.
type desktopBotScheduleView struct {
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

func desktopBotScheduleOwned(task scheduler.ScheduledTask, instanceID, botID, userID string) bool {
	if strings.TrimSpace(task.DesktopBotID) == "" || task.DesktopBotID != strings.TrimSpace(botID) {
		return false
	}
	if strings.TrimSpace(task.InstanceID) != strings.TrimSpace(instanceID) {
		return false
	}
	owner := strings.TrimSpace(task.OwnerUserID)
	userID = strings.TrimSpace(userID)
	return owner == "" || userID == "" || owner == userID
}

func listDesktopBotSchedules(mgr *scheduler.Manager, instanceID, botID, userID string) []desktopBotScheduleView {
	if mgr == nil {
		return []desktopBotScheduleView{}
	}
	out := []desktopBotScheduleView{}
	for _, task := range mgr.List() {
		if !desktopBotScheduleOwned(task, instanceID, botID, userID) || task.Status == "expired" {
			continue
		}
		// watch-job rows are the long-job patrol clock. They are not timers
		// the person asked for, and cancelling one from this list stops the patrol.
		if strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		view := desktopBotScheduleView{
			ID:              task.ID,
			Name:            task.Name,
			Action:          task.Action,
			IntervalMinutes: task.IntervalMinutes,
			Hour:            task.Hour,
			Minute:          task.Minute,
			DayOfWeek:       task.DayOfWeek,
			Status:          task.Status,
		}
		if task.NextRunAt != nil && !task.NextRunAt.IsZero() {
			view.NextRunAt = task.NextRunAt.UTC().Format(time.RFC3339)
		}
		out = append(out, view)
	}
	return out
}

func deleteDesktopBotSchedule(mgr *scheduler.Manager, instanceID, botID, userID, taskID string) error {
	if mgr == nil {
		return errDesktopScheduleUnavailable
	}
	taskID = strings.TrimSpace(taskID)
	owned := false
	for _, task := range mgr.List() {
		if task.ID != taskID {
			continue
		}
		if !desktopBotScheduleOwned(task, instanceID, botID, userID) {
			return errDesktopScheduleNotFound
		}
		owned = true
		break
	}
	if !owned {
		return errDesktopScheduleNotFound
	}
	err := mgr.Delete(taskID)
	if err != nil && strings.Contains(err.Error(), "cancellation requested") {
		err = mgr.ForceDelete(taskID)
	}
	return err
}

func desktopBotScheduleIsPatrol(mgr *scheduler.Manager, taskID string) bool {
	if mgr == nil {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	for _, task := range mgr.List() {
		if task.ID == taskID {
			return strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix)
		}
	}
	return false
}

func deleteDesktopBotSchedules(mgr *scheduler.Manager, instanceID, botID, userID string) int {
	if mgr == nil {
		return 0
	}
	n := 0
	for _, task := range mgr.List() {
		if !desktopBotScheduleOwned(task, instanceID, botID, userID) {
			continue
		}
		if deleteDesktopBotSchedule(mgr, instanceID, botID, userID, task.ID) == nil {
			n++
		}
	}
	return n
}

var (
	errDesktopScheduleUnavailable = errors.New("desktop bot clock is not running")
	errDesktopScheduleNotFound    = errors.New("desktop bot schedule not found")
)

// authorizeScheduleInstance refuses a caller who does not own this instance.
// The schedule file is one list for every user. The instance check is what
// keeps one account from reading or stopping another's timers.
func (s *HTTPServer) authorizeScheduleInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) (string, bool) {
	if s == nil || s.svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errDesktopScheduleUnavailable.Error()})
		return "", false
	}
	instanceID := strings.TrimSpace(r.PathValue("instanceId"))
	if _, err := s.svc.GetInstance(r.Context(), p, instanceID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return "", false
	}
	return instanceID, true
}

func (s *HTTPServer) scheduleManager(w http.ResponseWriter) (*scheduler.Manager, bool) {
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errDesktopScheduleUnavailable.Error()})
		return nil, false
	}
	return mgr, true
}

func (s *HTTPServer) handleListBotSchedules(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	instanceID, ok := s.authorizeScheduleInstance(w, r, p)
	if !ok {
		return
	}
	mgr, ok := s.scheduleManager(w)
	if !ok {
		return
	}
	botID := strings.TrimSpace(r.URL.Query().Get("bot_id"))
	if botID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bot_id is required"})
		return
	}
	adoptUntaggedBotSchedules(s.svc, mgr)
	dropDesktopBotScheduleDelivery(mgr)
	items := listDesktopBotSchedules(mgr, instanceID, botID, p.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleDeleteBotSchedule(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	instanceID, ok := s.authorizeScheduleInstance(w, r, p)
	if !ok {
		return
	}
	mgr, ok := s.scheduleManager(w)
	if !ok {
		return
	}
	botID := strings.TrimSpace(r.URL.Query().Get("bot_id"))
	if botID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bot_id is required"})
		return
	}
	if taskID := strings.TrimSpace(r.PathValue("taskId")); taskID != "" {
		if desktopBotScheduleIsPatrol(mgr, taskID) {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": 0})
			return
		}
		err := deleteDesktopBotSchedule(mgr, instanceID, botID, p.UserID, taskID)
		if err == errDesktopScheduleNotFound {
			// Already gone. A 404 here becomes "server unavailable" once Hub
			// folds every upstream 404 into that sentence.
			writeJSON(w, http.StatusOK, map[string]any{"deleted": 0})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": 1})
		return
	}
	n := deleteDesktopBotSchedules(mgr, instanceID, botID, p.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// retainDesktopBotSchedules drops timers on this instance whose bot id is
// no longer in the live set. An empty set deletes nothing: a failed bot
// list must not clear the clock. Rows that are not desktop-bot timers stay.
func retainDesktopBotSchedules(mgr *scheduler.Manager, instanceID, userID string, live []string) int {
	if mgr == nil {
		return 0
	}
	keep := map[string]bool{}
	for _, id := range live {
		id = strings.TrimSpace(id)
		if id != "" {
			keep[id] = true
		}
	}
	if len(keep) == 0 {
		return 0
	}
	instanceID = strings.TrimSpace(instanceID)
	n := 0
	for _, task := range mgr.List() {
		if strings.TrimSpace(task.DesktopBotID) == "" || strings.TrimSpace(task.InstanceID) != instanceID {
			continue
		}
		if keep[task.DesktopBotID] {
			continue
		}
		if !desktopBotScheduleOwned(task, instanceID, task.DesktopBotID, userID) {
			continue
		}
		if deleteDesktopBotSchedule(mgr, instanceID, task.DesktopBotID, userID, task.ID) == nil {
			n++
		}
	}
	return n
}

func (s *HTTPServer) handleRetainBotSchedules(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	instanceID, ok := s.authorizeScheduleInstance(w, r, p)
	if !ok {
		return
	}
	mgr, ok := s.scheduleManager(w)
	if !ok {
		return
	}
	var in struct {
		BotIDs []string `json:"bot_ids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.BotIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bot_ids is required"})
		return
	}
	n := retainDesktopBotSchedules(mgr, instanceID, p.UserID, in.BotIDs)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}
