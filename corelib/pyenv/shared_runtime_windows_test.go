//go:build windows

package pyenv

import "testing"

func TestSharedRuntimeCommandsSuppressConsoleWindow(t *testing.T) {
	cmd := sharedRuntimeExecCommand("cmd.exe", "/c", "exit 0")
	if cmd.SysProcAttr == nil {
		t.Fatal("shared runtime command has no Windows process attributes")
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}
