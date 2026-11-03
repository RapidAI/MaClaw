//go:build windows && amd64

// npu_amd_ipu_windows.go — RadeonML IPU (AMD XDNA NPU) FFI bindings.
//
// This file owns the raw syscall layer: DLL loading, the rml_* ABI, tensor
// creation and the infer loop. It contains no policy — npu_amd_provider_windows.go
// decides whether the NPU is worth using and how a forward is shaped.
//
// ABI notes (these cost real debugging time, keep them):
//
//   - Constants come from include/rml/RadeonML.h, NOT from the rendered API
//     pages. The HTML docs show values for an older release; the header is
//     authoritative. Status codes are -100/-110/-120/... and dtype is 100-based,
//     so guessing small integers silently produces BAD_PARAMETER everywhere.
//   - rml_tensor_info is a fixed 28-byte struct: two int32 enums followed by an
//     inline uint32 shape[5]. It is not a pointer/rank pair.
//   - rmlMapTensor takes (tensor, void** data, size_t* size) and rmlUnmapTensor
//     takes (tensor, void* data) — the pointer returned by the map must be
//     handed back, unlike the one-argument form some docs imply.
//   - rmlLoadGraphFromFile takes (path, graph) with a UTF-16 wchar_t path on
//     Windows; everything else is ANSI/ASCII.
//
// Everything here is pure Go: syscall.NewLazyDLL, no CGO, no AMD SDK install.
// That matters because the NPU must not become a build dependency for hosts
// that do not have an AMD NPU.

package embedding

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// rml_status values (RadeonML.h).
const (
	rmlOK                 = 0
	rmlErrBadModel        = -100
	rmlErrBadParam        = -110
	rmlErrDeviceNotFound  = -120
	rmlErrFileNotFound    = -130
	rmlErrInternal        = -140
	rmlErrModelNotReady   = -150
	rmlErrNotImplemented  = -160
	rmlErrOutOfMemory     = -170
	rmlErrUnsupportedData = -180
)

// rml_dtype values. Only float32 and float16 are meaningful here.
const (
	rmlDTypeFloat32 = 100
	rmlDTypeFloat16 = 101
)

// rml_layout values.
const (
	rmlLayoutC    = 210
	rmlLayoutNC   = 221
	rmlLayoutNCHW = 240
)

// rml_access_mode values.
const (
	rmlAccessReadOnly  = 300
	rmlAccessReadWrite = 310
	rmlAccessWriteOnly = 320
)

// rmlTensorInfo mirrors rml_tensor_info: dtype, layout, then an inline shape
// array of RML_TENSOR_MAX_RANK (5) uint32. Total 28 bytes, no padding.
type rmlTensorInfo struct {
	DType  int32
	Layout int32
	Shape  [5]uint32
}

// rmlStrings mirrors rml_strings: a count and an array of C string pointers.
type rmlStrings struct {
	NumItems uintptr
	Items    uintptr // const char* const*
}

// rmlIPURuntime is the loaded DLL plus its resolved entry points. One instance
// per process: the IPU device is a singleton and a second context would fight
// the first for the overlay.
type rmlIPURuntime struct {
	dll *syscall.LazyDLL

	createContext    *syscall.LazyProc
	releaseContext   *syscall.LazyProc
	loadGraphFile    *syscall.LazyProc
	releaseGraph     *syscall.LazyProc
	createModel      *syscall.LazyProc
	releaseModel     *syscall.LazyProc
	createTensor     *syscall.LazyProc
	releaseTensor    *syscall.LazyProc
	getTensorInfo    *syscall.LazyProc
	mapTensor        *syscall.LazyProc
	unmapTensor      *syscall.LazyProc
	procSetInputInfo *syscall.LazyProc
	setModelInput    *syscall.LazyProc
	setModelOutput   *syscall.LazyProc
	getInputInfo     *syscall.LazyProc
	getOutputInfo    *syscall.LazyProc
	prepareModel     *syscall.LazyProc
	infer            *syscall.LazyProc
	resetStates      *syscall.LazyProc
	getLastError     *syscall.LazyProc
	version          *syscall.LazyProc
}

var (
	rmlOnce  sync.Once
	rmlRt    *rmlIPURuntime
	rmlRtErr error
)

