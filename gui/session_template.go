package main

import (
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// NewSessionTemplateManager creates a SessionTemplateManager that persists
// to the given path. Delegates to corelib implementation.
func NewSessionTemplateManager(path string) (*remote.SessionTemplateManager, error) {
	return remote.NewSessionTemplateManager(path)
}

// MarshalTemplate serializes a SessionTemplate to JSON.
// Delegates to corelib implementation.
func MarshalTemplate(tpl remote.SessionTemplate) ([]byte, error) {
	return remote.MarshalTemplate(tpl)
}

// UnmarshalTemplate deserializes JSON into a SessionTemplate.
// Delegates to corelib implementation.
func UnmarshalTemplate(data []byte) (remote.SessionTemplate, error) {
	return remote.UnmarshalTemplate(data)
}
