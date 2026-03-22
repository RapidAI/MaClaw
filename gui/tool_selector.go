package main

import (
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ToolProfile describes a programming tool's capability profile used for
// intelligent tool selection. Each profile captures the languages, frameworks,
// and task types the tool excels at, along with a base quality score.
type ToolProfile = tool.Profile

// ToolSelector recommends the best programming tool for a given task.
// This is a thin wrapper around corelib/tool.Selector.
type ToolSelector struct {
	inner *tool.Selector
}

// NewToolSelector creates a ToolSelector pre-loaded with default capability profiles.
func NewToolSelector() *ToolSelector {
	return &ToolSelector{inner: tool.NewSelector()}
}

// Recommend returns the name of the best tool for the given task description
// and a human-readable reason for the recommendation.
func (s *ToolSelector) Recommend(taskDescription string, installed []string) (string, string) {
	return s.inner.Recommend(taskDescription, installed)
}
