package skill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// SearchMCPClawHub tests
// ────────────────────────────────────────────────────────────────────────────

func TestSearchMCPClawHub_ParsesResponse(t *testing.T) {
	// Mock ClawHub API response
	mockResp := clawHubMCPSearchResponse{
		Skills: []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Version     string `json:"version"`
			Author      string `json:"author"`
		}{
			{
				ID:          "mcp-postgres",
				Name:        "PostgreSQL MCP Server",
				Description: "MCP server for PostgreSQL databases",
				Version:     "1.2.0",
				Author:      "dbtools",
			},
			{
				ID:          "mcp-redis",
				Name:        "Redis MCP Server",
				Description: "MCP server for Redis cache",
				Version:     "0.9.1",
				Author:      "cachedev",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request parameters
		if !strings.Contains(r.URL.Path, "/api/v1/skills/search") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query().Get("q")
		if q != "database" {
			t.Errorf("expected query 'database', got %q", q)
		}
		typ := r.URL.Query().Get("type")
		if typ != "mcp" {
			t.Errorf("expected type=mcp filter, got %q", typ)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer srv.Close()

	// Create client pointing to mock server
	client := &HubClient{
		httpClient: srv.Client(),
		userAgent:  "test/1.0",
	}

	// Temporarily override ClawHubMirrorURL by using a custom request
	// Since ClawHubMirrorURL is a const, we use the roundTripFunc pattern
	client.httpClient = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		// Forward to test server
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "database")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify first result
	r := results[0]
	if r.ID != "mcp-postgres" {
		t.Errorf("result[0].ID = %q, want %q", r.ID, "mcp-postgres")
	}
	if r.Name != "PostgreSQL MCP Server" {
		t.Errorf("result[0].Name = %q, want %q", r.Name, "PostgreSQL MCP Server")
	}
	if r.Description != "MCP server for PostgreSQL databases" {
		t.Errorf("result[0].Description = %q, want %q", r.Description, "MCP server for PostgreSQL databases")
	}
	if r.Version != "1.2.0" {
		t.Errorf("result[0].Version = %q, want %q", r.Version, "1.2.0")
	}
	if r.Author != "dbtools" {
		t.Errorf("result[0].Author = %q, want %q", r.Author, "dbtools")
	}
	if r.Source != "clawhub" {
		t.Errorf("result[0].Source = %q, want %q", r.Source, "clawhub")
	}
	if r.CapabilityType != "mcp" {
		t.Errorf("result[0].CapabilityType = %q, want %q", r.CapabilityType, "mcp")
	}
	if r.InstallRef != "mcp-postgres" {
		t.Errorf("result[0].InstallRef = %q, want %q", r.InstallRef, "mcp-postgres")
	}

	// Verify second result
	r2 := results[1]
	if r2.ID != "mcp-redis" {
		t.Errorf("result[1].ID = %q, want %q", r2.ID, "mcp-redis")
	}
	if r2.CapabilityType != "mcp" {
		t.Errorf("result[1].CapabilityType = %q, want %q", r2.CapabilityType, "mcp")
	}
}

func TestSearchMCPClawHub_EmptyQuery(t *testing.T) {
	client := NewHubClient()
	ctx := context.Background()

	results := client.SearchMCPClawHub(ctx, "")
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}

	results = client.SearchMCPClawHub(ctx, "   ")
	if results != nil {
		t.Fatalf("expected nil for whitespace query, got %v", results)
	}
}

func TestSearchMCPClawHub_APIError(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(500, []byte("internal server error"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "database")

	if results != nil {
		t.Fatalf("expected nil on API error, got %v", results)
	}
}

func TestSearchMCPClawHub_EmptyResults(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			body, _ := json.Marshal(clawHubMCPSearchResponse{Skills: nil})
			return newHTTPResponse(200, body, map[string]string{"Content-Type": "application/json"}), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "nonexistent")

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMCPClawHub_InvalidJSON(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(200, []byte("not json"), map[string]string{"Content-Type": "application/json"}), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "database")

	if results != nil {
		t.Fatalf("expected nil on invalid JSON, got %v", results)
	}
}

func TestSearchMCPClawHub_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := &HubClient{
		httpClient: &http.Client{},
		userAgent:  "test/1.0",
	}

	results := client.SearchMCPClawHub(ctx, "database")
	if results != nil {
		t.Fatalf("expected nil on cancelled context, got %v", results)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SearchMCPGitHub tests
// ────────────────────────────────────────────────────────────────────────────

func TestSearchMCPGitHub_ParsesResponse(t *testing.T) {
	// Build mock response as raw JSON to avoid struct literal tag issues
	mockRespJSON := `{
		"items": [
			{
				"name": "mcp-server-sqlite",
				"full_name": "anthropics/mcp-server-sqlite",
				"description": "SQLite MCP server implementation",
				"html_url": "https://github.com/anthropics/mcp-server-sqlite",
				"owner": {"login": "anthropics"}
			},
			{
				"name": "mcp-server-filesystem",
				"full_name": "modelcontextprotocol/mcp-server-filesystem",
				"description": "Filesystem access MCP server",
				"html_url": "https://github.com/modelcontextprotocol/mcp-server-filesystem",
				"owner": {"login": "modelcontextprotocol"}
			}
		]
	}`

	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			// Verify GitHub API request
			if !strings.Contains(req.URL.Host, "api.github.com") {
				t.Errorf("unexpected host: %s", req.URL.Host)
			}
			if !strings.Contains(req.URL.Path, "/search/repositories") {
				t.Errorf("unexpected path: %s", req.URL.Path)
			}
			q := req.URL.Query().Get("q")
			if !strings.Contains(q, "topic:mcp-server") {
				t.Errorf("expected topic:mcp-server in query, got %q", q)
			}
			if !strings.Contains(q, "sqlite") {
				t.Errorf("expected search term 'sqlite' in query, got %q", q)
			}
			// Verify Accept header
			if req.Header.Get("Accept") != "application/vnd.github.v3+json" {
				t.Errorf("unexpected Accept header: %s", req.Header.Get("Accept"))
			}

			return newHTTPResponse(200, []byte(mockRespJSON), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPGitHub(ctx, "sqlite")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify first result
	r := results[0]
	if r.ID != "anthropics/mcp-server-sqlite" {
		t.Errorf("result[0].ID = %q, want %q", r.ID, "anthropics/mcp-server-sqlite")
	}
	if r.Name != "mcp-server-sqlite" {
		t.Errorf("result[0].Name = %q, want %q", r.Name, "mcp-server-sqlite")
	}
	if r.Description != "SQLite MCP server implementation" {
		t.Errorf("result[0].Description = %q, want %q", r.Description, "SQLite MCP server implementation")
	}
	if r.Author != "anthropics" {
		t.Errorf("result[0].Author = %q, want %q", r.Author, "anthropics")
	}
	if r.Source != "github" {
		t.Errorf("result[0].Source = %q, want %q", r.Source, "github")
	}
	if r.CapabilityType != "mcp" {
		t.Errorf("result[0].CapabilityType = %q, want %q", r.CapabilityType, "mcp")
	}
	if r.RepoURL != "https://github.com/anthropics/mcp-server-sqlite" {
		t.Errorf("result[0].RepoURL = %q, want %q", r.RepoURL, "https://github.com/anthropics/mcp-server-sqlite")
	}
	if r.InstallRef != "anthropics/mcp-server-sqlite" {
		t.Errorf("result[0].InstallRef = %q, want %q", r.InstallRef, "anthropics/mcp-server-sqlite")
	}

	// Verify second result
	r2 := results[1]
	if r2.Author != "modelcontextprotocol" {
		t.Errorf("result[1].Author = %q, want %q", r2.Author, "modelcontextprotocol")
	}
	if r2.CapabilityType != "mcp" {
		t.Errorf("result[1].CapabilityType = %q, want %q", r2.CapabilityType, "mcp")
	}
}

func TestSearchMCPGitHub_WithToken(t *testing.T) {
	var gotAuthHeader string
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotAuthHeader = req.Header.Get("Authorization")
			body, _ := json.Marshal(ghMCPRepoSearchResponse{})
			return newHTTPResponse(200, body, nil), nil
		}),
		userAgent:   "test/1.0",
		githubToken: "ghp_test_token_123",
	}

	ctx := context.Background()
	client.SearchMCPGitHub(ctx, "test")

	if gotAuthHeader != "token ghp_test_token_123" {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, "token ghp_test_token_123")
	}
}

