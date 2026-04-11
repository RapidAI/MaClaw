package compute

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func tempFilePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "providers.json")
}

func TestNewLocalStoreEmptyFile(t *testing.T) {
	ls := NewLocalStore(tempFilePath(t))
	if got := ls.ListProviders(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d providers", len(got))
	}
}

func TestNewLocalStoreLoadsExisting(t *testing.T) {
	fp := tempFilePath(t)
	data := `{"providers":[{"id":"p1","name":"Test","protocol":"openai","base_url":"https://api.example.com"}]}`
	os.WriteFile(fp, []byte(data), 0644)

	ls := NewLocalStore(fp)
	got := ls.ListProviders()
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("expected 1 provider with id p1, got %+v", got)
	}
}

func TestSaveProviderCreate(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)

	p := ComputeProvider{Name: "New", Protocol: "openai", BaseURL: "https://api.example.com"}
	if err := ls.SaveProvider(p); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	got := ls.ListProviders()
	if len(got) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(got))
	}
	if got[0].ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	if got[0].Name != "New" {
		t.Fatalf("name = %q, want New", got[0].Name)
	}
	if got[0].CreatedAt == "" || got[0].UpdatedAt == "" {
		t.Fatal("timestamps should be set")
	}

	// Verify persisted to disk
	ls2 := NewLocalStore(fp)
	got2 := ls2.ListProviders()
	if len(got2) != 1 || got2[0].Name != "New" {
		t.Fatalf("persisted data mismatch: %+v", got2)
	}
}

func TestSaveProviderUpdate(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)

	p := ComputeProvider{ID: "p1", Name: "Original", Protocol: "openai", BaseURL: "https://api.example.com"}
	ls.SaveProvider(p)

	p.Name = "Updated"
	if err := ls.SaveProvider(p); err != nil {
		t.Fatalf("SaveProvider update: %v", err)
	}

	got := ls.ListProviders()
	if len(got) != 1 {
		t.Fatalf("expected 1 provider after update, got %d", len(got))
	}
	if got[0].Name != "Updated" {
		t.Fatalf("name = %q, want Updated", got[0].Name)
	}
}

func TestSaveProviderPreservesCreatedAt(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)

	p := ComputeProvider{ID: "p1", Name: "V1", Protocol: "openai", BaseURL: "https://a.com"}
	ls.SaveProvider(p)
	created := ls.GetProvider("p1").CreatedAt

	p.Name = "V2"
	ls.SaveProvider(p)
	got := ls.GetProvider("p1")
	if got.CreatedAt != created {
		t.Fatalf("CreatedAt changed from %q to %q", created, got.CreatedAt)
	}
}

func TestDeleteProvider(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)

	ls.SaveProvider(ComputeProvider{ID: "p1", Name: "A", Protocol: "openai", BaseURL: "https://a.com"})
	ls.SaveProvider(ComputeProvider{ID: "p2", Name: "B", Protocol: "openai", BaseURL: "https://b.com"})

	if err := ls.DeleteProvider("p1"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	got := ls.ListProviders()
	if len(got) != 1 || got[0].ID != "p2" {
		t.Fatalf("after delete: %+v", got)
	}

	// Verify persisted
	ls2 := NewLocalStore(fp)
	if len(ls2.ListProviders()) != 1 {
		t.Fatal("delete not persisted")
	}
}

func TestDeleteProviderNotFound(t *testing.T) {
	ls := NewLocalStore(tempFilePath(t))
	if err := ls.DeleteProvider("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent provider")
	}
}

func TestGetProvider(t *testing.T) {
	ls := NewLocalStore(tempFilePath(t))
	ls.SaveProvider(ComputeProvider{ID: "p1", Name: "A", Protocol: "openai", BaseURL: "https://a.com"})

	got := ls.GetProvider("p1")
	if got == nil || got.Name != "A" {
		t.Fatalf("GetProvider(p1) = %+v", got)
	}

	if ls.GetProvider("nonexistent") != nil {
		t.Fatal("GetProvider should return nil for nonexistent ID")
	}
}

func TestGetProviderReturnsCopy(t *testing.T) {
	ls := NewLocalStore(tempFilePath(t))
	ls.SaveProvider(ComputeProvider{ID: "p1", Name: "Original", Protocol: "openai", BaseURL: "https://a.com"})

	got := ls.GetProvider("p1")
	got.Name = "Mutated"

	internal := ls.GetProvider("p1")
	if internal.Name == "Mutated" {
		t.Fatal("GetProvider should return a copy")
	}
}

