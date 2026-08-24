//go:build !windows

package embedding

func detectNPU() (present bool, device, reason string) {
	return false, "", "NPU detect is Windows-only"
}
