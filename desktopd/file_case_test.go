package desktopd

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Section 4.2. The control plane asks the in-container file service to read,
// write, list, and return bytes. Offline: no browser, no docker run.
func TestDesktopBotFileCases(t *testing.T) {
	t.Run("file_service_inside_the_container", func(t *testing.T) {
		supervisor, err := os.ReadFile("image/desktop_supervisor.py")
		if err != nil {
			t.Fatal(err)
		}
		text := string(supervisor)
		if !strings.Contains(text, `server.bind(("0.0.0.0", listen_port))`) {
			t.Fatal("in-container gate bind changed")
		}
		if strings.Count(text, "18000 +") != 1 || strings.Contains(text, "18000+") {
			t.Fatal("chrome port formula moved")
		}

		name := containerName("tenant", "alice")
		var commands [][]string
		var stdin string
		root := t.TempDir()
		svc := &Service{
			ComputerDir: root,
			RunCommand: func(_ context.Context, in io.Reader, args ...string) (string, int, error) {
				commands = append(commands, append([]string(nil), args...))
				if in != nil {
					raw, _ := io.ReadAll(in)
					stdin = string(raw)
				}
				joined := strings.Join(args, " ")
				switch {
				case strings.Contains(joined, " file read "):
					return "hello\n", 0, nil
				case strings.Contains(joined, " file bytes "):
					return base64.StdEncoding.EncodeToString([]byte("%PDF")), 0, nil
				case strings.Contains(joined, " file list "):
					return "total 1\n-rw-r--r-- notes.txt\n", 0, nil
				case strings.Contains(joined, " /desktop_supervisor.py command "):
					return "hi\n", 0, nil
				default:
					return "", 0, nil
				}
			},
		}

		wrote, err := svc.WriteFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "int main(){}\n")
		if err != nil || !strings.Contains(wrote, "/home/desktop/Desktop/a.c") || stdin != "int main(){}\n" {
			t.Fatalf("write=%q stdin=%q err=%v", wrote, stdin, err)
		}
		assertFileServiceCall(t, commands[0], name, "write", "/home/desktop/Desktop/a.c", 0)

		read, err := svc.ReadFile(context.Background(), "tenant", "alice", "/root/Desktop/a.c")
		if err != nil || read != "hello\n" {
			t.Fatalf("read=%q err=%v", read, err)
		}
		assertFileServiceCall(t, commands[1], name, "read", "/root/Desktop/a.c", desktop.FileReadMax+1)

		listed, err := svc.ListFile(context.Background(), "tenant", "alice", "notes.txt")
		if err != nil || !strings.Contains(listed, "/home/desktop/notes.txt") || !strings.Contains(listed, "notes.txt") {
			t.Fatalf("list=%q err=%v", listed, err)
		}
		assertFileServiceCall(t, commands[2], name, "list", "/home/desktop/notes.txt", 0)

		encoded := base64.StdEncoding.EncodeToString([]byte("%PDF"))
		gotBytes, err := svc.ReadBytes(context.Background(), "tenant", "alice", "~/Desktop/北京天气.pdf")
		if err != nil || gotBytes != encoded {
			t.Fatalf("bytes=%q err=%v", gotBytes, err)
		}
		assertFileServiceCall(t, commands[3], name, "bytes", "/home/desktop/Desktop/北京天气.pdf", desktop.FileBytesMax+1)

		stdin = ""
		edited, err := svc.EditFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "hello", "hello!")
		if err != nil || !strings.Contains(edited, "/home/desktop/Desktop/a.c") || stdin != "hello!\n" {
			t.Fatalf("edit=%q stdin=%q err=%v", edited, stdin, err)
		}
		assertFileServiceCall(t, commands[4], name, "read", "/home/desktop/Desktop/a.c", desktop.FileContentMax+1)
		assertFileServiceCall(t, commands[5], name, "write", "/home/desktop/Desktop/a.c", 0)

		bash, err := svc.Bash(context.Background(), "tenant", "alice", "echo hi")
		if err != nil || bash != "hi" {
			t.Fatalf("bash=%q err=%v", bash, err)
		}
		bashArgs := strings.Join(commands[6], " ")
		if !strings.Contains(bashArgs, " /desktop_supervisor.py command ") || strings.Contains(bashArgs, " file ") {
			t.Fatalf("bash was sent to the file service: %s", bashArgs)
		}

		before := len(commands)
		if _, err := svc.WriteFile(context.Background(), "tenant", "alice", `C:\Windows\a.c`, "x"); err == nil || len(commands) != before {
			t.Fatalf("host path calls=%d err=%v", len(commands)-before, err)
		}
		for _, args := range commands {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, " rm ") || strings.Contains(joined, " run ") || strings.Contains(joined, " commit ") {
				t.Fatalf("file service rebuilt the container: %s", joined)
			}
		}

		meta, err := svc.ReadComputerMeta("tenant", "alice")
		if err != nil || meta.LastActivity == "" || meta.Generation != 0 || meta.SnapshotID != "" {
			t.Fatalf("meta=%+v err=%v", meta, err)
		}
		raw, err := os.ReadFile(metaPath(t, root))
		if err != nil || strings.Contains(string(raw), "token") {
			t.Fatalf("meta file=%q err=%v", raw, err)
		}

		out := runSupervisorPython(t, `
dest = (tmp / "desk" / "sub" / "a.c").as_posix()
sup.file_write(dest, b"int main(){}\n")
class Chunks:
    def __init__(self):
        self.left = [b"ab", b"c"]
    def read(self, n):
        if not self.left:
            return b""
        return self.left.pop(0)
stream = (tmp / "desk" / "stream.bin").as_posix()
sup.file_write(stream, Chunks())
print("stream " + open(stream, "rb").read().decode())
print("home " + sup.container_path("~/Desktop/a.c"))
print("rel " + sup.container_path("notes.txt"))
print("root " + sup.container_path("/root/Desktop/a.c"))
print("dot " + sup.container_path("../etc/passwd"))
try:
    sup.container_path("C:\\Windows\\a.c")
    print("host accepted")
except ValueError:
    print("host rejected")
print("list " + " ".join(sup.file_list_command("/home/desktop")))
print("bad-op " + str(sup.run_file(["py", "file", "nope", "/home/desktop/a.c"])))
print("bad-path " + str(sup.run_file(["py", "file", "read", "C:\\Windows\\a.c", "4"])))
print("short " + str(sup.run_file(["py", "file", "list"])))
print("cut " + sup.file_read(dest, 4).decode())
print("body " + sup.file_read(dest, 20).decode())
print("b64 " + sup.file_bytes(dest, 20).decode())
print("parent " + ("1" if (tmp / "desk" / "sub" / "a.c").is_file() else "0"))
print("registry " + ("1" if (sup.ROOT / "registry.json").exists() else "0"))
`)
		for _, want := range []string{
			"home /home/desktop/Desktop/a.c",
			"rel /home/desktop/notes.txt",
			"root /root/Desktop/a.c",
			"dot /home/etc/passwd",
			"host rejected",
			"list ls -la -- /home/desktop",
			"bad-op 2",
			"bad-path 2",
			"short 2",
			"cut int ",
			"body int main(){}\n",
			"stream abc",
			"b64 " + base64.StdEncoding.EncodeToString([]byte("int main(){}\n")),
			"parent 1",
			"registry 0",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("supervisor missing %q\n%s", want, out)
			}
		}
	})
}

func assertFileServiceCall(t *testing.T, args []string, container, op, path string, limit int) {
	t.Helper()
	joined := strings.Join(args, "\x00")
	want := []string{"exec"}
	if op == "write" {
		want = append(want, "-i")
	}
	want = append(want, container, "python3", "/desktop_supervisor.py", "file", op, path)
	if limit > 0 {
		want = append(want, strconv.Itoa(limit))
	}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("file %s args=%v", op, args)
	}
	for _, arg := range args {
		switch arg {
		case "sh", "bash", "head", "ls", "base64", "xdotool", "cat":
			t.Fatalf("host built %s into %s", arg, joined)
		}
	}
}

func metaPath(t *testing.T, root string) string {
	t.Helper()
	key, err := desktop.UserKey("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "computers", "tenant", key, "meta.json")
}
