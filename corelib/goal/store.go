package goal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is a thread-safe goal store with synchronous atomic persistence.
// Each user has at most one active goal. Persistence uses atomic file writes
// (temp file + rename) to prevent corruption on crash.
type Store struct {
	mu      sync.RWMutex
	goals   map[string]*Goal // userID → current goal
	dataDir string           // directory for goal JSON files
}

// NewStore creates a goal store. If dataDir is non-empty, goals are persisted
// to {dataDir}/{userID}.json. If empty, the store is memory-only (for tests).
func NewStore(dataDir string) *Store {
	s := &Store{
		goals:   make(map[string]*Goal),
		dataDir: dataDir,
	}
	if dataDir != "" {
		_ = os.MkdirAll(dataDir, 0o755)
		s.loadAll()
	}
	return s
}

// Get returns the current goal for a user, or nil if none exists.
func (s *Store) Get(userID string) *Goal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneGoal(s.goals[userID])
}

// Snapshot returns a copy of every stored goal. Callers can walk the set
// without holding the store lock. A process restart uses this to find goals
// that were still active when the previous process stopped.
func (s *Store) Snapshot() []*Goal {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Goal, 0, len(s.goals))
	for _, g := range s.goals {
		if g == nil {
			continue
		}
		out = append(out, cloneGoal(g))
	}
	return out
}

// Set creates or replaces the goal for a user. A new goal_id is generated,
// usage counters are reset. Returns the created goal.
func (s *Store) Set(userID string, objective string, opts ...SetOption) (*Goal, error) {
	if objective == "" {
		return nil, fmt.Errorf("objective cannot be empty")
	}

	cfg := &setConfig{}
	for _, o := range opts {
		o(cfg)
	}

	now := time.Now()
	g := &Goal{
		GoalID:             NewGoalID(),
		UserID:             userID,
		Objective:          objective,
		Status:             StatusActive,
		TokenBudget:        cfg.tokenBudget,
		MaxTurns:           cfg.maxTurns,
		AcceptanceCriteria: cfg.acceptanceCriteria,
		ProjectPath:        cfg.projectPath,
		Phase:              PhasePlan,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if g.MaxTurns == 0 {
		g.MaxTurns = DefaultMaxTurns
	}

	s.mu.Lock()
	s.goals[userID] = g
	s.mu.Unlock()

	if err := s.persist(userID, g); err != nil {
		return g, fmt.Errorf("goal created but persist failed: %w", err)
	}
	return g, nil
}

// pinComposerClock freezes the composer elapsed clock when a goal ends.
// prev is the status before this write and next is the status after it.
// Entering a terminal status stamps now. Staying terminal keeps that stamp,
// and an older row with no stamp keeps its previous update. Leaving a terminal
// status banks the gap into ClockHoldSeconds so the reading does not jump.
func pinComposerClock(g *Goal, prev, next Status, now time.Time) {
	if g == nil {
		return
	}
	if !statusEndsGoalClock(next) {
		if statusEndsGoalClock(prev) {
			stop := g.StoppedAt
			if stop.IsZero() {
				stop = g.UpdatedAt
			}
			if !stop.IsZero() {
				if gap := now.Unix() - stop.Unix(); gap > 0 {
					g.ClockHoldSeconds += gap
				}
			}
		}
		g.StoppedAt = time.Time{}
		return
	}
	if statusEndsGoalClock(prev) && !g.StoppedAt.IsZero() {
		return
	}
	if statusEndsGoalClock(prev) && !g.UpdatedAt.IsZero() {
		g.StoppedAt = g.UpdatedAt
		return
	}
	g.StoppedAt = now
}

func touchGoalClock(g *Goal, now time.Time) {
	if g == nil {
		return
	}
	if statusEndsGoalClock(g.Status) {
		pinComposerClock(g, g.Status, g.Status, now)
	}
	g.UpdatedAt = now
}

// UpdateStatus transitions the goal's status. The caller must pass the expected
// goalID — if it doesn't match the current goal, the update is silently ignored
// (stale update protection). Returns true if the update was applied.
func (s *Store) UpdateStatus(userID, goalID string, status Status, summary string) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return false
	}
	now := time.Now()
	pinComposerClock(g, g.Status, status, now)
	g.Status = status
	if summary != "" {
		g.Summary = summary
	}
	g.UpdatedAt = now
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// AccountUsage atomically adds token and time deltas to the goal's usage
// counters. Also increments TurnsUsed. Returns false if goalID doesn't match
// (stale protection). If budget is exceeded, status is automatically set to
// budget_limited.
func (s *Store) AccountUsage(userID, goalID string, deltaTokens, deltaSeconds int) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return false
	}
	prev := g.Status
	g.TokensUsed += deltaTokens
	g.TimeUsedSeconds += deltaSeconds
	g.TurnsUsed++

	// Auto budget-limit enforcement
	if g.TokenBudget > 0 && g.TokensUsed >= g.TokenBudget && g.Status == StatusActive {
		g.Status = StatusBudgetLimit
		g.Summary = fmt.Sprintf("Token budget exhausted: used %d of %d", g.TokensUsed, g.TokenBudget)
	}
	if g.MaxTurns > 0 && g.TurnsUsed >= g.MaxTurns && g.Status == StatusActive {
		g.Status = StatusBudgetLimit
		g.Summary = fmt.Sprintf("Turn limit reached: %d of %d turns", g.TurnsUsed, g.MaxTurns)
	}
	now := time.Now()
	pinComposerClock(g, prev, g.Status, now)
	g.UpdatedAt = now
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// RecordNoToolTurn increments the consecutive no-tool counter. Returns false
// if goalID doesn't match. If threshold (2) is reached, auto-pauses the goal.
func (s *Store) RecordNoToolTurn(userID, goalID string) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return false
	}
	prev := g.Status
	g.ConsecutiveNoToolTurns++

	if g.ConsecutiveNoToolTurns >= 2 && g.Status == StatusActive {
		g.Status = StatusPaused
		g.Summary = "Auto-paused: agent produced no tool calls for 2 consecutive turns"
	}
	now := time.Now()
	pinComposerClock(g, prev, g.Status, now)
	g.UpdatedAt = now
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// ResetNoToolCounter resets the consecutive no-tool counter (called when tools
// are successfully used). Returns false if goalID doesn't match.
func (s *Store) ResetNoToolCounter(userID, goalID string) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return false
	}
	if g.ConsecutiveNoToolTurns == 0 {
		s.mu.Unlock()
		return true
	}
	g.ConsecutiveNoToolTurns = 0
	touchGoalClock(g, time.Now())
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// Pause transitions an active goal to paused. System-controlled only.
func (s *Store) Pause(userID, goalID string) bool {
	return s.UpdateStatus(userID, goalID, StatusPaused, "")
}

