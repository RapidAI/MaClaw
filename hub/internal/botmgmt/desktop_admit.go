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

// botPreferenceWithdrawRounds bounds how many list-and-delete passes one
// withdrawal performs. The list answers one page at a time, so clearing a long
// history takes several; this caps them so the loop always terminates even if
// the store keeps returning entries.
const botPreferenceWithdrawRounds = 20

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

const (
	// admitPollFastWindow is how long a run polls at the base interval before
	// the gap starts stretching. Most runs finish well inside it, so they never
	// pay for the backoff at all.
	admitPollFastWindow = 30 * time.Second
	// admitPollBackoffEvery is how much longer a run waits before the gap
	// doubles again.
	admitPollBackoffEvery = 20 * time.Second
	// admitPollBackoffMaxSteps caps the stretch at base x 2^3. Keeping the
	// ceiling relative to the configured interval is what lets a test run at a
	// 10ms interval without inheriting a production-length wait.
	admitPollBackoffMaxSteps = 3
	// admitPollMaxWait is the hard ceiling whatever the base interval is.
	admitPollMaxWait = 30 * time.Second
)

// admitPollBackoff stretches the gap between polls as a run drags on.
//
// A fixed one-second interval suits a run that answers in seconds, but it
// spends thousands of round trips on one that takes an hour: a 51-minute run
// polled roughly two thousand times, each poll repeating the owner-token and
// run-read round trip to learn "still running". Most of that is noise — the
// status it is waiting on cannot change second to second.
//
// It keys off elapsed time rather than the poll count because the count means
// nothing on its own: a short interval burns through hundreds of polls in the
// time a long one spends on a handful, so backing off by count would slow
// exactly the runs that were never slow. Time also keeps the stretch off runs
// that finish quickly, which is where an extra delay would actually be felt.
//
// The stretch is paid for in how late a finished run is noticed, so it stays
// bounded: at the default interval the gap tops out at 8s.
func admitPollBackoff(base time.Duration, elapsed time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if base > admitPollMaxWait {
		base = admitPollMaxWait
	}
	if elapsed < admitPollFastWindow {
		return base
	}
	steps := int((elapsed - admitPollFastWindow) / admitPollBackoffEvery)
	if steps > admitPollBackoffMaxSteps {
		steps = admitPollBackoffMaxSteps
	}
	wait := base
	for i := 0; i < steps; i++ {
		wait *= 2
		if wait >= admitPollMaxWait {
			return admitPollMaxWait
		}
	}
	return wait
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
	// started anchors the poll backoff: the gap stretches with how long this
	// run has been running, not with how many times we have asked.
	started := time.Now()
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
			time.Sleep(admitPollBackoff(s.admitPollWait(), time.Since(started)))
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
				time.Sleep(admitPollBackoff(s.admitPollWait(), time.Since(started)))
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
				// Deliberately not ErrSrv. Nothing here failed to reach
				// MaClawSrv: the run reached a terminal state and simply
				// produced no text. Wrapping it as "maclawsrv request failed"
				// is what made a bot answer a run cut short by a provider
				// quota with "I can't reach my server", and the reader then
				// re-sent work that had already been finished.
				body.CallErr = fmt.Errorf("run %s ended as %s without a result", job.RunID, view.Status)
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

// SupplementDesktopRun forwards supplementary input to a run MaClawSrv still
// reports as running.
//
// Ownership is resolved here, from the caller's own record, and the run is
// named explicitly: nothing about the request body can choose which run
// receives it. A run that already finished is reported as such instead of
// quietly accepting text nobody will read.
func (s *Service) SupplementDesktopRun(ctx context.Context, tenantID, userID, botID, runID, content string) (SupplementResult, error) {
	if s == nil {
		return SupplementResult{}, ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	content = strings.TrimSpace(content)
	if runID == "" {
		return SupplementResult{}, ErrNotFound
	}
	if content == "" {
		return SupplementResult{}, fmt.Errorf("%w: supplement is required", ErrInvalidInput)
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return SupplementResult{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return SupplementResult{}, ErrNotFound
	}
	botID = rec.Bots[index].ID
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return SupplementResult{}, ErrNotFound
	}
	// The run is only forwarded to when this service admitted it for this bot,
	// so a caller cannot aim input at a run belonging to another principal.
	s.mu.Lock()
	_, admitted := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if !admitted {
		return SupplementResult{}, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return SupplementResult{}, err
	}
	var out SupplementResult
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID) + "/supplement"
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, path, map[string]string{"content": content}, &out); err != nil {
		return SupplementResult{}, err
	}
	return out, nil
}

