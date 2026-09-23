// Command kokoro_bench benchmarks Kokoro TTS synthesis end-to-end.
// It loads the model once, then synthesizes the same utterance N times and
// reports per-iteration latency, median/min, and real-time factor (RTF).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

func main() {
	assets := flag.String("assets", "", "directory containing config.json, kokoro-v1_0.koro and voices/")
	voiceID := flag.String("voice", "zm_yunxi", "voice name")
	text := flag.String("text", "你好，欢迎来到 MacLaw。让我们一起构建伟大的作品。这是一段语音合成性能的测试。", "text to synthesize")
	n := flag.Int("n", 10, "iterations (after warmup)")
	warmup := flag.Int("warmup", 2, "warmup iterations")
	cpuprofile := flag.String("cpuprofile", "", "write CPU profile to file")
	flag.Parse()

	if *assets == "" {
		fmt.Fprintln(os.Stderr, "-assets is required")
		os.Exit(1)
	}

	model, err := kokoro.LoadModel(kokoro.Assets{
		ConfigPath:  filepath.Join(*assets, "config.json"),
		WeightsPath: filepath.Join(*assets, "kokoro-v1_0.koro"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "load model:", err)
		os.Exit(1)
	}
	voice, err := model.LoadVoice(filepath.Join(*assets, "voices"), *voiceID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load voice:", err)
		os.Exit(1)
	}

	phonemes := tts.KokoroTextToPhonemes(*text)
	if phonemes == "" {
		fmt.Fprintln(os.Stderr, "text produced no phonemes")
		os.Exit(1)
	}
	fmt.Printf("phonemes: %d chars\n", len(phonemes))

	var pcmLen int
	for i := 0; i < *warmup; i++ {
		pcm, err := model.SynthesizePhonemes(phonemes, voice, 1)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warmup synth:", err)
			os.Exit(1)
		}
		pcmLen = len(pcm)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	durs := make([]time.Duration, 0, *n)
	var pcm []float32
	for i := 0; i < *n; i++ {
		t0 := time.Now()
		pcm, err = model.SynthesizePhonemes(phonemes, voice, 1)
		if err != nil {
			fmt.Fprintln(os.Stderr, "synth:", err)
			os.Exit(1)
		}
		durs = append(durs, time.Since(t0))
		pcmLen = len(pcm)
	}

	sorted := make([]time.Duration, len(durs))
	copy(sorted, durs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total time.Duration
	for _, d := range durs {
		total += d
	}
	audioSec := float64(pcmLen) / float64(kokoro.DefaultSampleRate)
	median := sorted[len(sorted)/2]
	fmt.Printf("audio: %.2fs (%d samples @ %d Hz)\n", audioSec, pcmLen, kokoro.DefaultSampleRate)
	fmt.Printf("iterations: %d  total: %v  avg: %v\n", len(durs), total, total/time.Duration(len(durs)))
	fmt.Printf("median: %v  min: %v  max: %v\n", median, sorted[0], sorted[len(sorted)-1])
	fmt.Printf("RTF(median): %.3f  RTF(min): %.3f\n",
		median.Seconds()/audioSec, sorted[0].Seconds()/audioSec)
}
