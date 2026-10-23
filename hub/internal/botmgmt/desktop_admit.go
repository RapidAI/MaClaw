package botmgmt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// desktopAdmitState is one accepted desktop command whose reply is not the
// admitting HTTP call. The watcher fills it when the run finishes.
type desktopAdmitState struct {
	mu           sync.Mutex
	done         bool
	delivered    bool
	reply        Reply
	err          error
	finished     time.Time
	task         string
	status       string
	userID       string
	botID        string
	runID        string
	instanceID   string
	phase        string
	started      time.Time
	mediaFetched bool
	mediaNext    time.Time
	// clientMessageID is the caller's turn key. A repeat of this key is the
	// same execute, not a second one. It is persisted only once a real run id
	// exists, so a restart can still replay that run.
	clientMessageID string
	// posting is how many message posts are still in flight for an empty-run
	// reserve. The reserve stays while this is above zero. The last post to
	// leave deletes it.
	posting int
	// abandoned is when the last post left this empty reserve behind. A sweep
	// drops it after desktopAdmitAbandonAfter. A post still in flight does not
	// set this.
	abandoned time.Time
	// activity is the step MaClawSrv last reported for this run. It is not
	// persisted: a restart learns it again from the next run read. Empty
	// means the run has not named a step.
	activity string
	// progress is the last PROGRESS.md copy the run published. A later read
	// with empty sections keeps this copy.
	progressWhere string
	progressNext  string
	progressOpen  string
}

// desktopRunActivityText keeps a working-row phrase. The closed set lives in
// desktopbot.ActivityPhrase, so a typed password or a tool name cannot become
// the label here either.
func desktopRunActivityText(raw string) string {
	return desktopbot.ActivityPhrase(raw)
}

// desktopAdmitAbandonAfter is how long an empty-run reserve may sit after its
// post has been given up. A post that is still waiting is not abandoned.
const desktopAdmitAbandonAfter = 30 * time.Second

// botPreferenceWithdrawRounds bounds how many list-and-delete passes one
// withdrawal performs. The list answers one page at a time, so clearing a long
// history takes several; this caps them so the loop always terminates even if
// the store keeps returning entries.
const botPreferenceWithdrawRounds = 20

// admittedDesktopFollow is the desktop bookkeeping that has to wait for the
// background run. It does not carry the caller's request context: that
// request has already returned.
type admittedDesktopFollow struct {
	TenantID        string
	UserID          string
	BotID           string
	InstanceID      string
	Novnc           string
	RunID           string
	Phase           string
	Task            string
	Opened          bool
	Isolated        bool
	Started         time.Time
	ClientMessageID string
}

type desktopRunView struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	DesktopReady    bool   `json:"desktop_ready"`
	DesktopHandoff  bool   `json:"desktop_handoff"`
	AttentionReason string `json:"attention_reason"`
	Activity        string `json:"activity,omitempty"`
	ProgressWhere   string `json:"progress_where,omitempty"`
	ProgressNext    string `json:"progress_next,omitempty"`
	ProgressOpen    string `json:"progress_open,omitempty"`
	Message         struct {
		Content     string            `json:"content"`
		Metadata    map[string]string `json:"metadata"`
		Attachments []replyAttachment `json:"attachments"`
	} `json:"message"`
}

func desktopAdmitKey(tenantID, userID, botID, runID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(runID)
}

// desktopReserveAbandoned reports an empty-run reserve whose post is no longer
// waiting and has been sitting longer than desktopAdmitAbandonAfter. Callers
// hold state.mu. A post still in flight is never abandoned.
func desktopReserveAbandoned(state *desktopAdmitState, now time.Time) bool {
	if state == nil || strings.TrimSpace(state.runID) != "" || state.done || state.posting > 0 {
		return false
	}
	stamp := state.abandoned
	if stamp.IsZero() {
		stamp = state.started
	}
	if stamp.IsZero() {
		return false
	}
	return now.Sub(stamp) > desktopAdmitAbandonAfter
}

