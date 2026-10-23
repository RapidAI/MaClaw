package botmgmt

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Hub's bot dispatcher: the server-side half of main-bot coordination.
//
//   - A delegate note (main bot -> teammate) is claimed and admitted by Hub
//     itself, with the note id as the turn's idempotency key. A GUI that
//     tries the same note afterwards loses the claim and only shows the run.
//   - A report that lands in an idle server-side bot's mailbox starts a
//     turn of that bot, so it reads and answers the mail now instead of on
//     its next run. The main bot's mail is left to its GUI while a GUI is
//     polling it; with no GUI the main bot's server-side twin is woken.
//   - The desktop allows one run per user. A busy desktop retries later and
//     every run end kicks the dispatcher again.
//
// Nothing here needs a GUI, so a cross-bot task completes with it closed.

const (
	dispatchRetry      = 45 * time.Second
	dispatchMaxRetries = 40 // ~30 min of a busy desktop
	dispatchAdmitWait  = 3 * time.Minute
	guiReaderFresh     = 2 * time.Minute
	wakePerHour        = 6
	delegateTries      = 3
	sweepEvery         = 2 * time.Minute
	// Older mail and orders are left to a person: Hub does not start work
	// that was written before it could have started it.
	dispatchMaxAge = 2 * time.Hour
)

type botDispatchState struct {
	mu      sync.Mutex
	running map[string]bool
	again   map[string]bool
	retries map[string]int
	timer   map[string]*time.Timer
	gui     map[string]time.Time
	wakes   map[string][]time.Time
	fails   map[string]int
	users   map[string][2]string
	sweep   bool
	// saved marks tenant|user pairs already on the persisted roster.
	saved    map[string]bool
	rosterMu sync.Mutex
}

func dispatchUserKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(userID)
}

func (d *botDispatchState) init() {
	if d.running == nil {
		d.running = map[string]bool{}
		d.again = map[string]bool{}
		d.retries = map[string]int{}
		d.timer = map[string]*time.Timer{}
		d.gui = map[string]time.Time{}
		d.wakes = map[string][]time.Time{}
		d.fails = map[string]int{}
		d.users = map[string][2]string{}
	}
	if d.saved == nil {
		d.saved = map[string]bool{}
	}
}

// noteGUIReader records that a user-side client just read botID's mail.
func (s *Service) noteGUIReader(tenantID, userID, botID string) {
	if s == nil {
		return
	}
	s.dispatch.mu.Lock()
	s.dispatch.init()
	s.dispatch.gui[dispatchUserKey(tenantID, userID)+"|"+botID] = time.Now()
	s.dispatch.mu.Unlock()
}

func (s *Service) guiReading(tenantID, userID, botID string) bool {
	s.dispatch.mu.Lock()
	defer s.dispatch.mu.Unlock()
	s.dispatch.init()
	at, ok := s.dispatch.gui[dispatchUserKey(tenantID, userID)+"|"+botID]
	return ok && time.Since(at) < guiReaderFresh
}

// kickDispatch schedules one dispatch pass for this user. It never blocks,
// so it is safe to call with s.mu held.
func (s *Service) kickDispatch(tenantID, userID string) {
	if s == nil || !s.BotDispatch {
		return
	}
	key := dispatchUserKey(tenantID, userID)
	s.dispatch.mu.Lock()
	s.dispatch.init()
	s.dispatch.users[key] = [2]string{tenantID, userID}
	fresh := !s.dispatch.saved[key]
	if !s.dispatch.sweep {
		s.dispatch.sweep = true
		go s.dispatchSweep()
	}
	if s.dispatch.running[key] {
		s.dispatch.again[key] = true
		s.dispatch.mu.Unlock()
		if fresh {
			s.rememberDispatchUser(tenantID, userID)
		}
		return
	}
	s.dispatch.running[key] = true
	s.dispatch.mu.Unlock()
	if fresh {
		s.rememberDispatchUser(tenantID, userID)
	}
	go s.dispatchLoop(tenantID, userID)
}

