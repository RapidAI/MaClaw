package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// historyWindowSize is the number of recent same-level memories used as
// historical context during consolidation (TiMem w=3).
const historyWindowSize = 3

// Consolidator performs TiMem-style stratified memory consolidation.
// It transforms child memories into higher-level abstractions using
// level-specific instruction prompts, without fine-tuning.
//
// Two-tier scheduling:
//   - Online (L1): invoked after each dialog turn via ConsolidateSegment
//   - Scheduled (L2-L5): invoked when temporal windows close via ConsolidateLevel
type Consolidator struct {
	mu    sync.RWMutex
	store *Store
	tree  *TemporalTree
	llm   LLMChatCaller
}

// NewConsolidator creates a Consolidator.
func NewConsolidator(store *Store, tree *TemporalTree, llm LLMChatCaller) *Consolidator {
	return &Consolidator{store: store, tree: tree, llm: llm}
}

// SetLLM rewires the LLM used by online and scheduled consolidation.
func (c *Consolidator) SetLLM(llm LLMChatCaller) {
	c.mu.Lock()
	c.llm = llm
	c.mu.Unlock()
}

// ConsolidateSegment performs online L1 consolidation for a single dialog turn.
// It creates a segment-level memory from the raw user-assistant exchange.
// ownerID is used for multi-tenant isolation (empty string for single-user mode).
func (c *Consolidator) ConsolidateSegment(ctx context.Context, userMsg, assistantMsg string, turnTime time.Time, ownerID string) (*ConsolidationResult, error) {
	c.mu.RLock()
	llm := c.llm
	c.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &ConsolidationResult{Level: LevelSegment}, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	start := time.Now()

	// Build consolidation prompt with history context.
	history := c.getHistoryContext(LevelSegment)
	prompt := c.buildSegmentPrompt(userMsg, assistantMsg, history)

	resp, err := llm.ChatCall(prompt)
	if err != nil {
		return nil, fmt.Errorf("consolidator: L1 segment: %w", err)
	}

	content := strings.TrimSpace(resp)
	if content == "" {
		return &ConsolidationResult{Level: LevelSegment}, nil
	}

	// Create the segment entry.
	interval := TimeInterval{Start: turnTime, End: turnTime}
	boundary := tmtBoundaryFromEntries(nil, ownerID, interval, "conversation")
	result, err := c.store.UpsertEntryByTags(UpsertByTagsOptions{
		Title:            "TMT segment",
		Content:          content,
		Category:         CategoryConversationSummary,
		Tags:             []string{"tmt", "L1", "segment", ownerID, "at:" + turnTime.UTC().Format(time.RFC3339Nano)},
		IdentityTagCount: 5,
		Level:            LevelSegment,
		Interval:         &interval,
		OwnerID:          ownerID,
		SourceType:       "tmt_consolidation",
		DerivedKind:      "tmt:segment",
		Boundary:         &boundary,
	})
	if err != nil {
		return nil, fmt.Errorf("consolidator: save L1: %w", err)
	}

	// Insert into TMT.
	savedID := result.EntryID
	if savedID != "" {
		_ = c.tree.Insert(savedID, LevelSegment, interval)
	}

	return &ConsolidationResult{
		Level:        LevelSegment,
		NodesCreated: 1,
		Duration:     fmt.Sprintf("%.1fs", time.Since(start).Seconds()),
	}, nil
}

