package oauth

import (
	"testing"
)

func TestExtractQRCodeFromHTML_ValidHTML(t *testing.T) {
	// HTML from design doc: class before src
	html := `<html><body><img class="ewm-img" src="/qrcode.php?v=http%3A%2F%2Fits.vpn.qianxin.com%3A80%2Fqrdata.php%3Fd%3Dabc123"></body></html>`
	got, err := extractQRCodeFromHTML(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http://its.vpn.qianxin.com:80/qrdata.php?d=abc123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractQRCodeFromHTML_SrcBeforeClass(t *testing.T) {
	html := `<img src="/qrcode.php?v=http%3A%2F%2Fexample.com" class="ewm-img">`
	got, err := extractQRCodeFromHTML(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "http://example.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractQRCodeFromHTML_NoEwmImg(t *testing.T) {
	html := `<html><body><img class="other" src="/qrcode.php?v=test"></body></html>`
	_, err := extractQRCodeFromHTML(html)
	if err == nil {
		t.Fatal("expected error for HTML without ewm-img, got nil")
	}
}

func TestExtractQRCodeFromHTML_NoVParam(t *testing.T) {
	html := `<img class="ewm-img" src="/qrcode.php?other=value">`
	_, err := extractQRCodeFromHTML(html)
	if err == nil {
		t.Fatal("expected error for missing v parameter, got nil")
	}
}

func TestExtractQRCodeFromHTML_EmptyHTML(t *testing.T) {
	_, err := extractQRCodeFromHTML("")
	if err == nil {
		t.Fatal("expected error for empty HTML, got nil")
	}
}
