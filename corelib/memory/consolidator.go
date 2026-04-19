package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// historyWindowSize is the number of recent same-level memories used as
// historical context during consolidation (TiMem wᵢ=3).
const historyWindowSize = 3

// Consolidator performs TiMem-style stratified memory consolidation.
// It transforms child memories into higher-level abstractions using
// level-specific instruction prompts, without fine-tuning.
//
// Two-tier scheduling:
//   - Online (L1): invoked after each dialog turn via ConsolidateSegment
//   - Scheduled (L2-L5): invoked when temporal windows close via ConsolidateLevel
type Consolidator struct {
	store *Store
	tree  *TemporalTree
	llm   LLMChatCaller
}

// NewConsolidator creates a Consolidator.
func NewConsolidator(store *Store, tree *TemporalTree, llm LLMChatCaller) *Consolidator {
	return &Consolidator{store: store, tree: tree, llm: llm}
}

// ConsolidateSegment performs online L1 consolidation for a single dialog turn.
// It creates a segment-level memory from the raw user-assistant exchange.
func (c *Consolidator) ConsolidateSegment(ctx context.Context, userMsg, assistantMsg string, turnTime time.Time) (*ConsolidationResult, error) {
	if c.llm == nil || !c.llm.IsConfigured() {
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

	resp, err := c.llm.ChatCall(prompt)
	if err != nil {
		return nil, fmt.Errorf("consolidator: L1 segment: %w", err)
	}

	content := strings.TrimSpace(resp)
	if content == "" {
		return &ConsolidationResult{Level: LevelSegment}, nil
	}

	// Create the segment entry.
	interval := TimeInterval{Start: turnTime, End: turnTime}
	entry := Entry{
		Content:  content,
		Category: CategoryConversationSummary,
		Tags:     []string{"tmt", "L1", "segment"},
		Level:    LevelSegment,
		Interval: &interval,
	}

	if err := c.store.Save(entry); err != nil {
		return nil, fmt.Errorf("consolidator: save L1: %w", err)
	}

	// Insert into TMT.
	// Find the saved entry ID (Save may have deduplicated).
	savedID := c.findEntryByContent(content, LevelSegment)
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
func (c *Consolidator) ConsolidateLevel(ctx context.Context, level TemporalLevel, window TimeInterval) (*ConsolidationResult, error) {
	if level < LevelSession || level > LevelProfile {
		return nil, fmt.Errorf("consolidator: invalid level %d for scheduled consolidation", level)
	}
	if c.llm == nil || !c.llm.IsConfigured() {
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

	// Gather child contents.
	childContents := c.gatherContents(childIDs)
	if len(childContents) == 0 {
		return &ConsolidationResult{Level: level}, nil
	}

	// Gather history context.
	history := c.getHistoryContext(level)

	// Build level-specific prompt.
	prompt := c.buildLevelPrompt(level, childContents, history)

	resp, err := c.llm.ChatCall(prompt)
	if err != nil {
		return nil, fmt.Errorf("consolidator: L%d: %w", level, err)
	}

	content := strings.TrimSpace(resp)
	if content == "" {
		return &ConsolidationResult{Level: level, ChildrenMerged: len(childIDs)}, nil
	}

	// Determine category based on level.
	cat := c.levelCategory(level)

	entry := Entry{
		Content:  content,
		Category: cat,
		Tags:     []string{"tmt", fmt.Sprintf("L%d", level), level.String()},
		Level:    level,
		Interval: &window,
	}

	if err := c.store.Save(entry); err != nil {
		return nil, fmt.Errorf("consolidator: save L%d: %w", level, err)
	}

	// Insert into TMT and link children.
	savedID := c.findEntryByContent(content, level)
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
			c.persistTreeLinks(savedID, childIDs)
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
func (c *Consolidator) RunScheduledConsolidation(ctx context.Context, now time.Time) []ConsolidationResult {
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

		result, err := c.ConsolidateLevel(ctx, cfg.level, window)
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

// getHistoryContext retrieves the wᵢ most recent memories at the same level
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

func (c *Consolidator) gatherContents(ids []string) []string {
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	// Single pass: collect content indexed by ID.
	contentByID := make(map[string]string, len(ids))
	for _, e := range c.store.entries {
		if idSet[e.ID] {
			contentByID[e.ID] = e.Content
		}
	}

	// Preserve order from input ids.
	var contents []string
	for _, id := range ids {
		if content, ok := contentByID[id]; ok {
			contents = append(contents, content)
		}
	}
	return contents
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

// persistTreeLinks writes ParentID/ChildIDs back to the store entries
// so the TMT structure survives restarts. Single O(n) pass.
func (c *Consolidator) persistTreeLinks(parentID string, childIDs []string) {
	childSet := make(map[string]bool, len(childIDs))
	for _, id := range childIDs {
		childSet[id] = true
	}

	c.store.mu.Lock()
	defer c.store.mu.Unlock()

	for i := range c.store.entries {
		id := c.store.entries[i].ID
		if id == parentID {
			c.store.entries[i].ChildIDs = childIDs
			c.store.dirty = true
		} else if childSet[id] {
			c.store.entries[i].ParentID = parentID
			c.store.dirty = true
		}
	}
	c.store.signalSave()
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
