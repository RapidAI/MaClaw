//go:build windows

package main

import "syscall"

func windowsUILanguageIsChinese() bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultUILanguage")
	ret, _, _ := proc.Call()
	langID := uint16(ret)
	primary := langID & 0x03ff
	return primary == 0x04
}
