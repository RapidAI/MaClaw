package agent

import (
	"fmt"
	"sync"
	"time"
)

// BackgroundLoopView is a read-only snapshot for the frontend.
type BackgroundLoopView struct {
	ID          string `json:"id"`
	SlotKind    string `json:"slot_kind"`
	Description string `json:"description"`
	Iteration   int    `json:"iteration"`
	MaxIter     int    `json:"max_iter"`
	Status      string `json:"status"`
	SessionID   string `json:"session_id"`
	StartedAt   string `json:"started_at"`
	QueuedCount int    `json:"queued_count"`
}

// pendingTask represents a queued task waiting for a slot.
type pendingTask struct {
	slotKind    SlotKind
	userID      string
	description string
	maxIter     int
	resultC     chan *LoopContext
}

// BackgroundLoopManager manages all background loops with slot-based
// concurrency control. Each SlotKind has a configurable max (default 1).
type BackgroundLoopManager struct {
	mu      sync.RWMutex
	loops   map[string]*LoopContext
	statusC chan StatusEvent

	slotLimits map[SlotKind]int
	slotCounts map[SlotKind]int
	queues     map[SlotKind][]*pendingTask

	idCounter int

	// OnChange is called when loop state changes (for UI event emission).
	OnChange func()
}

// NewBackgroundLoopManager creates a manager with default slot limits.
func NewBackgroundLoopManager(statusC chan StatusEvent) *BackgroundLoopManager {
	return &BackgroundLoopManager{
		loops:   make(map[string]*LoopContext),
		statusC: statusC,
		slotLimits: map[SlotKind]int{
			SlotKindCoding:    2,
			SlotKindScheduled: 1,
			SlotKindAuto:      1,
			SlotKindSSH:       10,
			SlotKindBrowser:   2,
		},
		slotCounts: make(map[SlotKind]int),
		queues:     make(map[SlotKind][]*pendingTask),
	}
}

// Spawn creates a new Background Loop if the slot is available.
// Returns nil if the slot is full.
func (m *BackgroundLoopManager) Spawn(slotKind SlotKind, userID string,
	description string, maxIter int, httpClient interface{}) *LoopContext {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit := m.slotLimits[slotKind]
	if limit <= 0 {
		limit = 1
	}
	if m.slotCounts[slotKind] >= limit {
		return nil
	}
	return m.spawnLocked(slotKind, description, maxIter)
}

// SpawnOrQueue creates a loop if slot available, otherwise queues.
func (m *BackgroundLoopManager) SpawnOrQueue(slotKind SlotKind, userID string,
	description string, maxIter int) (*LoopContext, <-chan *LoopContext) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit := m.slotLimits[slotKind]
	if limit <= 0 {
		limit = 1
	}
	if m.slotCounts[slotKind] < limit {
		ctx := m.spawnLocked(slotKind, description, maxIter)
		return ctx, nil
	}

	ch := make(chan *LoopContext, 1)
	pt := &pendingTask{
		slotKind:    slotKind,
		userID:      userID,
		description: description,
		maxIter:     maxIter,
		resultC:     ch,
	}
	m.queues[slotKind] = append(m.queues[slotKind], pt)
	return nil, ch
}

func (m *BackgroundLoopManager) spawnLocked(slotKind SlotKind, description string, maxIter int) *LoopContext {
	m.idCounter++
	id := fmt.Sprintf("bg-%s-%d", slotKind.String(), m.idCounter)
	ctx := NewBackgroundLoopContext(id, slotKind, description, maxIter, nil, m.statusC)
	m.loops[id] = ctx
	m.slotCounts[slotKind]++
	m.notifyChange()
	return ctx
}

// Get returns the LoopContext for a given loop ID.
func (m *BackgroundLoopManager) Get(loopID string) *LoopContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loops[loopID]
}

// List returns all active background loops.
func (m *BackgroundLoopManager) List() []*LoopContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*LoopContext, 0, len(m.loops))
	for _, ctx := range m.loops {
		result = append(result, ctx)
	}
	return result
}

