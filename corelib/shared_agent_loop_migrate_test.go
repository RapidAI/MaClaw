package corelib

import (
	"encoding/json"
	"testing"
)

func TestSharedAgentLoopEnabled_JSONRoundTripFalse(t *testing.T) {
	// Default-true + UnmarshalJSON seeding means omitempty would drop false
	// and reload would flip back to true. Explicit false must survive.
	in := AppConfig{SharedAgentLoopEnabled: false, SharedAgentLoopMigrated: true}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AppConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.SharedAgentLoopEnabled {
		t.Fatalf("false must survive reload; json=%s", data)
	}
	if !out.SharedAgentLoopMigrated {
		t.Fatal("migrated should stay true")
	}
}

func TestApplySharedAgentLoopMigration_Once(t *testing.T) {
	cfg := AppConfig{} // legacy install: both flags false
	if !ApplySharedAgentLoopMigration(&cfg) {
		t.Fatal("expected first migration to change config")
	}
	if !cfg.SharedAgentLoopEnabled || !cfg.SharedAgentLoopMigrated {
		t.Fatalf("cfg=%+v", cfg)
	}
	if ApplySharedAgentLoopMigration(&cfg) {
		t.Fatal("second migration must be no-op")
	}
	// User disables after migration — stay off.
	cfg.SharedAgentLoopEnabled = false
	if ApplySharedAgentLoopMigration(&cfg) {
		t.Fatal("must not re-enable after user opt-out")
	}
	if cfg.SharedAgentLoopEnabled {
		t.Fatal("user opt-out must stick")
	}
}

func TestApplySharedAgentLoopMigration_Nil(t *testing.T) {
	if ApplySharedAgentLoopMigration(nil) {
		t.Fatal("nil must be no-op")
	}
}

func TestAppConfigDefaults_AlreadyMigrated(t *testing.T) {
	d := AppConfigDefaults()
	if !d.SharedAgentLoopEnabled || !d.SharedAgentLoopMigrated {
		t.Fatalf("defaults should enable+mark migrated: enabled=%v migrated=%v",
			d.SharedAgentLoopEnabled, d.SharedAgentLoopMigrated)
	}
	// Migration on defaults should no-op.
	if ApplySharedAgentLoopMigration(&d) {
		t.Fatal("defaults already migrated")
	}
}
