package guiapp

import (
	"testing"

	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
)

func TestGetAppConfigSchemaUsesCanonicalCoreSchema(t *testing.T) {
	response := (&App{}).GetAppConfigSchema()
	if response.SchemaVersion != coreconfig.AppConfigSchemaVersion {
		t.Fatalf("schema version = %q, want %q", response.SchemaVersion, coreconfig.AppConfigSchemaVersion)
	}
	canonical := coreconfig.AppConfigSchema()
	if len(response.Fields) != len(canonical) || len(response.Fields) == 0 {
		t.Fatalf("schema fields = %d, canonical = %d", len(response.Fields), len(canonical))
	}
	for i := range canonical {
		if response.Fields[i].Key != canonical[i].Key || response.Fields[i].Scope != canonical[i].Scope {
			t.Fatalf("field[%d] drifted: %#v vs %#v", i, response.Fields[i], canonical[i])
		}
	}
}
