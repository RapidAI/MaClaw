package botmgmt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// desktopAdmitState is one accepted desktop command whose reply is not the
// admitting HTTP call. The watcher fills it when the run finishes.
type desktopAdmitState struct {
	mu       sync.Mutex
	done     bool
	reply    Reply
	err      error
	finished time.Time
}

// admittedDesktopFollow is the desktop bookkeeping that has to wait for the
// background run. It does not carry the caller's request context: that
// request has already returned.
type admittedDesktopFollow struct {
	TenantID   string
	UserID     string
	BotID      string
	InstanceID string
	Novnc      string
	RunID      string
	Phase      string
	Opened     bool
	Started    time.Time
}

type desktopRunView struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	DesktopReady    bool   `json:"desktop_ready"`
	DesktopHandoff  bool   `json:"desktop_handoff"`
	AttentionReason string `json:"attention_reason"`
	Message         struct {
		Content     string            `json:"content"`
		Metadata    map[string]string `json:"metadata"`
		Attachments []replyAttachment `json:"attachments"`
	} `json:"message"`
}

func desktopAdmitKey(tenantID, userID, botID, runID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(runID)
}

func (s *Service) admitPollWait() time.Duration {
	if s != nil && s.admitPollInterval > 0 {
		return s.admitPollInterval
	}
	return time.Second
}

func (s *Service) admitReadyWait() time.Duration {
	if s != nil && s.admitReadyGrace > 0 {
		return s.admitReadyGrace
	}
	return 3 * time.Second
}

func desktopRunTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s *Service) followAdmittedDesktopRun(job admittedDesktopFollow) {
	if s == nil || strings.TrimSpace(job.RunID) == "" {
		return
	}
	state := &desktopAdmitState{}
	key := desktopAdmitKey(job.TenantID, job.UserID, job.BotID, job.RunID)
	s.mu.Lock()
	if s.desktopAdmits == nil {
		s.desktopAdmits = map[string]*desktopAdmitState{}
	}
	cutoff := time.Now().Add(-6 * time.Hour)
	for id, item := range s.desktopAdmits {
		if item == nil {
			delete(s.desktopAdmits, id)
			continue
		}
		item.mu.Lock()
		finished := item.done && !item.finished.IsZero() && item.finished.Before(cutoff)
		item.mu.Unlock()
		if finished {
			delete(s.desktopAdmits, id)
		}
	}
	s.desktopAdmits[key] = state
	s.mu.Unlock()
	go s.watchAdmittedDesktopRun(job, state)
}

