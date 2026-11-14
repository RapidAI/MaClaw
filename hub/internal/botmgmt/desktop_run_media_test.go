package botmgmt

import (
	"context"
	"encoding/base64"
	"testing"
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

func TestDesktopContainerFilePathStaysOnTheDesktop(t *testing.T) {
	got, err := DesktopContainerFilePath("~/work/bot_a/snake.cpp")
	if err != nil || got != "/home/desktop/work/bot_a/snake.cpp" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := DesktopContainerFilePath("/Users/dan/Desktop/report.pdf"); err == nil {
		t.Fatal("a path outside the desktop was accepted")
	}
	svc := &Service{MailFileDir: t.TempDir()}
	var asked string
	name, mime, data, err := svc.ReadDesktopContainerFile(context.Background(), "tenant-a", "alice", "bot_1", "/home/desktop/work/bot_a/snake.cpp", func(_ context.Context, _, _, path string) (string, error) {
		asked = path
		return base64.StdEncoding.EncodeToString([]byte("int main(){}")), nil
	})
	if err != nil || name != "snake.cpp" || mime != "text/plain" || data == "" || asked != "/home/desktop/work/bot_a/snake.cpp" {
		t.Fatalf("name=%s mime=%s data=%s asked=%s err=%v", name, mime, data, asked, err)
	}
	if _, _, _, err := svc.ReadDesktopContainerFile(context.Background(), "tenant-a", "alice", "bot_1", "/Users/dan/notes.txt", func(context.Context, string, string, string) (string, error) {
		t.Fatal("reader was called")
		return "", nil
	}); err == nil {
		t.Fatal("outside path was read")
	}
}
