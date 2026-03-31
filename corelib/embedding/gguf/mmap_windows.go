package gguf

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mmapFile(f *os.File, size int) ([]byte, uintptr, error) {
	h, err := windows.CreateFileMapping(
		windows.Handle(f.Fd()),
		nil,
		windows.PAGE_READONLY,
		0, 0,
		nil,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("CreateFileMapping: %w", err)
	}
	addr, err := windows.MapViewOfFile(
		h,
		windows.FILE_MAP_READ,
		0, 0,
		uintptr(size),
	)
	if err != nil {
		windows.CloseHandle(h)
		return nil, 0, fmt.Errorf("MapViewOfFile: %w", err)
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), size)
	return data, uintptr(h), nil
}

func munmapFile(data []byte, handle uintptr) {
	if len(data) > 0 {
		windows.UnmapViewOfFile(uintptr(unsafe.Pointer(&data[0])))
	}
	if handle != 0 {
		windows.CloseHandle(windows.Handle(handle))
	}
}
