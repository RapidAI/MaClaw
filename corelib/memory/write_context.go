package memory

import "strings"

// WriteContext is the host-authoritative provenance and visibility context for
// a memory write. Project and owner fields must come from the host, not model
// supplied tags or arguments.
type WriteContext struct {
	OwnerID     string `json:"owner_id"`
	ProjectPath string `json:"project_path,omitempty"`
	Source      string `json:"source,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	TurnID      string `json:"turn_id,omitempty"`
}

func (c WriteContext) apply(entry *Entry) {
	if entry == nil {
		return
	}
	entry.OwnerID = strings.TrimSpace(c.OwnerID)
	if entry.SourceType == "" {
		entry.SourceType = strings.TrimSpace(c.Source)
	}
	if entry.Scope == "" {
		entry.Scope = InferScope(entry.Category)
	}
	if entry.Scope != ScopeProject || strings.TrimSpace(c.ProjectPath) == "" {
		return
	}
	if entry.Boundary == nil {
		entry.Boundary = &MemoryBoundary{}
	} else {
		entry.Boundary = cloneMemoryBoundary(entry.Boundary)
	}
	entry.Boundary.ProjectPath = strings.TrimSpace(c.ProjectPath)
	if entry.Boundary.OwnerID == "" {
		entry.Boundary.OwnerID = strings.TrimSpace(c.OwnerID)
	}
}

func (s *Store) SaveWithWriteContext(entry Entry, writeContext WriteContext, contextHint string) error {
	writeContext.apply(&entry)
	return s.SaveWithContext(entry, contextHint)
}

func (s *Store) SaveManualMemoryWithContext(content string, category Category, tags []string, writeContext WriteContext) error {
	if s == nil {
		return nil
	}
	entry := Entry{
		Content:    content,
		Category:   category,
		Tags:       tags,
		SourceType: ManualMemorySourceType,
	}
	return s.SaveWithWriteContext(entry, writeContext, "")
}
