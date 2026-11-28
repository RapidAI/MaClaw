//go:build android

package nnapi

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// NNAPI operand and operation codes from NeuralNetworksTypes.h.
// Feature level 6 is 1000006 (BATCH_MATMUL). API level 34 is not that number.
const (
	nnFloat32     int32 = 0
	nnInt32       int32 = 1
	nnTensorF32   int32 = 3
	nnTensorI32   int32 = 4
	nnBool        int32 = 6
	nnAdd         int32 = 0
	nnDWConv      int32 = 4
	nnFC          int32 = 9
	nnMul         int32 = 18
	nnReshape     int32 = 22
	nnSoftmax     int32 = 25
	nnTanh        int32 = 28
	nnMean        int32 = 31
	nnSub         int32 = 36
	nnTranspose   int32 = 37
	nnArgMax      int32 = 39
	nnGather      int32 = 51
	nnRsqrt       int32 = 83
	nnSplit       int32 = 87
	nnBMM         int32 = 102
	nnFusedNone   int32 = 0
	nnFusedRelu   int32 = 1
	nnPadSame     int32 = 1
	nnPrefFast    int32 = 1
	nnDeviceAccel int32 = 4

	nnFeatureLevel6 int64 = 1000006

	// First SAN-M compile on an HTP can exceed a minute. Execution of one
	// utterance should not. Both are hints; a miss returns a transient code.
	compileTimeoutNS uint64 = 180e9
	execTimeoutNS    uint64 = 60e9
)

type nnSym struct {
	modelCreate, modelFree, modelFinish            uintptr
	addOperand, setFromMemory, addOperation        uintptr
	identify, relax                                uintptr
	compForDevices, compFree, compPref, compFinish uintptr
	compCaching, compTimeout                       uintptr
	execCreate, execFree, execSetIn, execSetOut    uintptr
	execCompute, execTimeout                       uintptr
	devCount, getDevice, devName, devType          uintptr
	devLevel                                       uintptr
	memFromFd, memFree                             uintptr
	supported                                      uintptr
	ashm                                           uintptr
}

// nnAccel is one feature-level-6 accelerator. Compilations use exactly one
// of these: setTimeout rejects a multi-device compilation, and the runtime
// would otherwise partition the graph across devices.
type nnAccel struct {
	ptr   uintptr
	name  string
	level int64
}

var (
	nnOnce   sync.Once
	nnLib    nnSym
	nnAccels []nnAccel
	nnErr    error
)

// Available reports whether an accelerator at feature level 6 or newer was found.
func Available() bool {
	nnOnce.Do(nnProbe)
	return nnErr == nil
}

// UnavailableReason explains why Available is false.
func UnavailableReason() string {
	nnOnce.Do(nnProbe)
	if nnErr != nil {
		return nnErr.Error()
	}
	return ""
}

