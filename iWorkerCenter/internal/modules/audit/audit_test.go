package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func setupTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func TestInsertAndListRecent(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)

	for i := 0; i < 5; i++ {
		_ = repo.Insert("test-tenant", &ProxyLog{
			RequestID:  "req-" + string(rune('a'+i)),
			ProviderID: "provider-1",
			Model:      "gpt-4",
			WorkType:   "office_writing",
			CostTier:   "medium",
			Status:     "ok",
			LatencyMs:  100 + i*50,
			Summary:    "test request",
		})
	}

	logs, err := repo.ListRecent("test-tenant", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 5 {
		t.Errorf("expected 5 logs, got %d", len(logs))
	}
}

func TestListRecentFiltered(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	records := []*ProxyLog{
		{RequestID: "req-mcp", ProviderID: "iworkercenter", WorkType: "mcp_server_installed", Status: "ok", Summary: "Finance MCP installed", ErrorMsg: "department: finance"},
		{RequestID: "req-model", ProviderID: "openai", WorkType: "data_analysis", Status: "error", Summary: "model failed", ErrorMsg: "timeout"},
		{RequestID: "req-skill", ProviderID: "iworkercenter", WorkType: "skill_evolution_run", Status: "ok", Summary: "skill evolution run", ErrorMsg: "published=1"},
	}
	for _, record := range records {
		if err := repo.Insert("test-tenant", record); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	logs, err := repo.ListRecentFiltered("test-tenant", LogFilter{Status: "ok", Query: "finance", Limit: 10})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(logs) != 1 || logs[0].WorkType != "mcp_server_installed" {
		t.Fatalf("logs = %+v", logs)
	}

	logs, err = repo.ListRecentFiltered("test-tenant", LogFilter{WorkType: "data_analysis", Limit: 10})
	if err != nil {
		t.Fatalf("work type list: %v", err)
	}
	if len(logs) != 1 || logs[0].Status != "error" {
		t.Fatalf("logs = %+v", logs)
	}

	logs, err = repo.ListRecentFiltered("test-tenant", LogFilter{Category: "skill", Limit: 10})
	if err != nil {
		t.Fatalf("category list: %v", err)
	}
	if len(logs) != 1 || logs[0].WorkType != "skill_evolution_run" {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestGetStats(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)

	// Insert mix of ok and error
	for i := 0; i < 8; i++ {
		status := "ok"
		errMsg := ""
		if i >= 6 {
			status = "error"
			errMsg = "timeout"
		}
		_ = repo.Insert("test-tenant", &ProxyLog{
			ProviderID: "p1",
			Model:      "gpt-4",
			WorkType:   "data_analysis",
			CostTier:   "high",
			Status:     status,
			LatencyMs:  200,
			ErrorMsg:   errMsg,
			CreatedAt:  time.Now(),
		})
	}
	for _, record := range []*ProxyLog{
		{ProviderID: "iworkercenter", WorkType: "mcp_server_installed", CostTier: "internal", Status: "ok", Summary: "mcp installed", CreatedAt: time.Now()},
		{ProviderID: "iworkercenter", WorkType: "skill_evolution_run", CostTier: "internal", Status: "ok", Summary: "skill evolution", CreatedAt: time.Now()},
		{ProviderID: "iworkercenter", WorkType: "role_routing_action", CostTier: "internal", Status: "ok", Summary: "collaboration routed", CreatedAt: time.Now()},
	} {
		if err := repo.Insert("test-tenant", record); err != nil {
			t.Fatalf("insert category record: %v", err)
		}
	}

	stats, err := repo.GetStats("test-tenant", 24)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalRequests != 11 {
		t.Errorf("expected 11 total, got %d", stats.TotalRequests)
	}
	if stats.OKCount != 9 {
		t.Errorf("expected 9 ok, got %d", stats.OKCount)
	}
	if stats.ErrorCount != 2 {
		t.Errorf("expected 2 errors, got %d", stats.ErrorCount)
	}
	if stats.ModelEvents != 8 || stats.MCPEvents != 1 || stats.SkillEvents != 1 || stats.CollaborationEvents != 1 {
		t.Errorf("category counts = model:%d mcp:%d skill:%d collaboration:%d", stats.ModelEvents, stats.MCPEvents, stats.SkillEvents, stats.CollaborationEvents)
	}
	if stats.ModelErrors != 2 || stats.MCPErrors != 0 || stats.SkillErrors != 0 || stats.CollaborationErrors != 0 {
		t.Errorf("category errors = model:%d mcp:%d skill:%d collaboration:%d", stats.ModelErrors, stats.MCPErrors, stats.SkillErrors, stats.CollaborationErrors)
	}
	if stats.TopProvider != "p1" {
		t.Errorf("expected top provider p1, got %s", stats.TopProvider)
	}
	if stats.TopWorkType != "data_analysis" {
		t.Errorf("expected top work type data_analysis, got %s", stats.TopWorkType)
	}
	if stats.TopErrorWorkType != "data_analysis" {
		t.Errorf("expected top error work type data_analysis, got %s", stats.TopErrorWorkType)
	}
	if stats.LastErrorAt == "" {
		t.Errorf("expected last error timestamp")
	}
}

func TestGetStatsCountsCategoryErrors(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	records := []*ProxyLog{
		{ProviderID: "iworkercenter", WorkType: "mcp_server_status_changed", CostTier: "internal", Status: "error", ErrorMsg: "mcp disabled", CreatedAt: time.Now()},
		{ProviderID: "iworkercenter", WorkType: "skill_evolution_run", CostTier: "internal", Status: "error", ErrorMsg: "review failed", CreatedAt: time.Now()},
		{ProviderID: "iworkercenter", WorkType: "role_routing_action", CostTier: "internal", Status: "error", ErrorMsg: "no colleague", CreatedAt: time.Now()},
		{ProviderID: "p1", Model: "gpt-4", WorkType: "data_analysis", CostTier: "high", Status: "error", ErrorMsg: "timeout", CreatedAt: time.Now()},
	}
	for _, record := range records {
		if err := repo.Insert("test-tenant", record); err != nil {
			t.Fatalf("insert record: %v", err)
		}
	}

	stats, err := repo.GetStats("test-tenant", 24)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.ErrorCount != 4 {
		t.Fatalf("expected 4 errors, got %d", stats.ErrorCount)
	}
	if stats.MCPErrors != 1 || stats.SkillErrors != 1 || stats.CollaborationErrors != 1 || stats.ModelErrors != 1 {
		t.Fatalf("category errors = model:%d mcp:%d skill:%d collaboration:%d", stats.ModelErrors, stats.MCPErrors, stats.SkillErrors, stats.CollaborationErrors)
	}
}

func TestGetStats_Empty(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)

	stats, err := repo.GetStats("test-tenant", 24)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total, got %d", stats.TotalRequests)
	}
}

