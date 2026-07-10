//go:build amd64

package tensor

import "golang.org/x/sys/cpu"

// CPU feature flags — checked once at package init, used for SIMD dispatch.
var (
	hasAVX2andFMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA // Haswell+
	hasAVX        = cpu.X86.HasAVX                     // Sandy Bridge+
	// AVX-512 for SenseVoice hot kernels (Zen4 / Ice Lake+).
	// F: foundation; DQ/VL: ZMM ops; BW: VPMOVSXBD byte→dword expand for Q8.
	hasAVX512 = cpu.X86.HasAVX512F && cpu.X86.HasAVX512DQ &&
		cpu.X86.HasAVX512VL && cpu.X86.HasAVX512BW
	// VNNI: VPDPBUSD int8 GEMM (FFN-down A prequant × Q8 B).
	hasAVX512VNNI = hasAVX512 && cpu.X86.HasAVX512VNNI
)
