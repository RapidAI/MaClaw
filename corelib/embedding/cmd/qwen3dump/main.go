// qwen3dump embeds JSONL texts with the pure Go Qwen3 embedder.
//
//	qwen3dump -model X.gguf -in in.jsonl -out out.jsonl [-role classification|query|none] [-tokens]
//
// Input rows: {"id":..., "text":...}. Output rows: {"id", "tokens"?, "vec"}.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func main() {
	model := flag.String("model", "", "GGUF path")
	in := flag.String("in", "", "input jsonl")
	out := flag.String("out", "", "output jsonl")
	role := flag.String("role", "none", "none|query|document|classification|similarity")
	tokOnly := flag.Bool("tokens-only", false, "only tokenize")
	withTok := flag.Bool("tokens", false, "include token ids")
	batch := flag.Int("batch", 64, "batch size")
	flag.Parse()
	t0 := time.Now()
	emb, err := embedding.NewQwen3Embedder(*model, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "load %.2fs id=%s\n", time.Since(t0).Seconds(), emb.ModelID())
	r := map[string]embedding.Role{"none": embedding.RoleNone, "query": embedding.RoleQuery, "document": embedding.RoleDocument,
		"classification": embedding.RoleClassification, "similarity": embedding.RoleSimilarity}[*role]
	type row struct {
		ID   any    `json:"id"`
		Text string `json:"text"`
	}
	var rows []row
	f, err := os.Open(*in)
	if err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var x row
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			panic(err)
		}
		rows = append(rows, x)
	}
	o, _ := os.Create(*out)
	w := bufio.NewWriter(o)
	enc := json.NewEncoder(w)
	t1 := time.Now()
	for s := 0; s < len(rows); s += *batch {
		e := s + *batch
		if e > len(rows) {
			e = len(rows)
		}
		texts := make([]string, e-s)
		for i := range texts {
			texts[i] = rows[s+i].Text
		}
		var vecs [][]float32
		if !*tokOnly {
			vecs, err = emb.EmbedBatchWithRole(texts, r)
			if err != nil {
				panic(err)
			}
		}
		for i := range texts {
			m := map[string]any{"id": rows[s+i].ID}
			if *withTok || *tokOnly {
				m["tokens"] = emb.Tokenize(embedding.ApplyQwen3Prompt(texts[i], r))
			}
			if vecs != nil {
				m["vec"] = vecs[i]
			}
			enc.Encode(m)
		}
		fmt.Fprintf(os.Stderr, "%d/%d %.1fs\n", e, len(rows), time.Since(t1).Seconds())
	}
	w.Flush()
	o.Close()
}
