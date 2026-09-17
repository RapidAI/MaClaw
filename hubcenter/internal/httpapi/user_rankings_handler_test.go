package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type fakeCenterUserUsageRepo struct {
	upserted []*store.HubUserUsageDaily
	replaced bool
}

func (r *fakeCenterUserUsageRepo) UpsertDaily(_ context.Context, items []*store.HubUserUsageDaily) error {
	r.upserted = append(r.upserted, items...)
	return nil
}

func (r *fakeCenterUserUsageRepo) ReplaceDaily(_ context.Context, _ string, _ []string, _, _ string, items []*store.HubUserUsageDaily) error {
	r.replaced = true
	r.upserted = append(r.upserted, items...)
	return nil
}

func (r *fakeCenterUserUsageRepo) Summarize(_ context.Context, _, _ string, _, _ time.Time) ([]*store.HubUserUsageDaily, error) {
	return nil, nil
}

func TestHubUserUsageSyncHandlerAcceptsBodyAboveDefaultLimit(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	ctx := context.Background()
	secret := "hub-usage-sync-secret"
	now := time.Now().UTC()
	if err := svc.store.Hubs.Create(ctx, &store.HubInstance{
		ID:            "hub_usage_sync",
		OwnerEmail:    "owner@example.com",
		Name:          "Usage Sync Hub",
		BaseURL:       "https://hub.example.com",
		Status:        "online",
		HubSecretHash: testHashToken(secret),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create hub: %v", err)
	}

	items := make([]map[string]any, 0, 2000)
	for i := 0; i < 2000; i++ {
		items = append(items, map[string]any{
			"user_email":   "user@example.com",
			"day":          "2026-04-27",
			"input_tokens": 100 + i,
		})
	}
	payload, err := json.Marshal(map[string]any{"hub_secret": secret, "items": items})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if len(payload) <= defaultJSONBodyLimit {
		t.Fatalf("payload len = %d, want > %d so the old limit would reject it", len(payload), defaultJSONBodyLimit)
	}

	repo := &fakeCenterUserUsageRepo{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hubs/{id}/user-usage/sync", HubUserUsageSyncHandler(svc.hubs, repo))
	req := httptest.NewRequest(http.MethodPost, "/api/hubs/hub_usage_sync/user-usage/sync", strings.NewReader(string(payload)))
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	if len(repo.upserted) != len(items) {
		t.Fatalf("upserted items = %d, want %d", len(repo.upserted), len(items))
	}
}
