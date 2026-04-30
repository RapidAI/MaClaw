package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	lexPath := filepath.Join("corelib", "tts", "testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, _ := tts.NewPiper(modelPath, lexPath)

	// Just "世界" with exact same phoneme IDs as ONNX test
	// ^ sh i 4 _ j ie 4 $
	pids := []int64{1, 20, 39, 67, 0, 15, 41, 67, 2}

	audio, err := model.Synthesize(tts.PiperSynthesizeInput{PhonemeIDs: pids})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	wav := tts.EncodeWAV(audio, model.HP.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_piper_世界_alone.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Go 世界: %d samples, %.2fs\n", len(audio), float64(len(audio))/22050)
}
