package guiapp

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeWebPreviewPathStaysInRoot(t *testing.T) {
	root := t.TempDir()
	got, err := safeWebPreviewPath(root, "css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !pathWithinRoot(root, got) {
		t.Fatalf("resolved %s outside %s", got, root)
	}
	if !strings.HasSuffix(strings.ToLower(got), filepath.Join("css", "app.css")) && !strings.HasSuffix(strings.ToLower(filepath.ToSlash(got)), "css/app.css") {
		t.Fatalf("resolved path = %s", got)
	}
	for _, rel := range []string{"../secret.txt", `..\secret.txt`, "css/%2e%2e/secret.txt"} {
		normalized, normErr := safeWebPreviewPath(root, rel)
		if normErr != nil {
			t.Fatalf("normalized %q: %v", rel, normErr)
		}
		if !pathWithinRoot(root, normalized) {
			t.Fatalf("%q escaped to %s", rel, normalized)
		}
	}
	for _, rel := range []string{`C:\Windows\notepad.exe`, `/C:/Windows/notepad.exe`} {
		if _, err := safeWebPreviewPath(root, rel); err == nil {
			t.Fatalf("accepted escape %q", rel)
		}
	}
}

func TestInjectWebPreviewBridge(t *testing.T) {
	body := injectWebPreviewBridge([]byte(`<html><head><title>t</title></head><body>Hi</body></html>`), "/web-preview/v1/tok/__mc_bridge.js")
	html := string(body)
	if !strings.Contains(html, `<script src="/web-preview/v1/tok/__mc_bridge.js"></script>`) {
		t.Fatalf("bridge not injected: %s", html)
	}
	if !strings.Contains(html, "<title>t</title>") {
		t.Fatalf("head content lost: %s", html)
	}
	again := injectWebPreviewBridge(body, "/web-preview/v1/tok/__mc_bridge.js")
	if strings.Count(string(again), "__mc_bridge.js") != 1 {
		t.Fatalf("bridge injected twice: %s", again)
	}
	plain := string(injectWebPreviewBridge([]byte(`<h1>Hi</h1>`), "/web-preview/v1/tok/__mc_bridge.js"))
	if !strings.HasPrefix(plain, `<script src="/web-preview/v1/tok/__mc_bridge.js"></script>`) {
		t.Fatalf("bridge not prepended: %s", plain)
	}
}

func TestOpenWebPreviewServesDirectoryAndBlocksEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "secret-"+filepath.Base(dir)+".txt")
	if err := os.WriteFile(outside, []byte("secret-value"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Mkdir(filepath.Join(dir, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := `<!DOCTYPE html><html><head><title>Preview</title></head><body><link rel="stylesheet" href="css/app.css"><h1>Hello page</h1></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "css", "app.css"), []byte("body{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	info, err := app.OpenWebPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.CloseWebPreview(info.ID) })
	if info.Origin == "" || !strings.HasPrefix(info.Origin, "http://127.0.0.1:") {
		t.Fatalf("origin = %q", info.Origin)
	}
	if strings.Contains(info.EntryURL, info.Token) && strings.Contains(info.EntryPath, info.Token) {
		t.Fatalf("entry path leaked the token: %+v", info)
	}
	if info.EntryPath != "index.html" {
		t.Fatalf("entry path = %q", info.EntryPath)
	}

	res, err := http.Get(info.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	body := readPreviewBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "Hello page") || !strings.Contains(body, webPreviewBridgeName) {
		t.Fatalf("page = %s", body)
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("content type %q", res.Header.Get("Content-Type"))
	}

	cssRes, err := http.Get(info.Origin + webPreviewRoutePrefix + info.Token + "/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := readPreviewBody(t, cssRes)
	if cssRes.StatusCode != http.StatusOK || !strings.Contains(css, "color:red") {
		t.Fatalf("css status %d body %s", cssRes.StatusCode, css)
	}

	bridgeRes, err := http.Get(info.Origin + webPreviewRoutePrefix + info.Token + "/" + webPreviewBridgeName)
	if err != nil {
		t.Fatal(err)
	}
	bridge := readPreviewBody(t, bridgeRes)
	if bridgeRes.StatusCode != http.StatusOK || !strings.Contains(bridge, "mc-web-preview") {
		t.Fatalf("bridge status %d body %s", bridgeRes.StatusCode, bridge)
	}

	for _, rel := range []string{
		webPreviewRoutePrefix + info.Token + "/../" + filepath.Base(outside),
		webPreviewRoutePrefix + info.Token + "/%2e%2e/secret.txt",
		webPreviewRoutePrefix + "00000000000000000000000000000000/index.html",
	} {
		escapeRes, err := http.Get(info.Origin + rel)
		if err != nil {
			t.Fatal(err)
		}
		escapeBody := readPreviewBody(t, escapeRes)
		if escapeRes.StatusCode != http.StatusNotFound || strings.Contains(escapeBody, "secret-value") {
			t.Fatalf("escape %s status %d body %s", rel, escapeRes.StatusCode, escapeBody)
		}
	}

	other, err := app.OpenWebPreview(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if other.Token != info.Token {
		t.Fatalf("same directory opened a second session: %s vs %s", other.Token, info.Token)
	}

	if _, err := app.OpenWebPreview(filepath.Join(dir, "css", "app.css")); err == nil || !strings.Contains(err.Error(), "HTML") {
		t.Fatalf("css entry err = %v", err)
	}
	if _, err := app.OpenWebPreview(filepath.Join(dir, "missing.html")); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing err = %v", err)
	}

	app.CloseWebPreview(info.ID)
	still, err := http.Get(info.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	stillBody := readPreviewBody(t, still)
	if still.StatusCode != http.StatusOK || !strings.Contains(stillBody, "Hello page") {
		t.Fatalf("session closed while a second open was active: %d %s", still.StatusCode, stillBody)
	}
	app.CloseWebPreview(other.ID)
	closed, err := http.Get(info.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	closedBody := readPreviewBody(t, closed)
	if closed.StatusCode != http.StatusNotFound || strings.Contains(closedBody, "Hello page") {
		t.Fatalf("closed session status %d body %s", closed.StatusCode, closedBody)
	}
}

func readPreviewBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}
