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

	// Load ONNX reference z_p
	zpData, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_piper_z_p.bin"))
	inter := 192
	tMel := 114
	onnxZP := make([]float32, inter*tMel)
	for i := range onnxZP {
		onnxZP[i] = math.Float32frombits(binary.LittleEndian.Uint32(zpData[i*4 : (i+1)*4]))
	}

	var zpRMS float64
	for _, v := range onnxZP {
		zpRMS += float64(v) * float64(v)
	}
	zpRMS = math.Sqrt(zpRMS / float64(len(onnxZP)))
	fmt.Printf("ONNX z_p: RMS=%.6f, first 5: [%.6f, %.6f, %.6f, %.6f, %.6f]\n",
		zpRMS, onnxZP[0], onnxZP[1], onnxZP[2], onnxZP[3], onnxZP[4])

	// Run Go flow decoder on ONNX z_p
	hp := model.HP
	goZ := tts.PiperFlowReverseForward(onnxZP, inter, tMel, &model.W.Flow, hp)

	var zRMS float64
	for _, v := range goZ {
		zRMS += float64(v) * float64(v)
	}
	zRMS = math.Sqrt(zRMS / float64(len(goZ)))
	fmt.Printf("Go z (from ONNX z_p): RMS=%.6f, first 5: [%.6f, %.6f, %.6f, %.6f, %.6f]\n",
		zRMS, goZ[0], goZ[1], goZ[2], goZ[3], goZ[4])

	// Load ONNX reference z for comparison
	zData, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_piper_z.bin"))
	onnxZ := make([]float32, inter*tMel)
	for i := range onnxZ {
		onnxZ[i] = math.Float32frombits(binary.LittleEndian.Uint32(zData[i*4 : (i+1)*4]))
	}
	var onnxZRMS float64
	for _, v := range onnxZ {
		onnxZRMS += float64(v) * float64(v)
	}
	onnxZRMS = math.Sqrt(onnxZRMS / float64(len(onnxZ)))
	fmt.Printf("ONNX z:               RMS=%.6f, first 5: [%.6f, %.6f, %.6f, %.6f, %.6f]\n",
		onnxZRMS, onnxZ[0], onnxZ[1], onnxZ[2], onnxZ[3], onnxZ[4])

	// Compare
	var maxDiff float64
	var sumDiff float64
	for i := range goZ {
		d := math.Abs(float64(goZ[i]) - float64(onnxZ[i]))
		if d > maxDiff {
			maxDiff = d
		}
		sumDiff += d
	}
	meanDiff := sumDiff / float64(len(goZ))
	fmt.Printf("\nGo vs ONNX z: maxDiff=%.6f, meanDiff=%.6f\n", maxDiff, meanDiff)
}
