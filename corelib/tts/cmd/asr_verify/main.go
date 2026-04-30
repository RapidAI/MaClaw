// ASR verify existing WAV files.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

func main() {
	asrModel, err := asr.NewMoonshine("RapidSpeech.cpp/models/gguf/moonshine-base-zh.gguf")
	if err != nil {
		fmt.Printf("ASR load error: %v\n", err)
		os.Exit(1)
	}
	defer asrModel.Close()

	dir := "corelib/tts/testdata"
	files, _ := filepath.Glob(filepath.Join(dir, "aishell3_*.wav"))
	files2, _ := filepath.Glob(filepath.Join(dir, "melotts_zh_ref_*.wav"))
	files = append(files, files2...)
	files3, _ := filepath.Glob(filepath.Join(dir, "go_mixed_demo.wav"))
	files = append(files, files3...)
	for _, f := range files {
		pcm, err := asr.LoadWAV(f)
		if err != nil {
			fmt.Printf("%-40s ERROR: %v\n", filepath.Base(f), err)
			continue
		}
		text, err := asrModel.Transcribe(pcm)
		if err != nil {
			fmt.Printf("%-40s ASR ERROR: %v\n", filepath.Base(f), err)
			continue
		}
		base := filepath.Base(f)
		expected := strings.TrimPrefix(base, "aishell3_")
		expected = strings.TrimSuffix(expected, ".wav")
		fmt.Printf("%-40s → %q\n", base, text)
	}
}