func TestSearchMCPGitHub_WithoutToken(t *testing.T) {
	var gotAuthHeader string
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			gotAuthHeader = req.Header.Get("Authorization")
			body, _ := json.Marshal(ghMCPRepoSearchResponse{})
			return newHTTPResponse(200, body, nil), nil
		}),
		userAgent:   "test/1.0",
		githubToken: "", // no token
	}

	ctx := context.Background()
	client.SearchMCPGitHub(ctx, "test")

	if gotAuthHeader != "" {
		t.Errorf("Authorization header should be empty without token, got %q", gotAuthHeader)
	}
}

func TestSearchMCPGitHub_EmptyQuery(t *testing.T) {
	client := NewHubClient()
	ctx := context.Background()

	results := client.SearchMCPGitHub(ctx, "")
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}

	results = client.SearchMCPGitHub(ctx, "   ")
	if results != nil {
		t.Fatalf("expected nil for whitespace query, got %v", results)
	}
}

func TestSearchMCPGitHub_APIError(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(403, []byte("rate limited"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPGitHub(ctx, "database")

	if results != nil {
		t.Fatalf("expected nil on API error, got %v", results)
	}
}

func TestSearchMCPGitHub_EmptyResults(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			body, _ := json.Marshal(ghMCPRepoSearchResponse{Items: nil})
			return newHTTPResponse(200, body, nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPGitHub(ctx, "nonexistent")

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMCPGitHub_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &HubClient{
		httpClient: &http.Client{},
		userAgent:  "test/1.0",
	}

	results := client.SearchMCPGitHub(ctx, "database")
	if results != nil {
		t.Fatalf("expected nil on cancelled context, got %v", results)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SearchMCPFiltered tests
// ────────────────────────────────────────────────────────────────────────────

func TestSearchMCPFiltered_AllSources(t *testing.T) {
	clawHubJSON := `{"skills":[{"id":"mcp-1","name":"ClawHub MCP","description":"from clawhub","version":"1.0","author":"a"}]}`
	ghJSON := `{"items":[{"name":"gh-mcp","full_name":"org/gh-mcp","description":"from github","html_url":"https://github.com/org/gh-mcp","owner":{"login":"org"}}]}`

	callCount := map[string]int{}
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "clawhub") || strings.Contains(req.URL.String(), "cn.clawhub") {
				callCount["clawhub"]++
				return newHTTPResponse(200, []byte(clawHubJSON), nil), nil
			}
			if strings.Contains(req.URL.Host, "api.github.com") {
				callCount["github"]++
				return newHTTPResponse(200, []byte(ghJSON), nil), nil
			}
			return newHTTPResponse(404, nil, nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPFiltered(ctx, "test", nil) // nil = all sources

	if callCount["clawhub"] != 1 {
		t.Errorf("expected 1 clawhub call, got %d", callCount["clawhub"])
	}
	if callCount["github"] != 1 {
		t.Errorf("expected 1 github call, got %d", callCount["github"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 clawhub + 1 github), got %d", len(results))
	}

	// All results must have capability_type = "mcp"
	for i, r := range results {
		if r.CapabilityType != "mcp" {
			t.Errorf("result[%d].CapabilityType = %q, want %q", i, r.CapabilityType, "mcp")
		}
	}
}

func TestSearchMCPFiltered_OnlyClawHub(t *testing.T) {
	callCount := map[string]int{}
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "clawhub") || strings.Contains(req.URL.String(), "cn.clawhub") {
				callCount["clawhub"]++
				resp := clawHubMCPSearchResponse{}
				body, _ := json.Marshal(resp)
				return newHTTPResponse(200, body, nil), nil
			}
			callCount["github"]++
			return newHTTPResponse(200, []byte("{}"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	client.SearchMCPFiltered(ctx, "test", []string{"clawhub"})

	if callCount["clawhub"] != 1 {
		t.Errorf("expected 1 clawhub call, got %d", callCount["clawhub"])
	}
	if callCount["github"] != 0 {
		t.Errorf("expected 0 github calls when filtered to clawhub only, got %d", callCount["github"])
	}
}

func TestSearchMCPFiltered_EmptyQuery(t *testing.T) {
	client := NewHubClient()
	ctx := context.Background()

	results := client.SearchMCPFiltered(ctx, "", nil)
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// CapabilityType field verification
// ────────────────────────────────────────────────────────────────────────────

func TestSearchMCP_AllResultsHaveCapabilityTypeMCP(t *testing.T) {
	clawHubJSON := `{"skills":[
		{"id":"mcp-a","name":"A","description":"desc-a","version":"1.0","author":"auth-a"},
		{"id":"mcp-b","name":"B","description":"desc-b","version":"2.0","author":"auth-b"},
		{"id":"mcp-c","name":"C","description":"desc-c","version":"3.0","author":"auth-c"}
	]}`

	ghJSON := `{"items":[
		{"name":"gh-1","full_name":"org/gh-1","description":"gh desc 1","html_url":"https://github.com/org/gh-1","owner":{"login":"org"}},
		{"name":"gh-2","full_name":"org/gh-2","description":"gh desc 2","html_url":"https://github.com/org/gh-2","owner":{"login":"org"}}
	]}`

	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "clawhub") || strings.Contains(req.URL.String(), "cn.clawhub") {
				return newHTTPResponse(200, []byte(clawHubJSON), nil), nil
			}
			if strings.Contains(req.URL.Host, "api.github.com") {
				return newHTTPResponse(200, []byte(ghJSON), nil), nil
			}
			return newHTTPResponse(404, nil, nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()

	// Test ClawHub results
	clawResults := client.SearchMCPClawHub(ctx, "mcp")
	for i, r := range clawResults {
		if r.CapabilityType != "mcp" {
			t.Errorf("ClawHub result[%d].CapabilityType = %q, want %q", i, r.CapabilityType, "mcp")
		}
	}

	// Test GitHub results
	ghResults := client.SearchMCPGitHub(ctx, "mcp")
	for i, r := range ghResults {
		if r.CapabilityType != "mcp" {
			t.Errorf("GitHub result[%d].CapabilityType = %q, want %q", i, r.CapabilityType, "mcp")
		}
	}

	// Test combined via SearchMCPFiltered
	allResults := client.SearchMCPFiltered(ctx, "mcp", nil)
	for i, r := range allResults {
		if r.CapabilityType != "mcp" {
			t.Errorf("SearchMCPFiltered result[%d].CapabilityType = %q, want %q (source=%s)", i, r.CapabilityType, "mcp", r.Source)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Error handling edge cases
// ────────────────────────────────────────────────────────────────────────────

func TestSearchMCPClawHub_HTTP404(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(404, []byte("not found"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "test")
	if results != nil {
		t.Fatalf("expected nil on 404, got %v", results)
	}
}

func TestSearchMCPClawHub_HTTP429RateLimit(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(429, []byte("too many requests"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPClawHub(ctx, "test")
	if results != nil {
		t.Fatalf("expected nil on 429, got %v", results)
	}
}

func TestSearchMCPGitHub_HTTP401Unauthorized(t *testing.T) {
	client := &HubClient{
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return newHTTPResponse(401, []byte("unauthorized"), nil), nil
		}),
		userAgent: "test/1.0",
	}

	ctx := context.Background()
	results := client.SearchMCPGitHub(ctx, "test")
	if results != nil {
		t.Fatalf("expected nil on 401, got %v", results)
	}
}

func TestSearchMCPGitHub_Timeout(t *testing.T) {
	client := &HubClient{
		httpClient: &http.Client{Timeout: 1 * time.Millisecond},
		userAgent:  "test/1.0",
	}

	// Use a server that delays response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Override to point to slow server
	client.httpClient = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		ctx, cancel := context.WithTimeout(req.Context(), 1*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)
		return http.DefaultTransport.RoundTrip(req)
	})

	ctx := context.Background()
	results := client.SearchMCPGitHub(ctx, "test")
	if results != nil {
		t.Fatalf("expected nil on timeout, got %v", results)
	}
}
