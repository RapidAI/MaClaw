package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	imageIcon      = 1
	lrDefaultSize  = 0x00000040
	lrLoadFromFile = 0x00000010
	idiApplication = 32512
)

//go:embed assets/icon.ico
var embeddedWindowIconICO []byte

var (
	windowIconSmall uintptr
	windowIconBig   uintptr
)

func currentModuleHandle() uintptr {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getModuleHandle := kernel32.NewProc("GetModuleHandleW")
	module, _, _ := getModuleHandle.Call(0)
	return module
}

func loadWindowIcon() uintptr {
	return loadWindowIconBig()
}

func loadWindowIconSmall() uintptr {
	if windowIconSmall == 0 {
		windowIconSmall = loadEmbeddedICO(16)
	}
	if windowIconSmall != 0 {
		return windowIconSmall
	}
	return loadWindowIconBig()
}

func loadWindowIconBig() uintptr {
	if windowIconBig != 0 {
		return windowIconBig
	}
	windowIconBig = loadEmbeddedICO(32)
	if windowIconBig != 0 {
		return windowIconBig
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	loadImage := user32.NewProc("LoadImageW")
	loadIcon := user32.NewProc("LoadIconW")
	module := currentModuleHandle()
	if module != 0 {
		icon, _, _ := loadImage.Call(module, 1, imageIcon, 0, 0, lrDefaultSize)
		if icon != 0 {
			windowIconBig = icon
			return windowIconBig
		}
	}
	icon, _, _ := loadIcon.Call(0, idiApplication)
	windowIconBig = icon
	return windowIconBig
}

func loadEmbeddedICO(size int32) uintptr {
	path, err := embeddedICOPath()
	if err != nil {
		return 0
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	loadImage := user32.NewProc("LoadImageW")
	icon, _, _ := loadImage.Call(0, uintptr(unsafe.Pointer(ptr)), imageIcon, uintptr(size), uintptr(size), lrLoadFromFile)
	return icon
}

func embeddedICOPath() (string, error) {
	path := filepath.Join(os.TempDir(), "ins-maclaw-icon.ico")
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(embeddedWindowIconICO)) {
		return path, nil
	}
	if err := os.WriteFile(path, embeddedWindowIconICO, 0644); err != nil {
		return "", err
	}
	return path, nil
}
