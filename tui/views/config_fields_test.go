package views

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestConfigFields_SingleSourceOfTruth verifies the mechanism-level invariant:
// every key that appears in the UI (allConfigFields) has both a Get and Set
// accessor, and round-trips correctly through AppConfig.
//
// This test makes it impossible to add a field to the UI without also wiring
// up its Get/Set — the three-way sync problem that previously caused silent
// bugs (field visible but doesn't save, or saves but doesn't load).
func TestConfigFields_SingleSourceOfTruth(t *testing.T) {
	seen := make(map[string]bool)
	for _, def := range allConfigFields {
		// No duplicate keys.
		if seen[def.Key] {
			t.Errorf("duplicate key in allConfigFields: %q", def.Key)
		}
		seen[def.Key] = true

		// Every field must have Get and Set (except ReadOnly which only needs Get).
		if def.Get == nil {
			t.Errorf("key %q: missing Get accessor", def.Key)
		}
		if !def.ReadOnly && def.Set == nil {
			t.Errorf("key %q: missing Set accessor (non-ReadOnly field)", def.Key)
		}

		// Tab must be valid.
		if def.Tab < 0 || def.Tab >= CfgTabCount {
			t.Errorf("key %q: invalid Tab %d", def.Key, def.Tab)
		}

		// DescKey must be non-empty.
		if def.DescKey == "" {
			t.Errorf("key %q: empty DescKey", def.Key)
		}
	}
}

// TestConfigFields_RoundTrip verifies that every non-ReadOnly field can be
// written to AppConfig via Set and read back via Get with the same value.
func TestConfigFields_RoundTrip(t *testing.T) {
	for _, def := range allConfigFields {
		if def.ReadOnly || def.Get == nil || def.Set == nil {
			continue
		}

		// Pick a test value based on field type.
		testVal := "test_value_123"
		if def.Options != nil && len(def.Options) > 0 {
			// Use the last option (different from typical default which is first).
			testVal = def.Options[len(def.Options)-1]
		}
		// intGet returns "" for zero, so use a numeric string for int fields.
		// Detect int fields by checking if the default Get on a zero-value config
		// returns "" (intGet behavior) vs "false" (boolGet) vs "" (strGet).
		// More robust: just try Set+Get and if it fails with a non-numeric string,
		// retry with a numeric one.
		cfg := corelib.AppConfig{}
		def.Set(&cfg, testVal)
		got := def.Get(&cfg)

		if got != testVal {
			// Might be an int field — retry with numeric value.
			numVal := "42"
			def.Set(&cfg, numVal)
			got = def.Get(&cfg)
			if got != numVal {
				t.Errorf("key %q: round-trip failed: Set(%q) then Get() = %q (also tried %q → %q)",
					def.Key, testVal, got, numVal, got)
			}
		}
	}
}

// TestConfigFields_ApplyAndLoad verifies the exported ApplyConfigValue and
// LoadConfigValue functions work for every registered key.
func TestConfigFields_ApplyAndLoad(t *testing.T) {
	for _, def := range allConfigFields {
		if def.ReadOnly {
			continue
		}

		testVal := "exported_api_test"
		if def.Options != nil && len(def.Options) > 0 {
			testVal = def.Options[0]
		}

		cfg := corelib.AppConfig{}
		ApplyConfigValue(&cfg, def.Key, testVal)
		got, ok := LoadConfigValue(&cfg, def.Key)
		if !ok {
			t.Errorf("key %q: LoadConfigValue returned ok=false", def.Key)
			continue
		}
		if got != testVal {
			// Might be an int field — retry with numeric value.
			numVal := "99"
			cfg2 := corelib.AppConfig{}
			ApplyConfigValue(&cfg2, def.Key, numVal)
			got2, _ := LoadConfigValue(&cfg2, def.Key)
			if got2 != numVal {
				t.Errorf("key %q: ApplyConfigValue(%q) then LoadConfigValue() = %q (also tried %q → %q)",
					def.Key, testVal, got, numVal, got2)
			}
		}
	}
}

// TestConfigFields_UIEntriesMatchDefs verifies that NewConfigModel produces
// entries that exactly match allConfigFields — no entries are lost or added
// outside the single source of truth.
func TestConfigFields_UIEntriesMatchDefs(t *testing.T) {
	m := NewConfigModel("zh")

	// Collect all UI keys.
	uiKeys := make(map[string]bool)
	for tab := 0; tab < CfgTabCount; tab++ {
		for _, e := range m.tabs[tab] {
			if uiKeys[e.Key] {
				t.Errorf("duplicate UI key: %q", e.Key)
			}
			uiKeys[e.Key] = true
		}
	}

	// Every def must appear in UI.
	for _, def := range allConfigFields {
		if !uiKeys[def.Key] {
			t.Errorf("key %q defined in allConfigFields but missing from UI", def.Key)
		}
	}

	// Every UI key must be in defs.
	defKeys := make(map[string]bool)
	for _, def := range allConfigFields {
		defKeys[def.Key] = true
	}
	for key := range uiKeys {
		if !defKeys[key] {
			t.Errorf("UI key %q not found in allConfigFields", key)
		}
	}
}

// TestConfigFields_LoadFromAppConfig verifies that LoadFromAppConfig uses
// the Get accessors and populates all UI entries.
func TestConfigFields_LoadFromAppConfig(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteHubURL:            "https://hub.example.com",
		MaclawLLMModel:          "test-model",
		QQBotEnabled:            true,
		DefaultProxyEnabled:     true,
		SecurityPolicyMode:      "strict",
		SkillPurchaseMode:       "free_only",
		MaclawAgentMaxIterations: 42,
	}

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)

	checks := map[string]string{
		"hub_url":              "https://hub.example.com",
		"maclaw_llm_model":     "test-model",
		"qqbot_enabled":        "true",
		"default_proxy_enabled": "true",
		"security_policy_mode": "strict",
		"skill_purchase_mode":  "free_only",
		"max_iterations":       "42",
	}

	for key, want := range checks {
		found := false
		for tab := 0; tab < CfgTabCount; tab++ {
			for _, e := range m.tabs[tab] {
				if e.Key == key {
					if e.Value != want {
						t.Errorf("LoadFromAppConfig: key %q = %q, want %q", key, e.Value, want)
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("LoadFromAppConfig: key %q not found in UI", key)
		}
	}
}
