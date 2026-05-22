package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeishuConfigSaveAndLoad(t *testing.T) {
	settings := &testSystemSettingsRepo{}

	// Save config.
	payload, _ := json.Marshal(map[string]any{
		"enabled":    true,
		"app_id":     "cli_test123",
		"app_secret": "secret_abc",
	})
	saveReq := httptest.NewRequest(http.MethodPost, "/api/admin/feishu/config", bytes.NewReader(payload))
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	UpdateFeishuConfigHandler(settings, nil).ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("save: expected 200, got %d body=%s", saveRR.Code, saveRR.Body.String())
	}

	var saved FeishuConfigState
	if err := json.Unmarshal(saveRR.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if !saved.Enabled || saved.AppID != "cli_test123" {
		t.Fatalf("unexpected saved config: %+v", saved)
	}
	// Secret should be masked in response.
	if saved.AppSecret == "secret_abc" {
		t.Fatal("secret should be masked in response")
	}

	// Load config.
	loadReq := httptest.NewRequest(http.MethodGet, "/api/admin/feishu/config", nil)
	loadRR := httptest.NewRecorder()
	GetFeishuConfigHandler(settings).ServeHTTP(loadRR, loadReq)
	if loadRR.Code != http.StatusOK {
		t.Fatalf("load: expected 200, got %d body=%s", loadRR.Code, loadRR.Body.String())
	}

	var loaded FeishuConfigState
	if err := json.Unmarshal(loadRR.Body.Bytes(), &loaded); err != nil {
		t.Fatalf("decode loaded: %v", err)
	}
	if !loaded.Enabled || loaded.AppID != "cli_test123" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
	if loaded.AppSecret == "secret_abc" {
		t.Fatal("secret should be masked on load")
	}
}

func TestFeishuConfigSavePreservesMaskedSecret(t *testing.T) {
	settings := &testSystemSettingsRepo{}

	// First save with real secret.
	payload1, _ := json.Marshal(map[string]any{
		"enabled":    true,
		"app_id":     "cli_test",
		"app_secret": "my_real_secret_value",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/admin/feishu/config", bytes.NewReader(payload1))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	UpdateFeishuConfigHandler(settings, nil).ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first save failed: %d", rr1.Code)
	}

	// Second save with masked secret (simulating frontend re-submit).
	var resp1 FeishuConfigState
	json.Unmarshal(rr1.Body.Bytes(), &resp1)

	payload2, _ := json.Marshal(map[string]any{
		"enabled":    true,
		"app_id":     "cli_test",
		"app_secret": resp1.AppSecret, // masked value from first response
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/admin/feishu/config", bytes.NewReader(payload2))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	UpdateFeishuConfigHandler(settings, nil).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second save failed: %d", rr2.Code)
	}

	// Verify the real secret is preserved in storage.
	raw, _ := settings.Get(nil, feishuConfigKey)
	var stored FeishuConfigState
	json.Unmarshal([]byte(raw), &stored)
	if stored.AppSecret != "my_real_secret_value" {
		t.Fatalf("expected original secret preserved, got %q", stored.AppSecret)
	}
}

func TestFeishuConfigLoadEmpty(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/feishu/config", nil)
	rr := httptest.NewRecorder()
	GetFeishuConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var cfg FeishuConfigState
	json.Unmarshal(rr.Body.Bytes(), &cfg)
	if cfg.Enabled || cfg.AppID != "" {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestFeishuRouteViaRouter(t *testing.T) {
	// This test verifies the route is actually reachable through the full router.
	router, _ := newAdminRouterTestServices(t)
	globalToken := issueHubAdminToken(t, router)
	token := issueTenantAdminToken(t, router, globalToken, "feishu-route", "feishu-owner")

	payload, _ := json.Marshal(map[string]any{
		"enabled":    true,
		"app_id":     "cli_route_test",
		"app_secret": "secret123",
	})
	rr := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/feishu/config", json.RawMessage(payload), token)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/feishu/config via router: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Also test GET.
	rrGet := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/feishu/config", nil, token)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/feishu/config via router: expected 200, got %d body=%s", rrGet.Code, rrGet.Body.String())
	}
}