// ConsolidateLevel performs scheduled consolidation at the specified level
// for the given time window. It gathers child memories from (level-1) within
// the window, plus historical context, and generates a consolidated summary.
// ownerID is used for multi-tenant isolation (empty string for single-user mode).
func (c *Consolidator) ConsolidateLevel(ctx context.Context, level TemporalLevel, window TimeInterval, ownerID string) (*ConsolidationResult, error) {
	if level < LevelSession || level > LevelProfile {
		return nil, fmt.Errorf("consolidator: invalid level %d for scheduled consolidation", level)
	}
	c.mu.RLock()
	llm := c.llm
	c.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &ConsolidationResult{Level: level}, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	start := time.Now()

	// Find child entries pending consolidation.
	childIDs := c.tree.FindPendingConsolidation(level, window)
	if len(childIDs) == 0 {
		return &ConsolidationResult{Level: level}, nil
	}

	// Multi-tenant isolation: keep only child entries visible to this owner.
	if ownerID != "" {
		childIDs = c.filterChildrenByOwner(childIDs, ownerID)
		if len(childIDs) == 0 {
			return &ConsolidationResult{Level: level}, nil
		}
	}

	// Gather child entries and contents.
	childEntries := c.gatherEntries(childIDs)
	childContents := tmtEntryContents(childEntries)
	if len(childContents) == 0 {
		return &ConsolidationResult{Level: level}, nil
	}

	// Gather history context.
	history := c.getHistoryContext(level)

	// Build level-specific prompt.
	prompt := c.buildLevelPrompt(level, childContents, history)

	resp, err := llm.ChatCall(prompt)
	if err != nil {
		return nil, fmt.Errorf("consolidator: L%d: %w", level, err)
	}

	content := strings.TrimSpace(resp)
	if content == "" {
		return &ConsolidationResult{Level: level, ChildrenMerged: len(childIDs)}, nil
	}

	// Determine category based on level.
	cat := c.levelCategory(level)
	evidenceIDs := synthesisEvidenceIDs(childEntries)
	boundary := tmtBoundaryFromEntries(childEntries, ownerID, window, "tmt_consolidation")

	result, err := c.store.UpsertEntryByTags(UpsertByTagsOptions{
		Title:            fmt.Sprintf("TMT %s", level.String()),
		Content:          content,
		Category:         cat,
		Tags:             []string{"tmt", fmt.Sprintf("L%d", level), level.String(), ownerID, "window:" + window.Start.UTC().Format(time.RFC3339Nano)},
		IdentityTagCount: 5,
		Level:            level,
		Interval:         &window,
		OwnerID:          ownerID,
		SourceType:       "tmt_consolidation",
		EvidenceIDs:      evidenceIDs,
		RelatedIDs:       evidenceIDs,
		DerivedKind:      "tmt:" + strings.ToLower(level.String()),
		Boundary:         &boundary,
	})
	if err != nil {
		return nil, fmt.Errorf("consolidator: save L%d: %w", level, err)
	}

	// Insert into TMT and link children.
	savedID := result.EntryID
	if savedID != "" {
		if err := c.tree.Insert(savedID, level, window); err != nil {
			log.Printf("[consolidator] TMT insert L%d: %v", level, err)
		} else {
			for _, childID := range childIDs {
				if err := c.tree.SetParent(childID, savedID); err != nil {
					log.Printf("[consolidator] TMT set parent: %v", err)
				}
			}
			// Persist tree links back to entries.
			if err := c.persistTreeLinks(savedID, childIDs); err != nil {
				log.Printf("[consolidator] persist TMT links: %v", err)
			}
		}
	}

	return &ConsolidationResult{
		Level:          level,
		NodesCreated:   1,
		ChildrenMerged: len(childIDs),
		Duration:       fmt.Sprintf("%.1fs", time.Since(start).Seconds()),
	}, nil
}

// RunScheduledConsolidation checks all levels L2-L5 and consolidates any
// completed temporal windows. Called periodically by the pipeline.
// ownerID is used for multi-tenant isolation (empty string for single-user mode).
func (c *Consolidator) RunScheduledConsolidation(ctx context.Context, now time.Time, ownerID string) []ConsolidationResult {
	var results []ConsolidationResult

	type levelConfig struct {
		level    TemporalLevel
		truncate func(time.Time) time.Time
		duration time.Duration
	}

	configs := []levelConfig{
		{LevelSession, truncateToHour, 1 * time.Hour},
		{LevelDay, truncateToDay, 24 * time.Hour},
		{LevelWeek, truncateToWeek, 7 * 24 * time.Hour},
		{LevelProfile, truncateToMonth, 30 * 24 * time.Hour},
	}

	for _, cfg := range configs {
		select {
		case <-ctx.Done():
			return results
		default:
		}

		// The window is the PREVIOUS completed period.
		windowEnd := cfg.truncate(now)
		windowStart := windowEnd.Add(-cfg.duration)
		window := TimeInterval{Start: windowStart, End: windowEnd}

		// Only consolidate if there are pending children.
		pending := c.tree.FindPendingConsolidation(cfg.level, window)
		if len(pending) == 0 {
			continue
		}

		result, err := c.ConsolidateLevel(ctx, cfg.level, window, ownerID)
		if err != nil {
			log.Printf("[consolidator] L%d error: %v", cfg.level, err)
			continue
		}
		if result != nil && result.NodesCreated > 0 {
			results = append(results, *result)
		}
	}

	return results
}