// Resume transitions a paused goal back to active.
func (s *Store) Resume(userID, goalID string) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID || g.Status != StatusPaused {
		s.mu.Unlock()
		return false
	}
	prev := g.Status
	g.Status = StatusActive
	g.ConsecutiveNoToolTurns = 0 // reset on resume
	now := time.Now()
	pinComposerClock(g, prev, g.Status, now)
	g.UpdatedAt = now
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// UpdateObjective replaces the command text of the current goal. Progress,
// status, and goal id stay. A completed goal is left finished.
func (s *Store) UpdateObjective(userID, goalID, objective string) bool {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return false
	}
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID || g.Status == StatusComplete {
		s.mu.Unlock()
		return false
	}
	if g.Objective == objective {
		s.mu.Unlock()
		return true
	}
	g.Objective = objective
	touchGoalClock(g, time.Now())
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// ReleaseForContinue re-arms a goal after the user explicitly chooses to keep
// going. A completed goal stays finished. Pause, failure, and an exhausted
// turn or token cap all become active again, with another window so the next
// turn is allowed to run.
func (s *Store) ReleaseForContinue(userID, goalID string) bool {
	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID || g.Status == StatusComplete {
		s.mu.Unlock()
		return false
	}
	prev := g.Status
	g.Status = StatusActive
	g.ConsecutiveNoToolTurns = 0
	g.Summary = ""
	if g.MaxTurns > 0 && g.TurnsUsed >= g.MaxTurns {
		g.MaxTurns = g.TurnsUsed + DefaultMaxTurns
	}
	if g.TokenBudget > 0 && g.TokensUsed >= g.TokenBudget {
		if g.TokenWindow < 1 {
			g.TokenWindow = g.TokenBudget
		}
		g.TokenBudget = g.TokensUsed + g.TokenWindow
	}
	now := time.Now()
	pinComposerClock(g, prev, g.Status, now)
	g.UpdatedAt = now
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return true
}

