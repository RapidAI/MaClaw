package tts

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const testGGUF = "testdata/melotts-en-fp32.gguf"

func loadRef(t *testing.T, name string) []float32 {
	t.Helper()
	path := filepath.Join("testdata", name+".bin")
	data, err := loadBinFloat32(path)
	if err != nil {
		t.Skipf("ref file not found: %s (run convert_and_compare.py)", path)
	}
	return data
}

func report(t *testing.T, name string, goData, pyData []float32) (maxD, avgD float32) {
	t.Helper()
	if len(goData) != len(pyData) {
		t.Errorf("%s: size mismatch Go=%d Py=%d", name, len(goData), len(pyData))
		return
	}
	maxD, avgD = compareFloat32(goData, pyData)
	t.Logf("%s: maxDiff=%.6f avgDiff=%.6f (Go: mean=%.4f std=%.4f, Py: mean=%.4f std=%.4f)",
		name, maxD, avgD, mean(goData), std(goData), mean(pyData), std(pyData))
	return
}

// TestCompareLayerEmbedding tests embedding with the same GGUF checkpoint.
func TestCompareLayerEmbedding(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}
	pyRef := loadRef(t, "ref_01_embedding")

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	phoneIDs := []int{0, 49, 0, 127, 0, 70, 0, 80, 0}
	T := len(phoneIDs)
	hidden := hp.HiddenChannels
	sqrtH := float32(math.Sqrt(float64(hidden)))

	goEmb := make([]float32, hidden*T)
	for ti := 0; ti < T; ti++ {
		pid := phoneIDs[ti]
		for h := 0; h < hidden; h++ {
			v := w.TextEnc.Emb[pid*hidden+h]
			if w.TextEnc.ToneEmb != nil {
				v += w.TextEnc.ToneEmb[0*hidden+h] // tone=0
			}
			if w.TextEnc.LangEmb != nil {
				v += w.TextEnc.LangEmb[2*hidden+h] // lang=EN=2
			}
			goEmb[h*T+ti] = v * sqrtH
		}
	}

	maxD, _ := report(t, "embedding", goEmb, pyRef)
	if maxD > 1e-4 {
		t.Errorf("embedding diff too large: %.6f", maxD)
	}
}

// TestCompareLayerDurationPredictor tests the duration predictor output.
func TestCompareLayerDurationPredictor(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	// Load the encoder final output as input to duration predictor
	pyEncFinal := loadRef(t, "ref_04_enc_final")
	pyLogw := loadRef(t, "ref_07_logw")
	pySpeakerEmb := loadRef(t, "ref_00_speaker_emb")

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	T := 9
	hidden := hp.HiddenChannels

	// Use Python's encoder output as input (to isolate duration predictor)
	goLogw := DurationPredictorForward(pyEncFinal, hidden, T, pySpeakerEmb, hp.GinChannels, &w.DurPred)

	maxD, _ := report(t, "logw", goLogw, pyLogw)
	if maxD > 0.1 {
		t.Errorf("logw diff too large: %.6f", maxD)
		for i := 0; i < len(goLogw) && i < len(pyLogw); i++ {
			t.Logf("  [%d] Go=%.6f Py=%.6f diff=%.6f", i, goLogw[i], pyLogw[i], goLogw[i]-pyLogw[i])
		}
	}

	// Compare durations
	pyDurations := loadRef(t, "ref_08_durations")
	goDurations, goTMel := ComputeDurations(goLogw, 1.0)
	t.Logf("Go durations: %v (T_mel=%d)", goDurations, goTMel)

	pyDurInts := make([]int, len(pyDurations))
	pyTMel := 0
	for i, d := range pyDurations {
		pyDurInts[i] = int(d)
		pyTMel += int(d)
	}
	t.Logf("Py durations: %v (T_mel=%d)", pyDurInts, pyTMel)

	if goTMel != pyTMel {
		t.Errorf("T_mel mismatch: Go=%d Py=%d", goTMel, pyTMel)
	}
}

