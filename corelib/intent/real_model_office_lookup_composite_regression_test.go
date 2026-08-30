package intent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestRealModelOfficeLookupCompositeRegression pins the 2026-08-25 production
// failure class against the installed embedding model: a deck request that
// bundles an online image search ("网上随便找一下布偶照片") scored office
// 0.857 and shipped confident without the lookup half, so the turn's tool
// surface had no web_search at all. Office-only negatives score the lookup
// labels *higher* than the genuine phrasings on this model, so the pair must
// escalate to the tree with the companion attached — never ship locally, and
// never collapse to a plain office verdict.
func TestRealModelOfficeLookupCompositeRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("real-model regression skipped in short mode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("resolve home directory: %v", err)
	}
	modelPath := filepath.Join(home, ".maclaw", "models", embedding.DefaultModelFilename)
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("embedding model not installed: %v", err)
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()

	uic := New(Config{Embedder: emb})
	deadline := time.Now().Add(2 * time.Minute)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("anchor warmup timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, text := range []string{
		"生成庆祝我家布偶宝宝5岁生日的ppt，没有照片，网上随便找一下布偶照片。",
		"网上找几张布偶猫照片，做成生日PPT",
	} {
		result := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
		t.Logf("%q -> primary=%s secondary=%v confidence=%.3f reason=%q scores=%v", text, result.Primary, result.Secondary, result.Confidence, result.Reason, uic.DiagnoseScores(text))
		if result.Primary != LabelOffice {
			t.Fatalf("%q -> primary=%s, want office", text, result.Primary)
		}
		// The pair is not locally separable on this model, so the turn must
		// carry the lookup half as escalation evidence instead of shipping a
		// confident plain-office verdict.
		if len(result.Secondary) != 1 || !isLookupIntentLabel(result.Secondary[0]) {
			t.Fatalf("%q -> secondary=%v, want exactly one lookup half attached as composite evidence", text, result.Secondary)
		}
		if !strings.Contains(result.Reason, "composite") {
			t.Fatalf("%q -> reason=%q, want composite escalation evidence", text, result.Reason)
		}
	}
}
