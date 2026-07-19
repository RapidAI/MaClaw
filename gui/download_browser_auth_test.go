package main

import (
	"testing"
)

func TestBuildFetchHeaders_FromHeadersObject(t *testing.T) {
	headers, errMsg := buildFetchHeaders(map[string]interface{}{
		"headers": map[string]interface{}{
			"Referer": "https://example.com/list",
			"X-Token": "abc",
		},
	}, "https://example.com/f.pdf")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if headers["Referer"] != "https://example.com/list" || headers["X-Token"] != "abc" {
		t.Fatalf("headers not parsed: %v", headers)
	}
}

func TestBuildFetchHeaders_CookieShortcutOverridesHeadersObject(t *testing.T) {
	headers, errMsg := buildFetchHeaders(map[string]interface{}{
		"headers": map[string]interface{}{"Cookie": "a=1"},
		"cookie":  "b=2",
	}, "https://example.com/f.pdf")
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if headers["Cookie"] != "b=2" {
		t.Fatalf("cookie shortcut should win, got %q", headers["Cookie"])
	}
}

func TestBuildFetchHeaders_NoneReturnsNil(t *testing.T) {
	headers, errMsg := buildFetchHeaders(map[string]interface{}{}, "https://example.com/f.pdf")
	if errMsg != "" || headers != nil {
		t.Fatalf("expected nil headers and no error, got %v / %q", headers, errMsg)
	}
}

func TestBuildFetchHeaders_UseBrowserCookiesWithoutSession(t *testing.T) {
	// No live browser session exists in tests, so the explicit request must
	// fail with agent-actionable guidance instead of silently continuing.
	_, errMsg := buildFetchHeaders(map[string]interface{}{
		"use_browser_cookies": true,
	}, "https://example.com/f.pdf")
	if errMsg == "" {
		t.Fatal("expected an error message when no browser session exists")
	}
}
