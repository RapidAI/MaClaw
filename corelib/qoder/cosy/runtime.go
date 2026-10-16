package cosy

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	wazero "github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// jsHeap mirrors the wasm-bindgen glue's JS-value heap exactly: slots 0..1023
// are undefined, 1024 undefined, 1025 null, 1026 true, 1027 false, and the
// free list stores its next index inside the freed slot (head starts at 1028).
// Slot indices cross the wasm boundary as plain i32, so the layout is part of
// the ABI and must not change.
type jsHeap struct {
	mu    sync.Mutex
	slots []any
	head  int32
}

var globalHeap = newJSHeap()

func newJSHeap() *jsHeap {
	h := &jsHeap{slots: make([]any, 1028)}
	h.head = 1028
	// Seed the constant slots exactly like the glue: 0..1024 undefined,
	// 1025 null (nil in Go), 1026 true, 1027 false.
	h.slots[1026] = true
	h.slots[1027] = false
	return h
}

func (h *jsHeap) push(v any) int32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.head == int32(len(h.slots)) {
		h.slots = append(h.slots, int32(len(h.slots)+1))
	}
	idx := h.head
	h.head = h.slots[idx].(int32)
	h.slots[idx] = v
	return idx
}

func (h *jsHeap) get(idx int32) any {
	h.mu.Lock()
	defer h.mu.Unlock()
	if idx < 0 || int(idx) >= len(h.slots) {
		return nil
	}
	return h.slots[idx]
}

func (h *jsHeap) drop(idx int32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if idx < 1028 || int(idx) >= len(h.slots) {
		return
	}
	h.slots[idx] = h.head
	h.head = idx
}

// Marker types for the object graph the wasm walks (globalThis.crypto etc.).
type (
	jsGlobal   struct{}
	jsCrypto   struct{}
	jsProcess  struct{}
	jsVersions struct{}
	jsNode     struct{}
	jsRequire  func()
)

// byteView is a Uint8Array-like window over the owning module's linear memory,
// or over an embedder scratch buffer for fresh arrays (new_with_length).
type byteView struct {
	owner   api.Module
	base    uint32
	length  uint32
	scratch []byte
}

func (v *byteView) writeRand() {
	if v.scratch != nil {
		_, _ = rand.Read(v.scratch)
		return
	}
	buf := make([]byte, v.length)
	_, _ = rand.Read(buf)
	v.owner.ExportedMemory("memory").Write(v.base, buf)
}

func (v *byteView) bytes(mod api.Module) []byte {
	if v.scratch != nil {
		return v.scratch
	}
	if v.owner != nil {
		mod = v.owner
	}
	out, ok := mod.ExportedMemory("memory").Read(v.base, v.length)
	if !ok {
		return nil
	}
	return out
}

// readString decodes (ptr,len) as UTF-8 from the module memory.
func readString(mod api.Module, ptr, length int32) string {
	if ptr == 0 || length <= 0 {
		return ""
	}
	buf, ok := mod.ExportedMemory("memory").Read(uint32(ptr), uint32(length))
	if !ok {
		return ""
	}
	return string(buf)
}

// allocString writes s into fresh wasm memory via the module's own allocator
// (__wbindgen_export2) and returns (ptr, len) in the wasm-bindgen ABI. An
// allocation failure surfaces as an error instead of a wasm trap.
func allocString(mod api.Module, s string) (int32, int32, error) {
	raw := []byte(s)
	alloc := mod.ExportedFunction("__wbindgen_export2")
	if alloc == nil {
		return 0, 0, fmt.Errorf("wasm export __wbindgen_export2 missing")
	}
	rets, err := alloc.Call(context.Background(), uint64(len(raw)), 1)
	if err != nil {
		return 0, 0, fmt.Errorf("wasm alloc failed: %w", err)
	}
	ptr := int32(uint32(rets[0]))
	if ptr == 0 && len(raw) > 0 {
		return 0, 0, fmt.Errorf("wasm alloc returned null for %d bytes", len(raw))
	}
	if len(raw) > 0 && !mod.ExportedMemory("memory").Write(uint32(ptr), raw) {
		return 0, 0, fmt.Errorf("wasm memory write failed for %d bytes", len(raw))
	}
	return ptr, int32(len(raw)), nil
}

