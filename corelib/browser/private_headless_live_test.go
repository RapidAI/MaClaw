package browser

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPrivateHeadlessCapturesLoadedLoopbackPage(t *testing.T) {
	if os.Getenv("MACLAW_PREVIEW_BROWSER") == "" {
		t.Skip("set MACLAW_PREVIEW_BROWSER=1 to launch a private browser")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0;background:#ff0000"></body></html>`))
	}))
	defer page.Close()
	if !strings.HasPrefix(page.URL, "http://127.0.0.1:") {
		t.Fatalf("test server = %s", page.URL)
	}

	proc, err := StartPrivateHeadless()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(proc.cmd.Args, " ")
	if strings.Contains(joined, "--remote-debugging-port=9222") {
		proc.Close()
		t.Fatal("private browser used the shared debug port")
	}
	if !strings.Contains(joined, "--user-data-dir=") || !strings.Contains(joined, "--headless=new") {
		proc.Close()
		t.Fatalf("args = %s", joined)
	}
	pngB64, err := proc.ScreenshotURL(page.URL)
	if err != nil {
		proc.Close()
		t.Fatal(err)
	}
	if !pngCenterRed(t, pngB64) {
		proc.Close()
		t.Fatal("screenshot was taken before the page painted")
	}
	proc.Close()
	if _, err := proc.ScreenshotURL(page.URL); err == nil {
		t.Fatal("closed browser still captured a page")
	}
}

func pngCenterRed(t *testing.T, pngB64 string) bool {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	r, g, b, _ := img.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	return r>>8 > 200 && g>>8 < 40 && b>>8 < 40
}
