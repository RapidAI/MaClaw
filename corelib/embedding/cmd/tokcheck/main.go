// Command tokcheck tokenizes each '\n'-separated line of a file with the
// reference Go tokenizer and prints the ids, so the C++ port's tokenizer can be
// diffed against it on adversarial input.
//
// Usage:
//
//	go build -o build/tokcheck ./corelib/embedding/cmd/tokcheck
//	./build/tokcheck -model <path.gguf> -in <path>
//
// Output format matches `embc.exe -encode-file <path>`: "<line>: id id id".
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

func main() {
	model := flag.String("model", "", "path to the .gguf model")
	in := flag.String("in", "", "file of \\n-separated texts (raw bytes)")
	flag.Parse()
	if *model == "" || *in == "" {
		fmt.Fprintln(os.Stderr, "usage: tokcheck -model <path.gguf> -in <path>")
		os.Exit(2)
	}

	mf, err := gguf.OpenMmap(*model)
	if err != nil {
		panic(err)
	}
	defer mf.CloseMmap()
	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	scores := gguf.LastF32Array()
	tk := embedding.LoadTokenizerFromGGUF(tokens, scores)

	raw, err := os.ReadFile(*in)
	if err != nil {
		panic(err)
	}
	// Same splitting rule as the C++ side: split on '\n', and a trailing '\n'
	// still yields the final (empty) field only if there is content after it.
	// Both sides iterate "from start to next '\n' or EOF", so they agree.
	start := 0
	line := 0
	for {
		nl := strings.IndexByte(string(raw[start:]), '\n')
		var text string
		if nl < 0 {
			text = string(raw[start:])
		} else {
			text = string(raw[start : start+nl])
		}
		ids := tk.Encode(text)
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf("%d", id))
		}
		fmt.Printf("%d: %s\n", line, strings.Join(parts, " "))
		line++
		if nl < 0 {
			break
		}
		start += nl + 1
	}
}
