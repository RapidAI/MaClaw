package browser

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPreviewCaptureURLAllowedOnlyLoopback(t *testing.T) {
	if !previewCaptureURLAllowed("http://127.0.0.1:9/web-preview/v1/abc/index.html") {
		t.Fatal("loopback preview URL was rejected")
	}
	if previewCaptureURLAllowed("https://127.0.0.1/index.html") || previewCaptureURLAllowed("http://example.com/") {
		t.Fatal("non-loopback URL was accepted")
	}
	want := "http://127.0.0.1:9/web-preview/v1/abc/index.html"
	if !previewLocationMatches(want, want) || !previewLocationMatches("http://127.0.0.1:9/", "http://127.0.0.1:9") {
		t.Fatal("loaded preview URL did not match")
	}
	if previewLocationMatches("about:blank", want) || previewLocationMatches("http://127.0.0.1:9/web-preview/v1/abc/other.html", want) || previewLocationMatches("http://127.0.0.1:10/web-preview/v1/abc/index.html", want) {
		t.Fatal("unloaded page was treated as ready")
	}
}

func TestPreviewDocumentReadyWaitsForFontsThenCaptures(t *testing.T) {
	want := "http://127.0.0.1:9/index.html"
	if previewDocumentReady(want, want, "loading", 3*time.Second) {
		t.Fatal("capture ran while fonts were still loading")
	}
	if !previewDocumentReady(want, want, "loading", time.Second) {
		t.Fatal("a loaded page was dropped because fonts were slow")
	}
	if !previewDocumentReady(want, want, "loaded", 3*time.Second) {
		t.Fatal("loaded page was not ready")
	}
	if previewDocumentReady("about:blank", want, "loaded", 3*time.Second) {
		t.Fatal("blank page was ready")
	}
	if previewCaptureReady(want, want, "loaded", "old", "old", 3*time.Second) {
		t.Fatal("the page from before the reload was ready")
	}
	if !previewCaptureReady(want, want, "loaded", "", "old", 3*time.Second) {
		t.Fatal("a reloaded page was not ready")
	}
}

func TestPrivateHeadlessDoesNotAttachExistingBrowser(t *testing.T) {
	src, err := os.ReadFile("private_headless.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, banned := range []string{"GetSession", "DiscoverOrLaunch", "DiscoverCDPAddr", "installGlobalSession"} {
		if strings.Contains(text, banned) {
			t.Fatalf("private headless references %s", banned)
		}
	}
}
