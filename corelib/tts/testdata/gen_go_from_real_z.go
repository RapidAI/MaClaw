// +build ignore

// Quick test: load Python's real z, run Go vocoder, save WAV.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	// Load Python's real z (from official model inference)
	zData, err := loadBin("corelib/tts/testdata/ref_real_z_ns0.bin")
	if err != nil {
		fmt.Println("Error loading z:", err)
		os.Exit(1)
	}
	fmt.Printf("z: %d elements, mean=%.4f, std=%.4f\n", len(zData), mean(zData), std(zData))

	// Load speaker embedding
	gData, err := loadBin("corelib/tts/testdata/ref_00_speaker_emb.bin")
	if err != nil {
		fmt.Println("Error loading g:", err)
		os.Exit(1)
	}

	// Load model
	hp := tts.DefaultHParams()
	hp.SampleRate = 44100
	hp.HopLength = 512
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil {
		fmt.Println("Error loading weights:", err)
		os.Exit(1)
	}

	// z shape: [192, 33]
	tMel := len(zData) / hp.InterChannels
	fmt.Printf("T_mel=%d, interCh=%d\n", tMel, hp.InterChannels)

	// Run vocoder
	audio := tts.HiFiGANForward(zData, hp.InterChannels, tMel,
		gData, hp.GinChannels, &w.Vocoder, hp)

	fmt.Printf("Audio: %d samples (%.2f sec at %d Hz), max=%.4f\n",
		len(audio), float64(len(audio))/float64(hp.SampleRate), hp.SampleRate,
		maxAbs(audio))

	// Save WAV
	wavData := tts.EncodeWAV(audio, hp.SampleRate)
	os.WriteFile("corelib/tts/testdata/go_from_real_z.wav", wavData, 0644)
	fmt.Println("Saved: corelib/tts/testdata/go_from_real_z.wav")
}

func loadBin(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return result, nil
}

func mean(x []float32) float32 {
	var s float32
	for _, v := range x {
		s += v
	}
	return s / float32(len(x))
}

func std(x []float32) float32 {
	m := mean(x)
	var s float32
	for _, v := range x {
		d := v - m
		s += d * d
	}
	return float32(math.Sqrt(float64(s / float32(len(x)))))
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
