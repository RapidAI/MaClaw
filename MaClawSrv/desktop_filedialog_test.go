package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBrowserFileChooserIsOutsideBrowserPage(t *testing.T) {
	run := func(name string) func(...string) (string, error) {
		return func(a ...string) (string, error) {
			if strings.Join(a, " ") == strings.Join(desktopWindowClassRead, " ") {
				return "Chromium\n", nil
			}
			return name + "\n", nil
		}
	}
	if !desktopFocusedWindowOutsideBrowser(run("打开文件")) || !desktopFocusedWindowOutsideBrowser(run("Open File")) {
		t.Fatal("file chooser must not count as a browser page")
	}
	if desktopFocusedWindowOutsideBrowser(run("登录 - Chromium")) || desktopFocusedWindowOutsideBrowser(run("小红书创作服务平台 - Chromium")) {
		t.Fatal("browser page window must stay guarded")
	}
}

func TestPasswordRefusalYieldsToAnOpenFileDialog(t *testing.T) {
	var calls []string
	active := "小红书创作服务平台 - Chromium"
	run := func(argv ...string) (string, error) {
		calls = append(calls, strings.Join(argv, " "))
		switch {
		case argv[0] == "search" && strings.Contains(strings.Join(argv, " "), "Open Files"):
			return "23068679\n", nil
		case argv[0] == "windowactivate":
			active = "Open Files"
			return "", nil
		case strings.Join(argv, " ") == strings.Join(desktopWindowClassRead, " "):
			return "Chromium", nil
		case argv[0] == "getactivewindow":
			return active, nil
		}
		return "", nil
	}
	refuse := func() error { return fmt.Errorf("password") }
	_, err := runGuardedDesktopAppSteps([][]string{{"type", "--", "/home/desktop/Desktop/a.png"}}, refuse, func() bool { return false }, run, nil)
	if err != nil {
		t.Fatalf("typing into the file dialog refused: %v calls=%v", err, calls)
	}
	typed := false
	for _, c := range calls {
		typed = typed || strings.HasPrefix(c, "type")
	}
	if !typed {
		t.Fatalf("path not typed: %v", calls)
	}
}
