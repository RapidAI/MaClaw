package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestIsValidProvider(t *testing.T) {
	tests := []struct {
		name string
		m    corelib.ModelConfig
		want bool
	}{
		{
			name: "IsBuiltin is valid",
			m:    corelib.ModelConfig{ModelName: "Original", IsBuiltin: true},
			want: true,
		},
		{
			name: "HasSubscription is valid",
			m:    corelib.ModelConfig{ModelName: "SubProvider", HasSubscription: true},
			want: true,
		},
		{
			name: "has ApiKey is valid",
			m:    corelib.ModelConfig{ModelName: "DeepSeek", ApiKey: "sk-abc123"},
			want: true,
		},
		{
			name: "empty ApiKey non-builtin is invalid",
			m:    corelib.ModelConfig{ModelName: "DeepSeek", ApiKey: ""},
			want: false,
		},
		{
			name: "whitespace-only ApiKey non-builtin is invalid",
			m:    corelib.ModelConfig{ModelName: "DeepSeek", ApiKey: "   "},
			want: false,
		},
		{
			name: "empty ModelName with no ApiKey is invalid",
			m:    corelib.ModelConfig{ModelName: "", ApiKey: ""},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidProvider(tt.m)
			if got != tt.want {
				t.Errorf("isValidProvider(%+v) = %v, want %v", tt.m, got, tt.want)
			}
		})
	}
}

func TestRemoteToolConfigReturnsErrorWhenConfigSelectorMissing(t *testing.T) {
	const toolName = "broken-config-selector"
	original, existed := remoteToolCatalog[toolName]
	remoteToolCatalog[toolName] = RemoteToolMetadata{
		Name:        toolName,
		DisplayName: "Broken Tool",
		BinaryName:  toolName,
	}
	defer func() {
		if existed {
			remoteToolCatalog[toolName] = original
		} else {
			delete(remoteToolCatalog, toolName)
		}
	}()

	_, err := remoteToolConfig(corelib.AppConfig{}, toolName)
	if err == nil {
		t.Fatal("expected error when ConfigSelector is missing")
	}
	if !strings.Contains(err.Error(), "tool config unavailable") {
		t.Fatalf("error = %q, want tool config unavailable", err)
	}
}

func TestRemoteToolConfigReturnsBuiltinConfig(t *testing.T) {
	cfg := corelib.AppConfig{
		Claude: corelib.ToolConfig{CurrentModel: "DeepSeek"},
	}

	got, err := remoteToolConfig(cfg, "claude")
	if err != nil {
		t.Fatalf("remoteToolConfig() error = %v", err)
	}
	if got.CurrentModel != "DeepSeek" {
		t.Fatalf("CurrentModel = %q, want %q", got.CurrentModel, "DeepSeek")
	}
}

func TestValidProviders(t *testing.T) {
	tc := corelib.ToolConfig{
		CurrentModel: "Original",
		Models: []corelib.ModelConfig{
			{ModelName: "Original", IsBuiltin: true},
			{ModelName: "DeepSeek", ApiKey: "sk-abc"},
			{ModelName: "EmptyKey", ApiKey: ""},
			{ModelName: "WhitespaceKey", ApiKey: "  "},
			{ModelName: "百度千帆", ApiKey: "key-123"},
		},
	}

	got := validProviders(tc)

	// Should contain Original, DeepSeek, 百度千帆 — not EmptyKey or WhitespaceKey
	if len(got) != 3 {
		t.Fatalf("validProviders returned %d items, want 3", len(got))
	}

	wantNames := map[string]bool{"Original": true, "DeepSeek": true, "百度千帆": true}
	for _, m := range got {
		if !wantNames[m.ModelName] {
			t.Errorf("validProviders returned unexpected provider %q", m.ModelName)
		}
	}

	// Verify no invalid provider sneaked in
	for _, m := range got {
		if !isValidProvider(m) {
			t.Errorf("validProviders returned invalid provider %+v", m)
		}
	}
}
