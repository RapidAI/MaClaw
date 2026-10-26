//go:build !windows && !linux && !darwin

package embedding

// physicalCores is unknown on this platform; callers fall back to
// runtime.NumCPU() (which is the right answer on most non-SMT targets).
func physicalCores() int { return 0 }
