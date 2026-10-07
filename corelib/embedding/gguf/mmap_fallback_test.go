package gguf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeTinyGGUF writes a v3 GGUF with one string KV and one F32 tensor.
func writeTinyGGUF(t *testing.T, vals []float32) string {
	t.Helper()
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	ws := func(s string) { w(uint64(len(s))); b.WriteString(s) }
	b.WriteString("GGUF")
	w(uint32(3))
	w(uint64(1)) // tensors
	w(uint64(1)) // kvs
	ws("general.architecture")
	w(uint32(8)) // string
	ws("tiny")
	ws("t.weight")
	w(uint32(1))
	w(uint64(len(vals)))
	w(uint32(TypeF32))
	w(uint64(0))
	for b.Len()%32 != 0 {
		b.WriteByte(0)
	}
	for _, v := range vals {
		w(math.Float32bits(v))
	}
	p := filepath.Join(t.TempDir(), "tiny.gguf")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenMmapFallsBackToHeapWhenMappingFails(t *testing.T) {
	vals := []float32{1, -2, 3.5, 4, 5, 6, 7, 8}
	p := writeTinyGGUF(t, vals)

	mf, err := OpenMmap(p)
	if err != nil {
		t.Fatalf("mmap open: %v", err)
	}
	if mf.Heap() {
		t.Fatal("normal open should be mapped")
	}
	mf.CloseMmap()

	orig := mmapFileFn
	mmapFileFn = func(*os.File, int) ([]byte, uintptr, error) { return nil, 0, errors.New("ENOMEM (simulated)") }
	defer func() { mmapFileFn = orig }()
	mf, err = OpenMmap(p)
	if err != nil {
		t.Fatalf("heap fallback open: %v", err)
	}
	defer mf.CloseMmap()
	if !mf.Heap() {
		t.Fatal("expected heap fallback")
	}
	if got := GetMetaStr(mf.Meta, "general.architecture"); got != "tiny" {
		t.Fatalf("arch = %q", got)
	}
	raw, _, err := mf.TensorRawBytes("t.weight")
	if err != nil {
		t.Fatal(err)
	}
	mf.Release(raw) // must not zero heap pages
	got, err := mf.TensorF32("t.weight")
	if err != nil {
		t.Fatal(err)
	}
	for i := range vals {
		if got[i] != vals[i] {
			t.Fatalf("t.weight[%d] = %v, want %v", i, got[i], vals[i])
		}
	}
}

func TestOpenMmapNoMmapEnv(t *testing.T) {
	p := writeTinyGGUF(t, []float32{1, 2, 3, 4})
	t.Setenv(NoMmapEnv, "1")
	mf, err := OpenMmap(p)
	if err != nil {
		t.Fatal(err)
	}
	defer mf.CloseMmap()
	if !mf.Heap() {
		t.Fatalf("%s=1 should read into heap", NoMmapEnv)
	}
}

func TestOpenMmapTruncatedFileErrorsNotPanics(t *testing.T) {
	p := writeTinyGGUF(t, []float32{1, 2, 3, 4})
	b, _ := os.ReadFile(p)
	os.WriteFile(p, b[:20], 0o644)
	if mf, err := OpenMmap(p); err == nil {
		mf.CloseMmap()
		t.Fatal("truncated header should fail")
	}
}
