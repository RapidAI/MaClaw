package botmgmt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopRunMediaSurvivesWithoutTheReply(t *testing.T) {
	svc := &Service{MailFileDir: t.TempDir()}
	data := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	err := svc.keepDesktopRunMedia("tenant-a", "alice", "bot_1", "run_7c0236e7", []ReplyImage{{MIME: "image/png", Data: data}}, []ReplyFile{{Name: "snake.cpp", MIME: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("int main(){}"))}})
	if err != nil {
		t.Fatal(err)
	}
	images, err := svc.DesktopRunImages("tenant-a", "alice", "bot_1", "run_7c0236e7")
	if err != nil || len(images) != 1 || images[0].Data != data || images[0].ID == "" {
		t.Fatalf("images %+v %v", images, err)
	}
	files, err := svc.DesktopRunFiles("tenant-a", "alice", "bot_1", "run_7c0236e7")
	if err != nil || len(files) != 1 || files[0].Name != "snake.cpp" {
		t.Fatalf("files %+v %v", files, err)
	}
	other, err := svc.DesktopRunImages("tenant-a", "bob", "bot_1", "run_7c0236e7")
	if err != nil || len(other) != 0 {
		t.Fatalf("other user %+v %v", other, err)
	}
}

func TestDesktopRunMediaExpires(t *testing.T) {
	svc := &Service{MailFileDir: t.TempDir()}
	data := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	if err := svc.keepDesktopRunMedia("tenant-a", "alice", "bot_1", "run_old", []ReplyImage{{MIME: "image/png", Data: data}}, nil); err != nil {
		t.Fatal(err)
	}
	dir := svc.desktopRunMediaDir("tenant-a", "alice", "bot_1")
	raw, err := os.ReadFile(filepath.Join(dir, "run_old.json"))
	if err != nil {
		t.Fatal(err)
	}
	var media desktopRunMedia
	if err := json.Unmarshal(raw, &media); err != nil {
		t.Fatal(err)
	}
	media.SavedAt = time.Now().Add(-desktopRunMediaTTL - time.Hour)
	rewritten, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run_old.json"), rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
	images, err := svc.DesktopRunImages("tenant-a", "alice", "bot_1", "run_old")
	if err != nil || len(images) != 0 {
		t.Fatalf("expired images %+v %v", images, err)
	}
	svc.forgetDesktopRunMedia("tenant-a", "alice", "bot_1")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("bot media dir remained: %v", err)
	}
}

func TestDesktopContainerFilePathStaysInWorkOrOnTheDesktop(t *testing.T) {
	got, err := DesktopContainerFilePath("~/work/bot_a/snake.cpp")
	if err != nil || got != "/home/desktop/work/bot_a/snake.cpp" {
		t.Fatalf("got %q %v", got, err)
	}
	desktop, err := DesktopContainerFilePath("~/Desktop/shot.png")
	if err != nil || desktop != "/home/desktop/Desktop/shot.png" {
		t.Fatalf("desktop %q %v", desktop, err)
	}
	for _, raw := range []string{"/Users/dan/Desktop/report.pdf", "/home/desktop/Documents/a.txt", "/home/desktop/work/bot_a/.secret/key"} {
		if _, err := DesktopContainerFilePath(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	settings := &memSettings{m: map[string]string{}}
	raw, _ := json.Marshal(record{Bots: []Bot{{ID: "bot_1", OwnerUserID: "alice"}}})
	settings.m[storageKey("tenant-a")] = string(raw)
	svc := NewService(settings)
	svc.MailFileDir = t.TempDir()
	var asked string
	name, mime, data, err := svc.ReadDesktopContainerFile(context.Background(), "tenant-a", "alice", "bot_1", "/home/desktop/work/bot_a/snake.cpp", func(_ context.Context, _, _, path string) (string, error) {
		asked = path
		return base64.StdEncoding.EncodeToString([]byte("int main(){}")), nil
	})
	if err != nil || name != "snake.cpp" || mime != "text/plain" || data == "" || asked != "/home/desktop/work/bot_a/snake.cpp" {
		t.Fatalf("name=%s mime=%s data=%s asked=%s err=%v", name, mime, data, asked, err)
	}
	if _, _, _, err := svc.ReadDesktopContainerFile(context.Background(), "tenant-a", "alice", "missing", "/home/desktop/work/bot_a/snake.cpp", func(context.Context, string, string, string) (string, error) {
		t.Fatal("an unknown bot read the owner desktop")
		return "", nil
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown bot err=%v", err)
	}
}
