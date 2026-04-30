package libopus

// opus_select_arch returns the CPU architecture index for SIMD dispatch.
// In the pure Go implementation, we always use the generic (arch=0) path.
func opus_select_arch() int { return 0 }
