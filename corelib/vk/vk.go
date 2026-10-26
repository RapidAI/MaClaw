package vk

// vk.go — minimal Vulkan 1.0 compute binding in pure Go.
//
// Only what a single-queue compute workload needs: instance, one physical
// device, one compute queue, host-visible storage buffers, compute pipelines,
// one descriptor set per pipeline, push constants, and serial submits.
//
// Every handle is a uintptr (Vulkan's handles are pointers or uint64 on 64-bit
// ABIs) and every struct is laid out by hand to match the C ABI, so the
// package builds with CGO_ENABLED=0 on windows/linux/darwin.

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Vulkan constants (values are ABI-frozen).
const (
	structureTypeApplicationInfo           = 0
	structureTypeInstanceCreateInfo        = 1
	structureTypeDeviceQueueCreateInfo     = 2
	structureTypeDeviceCreateInfo          = 3
	structureTypeSubmitInfo                = 4
	structureTypeMemoryAllocateInfo        = 5
	structureTypeFenceCreateInfo           = 8
	structureTypeBufferCreateInfo          = 12
	structureTypeShaderModuleCreateInfo    = 16
	structureTypePipelineShaderStageCreate = 18
	structureTypeComputePipelineCreateInfo = 29
	structureTypeDescriptorSetLayoutCreate = 32
	structureTypePipelineLayoutCreateInfo  = 30
	structureTypeDescriptorPoolCreateInfo  = 33
	structureTypeDescriptorSetAllocateInfo = 34
	structureTypeWriteDescriptorSet        = 35
	structureTypeCommandPoolCreateInfo     = 39
	structureTypeCommandBufferAllocateInfo = 40
	structureTypeCommandBufferBeginInfo    = 42
	structureTypeMemoryBarrier             = 46
	structureTypeBufferMemoryBarrier       = 44

	success = 0

	bufferUsageStorageBuffer = 0x20

	sharingModeExclusive = 0

	memoryPropertyDeviceLocal  = 0x1
	memoryPropertyHostVisible  = 0x2
	memoryPropertyHostCoherent = 0x4

	descriptorTypeStorageBuffer = 7

	shaderStageCompute = 0x20

	pipelineBindPointCompute = 1

	queueCompute = 0x2

	physicalDeviceTypeCPU = 4

	commandPoolCreateResetCommandBuffer = 0x2
	commandBufferUsageOneTimeSubmit     = 0x1

	accessShaderRead  = 0x20
	accessShaderWrite = 0x40

	pipelineStageComputeShader = 0x800

	maxPushConstantsSize = 128
)

// DeviceInfo describes the GPU a Device was created on.
type DeviceInfo struct {
	Name       string
	Type       int
	APIVersion uint32
	IsCPU      bool
}

// Device owns a VkDevice with one compute queue.
type Device struct {
	inst    uintptr
	phys    uintptr
	dev     uintptr
	queue   uintptr
	family  uint32
	Info    DeviceInfo
	memIdx  uint32 // host-visible+coherent memory type index
	memSize uint64
	cmdPool uintptr
	cmdBuf  uintptr
	fence   uintptr

	// Function pointers resolved through vkGetInstanceProcAddr.
	fn struct {
		createBuffer, getBufferMemoryRequirements, allocateMemory, bindBufferMemory,
		mapMemory, unmapMemory, destroyBuffer, freeMemory,
		createShaderModule, destroyShaderModule,
		createDescriptorSetLayout, destroyDescriptorSetLayout,
		createPipelineLayout, destroyPipelineLayout,
		createDescriptorPool, destroyDescriptorPool,
		allocateDescriptorSets, updateDescriptorSets,
		createComputePipeline, destroyPipeline,
		createCommandPool, destroyCommandPool, allocateCommandBuffers, freeCommandBuffers,
		beginCommandBuffer, endCommandBuffer, resetCommandBuffer,
		cmdBindPipeline, cmdBindDescriptorSets, cmdPushConstants, cmdDispatch, cmdPipelineBarrier,
		queueSubmit, queueWaitIdle, createFence, destroyFence, waitForFences, resetFences,
		deviceWaitIdle, destroyDevice uintptr
	}
	destroyInstance uintptr
	mu              sync.Mutex
}

// ErrUnavailable reports that no usable Vulkan compute device exists.
var ErrUnavailable = errors.New("vk: Vulkan unavailable")

// ErrNoMemory reports that a buffer allocation exceeded the host-visible heap.
var ErrNoMemory = errors.New("vk: out of host-visible memory")

func check(r uintptr, what string) error {
	if int32(r) == success {
		return nil
	}
	return fmt.Errorf("vk: %s failed (VkResult %d)", what, int32(r))
}

// cstr returns a NUL-terminated copy of s.
func cstr(s string) []byte { return append([]byte(s), 0) }

// call invokes a resolved Vulkan function pointer.
func call(ptr uintptr, args ...uintptr) uintptr {
	if ptr == 0 {
		return 1 // VK_ERROR_INITIALIZATION_FAILED
	}
	return syscallN(ptr, args...)
}

// ---- struct layouts (64-bit ABI) ----

type applicationInfo struct {
	sType              int32
	_                  int32
	pNext              uintptr
	pApplicationName   uintptr
	applicationVersion uint32
	pEngineName        uintptr
	engineVersion      uint32
	apiVersion         uint32
}

