package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

type stubProviderTrafficRepo struct {
	dayStart   time.Time
	weekStart  time.Time
	monthStart time.Time
}

func (r *stubProviderTrafficRepo) Insert(context.Context, *llmservice.TenantUsageRecord) error {
	return nil
}
func (r *stubProviderTrafficRepo) QuerySummary(context.Context, llmservice.UsageFilter) ([]llmservice.TenantUsageSummary, error) {
	return nil, nil
}
func (r *stubProviderTrafficRepo) QueryRecent(context.Context, string, string, int) ([]*llmservice.TenantUsageRecord, error) {
	return nil, nil
}
func (r *stubProviderTrafficRepo) QueryClassTraffic(context.Context, string, time.Time) ([]llmservice.ClassTrafficRow, map[string]int64, []llmservice.ClassTrafficSample, error) {
	return nil, nil, nil, nil
}
func (r *stubProviderTrafficRepo) QueryProviderTraffic(_ context.Context, dayStart, weekStart, monthStart time.Time) (map[string]llmservice.ProviderPeriodTraffic, error) {
	r.dayStart, r.weekStart, r.monthStart = dayStart, weekStart, monthStart
	return map[string]llmservice.ProviderPeriodTraffic{
		"deepseek": {
			Day:   llmservice.TokenTraffic{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
			Week:  llmservice.TokenTraffic{InputTokens: 100, OutputTokens: 200, TotalTokens: 300},
			Month: llmservice.TokenTraffic{InputTokens: 1000, OutputTokens: 2000, TotalTokens: 3000},
		},
	}, nil
}

func (r *stubProviderTrafficRepo) QueryServiceGroupTraffic(_ context.Context, dayStart, weekStart, monthStart time.Time) (map[string]llmservice.ProviderPeriodTraffic, error) {
	r.dayStart, r.weekStart, r.monthStart = dayStart, weekStart, monthStart
	return map[string]llmservice.ProviderPeriodTraffic{
		"maclaw-official": {
			Day:   llmservice.TokenTraffic{InputTokens: 12, OutputTokens: 24, TotalTokens: 36},
			Week:  llmservice.TokenTraffic{InputTokens: 120, OutputTokens: 240, TotalTokens: 360},
			Month: llmservice.TokenTraffic{InputTokens: 1200, OutputTokens: 2400, TotalTokens: 3600},
		},
	}, nil
}

func TestAdminLLMProviderTrafficHandler(t *testing.T) {
	repo := &stubProviderTrafficRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers/traffic?timezone=Asia/Shanghai", nil)
	rec := httptest.NewRecorder()
	adminLLMProviderTrafficHandler(llmservice.NewStatsService(repo))(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload llmservice.ProviderTrafficReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q", payload.Timezone)
	}
	got := payload.Traffic["deepseek"]
	if got.Day.TotalTokens != 30 || got.Week.TotalTokens != 300 || got.Month.TotalTokens != 3000 {
		t.Fatalf("traffic = %#v", got)
	}
	if repo.dayStart.IsZero() || repo.weekStart.IsZero() || repo.monthStart.IsZero() {
		t.Fatal("handler did not query period bounds")
	}
}

func TestAdminLLMProviderTrafficHandlerNilService(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers/traffic", nil)
	rec := httptest.NewRecorder()
	adminLLMProviderTrafficHandler(nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload llmservice.ProviderTrafficReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Traffic == nil {
		t.Fatal("nil service should return an empty traffic map")
	}
}

func TestAdminLLMServiceGroupTrafficHandler(t *testing.T) {
	repo := &stubProviderTrafficRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-groups/traffic?timezone=Asia/Shanghai", nil)
	rec := httptest.NewRecorder()
	adminLLMServiceGroupTrafficHandler(llmservice.NewStatsService(repo))(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload llmservice.ServiceGroupTrafficReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Traffic["maclaw-official"].Month.TotalTokens != 3600 {
		t.Fatalf("traffic = %#v", payload.Traffic)
	}
	if repo.dayStart.IsZero() || repo.weekStart.IsZero() || repo.monthStart.IsZero() {
		t.Fatal("handler did not query period bounds")
	}
}

func TestAdminLLMProviderTrafficHandlerIgnoresOversizedTimezone(t *testing.T) {
	repo := &stubProviderTrafficRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers/traffic?timezone="+strings.Repeat("X", 80), nil)
	rec := httptest.NewRecorder()
	adminLLMProviderTrafficHandler(llmservice.NewStatsService(repo))(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload llmservice.ProviderTrafficReport
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want default Asia/Shanghai", payload.Timezone)
	}
}