// loadRMLIPU resolves RadeonML_ipu.dll and every entry point the provider
// needs. A missing or partial DLL is cached as an error so a CPU-only host does
// not re-walk the loader on every embedder.
func loadRMLIPU() (*rmlIPURuntime, error) {
	rmlOnce.Do(func() {
		// System32 first (normal driver install), then the driver-store copy.
		candidates := []string{
			"RadeonML_ipu.dll",
			`C:\Windows\System32\RadeonML_ipu.dll`,
		}
		var lastErr error
		for _, name := range candidates {
			dll := syscall.NewLazyDLL(name)
			if err := dll.Load(); err != nil {
				lastErr = err
				continue
			}
			rt := &rmlIPURuntime{
				dll:              dll,
				createContext:    dll.NewProc("rmlCreateDefaultContext"),
				releaseContext:   dll.NewProc("rmlReleaseContext"),
				loadGraphFile:    dll.NewProc("rmlLoadGraphFromFile"),
				releaseGraph:     dll.NewProc("rmlReleaseGraph"),
				createModel:      dll.NewProc("rmlCreateModelFromGraph"),
				releaseModel:     dll.NewProc("rmlReleaseModel"),
				createTensor:     dll.NewProc("rmlCreateTensor"),
				releaseTensor:    dll.NewProc("rmlReleaseTensor"),
				getTensorInfo:    dll.NewProc("rmlGetTensorInfo"),
				mapTensor:        dll.NewProc("rmlMapTensor"),
				unmapTensor:      dll.NewProc("rmlUnmapTensor"),
				procSetInputInfo: dll.NewProc("rmlSetModelInputInfo"),
				setModelInput:    dll.NewProc("rmlSetModelInput"),
				setModelOutput:   dll.NewProc("rmlSetModelOutput"),
				getInputInfo:     dll.NewProc("rmlGetModelInputInfo"),
				getOutputInfo:    dll.NewProc("rmlGetModelOutputInfo"),
				prepareModel:     dll.NewProc("rmlPrepareModel"),
				infer:            dll.NewProc("rmlInfer"),
				resetStates:      dll.NewProc("rmlResetModelStates"),
				getLastError:     dll.NewProc("rmlGetLastError"),
				version:          dll.NewProc("getProjectVersion"),
			}
			// Only the entry points the provider cannot work without are
			// required; diagnostics helpers degrade to empty strings.
			required := []struct {
				name string
				proc *syscall.LazyProc
			}{
				{"rmlCreateDefaultContext", rt.createContext},
				{"rmlReleaseContext", rt.releaseContext},
				{"rmlLoadGraphFromFile", rt.loadGraphFile},
				{"rmlCreateModelFromGraph", rt.createModel},
				{"rmlCreateTensor", rt.createTensor},
				{"rmlMapTensor", rt.mapTensor},
				{"rmlSetModelInput", rt.setModelInput},
				{"rmlSetModelOutput", rt.setModelOutput},
				{"rmlInfer", rt.infer},
			}
			missing := ""
			for _, r := range required {
				if err := r.proc.Find(); err != nil {
					missing = r.name
					break
				}
			}
			if missing != "" {
				lastErr = fmt.Errorf("%s: missing export %s", name, missing)
				continue
			}
			rmlRt = rt
			return
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("RadeonML_ipu.dll not found")
		}
		rmlRtErr = lastErr
	})
	return rmlRt, rmlRtErr
}

// cString copies a NUL-terminated C string. The driver's buffers are short
// (messages, version strings); the cap keeps a missing terminator from walking
// off into unmapped memory.
func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	const maxLen = 1024
	buf := make([]byte, 0, 64)
	base := unsafe.Pointer(p)
	for i := uintptr(0); i < maxLen; i++ {
		c := *(*byte)(unsafe.Add(base, i))
		if c == 0 {
			break
		}
		buf = append(buf, c)
	}
	return string(buf)
}

// lastError returns rmlGetLastError as a Go string. RadeonML hands back a
// pointer into a thread-local buffer, so it is copied immediately.
func (r *rmlIPURuntime) lastError() string {
	if r == nil || r.getLastError == nil {
		return ""
	}
	if err := r.getLastError.Find(); err != nil {
		return ""
	}
	p, _, _ := r.getLastError.Call()
	return cString(p)
}