type instanceCreateInfo struct {
	sType                   int32
	_                       int32
	pNext                   uintptr
	flags                   uint32
	_                       uint32
	pApplicationInfo        uintptr
	enabledLayerCount       uint32
	_                       uint32
	ppEnabledLayerNames     uintptr
	enabledExtensionCount   uint32
	_                       uint32
	ppEnabledExtensionNames uintptr
}

type deviceQueueCreateInfo struct {
	sType            int32
	_                int32
	pNext            uintptr
	flags            uint32
	queueFamilyIndex uint32
	queueCount       uint32
	_                uint32
	pQueuePriorities uintptr
}

type deviceCreateInfo struct {
	sType                   int32
	_                       int32
	pNext                   uintptr
	flags                   uint32
	queueCreateInfoCount    uint32
	pQueueCreateInfos       uintptr
	enabledLayerCount       uint32
	_                       uint32
	ppEnabledLayerNames     uintptr
	enabledExtensionCount   uint32
	_                       uint32
	ppEnabledExtensionNames uintptr
	pEnabledFeatures        uintptr
}

type bufferCreateInfo struct {
	sType                 int32
	_                     int32
	pNext                 uintptr
	flags                 uint32
	size                  uint64
	usage                 uint32
	sharingMode           uint32
	queueFamilyIndexCount uint32
	_                     uint32
	pQueueFamilyIndices   uintptr
}

type memoryRequirements struct {
	size           uint64
	alignment      uint64
	memoryTypeBits uint32
}

type memoryAllocateInfo struct {
	sType           int32
	_               int32
	pNext           uintptr
	allocationSize  uint64
	memoryTypeIndex uint32
}

type shaderModuleCreateInfo struct {
	sType    int32
	_        int32
	pNext    uintptr
	flags    uint32
	_        uint32
	codeSize uintptr
	pCode    uintptr
}

type descriptorSetLayoutBinding struct {
	binding            uint32
	descriptorType     uint32
	descriptorCount    uint32
	stageFlags         uint32
	pImmutableSamplers uintptr
}

type descriptorSetLayoutCreateInfo struct {
	sType        int32
	_            int32
	pNext        uintptr
	flags        uint32
	bindingCount uint32
	pBindings    uintptr
}

type pushConstantRange struct {
	stageFlags uint32
	offset     uint32
	size       uint32
}

type pipelineLayoutCreateInfo struct {
	sType                  int32
	_                      int32
	pNext                  uintptr
	flags                  uint32
	setLayoutCount         uint32
	pSetLayouts            uintptr
	pushConstantRangeCount uint32
	_                      uint32
	pPushConstantRanges    uintptr
}

type pipelineShaderStageCreateInfo struct {
	sType               int32
	_                   int32
	pNext               uintptr
	flags               uint32
	stage               uint32
	module              uintptr
	pName               uintptr
	pSpecializationInfo uintptr
}

type computePipelineCreateInfo struct {
	sType              int32
	_                  int32
	pNext              uintptr
	flags              uint32
	_                  uint32
	stage              pipelineShaderStageCreateInfo
	layout             uintptr
	basePipelineHandle uintptr
	basePipelineIndex  int32
	_                  int32
}

type descriptorPoolSize struct {
	typ             uint32
	descriptorCount uint32
}

type descriptorPoolCreateInfo struct {
	sType         int32
	_             int32
	pNext         uintptr
	flags         uint32
	maxSets       uint32
	poolSizeCount uint32
	_             uint32
	pPoolSizes    uintptr
}

type descriptorSetAllocateInfo struct {
	sType              int32
	_                  int32
	pNext              uintptr
	descriptorPool     uintptr
	descriptorSetCount uint32
	_                  uint32
	pSetLayouts        uintptr
}

type descriptorBufferInfo struct {
	buffer uintptr
	offset uint64
	rng    uint64
}

type writeDescriptorSet struct {
	sType            int32
	_                int32
	pNext            uintptr
	dstSet           uintptr
	dstBinding       uint32
	dstArrayElement  uint32
	descriptorCount  uint32
	descriptorType   uint32
	pImageInfo       uintptr
	pBufferInfo      uintptr
	pTexelBufferView uintptr
}

type commandPoolCreateInfo struct {
	sType            int32
	_                int32
	pNext            uintptr
	flags            uint32
	queueFamilyIndex uint32
}

type commandBufferAllocateInfo struct {
	sType              int32
	_                  int32
	pNext              uintptr
	commandPool        uintptr
	level              uint32
	commandBufferCount uint32
}

type commandBufferBeginInfo struct {
	sType            int32
	_                int32
	pNext            uintptr
	flags            uint32
	_                uint32
	pInheritanceInfo uintptr
}

type memoryBarrier struct {
	sType         int32
	_             int32
	pNext         uintptr
	srcAccessMask uint32
	dstAccessMask uint32
}

type submitInfo struct {
	sType                int32
	_                    int32
	pNext                uintptr
	waitSemaphoreCount   uint32
	_                    uint32
	pWaitSemaphores      uintptr
	pWaitDstStageMask    uintptr
	commandBufferCount   uint32
	_                    uint32
	pCommandBuffers      uintptr
	signalSemaphoreCount uint32
	_                    uint32
	pSignalSemaphores    uintptr
}

type fenceCreateInfo struct {
	sType int32
	_     int32
	pNext uintptr
	flags uint32
}

func ptrOf(v any) uintptr { return uintptr(unsafe.Pointer(v.(*byte))) }
