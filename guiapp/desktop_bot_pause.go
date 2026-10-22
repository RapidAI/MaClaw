package guiapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errDesktopBotPauseUnsupported means the Hub (or the MaClawSrv behind it)
// predates real pause. Callers fall back to holding new orders locally.
var errDesktopBotPauseUnsupported = errors.New("this Hub does not support pausing a running task yet")

// DesktopBotRunPause is Hub's answer to pause/resume.
type DesktopBotRunPause struct {
	RunID      string `json:"run_id"`
	Paused     bool   `json:"paused"`
	Held       bool   `json:"held"`
	PausedAt   string `json:"paused_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	TimeoutSec int64  `json:"timeout_sec,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	OnTimeout  string `json:"on_timeout,omitempty"`
	// Code is only filled from an error body.
	Code string `json:"code,omitempty"`
}

// PauseDesktopBotRun holds a running remote run at its next step boundary.
// timeoutSec <= 0 uses the server default (30 minutes); when the pause
// times out the server cancels the run.
func (a *App) PauseDesktopBotRun(botID, runID, reason string, timeoutSec int) (*DesktopBotRunPause, error) {
	body := map[string]any{"reason": strings.TrimSpace(reason)}
	if timeoutSec > 0 {
		body["timeout_sec"] = timeoutSec
	}
	return a.desktopBotRunPauseCall(botID, runID, "pause", body)
}

// ResumeDesktopBotRun continues the same paused run.
func (a *App) ResumeDesktopBotRun(botID, runID string) (*DesktopBotRunPause, error) {
	return a.desktopBotRunPauseCall(botID, runID, "resume", map[string]any{})
}

func (a *App) desktopBotRunPauseCall(botID, runID, verb string, body any) (*DesktopBotRunPause, error) {
	botID = strings.TrimSpace(botID)
	runID = strings.TrimSpace(runID)
	if botID == "" || runID == "" {
		return nil, fmt.Errorf("bot id and run id are required")
	}
	path := "/api/v1/bots/" + url.PathEscape(botID) + "/runs/" + url.PathEscape(runID) + "/" + verb
	var out DesktopBotRunPause
	status, err := a.desktopBotExchange(context.Background(), http.MethodPost, path, body, &out, 20*time.Second, nil)
	if err != nil {
		if desktopBotPauseUnsupported(status, out.Code) {
			return nil, errDesktopBotPauseUnsupported
		}
		return nil, err
	}
	if out.RunID == "" {
		out.RunID = runID
	}
	return &out, nil
}

// desktopBotPauseUnsupported: a newer Hub names its error with a code; an
// older Hub answers an unknown route with a bare 404 (or 405).
func desktopBotPauseUnsupported(status int, code string) bool {
	switch status {
	case http.StatusNotImplemented, http.StatusMethodNotAllowed:
		return true
	case http.StatusNotFound:
		return strings.TrimSpace(code) == ""
	}
	return strings.EqualFold(code, "PAUSE_UNSUPPORTED")
}
