//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func macOSBundleGUI(macOSDir string) string {
	if filepath.Base(macOSDir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(macOSDir)
	plist := filepath.Join(contents, "Info.plist")
	body, err := os.ReadFile(plist)
	if err != nil {
		return ""
	}
	name := infoPlistString(string(body), "CFBundleExecutable")
	if name == "" || name == "maclaw-companion-host" {
		return ""
	}
	gui := filepath.Join(macOSDir, name)
	if fileExists(gui) {
		return gui
	}
	return ""
}

func infoPlistString(body, key string) string {
	needle := "<key>" + key + "</key>"
	at := strings.Index(body, needle)
	if at < 0 {
		return ""
	}
	rest := body[at+len(needle):]
	open := strings.Index(rest, "<string>")
	close := strings.Index(rest, "</string>")
	if open < 0 || close < open {
		return ""
	}
	return strings.TrimSpace(rest[open+len("<string>") : close])
}
