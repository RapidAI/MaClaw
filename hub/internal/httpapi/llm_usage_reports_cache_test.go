package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLLMUsageReportIncludesPromptCacheCounters(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsage(ts, "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:       100,
		OutputTokens:      20,
		TotalTokens:       120,
		CachedInputTokens: 40,
		CacheWriteTokens:  8,
		Requests:          1,
		CachedRequests:    1,
	}, 0.5)
	rep.addUsage(ts.Add(time.Hour), "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:       300,
		OutputTokens:      60,
		TotalTokens:       360,
		CachedInputTokens: 0,
		CacheWriteTokens:  4,
		Requests:          1,
		CachedRequests:    0,
	}, 1.5)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.Summary.InputTokens != 400 || resp.Summary.TotalTokens != 480 {
		t.Fatalf("summary tokens = input %d total %d", resp.Summary.InputTokens, resp.Summary.TotalTokens)
	}
	if resp.Summary.CachedInputTokens != 40 {
		t.Fatalf("summary cached input = %d, want 40", resp.Summary.CachedInputTokens)
	}
	if resp.Summary.CacheWriteTokens != 12 {
		t.Fatalf("summary cache write = %d, want 12", resp.Summary.CacheWriteTokens)
	}
	if resp.Summary.CachedRequests != 1 || resp.Summary.Requests != 2 {
		t.Fatalf("summary cached requests = %d/%d, want 1/2", resp.Summary.CachedRequests, resp.Summary.Requests)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(resp.Rows))
	}
	row := resp.Rows[0]
	if row.ID != "user@example.com" {
		t.Fatalf("row id = %q", row.ID)
	}
	if row.CachedInputTokens != 40 || row.CacheWriteTokens != 12 {
		t.Fatalf("row cache tokens = read %d write %d", row.CachedInputTokens, row.CacheWriteTokens)
	}
	if row.CachedRequests != 1 || row.Requests != 2 {
		t.Fatalf("row cached requests = %d/%d", row.CachedRequests, row.Requests)
	}
	if len(resp.Trend) != 24 {
		t.Fatalf("trend len = %d, want 24", len(resp.Trend))
	}
	if resp.Trend[9].CachedInputTokens != 40 || resp.Trend[10].CacheWriteTokens != 4 {
		t.Fatalf("trend cache counters not preserved: hour9=%+v hour10=%+v", resp.Trend[9], resp.Trend[10])
	}
}
