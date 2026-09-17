package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// newBindingReleaseTestMux registers the binding release route exactly as
// RegisterLLMRoutes does (llm_routes.go), so the tests exercise the real
// route-pattern + RequireHubMachine + handler composition.
func newBindingReleaseTestMux(t *testing.T, svc *hubCenterHTTPTestServices, proxyCfg *llmservice.ProxyConfig) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/llm/v1/binding/release", RequireHubMachine(svc.hubs, hubIDFromProxyRequest, llmservice.ProxyBindingReleaseHandler(proxyCfg)))
	return mux
}

func registerBindingReleaseHub(t *testing.T, svc *hubCenterHTTPTestServices, name string) (hubID, hubSecret string) {
	t.Helper()
	result, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:     name + "-owner@example.com",
		Name:           name,
		Description:    "binding release auth test hub",
		BaseURL:        "https://" + name + ".example.com",
		Visibility:     "private",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub %s: %v", name, err)
	}
	return result.HubID, result.HubSecret
}

func doBindingReleaseRequest(t *testing.T, handler http.Handler, hubID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/binding/release", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if hubID != "" {
		req.Header.Set("X-Hub-ID", hubID)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestBindingReleaseRouteRejectsWrongToken(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	hubID, _ := registerBindingReleaseHub(t, svc, "release-auth-a")
	mux := newBindingReleaseTestMux(t, svc, &llmservice.ProxyConfig{})

	rr := doBindingReleaseRequest(t, mux, hubID, "wrong-secret", `{"node_id":"hc-dead"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestBindingReleaseRouteRejectsMissingHubID(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	_, hubSecret := registerBindingReleaseHub(t, svc, "release-auth-b")
	mux := newBindingReleaseTestMux(t, svc, &llmservice.ProxyConfig{})

	rr := doBindingReleaseRequest(t, mux, "", hubSecret, `{"node_id":"hc-dead"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestBindingReleaseRouteRejectsHubIDTokenMismatch(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	hubAID, secretA := registerBindingReleaseHub(t, svc, "release-auth-c1")
	hubBID, _ := registerBindingReleaseHub(t, svc, "release-auth-c2")
	mux := newBindingReleaseTestMux(t, svc, &llmservice.ProxyConfig{})

	// X-Hub-ID claims hub B but the token belongs to hub A.
	rr := doBindingReleaseRequest(t, mux, hubBID, secretA, `{"node_id":"hc-dead"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}

	// Sanity: the same token with its own hub ID must pass the auth layer.
	rr = doBindingReleaseRequest(t, mux, hubAID, secretA, `{"node_id":"hc-dead"}`)
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("status = 401 with matching hub/token, want auth to pass; body=%s", rr.Body.String())
	}
}

func TestBindingReleaseRouteAuthenticatedRequestReachesHandler(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	hubID, hubSecret := registerBindingReleaseHub(t, svc, "release-auth-d")
	// ReleaseBinding is nil here (node has no HA binding manager), which the
	// handler contract defines as {"released":0} — reaching it at all proves
	// the request passed RequireHubMachine.
	mux := newBindingReleaseTestMux(t, svc, &llmservice.ProxyConfig{})

	rr := doBindingReleaseRequest(t, mux, hubID, hubSecret, `{"node_id":"hc-dead"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["released"] != float64(0) {
		t.Fatalf("released = %v, want 0", payload["released"])
	}
}
