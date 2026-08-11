package corelib

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOfficeReadConfigDefaultsAndRoundTrip(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal legacy config: %v", err)
	}
	if cfg.OfficeReadEngine != "officeread" || !reflect.DeepEqual(cfg.OfficeReadFormats, []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"}) || cfg.OfficeReadFallback == nil || !*cfg.OfficeReadFallback || cfg.OfficeReadEmitMarkdown == nil || *cfg.OfficeReadEmitMarkdown {
		t.Fatalf("unexpected OfficeRead defaults: %#v", cfg)
	}

	const stored = `{"office_read_engine":"dual","office_read_formats":[".doc","xls"],"office_read_fallback":false,"office_read_emit_markdown":true}`
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		t.Fatalf("unmarshal stored policy: %v", err)
	}
	if cfg.OfficeReadEngine != "dual" || !reflect.DeepEqual(cfg.OfficeReadFormats, []string{".doc", "xls"}) || cfg.OfficeReadFallback == nil || *cfg.OfficeReadFallback || cfg.OfficeReadEmitMarkdown == nil || !*cfg.OfficeReadEmitMarkdown {
		t.Fatalf("stored OfficeRead policy did not win: %#v", cfg)
	}
}

func TestOfficeReadScopeMigrationMarkerOnlyResetsForHistoricPPTScope(t *testing.T) {
	tests := []struct {
		name         string
		stored       string
		wantMigrated bool
	}{
		{
			name:         "missing policy keeps current default marker",
			stored:       `{}`,
			wantMigrated: true,
		},
		{
			name:         "historic ppt policy needs promotion",
			stored:       `{"office_read_formats":[".ppt"]}`,
			wantMigrated: false,
		},
		{
			name:         "partial rollback remains marked current",
			stored:       `{"office_read_formats":["doc"]}`,
			wantMigrated: true,
		},
		{
			name:         "explicit marker always wins",
			stored:       `{"office_read_formats":["ppt"],"office_read_scope_migrated":true}`,
			wantMigrated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg AppConfig
			if err := json.Unmarshal([]byte(tt.stored), &cfg); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.stored, err)
			}
			if cfg.OfficeReadScopeMigrated != tt.wantMigrated {
				t.Fatalf("OfficeReadScopeMigrated = %v, want %v for %s", cfg.OfficeReadScopeMigrated, tt.wantMigrated, tt.stored)
			}
		})
	}
}

func TestApplyOfficeReadFullScopeMigration(t *testing.T) {
	legacy := AppConfig{OfficeReadEngine: "officeread", OfficeReadFormats: []string{"ppt"}}
	if !ApplyOfficeReadFullScopeMigration(&legacy) {
		t.Fatal("historic PPT scope should migrate")
	}
	want := []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"}
	if !legacy.OfficeReadScopeMigrated || !reflect.DeepEqual(legacy.OfficeReadFormats, want) {
		t.Fatalf("migrated scope = %#v, want %#v", legacy, want)
	}
	if ApplyOfficeReadFullScopeMigration(&legacy) {
		t.Fatal("migration must be one-time")
	}

	partial := AppConfig{OfficeReadEngine: "officeread", OfficeReadFormats: []string{"doc"}}
	if !ApplyOfficeReadFullScopeMigration(&partial) || !partial.OfficeReadScopeMigrated || !reflect.DeepEqual(partial.OfficeReadFormats, []string{"doc"}) {
		t.Fatalf("partial rollback scope must remain intact: %#v", partial)
	}
	legacyEngine := AppConfig{OfficeReadEngine: "legacy", OfficeReadFormats: []string{"ppt"}}
	if !ApplyOfficeReadFullScopeMigration(&legacyEngine) || legacyEngine.OfficeReadEngine != "legacy" || !reflect.DeepEqual(legacyEngine.OfficeReadFormats, want) {
		t.Fatalf("global legacy rollback must retain engine choice: %#v", legacyEngine)
	}
}
