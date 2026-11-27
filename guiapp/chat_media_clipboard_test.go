package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	runtime "github.com/RapidAI/CodeClaw/guiapp/wruntime"
)

func TestChatMediaDIBIsBottomUpBGRA(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	img.SetRGBA(1, 0, color.RGBA{R: 5, G: 6, B: 7, A: 8})
	dib := dibFromImage(img)
	if len(dib) != 40+8 {
		t.Fatalf("dib length = %d", len(dib))
	}
	if binary.LittleEndian.Uint32(dib[0:]) != 40 || binary.LittleEndian.Uint32(dib[4:]) != 2 || binary.LittleEndian.Uint32(dib[8:]) != 1 {
		t.Fatalf("header = %v", dib[:16])
	}
	if dib[40] != 3 || dib[41] != 2 || dib[42] != 1 || dib[43] != 4 {
		t.Fatalf("pixel0 = %v", dib[40:44])
	}
	if dib[44] != 7 || dib[45] != 6 || dib[46] != 5 || dib[47] != 8 {
		t.Fatalf("pixel1 = %v", dib[44:48])
	}
}

func TestChatMediaDIBKeepsStraightAlphaAndBottomRowFirst(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 250, G: 10, B: 20, A: 128})
	img.SetNRGBA(0, 1, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	dib := dibFromImage(img)
	if dib[40] != 255 || dib[41] != 0 || dib[42] != 0 || dib[43] != 255 {
		t.Fatalf("bottom pixel = %v", dib[40:44])
	}
	if dib[44] != 20 || dib[45] != 10 || dib[46] != 250 || dib[47] != 128 {
		t.Fatalf("top pixel = %v, want straight alpha", dib[44:48])
	}
}

func TestChatMediaDIBDrawsGrayWithoutPerPixelAt(t *testing.T) {
	img := image.NewGray(image.Rect(2, 3, 3, 4))
	img.SetGray(2, 3, color.Gray{Y: 200})
	dib := dibFromImage(img)
	if dib[40] != 200 || dib[41] != 200 || dib[42] != 200 || dib[43] != 255 {
		t.Fatalf("gray pixel = %v", dib[40:44])
	}
}

func TestChatMediaHDROPListsUTF16Path(t *testing.T) {
	const path = `D:\shots\屏幕.png`
	raw := hdropBytes([]string{path})
	if binary.LittleEndian.Uint32(raw[0:]) != 20 || binary.LittleEndian.Uint32(raw[16:]) != 1 {
		t.Fatalf("header pFiles/fWide = %d %d", binary.LittleEndian.Uint32(raw[0:]), binary.LittleEndian.Uint32(raw[16:]))
	}
	files := raw[20:]
	decoded := utf16.Decode(u16s(files))
	text := string(decoded)
	if !strings.HasPrefix(text, path) {
		t.Fatalf("listing = %q", text)
	}
	if !strings.HasSuffix(text, "\x00") {
		t.Fatal("listing is missing the extra NUL")
	}
}

func TestChatMediaCopyImagePutsPictureOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, mustPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	var got chatMediaClipboard
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(payload chatMediaClipboard) error {
		got = payload
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })

	if err := copyChatMediaForTest("image", path); err != nil {
		t.Fatal(err)
	}
	if len(got.dib) < 40 || !bytes.HasPrefix(got.png, []byte("\x89PNG")) || len(got.hdrop) != 0 {
		t.Fatalf("payload dib=%d png=%d hdrop=%d", len(got.dib), len(got.png), len(got.hdrop))
	}
}

func TestChatMediaCopyFileIsFileDropOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(path, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got chatMediaClipboard
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(payload chatMediaClipboard) error {
		got = payload
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })
	if err := copyChatMediaForTest("file", path); err != nil {
		t.Fatal(err)
	}
	if len(got.dib) != 0 || len(got.png) != 0 || len(got.hdrop) < 20 {
		t.Fatalf("payload dib=%d png=%d hdrop=%d", len(got.dib), len(got.png), len(got.hdrop))
	}
}

