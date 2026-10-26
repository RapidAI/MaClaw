package vk

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// physicalDevice is the subset of VkPhysicalDeviceProperties we read. The
// struct is 824 bytes in the C ABI; only the leading fields are needed.
type physicalDeviceProperties struct {
	apiVersion    uint32
	driverVersion uint32
	vendorID      uint32
	deviceID      uint32
	deviceType    uint32
	deviceName    [256]byte
	pipelineCacheUUID [16]byte
	// Remaining limits/sparse fields are ignored (kept for size correctness).
	rest [512]byte
}

type physicalDeviceMemoryProperties struct {
	memoryTypeCount uint32
	memoryTypes     [32]struct {
		propertyFlags uint32
		heapIndex     uint32
	}
	memoryHeapCount uint32
	memoryHeaps     [16]struct {
		size  uint64
		flags uint32
	}
}

type queueFamilyProperties struct {
	queueFlags   uint32
	queueCount   uint32
	timestampValidBits uint32
	minImageTransferGranularity struct{ width, height, depth uint32 }
}

var (
	globalLoader *vkLoader
	globalInst   uintptr
	loaderOnce   sync.Once
	loaderErr    error
	loaderRefs   int
	loaderMu     sync.Mutex
)

// NewDevice creates a compute-capable Vulkan device. Only one instance is
// created per process and shared by every Device; Close decrements a refcount.
func NewDevice() (*Device, error) {
	inst, err := sharedInstance()
	if err != nil {
		return nil, err
	}
	d, err := newDeviceOnInstance(inst)
	if err != nil {
		releaseSharedInstance()
		return nil, err
	}
	return d, nil
}

func gp(which int) uintptr { return instanceProcAddr[which] }

const (
	gipaFn = iota
	instanceProcAddrCount
)

var instanceProcAddr [instanceProcAddrCount]uintptr

