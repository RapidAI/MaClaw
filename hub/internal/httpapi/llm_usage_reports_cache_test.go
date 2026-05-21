package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
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

func TestLLMUsageReportIncludesRMBCostCounters(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)
	rep.addUsage(ts, "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:   1_000_000,
		OutputTokens:  500_000,
		TotalTokens:   1_500_000,
		InputCostRMB:  3,
		OutputCostRMB: 3,
		TotalCostRMB:  6,
		Requests:      1,
	}, 0)
	rep.addUsage(ts.Add(time.Hour), "user@example.com", []string{"engineering"}, corelib.TokenUsageStat{
		InputTokens:   250_000,
		OutputTokens:  250_000,
		TotalTokens:   500_000,
		InputCostRMB:  0.25,
		OutputCostRMB: 0.5,
		TotalCostRMB:  0.75,
		Requests:      1,
	}, 0)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "daily", "2026-04-21", "2026-04", "", ts)
	if resp.Summary.InputCostRMB != 3.25 || resp.Summary.OutputCostRMB != 3.5 || resp.Summary.TotalCostRMB != 6.75 {
		t.Fatalf("summary cost = input %.4f output %.4f total %.4f, want 3.25/3.5/6.75", resp.Summary.InputCostRMB, resp.Summary.OutputCostRMB, resp.Summary.TotalCostRMB)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].TotalCostRMB != 6.75 {
		t.Fatalf("row cost not preserved: %#v", resp.Rows)
	}
	if resp.Trend[9].TotalCostRMB != 6 || resp.Trend[10].TotalCostRMB != 0.75 {
		t.Fatalf("trend cost not preserved: hour9=%+v hour10=%+v", resp.Trend[9], resp.Trend[10])
	}
}

func TestMonthlyUsageReportEntitySummaryDoesNotLeakGlobalTotals(t *testing.T) {
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	rep.addUsage(time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC), "alice@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		TotalCostRMB: 1.5,
		Requests:     1,
	}, 0.015)
	rep.addUsage(time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC), "bob@example.com", nil, corelib.TokenUsageStat{
		InputTokens:  200,
		OutputTokens: 100,
		TotalTokens:  300,
		TotalCostRMB: 3,
		Requests:     1,
	}, 0.03)

	resp := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "monthly", "", "2026-04", "alice@example.com", now)
	if resp.Summary.TotalTokens != 150 || resp.Summary.TotalCostRMB != 1.5 || resp.Summary.Credits != 0.015 {
		t.Fatalf("alice monthly summary leaked global totals: %+v", resp.Summary)
	}
	if len(resp.Rows) != 1 || resp.Rows[0].ID != "alice@example.com" || resp.Rows[0].TotalTokens != 150 {
		t.Fatalf("alice monthly rows = %#v", resp.Rows)
	}

	missing := buildLLMUsageReportResponse(context.Background(), rep, nil, "user", "monthly", "", "2026-04", "nobody@example.com", now)
	if missing.Summary.TotalTokens != 0 || missing.Summary.TotalCostRMB != 0 || len(missing.Rows) != 0 {
		t.Fatalf("missing entity should be empty, got summary=%+v rows=%#v", missing.Summary, missing.Rows)
	}
}

func TestLLMUsageReportHandlerUsesTenantScopedSettings(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	ts := time.Date(2026, 4, 21, 9, 30, 0, 0, time.UTC)

	globalRep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	globalRep.addUsage(ts, "global@example.com", nil, corelib.TokenUsageStat{TotalTokens: 900, Requests: 1}, 0)
	if err := saveLLMUsageReports(context.Background(), system, globalRep); err != nil {
		t.Fatalf("save global usage report: %v", err)
	}

	tenantRep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	tenantRep.addUsage(ts, "tenant@example.com", nil, corelib.TokenUsageStat{TotalTokens: 123, Requests: 1}, 0)
	if err := saveLLMUsageReports(context.Background(), scopedSystemSettingsForTenant("tenant_a", system), tenantRep); err != nil {
		t.Fatalf("save tenant usage report: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/usage-report?period=monthly&month=2026-04", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()
	GetLLMUsageReportHandler(system, nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmUsageReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Summary.TotalTokens != 123 || len(resp.Rows) != 1 || resp.Rows[0].ID != "tenant@example.com" {
		t.Fatalf("tenant report should not read global settings, summary=%+v rows=%#v", resp.Summary, resp.Rows)
	}
}