func TestChatMediaCopyDataURLHasNoFileDrop(t *testing.T) {
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mustPNG(t))
	var got chatMediaClipboard
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(payload chatMediaClipboard) error {
		got = payload
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })
	if err := copyChatMediaForTest("image", payload); err != nil {
		t.Fatal(err)
	}
	if len(got.dib) < 40 || len(got.hdrop) != 0 {
		t.Fatalf("dib=%d hdrop=%d", len(got.dib), len(got.hdrop))
	}
}

func TestChatMediaCopyRejectsRelativeAndCredentialURLs(t *testing.T) {
	if err := copyChatMediaForTest("file", "notes.pdf"); err == nil {
		t.Fatal("relative path was copied")
	}
	if err := copyChatMediaForTest("image", "http://user:pass@example.com/a.png"); err == nil {
		t.Fatal("credential URL was copied")
	}
	if err := copyChatMediaForTest("file", "https://example.com/a.pdf"); err == nil {
		t.Fatal("remote file was copied")
	}
}

func TestChatMediaCopyUnpaddedDataURL(t *testing.T) {
	raw := mustPNG(t)
	encoded := strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
	if strings.HasSuffix(encoded, "=") {
		t.Fatal("payload is still padded")
	}
	var got chatMediaClipboard
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(payload chatMediaClipboard) error {
		got = payload
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })
	if err := copyChatMediaForTest("image", "data:image/png;base64,"+encoded); err != nil {
		t.Fatal(err)
	}
	if len(got.dib) < 40 || len(got.hdrop) != 0 || !bytes.Equal(got.png, raw) {
		t.Fatalf("dib=%d hdrop=%d pngMatch=%v", len(got.dib), len(got.hdrop), bytes.Equal(got.png, raw))
	}
}

func TestChatMediaCopyRemoteImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(mustPNG(t))
	}))
	t.Cleanup(srv.Close)
	var got chatMediaClipboard
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(payload chatMediaClipboard) error {
		got = payload
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })
	if err := copyChatMediaForTest("image", srv.URL+"/shot.png"); err != nil {
		t.Fatal(err)
	}
	if len(got.dib) < 40 || len(got.hdrop) != 0 {
		t.Fatalf("dib=%d hdrop=%d", len(got.dib), len(got.hdrop))
	}
}

func TestChatMediaSaveCopiesOriginalBytesAndCancelWritesNothing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "shot.png")
	original := mustPNG(t)
	if err := os.WriteFile(src, original, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.png")
	prev := chatMediaSaveDialog
	chatMediaSaveDialog = func(_ *App, title, defaultName string, filters []runtime.FileFilter) (string, error) {
		if title != "保存" || defaultName != "shot.png" || len(filters) < 2 || filters[0].Pattern != "*.png" {
			t.Fatalf("dialog title=%q name=%q filters=%+v", title, defaultName, filters)
		}
		return dest, nil
	}
	t.Cleanup(func() { chatMediaSaveDialog = prev })
	got, err := saveChatMediaAs(&App{}, "image", src, "shot.png", "保存")
	if err != nil || got != dest {
		t.Fatalf("save = %q %v", got, err)
	}
	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, original) {
		t.Fatal("saved bytes differ from the original file")
	}
	if _, err := os.Stat(dest + ".maclawpart"); !os.IsNotExist(err) {
		t.Fatal("temp file was left behind")
	}

	chatMediaSaveDialog = func(_ *App, _, _ string, _ []runtime.FileFilter) (string, error) {
		return "", errors.New("cancelled by user")
	}
	cancelled := filepath.Join(dir, "nope.png")
	got, err = saveChatMediaAs(&App{}, "file", src, "shot.png", "保存")
	if err != nil || got != "" {
		t.Fatalf("cancel = %q %v", got, err)
	}
	if _, statErr := os.Stat(cancelled); !os.IsNotExist(statErr) {
		t.Fatal("cancel created a file")
	}
}

