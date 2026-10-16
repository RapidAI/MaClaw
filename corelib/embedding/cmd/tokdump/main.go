package main

import (
	"fmt"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

func main() {
	mf, err := gguf.OpenMmap(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer mf.CloseMmap()
	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	scores := gguf.LastF32Array()
	fmt.Println("len(tokens)=", len(tokens), "len(scores)=", len(scores))
	for _, id := range []int{270, 1902, 23391, 29104, 0, 1, 2} {
		fmt.Printf("  scores[%d] = %v   token=%q\n", id, scores[id], tokens[id])
	}
}
