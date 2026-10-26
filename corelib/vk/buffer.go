package vk

import (
	"fmt"
	"runtime"
	"unsafe"
)

// Buffer is a host-visible, host-coherent storage buffer. CPU and GPU share
// the same memory, so Bytes/Float32s/Int32s need no explicit flush and no
// staging copy — the same model as the Metal shared-storage backend.
type Buffer struct {
	dev    *Device
	buf    uintptr
	mem    uintptr
	ptr    unsafe.Pointer
	Size   int
	closed bool
}

// NewBuffer allocates n bytes of host-visible coherent storage.
func (d *Device) NewBuffer(n int) (*Buffer, error) {
	if err := d.Err(); err != nil {
		return nil, err
	}
	if n <= 0 {
		n = 4
	}
	ci := bufferCreateInfo{
		sType:       structureTypeBufferCreateInfo,
		size:        uint64(n),
		usage:       bufferUsageStorageBuffer,
		sharingMode: sharingModeExclusive,
	}
	var buf uintptr
	if err := check(call(d.fn.createBuffer, d.dev, uintptr(unsafe.Pointer(&ci)), 0, uintptr(unsafe.Pointer(&buf))), "vkCreateBuffer"); err != nil {
		return nil, err
	}
	var req memoryRequirements
	call(d.fn.getBufferMemoryRequirements, d.dev, buf, uintptr(unsafe.Pointer(&req)))
	ai := memoryAllocateInfo{
		sType:           structureTypeMemoryAllocateInfo,
		allocationSize:  req.size,
		memoryTypeIndex: d.memIdx,
	}
	var mem uintptr
	if err := check(call(d.fn.allocateMemory, d.dev, uintptr(unsafe.Pointer(&ai)), 0, uintptr(unsafe.Pointer(&mem))), "vkAllocateMemory"); err != nil {
		call(d.fn.destroyBuffer, d.dev, buf, 0)
		return nil, fmt.Errorf("%w: %v", ErrNoMemory, err)
	}
	if err := check(call(d.fn.bindBufferMemory, d.dev, buf, mem, 0), "vkBindBufferMemory"); err != nil {
		call(d.fn.destroyBuffer, d.dev, buf, 0)
		call(d.fn.freeMemory, d.dev, mem, 0)
		return nil, err
	}
	var mapped unsafe.Pointer
	if err := check(call(d.fn.mapMemory, d.dev, mem, 0, uintptr(req.size), 0, uintptr(unsafe.Pointer(&mapped))), "vkMapMemory"); err != nil {
		call(d.fn.destroyBuffer, d.dev, buf, 0)
		call(d.fn.freeMemory, d.dev, mem, 0)
		return nil, err
	}
	return &Buffer{dev: d, buf: buf, mem: mem, ptr: mapped, Size: n}, nil
}

// Bytes exposes the mapped storage (len == Size).
func (b *Buffer) Bytes() []byte {
	if b == nil || b.ptr == nil {
		return nil
	}
	return unsafe.Slice((*byte)(b.ptr), b.Size)
}

// Float32s views the storage as float32 (len == Size/4).
func (b *Buffer) Float32s() []float32 {
	if b == nil || b.ptr == nil {
		return nil
	}
	return unsafe.Slice((*float32)(b.ptr), b.Size/4)
}

// Int32s views the storage as int32 (len == Size/4).
func (b *Buffer) Int32s() []int32 {
	if b == nil || b.ptr == nil {
		return nil
	}
	return unsafe.Slice((*int32)(b.ptr), b.Size/4)
}

// Release frees the buffer and its memory.
func (b *Buffer) Release() {
	if b == nil || b.closed {
		return
	}
	b.closed = true
	d := b.dev
	// vkDestroyBuffer must precede vkFreeMemory: freeing memory that a live
	// buffer still references is a spec violation that corrupted the AMD
	// driver's internal state and crashed a later vkCreateBuffer.
	if b.mem != 0 {
		call(d.fn.unmapMemory, d.dev, b.mem)
	}
	if b.buf != 0 {
		call(d.fn.destroyBuffer, d.dev, b.buf, 0)
	}
	if b.mem != 0 {
		call(d.fn.freeMemory, d.dev, b.mem, 0)
	}
	b.ptr, b.buf, b.mem = nil, 0, 0
}

// Pipeline is a compute pipeline plus its descriptor set and layout.
type Pipeline struct {
	dev       *Device
	pipe      uintptr
	layout    uintptr
	setLayout uintptr
	pool      uintptr                // descriptor pool for this pipeline's sets
	sets      map[[8]uintptr]uintptr // descriptor set per buffer combination
	cur       uintptr                // set selected by the last Bind
	bindings  int
	pushSize  int
	closed    bool
	bindErr   error // last Bind failure; surfaced by the encoder at Wait
}

