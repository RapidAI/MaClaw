package intent

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestCloseEmbedderDuringAnchorWarmupRealModel reproduces the old SIGSEGV:
// TestComputerUseCalibrationWithRealModel timed out under load and its
// deferred emb.Close() unmapped the Gemma weights while the background anchor
// warmup was still inside EmbedBatch. Closing mid-warmup must now end the
// warmup with ErrEmbedderClosed instead of crashing the process.
// MACLAW_CLOSE_RACE_ITERS controls the repetitions (default 3).
func TestCloseEmbedderDuringAnchorWarmupRealModel(t *testing.T) {
	if testing.Short() {
		t.Skip("real-model test skipped in -short mode")
	}
	modelPath := embedding.DefaultModelPath()
	if modelPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			modelPath = filepath.Join(home, ".maclaw", "models", embedding.DefaultModelFilename)
		}
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("embedding model not installed: %v", err)
	}
	t.Setenv("MACLAW_INTENT_ANCHOR_CACHE", "0") // force a real (slow) warmup
	iters := 3
	if n, err := strconv.Atoi(os.Getenv("MACLAW_CLOSE_RACE_ITERS")); err == nil && n > 0 {
		iters = n
	}
	for i := 0; i < iters; i++ {
		emb, err := embedding.NewGemmaEmbedder(modelPath, embedding.DefaultEmbeddingDim)
		if err != nil {
			t.Skipf("gemma embedder unavailable: %v", err)
		}
		uic := New(Config{Embedder: emb})
		time.Sleep(time.Duration(50+rand.Intn(400)) * time.Millisecond)
		start := time.Now()
		emb.Close() // warmup is mid-flight (it takes tens of seconds)
		closeDur := time.Since(start)
		if _, err := emb.Embed("after close"); !errors.Is(err, embedding.ErrEmbedderClosed) {
			t.Fatalf("Embed after Close: %v", err)
		}
		// The warmup goroutine must observe the close and stop; give it a moment
		// (a crash would kill the test binary here).
		time.Sleep(100 * time.Millisecond)
		if uic.Ready() {
			t.Logf("iteration %d: warmup had already finished before Close", i)
		}
		res := uic.ClassifyEmbeddingOnly(MessageContext{Text: "打开word程序"})
		t.Logf("iteration %d: Close took %s; after close L2 result=%s layer=%d", i, closeDur, res.Primary, res.Layer)
	}
}