// dispatchSweep re-kicks every user seen since boot, so work that lost
// its kick (a Hub restart, a dropped timer) is still picked up.
func (s *Service) dispatchSweep() {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for range t.C {
		s.dispatch.mu.Lock()
		users := make([][2]string, 0, len(s.dispatch.users))
		for _, u := range s.dispatch.users {
			users = append(users, u)
		}
		s.dispatch.mu.Unlock()
		for _, u := range users {
			s.kickDispatch(u[0], u[1])
		}
	}
}

// KickBotDispatch is the boot/ops entry: start a pass for this user.
func (s *Service) KickBotDispatch(tenantID, userID string) { s.kickDispatch(tenantID, userID) }

func (s *Service) dispatchLoop(tenantID, userID string) {
	key := dispatchUserKey(tenantID, userID)
	for {
		busy := s.dispatchOnce(tenantID, userID)
		s.dispatch.mu.Lock()
		if busy {
			s.dispatch.retries[key]++
			if s.dispatch.retries[key] <= dispatchMaxRetries && s.dispatch.timer[key] == nil {
				s.dispatch.timer[key] = time.AfterFunc(dispatchRetry, func() {
					s.dispatch.mu.Lock()
					delete(s.dispatch.timer, key)
					s.dispatch.mu.Unlock()
					s.kickDispatch(tenantID, userID)
				})
			}
		} else {
			s.dispatch.retries[key] = 0
		}
		if s.dispatch.again[key] && !busy {
			s.dispatch.again[key] = false
			s.dispatch.mu.Unlock()
			continue
		}
		s.dispatch.again[key] = false
		s.dispatch.running[key] = false
		s.dispatch.mu.Unlock()
		return
	}
}

type dispatchJob struct {
	kind    string // delegate or wake
	botID   string
	noteIDs []string
	content string
	key     string
}

// dispatchDestructive mirrors the GUI's desktopBotDestructive: such orders
// wait for the person to confirm and are never started by Hub.
var dispatchInstallWord = regexp.MustCompile(`\b(install|reinstall)\b`)

// dispatchNegated matches a clause that forbids something ("do not ...
// delete anything", "不要删除"); such clauses are guardrails, not orders.
var dispatchNegated = regexp.MustCompile(`(?i)(\bdo not\b|\bdon't\b|\bnever\b|\bmust not\b|\bwithout\b|不要|不得|禁止|别|勿|不准)[^.。;；!！?？\n]*`)

func dispatchDestructive(text string) bool {
	text = dispatchNegated.ReplaceAllString(text, " ")
	for _, w := range []string{"安装", "卸载", "刪除", "删除", "付款", "支付", "代发", "代發", "清空"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "uninstall") || strings.Contains(lower, "wipe") || strings.Contains(lower, "delete") {
		return true
	}
	return dispatchInstallWord.MatchString(lower)
}

const dispatchWakePrompt = "你的 Hub 信箱里有其他机器人发来的新消息（见说明里的 Messages）。请处理：需要回复就用 action=notify_main kind=reply，需要安排别的机器人就用 action=delegate；处理完就结束本轮，不要做无关的事。/ New messages from teammate bots are in your Hub mailbox (shown in your instructions). Handle them, answer with notify_main kind=reply or hand work on with delegate, then finish."

// dispatchOnce runs at most one job. It reports true when the desktop was
// busy and the pass must be retried.
func (s *Service) dispatchOnce(tenantID, userID string) bool {
	job, ok := s.pickDispatchJob(tenantID, userID)
	if !ok {
		return false
	}
	admit := s.dispatchAdmit
	if admit == nil {
		admit = s.AdmitDesktopMessage
	}
	ctx, cancel := context.WithTimeout(context.Background(), dispatchAdmitWait)
	adm, err := admit(ctx, tenantID, userID, job.botID, job.content, "execute", job.key)
	cancel()
	if errors.Is(err, ErrDesktopBusy) {
		s.undoDispatchMark(tenantID, userID, job)
		return true
	}
	traceBot(job.botID, "hub_dispatch_"+job.kind, err, "key", job.key, "run", adm.RunID)
	if job.kind != "delegate" {
		if err == nil && strings.TrimSpace(adm.RunID) != "" {
			s.markWakeRun(tenantID, userID, job.noteIDs, strings.TrimSpace(adm.RunID))
		}
		return false
	}
	noteID := job.noteIDs[0]
	if err != nil {
		s.dispatch.mu.Lock()
		s.dispatch.fails[noteID]++
		n := s.dispatch.fails[noteID]
		s.dispatch.mu.Unlock()
		if n < delegateTries {
			s.undoDispatchMark(tenantID, userID, job)
			return true
		}
		s.reportDelegateRunEnd(tenantID, userID, job.botID, s.markDelegateRun(tenantID, userID, job.botID, noteID, job.key), "failed", Reply{}, err)
		return false
	}
	runID := strings.TrimSpace(adm.RunID)
	if runID == "" {
		runID = job.key
	}
	s.markDelegateRun(tenantID, userID, job.botID, noteID, runID)
	if adm.Settled {
		s.reportDelegateRunEnd(tenantID, userID, job.botID, runID, "succeeded", adm.Reply, nil)
	}
	return false
}

