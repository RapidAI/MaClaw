package intent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestComputerUseCalibrationWithRealModel validates the LabelComputerUse
// anchors against the production Gemma embedder, using the message that
// originally failed to activate Computer Use. Skipped in -short mode or when
// the embedding model is not installed locally.
func TestComputerUseCalibrationWithRealModel(t *testing.T) {
	if testing.Short() {
		t.Skip("real-model calibration skipped in -short mode")
	}
	modelPath := embedding.DefaultModelPath()
	if modelPath == "" {
		// BaseDirFunc is only wired in the running app; fall back to the
		// conventional ~/.maclaw/models location for local calibration runs.
		if home, err := os.UserHomeDir(); err == nil {
			modelPath = filepath.Join(home, ".maclaw", "models", embedding.DefaultModelFilename)
		}
	}
	if modelPath == "" {
		t.Skip("embedding model path not configured")
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("embedding model not installed: %v", err)
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Skipf("gemma embedder unavailable: %v", err)
	}
	defer emb.Close()

	uic := New(Config{Embedder: emb})
	deadline := time.Now().Add(120 * time.Second)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("anchor warmup timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Phrases that must activate Computer Use. The first is the original
	// failure: the agent claimed it could not operate GUI programs.
	positives := []string{
		"打开word程序，编写一个你（maclaw）的简历",
		"帮我在电脑上打开Excel程序，把这一列求和",
		"点击窗口上的确定按钮",
	}
	for _, text := range positives {
		res := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
		t.Logf("%q → primary=%s conf=%.3f scores=%v", text, res.Primary, res.Confidence, uic.DiagnoseScores(text))
		if res.Primary != LabelComputerUse {
			t.Errorf("%q: primary = %s, want %s", text, res.Primary, LabelComputerUse)
		}
		// GUI gate uses 0.65 min confidence (see gui computerUseIntentMinConfidence).
		if res.Confidence < 0.65 {
			t.Errorf("%q: confidence %.3f below the 0.65 activation gate", text, res.Confidence)
		}
	}

	// Guard phrases that must NOT activate Computer Use: content creation
	// goes to office, web tasks go to browser.
	guards := []string{
		"帮我写一份word简历",
		"在浏览器里打开知乎发帖",
	}
	for _, text := range guards {
		res := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
		t.Logf("%q → primary=%s conf=%.3f scores=%v", text, res.Primary, res.Confidence, uic.DiagnoseScores(text))
		// Match production gate: primary CU + conf>=0.65 would open the surface.
		if res.Primary == LabelComputerUse && res.Confidence >= 0.65 {
			t.Errorf("%q unexpectedly activates computer_use (conf=%.3f)", text, res.Confidence)
		}
	}
}
