package tts

import (
	"os"
	"testing"
)

func TestCompareFlowReverse(t *testing.T) {
	requireTTSReferenceCompare(t)
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	// Load Python reference
	pyZP := loadRef(t, "ref_flow_01_z_p")
	pyZFinal := loadRef(t, "ref_flow_04_z_final")
	pyAfterFlip3 := loadRef(t, "ref_flow_02_after_flip_3")
	pyAfterCoupling3 := loadRef(t, "ref_flow_03_after_coupling_3")
	pySpeakerEmb := loadRef(t, "ref_00_speaker_emb")

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	tMel := 31
	inter := hp.InterChannels

	// Test 1: Flip operation
	zFlipped := make([]float32, len(pyZP))
	copy(zFlipped, pyZP)
	FlipChannels(zFlipped, inter, tMel)

	maxD, _ := report(t, "flip_3 (last→first)", zFlipped, pyAfterFlip3)
	if maxD > 1e-4 {
		t.Errorf("flip diff too large: %.6f", maxD)
	}

	// Test 2: Single coupling layer reverse (layer 3)
	// Input: pyAfterFlip3, expected output: pyAfterCoupling3
	goInput := make([]float32, len(pyAfterFlip3))
	copy(goInput, pyAfterFlip3)

	goOut := couplingLayerReverse(goInput, inter, tMel, pySpeakerEmb, hp.GinChannels,
		&w.Flow.Layers[3], hp)

	maxD, _ = report(t, "coupling_3_reverse", goOut, pyAfterCoupling3)
	if maxD > 1.0 {
		t.Errorf("coupling_3 diff too large: %.6f", maxD)
	}

	// Test 3: Full flow reverse
	goZ := make([]float32, len(pyZP))
	copy(goZ, pyZP)
	goZ = FlowReverseForward(goZ, inter, tMel, pySpeakerEmb, hp.GinChannels, &w.Flow, hp)

	maxD, _ = report(t, "flow_reverse_full", goZ, pyZFinal)
	if maxD > 5.0 {
		t.Errorf("flow full diff too large: %.6f", maxD)
	}
}

func TestCompareVocoderConvPre(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	pyZFinal := loadRef(t, "ref_flow_04_z_final")
	pyConvPre := loadRef(t, "ref_flow_05_vocoder_conv_pre")

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	tMel := 31
	// Apply conv_pre
	goConvPre := Conv1D(pyZFinal, hp.InterChannels, tMel,
		w.Vocoder.ConvPre.Weight, w.Vocoder.ConvPre.KSize, w.Vocoder.ConvPre.OutCh, 1,
		(w.Vocoder.ConvPre.KSize-1)/2, w.Vocoder.ConvPre.Bias)

	maxD, _ := report(t, "vocoder_conv_pre", goConvPre, pyConvPre)
	if maxD > 1.0 {
		t.Errorf("vocoder conv_pre diff too large: %.6f", maxD)
	}
}
