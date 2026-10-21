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
	type call struct {
		args []string
		in   []byte
	}
	var calls []call
	svc := &Service{
		ComputerDir: t.TempDir(),
		RunCommand: func(_ context.Context, in io.Reader, a ...string) (string, int, error) {
			var body []byte
			if in != nil {
				body, _ = io.ReadAll(in)
			}
			calls = append(calls, call{args: append([]string(nil), a...), in: body})
			return "", 0, nil
		},
	}
	payload := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 0xff}
	final := "/home/desktop/素材/小红书/封面.png"
	partial := final + ".partial"
	container := containerName("tenant", "alice")
	out, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/小红书/封面.png", bytes.NewReader(payload), int64(len(payload)))
	if err != nil || len(calls) != 3 || !bytes.Equal(calls[0].in, payload) || !strings.Contains(out, final) {
		t.Fatalf("out=%q err=%v calls=%d", out, err, len(calls))
	}
	assertFileServiceCall(t, calls[0].args, container, "write", partial, 0)
	wantMove := []string{"exec", container, "mv", "-f", "--", partial, final}
	if strings.Join(calls[1].args, "\x00") != strings.Join(wantMove, "\x00") {
		t.Fatalf("mv args=%v", calls[1].args)
	}
	// The write runs as root; the folders and the finished file go back to the desktop user.
	wantChown := []string{"exec", container, "chown", "desktop:desktop", "--",
		"/home/desktop/素材", "/home/desktop/素材/小红书", final}
	if strings.Join(calls[2].args, "\x00") != strings.Join(wantChown, "\x00") {
		t.Fatalf("chown args=%v", calls[2].args)
	}

	committed := len(calls)
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/.ssh/authorized_keys", bytes.NewReader(payload), int64(len(payload))); err == nil || len(calls) != committed {
		t.Fatal("upload outside materials accepted")
	}
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/a/b.bin", bytes.NewReader(payload), desktop.UploadMax+1); err == nil || len(calls) != committed {
		t.Fatal("oversize accepted")
	}
	shortFinal := "/home/desktop/素材/a/b.bin"
	if _, err := svc.UploadFile(context.Background(), "tenant", "alice", "~/素材/a/b.bin", bytes.NewReader(payload[:3]), int64(len(payload))); err == nil {
		t.Fatal("short body accepted")
	}
	short := calls[committed:]
	if len(short) != 3 {
		t.Fatalf("short calls=%v", short)
	}
	assertFileServiceCall(t, short[0].args, container, "write", shortFinal+".partial", 0)
	wantRemove := []string{"exec", container, "rm", "-f", "--", shortFinal + ".partial"}
	if strings.Join(short[1].args, "\x00") != strings.Join(wantRemove, "\x00") {
		t.Fatalf("rm args=%v", short[1].args)
	}
	wantDirs := []string{"exec", container, "chown", "desktop:desktop", "--",
		"/home/desktop/素材", "/home/desktop/素材/a"}
	if strings.Join(short[2].args, "\x00") != strings.Join(wantDirs, "\x00") {
		t.Fatalf("dir chown args=%v", short[2].args)
	}
	for _, one := range short {
		for _, arg := range one.args {
			if arg == shortFinal {
				t.Fatalf("short upload touched the finished path: %v", one.args)
			}
		}
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

func TestUploadFilePublishesAfterTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls [][]string
	svc := &Service{
		ComputerDir: t.TempDir(),
		RunCommand: func(cmdCtx context.Context, in io.Reader, a ...string) (string, int, error) {
			if cmdCtx.Err() != nil {
				return "", 0, cmdCtx.Err()
			}
			if in != nil {
				_, _ = io.ReadAll(in)
			}
			calls = append(calls, append([]string(nil), a...))
			for i := 0; i+1 < len(a); i++ {
				if a[i] == "file" && a[i+1] == "write" {
					cancel()
				}
			}
			return "", 0, nil
		},
	}
	if _, err := svc.UploadFile(ctx, "tenant", "alice", "~/素材/b/a.bin", bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("publish stopped after cancel: %d calls", len(calls))
	}
	if calls[1][2] != "mv" || calls[2][2] != "chown" {
		t.Fatalf("calls=%v", calls)
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
