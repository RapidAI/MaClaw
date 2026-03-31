//go:build !windows

package gguf

import (
	"os"
	"syscall"
)

func mmapFile(f *os.File, size int) ([]byte, uintptr, error) {
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, 0, err
	}
	return data, 0, nil
}

func munmapFile(data []byte, _ uintptr) {
	if len(data) > 0 {
		syscall.Munmap(data)
	}
}
