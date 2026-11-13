package botmgmt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withTestModelFetchClient points the model fetcher at a fake provider and
// restores the real client after the test, so a parallel test never inherits
// the override.
func withTestModelFetchClient(t *testing.T, client *http.Client) {
	t.Helper()
	previous := llmModelFetchClient
	llmModelFetchClient = client
	t.Cleanup(func() { llmModelFetchClient = previous })
}

// openAIModelsFake is a provider that answers /v1/models but not /models,
// which is the shape an administrator gets when the base URL omits the
// version segment. It records the Authorization header it saw.
func openAIModelsFake(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server, &authHeaders
}

func TestFetchLLMProviderModelsOpenAIFallsBackToVersionedEndpoint(t *testing.T) {
	server, authHeaders := openAIModelsFake(t, http.StatusOK,
		`{"object":"list","data":[{"id":"b-model"},{"id":"a-model"},{"id":"a-model"}]}`)
	withTestModelFetchClient(t, server.Client())

	svc := NewService(&memSettings{})
	items, err := svc.FetchLLMProviderModels(context.Background(), "tenant-a", LLMModelsInput{
		Protocol: "openai",
		URL:      server.URL,
		Key:      "sk-test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "a-model" || items[1].ID != "b-model" {
		t.Fatalf("items = %#v", items)
	}
	if len(*authHeaders) != 1 || (*authHeaders)[0] != "Bearer sk-test-key" {
		t.Fatalf("auth headers = %#v", authHeaders)
	}
}

func TestFetchLLMProviderModelsReusesTheStoredKey(t *testing.T) {
	server, authHeaders := openAIModelsFake(t, http.StatusOK,
		`{"data":[{"id":"gpt-4o","name":"GPT-4o"}]}`)
	withTestModelFetchClient(t, server.Client())

	svc := NewService(&memSettings{})
	saved, err := svc.SaveLLMSettings(context.Background(), "tenant-a", "", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "OpenAI", Protocol: "openai",
		URL: server.URL, Key: "sk-stored-key", Model: "gpt-4o",
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.FetchLLMProviderModels(context.Background(), "tenant-a", LLMModelsInput{
		ProviderID: saved.LLM.Providers[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "gpt-4o" || items[0].Name != "GPT-4o" {
		t.Fatalf("items = %#v", items)
	}
	if len(*authHeaders) != 1 || (*authHeaders)[0] != "Bearer sk-stored-key" {
		t.Fatalf("stored key was not used: %#v", authHeaders)
	}
}

func TestFetchLLMProviderModelsKeysOffStoredTenant(t *testing.T) {
	server, _ := openAIModelsFake(t, http.StatusOK, `{"data":[{"id":"m1"}]}`)
	withTestModelFetchClient(t, server.Client())

	svc := NewService(&memSettings{})
	if _, err := svc.SaveLLMSettings(context.Background(), "tenant-a", "", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "OpenAI", Protocol: "openai",
		URL: server.URL, Key: "sk-a", Model: "m",
	}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// A second tenant's provider ids are invisible: provider resolution is
	// tenant-scoped, so the request cannot ride another tenant's key.
	_, err := svc.FetchLLMProviderModels(context.Background(), "tenant-b", LLMModelsInput{
		ProviderID: "lp_abcdef0123456789",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-tenant provider lookup err = %v", err)
	}
}

func TestFetchLLMProviderModelsRejectsABadKey(t *testing.T) {
	server, _ := openAIModelsFake(t, http.StatusUnauthorized, `{"error":"bad key"}`)
	withTestModelFetchClient(t, server.Client())

	svc := NewService(&memSettings{})
	_, err := svc.FetchLLMProviderModels(context.Background(), "tenant-a", LLMModelsInput{
		Protocol: "openai", URL: server.URL, Key: "sk-wrong",
	})
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "llm provider key is invalid") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchLLMProviderModelsValidatesInput(t *testing.T) {
	svc := NewService(&memSettings{})
	cases := []LLMModelsInput{
		{ProviderID: "lp_missing00000001"},
		{Protocol: "azure", URL: "https://llm.example/v1", Key: "sk-1"},
		{Protocol: "openai", URL: "ftp://llm.example", Key: "sk-1"},
		{Protocol: "openai", URL: "https://llm.example/v1"},
		{Protocol: "openai", URL: "https://llm.example/v1", Key: "bad\nkey"},
	}
	for i, in := range cases {
		if _, err := svc.FetchLLMProviderModels(context.Background(), "tenant-a", in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: err = %v", i, err)
		}
	}
}

func TestFetchLLMProviderModelsAnthropicUsesSDKHeaders(t *testing.T) {
	var gotPath, gotKey, gotVersion, gotLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			gotPath = r.URL.Path
			gotKey = r.Header.Get("x-api-key")
			gotVersion = r.Header.Get("anthropic-version")
			gotLimit = r.URL.Query().Get("limit")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-x","display_name":"Claude X"}],"has_more":false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	withTestModelFetchClient(t, server.Client())

	svc := NewService(&memSettings{})
	items, err := svc.FetchLLMProviderModels(context.Background(), "tenant-a", LLMModelsInput{
		Protocol: "anthropic",
		URL:      server.URL + "/v1",
		Key:      "sk-ant-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "claude-x" || items[0].Name != "Claude X" {
		t.Fatalf("items = %#v", items)
	}
	if !strings.HasSuffix(gotPath, "/v1/models") {
		t.Fatalf("path = %s", gotPath)
	}
	if gotKey != "sk-ant-test" {
		t.Fatalf("x-api-key = %q", gotKey)
	}
	if gotVersion == "" {
		t.Fatal("anthropic-version header missing")
	}
	if gotLimit != "1000" {
		t.Fatalf("limit = %q, want 1000 so the catalog is not truncated at 20", gotLimit)
	}
}

func TestParseLLMModelListBodyShapes(t *testing.T) {
	cases := []struct {
		body string
		want []LLMModelItem
	}{
		{`{"data":[{"id":"m1"}]}`, []LLMModelItem{{ID: "m1", Name: "m1"}}},
		{`{"models":[{"id":"m1","display_name":"M one"}]}`, []LLMModelItem{{ID: "m1", Name: "M one"}}},
		{`[{"id":"m1"},{"id":"m2","name":"M two"}]`, []LLMModelItem{{ID: "m1", Name: "m1"}, {ID: "m2", Name: "M two"}}},
		{`<html>blocked</html>`, nil},
		{`{"data":[{"object":"model"}]}`, nil},
		{``, nil},
	}
	for i, item := range cases {
		got := parseLLMModelListBody([]byte(item.body))
		if len(got) != len(item.want) {
			t.Fatalf("case %d: got %#v want %#v", i, got, item.want)
		}
		for j := range item.want {
			if got[j] != item.want[j] {
				t.Fatalf("case %d item %d: got %#v want %#v", i, j, got[j], item.want[j])
			}
		}
	}
}

func TestFetchLLMModelListTimesOutWithoutTheProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := fetchLLMModelList(ctx, "openai", server.URL, "sk-1"); err == nil {
		t.Fatal("expected a timeout error")
	}
}
