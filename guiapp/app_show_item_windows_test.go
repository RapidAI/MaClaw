//go:build windows

package guiapp

import (
	"os/exec"
	"strings"
	"testing"
)

func TestShowItemInFolderSelectsAPathThatContainsSpaces(t *testing.T) {
	var got *exec.Cmd
	previous := startShownItemCommand
	startShownItemCommand = func(cmd *exec.Cmd) error {
		got = cmd
		return nil
	}
	t.Cleanup(func() { startShownItemCommand = previous })

	app := &App{}
	path := `C:\Users\A\My Documents\周报-伴读对话.pdf`
	if err := app.ShowItemInFolder(path); err != nil {
		t.Fatalf("ShowItemInFolder: %v", err)
	}
	if got == nil || got.Process != nil {
		t.Fatal("expected the reveal command to be captured without starting Explorer")
	}
	if got.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr")
	}
	line := got.SysProcAttr.CmdLine
	want := `/select,"` + path + `"`
	if !strings.Contains(line, want) {
		t.Fatalf("CmdLine = %q, want it to contain %q", line, want)
	}
	if strings.Contains(line, "/select, ") {
		t.Fatalf("CmdLine has a space after /select,: %q", line)
	}
	if got.SysProcAttr.CreationFlags&_CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = %#x, want _CREATE_NO_WINDOW", got.SysProcAttr.CreationFlags)
	}
}
