package embedding

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

// TestZZPooledDump (env-gated, local only): ZZ_POOL_IN jsonl {id,text} ->
// ZZ_POOL_OUT.f32 (n x 2*dim: last|mean, classification role) + .ids, and
// per-call latency percentiles for EmbedPooledWithRole vs EmbedWithRole.
func TestZZPooledDump(t *testing.T) {
	in, out := os.Getenv("ZZ_POOL_IN"), os.Getenv("ZZ_POOL_OUT")
	if in == "" || out == "" {
		t.Skip("ZZ_POOL_IN/ZZ_POOL_OUT not set")
	}
	e := loadQwen3(t)
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	vf, _ := os.Create(out + ".f32")
	idf, _ := os.Create(out + ".ids")
	defer vf.Close()
	defer idf.Close()
	bw := bufio.NewWriter(vf)
	defer bw.Flush()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var tp, tl []float64
	lat := os.Getenv("ZZ_POOL_LAT") == "1"
	_, _, _ = e.EmbedPooledWithRole("warm", RoleClassification)
	for sc.Scan() {
		var r struct{ ID, Text string }
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		t0 := time.Now()
		last, mean, err := e.EmbedPooledWithRole(r.Text, RoleClassification)
		if err != nil {
			t.Fatal(err)
		}
		tp = append(tp, float64(time.Since(t0).Microseconds())/1000)
		if lat {
			t1 := time.Now()
			_, _ = e.EmbedWithRole(r.Text, RoleClassification)
			tl = append(tl, float64(time.Since(t1).Microseconds())/1000)
		}
		binary.Write(bw, binary.LittleEndian, last)
		binary.Write(bw, binary.LittleEndian, mean)
		idf.WriteString(r.ID + "\n")
	}
	pc := func(a []float64) []float64 {
		if len(a) == 0 {
			return nil
		}
		b := append([]float64(nil), a...)
		sort.Float64s(b)
		return []float64{b[len(b)/2], b[len(b)*9/10], b[len(b)*99/100]}
	}
	t.Logf("n %d pooled ms p50/p90/p99 %v; last-only EmbedWithRole %v; compute %s", len(tp), pc(tp), pc(tl), EmbedderComputeMode(e))
}
