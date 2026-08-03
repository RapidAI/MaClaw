package memory

import (
	"context"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// ProactiveRecallOptions controls prompt-context recall shared by GUI, TUI,
// and server agents.
type ProactiveRecallOptions struct {
	ProjectPath string
	OwnerID     string
	// StrictOwner excludes legacy shared entries from an isolated prompt.
	StrictOwner        bool
	StrictProject      bool
	MaxEntries         int
	EntityLimit        int
	IncludeUserProfile bool
	EventContext       lifecycle.EventContext
	Provider           lifecycle.Provider
}

// RecallProactive builds the shared proactive recall set for system prompts.
// It asks the retrieval policy for a decision, searches through the experience
// provider, then lets the lifecycle balanced retriever select injected entries.
func (s *Store) RecallProactive(query string, opts ProactiveRecallOptions) []Entry {
	decision := lifecycle.DefaultRetrievalPolicy{}.Decide(context.Background(), lifecycle.RetrievalPolicyInput{
		TraceID:     opts.EventContext.TraceID,
		TaskID:      opts.EventContext.TaskID,
		CurrentGoal: query,
		TokenBudget: 0,
		Boundary:    proactiveRecallBoundary(opts),
		MissingSignals: []string{
			"max_entries:" + strconv.Itoa(defaultPositive(opts.MaxEntries, 12)),
		},
	})
	return s.RecallProactiveWithDecision(decision, opts)
}

func (s *Store) RecallProactiveWithDecision(decision lifecycle.RetrievalDecision, opts ProactiveRecallOptions) []Entry {
	return s.entriesForExperienceCandidatesWithOptions(s.RecallProactiveCandidatesWithDecision(decision, opts), opts)
}

func (s *Store) RecallProactiveCandidatesWithDecision(decision lifecycle.RetrievalDecision, opts ProactiveRecallOptions) []lifecycle.Candidate {
	if !decision.ShouldRetrieve {
		return nil
	}
	query := strings.TrimSpace(decision.Query)
	if query == "" {
		return nil
	}
	maxEntries := opts.MaxEntries
	if decision.Budget.MaxEntries > 0 {
		maxEntries = decision.Budget.MaxEntries
	}
	if maxEntries <= 0 {
		maxEntries = 12
	}
	poolLimit := maxEntries * 4
	if poolLimit < 12 {
		poolLimit = 12
	}
	provider := s.proactiveExperienceProvider(opts)
	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{Text: query, Types: decision.Types, Boundary: decision.Boundary, Limit: poolLimit})
	if err != nil || len(candidates) == 0 {
		return nil
	}
	candidates = s.filterProactiveCandidates(candidates, opts)
	decision.Budget.MaxEntries = maxEntries
	selected := lifecycle.SelectBalancedCandidates(candidates, decision)
	s.recordCandidateExperienceEvent(lifecycle.EventExperienceRetrieved, "provider:"+string(decision.Mode), query, selected, 0, opts.EventContext)
	return selected
}

func (s *Store) proactiveExperienceProvider(opts ProactiveRecallOptions) lifecycle.Provider {
	if opts.Provider != nil {
		return opts.Provider
	}
	return lifecycle.NewCompositeProvider(NewExperienceProvider(s))
}

func (s *Store) recallProactiveCore(query string, opts ProactiveRecallOptions, decision lifecycle.RetrievalDecision) []Entry {
	if s == nil || query == "" {
		return nil
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 12
	}
	entityLimit := opts.EntityLimit
	if entityLimit <= 0 {
		entityLimit = 1
	}

	recalled := filterEntriesForOwner(s.RecallLightMem(query, "", opts.ProjectPath, opts.OwnerID), opts.OwnerID, opts.StrictOwner)
	if len(recalled) < maxEntries {
		recalled = s.supplementProactiveRecallByEntity(query, recalled, opts, maxEntries, entityLimit)
	}

	filtered := make([]Entry, 0, len(recalled))
	for _, entry := range recalled {
		if shouldSkipProactiveRecallEntry(entry, opts) || !proactiveOwnerAllowed(entry, opts) {
			continue
		}
		filtered = append(filtered, entry)
		if len(filtered) >= maxEntries {
			break
		}
	}
	return selectBalancedProactiveEntries(filtered, decision)
}

