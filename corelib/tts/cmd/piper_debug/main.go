package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")

	// Load GGUF directly to check embedding values
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open GGUF: %v\n", err)
		os.Exit(1)
	}

	// Check sid (embedding) tensor
	sid, err := mf.TensorF32("sid")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load sid: %v\n", err)
		os.Exit(1)
	}
	ti := mf.Tensors["sid"]
	fmt.Printf("sid: dims=%v, len=%d\n", ti.Dims, len(sid))

	// Print first 5 values for phoneme IDs 0, 1, 10, 39
	hidden := 192
	for _, pid := range []int{0, 1, 10, 39} {
		start := pid * hidden
		fmt.Printf("  sid[%d] first 5: [%.8f, %.8f, %.8f, %.8f, %.8f]\n",
			pid, sid[start], sid[start+1], sid[start+2], sid[start+3], sid[start+4])
	}

	// Check GGUF tensor layout: is it [256, 192] row-major or [192, 256]?
	fmt.Printf("\nsid tensor info: NDims=%d, Dims=%v\n", ti.NDims, ti.Dims[:ti.NDims])
	// If Dims=[256, 192], then row-major means sid[pid] = sid[pid*192 : (pid+1)*192]
	// If Dims=[192, 256], then sid[pid] = sid[pid : 256*192 : 256] (column-major)

	// The ONNX model stores it as [256, 192] (vocab_size, hidden_channels)
	// GGUF should preserve this layout
	fmt.Printf("Expected: sid[1] first 5 = [-0.00158594 -0.00744048 -0.00463532 -0.09191663  0.04346777]\n")
	fmt.Printf("Got:      sid[1] first 5 = [%.8f, %.8f, %.8f, %.8f, %.8f]\n",
		sid[1*hidden+0], sid[1*hidden+1], sid[1*hidden+2], sid[1*hidden+3], sid[1*hidden+4])

	// Check if the layout might be transposed
	fmt.Printf("\nAlternative (transposed) sid[1] first 5 = [%.8f, %.8f, %.8f, %.8f, %.8f]\n",
		sid[1], sid[256+1], sid[512+1], sid[768+1], sid[1024+1])

	mf.CloseMmap()

	// Now test the full model
	fmt.Println("\n=== Full model test ===")
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load model: %v\n", err)
		os.Exit(1)
	}

	// Check encoder output for "你好世界"
	g2p := tts.PiperTextToPhonemes("你好世界")
	fmt.Printf("Phoneme IDs: %v\n", g2p.PhonemeIDs)

	audio, err := model.Synthesize(tts.PiperSynthesizeInput{
		PhonemeIDs: g2p.PhonemeIDs,
		NoiseScale: 0.0, // deterministic
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Synthesis failed: %v\n", err)
		os.Exit(1)
	}

	var rms float64
	for _, v := range audio {
		rms += float64(v) * float64(v)
	}
	rms = math.Sqrt(rms / float64(len(audio)))
	fmt.Printf("Audio: %d samples, RMS=%.6f\n", len(audio), rms)
}