// runtime compiles the wasm once per process; each Context instantiates it.
type runtime struct {
	compiled wazero.CompiledModule
	mod      wazero.Runtime
}

var (
	runtimeMu     sync.Mutex
	runtimeCached *runtime
)

func shared() (*runtime, error) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if runtimeCached != nil {
		return runtimeCached, nil
	}
	r := wazero.NewRuntime(context.Background())
	host := r.NewHostModuleBuilder("./qoder_auth_wasm_bg.js")
	installShims(host)
	if _, err := host.Instantiate(context.Background()); err != nil {
		return nil, fmt.Errorf("instantiate cosy shims: %w", err)
	}
	compiled, err := r.CompileModule(context.Background(), qoderAuthWASM)
	if err != nil {
		return nil, fmt.Errorf("compile qoder_auth_wasm: %w", err)
	}
	runtimeCached = &runtime{compiled: compiled, mod: r}
	return runtimeCached, nil
}

// wasmNegative16 is the wasm i32 encoding of -16 for the scratch-stack moves.
const wasmNegative16 = uint64(0xFFFFFFF0)

// installShims registers every import the wasm declares, mirroring the glue's
// V() import object one for one. All heap traffic is i32 slot indices; shims
// that touch linear memory take the calling module as their first parameter
// (wazero passes the caller automatically).
func installShims(b wazero.HostModuleBuilder) {
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) {
		globalHeap.drop(idx)
	}).Export("__wbindgen_object_drop_ref")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		return globalHeap.push(globalHeap.get(idx))
	}).Export("__wbindgen_object_clone_ref")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr, length int32) int32 {
		return globalHeap.push(fmt.Errorf("%s", readString(mod, ptr, length)))
	}).Export("__wbg_Error_2e59b1b37a9a34c3")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		if _, ok := globalHeap.get(idx).(jsRequire); ok {
			return 1
		}
		return 0
	}).Export("__wbg___wbindgen_is_function_49868bde5eb1e745")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		switch globalHeap.get(idx).(type) {
		case *byteView, map[string]string, jsGlobal, jsCrypto, jsProcess, jsVersions, jsNode:
			return 1
		}
		return 0
	}).Export("__wbg___wbindgen_is_object_40c5a80572e8f9d3")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		if _, ok := globalHeap.get(idx).(string); ok {
			return 1
		}
		return 0
	}).Export("__wbg___wbindgen_is_string_b29b5c5a8065ba1a")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		if globalHeap.get(idx) == nil {
			return 1
		}
		return 0
	}).Export("__wbg___wbindgen_is_undefined_c0cca72b82b86f4d")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr, length int32) {
		panic(readString(mod, ptr, length))
	}).Export("__wbg___wbindgen_throw_81fc77679af83bc6")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, fIdx, thisArg, arg int32) int32 {
		_, _, _ = fIdx, thisArg, arg
		return globalHeap.push(nil)
	}).Export("__wbg_call_d578befcc3145dee")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		_ = globalHeap.get(idx)
		return globalHeap.push(jsCrypto{})
	}).Export("__wbg_crypto_38df2bab126b63dc")

	// obj.getRandomValues(view) — the object path; fill the view in place.
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, obj, view int32) {
		_ = obj
		if v, ok := globalHeap.get(view).(*byteView); ok && v != nil {
			v.writeRand()
		}
	}).Export("__wbg_getRandomValues_c44a50d8cfdaebeb")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr, length int32) {
		buf := make([]byte, length)
		_, _ = rand.Read(buf)
		mod.ExportedMemory("memory").Write(uint32(ptr), buf)
	}).Export("__wbg_getRandomValues_d49329ff89a07af1")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, view int32) int32 {
		if v, ok := globalHeap.get(view).(*byteView); ok && v != nil {
			return int32(v.length)
		}
		return 0
	}).Export("__wbg_length_0c32cb8543c8e4c8")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		_ = idx
		return globalHeap.push(jsCrypto{})
	}).Export("__wbg_msCrypto_bd5a034af96bcba6")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context) int32 {
		return globalHeap.push(map[string]string{})
	}).Export("__wbg_new_99cabae501c0a8a0")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, length int32) int32 {
		return globalHeap.push(&byteView{length: uint32(length), scratch: make([]byte, length)})
	}).Export("__wbg_new_with_length_9cedd08484b73942")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		_ = idx
		return globalHeap.push(jsNode{})
	}).Export("__wbg_node_84ea875411254db1")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context) float64 {
		return float64(time.Now().UnixMilli())
	}).Export("__wbg_now_88621c9c9a4f3ffc")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		_ = idx
		return globalHeap.push(jsProcess{})
	}).Export("__wbg_process_44c7a14e11e9f69e")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, destPtr, destLen, src int32) {
		// Uint8Array.prototype.set(memory[destPtr..], heap[src]) — copy the
		// heap byte view into wasm memory.
		if v, ok := globalHeap.get(src).(*byteView); ok && v != nil {
			mod.ExportedMemory("memory").Write(uint32(destPtr), v.bytes(mod))
		}
	}).Export("__wbg_prototypesetcall_3e05eb9545565046")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, obj, view int32) {
		_ = obj
		if v, ok := globalHeap.get(view).(*byteView); ok && v != nil {
			v.writeRand()
		}
	}).Export("__wbg_randomFillSync_6c25eac9869eb53c")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context) int32 {
		return globalHeap.push(jsRequire(func() {}))
	}).Export("__wbg_require_b4edbdcf3e2a1ef0")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, mapIdx, keyIdx, valIdx int32) int32 {
		m, ok := globalHeap.get(mapIdx).(map[string]string)
		if !ok {
			return 0
		}
		key, _ := globalHeap.get(keyIdx).(string)
		var val string
		switch v := globalHeap.get(valIdx).(type) {
		case string:
			val = v
		case *byteView:
			val = string(v.bytes(mod))
		}
		if key != "" {
			m[key] = val
		}
		return 0
	}).Export("__wbg_set_08463b1df38a7e29")

	staticAccessor := func(name string) {
		b.NewFunctionBuilder().WithFunc(func(_ context.Context) int32 {
			return globalHeap.push(jsGlobal{})
		}).Export(name)
	}
	staticAccessor("__wbg_static_accessor_GLOBAL_THIS_a1248013d790bf5f")
	staticAccessor("__wbg_static_accessor_GLOBAL_f2e0f995a21329ff")
	staticAccessor("__wbg_static_accessor_SELF_24f78b6d23f286ea")
	staticAccessor("__wbg_static_accessor_WINDOW_59fd959c540fe405")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, view, start, end int32) int32 {
		v, ok := globalHeap.get(view).(*byteView)
		if !ok || v == nil {
			return globalHeap.push(&byteView{scratch: []byte{}})
		}
		sub := &byteView{owner: mod, base: v.base + uint32(start), length: uint32(end - start)}
		if v.scratch != nil {
			sub.scratch = v.scratch[start:end]
		}
		return globalHeap.push(sub)
	}).Export("__wbg_subarray_0f98d3fb634508ad")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, idx int32) int32 {
		_ = idx
		return globalHeap.push(jsVersions{})
	}).Export("__wbg_versions_276b2795b1c6a219")

	// cast 1: Uint8Array view over (ptr,len); cast 2: decoded string.
	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr, length int32) int32 {
		return globalHeap.push(&byteView{owner: mod, base: uint32(ptr), length: uint32(length)})
	}).Export("__wbindgen_cast_0000000000000001")

	b.NewFunctionBuilder().WithFunc(func(_ context.Context, mod api.Module, ptr, length int32) int32 {
		return globalHeap.push(readString(mod, ptr, length))
	}).Export("__wbindgen_cast_0000000000000002")
}