// sharedInstance lazily creates the process-wide VkInstance.
func sharedInstance() (uintptr, error) {
	loaderMu.Lock()
	defer loaderMu.Unlock()
	if globalInst != 0 {
		loaderRefs++
		return globalInst, nil
	}
	l, err := openLoader()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	gipaPtr := l.sym("vkGetInstanceProcAddr")
	if gipaPtr == 0 {
		l.close()
		return 0, fmt.Errorf("%w: vkGetInstanceProcAddr missing", ErrUnavailable)
	}
	instanceProcAddr[gipaFn] = gipaPtr
	createInstance := resolve(0, gipaPtr, "vkCreateInstance")
	if createInstance == 0 {
		l.close()
		return 0, fmt.Errorf("%w: vkCreateInstance missing", ErrUnavailable)
	}
	appName := cstr("maclaw-embedding")
	engName := cstr("maclaw")
	app := applicationInfo{
		sType:            structureTypeApplicationInfo,
		pApplicationName: uintptr(unsafe.Pointer(&appName[0])),
		pEngineName:      uintptr(unsafe.Pointer(&engName[0])),
		apiVersion:       1 << 22, // 1.0
	}
	ci := instanceCreateInfo{
		sType:            structureTypeInstanceCreateInfo,
		pApplicationInfo: uintptr(unsafe.Pointer(&app)),
	}
	var inst uintptr
	if err := check(call(createInstance, uintptr(unsafe.Pointer(&ci)), 0, uintptr(unsafe.Pointer(&inst))), "vkCreateInstance"); err != nil {
		l.close()
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	globalLoader = l
	globalInst = inst
	loaderRefs = 1
	runtime.KeepAlive(appName)
	runtime.KeepAlive(engName)
	return inst, nil
}

// resolve looks up an instance-level (inst != 0) or global entry point.
func resolve(inst, gipaPtr uintptr, name string) uintptr {
	n := cstr(name)
	r := call(gipaPtr, inst, uintptr(unsafe.Pointer(&n[0])))
	runtime.KeepAlive(n)
	return r
}

func newDeviceOnInstance(inst uintptr) (*Device, error) {
	gipaPtr := instanceProcAddr[gipaFn]
	enumPhys := resolve(inst, gipaPtr, "vkEnumeratePhysicalDevices")
	getProps := resolve(inst, gipaPtr, "vkGetPhysicalDeviceProperties")
	getMemProps := resolve(inst, gipaPtr, "vkGetPhysicalDeviceMemoryProperties")
	getQueueProps := resolve(inst, gipaPtr, "vkGetPhysicalDeviceQueueFamilyProperties")
	if enumPhys == 0 || getProps == 0 || getMemProps == 0 || getQueueProps == 0 {
		return nil, fmt.Errorf("%w: missing device entry points", ErrUnavailable)
	}
	var count uint32
	if err := check(call(enumPhys, inst, uintptr(unsafe.Pointer(&count)), 0), "vkEnumeratePhysicalDevices(count)"); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: no physical devices", ErrUnavailable)
	}
	phys := make([]uintptr, count)
	if err := check(call(enumPhys, inst, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&phys[0]))), "vkEnumeratePhysicalDevices"); err != nil {
		return nil, err
	}

	// Pick the first device with a compute queue; prefer a discrete GPU.
	best, bestScore := uintptr(0), -1
	var bestFamily uint32
	for _, p := range phys {
		var qfCount uint32
		call(getQueueProps, p, uintptr(unsafe.Pointer(&qfCount)), 0)
		if qfCount == 0 {
			continue
		}
		qf := make([]queueFamilyProperties, qfCount)
		call(getQueueProps, p, uintptr(unsafe.Pointer(&qfCount)), uintptr(unsafe.Pointer(&qf[0])))
		fam, found := uint32(0), false
		for i := range qf {
			if qf[i].queueFlags&queueCompute != 0 {
				fam, found = uint32(i), true
				break
			}
		}
		if !found {
			continue
		}
		var props physicalDeviceProperties
		call(getProps, p, uintptr(unsafe.Pointer(&props)))
		score := 0
		switch props.deviceType {
		case 2: // discrete
			score = 100
		case 1: // integrated
			score = 60
		case 3: // virtual
			score = 40
		case physicalDeviceTypeCPU:
			score = 10
		}
		if score > bestScore {
			best, bestScore, bestFamily = p, score, fam
		}
	}
	if best == 0 {
		return nil, fmt.Errorf("%w: no compute queue", ErrUnavailable)
	}

	var props physicalDeviceProperties
	call(getProps, best, uintptr(unsafe.Pointer(&props)))
	var mp physicalDeviceMemoryProperties
	call(getMemProps, best, uintptr(unsafe.Pointer(&mp)))

	// Host-visible + coherent memory for CPU<->GPU shared buffers.
	memIdx, found := uint32(0), false
	// Prefer DEVICE_LOCAL host-visible coherent memory. On an integrated GPU
	// that is the shared system-memory heap the GPU caches normally; falling
	// back to plain host-visible coherent can land in the small uncached BAR
	// heap, which measured ~10x slower for streaming 400 MB of weights.
	bestMem, bestScore := uint32(0), -1
	for i := uint32(0); i < mp.memoryTypeCount; i++ {
		f := mp.memoryTypes[i].propertyFlags
		if f&(memoryPropertyHostVisible|memoryPropertyHostCoherent) != (memoryPropertyHostVisible|memoryPropertyHostCoherent) {
			continue
		}
		score := 0
		if f&memoryPropertyDeviceLocal != 0 {
			score = 10
		}
		if score > bestScore {
			bestMem, bestScore = i, score
			found = true
		}
	}
	memIdx = bestMem
	if !found {
		return nil, fmt.Errorf("%w: no host-visible coherent memory", ErrUnavailable)
	}
	heap := mp.memoryTypes[memIdx].heapIndex
	var memSize uint64
	if heap < mp.memoryHeapCount {
		memSize = mp.memoryHeaps[heap].size
	}

	prio := float32(1.0)
	qci := deviceQueueCreateInfo{
		sType:            structureTypeDeviceQueueCreateInfo,
		queueFamilyIndex: bestFamily,
		queueCount:       1,
		pQueuePriorities: uintptr(unsafe.Pointer(&prio)),
	}
	dci := deviceCreateInfo{
		sType:                structureTypeDeviceCreateInfo,
		queueCreateInfoCount: 1,
		pQueueCreateInfos:    uintptr(unsafe.Pointer(&qci)),
	}
	createDevice := resolve(inst, gipaPtr, "vkCreateDevice")
	var dev uintptr
	if err := check(call(createDevice, best, uintptr(unsafe.Pointer(&dci)), 0, uintptr(unsafe.Pointer(&dev))), "vkCreateDevice"); err != nil {
		return nil, err
	}
	getQueue := resolve(inst, gipaPtr, "vkGetDeviceQueue")
	var queue uintptr
	call(getQueue, dev, uintptr(bestFamily), 0, uintptr(unsafe.Pointer(&queue)))

	d := &Device{
		inst: inst, phys: best, dev: dev, queue: queue, family: bestFamily,
		memIdx: memIdx, memSize: memSize,
		Info: DeviceInfo{
			Name:       cString(props.deviceName[:]),
			Type:       int(props.deviceType),
			APIVersion: props.apiVersion,
			IsCPU:      props.deviceType == physicalDeviceTypeCPU,
		},
	}
	if err := d.resolveDeviceFns(inst, gipaPtr); err != nil {
		call(resolve(inst, gipaPtr, "vkDestroyDevice"), dev, 0)
		return nil, err
	}
	if err := d.initCommands(); err != nil {
		d.destroy()
		return nil, err
	}
	return d, nil
}

