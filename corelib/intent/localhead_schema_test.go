package intent

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// The cross-family trainer writes "calibration" as a free-text string and the
// family as "embedder_model_id"; the r2bF release head failed LoadLocalHead
// with an unmarshal error before this (local L3 silently unavailable).
func TestParseLocalHeadAcceptsStringCalibrationAndEmbedderModelID(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(tinyHeadJSON(t, []string{"search", "ssh", "unknown"}, 4, 3, 0.5, 0.9, []string{"ssh"}), &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "embed_model")
	m["calibration"] = "T/tau/tau_s fitted on pooled leave-one-family-out predictions"
	m["embedder_model_id"] = "Qwen3 Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1"
	b, _ := json.Marshal(m)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatalf("string calibration must load: %v", err)
	}
	if h.CalibrationSource == "" || HeadFamily(h) != embedding.FamilyQwen3 {
		t.Fatalf("calibration=%q family=%q, want text kept and qwen3 family", h.CalibrationSource, HeadFamily(h))
	}
	m["calibration"] = map[string]string{"source": "shadow", "compute": "pure-go"}
	b, _ = json.Marshal(m)
	if h, err = ParseLocalHead(b); err != nil || h.CalibrationSource != "shadow" || h.CalibrationCompute != "pure-go" {
		t.Fatalf("object calibration: h=%+v err=%v", h, err)
	}
	m["calibration"] = 42
	b, _ = json.Marshal(m)
	if _, err = ParseLocalHead(b); err == nil {
		t.Fatal("non-string non-object calibration must still be rejected")
	}
}

// MACLAW_TEST_INTENT_HEAD=<release head json> checks the shipped artifact.
func TestReleaseIntentHeadLoads(t *testing.T) {
	p := os.Getenv("MACLAW_TEST_INTENT_HEAD")
	if p == "" {
		t.Skip("MACLAW_TEST_INTENT_HEAD not set")
	}
	h, err := LoadLocalHead(p)
	if err != nil {
		t.Fatal(err)
	}
	if HeadFamily(h) != embedding.FamilyQwen3 || h.Dim() != 1024 || h.Tau <= 0 || h.TauS < h.Tau {
		t.Fatalf("family=%q dim=%d tau=%v tau_s=%v", HeadFamily(h), h.Dim(), h.Tau, h.TauS)
	}
}
