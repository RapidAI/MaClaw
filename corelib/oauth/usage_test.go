package oauth

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestQueryUsageFrom_Success(t *testing.T) {
	resp := UsageInfo{
		TotalGranted:   50.0,
		TotalUsed:      12.34,
		TotalAvailable: 37.66,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("User-Agent") != "MaClaw" {
			t.Errorf("unexpected user-agent: %s", r.Header.Get("User-Agent"))
		}
		if r.URL.Query().Get("after") != "" {
			t.Errorf("costs API should paginate with page, got after=%q", r.URL.Query().Get("after"))
		}
		if r.URL.Query().Get("limit") != "32" {
			t.Errorf("limit = %q, want 32", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	info, err := QueryUsageFrom(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(info.TotalGranted-50.0) > 0.001 {
		t.Errorf("TotalGranted = %f, want 50.0", info.TotalGranted)
	}
	if math.Abs(info.TotalUsed-12.34) > 0.001 {
		t.Errorf("TotalUsed = %f, want 12.34", info.TotalUsed)
	}
	if math.Abs(info.TotalAvailable-37.66) > 0.001 {
		t.Errorf("TotalAvailable = %f, want 37.66", info.TotalAvailable)
	}
}

func TestQueryUsageFrom_OfficialNestedCosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"object": "page",
			"data": [{
				"object": "bucket",
				"results": [
					{"object": "organization.costs.result", "amount": {"value": "0.06", "currency": "usd"}},
					{"object": "organization.costs.result", "amount": {"value": 1.94, "currency": "usd"}}
				]
			}],
			"has_more": false,
			"next_page": null
		}`))
	}))
	defer srv.Close()

	info, err := QueryUsageFrom(srv.URL, "sk-admin-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(info.TotalUsed-2.0) > 0.001 {
		t.Fatalf("TotalUsed = %f, want 2.0", info.TotalUsed)
	}
	if info.TotalGranted != 0 || info.TotalAvailable != 0 {
		t.Fatalf("granted/available should stay unknown, got %+v", info)
	}
}

func TestQueryUsageFrom_PaginatesWithPageParam(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "" {
			w.Write([]byte(`{"object":"page","has_more":true,"next_page":"page_abc","data":[{"object":"bucket","results":[{"amount":{"value":1}}]}]}`))
			return
		}
		w.Write([]byte(`{"object":"page","has_more":false,"next_page":null,"data":[{"object":"bucket","results":[{"amount":{"value":2}}]}]}`))
	}))
	defer srv.Close()

	info, err := QueryUsageFrom(srv.URL, "sk-admin-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pages) != 2 || pages[0] != "" || pages[1] != "page_abc" {
		t.Fatalf("pages = %#v, want [\"\", \"page_abc\"]", pages)
	}
	if math.Abs(info.TotalUsed-3) > 0.001 {
		t.Fatalf("TotalUsed = %f, want 3", info.TotalUsed)
	}
}

func TestQueryUsageFrom_EmptyOfficialPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"page","data":[],"has_more":false,"next_page":null}`))
	}))
	defer srv.Close()

	info, err := QueryUsageFrom(srv.URL, "sk-admin-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.TotalUsed != 0 || info.TotalGranted != 0 {
		t.Fatalf("empty month should be zero, got %+v", info)
	}
}

func TestQueryUsageFrom_HTTPErrorIncludesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"You have insufficient permissions for this operation. Missing scopes: api.usage.read.","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	_, err := QueryUsageFrom(srv.URL, "bad-token")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error %q missing HTTP 401", err)
	}
	if !strings.Contains(err.Error(), "api.usage.read") {
		t.Fatalf("error %q missing API message", err)
	}
	if strings.Contains(err.Error(), "body_len=") {
		t.Fatalf("error %q still uses opaque body_len", err)
	}
}

func TestQueryUsageFrom_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := QueryUsageFrom(srv.URL, "token")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLooksLikeOpenAIAdminKey(t *testing.T) {
	if !LooksLikeOpenAIAdminKey("sk-admin-abc") {
		t.Fatal("expected admin key")
	}
	if LooksLikeOpenAIAdminKey("sk-proj-abc") || LooksLikeOpenAIAdminKey("sk-abc") || LooksLikeOpenAIAdminKey("oauth-jwt") {
		t.Fatal("project/oauth tokens must not look like admin keys")
	}
}

func TestOrganizationCostsUnsupportedReason(t *testing.T) {
	tests := []struct {
		name     string
		provider corelib.MaclawLLMProvider
		wantSub  string
	}{
		{
			name: "xai oauth",
			provider: corelib.MaclawLLMProvider{
				Name: "xAI-Grok", URL: "https://api.x.ai/v1", AuthType: "oauth", Key: "xai-token",
			},
			wantSub: "OAuth",
		},
		{
			name: "openai codex oauth",
			provider: corelib.MaclawLLMProvider{
				Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex", AuthType: "oauth", Key: "chatgpt-jwt",
			},
			wantSub: "ChatGPT/Codex",
		},
		{
			name: "openai project key",
			provider: corelib.MaclawLLMProvider{
				Name: "OpenAI Official", URL: "https://api.openai.com/v1", Key: "sk-proj-abc",
			},
			wantSub: "Admin API Key",
		},
		{
			name: "deepseek with admin-looking key",
			provider: corelib.MaclawLLMProvider{
				Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-admin-wrong-host",
			},
			wantSub: "不支持",
		},
		{
			name: "openai admin key",
			provider: corelib.MaclawLLMProvider{
				Name: "OpenAI Official", URL: "https://api.openai.com/v1", Key: "sk-admin-real",
			},
			wantSub: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OrganizationCostsUnsupportedReason(tt.provider)
			if tt.wantSub == "" {
				if got != "" {
					t.Fatalf("got %q, want eligible", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Fatalf("got %q, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestQueryOrganizationCostsRejectsOAuthWithoutHTTP(t *testing.T) {
	_, err := QueryOrganizationCosts(corelib.MaclawLLMProvider{
		Name: "xAI-Grok", URL: "https://api.x.ai/v1", AuthType: "oauth", Key: "xai-token",
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "OAuth") {
		t.Fatalf("got %q", err)
	}
}
