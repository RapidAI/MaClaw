package desktopd

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestContainerFileAndBashStayInTheUsersContainer(t *testing.T) {
	var commands [][]string
	var stdin string
	svc := &Service{RunCommand: func(_ context.Context, in io.Reader, args ...string) (string, int, error) {
		commands = append(commands, append([]string(nil), args...))
		if in != nil {
			raw, _ := io.ReadAll(in)
			stdin = string(raw)
		}
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, " curl "):
			return "hello" + "\n__MACLAW_HTTP__" + "200 text/html", 0, nil
		case strings.Contains(joined, " bash "):
			return "hi\n", 0, nil
		case strings.Contains(joined, "head -c"):
			return "int main(){}\n", 0, nil
		default:
			return "", 0, nil
		}
	}}
	text, err := svc.WriteFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "int main(){}\n")
	if err != nil || !strings.Contains(text, "/home/desktop/Desktop/a.c") || stdin != "int main(){}\n" {
		t.Fatalf("write=%q stdin=%q err=%v", text, stdin, err)
	}
	if !strings.Contains(strings.Join(commands[0], " "), `cat > "$1"`) || commands[0][len(commands[0])-1] != "/home/desktop/Desktop/a.c" {
		t.Fatalf("write args=%v", commands[0])
	}
	text, err = svc.ReadFile(context.Background(), "tenant", "alice", "/root/Desktop/a.c")
	if err != nil || text != "int main(){}\n" {
		t.Fatalf("read=%q err=%v", text, err)
	}
	text, err = svc.Bash(context.Background(), "tenant", "alice", "echo hi")
	if err != nil || text != "hi" {
		t.Fatalf("bash=%q err=%v", text, err)
	}
	body, status, kind, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com")
	if err != nil || body != "hello" || status != 200 || kind != "text/html" {
		t.Fatalf("http body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	if _, err := svc.WriteFile(context.Background(), "tenant", "alice", `C:\Windows\a.c`, "x"); err == nil {
		t.Fatal("host path")
	}
	if len(commands) != 4 {
		t.Fatalf("docker calls=%d", len(commands))
	}
}

func TestReadBytesKeepsAPDFIntact(t *testing.T) {
	raw := append([]byte("%PDF-1.4\n"), 0xff, 0xfe)
	encoded := base64.StdEncoding.EncodeToString(raw)
	var commands [][]string
	svc := &Service{RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
		commands = append(commands, append([]string(nil), args...))
		return encoded, 0, nil
	}}
	got, err := svc.ReadBytes(context.Background(), "tenant", "alice", "~/Desktop/北京天气.pdf")
	if err != nil || got != encoded {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
	joined := strings.Join(commands[0], " ")
	if !strings.Contains(joined, "base64 -w 0") || !strings.Contains(joined, "/home/desktop/Desktop/北京天气.pdf") {
		t.Fatalf("args=%v", commands[0])
	}
	svc.RunCommand = func(context.Context, io.Reader, ...string) (string, int, error) {
		return base64.StdEncoding.EncodeToString(make([]byte, desktop.FileBytesMax+1)), 0, nil
	}
	if _, err := svc.ReadBytes(context.Background(), "tenant", "alice", "~/Desktop/北京天气.pdf"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("large err=%v", err)
	}
}

func TestBashFailureStaysInTheToolOutput(t *testing.T) {
	svc := &Service{RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
		return "a.c:1: error: expected ';'\n", 1, nil
	}}
	text, err := svc.Bash(context.Background(), "tenant", "alice", "gcc a.c")
	if err != nil || !strings.Contains(text, "expected ';'") || !strings.Contains(text, "(exit 1)") {
		t.Fatalf("bash=%q err=%v", text, err)
	}
}

func TestContainerEditHoldsTheFileAcrossTheRewrite(t *testing.T) {
	var stdin string
	svc := &Service{RunCommand: func(_ context.Context, in io.Reader, args ...string) (string, int, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "head -c") {
			return "int main(){}\n", 0, nil
		}
		if in != nil {
			raw, _ := io.ReadAll(in)
			stdin = string(raw)
		}
		return "", 0, nil
	}}
	text, err := svc.EditFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "int main(){}", "int main(void){}")
	if err != nil || !strings.Contains(text, "/home/desktop/Desktop/a.c") || stdin != "int main(void){}\n" {
		t.Fatalf("edit=%q stdin=%q err=%v", text, stdin, err)
	}
}
