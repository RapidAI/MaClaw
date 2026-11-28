//go:build arm64

package kokoro

func cpuSupportsKokoroSIMD() bool {
	// Advanced SIMD is mandatory in AArch64, and every Android arm64 device
	// has it. x/sys/cpu reports HasASIMD false when a c-shared library cannot
	// see the auxiliary vector, which used to disable every NEON kernel and
	// leave Kokoro on scalar dots.
	return true
}

func cpuSupportsKokoroFMA() bool { return false }
