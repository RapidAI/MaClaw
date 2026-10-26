package vk

import (
	"runtime"
	"unsafe"
)

// Encoder records compute dispatches into one command buffer and submits them
// as a single batch on Wait. Recording the whole forward pass into one command
// buffer is what keeps CPU overhead low: one submit + one fence wait per
// forward, not one per dispatch.
type Encoder struct {
	dev    *Device
	active bool
	failed error
	cur    *Pipeline
}

// Begin starts recording. Must be paired with Wait.
func (d *Device) Begin() *Encoder {
	e := &Encoder{dev: d}
	if err := d.Err(); err != nil {
		e.failed = err
		return e
	}
	bi := commandBufferBeginInfo{
		sType: structureTypeCommandBufferBeginInfo,
		flags: commandBufferUsageOneTimeSubmit,
	}
	call(d.fn.resetCommandBuffer, d.cmdBuf, 0)
	if err := check(call(d.fn.beginCommandBuffer, d.cmdBuf, uintptr(unsafe.Pointer(&bi))), "vkBeginCommandBuffer"); err != nil {
		e.failed = err
		return e
	}
	e.active = true
	return e
}

// SetPipeline binds a compute pipeline and its descriptor set.
func (e *Encoder) SetPipeline(p *Pipeline) {
	if e == nil || !e.active || p == nil {
		return
	}
	d := e.dev
	if err := p.BindErr(); err != nil {
		e.failed = err
		return
	}
	call(d.fn.cmdBindPipeline, d.cmdBuf, pipelineBindPointCompute, p.pipe)
	if set := p.CurrentSet(); set != 0 {
		call(d.fn.cmdBindDescriptorSets, d.cmdBuf, pipelineBindPointCompute, p.layout,
			0, 1, uintptr(unsafe.Pointer(&set)), 0, 0)
	}
	e.cur = p
}

// SetBytes pushes a push-constant block for the currently bound pipeline.
func (e *Encoder) SetBytes(ptr unsafe.Pointer, n, index int) {
	if e == nil || !e.active || e.cur == nil || n <= 0 {
		return
	}
	d := e.dev
	call(d.fn.cmdPushConstants, d.cmdBuf, e.cur.layout, shaderStageCompute, 0, uintptr(n), uintptr(ptr))
}

// Dispatch records a compute dispatch. Workgroup sizes are the shader's
// (local_size_*), so gx/gy/gz are group counts.
func (e *Encoder) Dispatch(gx, gy, gz, tx, ty, tz int) {
	if e == nil || !e.active || gx <= 0 {
		return
	}
	if gx < 0 {
		gx = 1
	}
	if gy < 0 {
		gy = 1
	}
	if gz < 0 {
		gz = 1
	}
	call(e.dev.fn.cmdDispatch, e.dev.cmdBuf, uintptr(gx), uintptr(gy), uintptr(gz))
}

// Barrier inserts a compute->compute memory barrier so a following dispatch
// sees the previous dispatch's writes. Vulkan does not insert these
// automatically, unlike Metal's serial encoders.
func (e *Encoder) Barrier() {
	if e == nil || !e.active {
		return
	}
	mb := memoryBarrier{
		sType:         structureTypeMemoryBarrier,
		srcAccessMask: accessShaderWrite,
		dstAccessMask: accessShaderRead | accessShaderWrite,
	}
	call(e.dev.fn.cmdPipelineBarrier, e.dev.cmdBuf,
		uintptr(pipelineStageComputeShader), uintptr(pipelineStageComputeShader),
		0, 1, uintptr(unsafe.Pointer(&mb)), 0, 0, 0, 0)
}

// Wait ends recording, submits once, and blocks until the GPU finishes.
func (e *Encoder) Wait() error {
	if e == nil {
		return nil
	}
	if e.failed != nil {
		return e.failed
	}
	if !e.active {
		return nil
	}
	e.active = false
	d := e.dev
	if err := check(call(d.fn.endCommandBuffer, d.cmdBuf), "vkEndCommandBuffer"); err != nil {
		return err
	}
	si := submitInfo{
		sType:              structureTypeSubmitInfo,
		commandBufferCount: 1,
		pCommandBuffers:    uintptr(unsafe.Pointer(&d.cmdBuf)),
	}
	call(d.fn.resetFences, d.dev, 1, uintptr(unsafe.Pointer(&d.fence)))
	if err := check(call(d.fn.queueSubmit, d.queue, 1, uintptr(unsafe.Pointer(&si)), d.fence), "vkQueueSubmit"); err != nil {
		return err
	}
	// waitForFences(dev, 1, &fence, VK_TRUE, UINT64_MAX)
	if err := check(call(d.fn.waitForFences, d.dev, 1, uintptr(unsafe.Pointer(&d.fence)), 1, ^uintptr(0)), "vkWaitForFences"); err != nil {
		return err
	}
	runtime.KeepAlive(si)
	return nil
}
