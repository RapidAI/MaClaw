package memory

import (
	"strings"
	"time"
)

// ConsolidationGateOptions controls when episodic evidence may become a schema.
// The gate is deliberately cheap and deterministic; LLM-backed maintenance can
// use it before creating derived memories, and tests can compare it against
// episodic-only baselines without invoking a model.
type ConsolidationGateOptions struct {
	MinEvidence        int
	MinDistinctSources int
	RequireBoundary    bool
}

// ConsolidationGateDecision explains whether a schema consolidation should run.
type ConsolidationGateDecision struct {
	Allowed         bool           `json:"allowed"`
	EvidenceCount   int            `json:"evidence_count"`
	DistinctSources int            `json:"distinct_sources"`
	Boundary        MemoryBoundary `json:"boundary,omitempty"`
	Reasons         []string       `json:"reasons,omitempty"`
}

// AssessConsolidationGate checks the non-LLM preconditions for creating a
// derived schema from raw evidence. It prevents one-off, mixed-boundary, or
// narrow-stream observations from being promoted as global rules.
func AssessConsolidationGate(evidence []Entry, opts ConsolidationGateOptions) ConsolidationGateDecision {
	if opts.MinEvidence <= 0 {
		opts.MinEvidence = 3
	}
	if opts.MinDistinctSources <= 0 {
		opts.MinDistinctSources = 1
	}
	decision := ConsolidationGateDecision{EvidenceCount: len(evidence)}
	decision.Boundary = InferMemoryBoundary(evidence)
	sources := map[string]struct{}{}
	owners := map[string]struct{}{}
	projects := map[string]struct{}{}
	for _, entry := range evidence {
		source := boundarySourceHint(entry)
		if source == "" {
			source = "unknown"
		}
		sources[source] = struct{}{}
		if owner := boundaryOwnerHint(entry); owner != "" {
			owners[owner] = struct{}{}
		}
		project := boundaryProjectHint(entry)
		if project != "" {
			projects[project] = struct{}{}
		}
	}
	decision.DistinctSources = len(sources)
	if len(evidence) < opts.MinEvidence {
		decision.Reasons = append(decision.Reasons, "insufficient_evidence")
	}
	if decision.DistinctSources < opts.MinDistinctSources {
		decision.Reasons = append(decision.Reasons, "insufficient_source_diversity")
	}
	if len(owners) > 1 {
		decision.Reasons = append(decision.Reasons, "mixed_owner_boundary")
	}
	if len(projects) > 1 {
		decision.Reasons = append(decision.Reasons, "mixed_project_boundary")
	}
	if opts.RequireBoundary && decision.Boundary.ProjectPath == "" && decision.Boundary.OwnerID == "" && decision.Boundary.SourceScope == "" {
		decision.Reasons = append(decision.Reasons, "missing_boundary")
	}
	decision.Allowed = len(decision.Reasons) == 0
	return decision
}

// InferMemoryBoundary derives a conservative boundary from evidence entries.
func InferMemoryBoundary(evidence []Entry) MemoryBoundary {
	var b MemoryBoundary
	var since, until *time.Time
	ownerSet := map[string]struct{}{}
	projectSet := map[string]struct{}{}
	sourceSet := map[string]struct{}{}
	for _, entry := range evidence {
		if owner := boundaryOwnerHint(entry); owner != "" {
			ownerSet[owner] = struct{}{}
		}
		if project := boundaryProjectHint(entry); project != "" {
			projectSet[project] = struct{}{}
		}
		if source := boundarySourceHint(entry); source != "" {
			sourceSet[source] = struct{}{}
		}
		entrySince, entryUntil := boundaryTimeHints(entry)
		if entrySince != nil {
			if since == nil || entrySince.Before(*since) {
				t := *entrySince
				since = &t
			}
		}
		if entryUntil != nil {
			if until == nil || entryUntil.After(*until) {
				t := *entryUntil
				until = &t
			}
		}
	}
	if len(ownerSet) == 1 {
		for owner := range ownerSet {
			b.OwnerID = owner
		}
	}
	if len(projectSet) == 1 {
		for project := range projectSet {
			b.ProjectPath = project
		}
	}
	if len(sourceSet) == 1 {
		for source := range sourceSet {
			b.SourceScope = source
		}
	}
	b.Since = since
	b.Until = until
	return b
}

func boundaryOwnerHint(entry Entry) string {
	if owner := strings.TrimSpace(entry.OwnerID); owner != "" {
		return owner
	}
	if entry.Boundary != nil {
		return strings.TrimSpace(entry.Boundary.OwnerID)
	}
	return ""
}

func boundaryProjectHint(entry Entry) string {
	if entry.Boundary != nil && strings.TrimSpace(entry.Boundary.ProjectPath) != "" {
		return strings.TrimSpace(entry.Boundary.ProjectPath)
	}
	if entry.Scope != ScopeProject {
		return ""
	}
	for _, tag := range entry.Tags {
		trimmed := strings.TrimSpace(tag)
		lower := strings.ToLower(trimmed)
		if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") || strings.HasPrefix(lower, "project:") {
			return trimmed
		}
	}
	return ""
}

func boundarySourceHint(entry Entry) string {
	if entry.Boundary != nil && strings.TrimSpace(entry.Boundary.SourceScope) != "" {
		return strings.TrimSpace(entry.Boundary.SourceScope)
	}
	return strings.TrimSpace(entry.SourceType)
}

func boundaryTimeHints(entry Entry) (*time.Time, *time.Time) {
	if entry.Boundary != nil && (entry.Boundary.Since != nil || entry.Boundary.Until != nil) {
		return entry.Boundary.Since, entry.Boundary.Until
	}
	when := entry.ValidAt
	if when == nil && !entry.UpdatedAt.IsZero() {
		t := entry.UpdatedAt
		when = &t
	}
	return when, when
}
