// Command kokoro_asr_eval synthesizes Mandarin Kokoro samples and verifies them
// with the local Moonshine ASR model.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

type sample struct {
	Name string
	Text string
}

func main() {
	defaultModel := firstExisting(
		"tts_eval/kokoro_go_assets_q8/kokoro-v1_0.koro",
		"tts_eval/kokoro_go_assets/kokoro-v1_0.koro",
	)
	defaultVoiceDir := firstExisting(
		"tts_eval/kokoro_go_assets_q8/voices",
		"tts_eval/kokoro_go_assets/voices",
	)
	defaultASR := firstExisting(
		"RapidSpeech.cpp/models/gguf/moonshine-base-zh.gguf",
		"RapidSpeech.cpp/models/gguf/moonshine-base.gguf",
		"RapidSpeech.cpp/models/gguf/moonshine-tiny.gguf",
	)
	modelPath := flag.String("model", defaultModel, "Kokoro .koro model path")
	voiceDir := flag.String("voices", defaultVoiceDir, "Kokoro voice directory")
	asrPath := flag.String("asr", defaultASR, "Moonshine ASR .gguf model path")
	voiceID := flag.String("voice", "zf_xiaoyi", "Kokoro voice ID")
	outDir := flag.String("out", filepath.Join("tts_eval", "kokoro_asr_eval"), "output directory for WAV files")
	flag.Parse()
	if *modelPath == "" || *voiceDir == "" || *asrPath == "" {
		fmt.Fprintf(os.Stderr, "missing assets: kokoro=%q voices=%q asr=%q\n", *modelPath, *voiceDir, *asrPath)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir: %v\n", err)
		os.Exit(1)
	}

	mgr := tts.NewKokoroManager(*modelPath, *voiceDir, *voiceID)
	defer mgr.Unload()
	asrModel, err := asr.NewMoonshine(*asrPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load ASR: %v\n", err)
		os.Exit(1)
	}
	defer asrModel.Close()

	samples := []sample{
		{Name: "safe_quan", Text: "\u8bf7\u6ce8\u610f\u5b89\u5168\u6743\u9650\u5168\u90e8\u6b63\u5e38"},
		{Name: "word_bao", Text: "\u62a5\u544a"},
		{Name: "word_pao", Text: "\u8dd1\u6b65"},
		{Name: "word_dao", Text: "\u5230\u8fbe"},
		{Name: "word_tao", Text: "\u5916\u5957"},
		{Name: "word_guo", Text: "\u901a\u8fc7"},
		{Name: "word_ke", Text: "\u8bfe\u7a0b"},
		{Name: "bp_minimal", Text: "\u7238\u7238\u628a\u62a5\u544a\u53d1\u7ed9\u5a46\u5a46\u8dd1\u6b65"},
		{Name: "bp_natural", Text: "\u8bf7\u628a\u62a5\u544a\u4fdd\u5b58\u597d\u4e0d\u8981\u8dd1\u504f"},
		{Name: "dt_minimal", Text: "\u5230\u8fbe\u5927\u5385\u4ee5\u540e\u5957\u4e0a\u5916\u5957"},
		{Name: "dt_natural", Text: "\u5230\u8fbe\u5927\u5385\u4ee5\u540e\u8bf7\u6253\u5f00\u5916\u5957"},
		{Name: "gk_minimal", Text: "\u54e5\u54e5\u521a\u521a\u6253\u5f00\u8bfe\u7a0b\u8bfe\u4ef6"},
		{Name: "jqx_umlaut", Text: "\u6743\u9650\u9700\u8981\u786e\u8ba4\u7fa4\u4f17\u9009\u9879"},
		{Name: "poly_sleep", Text: "\u6211\u8981\u7761\u89c9\u4e86"},
		{Name: "poly_jue", Text: "\u6211\u89c9\u5f97\u53ef\u4ee5"},
		{Name: "poly_music", Text: "\u8bf7\u64ad\u653e\u97f3\u4e50"},
		{Name: "poly_bank", Text: "\u94f6\u884c\u884c\u4e1a\u6b63\u5728\u91cd\u65b0\u589e\u957f"},
		{Name: "mixed_security", Text: "\u5b89\u5168\u7b56\u7565\u4fdd\u62a4\u5e73\u53f0\u4e0d\u8981\u8dd1\u504f"},
	}

	fmt.Printf("TTS model: %s\n", *modelPath)
	fmt.Printf("ASR model: %s\n", *asrPath)
	fmt.Printf("Voice: %s\n", *voiceID)
	fmt.Printf("%-16s %-32s %-32s %s\n", "case", "expected", "asr", "wav")
	for _, s := range samples {
		wav, err := mgr.SynthesizeText(s.Text)
		if err != nil {
			fmt.Printf("%-16s ERROR synthesize: %v\n", s.Name, err)
			continue
		}
		wavPath := filepath.Join(*outDir, s.Name+".wav")
		if err := os.WriteFile(wavPath, wav, 0o644); err != nil {
			fmt.Printf("%-16s ERROR write wav: %v\n", s.Name, err)
			continue
		}
		pcm, err := asr.WAVToFloat32(wav)
		if err != nil {
			fmt.Printf("%-16s ERROR decode wav: %v\n", s.Name, err)
			continue
		}
		got, err := asrModel.Transcribe(pcm)
		if err != nil {
			fmt.Printf("%-16s ERROR asr: %v\n", s.Name, err)
			continue
		}
		fmt.Printf("%-16s %-32s %-32s %s\n", s.Name, truncateRunes(s.Text, 16), truncateRunes(got, 16), wavPath)
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			if info.IsDir() || info.Size() > 0 {
				return p
			}
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
