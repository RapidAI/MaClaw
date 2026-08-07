package corelib

import (
	"encoding/json"
	"testing"
)

// TestOldConfigWithoutOCRFieldsLoadsDefaults ensures a config.json written
// before the OCR feature existed still enables OCR with the default tier:
// UnmarshalJSON seeds AppConfigDefaults before overlaying stored fields.
func TestOldConfigWithoutOCRFieldsLoadsDefaults(t *testing.T) {
	const oldConfig = `{"language":"en","current_project":"demo","active_tool":"claude"}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(oldConfig), &cfg); err != nil {
		t.Fatalf("unmarshal old config: %v", err)
	}
	if !cfg.OCREnabled {
		t.Fatal("OCREnabled = false for old config without ocr_enabled, want default true")
	}
	if cfg.OCRModelTier != DefaultOCRModelTier {
		t.Fatalf("OCRModelTier = %q, want default %q", cfg.OCRModelTier, DefaultOCRModelTier)
	}
}

// TestOCRConfigFieldsRoundTrip ensures explicitly stored OCR settings win
// over the seeded defaults.
func TestOCRConfigFieldsRoundTrip(t *testing.T) {
	const stored = `{"ocr_enabled":false,"ocr_model_tier":"medium"}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.OCREnabled {
		t.Fatal("OCREnabled = true, want stored false")
	}
	if cfg.OCRModelTier != "medium" {
		t.Fatalf("OCRModelTier = %q, want medium", cfg.OCRModelTier)
	}
}

func TestNormalizeOCRModelTier(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"huge", DefaultOCRModelTier},  // unknown value
		{"", DefaultOCRModelTier},      // empty
		{"SMALL", DefaultOCRModelTier}, // case-sensitive: junk
		{"  ", DefaultOCRModelTier},    // whitespace only
		{"tiny", "tiny"},
		{"small", "small"},
		{"medium", "medium"},
		{" tiny ", "tiny"}, // surrounding whitespace tolerated
	} {
		if got := NormalizeOCRModelTier(tc.in); got != tc.want {
			t.Errorf("NormalizeOCRModelTier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