// SavePlan records the planning turn and switches the goal to execution.
// The goal_id must match. Steps and criteria are copied.
func (s *Store) SavePlan(userID, goalID, understanding string, steps []Step, criteria []string) error {
	understanding = strings.TrimSpace(understanding)
	steps, err := normalizeSteps(steps)
	if err != nil {
		return err
	}
	criteria = compactStrings(criteria)
	if understanding == "" {
		return fmt.Errorf("understanding cannot be empty")
	}
	if len(criteria) == 0 {
		return fmt.Errorf("acceptance criteria cannot be empty")
	}

	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return fmt.Errorf("goal not found")
	}
	if g.IsTerminal() {
		s.mu.Unlock()
		return fmt.Errorf("goal is terminal")
	}
	g.Understanding = understanding
	g.Steps = steps
	g.AcceptanceCriteria = append([]string(nil), criteria...)
	g.Phase = PhaseExecute
	touchGoalClock(g, time.Now())
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return nil
}

// UpdateStep records the outcome of one step. Index is 1-based.
func (s *Store) UpdateStep(userID, goalID string, index int, status StepStatus, result string) error {
	status, ok := canonicalStepStatus(string(status))
	if !ok {
		return fmt.Errorf("invalid step status")
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return fmt.Errorf("result cannot be empty")
	}

	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return fmt.Errorf("goal not found")
	}
	if g.IsTerminal() {
		s.mu.Unlock()
		return fmt.Errorf("goal is terminal")
	}
	found := false
	for i := range g.Steps {
		if g.Steps[i].Index != index {
			continue
		}
		if g.Steps[i].Status == StepDone {
			s.mu.Unlock()
			return fmt.Errorf("step already done")
		}
		g.Steps[i].Status = status
		g.Steps[i].Result = result
		found = true
		break
	}
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("step not found")
	}
	touchGoalClock(g, time.Now())
	data := s.marshalLocked(g)
	s.mu.Unlock()

	s.persistBytes(userID, data)
	return nil
}

// Clear removes the goal for a user entirely. Returns true if a goal existed.
func (s *Store) Clear(userID string) bool {
	s.mu.Lock()
	_, existed := s.goals[userID]
	delete(s.goals, userID)
	s.mu.Unlock()

	if existed && s.dataDir != "" {
		_ = os.Remove(s.filePath(userID))
	}
	return existed
}

// --- Persistence (synchronous atomic writes) ---

func (s *Store) filePath(userID string) string {
	// Sanitize userID for filesystem safety
	safe := sanitizeForFilename(userID)
	return filepath.Join(s.dataDir, safe+".json")
}

// marshalLocked serializes the goal to JSON bytes. MUST be called while holding mu.
func (s *Store) marshalLocked(g *Goal) []byte {
	data, _ := json.MarshalIndent(g, "", "  ")
	return data
}

// persistBytes writes pre-serialized JSON data to disk atomically.
// Safe to call without holding mu (file I/O only, no shared state access).
func (s *Store) persistBytes(userID string, data []byte) {
	if s.dataDir == "" || len(data) == 0 {
		return
	}
	tmp := s.filePath(userID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.filePath(userID))
}

// persist serializes and writes a goal to disk. Used by Set() which holds
// the lock only briefly for the map assignment.
func (s *Store) persist(userID string, g *Goal) error {
	if s.dataDir == "" {
		return nil
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename
	tmp := s.filePath(userID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath(userID))
}

func (s *Store) loadAll() {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dataDir, e.Name()))
		if err != nil {
			continue
		}
		var g Goal
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		if g.UserID != "" {
			s.goals[g.UserID] = &g
		}
	}
}

// sanitizeForFilename replaces characters unsafe for filenames.
func sanitizeForFilename(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "_"
	}
	return string(b)
}

// --- SetOption functional options for Set() ---

type setConfig struct {
	tokenBudget        int
	maxTurns           int
	acceptanceCriteria []string
	projectPath        string
}

// SetOption configures optional parameters for goal creation.
type SetOption func(*setConfig)

// WithTokenBudget sets the maximum token budget.
func WithTokenBudget(budget int) SetOption {
	return func(c *setConfig) { c.tokenBudget = budget }
}

// WithMaxTurns sets the maximum iteration count.
func WithMaxTurns(turns int) SetOption {
	return func(c *setConfig) { c.maxTurns = turns }
}

// WithAcceptanceCriteria sets verifiable completion conditions.
func WithAcceptanceCriteria(criteria []string) SetOption {
	return func(c *setConfig) { c.acceptanceCriteria = criteria }
}

// WithProjectPath sets the project working directory.
func WithProjectPath(path string) SetOption {
	return func(c *setConfig) { c.projectPath = path }
}
