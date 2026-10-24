package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDesktopScreenYOLOModeEnv(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "auto": "auto", "junk": "auto", "on": "on", "ON": "on", "1": "on", "off": "off", "0": "off"} {
		t.Setenv("MACLAW_SCREEN_YOLO", in)
		if got := desktopScreenYOLOMode(); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	t.Setenv("MACLAW_SCREEN_YOLO_MIN_ELEMENTS", "")
	if desktopScreenYOLOMinElements() != desktopYOLODefaultMinElements {
		t.Fatal("default threshold")
	}
	t.Setenv("MACLAW_SCREEN_YOLO_MIN_ELEMENTS", "4")
	if desktopScreenYOLOMinElements() != 4 {
		t.Fatal("threshold env")
	}
	t.Setenv("MACLAW_SCREEN_YOLO_TIMEOUT_MS", "250")
	if desktopScreenYOLOTimeout() != 250*time.Millisecond {
		t.Fatal("timeout env")
	}
}

func TestDecideDesktopYOLO(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		pixels       bool
		class, title string
		winOK        bool
		ocr, ax      int
		run          bool
		reasonPrefix string
	}{
		{"off", "off", true, "Thunar", "", true, 0, 0, false, "mode_off"},
		{"on forces browser", "on", true, "google-chrome", "x", true, 99, 99, true, "mode_on"},
		{"web page", "auto", false, "Thunar", "", true, 0, 0, false, "web_page"},
		{"window unknown", "auto", true, "", "", false, 0, 0, false, "window_unknown"},
		{"browser", "auto", true, "Google-chrome", "知乎 - Chrome", true, 2, -1, false, "browser_window"},
		{"browser file dialog", "auto", true, "google-chrome", "Open Files", true, 3, -1, true, "native_dialog_few_elements"},
		{"app enough ocr", "auto", true, "libreoffice-calc", "Untitled 1", true, 40, -1, false, "enough_elements"},
		{"app ocr+ax enough", "auto", true, "Xmessage", "", true, 3, 12, false, "enough_elements(15>=15)"},
		{"app few", "auto", true, "Xmessage", "xmessage", true, 3, 2, true, "few_elements(5<15)"},
	}
	for _, c := range cases {
		d := decideDesktopYOLO(c.mode, c.pixels, c.class, c.title, c.winOK, c.ocr, c.ax, 15)
		if d.Run != c.run || !strings.HasPrefix(d.Reason, c.reasonPrefix) {
			t.Errorf("%s: got %+v", c.name, d)
		}
	}
}

func TestDesktopReadFocusedWindowAndAXCount(t *testing.T) {
	run := func(argv ...string) (string, error) {
		if argv[len(argv)-1] == "getwindowclassname" {
			return "google-chrome\n", nil
		}
		return "打开文件\n", nil
	}
	c, title, ok := desktopReadFocusedWindow(run)
	if !ok || c != "google-chrome" || title != "打开文件" {
		t.Fatalf("%q %q %v", c, title, ok)
	}
	if _, _, ok := desktopReadFocusedWindow(func(...string) (string, error) { return "", errors.New("x") }); ok {
		t.Fatal("failed read reported ok")
	}
	block := desktopAXFirstBlock("window: Open, 2 listed\n[1] push button \"Open\"\n[2] push button \"Cancel\"")
	if n := desktopAXElementCount(block); n != 2 {
		t.Fatalf("ax count %d", n)
	}
	if desktopAXElementCount("") != 0 {
		t.Fatal("empty block")
	}
}

