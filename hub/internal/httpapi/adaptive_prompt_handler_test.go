package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

func TestAdaptivePromptMetricsHandler_EmptyDevices(t *testing.T) {
	rr := httptest.NewRecorder()
	AdaptivePromptMetricsHandler(nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/adaptive-prompt/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("out=%#v", out)
	}
	if out["online_machines"] != float64(0) {
		t.Fatalf("online=%v", out["online_machines"])
	}
}

func TestAdaptivePromptMetricsHandler_EmptyOnlineService(t *testing.T) {
	svc := device.NewService(nil, device.NewRuntime())
	rr := httptest.NewRecorder()
	AdaptivePromptMetricsHandler(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/adaptive-prompt/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("out=%#v", out)
	}
	if _, ok := out["totals"]; !ok {
		t.Fatalf("missing totals: %#v", out)
	}
}
