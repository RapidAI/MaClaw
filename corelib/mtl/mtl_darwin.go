//go:build darwin

// Package mtl is a minimal Metal compute binding for macOS written in pure Go
// (Objective-C runtime via github.com/ebitengine/purego; works with
// CGO_ENABLED=0). It covers what maclaw's GPU inference needs: a device, one
// command queue, shared buffers, pipelines compiled at runtime from Metal
// Shading Language source, and serial compute encoders.
package mtl

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	initOnce sync.Once
	initErr  error

	msgSend uintptr // objc_msgSend

	createSystemDefaultDevice func() objc.ID

	selAlloc, selInit, selRelease, selDrain                         objc.SEL
	selNewCommandQueue, selNewBufferWithLength, selContents         objc.SEL
	selNewLibraryWithSource, selNewFunctionWithName                 objc.SEL
	selNewComputePipelineStateWithFunction                          objc.SEL
	selCommandBuffer, selComputeCommandEncoder, selSetComputePipe   objc.SEL
	selSetBuffer, selSetBytes, selDispatchThreadgroups, selEndEnc   objc.SEL
	selCommit, selWaitUntilCompleted, selStatus, selError           objc.SEL
	selStringWithUTF8String, selUTF8String, selLocalizedDescription objc.SEL
	selName, selMaxThreads, selLength, selRetain                    objc.SEL
	clsNSString, clsNSAutoreleasePool                               objc.Class
)

func load() error {
	initOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				initErr = fmt.Errorf("mtl: init panic: %v", r)
			}
		}()
		mlib, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = fmt.Errorf("mtl: dlopen Metal: %w", err)
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			initErr = fmt.Errorf("mtl: dlopen Foundation: %w", err)
			return
		}
		lobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}
		msgSend, err = purego.Dlsym(lobjc, "objc_msgSend")
		if err != nil {
			initErr = err
			return
		}
		purego.RegisterLibFunc(&createSystemDefaultDevice, mlib, "MTLCreateSystemDefaultDevice")
		r := objc.RegisterName
		selAlloc, selInit, selRelease, selDrain = r("alloc"), r("init"), r("release"), r("drain")
		selRetain = r("retain")
		selNewCommandQueue = r("newCommandQueue")
		selNewBufferWithLength = r("newBufferWithLength:options:")
		selContents, selLength = r("contents"), r("length")
		selNewLibraryWithSource = r("newLibraryWithSource:options:error:")
		selNewFunctionWithName = r("newFunctionWithName:")
		selNewComputePipelineStateWithFunction = r("newComputePipelineStateWithFunction:error:")
		selCommandBuffer = r("commandBuffer")
		selComputeCommandEncoder = r("computeCommandEncoder")
		selSetComputePipe = r("setComputePipelineState:")
		selSetBuffer = r("setBuffer:offset:atIndex:")
		selSetBytes = r("setBytes:length:atIndex:")
		selDispatchThreadgroups = r("dispatchThreadgroups:threadsPerThreadgroup:")
		selEndEnc = r("endEncoding")
		selCommit, selWaitUntilCompleted = r("commit"), r("waitUntilCompleted")
		selStatus, selError = r("status"), r("error")
		selStringWithUTF8String, selUTF8String = r("stringWithUTF8String:"), r("UTF8String")
		selLocalizedDescription, selName = r("localizedDescription"), r("name")
		selMaxThreads = r("maxTotalThreadsPerThreadgroup")
		clsNSString = objc.GetClass("NSString")
		clsNSAutoreleasePool = objc.GetClass("NSAutoreleasePool")
		if clsNSString == 0 || clsNSAutoreleasePool == 0 {
			initErr = errors.New("mtl: Foundation classes missing")
		}
	})
	return initErr
}

// send calls objc_msgSend with integer/pointer arguments (no reflection).
func send(id objc.ID, sel objc.SEL, args ...uintptr) uintptr {
	a := make([]uintptr, 0, 2+len(args))
	a = append(a, uintptr(id), uintptr(sel))
	a = append(a, args...)
	r1, _, _ := purego.SyscallN(msgSend, a...)
	return r1
}

