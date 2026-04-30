package tts

import (
	"os"
	"testing"
)

func TestCompareWeightNormReconstruction(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Compare ups[0] weight
	pyUps0W := loadRef(t, "ref_ups0_weight")
	goUps0W := w.Vocoder.Ups[0].Weight
	t.Logf("ups[0] weight: Go len=%d, Py len=%d", len(goUps0W), len(pyUps0W))
	if len(goUps0W) == len(pyUps0W) {
		maxD, _ := report(t, "ups0_weight", goUps0W, pyUps0W)
		if maxD > 0.001 {
			t.Errorf("ups0 weight diff: %.6f", maxD)
		}
	} else {
		t.Errorf("ups0 weight size mismatch: Go=%d Py=%d", len(goUps0W), len(pyUps0W))
	}

	// Compare resblock 0 convs1[0] weight
	pyRbW := loadRef(t, "ref_resblock0_convs1_0_weight")
	goRbW := w.Vocoder.ResBlocks[0].Convs1[0].Weight
	t.Logf("resblock0.convs1[0] weight: Go len=%d, Py len=%d", len(goRbW), len(pyRbW))
	if len(goRbW) == len(pyRbW) {
		maxD, _ := report(t, "rb0_convs1_0_weight", goRbW, pyRbW)
		if maxD > 0.001 {
			t.Errorf("resblock weight diff: %.6f", maxD)
		}
	} else {
		t.Errorf("resblock weight size mismatch: Go=%d Py=%d", len(goRbW), len(pyRbW))
	}

	// Compare resblock 0 convs1[0] bias
	pyRbB := loadRef(t, "ref_resblock0_convs1_0_bias")
	goRbB := w.Vocoder.ResBlocks[0].Convs1[0].Bias
	if len(goRbB) == len(pyRbB) {
		maxD, _ := report(t, "rb0_convs1_0_bias", goRbB, pyRbB)
		if maxD > 0.001 {
			t.Errorf("resblock bias diff: %.6f", maxD)
		}
	}
}
