package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	lexPath := filepath.Join("corelib", "tts", "testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, _ := tts.NewPiper(modelPath, lexPath)

	g2p := tts.PiperTextToPhonemesWithLexicon("人工智能正在改变世界，让我们一起来探索未来的无限可能", model.Lex)
	input := tts.PiperSynthesizeInput{PhonemeIDs: g2p.PhonemeIDs}

	// Warmup
	model.Synthesize(input)

	// Profile
	f, _ := os.Create("cpu.prof")
	pprof.StartCPUProfile(f)

	t0 := time.Now()
	for i := 0; i < 3; i++ {
		model.Synthesize(input)
	}
	elapsed := time.Since(t0)
	pprof.StopCPUProfile()
	f.Close()

	fmt.Printf("3 runs in %v\n", elapsed)
	fmt.Println("Run: go tool pprof -top cpu.prof")
}
