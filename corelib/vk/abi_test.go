package vk

import (
	"testing"
	"unsafe"
)

// TestStructSizes pins every hand-laid-out struct against the Vulkan C ABI
// (64-bit). A struct that is too small makes the driver write past the Go
// allocation; one that is too large silently misaligns the next field.
func TestStructSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"VkApplicationInfo", unsafe.Sizeof(applicationInfo{}), 48},
		{"VkInstanceCreateInfo", unsafe.Sizeof(instanceCreateInfo{}), 64},
		{"VkDeviceQueueCreateInfo", unsafe.Sizeof(deviceQueueCreateInfo{}), 40},
		{"VkDeviceCreateInfo", unsafe.Sizeof(deviceCreateInfo{}), 72},
		{"VkBufferCreateInfo", unsafe.Sizeof(bufferCreateInfo{}), 56},
		{"VkMemoryRequirements", unsafe.Sizeof(memoryRequirements{}), 24},
		{"VkMemoryAllocateInfo", unsafe.Sizeof(memoryAllocateInfo{}), 32},
		{"VkShaderModuleCreateInfo", unsafe.Sizeof(shaderModuleCreateInfo{}), 40},
		{"VkDescriptorSetLayoutBinding", unsafe.Sizeof(descriptorSetLayoutBinding{}), 24},
		{"VkDescriptorSetLayoutCreateInfo", unsafe.Sizeof(descriptorSetLayoutCreateInfo{}), 32},
		{"VkPushConstantRange", unsafe.Sizeof(pushConstantRange{}), 12},
		{"VkPipelineLayoutCreateInfo", unsafe.Sizeof(pipelineLayoutCreateInfo{}), 48},
		{"VkPipelineShaderStageCreateInfo", unsafe.Sizeof(pipelineShaderStageCreateInfo{}), 48},
		{"VkComputePipelineCreateInfo", unsafe.Sizeof(computePipelineCreateInfo{}), 96},
		{"VkDescriptorPoolSize", unsafe.Sizeof(descriptorPoolSize{}), 8},
		{"VkDescriptorPoolCreateInfo", unsafe.Sizeof(descriptorPoolCreateInfo{}), 40},
		{"VkDescriptorSetAllocateInfo", unsafe.Sizeof(descriptorSetAllocateInfo{}), 40},
		{"VkDescriptorBufferInfo", unsafe.Sizeof(descriptorBufferInfo{}), 24},
		{"VkWriteDescriptorSet", unsafe.Sizeof(writeDescriptorSet{}), 64},
		{"VkCommandPoolCreateInfo", unsafe.Sizeof(commandPoolCreateInfo{}), 24},
		{"VkCommandBufferAllocateInfo", unsafe.Sizeof(commandBufferAllocateInfo{}), 32},
		{"VkCommandBufferBeginInfo", unsafe.Sizeof(commandBufferBeginInfo{}), 32},
		{"VkMemoryBarrier", unsafe.Sizeof(memoryBarrier{}), 24},
		{"VkSubmitInfo", unsafe.Sizeof(submitInfo{}), 72},
		{"VkFenceCreateInfo", unsafe.Sizeof(fenceCreateInfo{}), 24},
		{"VkPhysicalDeviceProperties", unsafe.Sizeof(physicalDeviceProperties{}), 824},
		{"VkPhysicalDeviceMemoryProperties", unsafe.Sizeof(physicalDeviceMemoryProperties{}), 520},
		{"VkQueueFamilyProperties", unsafe.Sizeof(queueFamilyProperties{}), 24},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: size %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestStructFieldOffsets checks the offsets the driver reads through.
func TestStructFieldOffsets(t *testing.T) {
	var sm shaderModuleCreateInfo
	if off := unsafe.Offsetof(sm.codeSize); off != 24 {
		t.Errorf("ShaderModuleCreateInfo.codeSize offset %d, want 24", off)
	}
	if off := unsafe.Offsetof(sm.pCode); off != 32 {
		t.Errorf("ShaderModuleCreateInfo.pCode offset %d, want 32", off)
	}
	var b bufferCreateInfo
	if off := unsafe.Offsetof(b.size); off != 24 {
		t.Errorf("BufferCreateInfo.size offset %d, want 24", off)
	}
	if off := unsafe.Offsetof(b.usage); off != 32 {
		t.Errorf("BufferCreateInfo.usage offset %d, want 32", off)
	}
}
