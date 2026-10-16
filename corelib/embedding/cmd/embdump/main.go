// Command embdump dumps every intermediate activation of the reference Go
// Gemma embedding forward pass for one text, so the C++ port can be diffed
// stage by stage.
//
// Usage:
//
//	embdump -model <path.gguf> -dim 768 -text "hello world" -out trace_go.bin
//	embdump -model <path.gguf> -dim 768 -text-file some.txt -out trace_go.bin
//
// -text-file reads the text as raw UTF-8 bytes and is preferred for non-ASCII
// input: the Windows command line is not a safe transport for it, because a C
// runtime narrows argv through the ANSI code page (see the C++ port's defect
// #17).  Go's own os.Args is correct, but reading from a file keeps both sides
// of the diff independent of the shell.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func main() {
	model := flag.String("model", "", "path to embeddinggemma-300M-Q8_0.gguf")
	dim := flag.Int("dim", 768, "output embedding dim")
	text := flag.String("text", "hello world", "text to trace")
	textFile := flag.String("text-file", "", "read the text from this file instead (raw bytes)")
	out := flag.String("out", "trace_go.bin", "output trace file")
	flag.Parse()

	if *textFile != "" {
		b, err := os.ReadFile(*textFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read text-file failed:", err)
			os.Exit(1)
		}
		// Match the port's -dump-text-file: strip trailing CR/LF so the two
		// transports are interchangeable.
		*text = strings.TrimRight(string(b), "\r\n")
	}

	if *model == "" {
		home, _ := os.UserHomeDir()
		*model = home + "/.maclaw/models/embeddinggemma-300M-Q8_0.gguf"
	}

	emb, err := embedding.NewGemmaEmbedder(*model, *dim)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load failed:", err)
		os.Exit(1)
	}
	defer emb.Close()

	hp := emb.HParams()
	fmt.Printf("hparams: dim=%d layers=%d nHeads=%d nKVHeads=%d headDim=%d kvDim=%d ffDim=%d vocab=%d maxSeq=%d eps=%g theta=%g\n",
		hp.Dim, hp.NLayers, hp.NHeads, hp.NKVHeads, hp.HeadDim, hp.KVDim, hp.FFDim, hp.VocabSize, hp.MaxSeqLen, hp.RMSNormEps, hp.RopeTheta)

	ids := emb.TokenIDs(*text)
	fmt.Printf("tokens: %v\n", ids)

	if err := emb.DumpForwardTrace(ids, *out); err != nil {
		fmt.Fprintln(os.Stderr, "dump failed:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote trace to %s\n", *out)
}
