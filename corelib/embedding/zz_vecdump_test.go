package embedding

// Audit helper (skipped unless ZZ_VD_MODEL is set): embeds pre-formatted texts
// verbatim with the production pure-Go runner and writes raw float32 vectors,
// so prompt/instruction variants can be compared offline without re-running Go.
//
//	ZZ_VD_MODEL=<gguf> ZZ_VD_IN=<jsonl {"k":key,"t":text}> ZZ_VD_OUT=<prefix> go test -run TestZZVecDump
//
// Output: <prefix>.f32 (N×dim little-endian float32, L2-normalised as Embed
// returns them) and <prefix>.keys (one key per line, same order).

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestZZVecDump(t *testing.T) {
	model := os.Getenv("ZZ_VD_MODEL")
	if model == "" {
		t.Skip("ZZ_VD_MODEL not set")
	}
	in, out := os.Getenv("ZZ_VD_IN"), os.Getenv("ZZ_VD_OUT")
	emb, err := NewEmbedderFromFile(model, DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var keys, texts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct{ K, T string }
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, r.K)
		texts = append(texts, r.T)
	}
	vf, err := os.Create(out + ".f32")
	if err != nil {
		t.Fatal(err)
	}
	defer vf.Close()
	kf, err := os.Create(out + ".keys")
	if err != nil {
		t.Fatal(err)
	}
	defer kf.Close()
	w := bufio.NewWriter(vf)
	defer w.Flush()
	buf := make([]byte, 4)
	for i := 0; i < len(texts); i += 32 {
		j := min(i+32, len(texts))
		vecs, err := emb.EmbedBatch(texts[i:j])
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range vecs {
			for _, x := range v {
				binary.LittleEndian.PutUint32(buf, math.Float32bits(x))
				w.Write(buf)
			}
			kf.WriteString(keys[i+k] + "\n")
		}
		if i%640 == 0 {
			t.Logf("%d/%d", j, len(texts))
		}
	}
}
