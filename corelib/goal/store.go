package goal

import (
	"database/sql"
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
	dataDir string           // directory for the goal database and legacy JSON files
	db      *sql.DB
}

// NewStore creates a goal store. If dataDir is non-empty, goals are persisted
// to {dataDir}/goals.db with one row per user. {userID}.json files are imported
// once. If dataDir is empty, the store is memory-only (for tests).
func NewStore(dataDir string) *Store {
	s := &Store{
		goals:   make(map[string]*Goal),
		dataDir: dataDir,
	}
	if dataDir != "" {
		_ = os.MkdirAll(dataDir, 0o755)
		if err := s.openDB(); err != nil {
			fmt.Printf("[goal] sqlite open failed, keeping legacy JSON files: %v\n", err)
		}
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

	s.mu.Lock()
	if prev := s.goals[userID]; prev != nil {
		g.StateRev = prev.StateRev
		g.TextRev = prev.TextRev
	}
	bumpGoalState(g)
	bumpGoalText(g)
	s.goals[userID] = g
	snap := cloneGoal(g)
	s.mu.Unlock()

	if err := s.persist(userID, snap); err != nil {
		return g, fmt.Errorf("goal created but persist failed: %w", err)
	}
	return g, nil
}

// pinComposerClock freezes the composer elapsed clock when a goal pauses or ends.
// prev is the status before this write and next is the status after it.
// Entering a clock-stopping status stamps now. Staying there keeps that stamp,
// and an older row with no stamp keeps its previous update. Leaving that status
// banks the gap into ClockHoldSeconds so the reading does not jump.
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalText(g)
	bumpGoalState(g)
	s.releaseAndPersist(userID, g)
	return true
}

// ReleaseForContinue re-arms a goal after the user explicitly chooses to keep
// going. A completed goal stays finished. Pause, failure, and an exhausted
// turn or token cap all become active again. An exhausted turn cap is lifted,
// so continuation is unlimited from there. A filled token cap gets another
// window of the same size.
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
		g.MaxTurns = 0
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
	bumpGoalState(g)
	s.releaseAndPersistState(userID, g)
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
	bumpGoalText(g)
	bumpGoalState(g)
	s.releaseAndPersist(userID, g)
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
	bumpGoalText(g)
	bumpGoalState(g)
	s.releaseAndPersist(userID, g)
	return nil
}

// AppendStepNote records a follow-up on a step that is already done. It does
// not reopen the step or move the current step. saved is false when the note
// was already recorded.
func (s *Store) AppendStepNote(userID, goalID string, index int, note string) (bool, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return false, fmt.Errorf("result cannot be empty")
	}

	s.mu.Lock()
	g := s.goals[userID]
	if g == nil || g.GoalID != goalID {
		s.mu.Unlock()
		return false, fmt.Errorf("goal not found")
	}
	if g.IsTerminal() {
		s.mu.Unlock()
		return false, fmt.Errorf("goal is terminal")
	}
	found := false
	for i := range g.Steps {
		if g.Steps[i].Index != index {
			continue
		}
		if g.Steps[i].Status != StepDone {
			s.mu.Unlock()
			return false, fmt.Errorf("step is not done")
		}
		next := joinStepNote(g.Steps[i].Result, note)
		if next == g.Steps[i].Result {
			s.mu.Unlock()
			return false, nil
		}
		g.Steps[i].Result = next
		found = true
		break
	}
	if !found {
		s.mu.Unlock()
		return false, fmt.Errorf("step not found")
	}
	touchGoalClock(g, time.Now())
	bumpGoalText(g)
	bumpGoalState(g)
	s.releaseAndPersist(userID, g)
	return true, nil
}

// joinStepNote keeps the new note. A long existing result is shortened from
// the end so the follow-up is not the part that gets cut off.
func joinStepNote(result, note string) string {
	note = strings.TrimSpace(note)
	const maxNote = 2000
	const maxTotal = 12000
	noteRunes := []rune(note)
	if len(noteRunes) > maxNote {
		note = string(noteRunes[:maxNote])
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return note
	}
	if stepNoteRecorded(result, note) {
		return result
	}
	suffix := "\n补充: " + note
	room := maxTotal - len([]rune(suffix))
	resultRunes := []rune(result)
	if room < 1 {
		suffixRunes := []rune(suffix)
		if len(suffixRunes) > maxTotal {
			return string(suffixRunes[:maxTotal])
		}
		return string(suffixRunes)
	}
	if len(resultRunes) > room {
		result = string(resultRunes[:room])
	}
	return strings.TrimSpace(result) + suffix
}

// stepNoteRecorded reports an exact earlier copy of note. A sentence that
// merely appears inside the original result is still a new follow-up.
func stepNoteRecorded(result, note string) bool {
	if strings.TrimSpace(result) == note {
		return true
	}
	parts := strings.Split(result, "\n补充: ")
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == note {
			return true
		}
	}
	return false
}

