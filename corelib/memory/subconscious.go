package memory

// SubconsciousEngine: continuous background reflection and knowledge maintenance.
// Inspired by OpenHuman's subconscious/engine.rs — runs periodically even when
// the user is not interacting, performing:
// - Reflection on recent conversations (extract insights)
// - Contradiction detection across knowledge entries
// - Fragment consolidation (merge related small entries)
// - User profile updates
//
// Unlike the old 6h Pipeline batch model, the Subconscious Engine runs
// incrementally every 30 minutes, processing only new/changed entries.

import (
	"context"
	"log"
	"sync"
	"time"
)

// SubconsciousEngine manages background knowledge maintenance.
type SubconsciousEngine struct {
	mu        sync.Mutex
	store     *Store
	interval  time.Duration
	stopCh    chan struct{}
	running   bool
	tickCount int

	// Subsystems (pluggable)
	reflector             SubconsciousReflector
	contradictionDetector SubconsciousContradictionDetector
	fragmentConsolidator  SubconsciousFragmentConsolidator
	profileUpdater        SubconsciousProfileUpdater

	// Tracking
	lastReflectAt     time.Time
	lastConsolidateAt time.Time
	lastProfileAt     time.Time
}

// SubconsciousReflector extracts insights from recent conversation entries.
type SubconsciousReflector interface {
	// ReflectRecent processes entries added since lastTime and returns new insights.
	ReflectRecent(ctx context.Context, entries []Entry, lastTime time.Time) ([]Entry, error)
}

// SubconsciousContradictionDetector finds contradictions between entries.
type SubconsciousContradictionDetector interface {
	// ScanContradictions checks recent entries against existing knowledge.
	ScanContradictions(ctx context.Context, recent []Entry, existing []Entry) []ContradictionPair
}

// ContradictionPair identifies two entries that contradict each other.
type ContradictionPair struct {
	ExistingID string
	NewID      string
	Reason     string
}

// SubconsciousFragmentConsolidator merges small related entries into larger coherent ones.
type SubconsciousFragmentConsolidator interface {
	// ConsolidateFragments finds and merges related small entries.
	ConsolidateFragments(ctx context.Context, entries []Entry) (merged []Entry, removedIDs []string, err error)
}

// SubconsciousProfileUpdater maintains the user profile from accumulated knowledge.
type SubconsciousProfileUpdater interface {
	// UpdateProfile refreshes the user profile based on current knowledge.
	UpdateProfile(ctx context.Context, entries []Entry) error
}

// SubconsciousConfig configures the engine behavior.
type SubconsciousConfig struct {
	Interval          time.Duration // tick interval (default 30min)
	ReflectEveryN     int           // reflect every N ticks (default 1)
	ConsolidateEveryN int           // consolidate every N ticks (default 2)
	ProfileEveryN     int           // update profile every N ticks (default 6)
	ContradictEveryN  int           // scan contradictions every N ticks (default 3)
}

// DefaultSubconsciousConfig returns sensible defaults.
func DefaultSubconsciousConfig() SubconsciousConfig {
	return SubconsciousConfig{
		Interval:          30 * time.Minute,
		ReflectEveryN:     1, // every tick (30min)
		ConsolidateEveryN: 2, // every 1h
		ProfileEveryN:     6, // every 3h
		ContradictEveryN:  3, // every 1.5h
	}
}

// NewSubconsciousEngine creates an engine backed by the given store.
func NewSubconsciousEngine(store *Store, cfg SubconsciousConfig) *SubconsciousEngine {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Minute
	}
	return &SubconsciousEngine{
		store:    store,
		interval: cfg.Interval,
		stopCh:   make(chan struct{}),
	}
}

// SetReflector sets the reflection subsystem.
func (e *SubconsciousEngine) SetReflector(r SubconsciousReflector) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reflector = r
}

// SetContradictionDetector sets the contradiction detection subsystem.
func (e *SubconsciousEngine) SetContradictionDetector(d SubconsciousContradictionDetector) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contradictionDetector = d
}

// SetFragmentConsolidator sets the fragment consolidation subsystem.
func (e *SubconsciousEngine) SetFragmentConsolidator(c SubconsciousFragmentConsolidator) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fragmentConsolidator = c
}

// SetProfileUpdater sets the profile update subsystem.
func (e *SubconsciousEngine) SetProfileUpdater(u SubconsciousProfileUpdater) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profileUpdater = u
}

// Start begins the background loop.
func (e *SubconsciousEngine) Start() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	go e.loop()
	log.Printf("[subconscious] started with interval=%s", e.interval)
}

// Stop terminates the background loop.
func (e *SubconsciousEngine) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()
	close(e.stopCh)
	log.Printf("[subconscious] stopped after %d ticks", e.tickCount)
}

// IsRunning returns whether the engine is active.
func (e *SubconsciousEngine) IsRunning() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// RunOnce executes one tick manually (for testing).
func (e *SubconsciousEngine) RunOnce(ctx context.Context) *TickResult {
	if e == nil {
		return nil
	}
	return e.tick(ctx)
}

// TickResult summarizes what happened in one tick.
type TickResult struct {
	ReflectedInsights   int
	ContradictionsFound int
	FragmentsMerged     int
	ProfileUpdated      bool
	Duration            time.Duration
}

func (e *SubconsciousEngine) loop() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			result := e.tick(ctx)
			cancel()
			if result != nil && (result.ReflectedInsights > 0 || result.ContradictionsFound > 0 || result.FragmentsMerged > 0) {
				log.Printf("[subconscious] tick #%d: insights=%d contradictions=%d merged=%d profile=%v duration=%s",
					e.tickCount, result.ReflectedInsights, result.ContradictionsFound, result.FragmentsMerged, result.ProfileUpdated, result.Duration)
			}
		}
	}
}