func TestDesktopDetectViaServiceSkips(t *testing.T) {
	// No model service: never load locally.
	m := newSrvAIModelManager(t.TempDir())
	m.remote = nil
	if _, _, err := m.desktopDetectViaService(context.Background(), "x"); desktopYOLOSkipReason(err) != "no_model_service" {
		t.Fatalf("err=%v", err)
	}
	if m.yoloParser != nil {
		t.Fatal("client loaded a parser")
	}
	// Service without weights.
	svc := newSrvAIModelManager(t.TempDir())
	svc.remote = nil
	client := newSrvAIModelManager(t.TempDir())
	client.remote = newModelRemote(startTestModelService(t, svc), true)
	if _, _, err := client.desktopDetectViaService(context.Background(), "x"); desktopYOLOSkipReason(err) != "weights_missing" {
		t.Fatalf("err=%v", err)
	}
	if client.yoloParser != nil {
		t.Fatal("in-process fallback loaded a parser")
	}
	// Busy: a pass already in flight in this process.
	desktopYOLOSem <- struct{}{}
	_, _, err := client.desktopDetectViaService(context.Background(), "x")
	<-desktopYOLOSem
	if desktopYOLOSkipReason(err) != "busy" {
		t.Fatalf("busy err=%v", err)
	}
	// Slow service: skipped after the timeout.
	t.Setenv("MACLAW_SCREEN_YOLO_TIMEOUT_MS", "200")
	slow := startSlowModelService(t, 2*time.Second)
	client.remote = newModelRemote(slow, false)
	start := time.Now()
	_, _, err = client.desktopDetectViaService(context.Background(), "x")
	if desktopYOLOSkipReason(err) != "timeout" || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("timeout err=%v after %v", err, time.Since(start))
	}
}

func startSlowModelService(t *testing.T, d time.Duration) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "slow.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
		}
		_, _ = w.Write([]byte(`{"elements":[]}`))
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func captureVisionLog(t *testing.T) func() []string {
	var mu sync.Mutex
	var lines []string
	prev := desktopVisionLog
	desktopVisionLog = func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	t.Cleanup(func() { desktopVisionLog = prev })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

func TestScreenVisionGateLogsMetrics(t *testing.T) {
	t.Setenv("MACLAW_SCREEN_YOLO", "")
	svc := newSrvAIModelManager(t.TempDir())
	svc.remote = nil
	client := newSrvAIModelManager(t.TempDir())
	client.remote = newModelRemote(startTestModelService(t, svc), false)
	logs := captureVisionLog(t)
	img := tinyPNG(t)

	win := func(class, title string) context.Context {
		return withDesktopVisionHints(context.Background(), desktopVisionHints{AXCount: 1, Window: func() (string, string, bool) { return class, title, true }})
	}
	client.screenVision(win("google-chrome", "知乎"), img, true)
	client.screenVision(win("Xmessage", "xmessage"), img, true)
	t.Setenv("MACLAW_SCREEN_YOLO", "off")
	client.screenVision(win("Xmessage", "xmessage"), img, true)

	got := logs()
	if len(got) != 2 { // off with no OCR installed has nothing to observe
		t.Fatalf("logs=%q", got)
	}
	if !strings.Contains(got[0], "yolo=no reason=browser_window") || !strings.Contains(got[0], `window_class="google-chrome"`) {
		t.Fatalf("browser log %q", got[0])
	}
	// Gate says run; the service has no weights, so it is skipped cleanly.
	if !strings.Contains(got[1], "yolo=no reason=few_elements(1<15)/skipped_weights_missing") || !strings.Contains(got[1], "peak_rss_mb=") {
		t.Fatalf("app log %q", got[1])
	}
	if client.yoloParser != nil {
		t.Fatal("client loaded a parser")
	}
}

func TestScreenVisionNoServiceNeverRunsYOLO(t *testing.T) {
	t.Setenv("MACLAW_SCREEN_YOLO", "on")
	m := newSrvAIModelManager(t.TempDir())
	m.remote = nil
	logs := captureVisionLog(t)
	if got := m.screenVision(context.Background(), tinyPNG(t), true); got != "" {
		t.Fatalf("vision=%q", got)
	}
	if len(logs()) != 0 || m.yoloParser != nil {
		t.Fatalf("logs=%q parser=%v", logs(), m.yoloParser != nil)
	}
}
