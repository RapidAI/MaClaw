package corelib

import "strings"

var remoteCodingToolTokenUsageProviderPrefixes = [...]string{
	"codex:",
	"claude:",
	"opencode:",
	"codebuddy:",
	"iflow:",
	"kilo:",
	"remote:",
}

// IsRemoteCodingToolTokenUsageProvider reports whether a provider key belongs
// to an independent remote coding tool. These token counts are diagnostics for
// the tool session, not Maclaw or Hub LLM provider usage.
func IsRemoteCodingToolTokenUsageProvider(provider string) bool {
	value := strings.ToLower(strings.TrimSpace(provider))
	if value == "" {
		return false
	}
	for _, prefix := range remoteCodingToolTokenUsageProviderPrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// FilterRemoteCodingToolTokenUsage returns a copy of usage with remote coding
// tool diagnostic keys removed. Normal provider stats are cloned so callers can
// safely expose or mutate the returned map without changing persisted config.
func FilterRemoteCodingToolTokenUsage(usage map[string]*TokenUsageStat) map[string]*TokenUsageStat {
	if usage == nil {
		return nil
	}
	filtered := make(map[string]*TokenUsageStat, len(usage))
	for provider, stat := range usage {
		if IsRemoteCodingToolTokenUsageProvider(provider) {
			continue
		}
		if stat == nil {
			filtered[provider] = nil
			continue
		}
		copy := *stat
		filtered[provider] = &copy
	}
	return filtered
}
