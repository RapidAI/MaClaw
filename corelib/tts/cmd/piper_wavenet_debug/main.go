package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	// Load ONNX z_p
	zpData, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_piper_z_p.bin"))
	inter := 192
	tMel := 114
	zp := make([]float32, inter*tMel)
	for i := range zp {
		zp[i] = math.Float32frombits(binary.LittleEndian.Uint32(zpData[i*4 : (i+1)*4]))
	}

	// Step 1: Flip channels
	z := make([]float32, len(zp))
	copy(z, zp)
	tts.FlipChannels(z, inter, tMel)
	fmt.Printf("After flip ch0[:5]: [%.8f, %.8f, %.8f, %.8f, %.8f]\n",
		z[0], z[1], z[2], z[3], z[4])
	fmt.Printf("Python ref:         [0.12792143, 0.12792143, 0.12792143, 0.12792143, 0.14487204]\n")

	// Step 2: Split
	halfCh := inter / 2 // 96
	x0 := z[:halfCh*tMel]
	_ = z[halfCh*tMel:] // x1, used later

	// Step 3: Pre conv
	// flows.6 is Layers[3] in Go (loaded at index 3 from flowLayerIndices=[0,2,4,6])
	layer := &model.W.Flow.Layers[3] // flows.6
	hp := model.HP
	hidden := hp.HiddenChannels

	h := tts.Conv1D(x0, halfCh, tMel, layer.Pre.Weight, layer.Pre.KSize, hidden, 1,
		(layer.Pre.KSize-1)/2, layer.Pre.Bias)

	var preRMS float64
	for _, v := range h {
		preRMS += float64(v) * float64(v)
	}
	preRMS = math.Sqrt(preRMS / float64(len(h)))
	fmt.Printf("\nAfter pre: RMS=%.6f (Python: 0.177104)\n", preRMS)
	fmt.Printf("  first 5: [%.8f, %.8f, %.8f, %.8f, %.8f]\n", h[0], h[1], h[2], h[3], h[4])
	fmt.Printf("  Python:  [-0.01617797, -0.01617797, -0.01617797, -0.01617797, -0.05419935]\n")

	// Step 4: WaveNet layer 0 - in_layers conv
	wn := &layer.WN[0]
	kSize := wn.InLayer.KSize
	if kSize == 0 {
		kSize = 5
	}
	fmt.Printf("\nWN layer 0: InLayer OutCh=%d InCh=%d KSize=%d, weight len=%d\n",
		wn.InLayer.OutCh, wn.InLayer.InCh, wn.InLayer.KSize, len(wn.InLayer.Weight))

	acts := tts.Conv1D(h, hidden, tMel, wn.InLayer.Weight, kSize, hidden*2, 1, 2, wn.InLayer.Bias)
	var actsRMS float64
	for _, v := range acts {
		actsRMS += float64(v) * float64(v)
	}
	actsRMS = math.Sqrt(actsRMS / float64(len(acts)))
	fmt.Printf("in_layers.0 output: RMS=%.6f (Python: 1.896510)\n", actsRMS)

	// Gated activation
	gated := make([]float32, hidden*tMel)
	for c := 0; c < hidden; c++ {
		for t := 0; t < tMel; t++ {
			tVal := float32(math.Tanh(float64(acts[c*tMel+t])))
			sVal := float32(1.0 / (1.0 + math.Exp(float64(-acts[(hidden+c)*tMel+t]))))
			gated[c*tMel+t] = tVal * sVal
		}
	}
	var gatedRMS float64
	for _, v := range gated {
		gatedRMS += float64(v) * float64(v)
	}
	gatedRMS = math.Sqrt(gatedRMS / float64(len(gated)))
	fmt.Printf("Gated: RMS=%.6f (Python: 0.177086)\n", gatedRMS)
}