// Compile uploads constants into ashmem and compiles the graph for accelerators
// only. A missing symbol, a rejected op, or an ashmem failure returns an error
// so the caller keeps the CPU kernels. Go weight slices on the builder are
// dropped after the copy, before Compilation_finish.
func Compile(b *Builder, cacheDir string, token [32]byte) (*Session, error) {
	if b == nil {
		return nil, fmt.Errorf("nnapi: nil graph")
	}
	if b.err != nil {
		return nil, b.err
	}
	if len(b.outputs) == 0 {
		return nil, fmt.Errorf("nnapi: graph has no outputs")
	}
	nnOnce.Do(nnProbe)
	if nnErr != nil {
		return nil, nnErr
	}

	plan, err := preparePlan(b)
	if err != nil {
		return nil, err
	}
	mapping, mem, err := uploadPlan(b, plan)
	if err != nil {
		return nil, err
	}
	for i := range b.tensors {
		b.tensors[i].f32 = nil
		b.tensors[i].i32 = nil
		b.tensors[i].fill = nil
	}
	debug.FreeOSMemory()

	var model uintptr
	success := false
	var comp uintptr
	defer func() {
		if success {
			return
		}
		if comp != 0 {
			call(nnLib.compFree, comp)
		}
		if model != 0 {
			call(nnLib.modelFree, model)
		}
		if mem != 0 {
			call(nnLib.memFree, mem)
		}
		if mapping != nil {
			_ = unix.Munmap(mapping)
		}
	}()

	// Out-params and operand structs live on the heap. call converts pointers
	// to uintptr before the cgo trampoline, so a stack growth inside call
	// would leave those integers aimed at the old stack.
	modelBox := heapUintptr()
	st := call(nnLib.modelCreate, uintptr(unsafe.Pointer(modelBox)))
	runtime.KeepAlive(modelBox)
	if st != 0 || *modelBox == 0 {
		return nil, fmt.Errorf("nnapi: model create %d", st)
	}
	model = *modelBox
	dimKeep := make([][]uint32, 0, len(b.tensors))
	ots := heapOperandTypes(len(b.tensors) + len(plan.scalars))
	for i, t := range b.tensors {
		dims := shapeU32(t.shape)
		dimKeep = append(dimKeep, dims)
		ots[i] = operandType{
			typ:            tensorNNType(t.dt),
			dimensionCount: uint32(len(dims)),
			dimensions:     u32ptr(dims),
		}
		if st := call(nnLib.addOperand, model, uintptr(unsafe.Pointer(&ots[i]))); st != 0 {
			runtime.KeepAlive(ots)
			runtime.KeepAlive(dimKeep)
			return nil, fmt.Errorf("nnapi: addOperand %d: %d", i, st)
		}
	}
	baseOT := len(b.tensors)
	for i, slot := range plan.scalars {
		ots[baseOT+i] = operandType{typ: slot.typ}
		if st := call(nnLib.addOperand, model, uintptr(unsafe.Pointer(&ots[baseOT+i]))); st != 0 {
			runtime.KeepAlive(ots)
			return nil, fmt.Errorf("nnapi: add scalar operand %d: %d", i, st)
		}
	}
	runtime.KeepAlive(ots)
	runtime.KeepAlive(dimKeep)
	if mem != 0 {
		for i := range b.tensors {
			if plan.tensorN[i] == 0 {
				continue
			}
			st := call(nnLib.setFromMemory, model, uintptr(i), mem, uintptr(plan.tensorOff[i]), uintptr(plan.tensorN[i]))
			if st != 0 {
				return nil, fmt.Errorf("nnapi: setOperandValueFromMemory %d: %d", i, st)
			}
		}
		base := len(b.tensors)
		for i, slot := range plan.scalars {
			st := call(nnLib.setFromMemory, model, uintptr(base+i), mem, uintptr(slot.off), uintptr(len(slot.raw)))
			if st != 0 {
				return nil, fmt.Errorf("nnapi: set scalar %d: %d", i, st)
			}
		}
	}
	if err := plan.emit(model); err != nil {
		return nil, err
	}
	inIdx := idsToU32(b.inputs)
	outIdx := idsToU32(b.outputs)
	if st := call(nnLib.identify, model, uintptr(len(inIdx)), u32addr(inIdx), uintptr(len(outIdx)), u32addr(outIdx)); st != 0 {
		return nil, fmt.Errorf("nnapi: identifyInputsAndOutputs %d", st)
	}
	runtime.KeepAlive(inIdx)
	runtime.KeepAlive(outIdx)
	if st := call(nnLib.relax, model, 1); st != 0 {
		return nil, fmt.Errorf("nnapi: relaxFloat32toFloat16 %d", st)
	}
	if st := call(nnLib.modelFinish, model); st != 0 {
		return nil, fmt.Errorf("nnapi: model finish %d", st)
	}
	runtime.KeepAlive(dimKeep)

	if cacheDir != "" {
		if mkErr := os.MkdirAll(cacheDir, 0o700); mkErr != nil {
			cacheDir = ""
		}
	}
	levels := make([]int64, len(nnAccels))
	for i, dev := range nnAccels {
		levels[i] = dev.level
	}
	var fails []string
	chosen := ""
	for _, idx := range orderAccelRank(levels) {
		dev := nnAccels[idx]
		// one points at the package slice, which does not move if call grows the stack.
		one := &nnAccels[idx].ptr
		supported := heapBytes(len(plan.names))
		if len(supported) > 0 {
			st := call(nnLib.supported, model, uintptr(unsafe.Pointer(one)), 1, uintptr(unsafe.Pointer(&supported[0])))
			runtime.KeepAlive(supported)
			if st != 0 {
				fails = append(fails, fmt.Sprintf("%s: supported %d", dev.name, st))
				continue
			}
			bad := -1
			for i, ok := range supported {
				if ok == 0 {
					bad = i
					break
				}
			}
			if bad >= 0 {
				fails = append(fails, fmt.Sprintf("%s: unsupported %s", dev.name, plan.names[bad]))
				continue
			}
		}
		compBox := heapUintptr()
		st = call(nnLib.compForDevices, model, uintptr(unsafe.Pointer(one)), 1, uintptr(unsafe.Pointer(compBox)))
		runtime.KeepAlive(compBox)
		if st != 0 || *compBox == 0 {
			fails = append(fails, fmt.Sprintf("%s: create %d", dev.name, st))
			continue
		}
		comp = *compBox
		if st := call(nnLib.compPref, comp, uintptr(nnPrefFast)); st != 0 {
			call(nnLib.compFree, comp)
			comp = 0
			fails = append(fails, fmt.Sprintf("%s: preference %d", dev.name, st))
			continue
		}
		if cacheDir != "" && nnLib.compCaching != 0 {
			dir := cString(cacheDir)
			tok := heapToken()
			*tok = scopeToken(token, dev.name)
			_ = call(nnLib.compCaching, comp, uintptr(unsafe.Pointer(&dir[0])), uintptr(unsafe.Pointer(&tok[0])))
			runtime.KeepAlive(dir)
			runtime.KeepAlive(tok)
		}
		if nnLib.compTimeout != 0 {
			if st := call(nnLib.compTimeout, comp, uintptr(compileTimeoutNS)); st != 0 {
				log.Printf("nnapi: %s compilation setTimeout %d", dev.name, st)
			}
		}
		if st := call(nnLib.compFinish, comp); st != 0 {
			call(nnLib.compFree, comp)
			comp = 0
			fails = append(fails, fmt.Sprintf("%s: finish %d", dev.name, st))
			continue
		}
		chosen = dev.name
		break
	}
	if comp == 0 {
		if len(fails) == 0 {
			return nil, fmt.Errorf("nnapi: no accelerator")
		}
		return nil, fmt.Errorf("nnapi: %s", strings.Join(fails, "; "))
	}

	inSpecs := portSpecs(b, b.inputs)
	outSpecs := portSpecs(b, b.outputs)
	sess := &Session{device: chosen, inputs: len(inSpecs)}
	keptComp, keptModel, keptMem, keptMap := comp, model, mem, mapping
	sess.run = func(ins []ExecIn) ([]ExecOut, error) {
		return runExecution(keptComp, inSpecs, outSpecs, ins)
	}
	sess.release = func() {
		if keptComp != 0 {
			call(nnLib.compFree, keptComp)
		}
		if keptModel != 0 {
			call(nnLib.modelFree, keptModel)
		}
		if keptMem != 0 {
			call(nnLib.memFree, keptMem)
		}
		if keptMap != nil {
			_ = unix.Munmap(keptMap)
		}
	}
	success = true
	return sess, nil
}