func (s *Service) markDelegateRun(tenantID, userID, botID, noteID, runID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return runID
	}
	if n := findCrossNote(rec.CrossNotes, noteID); n != nil && n.Target == botID {
		if strings.TrimSpace(n.RunID) == "" {
			n.RunID = runID
			_ = s.save(ctx, tenantID, rec)
		}
		return n.RunID
	}
	return runID
}

func (s *Service) undoDispatchMark(tenantID, userID string, job dispatchJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return
	}
	for _, id := range job.noteIDs {
		n := findCrossNote(rec.CrossNotes, id)
		if n == nil {
			continue
		}
		if job.kind == "delegate" && n.RunID == "" && n.StartedBy == "hub" {
			n.StartedAt, n.StartedBy = "", ""
		}
		if job.kind == "wake" {
			n.WokenAt = ""
		}
	}
	_ = s.save(ctx, tenantID, rec)
}

// pickDispatchJob chooses and marks the next job under s.mu: the oldest
// unstarted delegate first, then a wake for a bot with unread mail.
func (s *Service) pickDispatchJob(tenantID, userID string) (dispatchJob, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return dispatchJob{}, false
	}
	now := s.now()
	stamp := now.UTC().Format(time.RFC3339)
	primary := primaryBotID(rec, userID)
	changed := false
	var held []BotCrossNote
	defer func() {
		// Tell the main bot once that the order waits for the person.
		for _, n := range held {
			body := strings.TrimSpace(firstNonEmptyString(n.Order, n.Text))
			_, _, _ = s.reportLocked(&rec, userID, n.Target, BotCrossNote{Target: n.From, Topic: "blocked", Text: "这件需要人确认后才能做（安装、删除、付款等），请在 MaClaw 里确认：" + body, ReplyTo: n.ID, Key: "held-" + n.ID})
		}
		if changed {
			_ = s.save(ctx, tenantID, rec)
		}
	}()
	for i := range rec.CrossNotes {
		n := &rec.CrossNotes[i]
		if n.Owner != userID || n.Kind != "delegate" || n.RunID != "" || n.HeldAt != "" || resultFor(rec.CrossNotes, n.ID) != nil {
			continue
		}
		// Any claim (Hub's or a GUI's) is left alone while its admit may
		// still be in flight.
		if dispatchClaimFresh(n.StartedAt, now) || !botOwned(rec.Bots, userID, n.Target) {
			continue
		}
		if age, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil && now.Sub(age) > dispatchMaxAge {
			continue
		}
		body := strings.TrimSpace(firstNonEmptyString(n.Order, n.Text))
		if body == "" {
			continue
		}
		if dispatchDestructive(body) {
			n.HeldAt = stamp
			changed = true
			held = append(held, *n)
			continue
		}
		n.StartedAt, n.StartedBy = stamp, "hub"
		changed = true
		return dispatchJob{kind: "delegate", botID: n.Target, noteIDs: []string{n.ID}, content: body, key: "xdel-" + n.ID}, true
	}
	// Wakes: one per bot with unread, not yet woken mail.
	byBot := map[string][]int{}
	order := []string{}
	for i, n := range rec.CrossNotes {
		if n.Owner != userID || !mailboxKinds[n.Kind] || n.AckedAt != "" || n.WokenAt != "" || !botOwned(rec.Bots, userID, n.Target) {
			continue
		}
		if age, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil && now.Sub(age) > dispatchMaxAge {
			continue
		}
		if _, ok := byBot[n.Target]; !ok {
			order = append(order, n.Target)
		}
		byBot[n.Target] = append(byBot[n.Target], i)
	}
	for _, bot := range order {
		if bot == primary && s.guiReading(tenantID, userID, bot) {
			continue
		}
		if !s.takeWake(tenantID, userID, bot) {
			continue
		}
		ids := []string{}
		for _, i := range byBot[bot] {
			rec.CrossNotes[i].WokenAt = stamp
			ids = append(ids, rec.CrossNotes[i].ID)
		}
		changed = true
		return dispatchJob{kind: "wake", botID: bot, noteIDs: ids, content: dispatchWakePrompt, key: "xwake-" + ids[0]}, true
	}
	return dispatchJob{}, false
}

