package browser

import (
	"os"
	"strings"
	"testing"
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
	if previewLocationMatches("about:blank", want) || previewLocationMatches("http://127.0.0.1:9/web-preview/v1/abc/other.html", want) {
		t.Fatal("unloaded page was treated as ready")
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