type portSpec struct {
	dt    DType
	shape []int
}

func portSpecs(b *Builder, ids []int) []portSpec {
	out := make([]portSpec, len(ids))
	for i, id := range ids {
		t := b.tensors[id]
		out[i] = portSpec{dt: t.dt, shape: append([]int(nil), t.shape...)}
	}
	return out
}

func runExecution(comp uintptr, inSpecs, outSpecs []portSpec, ins []ExecIn) ([]ExecOut, error) {
	shapes := make([][]int, len(inSpecs))
	for i, spec := range inSpecs {
		shapes[i] = spec.shape
	}
	dyn, err := dynamicLengthShapes(shapes, ins)
	if err != nil {
		return nil, err
	}
	if dyn <= 0 {
		return nil, fmt.Errorf("nnapi: empty execution")
	}
	execBox := heapUintptr()
	st := call(nnLib.execCreate, comp, uintptr(unsafe.Pointer(execBox)))
	runtime.KeepAlive(execBox)
	if st != 0 || *execBox == 0 {
		return nil, fmt.Errorf("nnapi: execution create %d", st)
	}
	exec := *execBox
	defer call(nnLib.execFree, exec)
	if nnLib.execTimeout != 0 {
		if st := call(nnLib.execTimeout, exec, uintptr(execTimeoutNS)); st != 0 {
			log.Printf("nnapi: execution setTimeout %d", st)
		}
	}

	var pin runtime.Pinner
	defer pin.Unpin()
	ots := heapOperandTypes(len(inSpecs) + len(outSpecs))
	var dimsKeep [][]uint32
	for i, spec := range inSpecs {
		shape := concrete(spec.shape, dyn)
		ptr, nbytes, err := bindInput(&pin, spec.dt, ins[i], shape, i)
		if err != nil {
			return nil, err
		}
		ot, dims := nnOperand(spec.dt, shape)
		ots[i] = ot
		dimsKeep = append(dimsKeep, dims)
		st := call(nnLib.execSetIn, exec, uintptr(i), uintptr(unsafe.Pointer(&ots[i])), ptr, uintptr(nbytes))
		runtime.KeepAlive(dims)
		if st != 0 {
			runtime.KeepAlive(ots)
			return nil, fmt.Errorf("nnapi: setInput %d: %d", i, st)
		}
	}
	outs := make([]ExecOut, len(outSpecs))
	for i, spec := range outSpecs {
		shape := concrete(spec.shape, dyn)
		n := product(shape)
		if n <= 0 {
			return nil, fmt.Errorf("nnapi: output %d empty", i)
		}
		ptr, nbytes := bindOutput(&pin, spec.dt, n, &outs[i])
		ot, dims := nnOperand(spec.dt, shape)
		ots[len(inSpecs)+i] = ot
		dimsKeep = append(dimsKeep, dims)
		st := call(nnLib.execSetOut, exec, uintptr(i), uintptr(unsafe.Pointer(&ots[len(inSpecs)+i])), ptr, uintptr(nbytes))
		runtime.KeepAlive(dims)
		if st != 0 {
			runtime.KeepAlive(ots)
			return nil, fmt.Errorf("nnapi: setOutput %d: %d", i, st)
		}
	}
	if st := call(nnLib.execCompute, exec); st != 0 {
		runtime.KeepAlive(ots)
		runtime.KeepAlive(dimsKeep)
		return nil, &execError{code: st}
	}
	runtime.KeepAlive(ots)
	runtime.KeepAlive(dimsKeep)
	return outs, nil
}