func (e *SubconsciousEngine) tick(ctx context.Context) *TickResult {
	start := time.Now()
	e.mu.Lock()
	e.tickCount++
	tick := e.tickCount
	reflector := e.reflector
	detector := e.contradictionDetector
	consolidator := e.fragmentConsolidator
	profiler := e.profileUpdater
	cfg := DefaultSubconsciousConfig()
	e.mu.Unlock()

	result := &TickResult{}

	// Reflection (every tick)
	if reflector != nil && tick%cfg.ReflectEveryN == 0 {
		insights := e.doReflect(ctx, reflector)
		result.ReflectedInsights = insights
	}

	// Contradiction detection
	if detector != nil && tick%cfg.ContradictEveryN == 0 {
		found := e.doContradictionScan(ctx, detector)
		result.ContradictionsFound = found
	}

	// Fragment consolidation
	if consolidator != nil && tick%cfg.ConsolidateEveryN == 0 {
		merged := e.doConsolidate(ctx, consolidator)
		result.FragmentsMerged = merged
	}

	// Profile update (least frequent)
	if profiler != nil && tick%cfg.ProfileEveryN == 0 {
		e.doProfileUpdate(ctx, profiler)
		result.ProfileUpdated = true
	}

	result.Duration = time.Since(start)
	return result
}

func (e *SubconsciousEngine) doReflect(ctx context.Context, r SubconsciousReflector) int {
	if e.store == nil {
		return 0
	}
	// Get entries added since last reflection
	entries := e.store.EntriesSince(e.lastReflectAt)
	if len(entries) == 0 {
		return 0
	}

	insights, err := r.ReflectRecent(ctx, entries, e.lastReflectAt)
	if err != nil {
		log.Printf("[subconscious] reflection failed: %v", err)
		return 0
	}

	for i := range insights {
		e.store.Save(insights[i])
	}
	e.lastReflectAt = time.Now()
	return len(insights)
}

func (e *SubconsciousEngine) doContradictionScan(ctx context.Context, d SubconsciousContradictionDetector) int {
	if e.store == nil {
		return 0
	}
	recent := e.store.EntriesSince(e.lastConsolidateAt)
	all := e.store.AllEntries()

	pairs := d.ScanContradictions(ctx, recent, all)
	for _, pair := range pairs {
		// Mark the existing entry as volatile
		e.store.MarkVolatile(pair.ExistingID)
		log.Printf("[subconscious] contradiction: %s vs %s — %s", pair.ExistingID, pair.NewID, pair.Reason)
	}
	return len(pairs)
}

func (e *SubconsciousEngine) doConsolidate(ctx context.Context, c SubconsciousFragmentConsolidator) int {
	if e.store == nil {
		return 0
	}
	entries := e.store.SmallEntries(100) // entries with < 100 tokens
	if len(entries) < 3 {
		return 0
	}

	merged, removedIDs, err := c.ConsolidateFragments(ctx, entries)
	if err != nil {
		log.Printf("[subconscious] consolidation failed: %v", err)
		return 0
	}

	for _, id := range removedIDs {
		e.store.Remove(id)
	}
	for i := range merged {
		e.store.Save(merged[i])
	}
	e.lastConsolidateAt = time.Now()
	return len(merged)
}

func (e *SubconsciousEngine) doProfileUpdate(ctx context.Context, p SubconsciousProfileUpdater) {
	if e.store == nil {
		return
	}
	all := e.store.AllEntries()
	if err := p.UpdateProfile(ctx, all); err != nil {
		log.Printf("[subconscious] profile update failed: %v", err)
	}
	e.lastProfileAt = time.Now()
}

// --- Store helper methods (added to support subconscious) ---

// EntriesSince returns entries created after the given time.
func (s *Store) EntriesSince(since time.Time) []Entry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []Entry
	for _, e := range s.entries {
		if e.CreatedAt.After(since) {
			result = append(result, e)
		}
	}
	return result
}

// AllEntries returns a copy of all entries.
func (s *Store) AllEntries() []Entry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// SmallEntries returns entries with estimated token count below threshold.
func (s *Store) SmallEntries(maxTokens int) []Entry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []Entry
	for _, e := range s.entries {
		if len([]rune(e.Content))/2 < maxTokens {
			result = append(result, e)
		}
	}
	return result
}

// MarkVolatile marks an entry as contradicted (volatile stability + stale flag).
func (s *Store) MarkVolatile(id string) {
	if s == nil {
		return
	}
	s.mu.RLock()
	var updated Entry
	found := false
	for _, entry := range s.entries {
		if entry.ID != id {
			continue
		}
		updated = entry
		found = true
		break
	}
	s.mu.RUnlock()
	if !found {
		return
	}
	updated.Stale = true
	if updated.Stability == nil {
		updated.Stability = &StabilityMeta{}
	}
	updated.Stability.RecordContradiction()
	if err := s.updateMetadataEntriesByID([]Entry{updated}); err != nil {
		log.Printf("[subconscious] mark volatile failed: %v", err)
	}
}

// Remove deletes an entry by ID.
func (s *Store) Remove(id string) {
	if s == nil {
		return
	}
	if err := s.UpdateEntriesAndDeleteIDs(nil, []string{id}); err != nil {
		log.Printf("[subconscious] remove failed: %v", err)
	}
}
