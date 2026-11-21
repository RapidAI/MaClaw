package im

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

type memSettings map[string]string

func (m memSettings) Set(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}
func (m memSettings) Get(_ context.Context, key string) (string, error) {
	return m[key], nil
}

func TestResolveSystemFreeHubLLMConfigUsesLocalProvider(t *testing.T) {
	settings := memSettings{}
	ctx := context.Background()
	providerReg := &LLMProviderRegistry{
		Enabled: true,
		Providers: []LLMProvider{{
			ID: "deepseek", Name: "DeepSeek",
			APIURL: "https://api.example/v1", APIKey: "sk-test", Model: "deepseek-chat",
		}},
	}
	if err := SaveLLMProviderRegistry(ctx, settings, providerReg); err != nil {
		t.Fatal(err)
	}
	svc := &llmservice.Registry{
		SystemDefaultServiceGroupID: llmservice.SystemFreeServiceGroupID,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: llmservice.SystemFreeServiceGroupID, Name: "SF", AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"deepseek"}}},
		}},
	}
	if err := llmservice.SaveRegistry(ctx, settings, svc); err != nil {
		t.Fatal(err)
	}

	r := &SystemLLMResolver{System: settings}
	cfg := r.ResolveHubLLMConfig(ctx)
	if cfg == nil || !cfg.Enabled {
		t.Fatalf("expected config, got %#v", cfg)
	}
	if cfg.APIURL != "https://api.example/v1" || cfg.APIKey != "sk-test" || cfg.Model != "deepseek-chat" {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestResolveSystemFreeHubLLMConfigSkipsBuiltinOnly(t *testing.T) {
	settings := memSettings{}
	ctx := context.Background()
	svc := &llmservice.Registry{}
	llmservice.EnsureSystemFreeServiceGroup(svc)
	if err := llmservice.SaveRegistry(ctx, settings, svc); err != nil {
		t.Fatal(err)
	}
	r := &SystemLLMResolver{System: settings}
	if cfg := r.ResolveHubLLMConfig(ctx); cfg != nil {
		t.Fatalf("builtin-only should not yield direct config, got %#v", cfg)
	}
	if !r.systemFreeUsesMaClaw(ctx) {
		t.Fatal("expected maclaw route")
	}
}

func TestDoSystemLLMUsesLocalProvider(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "pong"}}},
		})
	}))
	defer srv.Close()

	settings := memSettings{}
	ctx := context.Background()
	if err := SaveLLMProviderRegistry(ctx, settings, &LLMProviderRegistry{
		Enabled: true,
		Providers: []LLMProvider{{
			ID: "local", APIURL: srv.URL + "/v1", APIKey: "k", Model: "m", WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &llmservice.Registry{
		SystemDefaultServiceGroupID: llmservice.SystemFreeServiceGroupID,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: llmservice.SystemFreeServiceGroupID, AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"local"}}},
		}},
	}
	if err := llmservice.SaveRegistry(ctx, settings, svc); err != nil {
		t.Fatal(err)
	}

	resolver := &SystemLLMResolver{System: settings}
	SetSystemLLMResolver(resolver)
	t.Cleanup(func() { SetSystemLLMResolver(nil) })

	resp, err := DoSystemLLM(ctx, resolver, nil, []interface{}{
		map[string]string{"role": "user", "content": "hi"},
	}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("DoSystemLLM: %v", err)
	}
	if resp == nil || resp.Content != "pong" {
		t.Fatalf("resp = %#v", resp)
	}
	if gotPath == "" {
		t.Fatal("upstream not called")
	}
}

func TestCallStopsWhenContextCanceled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		// HTTP/1 does not always cancel the request context when the
		// client gives up, so the test releases the handler itself.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		srv.CloseClientConnections()
		srv.Close()
	}()

	settings := memSettings{}
	if err := SaveLLMProviderRegistry(context.Background(), settings, &LLMProviderRegistry{
		Enabled: true,
		Providers: []LLMProvider{{
			ID: "local", APIURL: srv.URL + "/v1", APIKey: "k", Model: "m", WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &llmservice.Registry{
		SystemDefaultServiceGroupID: llmservice.SystemFreeServiceGroupID,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: llmservice.SystemFreeServiceGroupID, AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"local"}}},
		}},
	}
	if err := llmservice.SaveRegistry(context.Background(), settings, svc); err != nil {
		t.Fatal(err)
	}

	resolver := &SystemLLMResolver{System: settings}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := resolver.Call(ctx, []interface{}{
			map[string]string{"role": "user", "content": "hi"},
		}, 20*time.Second)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("local system-free call did not reach the provider")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled system-free call kept running")
	}
}

func TestDoSystemLLMDoesNotFallBackWhenCallerCancels(t *testing.T) {
	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "fallback"}}},
		})
	}))
	defer fallback.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		primary.CloseClientConnections()
		primary.Close()
	}()

	settings := memSettings{}
	if err := SaveLLMProviderRegistry(context.Background(), settings, &LLMProviderRegistry{
		Enabled: true,
		Providers: []LLMProvider{{
			ID: "local", APIURL: primary.URL + "/v1", APIKey: "k", Model: "m", WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &llmservice.Registry{
		SystemDefaultServiceGroupID: llmservice.SystemFreeServiceGroupID,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID: llmservice.SystemFreeServiceGroupID, AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"local"}}},
		}},
	}
	if err := llmservice.SaveRegistry(context.Background(), settings, svc); err != nil {
		t.Fatal(err)
	}

	resolver := &SystemLLMResolver{System: settings}
	cfg := &HubLLMConfig{Enabled: true, APIURL: fallback.URL + "/v1", APIKey: "k", Model: "m"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := DoSystemLLM(ctx, resolver, cfg, []interface{}{
			map[string]string{"role": "user", "content": "hi"},
		}, nil, 20*time.Second)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("local system-free call did not reach the provider")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled system-free call kept running")
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback calls = %d", fallbackHits.Load())
	}
}

func TestConfigProviderFallsBackToLegacy(t *testing.T) {
	settings := memSettings{}
	r := &SystemLLMResolver{System: settings}
	legacy := r.ConfigProvider(func(context.Context) *HubLLMConfig {
		return &HubLLMConfig{Enabled: true, APIURL: "https://legacy", APIKey: "k", Model: "m"}
	})
	cfg := legacy(context.Background())
	if cfg == nil || cfg.APIURL != "https://legacy" {
		t.Fatalf("cfg = %#v", cfg)
	}
}