func bindInput(pin *runtime.Pinner, dt DType, in ExecIn, shape []int, index int) (uintptr, int, error) {
	n := product(shape)
	if dt == I32 {
		if len(in.I32) != n {
			return 0, 0, fmt.Errorf("nnapi: input %d len %d want %d", index, len(in.I32), n)
		}
		if n == 0 {
			return 0, 0, nil
		}
		pin.Pin(&in.I32[0])
		return uintptr(unsafe.Pointer(&in.I32[0])), n * 4, nil
	}
	if len(in.F32) != n {
		return 0, 0, fmt.Errorf("nnapi: input %d len %d want %d", index, len(in.F32), n)
	}
	if n == 0 {
		return 0, 0, nil
	}
	pin.Pin(&in.F32[0])
	return uintptr(unsafe.Pointer(&in.F32[0])), n * 4, nil
}

func bindOutput(pin *runtime.Pinner, dt DType, n int, dst *ExecOut) (uintptr, int) {
	if dt == I32 {
		buf := make([]int32, n)
		dst.I32 = buf
		pin.Pin(&buf[0])
		return uintptr(unsafe.Pointer(&buf[0])), n * 4
	}
	buf := make([]float32, n)
	dst.F32 = buf
	pin.Pin(&buf[0])
	return uintptr(unsafe.Pointer(&buf[0])), n * 4
}

