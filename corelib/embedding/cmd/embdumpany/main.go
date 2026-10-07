// embdumpany embeds JSONL texts with whichever pure-Go embedder the GGUF
// selects (EmbeddingGemma or Qwen3-Embedding) under a chosen role, for
// model-vs-model retrieval audits. Local evaluation tool.
//
//	embdumpany -model X.gguf -in in.jsonl -out out.jsonl [-role query|document|classification|similarity|none] [-batch 32]
//
// Input rows: {"id":..., "text":...}. Output rows: {"id", "vec"} (float32 as JSON numbers).
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
	roleS := flag.String("role", "none", "none|query|document|classification|similarity")
	batch := flag.Int("batch", 32, "batch size")
	flag.Parse()
	role, ok := map[string]embedding.Role{"none": embedding.RoleNone, "query": embedding.RoleQuery, "document": embedding.RoleDocument,
		"classification": embedding.RoleClassification, "similarity": embedding.RoleSimilarity}[*roleS]
	if !ok {
		fmt.Fprintln(os.Stderr, "bad role", *roleS)
		os.Exit(2)
	}
	t0 := time.Now()
	emb, err := embedding.NewEmbedderFromFile(*model, embedding.DefaultEmbeddingDim)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	id := ""
	if m, ok := emb.(interface{ ModelID() string }); ok {
		id = m.ModelID()
	}
	fmt.Fprintf(os.Stderr, "load %.2fs %T id=%s dim=%d role=%s\n", time.Since(t0).Seconds(), emb, id, emb.Dim(), *roleS)
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
	f.Close()
	o, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	w := bufio.NewWriter(o)
	enc := json.NewEncoder(w)
	t1 := time.Now()
	for i := 0; i < len(rows); i += *batch {
		j := i + *batch
		if j > len(rows) {
			j = len(rows)
		}
		texts := make([]string, 0, j-i)
		for _, r := range rows[i:j] {
			texts = append(texts, r.Text)
		}
		vecs, err := embedding.EmbedBatchAs(emb, texts, role)
		if err != nil {
			panic(err)
		}
		for k, v := range vecs {
			_ = enc.Encode(map[string]any{"id": rows[i+k].ID, "vec": v})
		}
	}
	w.Flush()
	o.Close()
	fmt.Fprintf(os.Stderr, "embedded %d in %.1fs\n", len(rows), time.Since(t1).Seconds())
}
