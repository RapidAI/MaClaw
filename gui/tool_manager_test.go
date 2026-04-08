package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func withMockHTTPGet(t *testing.T, fn func(url string) (*http.Response, error)) {
	t.Helper()
	old := httpGet
	httpGet = fn
	t.Cleanup(func() {
		httpGet = old
	})
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestToolManagerNeedsUpdateForClaude(t *testing.T) {
	app := &App{}
	tm := NewToolManager(app)
	withMockHTTPGet(t, func(url string) (*http.Response, error) {
		if url != tm.claudeVersionEndpoint("latest") {
			t.Fatalf("unexpected url: %s", url)
		}
		return textResponse(http.StatusOK, "2.1.82\n"), nil
	})

	needsUpdate, latest, err := tm.NeedsUpdate("claude", "2.1.81")
	if err != nil {
		t.Fatalf("NeedsUpdate returned error: %v", err)
	}
	if !needsUpdate {
		t.Fatal("expected claude to need update")
	}
	if latest != "2.1.82" {
		t.Fatalf("latest = %q, want 2.1.82", latest)
	}
}

func TestToolManagerNeedsUpdateReturnsFalseWhenClaudeIsCurrent(t *testing.T) {
	app := &App{}
	tm := NewToolManager(app)
	withMockHTTPGet(t, func(url string) (*http.Response, error) {
		return textResponse(http.StatusOK, "2.1.82"), nil
	})

	needsUpdate, latest, err := tm.NeedsUpdate("claude", "2.1.82")
	if err != nil {
		t.Fatalf("NeedsUpdate returned error: %v", err)
	}
	if needsUpdate {
		t.Fatal("expected claude to already be current")
	}
	if latest != "2.1.82" {
		t.Fatalf("latest = %q, want 2.1.82", latest)
	}
}

func TestParseCursorVersionFromInstallScript(t *testing.T) {
	script := `#!/bin/sh
DOWNLOAD_URL="https://downloads.cursor.com/lab/2026.03.11-6dfa30c/${OS}/${ARCH}/agent-cli-package.tar.gz"`
	got := parseCursorVersionFromInstallScript(script)
	if got != "2026.03.11-6dfa30c" {
		t.Fatalf("parseCursorVersionFromInstallScript() = %q, want %q", got, "2026.03.11-6dfa30c")
	}
}