func TestChatMediaSaveDataURLUsesDetectedExtension(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "picture.png")
	var chosen string
	prev := chatMediaSaveDialog
	chatMediaSaveDialog = func(_ *App, _, defaultName string, filters []runtime.FileFilter) (string, error) {
		if defaultName != "picture.png" || filters[0].Pattern != "*.png" {
			t.Fatalf("default name = %q filters = %+v", defaultName, filters)
		}
		chosen = dest
		return dest, nil
	}
	t.Cleanup(func() { chatMediaSaveDialog = prev })
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mustPNG(t))
	// The bytes are PNG, so a misleading .jpg name is replaced before the dialog.
	got, err := saveChatMediaAs(&App{}, "image", payload, "picture.jpg", "保存")
	if err != nil || got != chosen {
		t.Fatalf("save = %q %v", got, err)
	}
	written, err := os.ReadFile(dest)
	if err != nil || !bytes.HasPrefix(written, []byte("\x89PNG")) {
		t.Fatalf("written %v %v", written, err)
	}
}

func TestChatMediaSaveReplacesExistingAndKeepsItWhenWriteFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.png")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeReaderAtomic(dest, bytes.NewReader([]byte("new"))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "new" {
		t.Fatalf("replaced = %q %v", got, err)
	}
	if _, statErr := os.Stat(dest + ".maclawpart"); !os.IsNotExist(statErr) {
		t.Fatal("temp file was left behind")
	}

	if err := os.WriteFile(dest, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeReaderAtomic(dest, errReader{}); err == nil {
		t.Fatal("expected write error")
	}
	got, err = os.ReadFile(dest)
	if err != nil || string(got) != "keep" {
		t.Fatalf("dest after failed write = %q %v", got, err)
	}
	if _, statErr := os.Stat(dest + ".maclawpart"); !os.IsNotExist(statErr) {
		t.Fatal("temp file was left behind after a failed write")
	}
}

