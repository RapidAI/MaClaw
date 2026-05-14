package main

import "syscall"

const (
	imageIcon      = 1
	lrDefaultSize  = 0x00000040
	idiApplication = 32512
)

var windowIcon uintptr

func loadWindowIcon() uintptr {
	if windowIcon != 0 {
		return windowIcon
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")
	getModuleHandle := kernel32.NewProc("GetModuleHandleW")
	loadImage := user32.NewProc("LoadImageW")
	loadIcon := user32.NewProc("LoadIconW")
	module, _, _ := getModuleHandle.Call(0)
	if module != 0 {
		icon, _, _ := loadImage.Call(module, 1, imageIcon, 0, 0, lrDefaultSize)
		if icon != 0 {
			windowIcon = icon
			return windowIcon
		}
	}
	icon, _, _ := loadIcon.Call(0, idiApplication)
	windowIcon = icon
	return windowIcon
}
