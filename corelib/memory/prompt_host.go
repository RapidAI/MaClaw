package memory

// SelfIdentitySummaryForHost returns a bounded self-identity summary for host
// prompt assembly without exposing category-specific scan details.
func (s *Store) SelfIdentitySummaryForHost(maxRunes int) string {
	if s == nil {
		return ""
	}
	return s.SelfIdentitySummary(maxRunes)
}

// UserFactSummaryForHost builds the shared user profile prompt section used by
// GUI/TUI/server host surfaces.
func (s *Store) UserFactSummaryForHost(opts UserFactSummaryPromptOptions) string {
	if s == nil {
		return ""
	}
	return s.UserFactSummaryForPrompt(opts)
}

// StaticMemorySectionForHost builds the shared static memory prompt section for
// host agent prompts.
func (s *Store) StaticMemorySectionForHost(opts StaticMemoryPromptOptions) string {
	if s == nil {
		return ""
	}
	return s.StaticMemorySectionForPrompt(opts)
}

// ProactiveContextForHost builds the shared dynamic memory prompt context and
// returns both the rendered section and recalled entries for host logging.
func (s *Store) ProactiveContextForHost(query string, opts ProactivePromptOptions) (string, []Entry) {
	if s == nil {
		return "", nil
	}
	return s.ProactiveContextForPrompt(query, opts)
}

// RecallByModeForHost applies the shared recall mode matrix for host command
// and agent surfaces.
func (s *Store) RecallByModeForHost(query string, category Category, mode string, projectPath string, limit int, ownerID ...string) (ToolRecallResult, error) {
	if s == nil {
		return ToolRecallResult{}, nil
	}
	return s.RecallByMode(query, category, mode, projectPath, limit, ownerID...)
}
