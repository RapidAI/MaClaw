package asr

import (
	"fmt"
	"math"
	"testing"
)

func stats(name string, x []float32) {
	if len(x) == 0 {
		fmt.Printf("  %s: EMPTY\n", name)
		return
	}
	mn, mx := x[0], x[0]
	var sum float64
	nonzero := 0
	for _, v := range x {
		sum += float64(v)
		if v < mn { mn = v }
		if v > mx { mx = v }
		if v != 0 { nonzero++ }
	}
	mean := sum / float64(len(x))
	var variance float64
	for _, v := range x { d := float64(v) - mean; variance += d * d }
	variance /= float64(len(x))
	fmt.Printf("  %s: len=%d min=%.4f max=%.4f mean=%.4f std=%.4f nonzero=%d\n",
		name, len(x), mn, mx, mean, math.Sqrt(variance), nonzero)
}

func TestSenseVoiceDiagnostic(t *testing.T) {
	modelPath := findSenseVoiceModel(t)
	m, err := NewSenseVoice(modelPath)
	if err != nil { t.Fatalf("load: %v", err) }
	defer m.Close()

	wavPath := findBeijingWAV(t)
	pcm, err := LoadWAV(wavPath)
	if err != nil { t.Fatalf("wav: %v", err) }
	t.Logf("PCM: %d samples", len(pcm))

	// Step 1: Fbank
	fbank := svMelFilterbank(pcm)
	numFrames := len(fbank) / svNumMels
	t.Logf("Fbank: %d frames", numFrames)
	stats("fbank", fbank)

	// Step 2: LFR
	lfrFeats, lfrFrames := svApplyLFR(fbank, numFrames)
	t.Logf("LFR: %d frames, dim=%d", lfrFrames, svFeatsDim)
	stats("lfr", lfrFeats)

	// Step 3: CMVN
	if m.cmvnIstd != nil {
		for i := range lfrFeats {
			lfrFeats[i] *= m.cmvnIstd[i%svFeatsDim]
		}
	}
	stats("after_cmvn", lfrFeats)

	// Step 4: Build input (embed + features + scale + pos)
	hp := m.hp
	hidden := hp.HiddenSize
	featsDim := hp.FeatsDim
	totalFrames := 4 + lfrFrames
	x := make([]float32, totalFrames*featsDim)
	if m.w.embedding != nil {
		promptIDs := [4]int{0, 1, 2, 14}
		embRows := len(m.w.embedding) / featsDim
		for i, pid := range promptIDs {
			if pid < embRows {
				copy(x[i*featsDim:(i+1)*featsDim], m.w.embedding[pid*featsDim:(pid+1)*featsDim])
			}
		}
	}
	copy(x[4*featsDim:], lfrFeats[:lfrFrames*featsDim])
	stats("before_scale", x)

	scale := float32(math.Sqrt(float64(hidden)))
	for i := range x { x[i] *= scale }
	stats("after_scale", x)

	svAddPosEncoding(x, totalFrames, featsDim)
	stats("after_posenc", x)

	// Step 5: Entry block (first SANM)
	x = m.sanmBlock(x, totalFrames, featsDim, &m.w.encoder0)
	stats("after_encoder0", x)

	// Step 6: First main block
	if len(m.w.encoders) > 0 {
		x = m.sanmBlock(x, totalFrames, hidden, &m.w.encoders[0])
		stats("after_encoders[0]", x)
	}

	// Run remaining encoder blocks
	for i := 1; i < len(m.w.encoders); i++ {
		x = m.sanmBlock(x, totalFrames, hidden, &m.w.encoders[i])
	}
	stats("after_all_encoders", x)

	// After-norm
	svLayerNormBias(x, totalFrames, hidden, m.w.afterNormW, m.w.afterNormB)
	stats("after_norm", x)

	// TP blocks
	for i := range m.w.tpEncoders {
		x = m.sanmBlock(x, totalFrames, hidden, &m.w.tpEncoders[i])
	}
	stats("after_tp", x)

	// TP norm
	svLayerNormBias(x, totalFrames, hidden, m.w.tpNormW, m.w.tpNormB)
	stats("after_tp_norm", x)

	// CTC head
	vocab := m.hp.VocabSize
	logits := make([]float32, totalFrames*vocab)
	matMulLinear(logits, x, m.w.ctcW, totalFrames, vocab, hidden)
	if m.w.ctcB != nil {
		for f := 0; f < totalFrames; f++ {
			off := f * vocab
			for v := 0; v < vocab; v++ { logits[off+v] += m.w.ctcB[v] }
		}
	}
	stats("ctc_logits", logits)

	// Check what token wins for frame 5 (skip prompt frames 0-3)
	frame5off := 5 * vocab
	bestID, bestVal := 0, logits[frame5off]
	for v := 1; v < vocab; v++ {
		if logits[frame5off+v] > bestVal { bestVal = logits[frame5off+v]; bestID = v }
	}
	t.Logf("Frame 5 best token: id=%d val=%.4f (blank=%.4f)", bestID, bestVal, logits[frame5off])

	t.Logf("Entry block norm1W len=%d, qW.Rows=%d, qW.f32 len=%d",
		len(m.w.encoder0.norm1W), m.w.encoder0.qW.Rows(),
		len(m.w.encoder0.qW.f32))
}
