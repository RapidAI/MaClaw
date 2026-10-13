package intent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Qwen3 cosines need their own map onto the raw Gemma scale; the Gemma
// classification map must never be applied to them (and vice versa).
func TestQwen3EmbedderGetsItsOwnScoreCalibration(t *testing.T) {
	prev := embedding.PromptsEnabled()
	defer embedding.SetPromptsEnabled(prev)
	q := &embedding.Qwen3Embedder{}
	embedding.SetPromptsEnabled(true)
	if got := intentScoreCalibrationFor(q); got != qwen3ClassificationScoreCalibration {
		t.Fatalf("qwen3 prompted calibration = %+v", got)
	}
	if qwen3ClassificationScoreCalibration == classificationPromptScoreCalibration {
		t.Fatal("qwen3 and gemma maps must differ")
	}
	embedding.SetPromptsEnabled(false)
	if got := intentScoreCalibrationFor(q); got != identityScoreCalibration {
		t.Fatalf("qwen3 raw calibration = %+v, want identity", got)
	}
	// monotone, bounded
	c := qwen3ClassificationScoreCalibration
	last := -1.0
	for s := 0.0; s <= 1.0001; s += 0.05 {
		v := c.apply(s)
		if v < last || v > 1 {
			t.Fatalf("map not monotone/bounded at %.2f: %.3f", s, v)
		}
		last = v
	}
}