func nnOperand(dt DType, shape []int) (operandType, []uint32) {
	dims := shapeU32(shape)
	return operandType{
		typ:            tensorNNType(dt),
		dimensionCount: uint32(len(dims)),
		dimensions:     u32ptr(dims),
	}, dims
}

func tensorNNType(dt DType) int32 {
	if dt == I32 {
		return nnTensorI32
	}
	return nnTensorF32
}

func shapeU32(shape []int) []uint32 {
	if len(shape) == 0 {
		return nil
	}
	dims := heapU32(len(shape))
	for i, d := range shape {
		if d < 0 {
			d = 0
		}
		dims[i] = uint32(d)
	}
	return dims
}

func u32ptr(dims []uint32) *uint32 {
	if len(dims) == 0 {
		return nil
	}
	return &dims[0]
}

func u32addr(v []uint32) uintptr {
	if len(v) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&v[0]))
}

func idsToU32(ids []int) []uint32 {
	out := heapU32(len(ids))
	for i, id := range ids {
		out[i] = uint32(id)
	}
	return out
}

type scalarSlot struct {
	typ int32
	raw []byte
	off int
}

type sKey struct {
	typ  int32
	bits int32
	n    int
}

type graphPlan struct {
	b         *Builder
	cache     map[sKey]int
	scalars   []scalarSlot
	tensorOff []int
	tensorN   []int
	size      int
	names     []string
}

func preparePlan(b *Builder) (*graphPlan, error) {
	p := &graphPlan{
		b:         b,
		cache:     make(map[sKey]int),
		tensorOff: make([]int, len(b.tensors)),
		tensorN:   make([]int, len(b.tensors)),
	}
	for i := range p.tensorOff {
		p.tensorOff[i] = -1
		t := b.tensors[i]
		n := 0
		if t.fillN > 0 {
			n = t.fillN * 4
		} else if t.f32 != nil {
			n = len(t.f32) * 4
		} else if t.i32 != nil {
			n = len(t.i32) * 4
		}
		if n == 0 {
			continue
		}
		p.tensorOff[i] = p.place(n)
		p.tensorN[i] = n
	}
	if err := p.walk(false, 0); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *graphPlan) place(n int) int {
	if p.size%64 != 0 {
		p.size += 64 - (p.size % 64)
	}
	at := p.size
	p.size += n
	return at
}

func (p *graphPlan) scalar(typ, bits int32, n int) uint32 {
	key := sKey{typ: typ, bits: bits, n: n}
	if id, ok := p.cache[key]; ok {
		return uint32(id)
	}
	raw := make([]byte, n)
	if n == 1 {
		raw[0] = byte(bits)
	} else {
		raw[0] = byte(bits)
		raw[1] = byte(uint32(bits) >> 8)
		raw[2] = byte(uint32(bits) >> 16)
		raw[3] = byte(uint32(bits) >> 24)
	}
	id := len(p.b.tensors) + len(p.scalars)
	p.scalars = append(p.scalars, scalarSlot{typ: typ, raw: raw, off: p.place(n)})
	p.cache[key] = id
	return uint32(id)
}

