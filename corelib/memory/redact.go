package memory

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// redactSecretsInMemory removes sensitive information (API keys, passwords,
// tokens, private keys, connection strings) from text before it is persisted
// to long-term memory.
//
// This prevents secrets that appear in conversation history (e.g. SSH passwords,
// API keys in config files, database connection strings) from being saved to
// memories.json and later recalled into LLM prompts.
//
// Inspired by Codex CLI's redact_secrets() which is applied at every memory
// write path (both input serialization and LLM extraction output).
//
// Uses the existing SensitiveDetector from corelib/security which already has
// patterns for sk-* API keys, AWS access keys, private key headers, password
// assignments, and JWT tokens.
var (
	memoryRedactor     *security.SensitiveDetector
	memoryRedactorOnce sync.Once
)

func redactSecretsInMemory(text string) string {
	if text == "" {
		return text
	}
	memoryRedactorOnce.Do(func() {
		memoryRedactor = security.NewSensitiveDetector()
	})
	return memoryRedactor.Redact(text)
}
