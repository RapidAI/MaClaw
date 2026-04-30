package tts

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestCompareOfficialEncoder compares Go encoder output with official enc_p output.
func TestCompareOfficialEncoder(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	// Load official encoder outputs (from compare_encoder_outputs.py)
	pyMp := loadRef(t, "ref_official_m_p")
	pyLogsP := loadRef(t, "ref_official_logs_p")
	pyEncOut := loadRef(t, "ref_official_enc_out")

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	phoneIDs := []int{0, 49, 0, 127, 0, 70, 0, 80, 0}
	toneIDs := make([]int, len(phoneIDs))
	langIDs := make([]int, len(phoneIDs))
	for i := range langIDs {
		if i%2 == 1 {
			langIDs[i] = LangEN
		}
	}

	g := make([]float32, hp.GinChannels)
	copy(g, w.SpeakerEmb[0:hp.GinChannels])

	goEncOut, goMp, goLogsP, _ := TextEncoderForward(
		phoneIDs, toneIDs, langIDs, g, hp.GinChannels, nil, nil, &w.TextEnc, hp)

	report(t, "enc_out", goEncOut, pyEncOut)
	maxD, _ := report(t, "m_p", goMp, pyMp)
	report(t, "logs_p", goLogsP, pyLogsP)

	if maxD > 5.0 {
		t.Errorf("m_p diff too large: %.4f", maxD)
	}

	// If encoder is reasonably close, run full pipeline
	if maxD < 5.0 {
		T := len(phoneIDs)
		logw := DurationPredictorForward(goEncOut, hp.HiddenChannels, T, g, hp.GinChannels, &w.DurPred)
		durations, tMel := ComputeDurations(logw, 1.0)
		t.Logf("Durations: %v, T_mel=%d", durations, tMel)

		path, _ := GeneratePath(durations)
		mPExp := ExpandByDurations(goMp, hp.InterChannels, T, path, tMel)
		logsPExp := ExpandByDurations(goLogsP, hp.InterChannels, T, path, tMel)

		// Sample with noise
		zP := make([]float32, hp.InterChannels*tMel)
		RandnScale(zP, 1.0)
		for i := range zP {
			lp := logsPExp[i]
			if lp > 10 {
				lp = 10
			} else if lp < -20 {
				lp = -20
			}
			zP[i] = mPExp[i] + zP[i]*0.667*float32(math.Exp(float64(lp)))
		}
		t.Logf("z_p: mean=%.4f, std=%.4f, max=%.4f", mean(zP), std(zP), maxAbs(zP))

		z := FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)
		t.Logf("z: mean=%.4f, std=%.4f, max=%.4f", mean(z), std(z), maxAbs(z))

		audio := HiFiGANForward(z, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)
		t.Logf("Audio: %d samples, max=%.4f, std=%.4f", len(audio), maxAbs(audio), std(audio))

		wavData := EncodeWAV(audio, 44100)
		outPath := filepath.Join("testdata", "go_official_hello.wav")
		os.WriteFile(outPath, wavData, 0644)
		t.Logf("Saved: %s", outPath)
	}
}