// dropAbandonedDesktopReserves removes empty-run reserves this user has given
// up on. Callers hold s.mu. An empty botID sweeps every bot of the user; a
// bot id sweeps only that bot. A post that is still waiting stays, and it
// still counts as a running admit.
func (s *Service) dropAbandonedDesktopReserves(tenantID, userID, botID string, now time.Time) {
	if s == nil || len(s.desktopAdmits) == 0 {
		return
	}
	prefix := desktopTenantPrefix(tenantID) + strings.TrimSpace(userID) + "\x00"
	if strings.TrimSpace(botID) != "" {
		prefix = desktopAdmitKey(tenantID, userID, botID, "")
	}
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		state.mu.Lock()
		drop := desktopReserveAbandoned(state, now)
		state.mu.Unlock()
		if drop {
			delete(s.desktopAdmits, key)
		}
	}
}

// claimDesktopTurn reserves this user's desktop for one message post.
//
// The lock covers only the ledger. openDesktop, the owner token, and the
// message post happen after it returns. The same client message does not
// reserve a second time and is not refused: MaClawSrv replays that key.
// Any other non-terminal admit for this user refuses the post. The returned
// function drops the empty reserve when the post ends without a real run id.
func (s *Service) claimDesktopTurn(tenantID, userID, botID, clientMessageID, task, phase string) (func(), error) {
	noop := func() {}
	if s == nil {
		return noop, ErrSettingsUnavailable
	}
	clientMessageID = strings.TrimSpace(clientMessageID)
	botID = strings.TrimSpace(botID)
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	s.ensureDesktopHydrated(tenantID)
	if s.desktopAdmits == nil {
		s.desktopAdmits = map[string]*desktopAdmitState{}
	}
	now := s.now()
	s.dropAbandonedDesktopReserves(tenantID, userID, "", now)
	userPrefix := desktopTenantPrefix(tenantID) + userID + "\x00"
	botPrefix := desktopAdmitKey(tenantID, userID, botID, "")
	var sameEmpty *desktopAdmitState
	sameReal := false
	otherLive := false
	for key, state := range s.desktopAdmits {
		if state == nil || !strings.HasPrefix(key, userPrefix) {
			continue
		}
		state.mu.Lock()
		live := !state.done
		id := strings.TrimSpace(state.clientMessageID)
		runID := strings.TrimSpace(state.runID)
		state.mu.Unlock()
		if !live {
			continue
		}
		sameBot := strings.HasPrefix(key, botPrefix)
		sameKey := clientMessageID != "" && id == clientMessageID && sameBot
		if sameKey && runID == "" {
			sameEmpty = state
			continue
		}
		if sameKey {
			sameReal = true
			continue
		}
		otherLive = true
	}
	if otherLive {
		s.mu.Unlock()
		return noop, ErrDesktopBusy
	}
	hold := false
	if sameEmpty != nil {
		sameEmpty.mu.Lock()
		sameEmpty.posting++
		sameEmpty.abandoned = time.Time{}
		sameEmpty.mu.Unlock()
		hold = true
	} else if !sameReal {
		s.desktopAdmits[botPrefix] = &desktopAdmitState{
			task:            clipDesktopRunes(task, 500),
			status:          "running",
			userID:          userID,
			botID:           botID,
			clientMessageID: clientMessageID,
			phase:           strings.TrimSpace(phase),
			posting:         1,
			started:         now,
		}
		hold = true
	}
	s.mu.Unlock()
	return func() { s.releaseDesktopTurn(botPrefix, clientMessageID, hold) }, nil
}

