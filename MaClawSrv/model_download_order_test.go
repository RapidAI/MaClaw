package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSrvModelDownloadURLsOrder(t *testing.T) {
	t.Setenv("MACLAW_MODEL_MIRROR_URLS", "https://cn.example/m/ , ")
	got := srvModelDownloadURLs("https://github.com/x/a.gguf", "https://hub.example", "a.gguf")
	want := []string{"https://cn.example/m/a.gguf", "https://hub.example/api/v1/models/a.gguf", "https://github.com/x/a.gguf"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
}

func TestDownloadModelFileAbortsWhenSlow(t *testing.T) {
	oldW, oldMin := modelDownloadSlowWindow, modelDownloadMinBytes
	modelDownloadSlowWindow, modelDownloadMinBytes = 200*time.Millisecond, 1<<20
	defer func() { modelDownloadSlowWindow, modelDownloadMinBytes = oldW, oldMin }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000000")
		w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	start := time.Now()
	err := downloadModelFileSlowAbort(srv.URL, filepath.Join(t.TempDir(), "m.bin"), make(chan struct{}), true)
	if !errors.Is(err, errModelDownloadTooSlow) || time.Since(start) > 3*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}