// statusName renders an rml_status symbolically so diagnostics read as
// "RML_ERROR_FILE_NOT_FOUND" instead of "-130".
func statusName(status int32) string {
	switch status {
	case rmlOK:
		return "RML_OK"
	case rmlErrBadModel:
		return "RML_ERROR_BAD_MODEL"
	case rmlErrBadParam:
		return "RML_ERROR_BAD_PARAMETER"
	case rmlErrDeviceNotFound:
		return "RML_ERROR_DEVICE_NOT_FOUND"
	case rmlErrFileNotFound:
		return "RML_ERROR_FILE_NOT_FOUND"
	case rmlErrInternal:
		return "RML_ERROR_INTERNAL"
	case rmlErrModelNotReady:
		return "RML_ERROR_MODEL_NOT_READY"
	case rmlErrNotImplemented:
		return "RML_ERROR_NOT_IMPLEMENTED"
	case rmlErrOutOfMemory:
		return "RML_ERROR_OUT_OF_MEMORY"
	case rmlErrUnsupportedData:
		return "RML_ERROR_UNSUPPORTED_DATA"
	}
	return fmt.Sprintf("rml_status %d", status)
}

// statusError turns a non-zero rml_status into an error carrying the driver's
// own message, which is the only actionable diagnostic the IPU backend gives.
func (r *rmlIPURuntime) statusError(op string, status int32) error {
	msg := r.lastError()
	if msg == "" {
		return fmt.Errorf("IPU %s: %s", op, statusName(status))
	}
	return fmt.Errorf("IPU %s: %s (%s)", op, msg, statusName(status))
}

// rmlIPUContext is one open IPU device context.
type rmlIPUContext struct {
	rt  *rmlIPURuntime
	ctx uintptr
}

// openIPUContext creates the default IPU context (device 0 = auto).
func openIPUContext() (*rmlIPUContext, error) {
	rt, err := loadRMLIPU()
	if err != nil {
		return nil, err
	}
	// rml_context_params is a single uint32 (device_idx); 0 = auto.
	params := struct{ DeviceIdx uint32 }{DeviceIdx: 0}
	var ctx uintptr
	status, _, _ := rt.createContext.Call(
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&ctx)),
	)
	if int32(status) != rmlOK {
		return nil, rt.statusError("rmlCreateDefaultContext", int32(status))
	}
	if ctx == 0 {
		return nil, fmt.Errorf("IPU context: null handle")
	}
	return &rmlIPUContext{rt: rt, ctx: ctx}, nil
}

// close releases the device context. Safe on nil and idempotent.
func (c *rmlIPUContext) close() {
	if c == nil || c.ctx == 0 {
		return
	}
	if c.rt.releaseContext != nil {
		c.rt.releaseContext.Call(c.ctx)
	}
	c.ctx = 0
}

// versionString reports the IPU backend build for diagnostics.
func (c *rmlIPUContext) versionString() string {
	if c == nil || c.rt == nil || c.rt.version == nil {
		return ""
	}
	if err := c.rt.version.Find(); err != nil {
		return ""
	}
	p, _, _ := c.rt.version.Call()
	return cString(p)
}

// loadGraph loads an ONNX or .amodel graph from a file. Kept separate from
// model creation so the caller can report "file loaded but not compilable",
// which is exactly the ONNX-on-IPU failure mode.
func (c *rmlIPUContext) loadGraph(path string) (uintptr, error) {
	if c == nil || c.ctx == 0 {
		return 0, fmt.Errorf("IPU graph: no context")
	}
	wpath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("IPU graph path: %w", err)
	}
	var graph uintptr
	status, _, _ := c.rt.loadGraphFile.Call(
		uintptr(unsafe.Pointer(wpath)),
		uintptr(unsafe.Pointer(&graph)),
	)
	runtime.KeepAlive(wpath)
	if int32(status) != rmlOK || graph == 0 {
		return 0, c.rt.statusError("rmlLoadGraphFromFile", int32(status))
	}
	return graph, nil
}

// releaseGraph frees a graph handle.
func (c *rmlIPUContext) releaseGraph(graph uintptr) {
	if graph != 0 && c != nil && c.rt.releaseGraph != nil {
		c.rt.releaseGraph.Call(graph)
	}
}

// rmlIPUModel is a compiled .amodel loaded onto the device.
type rmlIPUModel struct {
	ctx   *rmlIPUContext
	graph uintptr
	model uintptr

	// Tensor slots, allocated once from the model's own input/output info so
	// the shapes always match what the compiled overlay expects.
	inTensor  uintptr
	outTensor uintptr
	inDims    []uint64
	outDims   []uint64
	inElems   int
	outElems  int
}

