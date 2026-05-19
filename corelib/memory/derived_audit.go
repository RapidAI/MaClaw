package memory

import (
	"fmt"
	"strings"
	"time"
)

// DerivedMemoryAudit is a compact inspection record for generated memory views.
// It is intentionally read-only: surgery actions should inspect this first,
// then explicitly supersede or delete the derived entry while leaving raw
// evidence untouched.
type DerivedMemoryAudit struct {
	EntryID            string          `json:"entry_id"`
	Category           Category        `json:"category,omitempty"`
	Status             Status          `json:"status,omitempty"`
	DerivedKind        string          `json:"derived_kind,omitempty"`
	SourceType         string          `json:"source_type,omitempty"`
	EvidenceIDs        []string        `json:"evidence_ids,omitempty"`
	MissingEvidenceIDs []string        `json:"missing_evidence_ids,omitempty"`
	Boundary           *MemoryBoundary `json:"boundary,omitempty"`
	ContentPreview     string          `json:"content_preview,omitempty"`
	Issues             []string        `json:"issues,omitempty"`
}

// DerivedMemoryAudits lists derived memories visible in the requested owner /
// project boundary and reports audit issues such as missing evidence or missing
// boundary metadata.
func (s *Store) DerivedMemoryAudits(projectPath string, ownerID string, limit int) []DerivedMemoryAudit {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make(map[string]struct{}, len(s.entries))
	for _, entry := range s.entries {
		if entry.ID != "" {
			ids[entry.ID] = struct{}{}
		}
	}

	out := make([]DerivedMemoryAudit, 0)
	for _, entry := range s.entries {
		if !isDerivedMemoryEntry(entry) {
			continue
		}
		if !derivedAuditBoundaryAllowed(entry, projectLower, ownerID) {
			continue
		}
		audit := buildDerivedMemoryAudit(entry, ids)
		out = append(out, audit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// SupersedeDerivedMemory invalidates a generated memory view without touching
// its raw evidence. It is the first conservative memory-surgery primitive:
// callers must provide the current owner/project context, and non-derived
// entries are rejected so raw episodic evidence is preserved by default.
func (s *Store) SupersedeDerivedMemory(id string, projectPath string, ownerID string) error {
	if s == nil {
		return fmt.Errorf("memory_store: not initialized")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory_store: missing derived memory id")
	}
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.Lock()
	changed := false
	found := false
	var err error
	for _, entry := range s.entries {
		if entry.ID != id {
			continue
		}
		found = true
		if !isDerivedMemoryEntry(entry) {
			err = fmt.Errorf("memory_store: entry %q is not a derived memory", id)
			break
		}
		if err = derivedSurgeryBoundaryError(entry, projectLower, ownerID); err != nil {
			err = fmt.Errorf("memory_store: entry %q %w", id, err)
			break
		}
		changed = s.supersedeEntryLocked(id, time.Now())
		break
	}
	if !found {
		err = fmt.Errorf("memory_store: entry %q not found", id)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if changed {
		s.signalSave()
	}
	return nil
}

func derivedAuditBoundaryAllowed(entry Entry, projectLower, ownerID string) bool {
	if entry.OwnerID != "" && ownerID != entry.OwnerID {
		return false
	}
	boundary := entry.Boundary
	if boundary == nil {
		return true
	}
	if boundary.OwnerID != "" && ownerID != boundary.OwnerID {
		return false
	}
	if boundary.ProjectPath != "" {
		if projectLower == "" {
			return false
		}
		boundaryProject := semanticNormalizeProjectPath(boundary.ProjectPath)
		if boundaryProject != "" && !semanticProjectPathMatches(boundaryProject, projectLower) {
			return false
		}
	}
	return true
}
func derivedSurgeryBoundaryError(entry Entry, projectLower, ownerID string) error {
	if entry.OwnerID != "" {
		if ownerID == "" {
			return fmt.Errorf("requires owner boundary %q", entry.OwnerID)
		}
		if entry.OwnerID != ownerID {
			return fmt.Errorf("is outside owner boundary")
		}
	}
	boundary := entry.Boundary
	if boundary == nil {
		return nil
	}
	if boundary.OwnerID != "" {
		if ownerID == "" {
			return fmt.Errorf("requires owner boundary %q", boundary.OwnerID)
		}
		if boundary.OwnerID != ownerID {
			return fmt.Errorf("is outside owner boundary")
		}
	}
	if boundary.ProjectPath != "" {
		if projectLower == "" {
			return fmt.Errorf("requires project boundary %q", boundary.ProjectPath)
		}
		boundaryProject := semanticNormalizeProjectPath(boundary.ProjectPath)
		if boundaryProject != "" && !semanticProjectPathMatches(boundaryProject, projectLower) {
			return fmt.Errorf("is outside derived boundary")
		}
	}
	return nil
}

func isDerivedMemoryEntry(entry Entry) bool {
	if entry.DerivedKind != "" || len(entry.EvidenceIDs) > 0 || entry.Boundary != nil {
		return true
	}
	sourceType := strings.ToLower(strings.TrimSpace(entry.SourceType))
	return strings.Contains(sourceType, "consolidation") || strings.Contains(sourceType, "summary") || strings.HasPrefix(sourceType, "schema")
}

func buildDerivedMemoryAudit(entry Entry, existingIDs map[string]struct{}) DerivedMemoryAudit {
	missing := make([]string, 0)
	for _, id := range entry.EvidenceIDs {
		if _, ok := existingIDs[id]; !ok {
			missing = append(missing, id)
		}
	}
	issues := make([]string, 0)
	if entry.DerivedKind == "" {
		issues = append(issues, "missing_derived_kind")
	}
	if len(entry.EvidenceIDs) == 0 {
		issues = append(issues, "missing_evidence_ids")
	}
	if len(missing) > 0 {
		issues = append(issues, "missing_evidence_entries")
	}
	if entry.Boundary == nil {
		issues = append(issues, "missing_boundary")
	}
	return DerivedMemoryAudit{
		EntryID:            entry.ID,
		Category:           entry.Category,
		Status:             entry.Status,
		DerivedKind:        entry.DerivedKind,
		SourceType:         entry.SourceType,
		EvidenceIDs:        append([]string(nil), entry.EvidenceIDs...),
		MissingEvidenceIDs: missing,
		Boundary:           entry.Boundary,
		ContentPreview:     truncStr(strings.ReplaceAll(entry.Content, "\n", " "), 160),
		Issues:             issues,
	}
}

func FormatDerivedMemoryAuditsForTool(audits []DerivedMemoryAudit) string {
	if len(audits) == 0 {
		return "No derived memories found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Derived memories: %d\n", len(audits))
	for _, audit := range audits {
		kind := audit.DerivedKind
		if kind == "" {
			kind = "unknown"
		}
		fmt.Fprintf(&b, "- [%s] kind=%s category=%s", audit.EntryID, kind, audit.Category)
		if audit.Status != "" {
			fmt.Fprintf(&b, " status=%s", audit.Status)
		}
		if audit.SourceType != "" {
			fmt.Fprintf(&b, " source_type=%s", audit.SourceType)
		}
		if len(audit.EvidenceIDs) > 0 {
			fmt.Fprintf(&b, " evidence=%s", formatEvidenceIDList(audit.EvidenceIDs, 5))
		}
		if boundary := formatMemoryBoundary(audit.Boundary); boundary != "" {
			fmt.Fprintf(&b, " boundary={%s}", boundary)
		}
		if len(audit.Issues) > 0 {
			fmt.Fprintf(&b, " issues=%s", strings.Join(audit.Issues, ","))
		}
		if len(audit.MissingEvidenceIDs) > 0 {
			fmt.Fprintf(&b, " missing_evidence=%s", formatEvidenceIDList(audit.MissingEvidenceIDs, 5))
		}
		if audit.ContentPreview != "" {
			fmt.Fprintf(&b, "\n  preview: %s", audit.ContentPreview)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
