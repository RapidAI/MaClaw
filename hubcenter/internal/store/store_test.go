package store

import "testing"

func TestHubUserUsageDailyTotalTokensExcludesCacheBreakdown(t *testing.T) {
	usage := HubUserUsageDaily{InputTokens: 100, OutputTokens: 20, CachedInputTokens: 80, CacheWriteTokens: 10}
	if got := usage.TotalTokens(); got != 120 {
		t.Fatalf("TotalTokens() = %d, want input + output only", got)
	}
}