// ListViews returns frontend-friendly snapshots of all background loops.
func (m *BackgroundLoopManager) ListViews() []BackgroundLoopView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	views := make([]BackgroundLoopView, 0, len(m.loops))
	for _, ctx := range m.loops {
		views = append(views, BackgroundLoopView{
			ID:          ctx.ID,
			SlotKind:    ctx.SlotKind.String(),
			Description: ctx.Description,
			Iteration:   ctx.Iteration(),
			MaxIter:     ctx.MaxIterations(),
			Status:      ctx.State(),
			SessionID:   ctx.SessionID,
			StartedAt:   ctx.StartedAt.Format(time.RFC3339),
			QueuedCount: len(m.queues[ctx.SlotKind]),
		})
	}
	return views
}

// QueueLength returns the number of queued tasks for a given slot kind.
func (m *BackgroundLoopManager) QueueLength(kind SlotKind) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.queues[kind])
}

// RunningCount returns the number of running loops for a given slot kind.
func (m *BackgroundLoopManager) RunningCount(kind SlotKind) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.slotCounts[kind]
}

// SendContinue sends additional rounds to a paused background loop.
func (m *BackgroundLoopManager) SendContinue(loopID string, additionalRounds int) error {
	m.mu.RLock()
	ctx, ok := m.loops[loopID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("loop %s not found", loopID)
	}
	if ctx.State() != "paused" {
		return fmt.Errorf("loop %s is not paused (state=%s)", loopID, ctx.State())
	}
	select {
	case ctx.ContinueC <- additionalRounds:
		return nil
	default:
		return fmt.Errorf("loop %s continue channel full", loopID)
	}
}

// Stop gracefully stops a background loop and dequeues the next task.
func (m *BackgroundLoopManager) Stop(loopID string) {
	m.mu.Lock()
	ctx, ok := m.loops[loopID]
	if !ok {
		m.mu.Unlock()
		return
	}
	slotKind := ctx.SlotKind
	delete(m.loops, loopID)
	m.slotCounts[slotKind]--
	if m.slotCounts[slotKind] < 0 {
		m.slotCounts[slotKind] = 0
	}
	var next *pendingTask
	if q := m.queues[slotKind]; len(q) > 0 {
		next = q[0]
		m.queues[slotKind] = q[1:]
	}
	m.mu.Unlock()

	ctx.Cancel()
	ctx.SetState("stopped")

	if next != nil {
		m.mu.Lock()
		newCtx := m.spawnLocked(next.slotKind, next.description, next.maxIter)
		m.mu.Unlock()
		if next.resultC != nil {
			next.resultC <- newCtx
		}
		return
	}
	m.notifyChange()
}

// Complete marks a loop as completed and dequeues the next task.
func (m *BackgroundLoopManager) Complete(loopID string) {
	m.mu.Lock()
	ctx, ok := m.loops[loopID]
	if !ok {
		m.mu.Unlock()
		return
	}
	slotKind := ctx.SlotKind
	delete(m.loops, loopID)
	m.slotCounts[slotKind]--
	if m.slotCounts[slotKind] < 0 {
		m.slotCounts[slotKind] = 0
	}
	var next *pendingTask
	if q := m.queues[slotKind]; len(q) > 0 {
		next = q[0]
		m.queues[slotKind] = q[1:]
	}
	m.mu.Unlock()

	ctx.SetState("completed")

	if next != nil {
		m.mu.Lock()
		newCtx := m.spawnLocked(next.slotKind, next.description, next.maxIter)
		m.mu.Unlock()
		if next.resultC != nil {
			next.resultC <- newCtx
		}
		return
	}
	m.notifyChange()
}

func (m *BackgroundLoopManager) notifyChange() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

// NotifyChange is the exported variant, allowing external callers to trigger
// a UI refresh (e.g. SSH session update callbacks).
func (m *BackgroundLoopManager) NotifyChange() {
	m.notifyChange()
}

// SetSlotLimit dynamically adjusts the concurrency limit for a given SlotKind.
// The new limit takes effect immediately; already-running loops are not affected.
func (m *BackgroundLoopManager) SetSlotLimit(kind SlotKind, limit int) {
	if limit <= 0 {
		limit = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slotLimits[kind] = limit
}