// cString reads a NUL-terminated fixed-size char array.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func (d *Device) resolveDeviceFns(inst, gipaPtr uintptr) error {
	// Device-level commands must be resolved through vkGetDeviceProcAddr: the
	// instance-level lookup is only guaranteed for instance commands, and
	// drivers do return NULL for device commands through it (AMD's loader
	// returns NULL for vkCreateComputePipeline that way).
	gdpa := resolve(inst, gipaPtr, "vkGetDeviceProcAddr")
	if gdpa == 0 {
		return fmt.Errorf("%w: vkGetDeviceProcAddr missing", ErrUnavailable)
	}
	r := func(name string) uintptr {
		n := cstr(name)
		p := call(gdpa, d.dev, uintptr(unsafe.Pointer(&n[0])))
		runtime.KeepAlive(n)
		if p == 0 {
			// Fall back to the instance-level loader (harmless when it works).
			return resolve(inst, gipaPtr, name)
		}
		return p
	}
	d.destroyInstance = r("vkDestroyInstance")
	f := &d.fn
	f.createBuffer = r("vkCreateBuffer")
	f.getBufferMemoryRequirements = r("vkGetBufferMemoryRequirements")
	f.allocateMemory = r("vkAllocateMemory")
	f.bindBufferMemory = r("vkBindBufferMemory")
	f.mapMemory = r("vkMapMemory")
	f.unmapMemory = r("vkUnmapMemory")
	f.destroyBuffer = r("vkDestroyBuffer")
	f.freeMemory = r("vkFreeMemory")
	f.createShaderModule = r("vkCreateShaderModule")
	f.destroyShaderModule = r("vkDestroyShaderModule")
	f.createDescriptorSetLayout = r("vkCreateDescriptorSetLayout")
	f.destroyDescriptorSetLayout = r("vkDestroyDescriptorSetLayout")
	f.createPipelineLayout = r("vkCreatePipelineLayout")
	f.destroyPipelineLayout = r("vkDestroyPipelineLayout")
	f.createDescriptorPool = r("vkCreateDescriptorPool")
	f.destroyDescriptorPool = r("vkDestroyDescriptorPool")
	f.allocateDescriptorSets = r("vkAllocateDescriptorSets")
	f.updateDescriptorSets = r("vkUpdateDescriptorSets")
	f.createComputePipeline = r("vkCreateComputePipelines")
	if f.createComputePipeline == 0 {
		// Some drivers only expose the singular entry point.
		f.createComputePipeline = r("vkCreateComputePipeline")
	}
	f.destroyPipeline = r("vkDestroyPipeline")
	f.createCommandPool = r("vkCreateCommandPool")
	f.destroyCommandPool = r("vkDestroyCommandPool")
	f.allocateCommandBuffers = r("vkAllocateCommandBuffers")
	f.freeCommandBuffers = r("vkFreeCommandBuffers")
	f.beginCommandBuffer = r("vkBeginCommandBuffer")
	f.endCommandBuffer = r("vkEndCommandBuffer")
	f.resetCommandBuffer = r("vkResetCommandBuffer")
	f.cmdBindPipeline = r("vkCmdBindPipeline")
	f.cmdBindDescriptorSets = r("vkCmdBindDescriptorSets")
	f.cmdPushConstants = r("vkCmdPushConstants")
	f.cmdDispatch = r("vkCmdDispatch")
	f.cmdPipelineBarrier = r("vkCmdPipelineBarrier")
	f.queueSubmit = r("vkQueueSubmit")
	f.queueWaitIdle = r("vkQueueWaitIdle")
	f.createFence = r("vkCreateFence")
	f.destroyFence = r("vkDestroyFence")
	f.waitForFences = r("vkWaitForFences")
	f.resetFences = r("vkResetFences")
	f.deviceWaitIdle = r("vkDeviceWaitIdle")
	f.destroyDevice = r("vkDestroyDevice")
	if f.createBuffer == 0 || f.allocateMemory == 0 || f.createComputePipeline == 0 ||
		f.queueSubmit == 0 || f.cmdDispatch == 0 || f.createShaderModule == 0 {
		return fmt.Errorf("%w: missing device functions", ErrUnavailable)
	}
	return nil
}