// Clear removes the goal for a user entirely. Returns true if a goal existed.
func (s *Store) Clear(userID string) bool {
	s.mu.Lock()
	_, existed := s.goals[userID]
	delete(s.goals, userID)
	s.mu.Unlock()

	if existed && s.dataDir != "" {
		if s.db != nil {
			_ = s.deleteGoal(userID)
		}
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

// releaseAndPersistState writes status, phase, summary, counters, and times.
// Objective, understanding, acceptance, and steps stay on releaseAndPersist.
// The JSON fallback still uses that full write. The caller holds mu.
func bumpGoalState(g *Goal) {
	if g != nil {
		g.StateRev++
	}
}

func bumpGoalText(g *Goal) {
	if g != nil {
		g.TextRev++
	}
}

func (s *Store) releaseAndPersistState(userID string, g *Goal) {
	if s.db == nil {
		s.releaseAndPersist(userID, g)
		return
	}
	snap := cloneGoal(g)
	s.mu.Unlock()
	if snap != nil && strings.TrimSpace(snap.UserID) == "" {
		snap.UserID = userID
	}
	wrote, err := s.updateGoalState(snap)
	if err != nil || !wrote {
		_ = s.insertGoalIfAbsent(snap)
	}
}

// releaseAndPersist clones g while mu is held, unlocks, then writes the clone.
// SQLite stores the columns directly. JSON remains the fallback encoding.
func (s *Store) releaseAndPersist(userID string, g *Goal) {
	if s.db != nil {
		snap := cloneGoal(g)
		s.mu.Unlock()
		if snap != nil && strings.TrimSpace(snap.UserID) == "" {
			snap.UserID = userID
		}
		_ = s.persistGoalSnapshot(snap)
		return
	}
	data := s.marshalLocked(g)
	s.mu.Unlock()
	s.persistBytes(userID, data)
}

// persistBytes writes pre-serialized JSON data to disk atomically.
// Safe to call without holding mu (file I/O only, no shared state access).
func (s *Store) persistBytes(userID string, data []byte) {
	if s.dataDir == "" || len(data) == 0 {
		return
	}
	if s.db != nil {
		var g Goal
		if err := json.Unmarshal(data, &g); err != nil {
			return
		}
		if strings.TrimSpace(g.UserID) == "" {
			g.UserID = userID
		}
		_ = s.upsertGoal(&g)
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
	if s.db != nil {
		if g != nil && strings.TrimSpace(g.UserID) == "" {
			g.UserID = userID
		}
		return s.upsertGoal(g)
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
	if s.db != nil {
		s.loadFromDB()
		s.importLegacyJSON()
	} else {
		_ = s.loadLegacyJSONFiles(true)
	}
	upgraded := make([]string, 0)
	for userID, g := range s.goals {
		if g != nil && automaticTurnCap(g.MaxTurns) {
			g.MaxTurns = 0
			upgraded = append(upgraded, userID)
		}
	}
	for _, userID := range upgraded {
		g := s.goals[userID]
		if g != nil && g.MaxTurns == 0 {
			g.StateRev++
			if wrote, err := s.updateGoalState(g); err != nil || !wrote {
				_ = s.persist(userID, g)
			}
		}
	}
}

// loadLegacyJSONFiles reads {user}.json files into memory.
// overwrite replaces a user already loaded from SQLite. The one-shot import
// passes false, so a stale file leaves the SQLite row in place.
// The bool is true when the directory listing and every JSON file were read
// and parsed. A read or parse error is false, and importLegacyJSON then leaves
// goals_legacy_import unset so the next open reads the files again.
func (s *Store) loadLegacyJSONFiles(overwrite bool) bool {
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		fmt.Printf("[goal] legacy dir %s: %v\n", s.dataDir, err)
		return false
	}
	clean := true
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dataDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("[goal] legacy file %s: %v\n", e.Name(), err)
			clean = false
			continue
		}
		var g Goal
		if err := json.Unmarshal(data, &g); err != nil {
			fmt.Printf("[goal] legacy file %s: %v\n", e.Name(), err)
			clean = false
			continue
		}
		if g.UserID == "" {
			continue
		}
		if !overwrite {
			if _, exists := s.goals[g.UserID]; exists {
				continue
			}
		}
		s.goals[g.UserID] = &g
	}
	return clean
}

// automaticTurnCap reports a max_turns value create used to write when the
// caller omitted one. Load clears only these. The goal's status is left
// alone, so a budget stop stays stopped until the user continues.
func automaticTurnCap(turns int) bool {
	return turns == LegacyDefaultMaxTurns || turns == RaisedDefaultMaxTurns
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
