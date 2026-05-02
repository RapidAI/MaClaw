//go:build !amd64 && !arm64

package kokoro

func cpuSupportsKokoroSIMD() bool { return false }

func cpuSupportsKokoroFMA() bool { return false }
