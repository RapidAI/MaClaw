package agentservice

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botlog"
)

// runSupplementMaxPerRun caps how much supplementary text one admitted run can
// accumulate. It is a guard against an unbounded queue, not a product limit:
// a run that already holds this many pending items gets no more. Each item is
// also clipped by runSupplementMaxContent.
const (
	runSupplementMaxPerRun  = 32
	runSupplementMaxContent = 4000
	runSupplementIDPrefix   = "sup"
	runSupplementMaxApplied = 64
	// runSupplementMaxClosedRuns bounds how many finished runs keep a readable
	// outcome. The outcome has to outlive the run or a caller can never learn a
	// supplement was missed, but nobody is obliged to come back for it, so it
	// is capped instead of growing for the life of the process.
	runSupplementMaxClosedRuns = 256
	runSupplementStateQueued   = "queued"
	// Applied means a loop iteration consumed it. It stays in memory so the
	// rotating in-conversation summary can be rebuilt from scratch; it is never
	// re-delivered.
	runSupplementStateApplied = "applied"
	runSupplementStateMissed  = "missed"
)

// RunSupplement is one piece of supplementary information this person aimed at
// a run that was already running.
//
// It is deliberately NOT a new task: it has no tools, no history of its own,
// and no identity of its own. It exists only to steer the run it names.
type RunSupplement struct {
	ID        string     `json:"id"`
	RunID     string     `json:"run_id"`
	Content   string     `json:"content"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

// RunSupplementReceipt is what the caller shows the person. State is one of
// queued, applied, or missed, and there is deliberately no fourth value: the
// person must never be left wondering whether input took effect.
type RunSupplementReceipt struct {
	SupplementID string `json:"supplement_id"`
	Accepted     bool   `json:"accepted"`
	State        string `json:"state"`
	Detail       string `json:"detail,omitempty"`
	RunID        string `json:"run_id,omitempty"`
}

// runSupplements holds every supplement keyed by run. It is guarded by the
// service's runMu, the same lock that guards runningRuns, so a queue append
// racing a loop's take cannot corrupt the map.
//
// A run's items survive its terminal state, bounded, so that the outcome is
// still answerable afterwards. Deleting them at reap time made a missed
// supplement unobservable: the caller's receipt stayed at "queued" forever
// while the text had already been dropped.
type runSupplements struct {
	items map[string][]*RunSupplement
	// closed holds the terminal outcome of runs that have been reaped, most
	// recently closed last. It is bounded because nothing guarantees a caller
	// ever reads it.
	closed map[string][]*RunSupplement
	order  []string
}

func newRunSupplements() *runSupplements {
	return &runSupplements{items: map[string][]*RunSupplement{}, closed: map[string][]*RunSupplement{}}
}

// append adds one supplement to a run's queue. The caller must hold runMu.
func (r *runSupplements) append(runID, content string, now time.Time) (*RunSupplement, bool) {
	if r.items == nil {
		r.items = map[string][]*RunSupplement{}
	}
	existing := r.items[runID]
	pending := 0
	for _, item := range existing {
		if item.State == runSupplementStateQueued {
			pending++
		}
	}
	if pending >= runSupplementMaxPerRun {
		return nil, false
	}
	item := &RunSupplement{
		ID:        NewID(runSupplementIDPrefix),
		RunID:     runID,
		Content:   clipRunSupplements(content, runSupplementMaxContent),
		State:     runSupplementStateQueued,
		CreatedAt: now,
	}
	r.items[runID] = append(existing, item)
	return item, true
}

// takeQueued returns every queued supplement for a run and marks them applied
// in the same critical section. This is what makes delivery at-most-once: a
// second call cannot see the same items again.
//
// Applied items are retained (bounded) rather than deleted: the rotating
// conversation summary is rebuilt from the full applied set, so a loop-host
// restart inside one run can still render the whole set.
func (r *runSupplements) takeQueued(runID string, now time.Time) []*RunSupplement {
	items := r.items[runID]
	taken := make([]*RunSupplement, 0, len(items))
applied:
	for _, item := range items {
		if item.State != runSupplementStateQueued {
			continue
		}
		item.State = runSupplementStateApplied
		at := now
		item.AppliedAt = &at
		taken = append(taken, item)
		if len(taken) >= runSupplementMaxPerRun {
			// Bounded by the same cap as admission, so one drain cannot grow
			// without limit either.
			break applied
		}
	}
	if len(taken) > 0 {
		r.trimLocked(runID)
	}
	return taken
}

// applied returns every supplement that reached the run, newest last. Used to
// rebuild the rotating summary from scratch.
func (r *runSupplements) applied(runID string) []*RunSupplement {
	out := make([]*RunSupplement, 0, len(r.items[runID]))
	for _, item := range r.items[runID] {
		if item.State == runSupplementStateApplied || item.State == runSupplementStateQueued {
			out = append(out, item)
		}
	}
	return out
}

// reap marks whatever never reached the loop and reports those items. The run
// reached a terminal state, so nothing will consume them after this point.
// Reporting them is mandatory: silently dropping input the person typed is
// worse than telling them it did not apply.
//
// The items are moved aside rather than deleted. A caller that asks about this
// run after it ended — the usual case, since a receipt is checked once the turn
// settles — must still be able to see that the supplement was missed, or it
// keeps believing it took effect.
func (r *runSupplements) reap(runID string) []*RunSupplement {
	missed := make([]*RunSupplement, 0)
	for _, item := range r.items[runID] {
		if item.State != runSupplementStateQueued {
			continue
		}
		item.State = runSupplementStateMissed
		missed = append(missed, item)
	}
	kept := r.items[runID]
	delete(r.items, runID)
	if len(kept) == 0 {
		return missed
	}
	if r.closed == nil {
		r.closed = map[string][]*RunSupplement{}
	}
	if _, exists := r.closed[runID]; !exists {
		r.order = append(r.order, runID)
	}
	r.closed[runID] = kept
	r.trimClosedLocked()
	return missed
}

// trimClosedLocked bounds how many finished runs keep an outcome readable.
// Nothing guarantees a caller comes back for it, so retention is capped rather
// than left to grow for the life of the process.
func (r *runSupplements) trimClosedLocked() {
	for len(r.order) > runSupplementMaxClosedRuns {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.closed, oldest)
	}
}

// outcome reports the final state recorded for a run that has already been
// reaped. It is what turns a receipt from "queued" into "missed" once the run
// is over.
func (r *runSupplements) outcome(runID string) []*RunSupplement {
	return r.closed[runID]
}

// trimLocked keeps one run's retained applied set bounded.
func (r *runSupplements) trimLocked(runID string) {
	items := r.items[runID]
	if len(items) <= runSupplementMaxApplied {
		return
	}
	r.items[runID] = append([]*RunSupplement(nil), items[len(items)-runSupplementMaxApplied:]...)
}

func clipRunSupplements(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// AppendRunSupplement queues supplementary information for a run this person
// owns that is still running.
//
// Ownership is checked in full: tenant, user, instance and run together. A
// run id alone is never enough, and a run belonging to someone else is
// reported as not found rather than not running, so this call does not
// become an existence oracle for other people's run ids.
func (s *Service) AppendRunSupplement(ctx context.Context, p Principal, instanceID, runID, content string) (*RunSupplementReceipt, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrRunNotFound
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrRunSupplementEmpty
	}
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		// Another principal's run is indistinguishable from a missing one.
		return nil, ErrRunNotFound
	}
	if run.Status != RunStatusRunning {
		return nil, ErrRunNotRunning
	}
	now := s.now()
	s.runMu.Lock()
	var item *RunSupplement
	var ok bool
	if s.supplements == nil {
		s.supplements = newRunSupplements()
	}
	item, ok = s.supplements.append(run.ID, content, now)
	s.runMu.Unlock()
	if !ok {
		return &RunSupplementReceipt{Accepted: false, State: runSupplementStateQueued, Detail: "too many pending supplements", RunID: run.ID}, nil
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "run.supplement_queued", ResourceType: "run", ResourceID: run.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"instance_id": instanceID, "session_id": run.SessionID, "supplement_id": item.ID}})
	botlog.Write(runID, "srv.supplement_queued", nil,
		"run", run.ID,
		"supplement", item.ID,
		"text_len", strconv.Itoa(len(item.Content)),
	)
	return &RunSupplementReceipt{SupplementID: item.ID, Accepted: true, State: item.State, RunID: run.ID}, nil
}

// RunSupplements lists the supplements recorded for one run. Items still queued
// are waiting for the next loop iteration; missed means the run ended before
// the loop consumed them.
func (s *Service) RunSupplements(ctx context.Context, p Principal, instanceID, runID string) ([]RunSupplement, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrRunNotFound
	}
	if _, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID); err != nil {
		return nil, ErrRunNotFound
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.supplements == nil {
		return []RunSupplement{}, nil
	}
	// A run that already ended keeps its outcome readable, so a caller does not
	// go on believing a supplement was queued after it was dropped.
	items := s.supplements.applied(runID)
	if len(items) == 0 {
		items = s.supplements.outcome(runID)
	}
	out := make([]RunSupplement, 0, len(items))
	for _, item := range items {
		out = append(out, *item)
	}
	return out, nil
}

// takePendingRunSupplements is the loop-side drain. Called once per iteration
// through the hook; every item it returns is already marked applied, so later
// calls cannot return it again.
func (s *Service) takePendingRunSupplements(runID string) []*RunSupplement {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		// No run identity means no safe delivery. Refusing here is what keeps
		// the queue from falling back to a session-wide guess.
		return nil
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.supplements == nil {
		return nil
	}
	return s.supplements.takeQueued(runID, s.now())
}

// reapRunSupplements retires one run's queue. It runs on every terminal path,
// not just success: a cancelled or panicked run must not leave the person's
// input unreported either.
func (s *Service) reapRunSupplements(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	s.runMu.Lock()
	var missed []*RunSupplement
	if s.supplements != nil {
		missed = s.supplements.reap(runID)
	}
	s.runMu.Unlock()
	for _, item := range missed {
		botlog.Write(runID, "srv.supplement_missed", nil,
			"run", runID,
			"supplement", item.ID,
		)
	}
}
