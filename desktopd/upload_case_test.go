package desktopd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestUploadFileStreamsBytesUnderMaterials(t *testing.T) {
	var args []string
	var got []byte
	svc := &Service{
		ComputerDir: t.TempDir(),
		RunCommand: func(_ context.Context, in io.Reader, a ...string) (string, int, error) {
			args = append([]string(nil), a...)
			if in != nil {
				got, _ = io.ReadAll(in)
			}
			return "", 0, nil
		},
	}
	payload := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 0xff}
	out, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/小红书/封面.png", bytes.NewReader(payload), int64(len(payload)))
	if err != nil || !bytes.Equal(got, payload) || !strings.Contains(out, "/home/desktop/素材/小红书/封面.png") {
		t.Fatalf("out=%q err=%v got=%v", out, err, got)
	}
	assertFileServiceCall(t, args, containerName("tenant", "alice"), "write", "/home/desktop/素材/小红书/封面.png", 0)

	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/.ssh/authorized_keys", bytes.NewReader(payload), int64(len(payload))); err == nil {
		t.Fatal("upload outside materials accepted")
	}
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/a/b.bin", bytes.NewReader(payload), desktop.UploadMax+1); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/a/b.bin", bytes.NewReader(payload[:3]), int64(len(payload))); err == nil {
		t.Fatal("short body accepted")
	}
}

func TestWithBotIDLeavesUploadBodyAlone(t *testing.T) {
	body := []byte(`binary "bot_id" not json`)
	var seen []byte
	var user string
	h := withBotID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = io.ReadAll(r.Body)
		user = r.URL.Query().Get("user_id")
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/desktops/upload?user_id=alice&bot_id=b1&path=x", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(seen, body) {
		t.Fatalf("code=%d seen=%q", rec.Code, seen)
	}
	if user == "alice" || !strings.Contains(user, "b1") {
		t.Fatalf("bot not folded into user: %q", user)
	}
}