func TestListProvidersReturnsCopy(t *testing.T) {
	ls := NewLocalStore(tempFilePath(t))
	ls.SaveProvider(ComputeProvider{ID: "p1", Name: "Original", Protocol: "openai", BaseURL: "https://a.com"})

	got := ls.ListProviders()
	got[0].Name = "Mutated"

	internal := ls.ListProviders()
	if internal[0].Name == "Mutated" {
		t.Fatal("ListProviders should return a copy")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	fp := tempFilePath(t)
	os.WriteFile(fp, []byte(`{bad json`), 0644)

	ls := &LocalStore{filePath: fp}
	if err := ls.Load(); err == nil {
		t.Fatal("Load should return error for invalid JSON")
	}
}

func TestSaveProviderUserAgentAndComputeType(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)

	p := ComputeProvider{
		ID:          "p1",
		Name:        "Test",
		Protocol:    "anthropic",
		BaseURL:     "https://api.anthropic.com",
		UserAgent:   "claude-code/2.0.0",
		ComputeType: "coding",
	}
	ls.SaveProvider(p)

	// Reload from disk and verify extended fields
	ls2 := NewLocalStore(fp)
	got := ls2.GetProvider("p1")
	if got.UserAgent != "claude-code/2.0.0" {
		t.Fatalf("UserAgent = %q, want claude-code/2.0.0", got.UserAgent)
	}
	if got.ComputeType != "coding" {
		t.Fatalf("ComputeType = %q, want coding", got.ComputeType)
	}
}

func TestSaveProviderJSONFormat(t *testing.T) {
	fp := tempFilePath(t)
	ls := NewLocalStore(fp)
	ls.SaveProvider(ComputeProvider{ID: "p1", Name: "A", Protocol: "openai", BaseURL: "https://a.com"})

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var f localStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(f.Providers) != 1 {
		t.Fatalf("expected 1 provider in file, got %d", len(f.Providers))
	}
}

// ---------- TestComputeProvider (connectivity) ----------

func TestTestComputeProviderOpenAISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"gpt-4","choices":[{"message":{"content":"Hi"}}]}`))
	}))
	defer srv.Close()

	p := &ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "test-key",
		Protocol: "openai",
		Model:    "gpt-4",
	}
	result := TestComputeProvider(p)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Model != "gpt-4" {
		t.Fatalf("model = %q, want gpt-4", result.Model)
	}
	if result.Latency <= 0 {
		t.Fatal("latency should be positive")
	}
}

func TestTestComputeProviderAnthropicSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %q, want /messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"claude-3-haiku","content":[{"text":"Hi"}]}`))
	}))
	defer srv.Close()

	p := &ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "ant-key",
		Protocol: "anthropic",
	}
	result := TestComputeProvider(p)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}

func TestTestComputeProviderGeminiSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "gem-key" {
			t.Errorf("key = %q", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"modelVersion":"gemini-1.5","candidates":[{"content":{"parts":[{"text":"Hi"}]}}]}`))
	}))
	defer srv.Close()

	p := &ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "gem-key",
		Protocol: "gemini",
		Model:    "gemini-pro",
	}
	result := TestComputeProvider(p)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Model != "gemini-1.5" {
		t.Fatalf("model = %q, want gemini-1.5", result.Model)
	}
}

func TestTestComputeProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	p := &ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "bad-key",
		Protocol: "openai",
	}
	result := TestComputeProvider(p)
	if result.Success {
		t.Fatal("expected failure for 401 response")
	}
	if result.Error == "" {
		t.Fatal("error message should be set")
	}
}

func TestTestComputeProviderUnsupportedProtocol(t *testing.T) {
	p := &ComputeProvider{
		BaseURL:  "https://example.com",
		Protocol: "unknown",
	}
	result := TestComputeProvider(p)
	if result.Success {
		t.Fatal("expected failure for unsupported protocol")
	}
	if result.Error == "" {
		t.Fatal("error should mention unsupported protocol")
	}
}

func TestTestComputeProviderUnreachable(t *testing.T) {
	p := &ComputeProvider{
		BaseURL:  "http://127.0.0.1:1",
		APIKey:   "key",
		Protocol: "openai",
	}
	result := TestComputeProvider(p)
	if result.Success {
		t.Fatal("expected failure for unreachable server")
	}
	if result.Error == "" {
		t.Fatal("error should be set")
	}
}

func TestTestComputeProviderUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"gpt-4"}`))
	}))
	defer srv.Close()

	p := &ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "key",
		Protocol:  "openai",
		UserAgent: "openclaw",
	}
	TestComputeProvider(p)
	if gotUA != "openclaw" {
		t.Fatalf("User-Agent = %q, want openclaw", gotUA)
	}
}