func TestHandleLogsFormatsCreatedAtAsUTC(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	createdAt := time.Date(2026, 5, 6, 10, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	if err := repo.Insert("test-tenant", &ProxyLog{
		RequestID: "req-utc",
		WorkType:  "mcp_server_status_changed",
		Status:    "ok",
		Summary:   "MCP server status changed",
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/logs?limit=10", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "test-tenant"))
	rr := httptest.NewRecorder()
	NewHandler(repo).handleLogs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Logs []map[string]any `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Logs) != 1 {
		t.Fatalf("logs = %+v", body.Logs)
	}
	if got, want := body.Logs[0]["created_at"], "2026-05-06T02:30:00Z"; got != want {
		t.Fatalf("created_at = %v, want %s", got, want)
	}
}

func TestHandleLogsAppliesFilters(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	for _, record := range []*ProxyLog{
		{RequestID: "req-mcp", ProviderID: "iworkercenter", WorkType: "mcp_server_status_changed", Status: "ok", Summary: "Finance MCP disabled", ErrorMsg: "status: disabled"},
		{RequestID: "req-other", ProviderID: "openai", WorkType: "data_analysis", Status: "error", Summary: "analysis failed", ErrorMsg: "timeout"},
	} {
		if err := repo.Insert("test-tenant", record); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/logs?status=ok&category=mcp&q=finance", nil)
	req = req.WithContext(tenant.WithTenantID(context.Background(), "test-tenant"))
	rr := httptest.NewRecorder()
	NewHandler(repo).handleLogs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Logs []map[string]any `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Logs) != 1 || body.Logs[0]["work_type"] != "mcp_server_status_changed" {
		t.Fatalf("logs = %+v", body.Logs)
	}
}
