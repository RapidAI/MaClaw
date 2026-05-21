package httpapi

import "github.com/RapidAI/CodeClaw/corelib"

func isRemoteCodingToolUsageProviderID(providerID string) bool {
	return corelib.IsRemoteCodingToolTokenUsageProvider(providerID)
}

func filterRemoteCodingToolTokenUsage(usage map[string]*corelib.TokenUsageStat) map[string]*corelib.TokenUsageStat {
	return corelib.FilterRemoteCodingToolTokenUsage(usage)
}