func uploadPlan(b *Builder, p *graphPlan) ([]byte, uintptr, error) {
	if p.size == 0 {
		return nil, 0, nil
	}
	name := cString("maclaw-npu")
	fdRaw := call(nnLib.ashm, uintptr(unsafe.Pointer(&name[0])), uintptr(p.size))
	runtime.KeepAlive(name)
	fd := int(int32(fdRaw))
	if fd < 0 {
		return nil, 0, fmt.Errorf("nnapi: ashmem create failed")
	}
	mapping, err := unix.Mmap(fd, 0, p.size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("nnapi: ashmem mmap: %w", err)
	}
	for i, t := range b.tensors {
		if p.tensorN[i] == 0 {
			continue
		}
		dst := mapping[p.tensorOff[i] : p.tensorOff[i]+p.tensorN[i]]
		if t.fill != nil {
			floats := unsafe.Slice((*float32)(unsafe.Pointer(&dst[0])), t.fillN)
			if err := t.fill(floats); err != nil {
				_ = unix.Munmap(mapping)
				_ = unix.Close(fd)
				return nil, 0, err
			}
			continue
		}
		if t.f32 != nil {
			copy(dst, unsafe.Slice((*byte)(unsafe.Pointer(&t.f32[0])), len(t.f32)*4))
		} else if len(t.i32) > 0 {
			copy(dst, unsafe.Slice((*byte)(unsafe.Pointer(&t.i32[0])), len(t.i32)*4))
		} else {
			_ = unix.Munmap(mapping)
			_ = unix.Close(fd)
			return nil, 0, fmt.Errorf("nnapi: constant %d has no bytes", i)
		}
	}
	for _, slot := range p.scalars {
		copy(mapping[slot.off:slot.off+len(slot.raw)], slot.raw)
	}
	if err := unix.Msync(mapping, unix.MS_SYNC); err != nil {
		_ = unix.Munmap(mapping)
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("nnapi: ashmem msync: %w", err)
	}
	memBox := heapUintptr()
	st := call(nnLib.memFromFd, uintptr(p.size), uintptr(unix.PROT_READ), uintptr(fd), 0, uintptr(unsafe.Pointer(memBox)))
	_ = unix.Close(fd)
	runtime.KeepAlive(memBox)
	if st != 0 || *memBox == 0 {
		_ = unix.Munmap(mapping)
		return nil, 0, fmt.Errorf("nnapi: Memory_createFromFd %d", st)
	}
	return mapping, *memBox, nil
}

func (p *graphPlan) emit(model uintptr) error {
	return p.walk(true, model)
}

func (p *graphPlan) walk(emit bool, model uintptr) error {
	for i, op := range p.b.ops {
		code, name, ins, err := p.opInputs(op)
		if err != nil {
			return fmt.Errorf("nnapi: op %d: %w", i, err)
		}
		outs := heapU32(len(op.out))
		for j, id := range op.out {
			if id < 0 || id >= len(p.b.tensors) {
				return fmt.Errorf("nnapi: op %d bad output %d", i, id)
			}
			outs[j] = uint32(id)
		}
		if !emit {
			p.names = append(p.names, name)
			continue
		}
		st := call(nnLib.addOperation, model, uintptr(code), uintptr(len(ins)), u32addr(ins), uintptr(len(outs)), u32addr(outs))
		runtime.KeepAlive(ins)
		runtime.KeepAlive(outs)
		if st != 0 {
			return fmt.Errorf("nnapi: addOperation %s %d: %d", name, i, st)
		}
	}
	return nil
}

func nnOpCode(code opCode) int32 {
	switch code {
	case opAdd:
		return nnAdd
	case opMul:
		return nnMul
	case opSub:
		return nnSub
	case opFC:
		return nnFC
	case opReshape:
		return nnReshape
	case opTranspose:
		return nnTranspose
	case opSplit:
		return nnSplit
	case opBMM:
		return nnBMM
	case opSoftmax:
		return nnSoftmax
	case opDWConv:
		return nnDWConv
	case opMean:
		return nnMean
	case opRsqrt:
		return nnRsqrt
	case opArgMax:
		return nnArgMax
	case opTanh:
		return nnTanh
	case opGather:
		return nnGather
	default:
		return -1
	}
}

func (p *graphPlan) opInputs(op operation) (int32, string, []uint32, error) {
	name, kinds, err := wireInputs(op)
	if err != nil {
		return 0, "", nil, err
	}
	code := nnOpCode(op.code)
	if code < 0 {
		return 0, "", nil, fmt.Errorf("unknown op %d", op.code)
	}
	ins := heapU32(len(kinds))
	for i, kind := range kinds {
		id, err := p.wireOperand(op, kind)
		if err != nil {
			return 0, "", nil, err
		}
		ins[i] = id
	}
	return code, name, ins, nil
}