// Compile builds a compute pipeline from SPIR-V for the given bindings
// (number of storage buffers) and push-constant size in bytes.
func (d *Device) Compile(spirv []byte, bindings, pushSize int) (*Pipeline, error) {
	if len(spirv) < 20 || len(spirv)%4 != 0 {
		return nil, fmt.Errorf("vk: invalid SPIR-V (%d bytes)", len(spirv))
	}
	if pushSize > maxPushConstantsSize {
		return nil, fmt.Errorf("vk: push constants %d > %d", pushSize, maxPushConstantsSize)
	}
	// Shader module.
	sci := shaderModuleCreateInfo{
		sType:    structureTypeShaderModuleCreateInfo,
		codeSize: uintptr(len(spirv)),
		pCode:    uintptr(unsafe.Pointer(&spirv[0])),
	}
	var mod uintptr
	if err := check(call(d.fn.createShaderModule, d.dev, uintptr(unsafe.Pointer(&sci)), 0, uintptr(unsafe.Pointer(&mod))), "vkCreateShaderModule"); err != nil {
		return nil, err
	}
	defer call(d.fn.destroyShaderModule, d.dev, mod, 0)

	p := &Pipeline{dev: d, bindings: bindings, pushSize: pushSize, sets: map[[8]uintptr]uintptr{}}
	if err := p.createLayouts(); err != nil {
		p.Release()
		return nil, err
	}
	name := cstr("main")
	stage := pipelineShaderStageCreateInfo{
		sType:  structureTypePipelineShaderStageCreate,
		stage:  shaderStageCompute,
		module: mod,
		pName:  uintptr(unsafe.Pointer(&name[0])),
	}
	ci := computePipelineCreateInfo{
		sType:  structureTypeComputePipelineCreateInfo,
		stage:  stage,
		layout: p.layout,
	}
	// vkCreateComputePipelines(device, pipelineCache, createInfoCount,
	//                          pCreateInfos, pAllocator, pPipelines)
	if err := check(call(d.fn.createComputePipeline, d.dev, 0, 1,
		uintptr(unsafe.Pointer(&ci)), 0, uintptr(unsafe.Pointer(&p.pipe))), "vkCreateComputePipelines"); err != nil {
		runtime.KeepAlive(name)
		p.Release()
		return nil, err
	}
	runtime.KeepAlive(name)
	// Sets are allocated lazily by Bind (one per buffer combination), so
	// nothing to pre-allocate here.
	return p, nil
}

func (p *Pipeline) createLayouts() error {
	d := p.dev
	var bindings []descriptorSetLayoutBinding
	if p.bindings > 0 {
		bindings = make([]descriptorSetLayoutBinding, p.bindings)
		for i := range bindings {
			bindings[i] = descriptorSetLayoutBinding{
				binding:         uint32(i),
				descriptorType:  descriptorTypeStorageBuffer,
				descriptorCount: 1,
				stageFlags:      shaderStageCompute,
			}
		}
	}
	dsl := descriptorSetLayoutCreateInfo{
		sType:        structureTypeDescriptorSetLayoutCreate,
		bindingCount: uint32(len(bindings)),
	}
	if len(bindings) > 0 {
		dsl.pBindings = uintptr(unsafe.Pointer(&bindings[0]))
	}
	if err := check(call(d.fn.createDescriptorSetLayout, d.dev, uintptr(unsafe.Pointer(&dsl)), 0, uintptr(unsafe.Pointer(&p.setLayout))), "vkCreateDescriptorSetLayout"); err != nil {
		return err
	}
	var pcr pushConstantRange
	plci := pipelineLayoutCreateInfo{
		sType:          structureTypePipelineLayoutCreateInfo,
		setLayoutCount: 1,
		pSetLayouts:    uintptr(unsafe.Pointer(&p.setLayout)),
	}
	if p.pushSize > 0 {
		pcr = pushConstantRange{stageFlags: shaderStageCompute, offset: 0, size: uint32(p.pushSize)}
		plci.pushConstantRangeCount = 1
		plci.pPushConstantRanges = uintptr(unsafe.Pointer(&pcr))
	}
	if err := check(call(d.fn.createPipelineLayout, d.dev, uintptr(unsafe.Pointer(&plci)), 0, uintptr(unsafe.Pointer(&p.layout))), "vkCreatePipelineLayout"); err != nil {
		return err
	}
	runtime.KeepAlive(bindings)
	return nil
}

// maxSetsPerPipeline bounds the descriptor-set cache. The forward pass uses a
// fixed, small number of distinct buffer combinations per kernel (matmul: ~6,
// norm: 1, rope/attn/silu: 1 each), so this is generous.
const maxSetsPerPipeline = 32