func (d *Device) initCommands() error {
	ci := commandPoolCreateInfo{
		sType:            structureTypeCommandPoolCreateInfo,
		flags:            commandPoolCreateResetCommandBuffer,
		queueFamilyIndex: d.family,
	}
	if err := check(call(d.fn.createCommandPool, d.dev, uintptr(unsafe.Pointer(&ci)), 0, uintptr(unsafe.Pointer(&d.cmdPool))), "vkCreateCommandPool"); err != nil {
		return err
	}
	ai := commandBufferAllocateInfo{
		sType:              structureTypeCommandBufferAllocateInfo,
		commandPool:        d.cmdPool,
		level:              0, // primary
		commandBufferCount: 1,
	}
	if err := check(call(d.fn.allocateCommandBuffers, d.dev, uintptr(unsafe.Pointer(&ai)), uintptr(unsafe.Pointer(&d.cmdBuf))), "vkAllocateCommandBuffers"); err != nil {
		return err
	}
	fi := fenceCreateInfo{sType: structureTypeFenceCreateInfo}
	if err := check(call(d.fn.createFence, d.dev, uintptr(unsafe.Pointer(&fi)), 0, uintptr(unsafe.Pointer(&d.fence))), "vkCreateFence"); err != nil {
		return err
	}
	return nil
}

func (d *Device) destroy() {
	if d.dev != 0 {
		call(d.fn.deviceWaitIdle, d.dev)
		if d.cmdPool != 0 {
			call(d.fn.destroyCommandPool, d.dev, d.cmdPool, 0)
		}
		if d.fence != 0 {
			call(d.fn.destroyFence, d.dev, d.fence, 0)
		}
		call(d.fn.destroyDevice, d.dev, 0)
		d.dev = 0
	}
}

// Close destroys the device and releases the shared instance when it is the
// last one.
func (d *Device) Close() {
	if d == nil {
		return
	}
	d.destroy()
	releaseSharedInstance()
}

// releaseSharedInstance drops one reference to the process-wide instance.
func releaseSharedInstance() {
	loaderMu.Lock()
	defer loaderMu.Unlock()
	if loaderRefs > 0 {
		loaderRefs--
	}
	if loaderRefs == 0 && globalInst != 0 {
		destroyInst := resolve(globalInst, instanceProcAddr[gipaFn], "vkDestroyInstance")
		call(destroyInst, globalInst, 0)
		globalInst = 0
		if globalLoader != nil {
			globalLoader.close()
			globalLoader = nil
		}
	}
}
