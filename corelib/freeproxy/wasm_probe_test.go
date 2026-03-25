package freeproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const wasmURL = "https://ai.dangbei.com/_next/static/media/sign_bg.6ff3d844.wasm"

func downloadWasm(t *testing.T) []byte {
	t.Helper()
	home, _ := os.UserHomeDir()
	cachePath := filepath.Join(home, ".maclaw", "freeproxy", "sign_bg.wasm")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 1000 {
		t.Logf("Using cached WASM: %s (%d bytes)", cachePath, len(data))
		return data
	}
	t.Logf("Downloading WASM from %s ...", wasmURL)
	resp, err := http.Get(wasmURL)
	if err != nil {
		t.Fatalf("Download WASM: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Read WASM: %v", err)
	}
	t.Logf("Downloaded %d bytes", len(data))
	os.MkdirAll(filepath.Dir(cachePath), 0700)
	os.WriteFile(cachePath, data, 0600)
	return data
}

func TestWasmProbeExports(t *testing.T) {
	wasmBytes := downloadWasm(t)

	// --- Part 1: Raw binary parsing for export names ---
	t.Log("=== RAW WASM SECTIONS ===")
	pos := 8 // skip magic + version
	for pos < len(wasmBytes) {
		sid := wasmBytes[pos]
		pos++
		sz, n := binary.Uvarint(wasmBytes[pos:])
		if n <= 0 {
			break
		}
		pos += n
		end := pos + int(sz)
		if end > len(wasmBytes) {
			break
		}
		snames := []string{"custom", "type", "import", "function", "table", "memory", "global", "export", "start", "element", "code", "data"}
		sn := fmt.Sprintf("id=%d", sid)
		if int(sid) < len(snames) {
			sn = snames[sid]
		}
		t.Logf("Section %s size=%d", sn, sz)
		if sid == 7 { // export
			cur := pos
			cnt, cn := binary.Uvarint(wasmBytes[cur:])
			cur += cn
			t.Logf("  %d exports:", cnt)
			for i := uint64(0); i < cnt && cur < end; i++ {
				nl, nn := binary.Uvarint(wasmBytes[cur:])
				cur += nn
				nm := string(wasmBytes[cur : cur+int(nl)])
				cur += int(nl)
				kind := wasmBytes[cur]
				cur++
				idx, in2 := binary.Uvarint(wasmBytes[cur:])
				cur += in2
				ks := []string{"func", "table", "memory", "global"}
				kn := "?"
				if int(kind) < len(ks) {
					kn = ks[kind]
				}
				t.Logf("  [%d] %s %q idx=%d", i, kn, nm, idx)
			}
		}
		pos = end
	}

	// --- Part 2: wazero compiled module info ---
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Log("=== EXPORTS (wazero) ===")
	for name, exp := range compiled.ExportedFunctions() {
		t.Logf("  key=%q sig=(%v)->%v", name, fmtVT(exp.ParamTypes()), fmtVT(exp.ResultTypes()))
	}
	t.Log("=== IMPORTS (wazero) ===")
	for _, imp := range compiled.ImportedFunctions() {
		mod, name, _ := imp.Import()
		t.Logf("  %s.%s(%v)->%v", mod, name, fmtVT(imp.ParamTypes()), fmtVT(imp.ResultTypes()))
	}
}

func fmtVT(types []api.ValueType) string {
	names := make([]string, len(types))
	for i, vt := range types {
		switch vt {
		case api.ValueTypeI32:
			names[i] = "i32"
		case api.ValueTypeI64:
			names[i] = "i64"
		case api.ValueTypeF32:
			names[i] = "f32"
		case api.ValueTypeF64:
			names[i] = "f64"
		default:
			names[i] = fmt.Sprintf("0x%x", vt)
		}
	}
	r := ""
	for i, v := range names {
		if i > 0 {
			r += ","
		}
		r += v
	}
	return r
}
