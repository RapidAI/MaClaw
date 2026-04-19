package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ProfileConsolidator manages the TiMem L5 incremental persona profile.
// It maintains a single active CategoryProfile entry per user, updating
// it periodically by synthesizing weekly behavioral summaries (L4) with
// the existing profile.
type ProfileConsolidator struct {
	store   *Store
	tree    *TemporalTree
	llm     LLMChatCaller
	lastRun time.Time
}

// NewProfileConsolidator creates a ProfileConsolidator.
func NewProfileConsolidator(store *Store, tree *TemporalTree, llm LLMChatCaller) *ProfileConsolidator {
	return &ProfileConsolidator{store: store, tree: tree, llm: llm}
}

// Consolidate runs one L5 profile consolidation cycle.
// It gathers L4 (weekly) summaries and the current profile, then asks the
// LLM to produce an updated, comprehensive persona representation.
//
// The existing profile entry is updated in-place (with version snapshot),
// rather than creating a new entry, ensuring there is always at most one
// active profile.
//
// Skips if:
//   - LLM is not configured
//   - No weekly summaries exist
//   - Less than 7 days since last consolidation
func (pc *ProfileConsolidator) Consolidate(ctx context.Context) (*ConsolidationResult, error) {
	if pc.llm == nil || !pc.llm.IsConfigured() {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}
	if !pc.lastRun.IsZero() && time.Since(pc.lastRun) < 7*24*time.Hour {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	start := time.Now()

	// Gather weekly summaries (L4).
	weeklySummaries := pc.gatherWeeklySummaries()

	// Gather recent reflections and promoted insights as additional signal.
	recentInsights := pc.gatherRecentInsights()

	if len(weeklySummaries) == 0 && len(recentInsights) == 0 {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	// Get current profile (if exists).
	currentProfile := pc.getCurrentProfile()

	// Build prompt.
	prompt := pc.buildProfilePrompt(currentProfile, weeklySummaries, recentInsights)

	resp, err := pc.llm.ChatCall(prompt)
	if err != nil {
		return nil, fmt.Errorf("profile_consolidator: %w", err)
	}

	newProfile := strings.TrimSpace(resp)
	if newProfile == "" {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	// Update or create the profile entry.
	if err := pc.upsertProfile(newProfile); err != nil {
		return nil, fmt.Errorf("profile_consolidator: upsert: %w", err)
	}

	pc.lastRun = time.Now()

	return &ConsolidationResult{
		Level:        LevelProfile,
		NodesCreated: 1,
		Duration:     fmt.Sprintf("%.1fs", time.Since(start).Seconds()),
	}, nil
}

// gatherWeeklySummaries collects L4 entries from the tree or store.
func (pc *ProfileConsolidator) gatherWeeklySummaries() []string {
	// Try TMT first.
	if pc.tree != nil {
		weeklyIDs := pc.tree.RecentAtLevel(LevelWeek, 4) // last 4 weeks
		if len(weeklyIDs) > 0 {
			return pc.contentsByIDs(weeklyIDs)
		}
	}

	// Fallback: gather from store by tag.
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	var results []string
	for _, e := range pc.store.entries {
		if e.Level == LevelWeek && e.IsActive() {
			results = append(results, e.Content)
			if len(results) >= 4 {
				break
			}
		}
	}
	return results
}

// gatherRecentInsights collects recent reflection/promoted entries.
func (pc *ProfileConsolidator) gatherRecentInsights() []string {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	var results []string
	cutoff := time.Now().Add(-30 * 24 * time.Hour) // last 30 days

	for _, e := range pc.store.entries {
		if !e.IsActive() || e.CreatedAt.Before(cutoff) {
			continue
		}
		for _, tag := range e.Tags {
			if tag == "reflection" || tag == "promoted" {
				results = append(results, truncStr(e.Content, 200))
				break
			}
		}
		if len(results) >= 10 {
			break
		}
	}
	return results
}

// getCurrentProfile finds the active CategoryProfile entry.
func (pc *ProfileConsolidator) getCurrentProfile() string {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	for _, e := range pc.store.entries {
		if e.Category == CategoryProfile && e.IsActive() {
			return e.Content
		}
	}
	return ""
}

// upsertProfile updates the existing profile entry or creates a new one.
func (pc *ProfileConsolidator) upsertProfile(newContent string) error {
	now := time.Now()

	pc.store.mu.Lock()
	for i := range pc.store.entries {
		if pc.store.entries[i].Category == CategoryProfile && pc.store.entries[i].IsActive() {
			// Update existing: snapshot old version.
			old := pc.store.entries[i].Content
			if len(pc.store.entries[i].Versions) >= 3 {
				pc.store.entries[i].Versions = pc.store.entries[i].Versions[1:]
			}
			pc.store.entries[i].Versions = append(pc.store.entries[i].Versions, VersionSnapshot{
				Content:   old,
				Timestamp: pc.store.entries[i].UpdatedAt,
			})
			pc.store.entries[i].Content = newContent
			pc.store.entries[i].UpdatedAt = now
			pc.store.entries[i].Level = LevelProfile
			pc.store.entries[i].ContentHash = computeContentHash(newContent)
			pc.store.entries[i].AccessCount++
			pc.store.bm25.updateEntry(pc.store.entries[i])
			pc.store.dirty = true
			pc.store.mu.Unlock()
			pc.store.signalSave()
			return nil
		}
	}
	pc.store.mu.Unlock()

	// No existing profile — create one.
	entry := Entry{
		Content:  newContent,
		Category: CategoryProfile,
		Tags:     []string{"tmt", "L5", "profile", "auto_generated"},
		Level:    LevelProfile,
		Scope:    ScopeGlobal,
		Interval: &TimeInterval{Start: now, End: now},
	}
	return pc.store.Save(entry)
}

func (pc *ProfileConsolidator) buildProfilePrompt(currentProfile string, weeklySummaries, insights []string) []map[string]string {
	system := `You are a persona profile synthesizer. Your task is to create or update a comprehensive user profile by integrating new behavioral evidence with the existing profile.

The profile should capture:
1. **Identity**: Professional role, expertise areas, primary goals
2. **Technical preferences**: Languages, frameworks, tools, coding style
3. **Work patterns**: Typical workflows, problem-solving approaches, productivity habits
4. **Communication style**: Preferred language, verbosity level, formality
5. **Values**: What the user prioritizes (correctness, speed, elegance, pragmatism)

Rules:
- Preserve all stable traits from the existing profile
- Update or add traits when new evidence supports changes
- Mark uncertain traits with "possibly" or "tends to"
- Use concise, structured format (bullet points or short paragraphs)
- Maximum 500 words
- Return ONLY the profile text, no commentary`

	var userParts []string

	if currentProfile != "" {
		userParts = append(userParts, "=== Current Profile ===\n"+currentProfile)
	}

	if len(weeklySummaries) > 0 {
		userParts = append(userParts, "=== Recent Weekly Behavioral Summaries ===\n"+strings.Join(weeklySummaries, "\n---\n"))
	}

	if len(insights) > 0 {
		userParts = append(userParts, "=== Recent Insights ===\n"+strings.Join(insights, "\n"))
	}

	return []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": strings.Join(userParts, "\n\n")},
	}
}

func (pc *ProfileConsolidator) contentsByIDs(ids []string) []string {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	var contents []string
	for _, e := range pc.store.entries {
		if idSet[e.ID] {
			contents = append(contents, e.Content)
		}
	}
	return contents
}
