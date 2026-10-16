package main

import (
	"strings"
	"testing"
)

func TestGuardedStepsSkipBrowserReadOutsideBrowser(t *testing.T) {
	called := false
	run := func(argv ...string) (string, error) {
		if strings.Join(argv, " ") == strings.Join(desktopWindowClassRead, " ") {
			return "Mousepad\n", nil
		}
		return "", nil
	}
	_, err := runGuardedDesktopAppSteps([][]string{{"key", "ctrl+v"}}, func() error { called = true; return nil }, func() bool { return true }, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	if desktopArgvInsertsText([]string{"key", "ctrl+v"}) && called {
		t.Fatal("browser password read ran for a non-browser window")
	}
	called = false
	browserRun := func(argv ...string) (string, error) {
		if strings.Join(argv, " ") == strings.Join(desktopWindowClassRead, " ") {
			return "Chromium\n", nil
		}
		return "", nil
	}
	_, _ = runGuardedDesktopAppSteps([][]string{{"type", "--", "x"}}, func() error { called = true; return nil }, func() bool { return false }, browserRun, nil)
	if !called {
		t.Fatal("browser window must keep the password read")
	}
}
