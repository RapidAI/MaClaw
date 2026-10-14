package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDesktopBrowserOpenAsNavigate(t *testing.T) {
	got, ok := desktopBrowserOpenAsNavigate(map[string]any{"action": "app_open", "program": "chromium", "args": []any{"--new-window", "https://maclaw.top/docs"}})
	if !ok || got["action"] != "navigate" || got["url"] != "https://maclaw.top/docs" {
		t.Fatalf("%v %#v", ok, got)
	}
	if _, ok := desktopBrowserOpenAsNavigate(map[string]any{"action": "app_open", "program": "chromium"}); ok {
		t.Fatal("no url became navigate")
	}
	if _, ok := desktopBrowserOpenAsNavigate(map[string]any{"action": "app_open", "program": "libreoffice", "args": []any{"https://x"}}); ok {
		t.Fatal("non-browser rewritten")
	}
	if !desktopIsBrowserProgram("/usr/bin/google-chrome") || desktopIsBrowserProgram("bash") {
		t.Fatal("browser detection")
	}
}

func TestDesktopDeliverSizeHint(t *testing.T) {
	err := desktopDeliverSizeHint(map[string]any{"action": "deliver"}, errors.New("docker service rejected the desktop request (HTTP 400): invalid desktop request: file is too large"))
	if err == nil || !strings.Contains(err.Error(), "150000 bytes") {
		t.Fatal(err)
	}
	if desktopDeliverSizeHint(map[string]any{"action": "bash"}, nil) != nil {
		t.Fatal("nil error changed")
	}
}