// ensurePool creates the pipeline's descriptor pool on first use.
//
// Sets are never invalidated: the activation buffers are allocated once at
// load, so a cached set always refers to a live buffer. (Resetting or
// recreating sets was tried and crashed the AMD driver inside vkCreateBuffer —
// see qwen3VKMaxRows — so the simpler model is also the safer one.)
func (p *Pipeline) ensurePool() error {
	d := p.dev
	if p.bindings == 0 || p.pool != 0 {
		return nil
	}
	ps := descriptorPoolSize{typ: descriptorTypeStorageBuffer, descriptorCount: uint32(p.bindings * maxSetsPerPipeline)}
	poolInfo := descriptorPoolCreateInfo{
		sType:         structureTypeDescriptorPoolCreateInfo,
		maxSets:       maxSetsPerPipeline,
		poolSizeCount: 1,
		pPoolSizes:    uintptr(unsafe.Pointer(&ps)),
	}
	return check(call(d.fn.createDescriptorPool, d.dev, uintptr(unsafe.Pointer(&poolInfo)), 0, uintptr(unsafe.Pointer(&p.pool))), "vkCreateDescriptorPool")
}

// Bind records the descriptor set contents (buffer per binding) and remembers
// the set so SetPipeline can bind it.
//
// Vulkan descriptor sets are read at submit time, not at record time, so a
// set that is rewritten between recorded dispatches would make every dispatch
// see the final contents. Bind therefore allocates (and caches) one descriptor
// set per distinct buffer combination, and the encoder binds the matching set
// with the pipeline. The cache is keyed on the buffer handles.
func (p *Pipeline) Bind(buffers ...*Buffer) {
	if p == nil {
		return
	}
	n := len(buffers)
	if n > p.bindings {
		n = p.bindings
	}
	var key [8]uintptr
	for i := 0; i < n; i++ {
		if buffers[i] != nil {
			key[i] = buffers[i].buf
		}
	}
	if s, ok := p.sets[key]; ok {
		p.cur = s
		return
	}
	set, err := p.newSet()
	if err != nil {
		p.cur = 0
		p.bindErr = err
		return
	}
	p.bindErr = nil
	d := p.dev
	infos := make([]descriptorBufferInfo, n)
	writes := make([]writeDescriptorSet, n)
	for i := 0; i < n; i++ {
		if buffers[i] != nil {
			infos[i] = descriptorBufferInfo{buffer: buffers[i].buf, offset: 0, rng: uint64(buffers[i].Size)}
		}
		writes[i] = writeDescriptorSet{
			sType:           structureTypeWriteDescriptorSet,
			dstSet:          set,
			dstBinding:      uint32(i),
			descriptorCount: 1,
			descriptorType:  descriptorTypeStorageBuffer,
			pBufferInfo:     uintptr(unsafe.Pointer(&infos[i])),
		}
	}
	if n > 0 {
		call(d.fn.updateDescriptorSets, d.dev, uintptr(n), uintptr(unsafe.Pointer(&writes[0])), 0, 0)
	}
	p.sets[key] = set
	p.cur = set
	runtime.KeepAlive(infos)
	runtime.KeepAlive(writes)
}

// BindErr reports the last Bind failure (nil when the binding is usable).
func (p *Pipeline) BindErr() error {
	if p == nil {
		return nil
	}
	return p.bindErr
}

// CurrentSet returns the descriptor set selected by the last Bind call.
func (p *Pipeline) CurrentSet() uintptr {
	if p == nil {
		return 0
	}
	return p.cur
}

// newSet allocates one descriptor set from the pipeline's pool.
func (p *Pipeline) newSet() (uintptr, error) {
	if err := p.ensurePool(); err != nil {
		return 0, err
	}
	d := p.dev
	ai := descriptorSetAllocateInfo{
		sType:              structureTypeDescriptorSetAllocateInfo,
		descriptorPool:     p.pool,
		descriptorSetCount: 1,
		pSetLayouts:        uintptr(unsafe.Pointer(&p.setLayout)),
	}
	var set uintptr
	if err := check(call(d.fn.allocateDescriptorSets, d.dev, uintptr(unsafe.Pointer(&ai)), uintptr(unsafe.Pointer(&set))), "vkAllocateDescriptorSets"); err != nil {
		return 0, err
	}
	return set, nil
}

// Release destroys the pipeline, layout, and descriptor objects.
func (p *Pipeline) Release() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	d := p.dev
	if p.pipe != 0 {
		call(d.fn.destroyPipeline, d.dev, p.pipe, 0)
	}
	if p.pool != 0 {
		call(d.fn.destroyDescriptorPool, d.dev, p.pool, 0)
	}
	if p.layout != 0 {
		call(d.fn.destroyPipelineLayout, d.dev, p.layout, 0)
	}
	if p.setLayout != 0 {
		call(d.fn.destroyDescriptorSetLayout, d.dev, p.setLayout, 0)
	}
	p.pipe, p.pool, p.layout, p.setLayout, p.cur = 0, 0, 0, 0, 0
	p.sets = map[[8]uintptr]uintptr{}
}
