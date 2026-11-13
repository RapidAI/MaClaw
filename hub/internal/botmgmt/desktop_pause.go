package botmgmt

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// RunPauseState mirrors MaClawSrv's pause answer. Pause holds the run at its
// next step boundary (the step under way finishes); resume continues the same
// run; a pause left open past its timeout cancels the run on MaClawSrv.
type RunPauseState struct {
	RunID      string     `json:"run_id"`
	Paused     bool       `json:"paused"`
	Held       bool       `json:"held"`
	PausedAt   *time.Time `json:"paused_at,omitempty"`
	HeldAt     *time.Time `json:"held_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	TimeoutSec int64      `json:"timeout_sec,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	OnTimeout  string     `json:"on_timeout,omitempty"`
}

// ErrPauseUnsupported means the MaClawSrv behind this bot predates pause.
var ErrPauseUnsupported = errors.New("run pause is not supported by this server")

// ErrRunNotRunning means the run already ended (or is not executing here).
var ErrRunNotRunning = errors.New("run is not running")

func runPauseKey(tenantID, userID, botID, runID string) string {
	return desktopAdmitKey(tenantID, userID, botID, runID)
}

// PauseDesktopRun pauses one of the owner's runs at its next step boundary.
func (s *Service) PauseDesktopRun(ctx context.Context, tenantID, userID, botID, runID, reason string, timeoutSec int64) (*RunPauseState, error) {
	reason = strings.TrimSpace(reason)
	body := map[string]any{"reason": reason}
	if strings.EqualFold(reason, "takeover") && timeoutSec <= 0 {
		// The person has the screen. A short pause timeout would cancel
		// the run they are in the middle of.
		timeoutSec = int64((6 * time.Hour).Seconds())
	}
	if timeoutSec > 0 {
		body["timeout_sec"] = timeoutSec
	}
	st, bot, err := s.runPauseCall(ctx, tenantID, userID, botID, runID, "pause", body)
	if err != nil {
		return nil, err
	}
	if st.Paused {
		if strings.TrimSpace(st.Reason) == "" {
			st.Reason = reason
		}
		key := runPauseKey(tenantID, userID, bot, st.RunID)
		s.runPauses.Store(key, *st)
		s.storePausedRun(ctx, tenantID, key, *st)
	}
	if strings.EqualFold(reason, "takeover") {
		s.beginDesktopTakeover(ctx, tenantID, userID, bot)
	}
	return st, nil
}

// beginDesktopTakeover hands this bot's display to the person. A desktop
// that has no control channel is unchanged; the pause still holds the run.
func (s *Service) beginDesktopTakeover(ctx context.Context, tenantID, userID, botID string) {
	if s == nil {
		return
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		return
	}
	ctl, ok := s.Desktop.(DesktopCtl)
	if !ok || ctl == nil {
		return
	}
	dUser := desktopUserForBot(userID, rec.Bots[index])
	_, _ = ctl.Ctl(ctx, tenantID, dUser, map[string]any{"op": desktop.CtlTakeoverBegin, "reason": "takeover"})
}

// ResumeTakeoverRuns continues runs this Hub paused so the person could use
// the screen. The same run goes on. There is no second run.
func (s *Service) ResumeTakeoverRuns(ctx context.Context, tenantID, userID, botID string) (string, bool) {
	if s == nil {
		return "", false
	}
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	var ids []string
	s.runPauses.Range(func(k, v any) bool {
		key, _ := k.(string)
		st, _ := v.(RunPauseState)
		if strings.HasPrefix(key, prefix) && st.Paused && strings.EqualFold(st.Reason, "takeover") && st.RunID != "" {
			ids = append(ids, st.RunID)
		}
		return true
	})
	resumed := ""
	for _, id := range ids {
		if _, err := s.ResumeDesktopRun(ctx, tenantID, userID, botID, id); err == nil && resumed == "" {
			resumed = id
		}
	}
	return resumed, resumed != ""
}

// ResumeDesktopRun releases a paused run; the same run continues.
func (s *Service) ResumeDesktopRun(ctx context.Context, tenantID, userID, botID, runID string) (*RunPauseState, error) {
	st, bot, err := s.runPauseCall(ctx, tenantID, userID, botID, runID, "resume", map[string]any{})
	if err != nil {
		return nil, err
	}
	key := runPauseKey(tenantID, userID, bot, strings.TrimSpace(runID))
	s.runPauses.Delete(key)
	s.dropPausedRun(ctx, tenantID, key)
	return st, nil
}

func (s *Service) storePausedRun(ctx context.Context, tenantID, key string, st RunPauseState) {
	if s == nil || s.System == nil {
		return
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return
	}
	if rec.PausedRuns == nil {
		rec.PausedRuns = map[string]RunPauseState{}
	}
	rec.PausedRuns[key] = st
	_ = s.save(ctx, tenantID, rec)
}

func (s *Service) dropPausedRun(ctx context.Context, tenantID, key string) {
	if s == nil || s.System == nil {
		return
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil || rec.PausedRuns == nil {
		return
	}
	delete(rec.PausedRuns, key)
	_ = s.save(ctx, tenantID, rec)
	// load copies every saved pause back into memory. A resume that deleted
	// the key first would otherwise see it return before this write lands.
	s.runPauses.Delete(key)
}

func (s *Service) runPauseCall(ctx context.Context, tenantID, userID, botID, runID, verb string, body any) (*RunPauseState, string, error) {
	if s == nil {
		return nil, "", ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, "", ErrNotFound
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return nil, "", err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return nil, "", ErrNotFound
	}
	bot := rec.Bots[index].ID
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return nil, "", ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, "", err
	}
	var out RunPauseState
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID) + "/" + verb
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, path, body, &out); err != nil {
		if errors.Is(err, ErrSrvNotFound) {
			// A 404 is either a run MaClawSrv does not know or a server
			// older than pause. Reading the run tells them apart.
			var probe map[string]any
			runPath := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID)
			if s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, runPath, nil, &probe) == nil {
				return nil, "", ErrPauseUnsupported
			}
			return nil, "", ErrNotFound
		}
		return nil, "", classifyPauseError(err)
	}
	if out.RunID == "" {
		out.RunID = runID
	}
	return &out, bot, nil
}

// classifyPauseError turns MaClawSrv's answers into ones the GUI can explain:
// 404/405 on the pause route itself means an older server; 409 means the run
// already ended.
func classifyPauseError(err error) error {
	var se *upstream.StatusError
	if errors.As(err, &se) {
		switch se.Status {
		case http.StatusConflict:
			return ErrRunNotRunning
		case http.StatusMethodNotAllowed:
			return ErrPauseUnsupported
		}
	}
	return err
}

// markPausedRuns flags rows this Hub paused and forgets pauses whose run is
// no longer active or whose timeout has passed.
func (s *Service) markPausedRuns(tenantID, userID, botID string, items []ActiveDesktopRun) {
	now := s.now()
	live := map[string]int{}
	for i := range items {
		if items[i].RunID != "" {
			live[items[i].RunID] = i
		}
	}
	prefix := desktopAdmitKey(tenantID, userID, botID, "")
	s.runPauses.Range(func(k, v any) bool {
		key := k.(string)
		if !strings.HasPrefix(key, prefix) {
			return true
		}
		st := v.(RunPauseState)
		i, ok := live[st.RunID]
		if !ok || (st.ExpiresAt != nil && now.After(*st.ExpiresAt)) {
			s.runPauses.Delete(key)
			return true
		}
		items[i].Paused = true
		items[i].PausedAt = st.PausedAt
		items[i].PauseExpiresAt = st.ExpiresAt
		return true
	})
}