func (p *graphPlan) wireOperand(op operation, kind wireKind) (uint32, error) {
	switch kind {
	case wireT0:
		return uint32(op.in[0]), nil
	case wireT1:
		return uint32(op.in[1]), nil
	case wireT2:
		return uint32(op.in[2]), nil
	case wireAxis:
		ax := op.axis
		if op.code == opSoftmax && ax < 0 {
			ax += len(p.b.tensors[op.in[0]].shape)
		}
		return p.scalar(nnInt32, int32(ax), 4), nil
	case wireSplits:
		return p.scalar(nnInt32, int32(op.splits), 4), nil
	case wireFused:
		fused := nnFusedNone
		if op.relu && op.code == opFC {
			fused = nnFusedRelu
		}
		return p.scalar(nnInt32, fused, 4), nil
	case wireBeta:
		return p.scalar(nnFloat32, int32(math.Float32bits(1)), 4), nil
	case wireAdjX:
		v := int32(0)
		if op.adjX {
			v = 1
		}
		return p.scalar(nnBool, v, 1), nil
	case wireAdjY:
		v := int32(0)
		if op.adjY {
			v = 1
		}
		return p.scalar(nnBool, v, 1), nil
	case wirePadSame:
		return p.scalar(nnInt32, nnPadSame, 4), nil
	case wireStride1, wireDepthMul, wireKeepDims:
		return p.scalar(nnInt32, 1, 4), nil
	default:
		return 0, fmt.Errorf("operand kind %d", kind)
	}
}

func nnProbe() {
	nn, err := androidDLOpen("libneuralnetworks.so")
	if err != nil {
		nnErr = fmt.Errorf("nnapi: libneuralnetworks.so: %v", err)
		return
	}
	androidLib, err := androidDLOpen("libandroid.so")
	if err != nil {
		nnErr = fmt.Errorf("nnapi: libandroid.so: %v", err)
		return
	}
	load := func(h uintptr, name string) (uintptr, error) {
		p, symErr := androidDLSym(h, name)
		if symErr != nil || p == 0 {
			return 0, fmt.Errorf("nnapi: missing %s", name)
		}
		return p, nil
	}
	var symErr error
	take := func(h uintptr, name string) uintptr {
		if symErr != nil {
			return 0
		}
		p, err := load(h, name)
		if err != nil {
			symErr = err
			return 0
		}
		return p
	}
	nnLib.modelCreate = take(nn, "ANeuralNetworksModel_create")
	nnLib.modelFree = take(nn, "ANeuralNetworksModel_free")
	nnLib.modelFinish = take(nn, "ANeuralNetworksModel_finish")
	nnLib.addOperand = take(nn, "ANeuralNetworksModel_addOperand")
	nnLib.setFromMemory = take(nn, "ANeuralNetworksModel_setOperandValueFromMemory")
	nnLib.addOperation = take(nn, "ANeuralNetworksModel_addOperation")
	nnLib.identify = take(nn, "ANeuralNetworksModel_identifyInputsAndOutputs")
	nnLib.relax = take(nn, "ANeuralNetworksModel_relaxComputationFloat32toFloat16")
	nnLib.compForDevices = take(nn, "ANeuralNetworksCompilation_createForDevices")
	nnLib.compFree = take(nn, "ANeuralNetworksCompilation_free")
	nnLib.compPref = take(nn, "ANeuralNetworksCompilation_setPreference")
	nnLib.compFinish = take(nn, "ANeuralNetworksCompilation_finish")
	nnLib.execCreate = take(nn, "ANeuralNetworksExecution_create")
	nnLib.execFree = take(nn, "ANeuralNetworksExecution_free")
	nnLib.execSetIn = take(nn, "ANeuralNetworksExecution_setInput")
	nnLib.execSetOut = take(nn, "ANeuralNetworksExecution_setOutput")
	nnLib.execCompute = take(nn, "ANeuralNetworksExecution_compute")
	nnLib.devCount = take(nn, "ANeuralNetworks_getDeviceCount")
	nnLib.getDevice = take(nn, "ANeuralNetworks_getDevice")
	nnLib.devName = take(nn, "ANeuralNetworksDevice_getName")
	nnLib.devType = take(nn, "ANeuralNetworksDevice_getType")
	nnLib.devLevel = take(nn, "ANeuralNetworksDevice_getFeatureLevel")
	nnLib.memFromFd = take(nn, "ANeuralNetworksMemory_createFromFd")
	nnLib.memFree = take(nn, "ANeuralNetworksMemory_free")
	nnLib.supported = take(nn, "ANeuralNetworksModel_getSupportedOperationsForDevices")
	nnLib.ashm = take(androidLib, "ASharedMemory_create")
	if symErr != nil {
		nnErr = symErr
		return
	}
	nnLib.compCaching = maybeSym(nn, "ANeuralNetworksCompilation_setCaching")
	nnLib.compTimeout = maybeSym(nn, "ANeuralNetworksCompilation_setTimeout")
	nnLib.execTimeout = maybeSym(nn, "ANeuralNetworksExecution_setTimeout")
	// setOperandValue is part of the required surface even though constants
	// go through ashmem. A build without it is not a usable NNAPI.
	if _, err := load(nn, "ANeuralNetworksModel_setOperandValue"); err != nil {
		nnErr = err
		return
	}

	nBox := heapUint32()
	if st := call(nnLib.devCount, uintptr(unsafe.Pointer(nBox))); st != 0 {
		runtime.KeepAlive(nBox)
		nnErr = fmt.Errorf("nnapi: getDeviceCount %d", st)
		return
	}
	runtime.KeepAlive(nBox)
	n := *nBox
	for i := uint32(0); i < n; i++ {
		devBox := heapUintptr()
		if st := call(nnLib.getDevice, uintptr(i), uintptr(unsafe.Pointer(devBox))); st != 0 || *devBox == 0 {
			runtime.KeepAlive(devBox)
			continue
		}
		runtime.KeepAlive(devBox)
		dev := *devBox
		typBox := heapInt32()
		if st := call(nnLib.devType, dev, uintptr(unsafe.Pointer(typBox))); st != 0 || *typBox != nnDeviceAccel {
			runtime.KeepAlive(typBox)
			continue
		}
		runtime.KeepAlive(typBox)
		levelBox := heapInt64()
		if st := call(nnLib.devLevel, dev, uintptr(unsafe.Pointer(levelBox))); st != 0 || *levelBox < nnFeatureLevel6 {
			runtime.KeepAlive(levelBox)
			continue
		}
		level := *levelBox
		runtime.KeepAlive(levelBox)
		nameBox := heapUintptr()
		name := ""
		if call(nnLib.devName, dev, uintptr(unsafe.Pointer(nameBox))) == 0 {
			name = goCString(*nameBox)
		}
		runtime.KeepAlive(nameBox)
		if name == "" {
			name = fmt.Sprintf("accelerator-%d", i)
		}
		nnAccels = append(nnAccels, nnAccel{ptr: dev, name: name, level: level})
	}
	if len(nnAccels) == 0 {
		nnErr = fmt.Errorf("nnapi: no accelerator at feature level 6")
		return
	}
}

