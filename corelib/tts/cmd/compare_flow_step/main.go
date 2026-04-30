package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	pyZP := loadBin("corelib/tts/testdata/ref_official_flow_z_p.bin")
	pyStep6 := loadBin("corelib/tts/testdata/ref_official_flow_step_6.bin")
	pyG := loadBin("corelib/tts/testdata/ref_official_flow_g.bin")

	hp := tts.DefaultHParams()
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil { fmt.Printf("Load error: %v\n", err); os.Exit(1) }

	tMel := 33; inter := 192

	// Step 1: Flip (last coupling layer first in reverse)
	z := make([]float32, len(pyZP))
	copy(z, pyZP)
	tts.FlipChannels(z, inter, tMel)

	// Step 2: Coupling layer 3 reverse
	z = tts.CouplingLayerReverseExported(z, inter, tMel, pyG, hp.GinChannels, &w.Flow.Layers[3], hp)

	compare("coupling_3_output", z, pyStep6)
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path); if err != nil { return nil }
	n := len(data) / 4; r := make([]float32, n)
	for i := 0; i < n; i++ { r[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:])) }
	return r
}

func compare(name string, go_, py []float32) {
	if len(go_) != len(py) { fmt.Printf("%-30s SIZE: Go=%d Py=%d\n", name, len(go_), len(py)); return }
	var maxD, sumD float32
	for i := range go_ { d := float32(math.Abs(float64(go_[i]-py[i]))); if d > maxD { maxD = d }; sumD += d }
	fmt.Printf("%-30s maxDiff=%.6f avgDiff=%.6f\n", name, maxD, sumD/float32(len(go_)))
}
