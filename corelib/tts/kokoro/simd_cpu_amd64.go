//go:build amd64

package kokoro

import "golang.org/x/sys/cpu"

func cpuSupportsKokoroSIMD() bool {
	return cpu.X86.HasAVX2
}

func cpuSupportsKokoroFMA() bool {
	return cpu.X86.HasAVX2 && cpu.X86.HasFMA
}
