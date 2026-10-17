package main

import (
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

const maxAgentTypeRunes = 128

// normalizeAgentType trims a User-Agent and drops control characters. Preset
// names and custom text are kept as typed so a provider can match them exactly.
func normalizeAgentType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	if utf8.RuneCountInString(cleaned) <= maxAgentTypeRunes {
		return cleaned
	}
	runes := []rune(cleaned)
	return string(runes[:maxAgentTypeRunes])
}

// printableAgentType reports whether a User-Agent can be sent as an HTTP header.
// Empty is the QAgent default. Bytes outside printable ASCII are rejected by net/http.
func printableAgentType(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] > 0x7e {
			return false
		}
	}
	return true
}

// defaultCodexModelAlias is the model name written into Codex config.
// Requests that use it are rewritten to the selected upstream model.
const defaultCodexModelAlias = "auto"

// codexModelAlias returns the stable model name Codex should request.
// An empty setting uses auto.
func codexModelAlias(s Settings) string {
	alias := strings.TrimSpace(s.ModelAlias)
	if alias == "" {
		return defaultCodexModelAlias
	}
	return alias
}

// proxyClientName is the User-Agent the proxy sends upstream. An empty
// setting keeps the historical QAgent identity.
func proxyClientName(s Settings) string {
	if name := strings.TrimSpace(s.AgentType); name != "" {
		return name
	}
	return corelib.CodeGenClientName
}