func proactiveRecallBoundary(opts ProactiveRecallOptions) lifecycle.Boundary {
	return lifecycle.Boundary{OwnerID: opts.OwnerID, ProjectPath: opts.ProjectPath}
}

func (s *Store) filterProactiveCandidates(candidates []lifecycle.Candidate, opts ProactiveRecallOptions) []lifecycle.Candidate {
	if len(candidates) == 0 || s == nil {
		return candidates
	}
	entries := s.entriesByID(candidates)
	filtered := candidates[:0]
	for _, candidate := range candidates {
		entry, ok := entries[candidate.Entry.ID]
		if ok && (shouldSkipProactiveRecallEntry(entry, opts) || !proactiveOwnerAllowed(entry, opts)) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func (s *Store) entriesForExperienceCandidates(candidates []lifecycle.Candidate) []Entry {
	if len(candidates) == 0 || s == nil {
		return nil
	}
	entries := s.entriesByID(candidates)
	out := make([]Entry, 0, len(candidates))
	for _, candidate := range candidates {
		if entry, ok := entries[candidate.Entry.ID]; ok {
			out = append(out, entry)
		}
	}
	return out
}

func (s *Store) entriesForExperienceCandidatesWithOptions(candidates []lifecycle.Candidate, opts ProactiveRecallOptions) []Entry {
	return filterEntriesForOwner(s.entriesForExperienceCandidates(candidates), opts.OwnerID, opts.StrictOwner)
}

func (s *Store) filterExperienceCandidatesForOptions(candidates []lifecycle.Candidate, opts ProactiveRecallOptions) []lifecycle.Candidate {
	if !opts.StrictOwner || len(candidates) == 0 {
		return candidates
	}
	entries := s.entriesByID(candidates)
	filtered := candidates[:0]
	for _, candidate := range candidates {
		entry, ok := entries[candidate.Entry.ID]
		if !ok || !proactiveOwnerAllowed(entry, opts) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func (s *Store) entriesByID(candidates []lifecycle.Candidate) map[string]Entry {
	ids := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Entry.ID != "" {
			ids[candidate.Entry.ID] = struct{}{}
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make(map[string]Entry, len(ids))
	for _, entry := range s.entries {
		if _, ok := ids[entry.ID]; ok {
			entries[entry.ID] = entry
		}
	}
	return entries
}

func defaultPositive(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *Store) supplementProactiveRecallByEntity(query string, recalled []Entry, opts ProactiveRecallOptions, maxEntries int, entityLimit int) []Entry {
	expanded := ExpandQuery(query)
	if len(expanded.Entities) == 0 {
		return recalled
	}
	seen := make(map[string]bool, len(recalled))
	for _, entry := range recalled {
		seen[entry.ID] = true
	}
	entities := expanded.Entities
	if len(entities) > entityLimit {
		entities = entities[:entityLimit]
	}
	for _, entity := range entities {
		var extra []Entry
		if opts.StrictProject && opts.ProjectPath != "" {
			extra = s.recallDynamicStrictWithEventContext(entity, "", opts.ProjectPath, opts.EventContext, opts.OwnerID)
		} else {
			extra = s.recallDynamicWithEventContext(entity, "", opts.ProjectPath, opts.EventContext, opts.OwnerID)
		}
		for _, entry := range extra {
			if !proactiveOwnerAllowed(entry, opts) {
				continue
			}
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			recalled = append(recalled, entry)
			if len(recalled) >= maxEntries {
				return recalled
			}
		}
	}
	return recalled
}

func shouldSkipProactiveRecallEntry(entry Entry, opts ProactiveRecallOptions) bool {
	canonical := MapToCanonical(entry.Category)
	if !opts.IncludeUserProfile && (canonical == CategoryUserFact || canonical == CategorySelfIdentity) {
		return true
	}
	return canonical == CategorySessionCheckpoint || canonical == CategoryConversationSummary
}

func proactiveOwnerAllowed(entry Entry, opts ProactiveRecallOptions) bool {
	return len(filterEntriesForOwner([]Entry{entry}, opts.OwnerID, opts.StrictOwner)) == 1
}
