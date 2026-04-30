// Compare Go flow with official Python flow output.
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
	pyZOut := loadBin("corelib/tts/testdata/ref_official_flow_z_out.bin")
	pyG := loadBin("corelib/tts/testdata/ref_official_flow_g.bin")

	if pyZP == nil || pyZOut == nil {
		fmt.Println("Missing reference files")
		os.Exit(1)
	}

	hp := tts.DefaultHParams()
	hp.SampleRate = 44100
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil {
		fmt.Printf("Load error: %v\n", err)
		os.Exit(1)
	}

	tMel := len(pyZP) / 192
	fmt.Printf("T_mel=%d\n", tMel)
	fmt.Printf("Python z_p: mean=%.4f std=%.4f\n", mean(pyZP), stdev(pyZP))
	fmt.Printf("Python z_out: mean=%.4f std=%.4f\n", mean(pyZOut), stdev(pyZOut))

	// Run Go flow
	goZ := make([]float32, len(pyZP))
	copy(goZ, pyZP)
	goZ = tts.FlowReverseForward(goZ, 192, tMel, pyG, hp.GinChannels, &w.Flow, hp)

	fmt.Printf("Go z_out: mean=%.4f std=%.4f\n", mean(goZ), stdev(goZ))

	maxD, avgD := compare(goZ, pyZOut)
	fmt.Printf("Diff: maxDiff=%.6f avgDiff=%.6f\n", maxD, avgD)

	// Show first few diffs
	for i := 0; i < 20 && i < len(goZ); i++ {
		d := goZ[i] - pyZOut[i]
		if math.Abs(float64(d)) > 0.01 {
			fmt.Printf("  [%d] Go=%.6f Py=%.6f diff=%.6f\n", i, goZ[i], pyZOut[i], d)
		}
	}
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path)
	if err != nil { return nil }
	n := len(data) / 4
	r := make([]float32, n)
	for i := 0; i < n; i++ {
		r[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return r
}

func mean(x []float32) float32 {
	var s float32
	for _, v := range x { s += v }
	return s / float32(len(x))
}

func stdev(x []float32) float32 {
	m := mean(x)
	var s float32
	for _, v := range x { d := v - m; s += d * d }
	return float32(math.Sqrt(float64(s / float32(len(x)))))
}

func compare(a, b []float32) (maxD, avgD float32) {
	var sum float32
	for i := range a {
		d := float32(math.Abs(float64(a[i] - b[i])))
		if d > maxD { maxD = d }
		sum += d
	}
	return maxD, sum / float32(len(a))
}
