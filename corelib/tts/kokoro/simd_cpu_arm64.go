//go:build arm64

package kokoro

import "golang.org/x/sys/cpu"

func cpuSupportsKokoroSIMD() bool {
	return cpu.ARM64.HasASIMD
}

func cpuSupportsKokoroFMA() bool { return false }
