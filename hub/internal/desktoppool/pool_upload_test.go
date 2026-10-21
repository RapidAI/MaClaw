package desktoppool

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUploadFileStreamsRawBytes(t *testing.T) {
	var got []byte
	var path, query, ctype, auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/desktops/upload" {
			_, _ = w.Write([]byte(`{"ok":true,"status":"running"}`))
			return
		}
		path, query, ctype, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		got, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"output":"Wrote it"}`))
	}))
	defer server.Close()
	pool := New(&memSettings{}, memDir{})
	view, err := pool.CreateServer(context.Background(), "tenant-a", Server{Name: "u", BaseURL: server.URL, AccessToken: "up-token", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0, 1, 2, 0xff, '"'}
	out, err := pool.UploadFile(context.Background(), "tenant-a", "alice", "~/素材/bot/a.bin", bytes.NewReader(payload), int64(len(payload)))
	if err != nil || out != "Wrote it" || path != "/v1/desktops/upload" || !bytes.Equal(got, payload) || ctype != "application/octet-stream" || auth != "Bearer up-token" {
		t.Fatalf("out=%q err=%v path=%s ctype=%s got=%v", out, err, path, ctype, got)
	}
	if !bytes.Contains([]byte(query), []byte("user_id=alice")) {
		t.Fatalf("query %s", query)
	}
	path = ""
	if _, err := pool.UploadFile(context.Background(), "tenant-a", "alice", "~/.bashrc", bytes.NewReader(payload), 5); err == nil || path != "" {
		t.Fatalf("path outside materials forwarded: %v", err)
	}
}
