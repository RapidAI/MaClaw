//go:build !linux && !windows

package embedding

// availableMemory is unknown here (macOS has no cheap cgo-free "available"
// figure: free + inactive + purgeable + compressible pages). Unknown means the
// low-memory guard does not block; the load itself still degrades on error.
func availableMemory() (uint64, bool) { return 0, false }