func (s *Service) watchAdmittedDesktopRun(job admittedDesktopFollow, state *desktopAdmitState) {
	path := "/api/v1/instances/" + url.PathEscape(job.InstanceID) + "/runs/" + url.PathEscape(job.RunID)
	var firstTerminal time.Time
	polls := 0
	// WriteOnce ignores the fields, so a running line hides the later
	// waiting line for the whole repeat window. A changed state is written
	// immediately. An unchanged state stays one line per window.
	lastState := ""
	writePoll := func(state string, err error, kv ...string) {
		if state != lastState {
			traceBot(job.BotID, "run_poll", err, kv...)
			lastState = state
			return
		}
		traceBotOnce(job.BotID, "run_poll", err, kv...)
	}
	for {
		polls++
		view, err := s.readAdmittedDesktopRun(job, path)
		if err != nil || !desktopRunTerminal(view.Status) {
			if err == nil {
				firstTerminal = time.Time{}
				writePoll("status:"+view.Status, nil,
					"instance", job.InstanceID,
					"run", job.RunID,
					"status", view.Status,
					"ready", traceYes(view.DesktopReady),
					"polls", strconv.Itoa(polls),
				)
			} else {
				writePoll("retry", err,
					"instance", job.InstanceID,
					"run", job.RunID,
					"retry", "yes",
					"polls", strconv.Itoa(polls),
				)
			}
			time.Sleep(s.admitPollWait())
			continue
		}
		if !view.DesktopReady {
			if firstTerminal.IsZero() {
				firstTerminal = time.Now()
			}
			if time.Since(firstTerminal) < s.admitReadyWait() {
				writePoll("waiting", nil,
					"instance", job.InstanceID,
					"run", job.RunID,
					"status", view.Status,
					"ready", "no",
					"waiting", "yes",
					"polls", strconv.Itoa(polls),
				)
				time.Sleep(s.admitPollWait())
				continue
			}
			traceBot(job.BotID, "run_poll", nil,
				"instance", job.InstanceID,
				"run", job.RunID,
				"status", view.Status,
				"ready", "no",
				"waiting", "expired",
				"polls", strconv.Itoa(polls),
			)
		} else {
			traceBot(job.BotID, "run_poll", nil,
				"instance", job.InstanceID,
				"run", job.RunID,
				"status", view.Status,
				"ready", "yes",
				"text_len", strconv.Itoa(len(view.Message.Content)),
				"attachments", strconv.Itoa(len(view.Message.Attachments)),
				"handoff", traceYes(view.DesktopHandoff),
				"attention", view.AttentionReason,
				"polls", strconv.Itoa(polls),
			)
		}
		body := desktopCommandBody{
			Text:        view.Message.Content,
			Error:       view.Error,
			Handoff:     view.DesktopHandoff,
			Attention:   view.AttentionReason,
			Metadata:    view.Message.Metadata,
			Attachments: view.Message.Attachments,
		}
		if view.Status == "failed" || view.Status == "cancelled" {
			if strings.TrimSpace(view.Error) != "" {
				body.CallErr = fmt.Errorf("%s", strings.TrimSpace(view.Error))
			} else if strings.TrimSpace(body.Text) == "" && !body.Handoff {
				body.CallErr = fmt.Errorf("%w: instance returned no result", ErrSrv)
			}
		}
		reply, settleErr := s.settleDesktopCommand(job.TenantID, job.UserID, job.BotID, job.InstanceID, job.Novnc, job.Opened, body)
		state.mu.Lock()
		state.reply = reply
		state.err = settleErr
		state.done = true
		state.finished = time.Now()
		state.mu.Unlock()
		traceBot(job.BotID, "post_end", settleErr,
			"instance", job.InstanceID,
			"run", job.RunID,
			"phase", job.Phase,
			"text_len", fmt.Sprintf("%d", len(reply.Text)),
			"images", fmt.Sprintf("%d", len(reply.Images)),
			"handoff", traceYes(reply.Handoff),
			"attention", reply.AttentionReason,
			"ask", traceYes(reply.AskUserQuestion != "" || reply.AskUserSecretName != ""),
			"dur_ms", traceMS(time.Since(job.Started)),
		)
		return
	}
}

func (s *Service) readAdmittedDesktopRun(job admittedDesktopFollow, path string) (desktopRunView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, job.TenantID, job.UserID)
	if err != nil {
		return desktopRunView{}, err
	}
	var view desktopRunView
	if err := s.authAsOwner(ctx, job.TenantID, job.UserID, &fresh, &token, http.MethodGet, path, nil, &view); err != nil {
		return desktopRunView{}, err
	}
	return view, nil
}

// DesktopRunResult is the reply for a run AdmitDesktopMessage already accepted.
// done is false while that run is still going. A missing run is not found.
func (s *Service) DesktopRunResult(ctx context.Context, tenantID, userID, botID, runID string) (Reply, bool, error) {
	if s == nil {
		return Reply{}, false, ErrSettingsUnavailable
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return Reply{}, false, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return Reply{}, false, ErrNotFound
	}
	botID = rec.Bots[index].ID
	s.mu.Lock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if state == nil {
		return Reply{}, false, ErrNotFound
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.done {
		return Reply{}, false, nil
	}
	return state.reply, true, state.err
}
