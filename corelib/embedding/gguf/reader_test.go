package gguf

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBufferedHeaderKeepsDataOffset(t *testing.T) {
	for _, n := range []int{3, 100000} {
		dir := t.TempDir()
		path := filepath.Join(dir, "hdr.gguf")
		payload := []float32{1.25, -2, 3.5, 0.5}
		headerEnd := writeTensorGGUF(t, path, n, payload)
		gf, err := Open(path)
		if err != nil {
			t.Fatalf("n=%d open: %v", n, err)
		}
		if gf.dataOffset != align32(headerEnd) {
			t.Fatalf("n=%d dataOffset=%d want %d", n, gf.dataOffset, align32(headerEnd))
		}
		got, err := gf.ReadTensorF32("bias")
		gf.Close()
		if err != nil {
			t.Fatalf("n=%d read: %v", n, err)
		}
		if len(got) != len(payload) {
			t.Fatalf("n=%d len %d", n, len(got))
		}
		for i := range payload {
			if got[i] != payload[i] {
				t.Fatalf("n=%d [%d]=%g want %g", n, i, got[i], payload[i])
			}
		}
		toks := GetMetaStrArr(gf.Meta, "tokenizer.ggml.tokens")
		// Close cleared nothing in Meta; Get after Close is fine, Meta is in memory.
		if len(toks) != n || toks[0] != "aa" || toks[n-1] != "aa" {
			t.Fatalf("n=%d tokens=%d first=%q", n, len(toks), toks[0])
		}
	}
}

func TestRealModelHeaderParse(t *testing.T) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".maclaw", "models", "embeddinggemma-300M-Q8_0.gguf")
	if _, err := os.Stat(path); err != nil {
		t.Skip("no gemma embedding model")
	}
	start := time.Now()
	gf, err := Open(path)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	defer gf.Close()
	arch := GetMetaStr(gf.Meta, "general.architecture")
	toks := GetMetaStrArr(gf.Meta, "tokenizer.ggml.tokens")
	t.Logf("header %s arch=%s tokens=%d dataOffset=%d", elapsed, arch, len(toks), gf.dataOffset)
	if arch != "gemma-embedding" || len(toks) != 262144 {
		t.Fatalf("arch=%s tokens=%d", arch, len(toks))
	}
	if _, ok := gf.Tensors["token_embd.weight"]; !ok {
		t.Fatal("missing token_embd.weight")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("header parse %s", elapsed)
	}
}

func writeTensorGGUF(t *testing.T, path string, nTokens int, payload []float32) int64 {
	t.Helper()
	var buf []byte
	putU32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	putU64 := func(v uint64) { buf = binary.LittleEndian.AppendUint64(buf, v) }
	putStr := func(s string) {
		putU64(uint64(len(s)))
		buf = append(buf, s...)
	}
	putU32(Magic)
	putU32(Version)
	putU64(1) // one tensor
	putU64(1) // one meta pair
	putStr("tokenizer.ggml.tokens")
	putU32(9) // array
	putU32(8) // string
	putU64(uint64(nTokens))
	for i := 0; i < nTokens; i++ {
		putStr("aa")
	}
	putStr("bias")
	putU32(1) // nDims
	putU64(uint64(len(payload)))
	putU32(TypeF32)
	putU64(0) // offset within data section
	headerEnd := int64(len(buf))
	for align32(int64(len(buf))) != int64(len(buf)) {
		buf = append(buf, 0)
	}
	for _, v := range payload {
		buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return headerEnd
}
