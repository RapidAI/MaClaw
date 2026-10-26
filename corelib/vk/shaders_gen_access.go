package vk

// ShaderSPIRV returns the embedded SPIR-V module for a Qwen3 kernel name.
func ShaderSPIRV(name string) ([]byte, bool) {
	b, ok := shaderSPIRV[name]
	return b, ok
}

// ShaderNames lists the embedded kernels.
func ShaderNames() []string {
	out := make([]string, 0, len(shaderSPIRV))
	for k := range shaderSPIRV {
		out = append(out, k)
	}
	return out
}