// newModel builds a device model from a graph handle.
//
// This is the ONLY path that produces a usable IPU model: an ONNX graph loads
// but rmlCreateModelFromGraph refuses it ("IPU: Model cannot be created by a
// non-AModel graph object"), because the Phoenix overlay kernels are compiled
// for fixed shapes and the graph has to be quantized and compiled offline
// first.
func (c *rmlIPUContext) newModel(graph uintptr) (*rmlIPUModel, error) {
	if c == nil || c.ctx == 0 {
		return nil, fmt.Errorf("IPU model: no context")
	}
	var model uintptr
	status, _, _ := c.rt.createModel.Call(c.ctx, graph, uintptr(unsafe.Pointer(&model)))
	if int32(status) != rmlOK || model == 0 {
		return nil, c.rt.statusError("rmlCreateModelFromGraph", int32(status))
	}
	return &rmlIPUModel{ctx: c, graph: graph, model: model}, nil
}

// close releases tensors, model and graph. Safe on nil and idempotent.
func (m *rmlIPUModel) close() {
	if m == nil {
		return
	}
	if m.ctx != nil && m.ctx.rt != nil {
		if m.inTensor != 0 {
			m.ctx.rt.releaseT(m.inTensor)
		}
		if m.outTensor != 0 {
			m.ctx.rt.releaseT(m.outTensor)
		}
		if m.model != 0 && m.ctx.rt.releaseModel != nil {
			m.ctx.rt.releaseModel.Call(m.model)
		}
		m.ctx.releaseGraph(m.graph)
	}
	m.inTensor, m.outTensor, m.model, m.graph = 0, 0, 0, 0
}

// createF32Tensor allocates a float32 tensor on the device.
func (r *rmlIPURuntime) createF32Tensor(ctx uintptr, shape []uint32, layout int32, access int32) (uintptr, []uint64, int, error) {
	if len(shape) == 0 || len(shape) > 5 {
		return 0, nil, 0, fmt.Errorf("IPU tensor: bad rank %d", len(shape))
	}
	info := rmlTensorInfo{DType: rmlDTypeFloat32, Layout: layout}
	elems := 1
	for i, d := range shape {
		info.Shape[i] = d
		elems *= int(d)
	}
	var t uintptr
	status, _, _ := r.createTensor.Call(
		ctx,
		uintptr(unsafe.Pointer(&info)),
		uintptr(access),
		uintptr(unsafe.Pointer(&t)),
	)
	if int32(status) != rmlOK || t == 0 {
		return 0, nil, 0, r.statusError("rmlCreateTensor", int32(status))
	}
	dims := make([]uint64, len(shape))
	for i, d := range shape {
		dims[i] = uint64(d)
	}
	return t, dims, elems, nil
}

// mapF32Tensor maps a tensor for host access and returns a []float32 view over
// it. The caller MUST unmap with the same pointer before rmlInfer reads it.
func (r *rmlIPURuntime) mapF32Tensor(t uintptr, n int) ([]float32, uintptr, error) {
	if r.mapTensor == nil {
		return nil, 0, fmt.Errorf("IPU mapTensor: not available")
	}
	var p uintptr
	var size uintptr
	status, _, _ := r.mapTensor.Call(t, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&size)))
	if int32(status) != rmlOK || p == 0 {
		return nil, 0, r.statusError("rmlMapTensor", int32(status))
	}
	if n <= 0 {
		return nil, p, nil
	}
	// Trust the model's element count, but never read past the mapped size.
	if size > 0 && uintptr(n)*4 > size {
		return nil, p, fmt.Errorf("IPU mapTensor: %d floats need %d bytes, mapped %d", n, n*4, size)
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(p)), n), p, nil
}

// unmapTensor commits host writes back to the device buffer.
func (r *rmlIPURuntime) unmapF32Tensor(t uintptr, data uintptr) error {
	if r.unmapTensor == nil {
		return nil
	}
	status, _, _ := r.unmapTensor.Call(t, data)
	if int32(status) != rmlOK {
		return r.statusError("rmlUnmapTensor", int32(status))
	}
	return nil
}

// releaseT frees a device tensor.
func (r *rmlIPURuntime) releaseT(t uintptr) {
	if t != 0 && r.releaseTensor != nil {
		r.releaseTensor.Call(t)
	}
}

// tensorShape reads back a tensor description, used to learn the compiled
// model's expected input/output shapes instead of hardcoding them.
func (r *rmlIPURuntime) tensorShape(t uintptr) ([5]uint32, int32, int32, error) {
	if r.getTensorInfo == nil {
		return [5]uint32{}, 0, 0, fmt.Errorf("IPU getTensorInfo: not available")
	}
	var info rmlTensorInfo
	status, _, _ := r.getTensorInfo.Call(t, uintptr(unsafe.Pointer(&info)))
	if int32(status) != rmlOK {
		return [5]uint32{}, 0, 0, r.statusError("rmlGetTensorInfo", int32(status))
	}
	return info.Shape, info.DType, info.Layout, nil
}