// takeWake bounds server-side wakes per bot, so two bots answering each
// other cannot loop the desktop.
func (s *Service) takeWake(tenantID, userID, botID string) bool {
	s.dispatch.mu.Lock()
	defer s.dispatch.mu.Unlock()
	s.dispatch.init()
	k := dispatchUserKey(tenantID, userID) + "|" + botID
	keep := s.dispatch.wakes[k][:0]
	for _, at := range s.dispatch.wakes[k] {
		if time.Since(at) < time.Hour {
			keep = append(keep, at)
		}
	}
	if len(keep) >= wakePerHour {
		s.dispatch.wakes[k] = keep
		return false
	}
	s.dispatch.wakes[k] = append(keep, time.Now())
	return true
}

// PostBotDelegateFromInstance is the server-side main bot handing work to a
// teammate (MaClawSrv action=delegate). Only the account primary may
// delegate. The target may be a bot name. Hub starts it (kickDispatch).
func (s *Service) PostBotDelegateFromInstance(ctx context.Context, tenantID, userID, instanceID string, in BotCrossNote) (BotCrossNote, error) {
	if s == nil {
		return BotCrossNote{}, ErrSettingsUnavailable
	}
	userID = strings.TrimSpace(userID)
	instanceID = strings.TrimSpace(instanceID)
	if userID == "" || instanceID == "" || strings.TrimSpace(in.Text+in.Order) == "" {
		return BotCrossNote{}, ErrInvalidInput
	}
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, userID)
	s.mu.Unlock()
	if err != nil {
		return BotCrossNote{}, err
	}
	from := ""
	for _, bot := range botsOwnedBy(rec.Bots, userID) {
		if strings.TrimSpace(bot.InstanceID) == instanceID {
			from = bot.ID
		}
	}
	if from == "" {
		return BotCrossNote{}, ErrNotFound
	}
	in.Target = resolveParticipant(rec, userID, in.Target)
	in.Kind = "delegate"
	return s.PostBotCrossTalk(ctx, tenantID, userID, from, in)
}

// BotTeam lists this user's bots (id, name, primary) for a server-side bot
// that needs to address a teammate.
func (s *Service) BotTeam(ctx context.Context, tenantID, userID string) ([]map[string]any, error) {
	s.mu.Lock()
	rec, err := s.loadForUser(ctx, tenantID, strings.TrimSpace(userID))
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	primary := primaryBotID(rec, userID)
	out := []map[string]any{}
	for _, b := range botsOwnedBy(rec.Bots, userID) {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name, "primary": b.ID == primary})
	}
	for _, b := range rec.LocalBots {
		if b.Owner == strings.TrimSpace(userID) {
			out = append(out, map[string]any{"id": b.ID, "name": b.Name, "primary": false, "location": "local", "device": b.Device})
		}
	}
	return out, nil
}

// dispatchClaimFresh: Hub's own claim lasts as long as one admit may wait.
// After that (Hub restarted mid-admit) the same key is sent again, which
// MaClawSrv answers with the run it already started.
func dispatchClaimFresh(started string, now time.Time) bool {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(started))
	return err == nil && now.Sub(at) < dispatchAdmitWait+30*time.Second
}
