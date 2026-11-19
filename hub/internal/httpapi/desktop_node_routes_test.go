package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The edge-node routes are registered only when HUB_EDGE_NODES is on.
// A wildcard next to an existing /bots/{id}/... route panics the process
// before it listens, which is what took hub.mypapers.top down.
func TestDesktopNodeRoutesDoNotConflictWithBotRoutes(t *testing.T) {
	mux := http.NewServeMux()
	nop := func(http.ResponseWriter, *http.Request) {}
	for _, pattern := range []string{
		"DELETE /api/v1/bots/{id}",
		"DELETE /api/v1/bots/{id}/preference",
		"DELETE /api/v1/bots/{id}/schedules/{taskID}",
		"POST /api/v1/bots/{id}/preference",
		"PUT /api/v1/bots/{id}/appearance",
		"PUT /api/v1/bots/{id}/style",
		"PUT /api/v1/bots/{id}/desktop-mode",
		"PUT /api/v1/bots/{id}/demo/skills/{skillName}",
		"GET /api/v1/bots/{id}/desktop",
		"GET /api/v1/bots/desktop-node",
		"POST /api/v1/bots/desktop-node/code",
		"PUT /api/v1/bots/desktop-node/cloud",
		"PUT /api/v1/bots/desktop-node/{server_id}/active",
		"DELETE /api/v1/self-desktop-nodes/{server_id}",
	} {
		mux.HandleFunc(pattern, nop)
	}
	req := httptestNew(t, http.MethodDelete, "/api/v1/self-desktop-nodes/srv_1")
	if _, pat := mux.Handler(req); pat != "DELETE /api/v1/self-desktop-nodes/{server_id}" {
		t.Fatalf("delete matched %s", pat)
	}
	cloud := httptestNew(t, http.MethodPut, "/api/v1/bots/desktop-node/cloud")
	if _, pat := mux.Handler(cloud); pat != "PUT /api/v1/bots/desktop-node/cloud" {
		t.Fatalf("cloud matched %s", pat)
	}
}

func TestDesktopNodeDeleteUnderBotsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DELETE /api/v1/bots/desktop-node/{server_id} overlaps DELETE /api/v1/bots/{id}/preference")
		}
	}()
	mux := http.NewServeMux()
	nop := func(http.ResponseWriter, *http.Request) {}
	mux.HandleFunc("DELETE /api/v1/bots/{id}/preference", nop)
	mux.HandleFunc("DELETE /api/v1/bots/desktop-node/{server_id}", nop)
}

func httptestNew(t *testing.T, method, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestEdgeRoutesStayUnregisteredWhenTheSwitchIsOff(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "")
	handler, _ := newAdminRouterTestServices(t)
	for _, path := range []string{
		"/api/v1/edge/link",
		"/api/v1/edge/enroll",
		"/api/v1/desktop-cdp/ticket/json",
		"/api/v1/bots/desktop-node",
	} {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptestNew(t, http.MethodGet, path))
		if resp.Code != http.StatusNotFound || !strings.Contains(resp.Body.String(), "404 page not found") {
			t.Fatalf("%s = %d %q", path, resp.Code, resp.Body.String())
		}
	}
}

func TestEdgeRoutesExistWhenTheSwitchIsOn(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	handler, _ := newAdminRouterTestServices(t)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptestNew(t, http.MethodGet, "/api/v1/bots/desktop-node"))
	if resp.Code == http.StatusNotFound || strings.Contains(resp.Body.String(), "404 page not found") {
		t.Fatalf("desktop-node = %d %q", resp.Code, resp.Body.String())
	}
}
