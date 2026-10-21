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
	var calls [][]string
	var got []byte
	svc := &Service{
		ComputerDir: t.TempDir(),
		RunCommand: func(_ context.Context, in io.Reader, a ...string) (string, int, error) {
			calls = append(calls, append([]string(nil), a...))
			if len(calls) == 1 {
				args = calls[0]
			}
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
	// The write runs as root; the folders and file go back to the desktop user.
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	wantChown := []string{"exec", containerName("tenant", "alice"), "chown", "desktop:desktop", "--",
		"/home/desktop/素材", "/home/desktop/素材/小红书", "/home/desktop/素材/小红书/封面.png"}
	if strings.Join(calls[1], "\x00") != strings.Join(wantChown, "\x00") {
		t.Fatalf("chown args=%v", calls[1])
	}

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

func TestUploadFileFailsWhenChownFails(t *testing.T) {
	svc := &Service{
		ComputerDir: t.TempDir(),
		RunCommand: func(_ context.Context, in io.Reader, a ...string) (string, int, error) {
			if in != nil {
				_, _ = io.ReadAll(in)
			}
			if len(a) > 2 && a[2] == "chown" {
				return "chown: invalid user", 1, nil
			}
			return "", 0, nil
		},
	}
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/b/a.bin", bytes.NewReader([]byte("x")), 1); err == nil {
		t.Fatal("chown failure was hidden")
	}
}