// TestCompareLayerEncoderLayer0 tests the first encoder layer output.
func TestCompareLayerEncoderLayer0(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	pyRef := loadRef(t, "ref_03_enc_layer0")
	pyEmb := loadRef(t, "ref_02_after_spk_cond") // input to encoder layers

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	T := 9
	hidden := hp.HiddenChannels

	// Run one encoder layer using Python's post-spk-cond embedding as input
	x := make([]float32, len(pyEmb))
	copy(x, pyEmb)

	// Speaker embedding for conditioning
	pySpeakerEmb := loadRef(t, "ref_00_speaker_emb")
	_ = pySpeakerEmb

	x = encoderLayerForward(x, hidden, T, &w.TextEnc.Layers[0], hp)

	maxD, _ := report(t, "enc_layer0", x, pyRef)
	if maxD > 1.0 {
		t.Errorf("enc_layer0 diff too large: %.6f", maxD)
		// Print first few values
		for i := 0; i < 10 && i < len(x); i++ {
			fmt.Printf("  [%d] Go=%.6f Py=%.6f\n", i, x[i], pyRef[i])
		}
	}
}

// TestCompareFullPipeline runs the full pipeline with the test GGUF and compares durations.
func TestCompareFullPipeline(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	phoneIDs := []int{0, 49, 0, 127, 0, 70, 0, 80, 0}
	toneIDs := make([]int, len(phoneIDs))
	langIDs := make([]int, len(phoneIDs))
	for i := range langIDs {
		langIDs[i] = LangEN
	}
	T := len(phoneIDs)

	// Step 1: Speaker embedding
	g := make([]float32, hp.GinChannels)
	copy(g, w.SpeakerEmb[0:hp.GinChannels])
	t.Logf("Speaker emb: mean=%.4f std=%.4f", mean(g), std(g))

	// Step 2: Text Encoder
	x, mP, logsP, _ := TextEncoderForward(phoneIDs, toneIDs, langIDs, g, hp.GinChannels, nil, nil, &w.TextEnc, hp)
	t.Logf("Encoder out: mean=%.4f std=%.4f", mean(x), std(x))
	t.Logf("m_p: mean=%.4f std=%.4f", mean(mP), std(mP))
	t.Logf("logs_p: mean=%.4f std=%.4f", mean(logsP), std(logsP))

	// Step 3: Duration
	logw := DurationPredictorForward(x, hp.HiddenChannels, T, g, hp.GinChannels, &w.DurPred)
	durations, tMel := ComputeDurations(logw, 1.0)
	t.Logf("Durations: %v, T_mel=%d", durations, tMel)

	// Step 4: Expand
	path, _ := GeneratePath(durations)
	mPExp := ExpandByDurations(mP, hp.InterChannels, T, path, tMel)
	logsPExp := ExpandByDurations(logsP, hp.InterChannels, T, path, tMel)
	t.Logf("m_p expanded: mean=%.4f std=%.4f", mean(mPExp), std(mPExp))

	// Step 5: Sample
	zP := make([]float32, hp.InterChannels*tMel)
	for i := range zP {
		lp := logsPExp[i]
		if lp > 20 {
			lp = 20
		}
		zP[i] = mPExp[i] + float32(math.Sin(float64(i)*0.1))*0.667*float32(math.Exp(float64(lp)))
	}
	t.Logf("z_p: mean=%.4f std=%.4f max=%.4f", mean(zP), std(zP), maxAbs(zP))

	// Step 6: Flow reverse
	z := FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)
	t.Logf("z (after flow): mean=%.4f std=%.4f max=%.4f", mean(z), std(z), maxAbs(z))

	// Step 7: HiFi-GAN
	audio := HiFiGANForward(z, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)
	t.Logf("Audio: %d samples, mean=%.6f std=%.6f max=%.6f",
		len(audio), mean(audio), std(audio), maxAbs(audio))

	// Check vocoder weights
	t.Logf("Vocoder.ConvPre: outCh=%d inCh=%d kSize=%d weightLen=%d",
		w.Vocoder.ConvPre.OutCh, w.Vocoder.ConvPre.InCh, w.Vocoder.ConvPre.KSize,
		len(w.Vocoder.ConvPre.Weight))
	for i, up := range w.Vocoder.Ups {
		t.Logf("Vocoder.Ups[%d]: inCh=%d outCh=%d kSize=%d weightLen=%d maxW=%.4f",
			i, up.InCh, up.OutCh, up.KSize, len(up.Weight), maxAbs(up.Weight))
	}

	wavData := EncodeWAV(audio, hp.SampleRate)
	outPath := filepath.Join("testdata", "go_hello_en.wav")
	os.WriteFile(outPath, wavData, 0644)
	t.Logf("Saved: %s", outPath)
}

func maxAbs(x []float32) float32 {
	var m float32
	for _, v := range x {
		a := float32(math.Abs(float64(v)))
		if a > m {
			m = a
		}
	}
	return m
}
