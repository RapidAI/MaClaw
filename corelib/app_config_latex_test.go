package corelib

import (
	"encoding/json"
	"testing"
)

func TestOldConfigWithoutLatexFieldEnablesTinyTeX(t *testing.T) {
	const oldConfig = `{"language":"zh-Hans","current_project":"demo"}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(oldConfig), &cfg); err != nil {
		t.Fatalf("unmarshal old config: %v", err)
	}
	if !cfg.LatexTinyTeXEnabled {
		t.Fatal("LatexTinyTeXEnabled = false for a config without the field, want default true")
	}
}

func TestLatexTinyTeXEnabledCanBeTurnedOff(t *testing.T) {
	const stored = `{"latex_tinytex_enabled":false}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.LatexTinyTeXEnabled {
		t.Fatal("LatexTinyTeXEnabled = true, want stored false")
	}
}
