package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const (
	peMachineI386  = 0x014c
	peMachineAMD64 = 0x8664
	peMachineARM64 = 0xaa64
	peLFANEWMax    = 1 << 20
)

// windowsPython3CommandTokenRe matches python3 as a command token, including
// after && / || / quotes / newlines, but not inside a path like C:\python3\bin.
var windowsPython3CommandTokenRe = regexp.MustCompile(`(?im)(^|&&|\|\||[\s;"'])python3(?:\.exe)?(\s|$)`)

// windowsCodingPythonUsable reports whether path is a real interpreter this
// process can CreateProcess. LookPath success is not enough: Store stubs and
// pythoncore builds for another PE machine fail with "different kind of
// processor architecture".
func windowsCodingPythonUsable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if runtime.GOOS != "windows" {
		return true
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, `\microsoft\windowsapps\`) || strings.Contains(lower, "/microsoft/windowsapps/") {
		return false
	}
	return windowsExecutableMatchesProcessArch(path)
}

func windowsExecutableMatchesProcessArch(path string) bool {
	want := windowsProcessPEMachine()
	if want == 0 {
		return true
	}
	got, ok := windowsPEMachine(path)
	if !ok {
		return false
	}
	return got == want
}

func windowsProcessPEMachine() uint16 {
	switch runtime.GOARCH {
	case "amd64":
		return peMachineAMD64
	case "arm64":
		return peMachineARM64
	case "386":
		return peMachineI386
	default:
		return 0
	}
}

func windowsPEMachine(path string) (uint16, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var dos [64]byte
	if _, err := f.Read(dos[:]); err != nil {
		return 0, false
	}
	if dos[0] != 'M' || dos[1] != 'Z' {
		return 0, false
	}
	lfanew := binary.LittleEndian.Uint32(dos[60:64])
	if lfanew < 64 || lfanew > peLFANEWMax {
		return 0, false
	}
	if _, err := f.Seek(int64(lfanew), 0); err != nil {
		return 0, false
	}
	var pe [6]byte
	if _, err := f.Read(pe[:]); err != nil {
		return 0, false
	}
	if pe[0] != 'P' || pe[1] != 'E' || pe[2] != 0 || pe[3] != 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(pe[4:6]), true
}

func firstUsableWindowsPython() string {
	return cachedUsableWindowsPythonCommand()
}

var cachedUsableWindowsPythonCommand = sync.OnceValue(func() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, name := range []string{"python", "py"} {
		path, err := exec.LookPath(name)
		if err != nil || !windowsCodingPythonUsable(path) {
			continue
		}
		return name
	}
	return ""
})

func replaceWindowsPython3Command(command, replacement string) string {
	replacement = strings.TrimSpace(replacement)
	if command == "" || replacement == "" || strings.EqualFold(replacement, "python3") {
		return command
	}
	return windowsPython3CommandTokenRe.ReplaceAllString(command, "${1}"+replacement+"${2}")
}