func send0(id objc.ID, sel objc.SEL) uintptr {
	r1, _, _ := purego.SyscallN(msgSend, uintptr(id), uintptr(sel))
	return r1
}

func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	r := send(objc.ID(clsNSString), selStringWithUTF8String, uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	return objc.ID(r)
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	p := send0(ns, selUTF8String)
	if p == 0 {
		return ""
	}
	var n int
	for *(*byte)(unsafe.Pointer(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}

func errString(e objc.ID) string {
	if e == 0 {
		return "unknown error"
	}
	return goString(objc.ID(send0(e, selLocalizedDescription)))
}

// Pool is an NSAutoreleasePool; Drain must run on the same OS thread
// (callers use runtime.LockOSThread).
type Pool struct{ id objc.ID }

func NewPool() Pool {
	p := objc.ID(send0(objc.ID(clsNSAutoreleasePool), selAlloc))
	return Pool{objc.ID(send0(p, selInit))}
}

func (p Pool) Drain() { send0(p.id, selDrain) }

// Device is an MTLDevice with one command queue.
type Device struct {
	dev, queue objc.ID
	Name       string
}

// NewDevice returns the system default Metal device, or an error when Metal
// is unavailable.
func NewDevice() (*Device, error) {
	if err := load(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := NewPool()
	defer pool.Drain()
	dev := createSystemDefaultDevice()
	if dev == 0 {
		return nil, errors.New("mtl: no Metal device")
	}
	q := objc.ID(send0(dev, selNewCommandQueue))
	if q == 0 {
		return nil, errors.New("mtl: newCommandQueue failed")
	}
	d := &Device{dev: dev, queue: q}
	d.Name = goString(objc.ID(send0(dev, selName)))
	return d, nil
}

// Buffer is a shared-storage MTLBuffer (CPU and GPU see the same memory).
type Buffer struct {
	id   objc.ID
	ptr  unsafe.Pointer
	Size int
}

// NewBuffer allocates n bytes of shared storage (zero-filled by Metal).
func (d *Device) NewBuffer(n int) (*Buffer, error) {
	if n <= 0 {
		n = 16
	}
	id := objc.ID(send(d.dev, selNewBufferWithLength, uintptr(n), 0 /* MTLResourceStorageModeShared */))
	if id == 0 {
		return nil, fmt.Errorf("mtl: newBuffer(%d) failed", n)
	}
	p := send0(id, selContents)
	return &Buffer{id: id, ptr: unsafe.Pointer(p), Size: n}, nil
}

// Bytes returns the buffer contents as a byte slice.
func (b *Buffer) Bytes() []byte { return unsafe.Slice((*byte)(b.ptr), b.Size) }

// Float32s returns the buffer contents as float32s.
func (b *Buffer) Float32s() []float32 { return unsafe.Slice((*float32)(b.ptr), b.Size/4) }

// Int32s returns the buffer contents as int32s.
func (b *Buffer) Int32s() []int32 { return unsafe.Slice((*int32)(b.ptr), b.Size/4) }

// Release frees the buffer.
func (b *Buffer) Release() {
	if b != nil && b.id != 0 {
		send0(b.id, selRelease)
		b.id, b.ptr = 0, nil
	}
}

// Pipeline is a compiled compute pipeline state.
type Pipeline struct {
	id         objc.ID
	MaxThreads int
}

// Compile builds the MSL source (default options, fast math on) and returns
// pipelines for the named kernels.
func (d *Device) Compile(src string, names []string) (map[string]*Pipeline, error) {
	return d.CompileOpts(src, names, true)
}

// CompileOpts is Compile with fast math selectable; fastMath=false gives
// IEEE-conforming exp/div etc. (needed to track CPU float results closely).
func (d *Device) CompileOpts(src string, names []string, fastMath bool) (map[string]*Pipeline, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := NewPool()
	defer pool.Drain()
	var errID objc.ID
	var opts objc.ID
	if !fastMath {
		cls := objc.GetClass("MTLCompileOptions")
		opts = objc.ID(send0(objc.ID(send0(objc.ID(cls), selAlloc)), selInit))
		send(opts, objc.RegisterName("setFastMathEnabled:"), 0)
		defer send0(opts, selRelease)
	}
	lib := objc.ID(send(d.dev, selNewLibraryWithSource, uintptr(nsString(src)), uintptr(opts), uintptr(unsafe.Pointer(&errID))))
	if lib == 0 {
		return nil, fmt.Errorf("mtl: compile: %s", errString(errID))
	}
	out := make(map[string]*Pipeline, len(names))
	for _, n := range names {
		fn := objc.ID(send(lib, selNewFunctionWithName, uintptr(nsString(n))))
		if fn == 0 {
			return nil, fmt.Errorf("mtl: kernel %q not found", n)
		}
		var e2 objc.ID
		ps := objc.ID(send(d.dev, selNewComputePipelineStateWithFunction, uintptr(fn), uintptr(unsafe.Pointer(&e2))))
		send0(fn, selRelease)
		if ps == 0 {
			return nil, fmt.Errorf("mtl: pipeline %q: %s", n, errString(e2))
		}
		out[n] = &Pipeline{id: ps, MaxThreads: int(send0(ps, selMaxThreads))}
	}
	send0(lib, selRelease)
	return out, nil
}

type size3 struct{ X, Y, Z uint64 }

// Encoder records compute dispatches into one command buffer. Create,
// record and Wait on one goroutine with the OS thread locked (Begin does it).
type Encoder struct {
	pool    Pool
	cb, enc objc.ID
	tg, tpt size3
}

// Begin starts a command buffer with a serial compute encoder. It locks the
// calling goroutine to its OS thread until Wait.
func (d *Device) Begin() *Encoder {
	runtime.LockOSThread()
	e := &Encoder{pool: NewPool()}
	e.cb = objc.ID(send0(d.queue, selCommandBuffer))
	e.enc = objc.ID(send0(e.cb, selComputeCommandEncoder))
	return e
}

func (e *Encoder) SetPipeline(p *Pipeline) { send(e.enc, selSetComputePipe, uintptr(p.id)) }

func (e *Encoder) SetBuffer(b *Buffer, offset, index int) {
	send(e.enc, selSetBuffer, uintptr(b.id), uintptr(offset), uintptr(index))
}

// SetBytes passes a small constant block (≤4 KB) by value.
func (e *Encoder) SetBytes(p unsafe.Pointer, n, index int) {
	send(e.enc, selSetBytes, uintptr(p), uintptr(n), uintptr(index))
}

// Dispatch launches gx×gy×gz threadgroups of tx×ty×tz threads.
func (e *Encoder) Dispatch(gx, gy, gz, tx, ty, tz int) {
	e.tg = size3{uint64(gx), uint64(gy), uint64(gz)}
	e.tpt = size3{uint64(tx), uint64(ty), uint64(tz)}
	// MTLSize (24 B) is passed indirectly on arm64 (AAPCS64 composites > 16 B).
	send(e.enc, selDispatchThreadgroups, uintptr(unsafe.Pointer(&e.tg)), uintptr(unsafe.Pointer(&e.tpt)))
}

// Wait ends encoding, commits, waits, and reports a command-buffer error.
func (e *Encoder) Wait() error {
	defer runtime.UnlockOSThread()
	defer e.pool.Drain()
	send0(e.enc, selEndEnc)
	send0(e.cb, selCommit)
	send0(e.cb, selWaitUntilCompleted)
	if st := send0(e.cb, selStatus); st != 4 { // MTLCommandBufferStatusCompleted
		return fmt.Errorf("mtl: command buffer status %d: %s", st, errString(objc.ID(send0(e.cb, selError))))
	}
	return nil
}
