//go:build amd64

package tensor

import "golang.org/x/sys/cpu"

// CPU feature flags — checked once at package init, used for SIMD dispatch.
var (
	hasAVX2andFMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA // Haswell+
	hasAVX        = cpu.X86.HasAVX                     // Sandy Bridge+
)
