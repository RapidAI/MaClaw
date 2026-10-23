package main

import (
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