// releaseDesktopTurn drops the empty reserve this post was holding. A real
// run id is a different key and is left alone. The last in-flight post
// deletes the reserve immediately, so a failed send does not keep the desktop
// busy for the abandon window.
func (s *Service) releaseDesktopTurn(key, clientMessageID string, hold bool) {
	if s == nil || !hold {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.desktopAdmits[key]
	if state == nil {
		return
	}
	state.mu.Lock()
	if strings.TrimSpace(state.runID) != "" || strings.TrimSpace(state.clientMessageID) != strings.TrimSpace(clientMessageID) {
		state.mu.Unlock()
		return
	}
	if state.posting > 0 {
		state.posting--
	}
	left := state.posting
	state.mu.Unlock()
	if left <= 0 {
		delete(s.desktopAdmits, key)
	}
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

// desktopTerminalMiss is the error a failed or cancelled run settles with.
// A recorded reason stays that reason. A handoff is a result: the person
// still has the desktop. Assistant prose that is only a filler — 「无变化」
// and the same family — is not a result, so it settles like an empty body.
// The sentence names the run and its status and is itself a filler, so the
// bubble stays quiet. It must not say the server could not be reached: a
// quota cutoff used to be wrapped that way, and the reader re-sent work
// that had already finished.
func desktopTerminalMiss(runID, status, text, recorded string, handoff bool) error {
	if status != "failed" && status != "cancelled" {
		return nil
	}
	if reason := strings.TrimSpace(recorded); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if handoff || (strings.TrimSpace(text) != "" && !desktopbot.Filler(text)) {
		return nil
	}
	return fmt.Errorf("run %s ended as %s without a result", strings.TrimSpace(runID), strings.TrimSpace(status))
}

func (s *Service) followAdmittedDesktopRun(job admittedDesktopFollow) {
	if s == nil || strings.TrimSpace(job.RunID) == "" {
		return
	}
	state := newRunningDesktopTask(job)
	key := desktopAdmitKey(job.TenantID, job.UserID, job.BotID, job.RunID)
	s.mu.Lock()
	s.ensureDesktopHydrated(job.TenantID)
	if s.desktopAdmits == nil {
		s.desktopAdmits = map[string]*desktopAdmitState{}
	}
	now := s.now()
	for id, item := range s.desktopAdmits {
		if item == nil {
			delete(s.desktopAdmits, id)
			continue
		}
		item.mu.Lock()
		drop := desktopTaskExpired(item.done, item.delivered, item.finished, now)
		item.mu.Unlock()
		if drop {
			delete(s.desktopAdmits, id)
		}
	}
	// The empty reserve and the real run change hands under this same lock.
	// A gap would let a second execute through, and leaving the empty key
	// would keep the desktop busy after the run id is known.
	emptyKey := desktopAdmitKey(job.TenantID, job.UserID, job.BotID, "")
	if empty := s.desktopAdmits[emptyKey]; empty != nil {
		empty.mu.Lock()
		dropEmpty := strings.TrimSpace(empty.runID) == "" &&
			(job.ClientMessageID == "" || empty.clientMessageID == "" || empty.clientMessageID == job.ClientMessageID)
		empty.mu.Unlock()
		if dropEmpty {
			delete(s.desktopAdmits, emptyKey)
		}
	}
	// Adopting is a claim, not an upsert. One run gets one watcher, because
	// that watcher settles it — it hands back the keyboard, clears the hold,
	// and closes the desktop open. Two watchers for the same run run that
	// whole sequence twice, and whichever finishes second is settling a run
	// that is already done. A second caller adopts nothing; it reads the
	// entry the first one claimed.
	claimed := false
	if _, exists := s.desktopAdmits[key]; !exists {
		s.desktopAdmits[key] = state
		claimed = true
		s.persistDesktopState(job.TenantID)
	}
	s.mu.Unlock()
	if !claimed {
		return
	}
	go s.watchAdmittedDesktopRun(job, state)
}

// settleAdmittedDesktopRun records a run that had already finished when this
// Hub was asked about it, once. It exists because settling has side effects
// that do not repeat cleanly — the keyboard goes back, the hold clears, the
// desktop open closes — and a client polls for the reply more than once.
//
// The first caller settles and keeps the reply; every later read of the same
// run answers that same reply instead of settling again. settle runs outside
// the lock because it reaches into desktop bookkeeping that takes s.mu itself.
func (s *Service) settleAdmittedDesktopRun(tenantID, userID, botID, runID string, settle func() (Reply, error)) (Reply, bool, error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		reply, err := settle()
		return reply, true, err
	}
	key := desktopAdmitKey(tenantID, userID, botID, runID)
	s.mu.Lock()
	if s.desktopAdmits == nil {
		s.desktopAdmits = map[string]*desktopAdmitState{}
	}
	if existing, ok := s.desktopAdmits[key]; ok {
		s.mu.Unlock()
		existing.mu.Lock()
		defer existing.mu.Unlock()
		if !existing.done {
			// Another caller is still settling it. Answer that it is not
			// finished yet rather than guessing at a second settlement.
			return Reply{}, false, nil
		}
		return existing.reply, true, existing.err
	}
	state := &desktopAdmitState{
		userID: strings.TrimSpace(userID),
		botID:  strings.TrimSpace(botID),
		runID:  strings.TrimSpace(runID),
	}
	s.desktopAdmits[key] = state
	s.mu.Unlock()

	reply, err := settle()
	state.mu.Lock()
	state.reply = reply
	state.err = err
	state.done = true
	state.finished = time.Now()
	state.mediaFetched = true
	state.mu.Unlock()
	if err != nil {
		// Keeping the claim after a failed settle would answer every later
		// read with "still going" forever and strand the run for good, which
		// is the same dead end this whole fallback removes. Drop the claim
		// so the next poll can try again; settling did not take effect.
		s.mu.Lock()
		if current, ok := s.desktopAdmits[key]; ok && current == state {
			delete(s.desktopAdmits, key)
		}
		s.mu.Unlock()
	}
	return reply, true, err
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
				s.rememberDesktopRunActivity(state, view.Activity)
				s.rememberDesktopRunProgress(state, view)
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
		s.rememberDesktopRunProgress(state, view)
		body := desktopCommandBody{
			Text:        view.Message.Content,
			Error:       view.Error,
			Handoff:     view.DesktopHandoff,
			Attention:   view.AttentionReason,
			Metadata:    view.Message.Metadata,
			Attachments: view.Message.Attachments,
			Waiting:     strings.TrimSpace(job.ClientMessageID) != "",
		}
		if view.DesktopHandoff && !job.Isolated && !job.Opened && strings.TrimSpace(job.Novnc) == "" {
			if url, did, openErr := s.openDesktop(context.Background(), job.TenantID, job.UserID, job.InstanceID, job.BotID); openErr == nil {
				job.Novnc, job.Opened = url, did
			}
		}
		body.CallErr = desktopTerminalMiss(job.RunID, view.Status, body.Text, view.Error, body.Handoff)
		reply, settleErr := s.settleDesktopCommand(job.TenantID, job.UserID, job.BotID, job.InstanceID, job.Novnc, job.Opened, body)
		state.mu.Lock()
		state.reply = reply
		state.err = settleErr
		state.done = true
		state.finished = time.Now()
		state.mediaFetched = true
		state.status = strings.TrimSpace(view.Status)
		if state.status == "" {
			state.status = "succeeded"
		}
		runStatus := state.status
		state.mu.Unlock()
		s.persistDesktopTasks(job.TenantID)
		s.reportDelegateRunEnd(job.TenantID, job.UserID, job.BotID, job.RunID, runStatus, reply, settleErr)
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

// rememberDesktopRunActivity keeps the latest step phrase for the GUI poll.
// The watcher already reads the run, so this does not add a request. An
// empty or rejected phrase clears the previous one.
func (s *Service) rememberDesktopRunProgress(state *desktopAdmitState, view desktopRunView) {
	if state == nil {
		return
	}
	where := strings.TrimSpace(view.ProgressWhere)
	next := strings.TrimSpace(view.ProgressNext)
	open := strings.TrimSpace(view.ProgressOpen)
	if where == "" && next == "" && open == "" {
		return
	}
	state.mu.Lock()
	state.progressWhere = where
	state.progressNext = next
	state.progressOpen = open
	state.mu.Unlock()
}

func (s *Service) rememberDesktopRunActivity(state *desktopAdmitState, activity string) {
	if state == nil {
		return
	}
	activity = desktopRunActivityText(activity)
	state.mu.Lock()
	state.activity = activity
	state.mu.Unlock()
}

// DesktopRunActivity is the step phrase cached for a run that is still going.
// A finished run answers with its reply instead, so this is empty then.
func (s *Service) DesktopRunActivity(tenantID, userID, botID, runID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if state == nil {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.done {
		return ""
	}
	return state.activity
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
	err = s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodPost, path, nil, &out)
	if err != nil && !admitted {
		// A run this Hub does not follow (left running by a restart) is
		// closed on MaClawSrv under the owner's own principal and the bot's
		// own instance. Anything MaClawSrv refuses is not this bot's run.
		return ErrNotFound
	}
	return err
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
//
// The admit ledger is in memory, so a Hub that restarted mid-run has no
// entry for work MaClawSrv is still driving. Rather than answer "bot not
// found" — which stranded the reply of a run that was very much alive — the
// miss asks MaClawSrv directly: a finished run is settled from its recorded
// result, a running one is adopted back into the ledger and watched, and
// only a run MaClawSrv does not know either is not found.
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
	instanceID := strings.TrimSpace(rec.Bots[index].InstanceID)
	s.mu.Lock()
	state := s.desktopAdmits[desktopAdmitKey(tenantID, userID, botID, runID)]
	s.mu.Unlock()
	if state == nil {
		if instanceID == "" {
			return Reply{}, false, ErrNotFound
		}
		return s.readmitDesktopRun(ctx, tenantID, userID, botID, instanceID, runID)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.done {
		return Reply{}, false, nil
	}
	return state.reply, true, state.err
}

// readmitDesktopRun answers one run the admit ledger no longer holds, from
// MaClawSrv itself. A finished run settles from its recorded result; a
// running one is adopted back so the watcher this Hub does have finishes
// it. An unreachable MaClawSrv is reported as the error it is — the run may
// still be going, and the caller's poll already treats that as "ask again".
func (s *Service) readmitDesktopRun(ctx context.Context, tenantID, userID, botID, instanceID, runID string) (Reply, bool, error) {
	view, err := s.readDesktopRunFromSrv(ctx, tenantID, userID, instanceID, runID)
	if err != nil {
		if errors.Is(err, ErrSrvNotFound) {
			return Reply{}, false, ErrNotFound
		}
		// MaClawSrv could not be asked, so nothing was learned about the run.
		// A Hub that restarted has no ledger entry for work that may still be
		// driving the desktop, and the only evidence is absent — not negative.
		// Reporting the transport failure here would end the caller's poll and
		// strand that run the same way the 404 did, so this Hub answers "still
		// running" and lets the next poll look again. The caller's own read
		// budget is what ends a run nobody can verify, not a fabricated reply.
		traceBot(botID, "run_readmit_unverified", err,
			"instance", instanceID,
			"run", runID,
		)
		return Reply{}, false, nil
	}
	if !desktopRunTerminal(view.Status) {
		novnc, _, watchErr := s.DesktopWatch(ctx, tenantID, userID, botID)
		if watchErr != nil {
			novnc = ""
		}
		s.followAdmittedDesktopRun(admittedDesktopFollow{
			TenantID:   tenantID,
			UserID:     userID,
			BotID:      botID,
			InstanceID: instanceID,
			Novnc:      novnc,
			RunID:      runID,
			Started:    time.Now(),
		})
		traceBot(botID, "run_readmit", nil,
			"instance", instanceID,
			"run", runID,
			"status", view.Status,
		)
		return Reply{}, false, nil
	}
	novnc, _, watchErr := s.DesktopWatch(ctx, tenantID, userID, botID)
	if watchErr != nil {
		novnc = ""
	}
	body := desktopCommandBody{
		Text:        view.Message.Content,
		Error:       view.Error,
		Handoff:     view.DesktopHandoff,
		Attention:   view.AttentionReason,
		Metadata:    view.Message.Metadata,
		Attachments: view.Message.Attachments,
		Waiting:     strings.TrimSpace(view.Message.Metadata["client_message_id"]) != "",
	}
	body.CallErr = desktopTerminalMiss(runID, view.Status, body.Text, view.Error, body.Handoff)
	// Settling is claimed, not repeated. The caller polls again whenever the
	// answer was lost, and the desktop bookkeeping inside settle does not
	// survive being run twice.
	reply, settled, settleErr := s.settleAdmittedDesktopRun(tenantID, userID, botID, runID, func() (Reply, error) {
		return s.settleDesktopCommand(tenantID, userID, botID, instanceID, novnc, false, body)
	})
	if settled {
		s.noteDesktopTaskStatus(tenantID, userID, botID, instanceID, runID, view.Status)
	}
	traceBot(botID, "run_readmit", settleErr,
		"instance", instanceID,
		"run", runID,
		"status", view.Status,
		"settled", traceYes(settled),
		"text_len", fmt.Sprintf("%d", len(reply.Text)),
	)
	return reply, settled, settleErr
}

// readDesktopRunFromSrv reads one run straight from MaClawSrv, the way the
// watcher does, for a run this Hub never admitted.
func (s *Service) readDesktopRunFromSrv(ctx context.Context, tenantID, userID, instanceID, runID string) (desktopRunView, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fresh, token, err := s.ownerToken(ctx, tenantID, userID)
	if err != nil {
		return desktopRunView{}, err
	}
	var view desktopRunView
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/runs/" + url.PathEscape(runID)
	if err := s.authAsOwner(ctx, tenantID, userID, &fresh, &token, http.MethodGet, path, nil, &view); err != nil {
		return desktopRunView{}, err
	}
	return view, nil
}
