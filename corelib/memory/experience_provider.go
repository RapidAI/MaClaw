package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type ExperienceProvider struct {
	Store *Store
}

func NewExperienceProvider(store *Store) ExperienceProvider {
	return ExperienceProvider{Store: store}
}

func (p ExperienceProvider) ListExperience(_ context.Context, scope lifecycle.Scope) ([]lifecycle.Entry, error) {
	if p.Store == nil {
		return nil, nil
	}
	project := semanticNormalizeProjectPath(scope.Boundary.ProjectPath)
	p.Store.mu.RLock()
	defer p.Store.mu.RUnlock()
	out := make([]lifecycle.Entry, 0, len(p.Store.entries))
	for _, entry := range p.Store.entries {
		if !entry.IsActive() || !proactiveRecallBoundaryAllowed(entry, scope.Boundary) {
			continue
		}
		if project != "" && !recallBoundaryAllowed(entry, project, scope.Boundary.OwnerID) {
			continue
		}
		lifecycleEntry := memoryEntryToLifecycleEntry(entry)
		if !lifecycleEntryTypeAllowed(lifecycleEntry.EntryType, scope.Types) {
			continue
		}
		out = append(out, lifecycleEntry)
		if scope.Limit > 0 && len(out) >= scope.Limit {
			break
		}
	}
	return out, nil
}

func (p ExperienceProvider) SearchExperience(_ context.Context, query lifecycle.Query) ([]lifecycle.Candidate, error) {
	if p.Store == nil || strings.TrimSpace(query.Text) == "" {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 12
	}
	ownerIDs := ownerIDArg(query.Boundary.OwnerID)
	entries := p.Store.recallDynamicCore(query.Text, "", query.Boundary.ProjectPath, query.Boundary.ProjectPath != "", ownerIDs...)
	if lifecycleEntryTypeAllowed(lifecycle.EntryTypeFactual, query.Types) {
		entries = appendUniqueExperienceProviderEntries(entries, p.Store.recallDynamicCore(query.Text, CategoryUserFact, query.Boundary.ProjectPath, query.Boundary.ProjectPath != "", ownerIDs...)...)
		entries = appendUniqueExperienceProviderEntries(entries, p.Store.recallDynamicCore(query.Text, CategorySelfIdentity, query.Boundary.ProjectPath, query.Boundary.ProjectPath != "", ownerIDs...)...)
	}
	decision := lifecycle.RetrievalDecision{Query: query.Text, Types: query.Types, Boundary: query.Boundary, Budget: lifecycle.RetrievalBudget{MaxEntries: limit}}
	candidates := make([]lifecycle.Candidate, 0, len(entries))
	for i, entry := range entries {
		candidate, ok := buildProactiveRecallCandidate(entry, i, len(entries), decision)
		if !ok || !lifecycleEntryTypeAllowed(candidate.candidate.Entry.EntryType, query.Types) {
			continue
		}
		candidate.candidate.Reason = "memory_provider"
		candidates = append(candidates, candidate.candidate)
		if len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

func appendUniqueExperienceProviderEntries(entries []Entry, extra ...Entry) []Entry {
	seen := make(map[string]struct{}, len(entries)+len(extra))
	for _, entry := range entries {
		if entry.ID != "" {
			seen[entry.ID] = struct{}{}
		}
	}
	for _, entry := range extra {
		if entry.ID != "" {
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
		}
		entries = append(entries, entry)
	}
	return entries
}

func (p ExperienceProvider) UpdateUtility(_ context.Context, update lifecycle.UtilityUpdate) error {
	if p.Store == nil || strings.TrimSpace(update.EntryID) == "" {
		return nil
	}
	p.Store.mu.RLock()
	var updated Entry
	found := false
	for i := range p.Store.entries {
		if p.Store.entries[i].ID != update.EntryID {
			continue
		}
		updated = p.Store.entries[i]
		found = true
		break
	}
	p.Store.mu.RUnlock()
	if !found {
		return fmt.Errorf("memory experience entry %q not found", update.EntryID)
	}
	if update.Helpful || update.Success {
		updated.AccessCount++
		updated.Strength += 1
	}
	if update.Harmful {
		updated.Strength -= 1
		if updated.Strength < 0 {
			updated.Strength = 0
		}
	}
	updated.UpdatedAt = time.Now()
	return p.Store.updateMetadataEntriesByID([]Entry{updated})
}

func memoryEntryToLifecycleEntry(entry Entry) lifecycle.Entry {
	priority := proactiveRecallPriorityScore(entry)
	return lifecycle.Entry{
		ID:          entry.ID,
		EntryType:   classifyExperienceEntryType(entry),
		WhenToUse:   firstNonEmptyString(entry.Title, entry.DerivedKind),
		Content:     firstNonEmptyString(entry.CompactForm, entry.Content),
		SourceType:  entry.SourceType,
		SourceURL:   entry.SourceURL,
		EvidenceIDs: append([]string(nil), entry.EvidenceIDs...),
		Boundary:    memoryBoundaryToLifecycle(entry.Boundary, entry.OwnerID, entry.Scope, entry.Tags),
		Priority:    priority,
	}
}

func lifecycleEntryTypeAllowed(entryType lifecycle.EntryType, allowed []lifecycle.EntryType) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidateType := range allowed {
		if entryType == candidateType {
			return true
		}
	}
	return false
}

func ownerIDArg(ownerID string) []string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil
	}
	return []string{ownerID}
}
