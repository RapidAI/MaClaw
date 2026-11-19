package qoder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledCatalogUsesDisplayNamesOnce(t *testing.T) {
	models := []Model{
		{Key: "auto", DisplayName: "Fast", Enabled: true, IsDefault: true, Scene: "assistant"},
		{Key: "auto", DisplayName: "Fast", Enabled: true, Scene: "cli"},
		{Key: "qmodel_38max", DisplayName: "Qwen3.8-Max", Enabled: true},
		{Key: "dfmodel", DisplayName: "DeepSeek-Flash", Enabled: true},
		{Key: "hidden", DisplayName: "Hidden", Enabled: false},
		{Key: "plain", Enabled: true},
	}
	labels, defaultLabel := EnabledCatalog(models)
	want := []string{"Fast", "Qwen3.8-Max", "DeepSeek-Flash", "plain"}
	if len(labels) != len(want) {
		t.Fatalf("labels = %#v, want %#v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels = %#v, want %#v", labels, want)
		}
	}
	if defaultLabel != "Fast" {
		t.Fatalf("default = %q, want Fast", defaultLabel)
	}
}

func TestCatalogDisplayNameSignsAsWireKey(t *testing.T) {
	RememberModels([]Model{
		{Key: "dmodel", DisplayName: "DeepSeek-V4-Pro", Enabled: true},
		{Key: "dfmodel", DisplayName: "DeepSeek-Flash", Enabled: true},
		{Key: "auto", DisplayName: "Fast", Enabled: true},
	})
	if got := canonicalQoderModelKey("DeepSeek-V4-Pro"); got != "dmodel" {
		t.Fatalf("display name key = %q", got)
	}
	if got := canonicalQoderModelKey("dfmodel"); got != "dfmodel" {
		t.Fatalf("short key = %q", got)
	}
	if got := canonicalQoderModelKey("Qwen3.8-Max"); got != "qmodel_38max" {
		t.Fatalf("builtin label key = %q", got)
	}
	if got := CanonicalDisplayName("dfmodel"); got != "DeepSeek-Flash" {
		t.Fatalf("short key label = %q", got)
	}
	if got := CanonicalDisplayName("Fast"); got != "Fast" {
		t.Fatalf("display label = %q", got)
	}
	if got := CanonicalDisplayName("not-a-qoder-model"); got != "" {
		t.Fatalf("unknown label = %q", got)
	}
	raw, err := chatBodyJSON(ChatRequest{
		Model:    "DeepSeek-V4-Pro",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"model":"dmodel"`) || strings.Contains(raw, "DeepSeek-V4-Pro") {
		t.Fatalf("signed body = %s", raw)
	}
}

func TestModelAliasFileRestoresDisplayNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliases.json")
	t.Setenv("QODER_MODEL_ALIAS_FILE", path)
	resetModelAliases()
	RememberModels([]Model{{Key: "gmodel", DisplayName: "GLM-5.3", Enabled: true}})
	resetModelAliases()
	if got := canonicalQoderModelKey("GLM-5.3"); got != "gmodel" {
		t.Fatalf("reloaded key = %q", got)
	}
	if got := CanonicalDisplayName("gmodel"); got != "GLM-5.3" {
		t.Fatalf("reloaded label = %q", got)
	}
}

func TestLegacyAliasFileIgnoresWireKeySelfMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliases.json")
	t.Setenv("QODER_MODEL_ALIAS_FILE", path)
	body := []byte(`{"by_name":{"fast":"auto","auto":"auto","deepseek-flash":"dfmodel"}}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	resetModelAliases()
	if got := canonicalQoderModelKey("Fast"); got != "auto" {
		t.Fatalf("Fast = %q", got)
	}
	if got := canonicalQoderModelKey("DeepSeek-Flash"); got != "dfmodel" {
		t.Fatalf("DeepSeek-Flash = %q", got)
	}
	if got := CanonicalDisplayName("auto"); got == "" {
		t.Fatal("wire-key entry cleared the display name")
	}
}

func TestLaterWireKeyDoesNotEraseDisplayName(t *testing.T) {
	resetModelAliases()
	RememberModels([]Model{{Key: "auto", DisplayName: "Fast", Enabled: true}})
	RememberModels([]Model{{Key: "mmodel", DisplayName: "MiniMax-M3.1-Flash", Enabled: true}})
	if got := canonicalQoderModelKey("Fast"); got != "auto" {
		t.Fatalf("Fast key = %q, want auto", got)
	}
	if got := canonicalQoderModelKey("mmodel"); got != "mmodel" {
		t.Fatalf("wire key = %q", got)
	}
	if got := canonicalQoderModelKey("MiniMax-M3.1-Flash"); got != "mmodel" {
		t.Fatalf("display key = %q", got)
	}
	// A display name that is only a different spelling of the wire key is
	// not a second model. The retired name must stop resolving.
	RememberModels([]Model{{Key: "auto", DisplayName: "Auto", Enabled: true}})
	if got := canonicalQoderModelKey("Fast"); got != "Fast" {
		t.Fatalf("retired Fast still maps to %q", got)
	}
	if got := canonicalQoderModelKey("auto"); got != "auto" {
		t.Fatalf("wire key auto = %q", got)
	}
	if got := CanonicalDisplayName("auto"); got != "" {
		t.Fatalf("case-only display name = %q", got)
	}
}

func TestSharedDisplayNameKeepsFirstWireKey(t *testing.T) {
	resetModelAliases()
	RememberModels([]Model{{Key: "auto", DisplayName: "Fast", Enabled: true}})
	RememberModels([]Model{{Key: "other", DisplayName: "Fast", Enabled: true}})
	if got := canonicalQoderModelKey("Fast"); got != "auto" {
		t.Fatalf("Fast key = %q", got)
	}
	if got := CanonicalDisplayName("other"); got != "" {
		t.Fatalf("colliding key label = %q", got)
	}
	if got := canonicalQoderModelKey("other"); got != "other" {
		t.Fatalf("other key = %q", got)
	}
}

func TestEnabledCatalogKeepsDefaultFromLaterScene(t *testing.T) {
	models := []Model{
		{Key: "qmodel_38max", DisplayName: "Qwen3.8-Max", Enabled: true, Scene: "assistant"},
		{Key: "qmodel_38max", DisplayName: "Qwen3.8-Max", Enabled: true, IsDefault: true, Scene: "cli"},
		{Key: "dfmodel", DisplayName: "DeepSeek-Flash", Enabled: true, Scene: "assistant"},
	}
	labels, defaultLabel := EnabledCatalog(models)
	if len(labels) != 2 || labels[0] != "Qwen3.8-Max" || labels[1] != "DeepSeek-Flash" {
		t.Fatalf("labels = %#v", labels)
	}
	if defaultLabel != "Qwen3.8-Max" {
		t.Fatalf("default = %q", defaultLabel)
	}
}

func resetModelAliases() {
	aliasMu.Lock()
	defer aliasMu.Unlock()
	aliasLoaded = false
	aliasLabelToKey = map[string]string{}
	aliasLabelByKey = map[string]string{}
}
