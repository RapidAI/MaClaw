package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

func main() {
	root := filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
	model, err := kokoro.LoadModel(kokoro.Assets{
		ConfigPath: filepath.Join(root, "config.json"),
		WeightsPath: filepath.Join(root, "kokoro-v1_0.koro"),
	})
	if err != nil { log.Fatal(err) }
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil { log.Fatal(err) }
	t0 := time.Now()
	pcm, err := model.SynthesizePhonemes("a", voice, 1)
	if err != nil { log.Fatal(err) }
	out := `D:\workprj\aicoder\tts_eval\kokoro_go_assets\go_synth_a.wav`
	if err := kokoro.WriteWAV(out, pcm, kokoro.DefaultSampleRate); err != nil { log.Fatal(err) }
	fmt.Printf("wrote %s samples=%d duration=%.3fs elapsed=%s\n", out, len(pcm), float64(len(pcm))/kokoro.DefaultSampleRate, time.Since(t0))
}
