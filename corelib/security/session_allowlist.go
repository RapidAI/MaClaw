package security

import (
	"strings"
	"sync"
	"time"
)

// SessionAllowlist manages session-scoped approval caching.
// When a user approves a dangerous operation, similar operations within the
// same session are auto-approved without re-prompting.
//
// Matching strategies:
//   - Exact tool name match
//   - Category match (e.g., approve "file_delete" category → all file delete patterns pass)
//   - Wildcard "*" approves everything for the session
type SessionAllowlist struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntries
}

type sessionEntries struct {
	byTool     map[string]*AllowlistEntry
	byCategory map[string]*AllowlistEntry
	wildcard   *AllowlistEntry
}

// AllowlistEntry records a single approval with metadata.
type AllowlistEntry struct {
	ApprovedAt time.Time `json:"approved_at"`
	ToolName   string    `json:"tool_name,omitempty"`
	Category   string    `json:"category,omitempty"`
	Pattern    string    `json:"pattern,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// NewSessionAllowlist creates a new SessionAllowlist.
func NewSessionAllowlist() *SessionAllowlist {
	return &SessionAllowlist{
		sessions: make(map[string]*sessionEntries),
	}
}

func newSessionEntries() *sessionEntries {
	return &sessionEntries{
		byTool:     make(map[string]*AllowlistEntry),
		byCategory: make(map[string]*AllowlistEntry),
	}
}

// IsApproved checks if a tool call is covered by an existing session approval.
// It checks in order: wildcard → exact tool name → category → pattern substring.
func (sal *SessionAllowlist) IsApproved(sessionID, toolName string, categories []string) bool {
	sal.mu.RLock()
	defer sal.mu.RUnlock()

	entries, ok := sal.sessions[sessionID]
	if !ok {
		return false
	}

	now := time.Now()

	// Wildcard check
	if entries.wildcard != nil && !isExpired(entries.wildcard, now) {
		return true
	}

	// Exact tool name check
	if e, ok := entries.byTool[toolName]; ok && !isExpired(e, now) {
		return true
	}

	// Category check
	for _, cat := range categories {
		if e, ok := entries.byCategory[cat]; ok && !isExpired(e, now) {
			return true
		}
	}

	// Substring match on tool patterns
	for pattern, e := range entries.byTool {
		if pattern != toolName && pattern != "" && strings.Contains(toolName, pattern) && !isExpired(e, now) {
			return true
		}
	}

	return false
}

// ApproveTool records a tool-level approval for the session.
func (sal *SessionAllowlist) ApproveTool(sessionID, toolName string, ttl time.Duration) {
	sal.mu.Lock()
	defer sal.mu.Unlock()

	entries := sal.getOrCreate(sessionID)
	entry := &AllowlistEntry{
		ApprovedAt: time.Now(),
		ToolName:   toolName,
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}
	entries.byTool[toolName] = entry
}

// ApproveCategory records a category-level approval for the session.
// All tools whose RiskPattern.Category matches will be auto-approved.
func (sal *SessionAllowlist) ApproveCategory(sessionID, category string, ttl time.Duration) {
	sal.mu.Lock()
	defer sal.mu.Unlock()

	entries := sal.getOrCreate(sessionID)
	entry := &AllowlistEntry{
		ApprovedAt: time.Now(),
		Category:   category,
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}
	entries.byCategory[category] = entry
}

// ApproveAll records a wildcard approval for the session (approve everything).
func (sal *SessionAllowlist) ApproveAll(sessionID string, ttl time.Duration) {
	sal.mu.Lock()
	defer sal.mu.Unlock()

	entries := sal.getOrCreate(sessionID)
	entry := &AllowlistEntry{
		ApprovedAt: time.Now(),
		Pattern:    "*",
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}
	entries.wildcard = entry
}

// ClearSession removes all approvals for a session.
func (sal *SessionAllowlist) ClearSession(sessionID string) {
	sal.mu.Lock()
	defer sal.mu.Unlock()
	delete(sal.sessions, sessionID)
}

// SessionEntryCount returns the number of approval entries for a session.
func (sal *SessionAllowlist) SessionEntryCount(sessionID string) int {
	sal.mu.RLock()
	defer sal.mu.RUnlock()

	entries, ok := sal.sessions[sessionID]
	if !ok {
		return 0
	}
	count := len(entries.byTool) + len(entries.byCategory)
	if entries.wildcard != nil {
		count++
	}
	return count
}

func (sal *SessionAllowlist) getOrCreate(sessionID string) *sessionEntries {
	entries, ok := sal.sessions[sessionID]
	if !ok {
		entries = newSessionEntries()
		sal.sessions[sessionID] = entries
	}
	return entries
}

func isExpired(e *AllowlistEntry, now time.Time) bool {
	return !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
}

// CategoriesForAssessment extracts the risk pattern categories from a RiskAssessment.
// It maps each matched factor name back to its RiskPattern.Category.
func CategoriesForAssessment(assessment RiskAssessment, analyzer *RiskAnalyzer) []string {
	if analyzer == nil || len(assessment.Factors) == 0 {
		return nil
	}

	analyzer.mu.RLock()
	defer analyzer.mu.RUnlock()

	// Build name→category index (O(patterns)), then look up each factor (O(factors)).
	nameToCategory := make(map[string]string, len(analyzer.builtinPatterns)+len(analyzer.customPatterns))
	for _, p := range analyzer.builtinPatterns {
		if p.Category != "" {
			nameToCategory[p.Name] = p.Category
		}
	}
	for _, p := range analyzer.customPatterns {
		if p.Category != "" {
			nameToCategory[p.Name] = p.Category
		}
	}

	categorySet := make(map[string]bool)
	for _, factor := range assessment.Factors {
		if cat, ok := nameToCategory[factor]; ok {
			categorySet[cat] = true
		}
	}

	if len(categorySet) == 0 {
		return nil
	}
	categories := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	return categories
}
