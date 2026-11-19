package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestToolFileOnTheSameNodeDoesNotCrossTheTunnel(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDirectBase(context.Background(), enrolled.NodeID, "http://10.9.9.9:18081"); err != nil {
		t.Fatal(err)
	}
	prev := edgeRegistry
	edgeRegistry = reg
	t.Cleanup(func() { edgeRegistry = prev })

	var hits []string
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "running", "output": "should-not-travel"})
	}))
	t.Cleanup(docker.Close)

	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	pool.Nodes = func(context.Context, string, string, string) *http.Client {
		return &http.Client{Transport: rewriteHost{base: docker.URL}}
	}
	server, err := pool.CreateServer(context.Background(), "tenant-a", desktoppool.Server{
		Name: "node", BaseURL: desktoppool.NodeBaseURL(enrolled.NodeID), AccessToken: "desk-key",
		NodeID: enrolled.NodeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", desktoppool.Assignment{Scope: desktoppool.ScopeGlobal, ServerID: server.ID}); err != nil {
		t.Fatal(err)
	}

	body := `{"run_id":"r","tool_call_id":"c","tenant":"tenant-a","user":"alice","action":"file_read","args":{"path":"/home/desktop/a.txt"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+enrolled.HubToken)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	PostDesktopToolHandler(pool).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		DirectURL string `json:"direct_url"`
		Output    string `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 || out.Output != "" || !strings.HasPrefix(out.DirectURL, "http://10.9.9.9:18081/v1/desktops/local/") {
		t.Fatalf("file woke %v out=%+v", hits, out)
	}

	shotReq := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/screenshot", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice"}`))
	shotReq.Header.Set("Authorization", "Bearer "+enrolled.HubToken)
	shotReq.Header.Set("Content-Type", "application/json")
	shotRec := httptest.NewRecorder()
	PostDesktopScreenshotHandler(pool).ServeHTTP(shotRec, shotReq)
	if shotRec.Code != http.StatusOK || !strings.Contains(shotRec.Body.String(), "/v1/desktops/local/") {
		t.Fatalf("screenshot %d %s", shotRec.Code, shotRec.Body.String())
	}
	woke := false
	for _, hit := range hits {
		if hit == "/v1/desktops/ensure" {
			woke = true
		}
		if hit == "/v1/desktops/file" || hit == "/v1/desktops/screenshot" {
			t.Fatalf("screenshot bytes crossed the tunnel: %v", hits)
		}
	}
	if !woke {
		t.Fatalf("screenshot did not wake the desktop: %v", hits)
	}

	skip := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(body))
	skip.Header.Set("Authorization", "Bearer "+enrolled.HubToken)
	skip.Header.Set("Content-Type", "application/json")
	skip.Header.Set("X-Maclaw-Skip-Local", "1")
	skip.ContentLength = int64(len(body))
	skipRec := httptest.NewRecorder()
	PostDesktopToolHandler(pool).ServeHTTP(skipRec, skip)
	if skipRec.Code != http.StatusOK || strings.Contains(skipRec.Body.String(), "direct_url") || !strings.Contains(skipRec.Body.String(), "should-not-travel") {
		t.Fatalf("skip local %d %s", skipRec.Code, skipRec.Body.String())
	}
	crossed := false
	for _, hit := range hits {
		if hit == "/v1/desktops/file" {
			crossed = true
		}
	}
	if !crossed {
		t.Fatalf("skipped shortcut did not use the tunnel: %v", hits)
	}

	before := len(hits)
	probeBody := `{"run_id":"r2","tool_call_id":"c2","tenant":"tenant-a","user":"alice","action":"file_write","args":{"path":"/home/desktop/a.txt","content":"not-sent-through-the-tunnel"}}`
	probe := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(probeBody))
	probe.Header.Set("Authorization", "Bearer "+enrolled.HubToken)
	probe.Header.Set("Content-Type", "application/json")
	probe.Header.Set("X-Maclaw-Local-Probe", "1")
	probe.ContentLength = int64(len(probeBody))
	probeRec := httptest.NewRecorder()
	PostDesktopToolHandler(pool).ServeHTTP(probeRec, probe)
	if probeRec.Code != http.StatusOK || !strings.Contains(probeRec.Body.String(), "/v1/desktops/local/") {
		t.Fatalf("probe %d %s", probeRec.Code, probeRec.Body.String())
	}
	for _, hit := range hits[before:] {
		if hit == "/v1/desktops/file" {
			t.Fatalf("probe wrote through the tunnel: %v", hits[before:])
		}
	}
}

func TestLocalProbeStopsWhenTheFileCannotStayLocal(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	t.Setenv("HUB_EDGE_NODES", "off")
	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	body := `{"tenant_id":"tenant-a","user_id":"alice","action":"write","path":"/home/desktop/a.txt","content":"keep-me"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/file", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer desktop-api-token")
	req.Header.Set("X-Maclaw-Local-Probe", "1")
	rec := httptest.NewRecorder()
	PostDesktopFileHandler(pool).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LOCAL_UNAVAILABLE") {
		t.Fatalf("file probe %d %s", rec.Code, rec.Body.String())
	}
	tool := `{"run_id":"r","tool_call_id":"c","tenant":"tenant-a","user":"alice","action":"file_write","args":{"path":"/home/desktop/a.txt","content":"keep-me"}}`
	toolReq := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(tool))
	toolReq.Header.Set("Authorization", "Bearer desktop-api-token")
	toolReq.Header.Set("X-Maclaw-Local-Probe", "1")
	toolRec := httptest.NewRecorder()
	PostDesktopToolHandler(pool).ServeHTTP(toolRec, toolReq)
	if toolRec.Code != http.StatusConflict || !strings.Contains(toolRec.Body.String(), "LOCAL_UNAVAILABLE") {
		t.Fatalf("tool probe %d %s", toolRec.Code, toolRec.Body.String())
	}
}

type rewriteHost struct{ base string }

func (h rewriteHost) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	base, err := http.NewRequest(http.MethodGet, h.base, nil)
	if err != nil {
		return nil, err
	}
	next.URL.Scheme = base.URL.Scheme
	next.URL.Host = base.URL.Host
	next.Host = base.URL.Host
	return http.DefaultTransport.RoundTrip(next)
}
