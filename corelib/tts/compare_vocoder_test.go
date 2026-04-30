package tts

import (
	"os"
	"testing"
)

// TestCompareVocoderLayerByLayer tests each upsample layer individually.
func TestCompareVocoderLayerByLayer(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Test resblock 0 step by step using Python's ups_0 output as input
	pyUps0 := loadRef(t, "ref_voc_03_ups_0")
	pyConvs1_0 := loadRef(t, "ref_rb0_convs1_0")
	pyAfterPair0 := loadRef(t, "ref_rb0_after_pair_0")

	ch := 256
	T := 248
	rb := &w.Vocoder.ResBlocks[0]

	// Step 1: LeakyReLU → dilated Conv1d (dilation=1)
	x := make([]float32, len(pyUps0))
	copy(x, pyUps0)
	LeakyReLU(x, lreluSlope)

	c1 := &rb.Convs1[0]
	dilation := 1
	padding := (c1.KSize - 1) * dilation / 2
	t.Logf("convs1[0]: outCh=%d inCh=%d kSize=%d dilation=%d padding=%d",
		c1.OutCh, c1.InCh, c1.KSize, dilation, padding)

	y := conv1DDilated(x, ch, T, c1.Weight, c1.Bias, c1.KSize, ch, 1, padding, dilation)

	maxD, _ := report(t, "rb0_convs1_0", y, pyConvs1_0)
	if maxD > 1.0 {
		t.Errorf("rb0_convs1_0 diff: %.6f", maxD)
		for i := 0; i < 10; i++ {
			t.Logf("  [%d] Go=%.6f Py=%.6f", i, y[i], pyConvs1_0[i])
		}
	}

	// Continue: LeakyReLU → Conv1d → residual
	LeakyReLU(y, lreluSlope)
	c2 := &rb.Convs2[0]
	padding2 := (c2.KSize - 1) / 2
	z := Conv1D(y, ch, T, c2.Weight, c2.KSize, ch, 1, padding2, c2.Bias)

	// Residual: x_orig + z
	xOrig := make([]float32, len(pyUps0))
	copy(xOrig, pyUps0)
	result := make([]float32, len(xOrig))
	for i := range result {
		result[i] = xOrig[i] + z[i]
	}

	maxD, _ = report(t, "rb0_after_pair_0", result, pyAfterPair0)
	if maxD > 1.0 {
		t.Errorf("rb0_after_pair_0 diff: %.6f", maxD)
	}

	// Full resblock 0
	pyRb0Out := loadRef(t, "ref_rb0_output")
	xFull := make([]float32, len(pyUps0))
	copy(xFull, pyUps0)
	xFull = ResBlock1Forward(xFull, ch, T, rb, []int{1, 3, 5})

	maxD, _ = report(t, "rb0_full_output", xFull, pyRb0Out)
	if maxD > 1.0 {
		t.Errorf("rb0 full output diff: %.6f", maxD)
	}

	// Average of 3 resblocks
	pyRbAvg := loadRef(t, "ref_rb_avg_after_ups0")
	nResKernels := len(hp.ResblockKernelSizes)
	var sum []float32
	for j := 0; j < nResKernels; j++ {
		rbJ := &w.Vocoder.ResBlocks[j]
		dilations := hp.ResblockDilationSizes[j]
		xClone := make([]float32, len(pyUps0))
		copy(xClone, pyUps0)
		xClone = ResBlock1Forward(xClone, ch, T, rbJ, dilations)
		t.Logf("  rb_%d: mean=%.4f std=%.4f", j, mean(xClone), std(xClone))
		if sum == nil {
			sum = xClone
		} else {
			for k := range sum {
				sum[k] += xClone[k]
			}
		}
	}
	for k := range sum {
		sum[k] /= float32(nResKernels)
	}

	maxD, _ = report(t, "rb_avg_after_ups0", sum, pyRbAvg)
	if maxD > 1.0 {
		t.Errorf("rb avg diff: %.6f", maxD)
	}
}
