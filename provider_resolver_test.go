package main

import (
	"strings"
	"testing"
)

// helper to build a ModelConfig quickly
func mkModel(name, apiKey string) ModelConfig {
	return ModelConfig{ModelName: name, ApiKey: apiKey}
}

func TestProviderResolver_ExplicitValid(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	res, err := r.Resolve(cfg, "DeepSeek")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider.ModelName != "DeepSeek" {
		t.Errorf("expected DeepSeek, got %s", res.Provider.ModelName)
	}
	if res.Fallback {
		t.Error("should not be a fallback")
	}
}

func TestProviderResolver_ExplicitNotFound(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	_, err := r.Resolve(cfg, "NonExistent")
	if err == nil {
		t.Fatal("expected error for non-existent provider")
	}
	if !strings.Contains(err.Error(), "不存在") {
		t.Errorf("error should mention provider not found, got: %v", err)
	}
	// Should list available providers
	if !strings.Contains(err.Error(), "Original") || !strings.Contains(err.Error(), "DeepSeek") {
		t.Errorf("error should list available providers, got: %v", err)
	}
}

func TestProviderResolver_ExplicitNoApiKey(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", ""), // no API key
		},
	}
	r := &ProviderResolver{}
	_, err := r.Resolve(cfg, "DeepSeek")
	if err == nil {
		t.Fatal("expected error for provider without API key")
	}
	if !strings.Contains(err.Error(), "未配置 API Key") {
		t.Errorf("error should mention missing API key, got: %v", err)
	}
}

func TestProviderResolver_DefaultAvailable(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "DeepSeek",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	res, err := r.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider.ModelName != "DeepSeek" {
		t.Errorf("expected DeepSeek, got %s", res.Provider.ModelName)
	}
	if res.Fallback {
		t.Error("should not be a fallback when default is available")
	}
}

func TestProviderResolver_DefaultUnavailableFallback(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "BadProvider",
		Models: []ModelConfig{
			mkModel("BadProvider", ""), // no key, not "original"
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	res, err := r.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider.ModelName != "DeepSeek" {
		t.Errorf("expected DeepSeek fallback, got %s", res.Provider.ModelName)
	}
	if !res.Fallback {
		t.Error("should be marked as fallback")
	}
	if res.OriginalName != "BadProvider" {
		t.Errorf("OriginalName should be BadProvider, got %s", res.OriginalName)
	}
	if len(res.Tried) == 0 {
		t.Error("Tried list should not be empty")
	}
	if len(res.Errors) == 0 {
		t.Error("Errors list should not be empty")
	}
}

func TestProviderResolver_AllUnavailable(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "ProvA",
		Models: []ModelConfig{
			mkModel("ProvA", ""),
			mkModel("ProvB", ""),
		},
	}
	r := &ProviderResolver{}
	_, err := r.Resolve(cfg, "")
	if err == nil {
		t.Fatal("expected error when all providers unavailable")
	}
	if !strings.Contains(err.Error(), "所有服务商均不可用") {
		t.Errorf("error should mention all unavailable, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ProvA") || !strings.Contains(err.Error(), "ProvB") {
		t.Errorf("error should list all tried providers, got: %v", err)
	}
}

func TestProviderResolver_CaseInsensitive(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "deepseek",
		Models: []ModelConfig{
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}

	// Test explicit override with different case
	res, err := r.Resolve(cfg, "DEEPSEEK")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider.ModelName != "DeepSeek" {
		t.Errorf("expected DeepSeek, got %s", res.Provider.ModelName)
	}

	// Test default with different case
	res2, err := r.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Provider.ModelName != "DeepSeek" {
		t.Errorf("expected DeepSeek via default, got %s", res2.Provider.ModelName)
	}
}

func TestProviderResolver_EmptyModels(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "anything",
		Models:       []ModelConfig{},
	}
	r := &ProviderResolver{}
	_, err := r.Resolve(cfg, "")
	if err == nil {
		t.Fatal("expected error for empty Models list")
	}
	if !strings.Contains(err.Error(), "没有可用的服务商配置") {
		t.Errorf("error should mention no providers, got: %v", err)
	}
}

func TestProviderResolver_Idempotent(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "BadDefault",
		Models: []ModelConfig{
			mkModel("BadDefault", ""),
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}

	res1, err1 := r.Resolve(cfg, "")
	res2, err2 := r.Resolve(cfg, "")

	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("idempotency broken: err1=%v, err2=%v", err1, err2)
	}
	if res1.Provider.ModelName != res2.Provider.ModelName {
		t.Errorf("idempotency broken: provider1=%s, provider2=%s",
			res1.Provider.ModelName, res2.Provider.ModelName)
	}
	if res1.Fallback != res2.Fallback {
		t.Errorf("idempotency broken: fallback1=%v, fallback2=%v",
			res1.Fallback, res2.Fallback)
	}
	if res1.OriginalName != res2.OriginalName {
		t.Errorf("idempotency broken: original1=%s, original2=%s",
			res1.OriginalName, res2.OriginalName)
	}
}

func TestProviderResolver_EmptyCurrentModel(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	res, err := r.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "Original" is always valid (isValidProvider returns true for it),
	// so it should be picked first.
	if res.Provider.ModelName != "Original" {
		t.Errorf("expected Original, got %s", res.Provider.ModelName)
	}
	// No default was configured, so this shouldn't be marked as fallback.
	if res.Fallback {
		t.Error("should not be a fallback when CurrentModel is empty")
	}
}

func TestProviderResolver_CurrentModelNotInList(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "StaleProvider",
		Models: []ModelConfig{
			mkModel("Original", ""),
			mkModel("DeepSeek", "sk-deep"),
		},
	}
	r := &ProviderResolver{}
	res, err := r.Resolve(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default not found in list, should fallback to first valid.
	if res.Provider.ModelName != "Original" {
		t.Errorf("expected Original, got %s", res.Provider.ModelName)
	}
	if !res.Fallback {
		t.Error("should be marked as fallback when default is not in list")
	}
	if res.OriginalName != "StaleProvider" {
		t.Errorf("expected OriginalName=StaleProvider, got %s", res.OriginalName)
	}
}

func TestProviderResolver_ExplicitWhitespace(t *testing.T) {
	cfg := ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			mkModel("Original", ""),
		},
	}
	r := &ProviderResolver{}
	// Whitespace-only override should be treated as empty (use default).
	res, err := r.Resolve(cfg, "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Provider.ModelName != "Original" {
		t.Errorf("expected Original, got %s", res.Provider.ModelName)
	}
}
