//go:build darwin

package embedding

import "golang.org/x/sys/unix"

// physicalCores reports hw.physicalcpu (8 on an 8P+4E M-series die), the
// width the memory-bound embedding kernels can use. Returns 0 on failure so
// callers fall back to runtime.NumCPU().
func physicalCores() int {
	n, err := unix.SysctlUint32("hw.physicalcpu")
	if err != nil || n == 0 {
		return 0
	}
	return int(n)
}
