package agent_test

// Replace-audit harness (local only): dumps the raw signals the production
// topic-switch detector, IM interrupt relevance and coding-subagent scoring
// compute, for one embedding model. Skipped unless ZZ_CAP3_MODEL is set.
//
//	ZZ_CAP3_MODEL=<gguf> ZZ_CAP3_DIR=<dir with topic/interrupt/coding.jsonl> ZZ_CAP3_TAG=qwen3|gemma \
//	  go test ./corelib/agent -run TestZZCap3Dump -count=1 -timeout 2h

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/viterin/vek/vek32"
)

func zzReadJSONL(t *testing.T, p string) []map[string]any {
	f, err := os.Open(p)
	if err != nil {
		t.Logf("skip %s: %v", p, err)
		return nil
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func zzStrs(v any) []string {
	var s []string
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if xs, ok := x.(string); ok {
				s = append(s, xs)
			}
		}
	}
	return s
}

// copies of guiapp/coding_subagent_skills.go extractBigrams / bigramJaccard
func zzBigrams(text string) map[string]bool {
	r := []rune(text)
	if len(r) < 2 {
		return nil
	}
	set := make(map[string]bool, len(r)-1)
	for i := 0; i < len(r)-1; i++ {
		set[string(r[i:i+2])] = true
	}
	return set
}

func zzJaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(n) / float64(len(a)+len(b)-n)
}

func TestZZCap3Dump(t *testing.T) {
	model := os.Getenv("ZZ_CAP3_MODEL")
	if model == "" {
		t.Skip("ZZ_CAP3_MODEL not set")
	}
	dir, tag := os.Getenv("ZZ_CAP3_DIR"), os.Getenv("ZZ_CAP3_TAG")
	emb, err := embedding.NewEmbedderFromFile(model, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	// ZZ_CAP3_ROLES: "" = production (plain Embed on both sides);
	// "sim" = RoleSimilarity both sides; "qd" = RoleQuery for the new
	// message / task side, RoleDocument for the context / task-desc / doc side.
	roles := os.Getenv("ZZ_CAP3_ROLES")
	if roles != "" {
		tag += "_" + roles
	}
	cache := map[string][]float32{}
	EA := func(s string, role embedding.Role) []float32 {
		k := fmt.Sprint(int(role), "|", s)
		if v, ok := cache[k]; ok {
			return v
		}
		var v []float32
		var err error
		if role == embedding.RoleNone {
			v, err = emb.Embed(s)
		} else {
			v, err = embedding.EmbedAs(emb, s, role)
		}
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		cache[k] = v
		return v
	}
	side := func(query bool) embedding.Role {
		switch roles {
		case "sim":
			return embedding.RoleSimilarity
		case "qd":
			if query {
				return embedding.RoleQuery
			}
			return embedding.RoleDocument
		}
		return embedding.RoleNone
	}
	E := func(s string) []float32 { return EA(s, embedding.RoleNone) }
	_ = E
	write := func(name string, rows []map[string]any) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		for _, r := range rows {
			enc.Encode(r)
		}
	}
	// --- topic: corelib/agent TopicSwitchDetector.Detect context construction ---
	var out []map[string]any
	for _, it := range zzReadJSONL(t, filepath.Join(dir, "topic.jsonl")) {
		hist := zzStrs(it["history"])
		all := append([]string{}, hist...)
		if a, _ := it["assistant_last"].(string); a != "" {
			all = append(all, a)
		}
		if len(all) > 8 {
			all = all[len(all)-8:]
		}
		ctx := strings.Join(all, "\n")
		msg, _ := it["new"].(string)
		idx := bm25.New()
		idx.Rebuild([]bm25.Doc{{ID: "ctx", Text: ctx}})
		raw := idx.Score(msg)["ctx"]
		cv, mv := EA(agent.TruncateRunes(ctx, 512), side(false)), EA(agent.TruncateRunes(msg, 512), side(true))
		out = append(out, map[string]any{"id": it["id"], "label": it["label"], "bm25": raw,
			"cos": float64(vek32.Dot(cv, mv)), "words": agent.CountWords(msg), "user_turns": len(hist)})
	}
	write("sig_topic_"+tag+".jsonl", out)
	// --- interrupt: guiapp/im_interrupt_handler.go relevance ---
	out = nil
	for _, it := range zzReadJSONL(t, filepath.Join(dir, "interrupt.jsonl")) {
		task, _ := it["task"].(string)
		msg, _ := it["message"].(string)
		out = append(out, map[string]any{"id": it["id"], "label": it["label"],
			"rel": progress.CosineSimilarity(EA(task, side(false)), EA(msg, side(true))), "words": agent.CountWords(msg)})
	}
	write("sig_interrupt_"+tag+".jsonl", out)
	// --- coding subagent: guiapp/coding_subagent_scoring.go scoreAndSelectTopK signals (MCP doc form) ---
	out = nil
	var cands []struct{ Name, Description string }
	if b, err := os.ReadFile(filepath.Join(dir, "coding_cands.json")); err == nil {
		json.Unmarshal(b, &cands)
	}
	docs := make([]string, len(cands))
	for i, c := range cands {
		docs[i] = c.Name + " " + c.Description
	}
	var dvec [][]float32
	if len(docs) > 0 {
		if side(false) == embedding.RoleNone {
			dvec, err = emb.EmbedBatch(docs)
		} else {
			dvec, err = embedding.EmbedBatchAs(emb, docs, side(false))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range zzReadJSONL(t, filepath.Join(dir, "coding.jsonl")) {
		task, _ := it["task"].(string)
		qt := cskill.TokenizeSimple(task)
		qb := zzBigrams(strings.ToLower(task))
		qv := EA(task, side(true))
		bm, bg, cs := make([]float64, len(docs)), make([]float64, len(docs)), make([]float64, len(docs))
		for i, d := range docs {
			bm[i] = cskill.BM25ScoreSimple(qt, cskill.TokenizeSimple(d))
			bg[i] = zzJaccard(qb, zzBigrams(strings.ToLower(d)))
			cs[i] = progress.CosineSimilarity(qv, dvec[i])
		}
		out = append(out, map[string]any{"id": it["id"], "gold": it["gold"], "bm25": bm, "bigram": bg, "cos": cs})
	}
	write("sig_coding_"+tag+".jsonl", out)
	t.Logf("%s: dumped (cache %d)", tag, len(cache))
}
