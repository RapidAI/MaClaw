//go:build windows

package main

import (
	"log"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	imageIcon       = 1
	lrLoadFromFile  = 0x0010
	wmSetIcon       = 0x0080
	iconSmall       = 0
	iconBig         = 1
	windowIconTries = 10
)

var (
	windowIconUser32    = syscall.NewLazyDLL("user32.dll")
	procLoadImageWIcon  = windowIconUser32.NewProc("LoadImageW")
	procSendMessageIcon = windowIconUser32.NewProc("SendMessageW")
)

// setMainWindowIconFromTray gives the Wails HWND the same icon bytes used by
// systray.SetIcon. Wails' default window class may otherwise use its fallback
// icon when the application resource group cannot be resolved on Windows.
func setMainWindowIconFromTray() {
	go func() {
		for attempt := 0; attempt < windowIconTries; attempt++ {
			if hwnd := findMainWindowHWND(); hwnd != 0 {
				if err := setWindowIconFromBytes(hwnd, icon); err != nil {
					log.Printf("[window] failed to apply tray icon to main window: %v", err)
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		log.Printf("[window] main HWND unavailable; tray icon was not applied to window")
	}()
}

func setWindowIconFromBytes(hwnd uintptr, iconBytes []byte) error {
	if hwnd == 0 || len(iconBytes) == 0 {
		return nil
	}

	iconFile, err := os.CreateTemp("", "maclaw-window-icon-*.ico")
	if err != nil {
		return err
	}
	iconPath := iconFile.Name()
	defer os.Remove(iconPath)

	if _, err := iconFile.Write(iconBytes); err != nil {
		_ = iconFile.Close()
		return err
	}
	if err := iconFile.Close(); err != nil {
		return err
	}

	path, err := syscall.UTF16PtrFromString(iconPath)
	if err != nil {
		return err
	}
	flags := uintptr(lrLoadFromFile)
	bigIcon, _, bigErr := procLoadImageWIcon.Call(
		0, uintptr(unsafe.Pointer(path)), imageIcon, 32, 32, flags,
	)
	smallIcon, _, smallErr := procLoadImageWIcon.Call(
		0, uintptr(unsafe.Pointer(path)), imageIcon, 16, 16, flags,
	)
	if bigIcon == 0 && smallIcon == 0 {
		if bigErr != syscall.Errno(0) {
			return bigErr
		}
		return smallErr
	}

	if bigIcon != 0 {
		procSendMessageIcon.Call(hwnd, wmSetIcon, iconBig, bigIcon)
	}
	if smallIcon != 0 {
		procSendMessageIcon.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
	}
	return nil
}
