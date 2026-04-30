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
	zPath := filepath.Join("corelib", "tts", "testdata", "ref_piper_z.bin")

	// Load model
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load model: %v\n", err)
		os.Exit(1)
	}

	// Load reference z from ONNX
	zData, err := os.ReadFile(zPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read z: %v\n", err)
		os.Exit(1)
	}

	// z shape: [192, 114]
	inter := 192
	tMel := 114
	z := make([]float32, inter*tMel)
	for i := range z {
		z[i] = math.Float32frombits(binary.LittleEndian.Uint32(zData[i*4 : (i+1)*4]))
	}

	var zRMS float64
	for _, v := range z {
		zRMS += float64(v) * float64(v)
	}
	zRMS = math.Sqrt(zRMS / float64(len(z)))
	fmt.Printf("Loaded z: [%d, %d], RMS=%.6f\n", inter, tMel, zRMS)
	fmt.Printf("z first 5: [%.6f, %.6f, %.6f, %.6f, %.6f]\n", z[0], z[1], z[2], z[3], z[4])

	// Run vocoder only
	hp := model.HP
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &model.W.Vocoder, hp)

	var rms, peak float64
	for _, v := range audio {
		rms += float64(v) * float64(v)
		if math.Abs(float64(v)) > peak {
			peak = math.Abs(float64(v))
		}
	}
	rms = math.Sqrt(rms / float64(len(audio)))
	fmt.Printf("\nGo vocoder output: %d samples, RMS=%.6f, peak=%.6f\n", len(audio), rms, peak)
	fmt.Printf("Expected (ONNX):   29184 samples, RMS=0.054818, peak=0.441546\n")

	// Save WAV
	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_piper_vocoder_from_onnx_z.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Saved: %s\n", wavPath)
}
