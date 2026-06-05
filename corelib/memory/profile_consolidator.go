package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProfileConsolidator manages the TiMem L5 incremental persona profile.
// It maintains a single active CategoryProfile entry per user, updating
// it periodically by synthesizing weekly behavioral summaries (L4) with
// the existing profile.
type ProfileConsolidator struct {
	mu      sync.RWMutex
	store   *Store
	tree    *TemporalTree
	llm     LLMChatCaller
	lastRun map[string]time.Time
}

// NewProfileConsolidator creates a ProfileConsolidator.
func NewProfileConsolidator(store *Store, tree *TemporalTree, llm LLMChatCaller) *ProfileConsolidator {
	return &ProfileConsolidator{store: store, tree: tree, llm: llm, lastRun: make(map[string]time.Time)}
}

// SetLLM rewires the LLM used by profile consolidation.
func (pc *ProfileConsolidator) SetLLM(llm LLMChatCaller) {
	pc.mu.Lock()
	pc.llm = llm
	pc.mu.Unlock()
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
	return pc.ConsolidateForOwner(ctx, "")
}

// ConsolidateForOwner runs one owner-scoped L5 profile consolidation cycle.
// Empty ownerID is single-user/shared mode.
func (pc *ProfileConsolidator) ConsolidateForOwner(ctx context.Context, ownerID string) (*ConsolidationResult, error) {
	pc.mu.RLock()
	llm := pc.llm
	lastRun := pc.lastRun[ownerID]
	pc.mu.RUnlock()
	if llm == nil || !llm.IsConfigured() {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}
	if !lastRun.IsZero() && time.Since(lastRun) < 7*24*time.Hour {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	start := time.Now()

	// Gather weekly summaries (L4).
	weeklyEvidence := pc.gatherWeeklySummaryEntries(ownerID)
	weeklySummaries := entryContents(weeklyEvidence)

	// Gather recent reflections and promoted insights as additional signal.
	insightEvidence := pc.gatherRecentInsightEntries(ownerID)
	recentInsights := entryContents(insightEvidence)

	if len(weeklySummaries) == 0 && len(recentInsights) == 0 {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	profileEvidence := append(append([]Entry(nil), weeklyEvidence...), insightEvidence...)
	gate := AssessConsolidationGate(profileEvidence, ConsolidationGateOptions{MinEvidence: 2})
	if !gate.Allowed {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	// Get current profile (if exists).
	currentProfile := pc.getCurrentProfile(ownerID)

	// Build prompt.
	prompt := pc.buildProfilePrompt(currentProfile, weeklySummaries, recentInsights)

	resp, err := chatCallWithContext(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("profile_consolidator: %w", err)
	}

	newProfile := strings.TrimSpace(resp)
	if newProfile == "" {
		return &ConsolidationResult{Level: LevelProfile}, nil
	}

	// Update or create the profile entry.
	if err := pc.upsertProfile(newProfile, ownerID, profileEvidence); err != nil {
		return nil, fmt.Errorf("profile_consolidator: upsert: %w", err)
	}

	pc.mu.Lock()
	pc.lastRun[ownerID] = time.Now()
	pc.mu.Unlock()

	return &ConsolidationResult{
		Level:        LevelProfile,
		NodesCreated: 1,
		Duration:     fmt.Sprintf("%.1fs", time.Since(start).Seconds()),
	}, nil
}

// gatherWeeklySummaries collects L4 entries from the tree or store.
func (pc *ProfileConsolidator) gatherWeeklySummaries(ownerID string) []string {
	return entryContents(pc.gatherWeeklySummaryEntries(ownerID))
}

func (pc *ProfileConsolidator) gatherWeeklySummaryEntries(ownerID string) []Entry {
	// Try TMT first.
	if pc.tree != nil {
		weeklyIDs := pc.tree.RecentAtLevel(LevelWeek, 4) // last 4 weeks
		if len(weeklyIDs) > 0 {
			entries := pc.entriesByIDs(weeklyIDs, ownerID)
			if len(entries) > 0 {
				return entries
			}
		}
	}

	// Fallback: gather from store by tag.
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	var results []Entry
	for _, e := range pc.store.entries {
		if e.Level == LevelWeek && e.IsActive() && ownerMatches(e.OwnerID, ownerID) {
			results = append(results, e)
			if len(results) >= 4 {
				break
			}
		}
	}
	return results
}

// gatherRecentInsights collects recent reflection/promoted entries.
func (pc *ProfileConsolidator) gatherRecentInsights(ownerID string) []string {
	return entryContents(pc.gatherRecentInsightEntries(ownerID))
}

func (pc *ProfileConsolidator) gatherRecentInsightEntries(ownerID string) []Entry {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	var results []Entry
	cutoff := time.Now().Add(-30 * 24 * time.Hour) // last 30 days

	for _, e := range pc.store.entries {
		if !e.IsActive() || e.CreatedAt.Before(cutoff) || !ownerMatches(e.OwnerID, ownerID) {
			continue
		}
		for _, tag := range e.Tags {
			if tag == "reflection" || tag == "promoted" {
				results = append(results, e)
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
func (pc *ProfileConsolidator) getCurrentProfile(ownerID string) string {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	for _, e := range pc.store.entries {
		if e.Category == CategoryProfile && e.IsActive() && e.OwnerID == ownerID {
			return e.Content
		}
	}
	return ""
}

// upsertProfile updates the existing profile entry or creates a new one.
func (pc *ProfileConsolidator) upsertProfile(newContent string, ownerID string, evidence ...[]Entry) error {
	now := time.Now()
	evidenceEntries := flattenProfileEvidence(evidence...)
	evidenceIDs := synthesisEvidenceIDs(evidenceEntries)
	boundary := InferMemoryBoundary(evidenceEntries)

	pc.store.mu.RLock()
	for i := range pc.store.entries {
		if pc.store.entries[i].Category == CategoryProfile && pc.store.entries[i].IsActive() && pc.store.entries[i].OwnerID == ownerID {
			updated := pc.store.entries[i]
			pc.store.mu.RUnlock()

			updated.Content = newContent
			updated.Level = LevelProfile
			updated.AccessCount++
			updated.EvidenceIDs = evidenceIDs
			updated.RelatedIDs = mergeTags(updated.RelatedIDs, evidenceIDs)
			updated.DerivedKind = "profile"
			updated.Boundary = &boundary
			updated.SourceType = "profile_consolidation"
			if updated.Interval == nil {
				updated.Interval = &TimeInterval{Start: now, End: now}
			}
			return pc.store.UpdateEntriesByID([]Entry{updated})
		}
	}
	pc.store.mu.RUnlock()

	// No existing profile: create one.
	_, err := pc.store.UpsertEntryByTags(UpsertByTagsOptions{
		Title:            "User profile",
		Content:          newContent,
		Category:         CategoryProfile,
		Tags:             []string{"tmt", "L5", "profile", "auto_generated"},
		IdentityTagCount: 3,
		Scope:            ScopeGlobal,
		OwnerID:          ownerID,
		SourceType:       "profile_consolidation",
		Level:            LevelProfile,
		Interval:         &TimeInterval{Start: now, End: now},
		EvidenceIDs:      evidenceIDs,
		RelatedIDs:       evidenceIDs,
		DerivedKind:      "profile",
		Boundary:         &boundary,
	})
	return err
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

func (pc *ProfileConsolidator) contentsByIDs(ids []string, ownerID string) []string {
	return entryContents(pc.entriesByIDs(ids, ownerID))
}

func (pc *ProfileConsolidator) entriesByIDs(ids []string, ownerID string) []Entry {
	pc.store.mu.RLock()
	defer pc.store.mu.RUnlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	var entries []Entry
	for _, e := range pc.store.entries {
		if idSet[e.ID] && ownerMatches(e.OwnerID, ownerID) {
			entries = append(entries, e)
		}
	}
	return entries
}

func ownerMatches(entryOwner, filterOwner string) bool {
	if filterOwner == "" {
		return entryOwner == ""
	}
	return entryOwner == filterOwner || entryOwner == ""
}

func entryContents(entries []Entry) []string {
	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		content := strings.TrimSpace(entry.Content)
		if content != "" {
			contents = append(contents, truncStr(content, 200))
		}
	}
	return contents
}

func flattenProfileEvidence(groups ...[]Entry) []Entry {
	var out []Entry
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, entry := range group {
			key := entry.ID
			if key == "" {
				key = entry.Content
			}
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			out = append(out, entry)
		}
	}
	return out
}