// ---------------------------------------------------------------------------
// Level-specific prompt builders
// ---------------------------------------------------------------------------

func (c *Consolidator) buildSegmentPrompt(userMsg, assistantMsg, history string) []map[string]string {
	system := `You are a memory segment consolidator. Extract the key factual details from this dialog turn into a concise memory segment.

Rules:
- Capture WHO, WHAT, WHEN, WHERE if mentioned
- Preserve names, numbers, paths, commands, technical terms EXACTLY
- Use telegraphic style: omit filler words
- Output 1-3 concise sentences
- Return ONLY the segment text, no commentary`

	if history != "" {
		system += "\n\nRecent segment history for continuity:\n" + history
	}

	user := fmt.Sprintf("User: %s\nAssistant: %s", truncStr(userMsg, 1000), truncStr(assistantMsg, 1000))

	return []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": user},
	}
}

func (c *Consolidator) buildLevelPrompt(level TemporalLevel, childContents []string, history string) []map[string]string {
	var system string

	switch level {
	case LevelSession:
		system = `You are a session memory consolidator. Merge the following dialog segments from one session into a non-redundant event summary.

Rules:
- Combine related facts, remove duplicates
- Preserve all key decisions, outcomes, and action items
- Maintain chronological order within the summary
- Use concise paragraphs or bullet points
- Return ONLY the consolidated summary`

	case LevelDay:
		system = `You are a daily pattern consolidator. Analyze the following session summaries from one day and extract the user's routine context, recurring interests, and daily patterns.

Rules:
- Identify recurring themes across sessions
- Note time-of-day patterns if apparent
- Capture daily routine context (work focus, tools used, projects active)
- Distinguish one-time events from recurring patterns
- Return ONLY the daily pattern summary`

	case LevelWeek:
		system = `You are a weekly behavioral consolidator. Analyze the following daily patterns from one week and extract evolving behavioral features and preference patterns.

Rules:
- Identify cross-day behavioral trends
- Note preference changes or reinforcements
- Capture workflow patterns and tool usage trends
- Highlight any significant shifts from previous weeks
- Return ONLY the weekly behavioral summary`

	case LevelProfile:
		system = `You are a persona profile consolidator. Synthesize the following weekly behavioral summaries into a comprehensive, incrementally refined user profile.

Rules:
- Capture stable personality traits and values
- Document confirmed preferences (languages, tools, styles)
- Note professional role and expertise areas
- Include communication style preferences
- Distinguish stable traits from evolving preferences
- Return ONLY the updated profile text`
	}

	if history != "" {
		system += "\n\nRecent same-level history for continuity:\n" + history
	}

	var sb strings.Builder
	for i, content := range childContents {
		fmt.Fprintf(&sb, "[%d] %s\n\n", i, content)
	}

	return []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": sb.String()},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getHistoryContext retrieves the most recent memories at the same level
// for continuity during consolidation.
func (c *Consolidator) getHistoryContext(level TemporalLevel) string {
	recentIDs := c.tree.RecentAtLevel(level, historyWindowSize)
	if len(recentIDs) == 0 {
		return ""
	}

	idSet := make(map[string]bool, len(recentIDs))
	for _, id := range recentIDs {
		idSet[id] = true
	}

	// Single pass: collect content indexed by ID.
	contentByID := make(map[string]string, len(recentIDs))
	c.store.mu.RLock()
	for _, e := range c.store.entries {
		if idSet[e.ID] {
			contentByID[e.ID] = truncStr(e.Content, 300)
		}
	}
	c.store.mu.RUnlock()

	// Preserve order from recentIDs.
	var parts []string
	for _, id := range recentIDs {
		if content, ok := contentByID[id]; ok {
			parts = append(parts, content)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n---\n")
}

func (c *Consolidator) gatherEntries(ids []string) []Entry {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	entryByID := make(map[string]Entry, len(ids))
	for _, e := range c.store.entries {
		if idSet[e.ID] {
			entryByID[e.ID] = e
		}
	}

	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		if entry, ok := entryByID[id]; ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func tmtEntryContents(entries []Entry) []string {
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) != "" {
			contents = append(contents, entry.Content)
		}
	}
	return contents
}

func tmtBoundaryFromEntries(entries []Entry, ownerID string, interval TimeInterval, sourceScope string) MemoryBoundary {
	boundary := InferMemoryBoundary(entries)
	if boundary.OwnerID == "" {
		boundary.OwnerID = strings.TrimSpace(ownerID)
	}
	if boundary.SourceScope == "" {
		boundary.SourceScope = sourceScope
	}
	start := interval.Start
	end := interval.End
	if !start.IsZero() {
		boundary.Since = &start
	}
	if !end.IsZero() {
		boundary.Until = &end
	}
	return boundary
}

// filterChildrenByOwner filters child entry IDs to only include those
// belonging to the specified owner. Used for multi-tenant isolation.
func (c *Consolidator) filterChildrenByOwner(childIDs []string, ownerID string) []string {
	if ownerID == "" {
		return childIDs
	}

	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	// Build ID -> OwnerID map in single pass.
	ownerByID := make(map[string]string, len(childIDs))
	idSet := make(map[string]bool, len(childIDs))
	for _, id := range childIDs {
		idSet[id] = true
	}
	for _, e := range c.store.entries {
		if idSet[e.ID] {
			ownerByID[e.ID] = e.OwnerID
		}
	}

	// Filter: keep only entries with matching OwnerID or empty OwnerID (shared).
	var filtered []string
	for _, id := range childIDs {
		entryOwner := ownerByID[id]
		if entryOwner == "" || entryOwner == ownerID {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func (c *Consolidator) findEntryByContent(content string, level TemporalLevel) string {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	// Search from the end (most recently added).
	for i := len(c.store.entries) - 1; i >= 0; i-- {
		e := c.store.entries[i]
		if e.Level == level && e.Content == content {
			return e.ID
		}
	}
	return ""
}

func (c *Consolidator) levelCategory(level TemporalLevel) Category {
	switch level {
	case LevelSegment, LevelSession, LevelDay:
		return CategoryConversationSummary
	case LevelWeek:
		return CategoryPreference
	case LevelProfile:
		return CategoryProfile
	default:
		return CategoryConversationSummary
	}
}

// persistTreeLinks writes ParentID/ChildIDs back to the store entries so the
// TMT structure survives restarts. It routes through the store batch primitive
// so backend-backed stores persist the parent/child transition atomically.
func (c *Consolidator) persistTreeLinks(parentID string, childIDs []string) error {
	childSet := make(map[string]bool, len(childIDs))
	for _, id := range childIDs {
		childSet[id] = true
	}

	c.store.mu.RLock()
	updates := make([]Entry, 0, len(childIDs)+1)

	for i := range c.store.entries {
		entry := c.store.entries[i]
		id := entry.ID
		if id == parentID {
			entry.ChildIDs = append([]string(nil), childIDs...)
			updates = append(updates, entry)
		} else if childSet[id] {
			entry.ParentID = parentID
			updates = append(updates, entry)
		}
	}
	c.store.mu.RUnlock()

	if len(updates) == 0 {
		return nil
	}
	return c.store.UpdateEntriesByID(updates)
}

// ---------------------------------------------------------------------------
// Time truncation helpers
// ---------------------------------------------------------------------------

func truncateToHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func truncateToWeek(t time.Time) time.Time {
	// Truncate to Monday 00:00.
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	d := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}

func truncateToMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}