func maybeSym(handle uintptr, name string) uintptr {
	p, err := androidDLSym(handle, name)
	if err != nil {
		return 0
	}
	return p
}

func call(fn uintptr, args ...uintptr) int32 {
	if fn == 0 {
		return -1
	}
	return int32(androidCall(fn, args...))
}

func cString(s string) []byte {
	b := heapBytes(len(s) + 1)
	copy(b, s)
	return b
}

// The helpers below are noinline so the allocation escapes to the heap
// inside the helper. Inlining would let the compiler put the object back
// on the caller stack, which is the bug these exist to avoid.

//go:noinline
func heapUintptr() *uintptr { return new(uintptr) }

//go:noinline
func heapUint32() *uint32 { return new(uint32) }

//go:noinline
func heapInt32() *int32 { return new(int32) }

//go:noinline
func heapInt64() *int64 { return new(int64) }

//go:noinline
func heapBytes(n int) []byte { return make([]byte, n) }

//go:noinline
func heapU32(n int) []uint32 { return make([]uint32, n) }

//go:noinline
func heapOperandTypes(n int) []operandType { return make([]operandType, n) }

//go:noinline
func heapToken() *[32]byte { return new([32]byte) }

func goCString(p uintptr) string {
	if p == 0 {
		return ""
	}
	buf := make([]byte, 0, 32)
	for i := 0; i < 256; i++ {
		b := *(*byte)(unsafe.Pointer(p + uintptr(i)))
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}
