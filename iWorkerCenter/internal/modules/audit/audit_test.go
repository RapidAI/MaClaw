package audit

import (
	"testing"
	"time"

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

	stats, err := repo.GetStats("test-tenant", 24)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalRequests != 8 {
		t.Errorf("expected 8 total, got %d", stats.TotalRequests)
	}
	if stats.OKCount != 6 {
		t.Errorf("expected 6 ok, got %d", stats.OKCount)
	}
	if stats.ErrorCount != 2 {
		t.Errorf("expected 2 errors, got %d", stats.ErrorCount)
	}
	if stats.TopProvider != "p1" {
		t.Errorf("expected top provider p1, got %s", stats.TopProvider)
	}
	if stats.TopWorkType != "data_analysis" {
		t.Errorf("expected top work type data_analysis, got %s", stats.TopWorkType)
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
