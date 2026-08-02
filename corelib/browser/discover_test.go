package browser

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWaitForPersistentCDPStopsWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if addr, ok := waitForPersistentCDPCtx(ctx, t.TempDir(), 10*time.Second); ok || addr != "" {
		t.Fatalf("wait result = %q,%v; want empty,false", addr, ok)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v", ctx.Err())
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("cancelled persistent CDP wait took %v", elapsed)
	}
}

func TestWindowsProfileDevToolsCandidatesIncludesProfiles(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "Default"), 0o755); err != nil {
		t.Fatalf("mkdir Default: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "Profile 2"), 0o755); err != nil {
		t.Fatalf("mkdir Profile 2: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "System Profile"), 0o755); err != nil {
		t.Fatalf("mkdir System Profile: %v", err)
	}

	candidates := windowsProfileDevToolsCandidates(base)
	joined := strings.Join(candidates, "\n")
	for _, want := range []string{
		filepath.Join(base, "DevToolsActivePort"),
		filepath.Join(base, "Default", "DevToolsActivePort"),
		filepath.Join(base, "Profile 2", "DevToolsActivePort"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("candidates missing %q: %v", want, candidates)
		}
	}
	if strings.Contains(joined, filepath.Join(base, "System Profile", "DevToolsActivePort")) {
		t.Fatalf("unexpected non-profile candidate: %v", candidates)
	}
}

func TestSummarizeStderrCompactsWhitespace(t *testing.T) {
	got := summarizeStderr("\n  first line\r\n second\tline  ")
	if got != "first line second line" {
		t.Fatalf("summarizeStderr = %q", got)
	}
}

func TestRemoteDebuggingPortFromCommandLine(t *testing.T) {
	cmd := `"C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=3717 --user-data-dir="C:\Users\ma139\.maclaw\browser-profile"`
	port, ok := remoteDebuggingPortFromCommandLine(cmd)
	if !ok || port != 3717 {
		t.Fatalf("remoteDebuggingPortFromCommandLine = %d,%v; want 3717,true", port, ok)
	}
	if _, ok := remoteDebuggingPortFromCommandLine(`chrome.exe --remote-debugging-port=bad`); ok {
		t.Fatal("expected invalid port to be rejected")
	}
	if _, ok := remoteDebuggingPortFromCommandLine(`chrome.exe --remote-debugging-port=70000`); ok {
		t.Fatal("expected out-of-range port to be rejected")
	}
	port, ok = remoteDebuggingPortFromCommandLine(`chrome.exe --remote-debugging-port="9222"`)
	if !ok || port != 9222 {
		t.Fatalf("quoted port = %d,%v; want 9222,true", port, ok)
	}
	port, ok = remoteDebuggingPortFromCommandLine(`chrome.exe --remote-debugging-port 9333`)
	if !ok || port != 9333 {
		t.Fatalf("space-separated port = %d,%v; want 9333,true", port, ok)
	}
}

func TestBrowserProcessesByDirPowerShellCanInspectWithoutKill(t *testing.T) {
	ps := browserProcessesByDirPowerShell(`C:\Users\ma139\.maclaw\browser-profile`, false)
	if strings.Contains(ps, "Stop-Process") {
		t.Fatalf("inspect script must not kill browser processes: %s", ps)
	}
	if !strings.Contains(ps, ".maclaw") || !strings.Contains(ps, "ProcessId") {
		t.Fatalf("inspect script missing profile/process lookup: %s", ps)
	}
}

func TestBrowserProcessesByDirPowerShellKillIsExplicit(t *testing.T) {
	ps := browserProcessesByDirPowerShell(`C:\Users\ma139\.maclaw\browser-profile`, true)
	if !strings.Contains(ps, "Stop-Process") {
		t.Fatalf("kill script should include explicit process stop: %s", ps)
	}
}

func TestBrowserProfileKindLabelsPersistentAndIsolated(t *testing.T) {
	if got := browserProfileKind(persistentProfileDir()); got != "persistent managed profile" {
		t.Fatalf("persistent profile kind = %q", got)
	}
	if got := browserProfileKind(debugProfileDir()); got != "isolated debug profile" {
		t.Fatalf("isolated profile kind = %q", got)
	}
}

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return port
}

func TestProbeBrowserCDPAcceptsRealBrowserEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Browser":"Chrome/146.0.0.0","webSocketDebuggerUrl":"ws://localhost/devtools/browser/abc"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if !probeBrowserCDP(portOf(t, srv)) {
		t.Fatal("probeBrowserCDP should accept an endpoint with a Browser field")
	}
}

func TestProbeBrowserCDPRejectsNodeInspector(t *testing.T) {
	// Node.js inspector serves /json and /json/list but /json/version has no
	// "Browser" field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"node-1","type":"node","webSocketDebuggerUrl":"ws://localhost/abc"}]`))
	}))
	defer srv.Close()
	if probeBrowserCDP(portOf(t, srv)) {
		t.Fatal("probeBrowserCDP should reject endpoints without a Browser field")
	}
}

func TestProbeBrowserCDPRejectsClosedPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	port := portOf(t, srv)
	srv.Close()
	if probeBrowserCDP(port) {
		t.Fatal("probeBrowserCDP should reject a closed port")
	}
}
