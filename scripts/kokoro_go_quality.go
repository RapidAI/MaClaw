package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

type metaFile struct {
	Text     string `json:"text"`
	Voice    string `json:"voice"`
	Phonemes string `json:"phonemes"`
}

func main() {
	root := filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
	if envRoot := os.Getenv("KOKORO_GO_ASSETS"); envRoot != "" {
		root = filepath.Clean(envRoot)
	}
	metaPath := filepath.Join(root, "quality_zh_short_meta.json")
	if envMeta := os.Getenv("KOKORO_QUALITY_META"); envMeta != "" {
		metaPath = filepath.Clean(envMeta)
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		log.Fatal(err)
	}
	var meta metaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		log.Fatal(err)
	}
	model, err := kokoro.LoadModel(kokoro.Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		log.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), meta.Voice)
	if err != nil {
		log.Fatal(err)
	}
	repeat := 1
	if envRepeat := os.Getenv("KOKORO_QUALITY_REPEAT"); envRepeat != "" {
		if n, err := strconv.Atoi(envRepeat); err == nil && n > 0 {
			repeat = n
		}
	}
	var pcm []float32
	var elapsed time.Duration
	for i := 0; i < repeat; i++ {
		t0 := time.Now()
		pcm, err = model.SynthesizePhonemes(meta.Phonemes, voice, 1)
		elapsed = time.Since(t0)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("run=%d/%d elapsed=%s\n", i+1, repeat, elapsed)
	}
	out := filepath.Join(root, "quality_go_zh_short.wav")
	if err := kokoro.WriteWAV(out, pcm, kokoro.DefaultSampleRate); err != nil {
		log.Fatal(err)
	}
	peak := float32(0)
	ss := float64(0)
	for _, v := range pcm {
		av := v
		if av < 0 {
			av = -av
		}
		if av > peak {
			peak = av
		}
		ss += float64(v * v)
	}
	rms := math.Sqrt(ss / float64(len(pcm)))
	fmt.Printf("wrote %s\ntext=%s\nvoice=%s\nphonemes=%s\nsamples=%d duration=%.3fs last_elapsed=%s peak=%.4f rms=%.4f\n", out, meta.Text, meta.Voice, meta.Phonemes, len(pcm), float64(len(pcm))/kokoro.DefaultSampleRate, elapsed, peak, rms)
}
