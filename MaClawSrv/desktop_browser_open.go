package main

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// app_open of the browser: the desktop already runs the agent browser, and a
// second Chromium would not share its profile or CDP. Models still ask for it
// (seen live twice, both rejected), so it is served instead of refused: with
// a URL argument it becomes navigate, otherwise the existing browser window
// is brought to the front.

func desktopIsBrowserProgram(program string) bool {
	base := strings.ToLower(strings.TrimSpace(program))
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome",
		"firefox", "firefox-esr", "maclaw-browser", "x-www-browser", "sensible-browser", "browser":
		return true
	}
	return false
}

func desktopBrowserOpenURL(raw any) string {
	var items []string
	switch typed := raw.(type) {
	case string:
		items = []string{typed}
	case []string:
		items = typed
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		low := strings.ToLower(item)
		if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			return item
		}
	}
	return ""
}

func desktopBrowserOpenAsNavigate(args map[string]any) (map[string]any, bool) {
	if strings.ToLower(strings.TrimSpace(desktopArg(args, "action"))) != "app_open" || !desktopIsBrowserProgram(desktopArg(args, "program")) {
		return args, false
	}
	url := strings.TrimSpace(desktopArg(args, "url"))
	if url == "" {
		url = desktopBrowserOpenURL(args["args"])
	}
	if url == "" {
		return args, false
	}
	return map[string]any{"action": "navigate", "url": url}, true
}

func desktopActivateBrowser(ctx context.Context, scope agentruntime.Scope, display string) (string, error) {
	windows, err := desktopWindowsNow(ctx, scope, display)
	if err != nil {
		return "", err
	}
	for i := len(windows) - 1; i >= 0; i-- {
		if desktopIsBrowserClass(windows[i].class) {
			return "The agent browser is already running; app_open did not start another one. " +
				desktopActivateWindow(ctx, scope, display, windows[i]) +
				" Use navigate to open a page in it.", nil
		}
	}
	return "The agent browser has no window yet; use navigate with a URL to open it.", nil
}
