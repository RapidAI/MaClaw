//go:build windows

package guiapp

import "syscall"

var (
	kernel32Language             = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultUILanguage = kernel32Language.NewProc("GetUserDefaultUILanguage")
)

func detectOSAppLanguage() string {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	if r == 0 {
		return string(appLanguageEnglish)
	}
	return languageFromLANGID(uint16(r))
}
