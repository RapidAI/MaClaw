package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestChromeModeDoesNotTouchStdio(t *testing.T) {
	gui := filepath.Join(t.TempDir(), "MaClaw.exe")
	prevPath := companionGUIPathHook
	prevLaunch := companionHostLaunchHook
	companionGUIPathHook = func() (string, error) { return gui, nil }
	var got []string
	companionHostLaunchHook = func(path string, argv []string) (int, error) {
		if path != gui {
			t.Fatalf("gui=%s", path)
		}
		got = append([]string(nil), argv...)
		return 0, nil
	}
	t.Cleanup(func() {
		companionGUIPathHook = prevPath
		companionHostLaunchHook = prevLaunch
	})
	stdout := os.Stdout
	stderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	os.Stderr = w
	code := runCompanionHost([]string{
		"maclaw-companion-host",
		"--companion-host-listen",
		"chrome-extension://iidkjocaelnacgnadidhbedomegjndjk/",
		"--parent-window=0",
	})
	_ = w.Close()
	os.Stdout = stdout
	os.Stderr = stderr
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if len(got) != 2 || got[0] != gui || got[1] != companionPaperHostArg {
		t.Fatalf("argv=%#v", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("stdio=%q", buf.String())
	}
}

func TestListenAndBareArgsWriteNothing(t *testing.T) {
	prev := companionHostLaunchHook
	companionHostLaunchHook = func(string, []string) (int, error) {
		t.Fatal("launched the GUI")
		return 0, nil
	}
	t.Cleanup(func() { companionHostLaunchHook = prev })
	cases := [][]string{
		{"maclaw-companion-host", "--companion-host-listen"},
		{"maclaw-companion-host", "safari-app-group"},
		{"maclaw-companion-host", "--parent-window=0"},
	}
	for _, args := range cases {
		stdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		code := runCompanionHost(args)
		_ = w.Close()
		os.Stdout = stdout
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		if code == 0 || buf.Len() != 0 {
			t.Fatalf("%v code=%d out=%q", args, code, buf.String())
		}
	}
}

func TestSiblingGUIUsesInstallDirectoryName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MaClaw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(dir, "maclaw-companion-host.exe")
	gui := filepath.Join(dir, "MaClaw.exe")
	if err := os.WriteFile(host, []byte("host"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gui, []byte("gui"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := companionGUIPathHook
	companionGUIPathHook = nil
	t.Cleanup(func() { companionGUIPathHook = prev })
	// os.Executable is this test binary, so the sibling rule is checked directly.
	got := siblingGUI(host)
	if got != gui {
		t.Fatalf("sibling=%s", got)
	}
}
