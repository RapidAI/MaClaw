package workflow

import "strings"

// WorkflowChecker is the minimal interface QuickFilter needs from the engine.
type WorkflowChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// QuickFilter routes only deterministic workflow state. It must not infer user
// intent from words; non-empty free-form input goes to intent understanding.
type QuickFilter struct {
	engine WorkflowChecker
}

// NewQuickFilter creates a QuickFilter with the given engine reference.
func NewQuickFilter(engine WorkflowChecker) *QuickFilter {
	return &QuickFilter{engine: engine}
}

// Classify determines the FilterResult for a user message.
//
// Priority:
//  1. active_workflow      - user has an active workflow
//  2. active_understanding - user has an active understanding session
//  3. simple_directive     - empty/whitespace only input
//  4. needs_understanding  - classifier decides intent and workflow category
func (f *QuickFilter) Classify(userID, text string) FilterResult {
	if f.engine != nil {
		if f.engine.HasActiveWorkflow(userID) {
			return FilterActiveWorkflow
		}
		if f.engine.HasActiveUnderstanding(userID) {
			return FilterActiveUnderstanding
		}
	}

	if strings.TrimSpace(text) == "" {
		return FilterSimpleDirective
	}

	return FilterNeedsUnderstanding
}
