package main

import (
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// Type aliases for corelib types to maintain compatibility
type SessionTemplate = remote.SessionTemplate
type SessionTemplateManager = remote.SessionTemplateManager

// NewSessionTemplateManager creates a SessionTemplateManager that persists
// to the given path. Delegates to corelib implementation.
func NewSessionTemplateManager(path string) (*SessionTemplateManager, error) {
	return remote.NewSessionTemplateManager(path)
}

// MarshalTemplate serializes a SessionTemplate to JSON.
// Delegates to corelib implementation.
func MarshalTemplate(tpl SessionTemplate) ([]byte, error) {
	return remote.MarshalTemplate(tpl)
}

// UnmarshalTemplate deserializes JSON into a SessionTemplate.
// Delegates to corelib implementation.
func UnmarshalTemplate(data []byte) (SessionTemplate, error) {
	return remote.UnmarshalTemplate(data)
}