// DesktopRunSupplement is one supplementary instruction's final state as
// MaClawSrv recorded it. State is queued, applied, or missed, and Content is
// kept so the client can offer the exact words again when the run ended
// before they were used (design doc §3.1d 问题 6). The content travels in the
// response only; it is never written to a log or durable store.
type DesktopRunSupplement struct {
	ID      string `json:"id"`
	RunID   string `json:"run_id"`
	State   string `json:"state"`
	Content string `json:"content"`
}

// DesktopRunSupplements reports the final state of the supplements recorded
// for one run, so the client can tell the person which reached the loop and
// which arrived too late.
//
// Ownership resolves from the caller's own bot record, not from the desktop
// admit ledger: the question is asked after the run has finished, exactly when
// the admit record is being swept away. Scoping is still strict — the run id
// is resolved inside MaClawSrv against the same tenant, user and instance, so
// another principal's run answers not-found and reveals nothing.
func (s *Service) DesktopRunSupplements(ctx context.Context, tenantID, userID, botID, runID string) ([]DesktopRunSupplement, error) {
	if s == nil {
		return nil, ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrNotFound
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
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []DesktopRunSupplement `json:"items"`
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID) + "/supplements"
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// CancelDesktopRun stops a run MaClawSrv still reports as running.
//
// Ownership is resolved exactly as it is for a supplement: the caller's own
// record names the instance, the run is named explicitly, and this service must
// have admitted it for this bot. Without this route a person could start desktop
// work but never stop it, which is why it travels with the supplement route
// rather than behind it.
func (s *Service) CancelDesktopRun(ctx context.Context, tenantID, userID, botID, runID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
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
	botID = rec.Bots[index].ID
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	_, admitted := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if !admitted {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	var out struct {
		Status string `json:"status"`
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID) + "/cancel"
	return s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, path, nil, &out)
}

// SaveBotPreference stores a durable preference on the user's own instance.
//
// Desktop work runs on the server, so the executor only ever reads the copy
// there; keeping a preference on the client would leave future tasks unaware
// of it. The write is a no-op for a bot with no instance rather than an error,
// because a preference is a hint and must never break a turn that is trying to
// record one.
func (s *Service) SaveBotPreference(ctx context.Context, tenantID, userID, botID, content string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]string{"content": content, "category": "preference"}
	return s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, "/api/v1/memory", body, &out)
}

// WithdrawBotPreference removes the durable preferences recorded for this
// person's instance.
//
// It exists because a preference is withdrawn in conversation — a persona of
// "-" — and the server copy is the one the worker reads. A withdrawal that
// cleared only the client would leave future work honouring something the
// person had already taken back, with nothing on screen to explain it.
//
// Removal is by category rather than by id because the client never kept the
// server's entry ids. An instance with none to remove is a no-op.
func (s *Service) WithdrawBotPreference(ctx context.Context, tenantID, userID, botID string) error {
	if s == nil {
		return ErrSettingsUnavailable
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return ErrNotFound
	}
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	if instanceID == "" {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	// Deleting shifts later entries into earlier pages, so each round reads the
	// head of the list again and stops when nothing is left. The bound is a
	// guard against a store that keeps answering with something: a preference is
	// a hint, and it must not turn into an unbounded delete loop.
	var listed struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	for rounds := 0; rounds < botPreferenceWithdrawRounds; rounds++ {
		listed.Items = nil
		if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, "/api/v1/memory?category=preference", nil, &listed); err != nil {
			return err
		}
		if len(listed.Items) == 0 {
			return nil
		}
		for _, item := range listed.Items {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodDelete, "/api/v1/memory/"+url.PathEscape(id), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// SupplementResult is MaClawSrv's answer to one supplementary instruction.
type SupplementResult struct {
	SupplementID string `json:"supplement_id"`
	Accepted     bool   `json:"accepted"`
	State        string `json:"state"`
	Detail       string `json:"detail"`
	RunID        string `json:"run_id,omitempty"`
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