func TestChatMediaSaveRemoteImagePromptsBeforeDownload(t *testing.T) {
	pngBytes := mustPNG(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	dest := filepath.Join(dir, "shot.png")
	calls := 0
	prev := chatMediaSaveDialog
	chatMediaSaveDialog = func(_ *App, title, defaultName string, filters []runtime.FileFilter) (string, error) {
		calls++
		if hits != 0 {
			t.Fatalf("download started before the dialog, hits=%d", hits)
		}
		if title != "保存" || defaultName != "shot.png" || len(filters) < 1 || filters[0].Pattern != "*.png" {
			t.Fatalf("dialog title=%q name=%q filters=%+v", title, defaultName, filters)
		}
		if calls == 1 {
			return "", errors.New("cancelled by user")
		}
		return dest, nil
	}
	t.Cleanup(func() { chatMediaSaveDialog = prev })

	got, err := saveChatMediaAs(&App{}, "image", srv.URL+"/shot.png", "shot.png", "保存")
	if err != nil || got != "" || hits != 0 {
		t.Fatalf("cancel = %q %v hits=%d", got, err, hits)
	}
	got, err = saveChatMediaAs(&App{}, "image", srv.URL+"/shot.png", "shot.png", "保存")
	if err != nil || got != dest {
		t.Fatalf("save = %q %v", got, err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d", hits)
	}
	written, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(written, pngBytes) {
		t.Fatalf("written %v %v", written, err)
	}
	if _, statErr := os.Stat(dest + ".maclawpart"); !os.IsNotExist(statErr) {
		t.Fatal("temp file was left behind")
	}
}

func TestChatMediaCopyRejectsMetadataAddress(t *testing.T) {
	prev := writeChatMediaClipboard
	writeChatMediaClipboard = func(chatMediaClipboard) error {
		t.Fatal("clipboard was written")
		return nil
	}
	t.Cleanup(func() { writeChatMediaClipboard = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	app := &App{ctx: ctx}

	_, err := buildChatMediaClipboard(app, "image", "http://169.254.169.254/latest/meta-data/")
	if err == nil || !strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("direct metadata = %v", err)
	}

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	_, err = buildChatMediaClipboard(app, "image", srv.URL+"/shot.png")
	if err == nil || !strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("metadata redirect = %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestChatMediaDialAllowsOnlyTheConfiguredProxy(t *testing.T) {
	dialer := &net.Dialer{Timeout: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := dialChatMediaImage(ctx, dialer, "cdn.example.test", "tcp", "127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("image dial = %v", err)
	}
	// A different private address stays blocked when the value names another port.
	other := context.WithValue(ctx, chatMediaProxyDialKey{}, "127.0.0.1:2")
	_, err = dialChatMediaImage(other, dialer, "cdn.example.test", "tcp", "127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("mismatched proxy dial = %v", err)
	}
	// Port 1 refuses quickly. The failure must be the dial, so the proxy
	// address itself was not classified as a forbidden picture host.
	matched := context.WithValue(ctx, chatMediaProxyDialKey{}, "127.0.0.1:1")
	_, err = dialChatMediaImage(matched, dialer, "cdn.example.test", "tcp", "127.0.0.1:1")
	if err == nil || strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("proxy dial = %v", err)
	}
}

func TestChatMediaFetchUsesProxyAndStillBlocksMetadata(t *testing.T) {
	pngBytes := mustPNG(t)
	var hits int
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.RequestURI, "cdn.example.test") {
			t.Errorf("proxy URI = %q", r.RequestURI)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	t.Cleanup(proxy.Close)
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	prevProxy := chatMediaProxyFromEnvironment
	chatMediaProxyFromEnvironment = func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}
	prevLookup := chatMediaLookupIP
	chatMediaLookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "cdn.example.test":
			return []net.IPAddr{{IP: net.IPv4(8, 8, 8, 8)}}, nil
		case "169.254.169.254":
			return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
		default:
			return net.DefaultResolver.LookupIPAddr(ctx, host)
		}
	}
	t.Cleanup(func() {
		chatMediaProxyFromEnvironment = prevProxy
		chatMediaLookupIP = prevLookup
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	_, err = fetchRemoteChatImage(ctx, "http://169.254.169.254/latest/meta-data/")
	if err == nil || !strings.Contains(err.Error(), "unsupported image address") {
		t.Fatalf("metadata via proxy = %v", err)
	}
	if hits != 0 {
		t.Fatalf("blocked host reached the proxy, hits=%d", hits)
	}
	data, err := fetchRemoteChatImage(ctx, "http://cdn.example.test/shot.png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, pngBytes) || hits != 1 {
		t.Fatalf("hits=%d bytes=%d", hits, len(data))
	}
}

func TestChatMediaIPAllowed(t *testing.T) {
	cases := []struct {
		host string
		ip   string
		ok   bool
	}{
		{"127.0.0.1", "127.0.0.1", true},
		{"localhost", "127.0.0.1", true},
		{"localhost", "::1", true},
		{"localhost.", "127.0.0.1", true},
		{"169.254.169.254", "169.254.169.254", false},
		{"evil.example", "169.254.169.254", false},
		{"evil.example", "::ffff:169.254.169.254", false},
		{"8.8.8.8", "8.8.8.8", true},
		{"10.0.0.1", "10.0.0.1", true},
		{"evil.example", "10.0.0.1", false},
		{"127.0.0.1", "::ffff:127.0.0.1", true},
		{"camera.local", "192.168.1.5", false},
		{"0.0.0.0", "0.0.0.0", false},
	}
	for _, tc := range cases {
		if got := chatMediaIPAllowed(tc.host, net.ParseIP(tc.ip)); got != tc.ok {
			t.Errorf("host=%s ip=%s got %v want %v", tc.host, tc.ip, got, tc.ok)
		}
	}
	if chatMediaIPAllowed("127.0.0.1", nil) {
		t.Error("nil address was allowed")
	}
}

func TestChatMediaFileNameDropsPathAndReservedDevice(t *testing.T) {
	if got := chatMediaFileName(`..\con.txt`, "image.png"); got != "image.png" {
		t.Fatalf("reserved = %q", got)
	}
	if got := chatMediaFileName(`C:\a\b\my shot.png`, "image.png"); got != "my shot.png" {
		t.Fatalf("base = %q", got)
	}
}

func copyChatMediaForTest(kind, source string) error {
	payload, err := buildChatMediaClipboard(nil, kind, source)
	if err != nil {
		return err
	}
	return writeChatMediaClipboard(payload)
}

func mustPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func u16s(raw []byte) []uint16 {
	out := make([]uint16, len(raw)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return out
}
