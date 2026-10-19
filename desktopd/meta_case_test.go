package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotMetaCases(t *testing.T) {
	t.Run("screen_table_without_exec", func(t *testing.T) {
		if (&Service{}).Stage2Sleep {
			t.Fatal("writing meta turned stage-2 sleep on")
		}
		if Stage2Idle != 5*time.Minute {
			t.Fatalf("idle limit = %s", Stage2Idle)
		}
		token := strings.Repeat("cd", 16)
		ownerKey, err := desktop.UserKey("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		botKey, err := desktop.BotKey("tenant", "alice", "bot-21")
		if err != nil {
			t.Fatal(err)
		}
		mounts, err := desktop.PrivateMounts("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		mountText := make([]string, 0, len(mounts))
		for _, mount := range mounts {
			mountText = append(mountText, mount.Volume+"="+mount.Target)
		}
		inspectLine := "true|1g|" + strings.Join(mountText, " ") + "|maclaw-gui:2|maclaw-gui:2|"
		exists := false
		var calls []string
		root := t.TempDir()
		svc := &Service{
			AdvertiseHost: "docker.example",
			ComputerDir:   root,
			Now: func() time.Time {
				return time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
			},
			Run: func(_ context.Context, args ...string) (string, error) {
				joined := strings.Join(args, " ")
				calls = append(calls, joined)
				switch args[0] {
				case "image":
					if strings.Contains(joined, "maclaw-desktop-user-") {
						return "", errors.New("Error: No such image")
					}
					return "sha256:img", nil
				case "inspect":
					if !exists {
						return "", errors.New("Error: No such object")
					}
					return inspectLine, nil
				case "run":
					exists = true
					if strings.Contains(joined, "computers:") || strings.Contains(joined, root) {
						t.Fatalf("docker run gained a meta mount: %s", joined)
					}
					return "container", nil
				case "exec":
					if strings.Contains(joined, " ensure ") && strings.Contains(joined, botKey) {
						return `{"display":":21","cdp_port":19020,"token":"` + token + `"}`, nil
					}
					if strings.Contains(joined, " ensure ") {
						return `{"display":":20","cdp_port":19020,"token":"` + token + `"}`, nil
					}
					return "", nil
				case "port":
					if strings.Contains(joined, "6080") {
						return "0.0.0.0:6081", nil
					}
					return "0.0.0.0:32768", nil
				case "update", "stop":
					return "", nil
				default:
					return "", errors.New("unexpected docker args: " + joined)
				}
			},
			RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
				return "ok\n", 0, nil
			},
		}
		spec := Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "2g", CPUs: "1", ShmSize: "1g"}
		if _, err := svc.OpenSession(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		spec.BotID = "bot-21"
		if _, err := svc.OpenSession(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		runs, rms := 0, 0
		for _, call := range calls {
			if strings.HasPrefix(call, "run ") {
				runs++
				if !strings.Contains(call, "--publish 127.0.0.1::19020/tcp") || !strings.Contains(call, "--publish 127.0.0.1::6080/tcp") {
					t.Fatalf("publish changed: %s", call)
				}
				if strings.Contains(call, "--publish 19020/tcp") || strings.Contains(call, "--publish 6080/tcp") {
					t.Fatalf("publish is still on every interface: %s", call)
				}
			}
			if strings.HasPrefix(call, "rm ") {
				rms++
			}
		}
		if runs != 1 || rms != 0 {
			t.Fatalf("container was recreated runs=%d rm=%d\n%v", runs, rms, calls)
		}
		svc.Now = func() time.Time { return time.Date(2026, 10, 18, 4, 1, 0, 0, time.UTC) }
		if _, err := svc.ReadFile(context.Background(), "tenant", "alice", "notes.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Bash(context.Background(), "tenant", "alice", "echo hi"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.App(context.Background(), "tenant", "alice", ":20", []string{"key", "a"}); err != nil {
			t.Fatal(err)
		}
		before := len(calls)
		meta, err := svc.ReadComputerMeta("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != before {
			t.Fatalf("reading meta called docker: %d new calls", len(calls)-before)
		}
		if meta.State != "running" || meta.Runtime != "docker" || meta.SnapshotID != "" || meta.Generation != 0 {
			t.Fatalf("meta = %#v", meta)
		}
		if meta.Host != "docker.example" {
			t.Fatalf("host = %q", meta.Host)
		}
		if meta.LastActivity != "2026-10-18T04:01:00Z" {
			t.Fatalf("activity = %q", meta.LastActivity)
		}
		if len(meta.Screens) != 2 || meta.Screens[0].Screen != 1 || meta.Screens[0].Display != ":20" || meta.Screens[0].Agent != "" {
			t.Fatalf("screens = %#v", meta.Screens)
		}
		if meta.Screens[1].Screen != 2 || meta.Screens[1].Display != ":21" || meta.Screens[1].Agent != "bot-21" {
			t.Fatalf("bot screen = %#v", meta.Screens[1])
		}
		raw, err := os.ReadFile(filepath.Join(svc.ComputerDir, "computers", "tenant", ownerKey, "meta.json"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, token) || strings.Contains(text, `"token"`) {
			t.Fatalf("meta contains a token: %s", text)
		}
		if _, err := svc.ReadComputerMeta("tenant", "bob"); err == nil {
			t.Fatal("bob read alice's record")
		}
		if _, err := svc.ReadComputerMeta("..", "alice"); err == nil {
			t.Fatal("tenant .. was accepted")
		}
		if runtime.GOOS != "windows" {
			for _, dir := range []string{
				filepath.Join(svc.ComputerDir, "computers"),
				filepath.Join(svc.ComputerDir, "computers", "tenant"),
				filepath.Join(svc.ComputerDir, "computers", "tenant", ownerKey),
			} {
				info, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o700 {
					t.Fatalf("%s mode = %o", dir, info.Mode().Perm())
				}
			}
			info, err := os.Stat(filepath.Join(svc.ComputerDir, "computers", "tenant", ownerKey, "meta.json"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("meta mode = %o", info.Mode().Perm())
			}
		}
		stopped, err := svc.Stop(context.Background(), "tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if stopped.Status != "stopped" {
			t.Fatalf("api status = %s", stopped.Status)
		}
		meta, err = svc.ReadComputerMeta("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if meta.State != "absent" || len(meta.Screens) != 2 {
			t.Fatalf("after stop = %#v", meta)
		}
		if strings.Contains(string(mustRead(t, filepath.Join(svc.ComputerDir, "computers", "tenant", ownerKey, "meta.json"))), token) {
			t.Fatal("token appeared after stop")
		}
	})

	t.Run("file_activity_does_not_shorten_idle", func(t *testing.T) {
		if Stage2Idle != 5*time.Minute {
			t.Fatalf("idle limit = %s", Stage2Idle)
		}
		now := time.Date(2026, 10, 18, 5, 0, 0, 0, time.UTC)
		docker := &reaperDocker{}
		svc := newReaperService(docker)
		svc.ComputerDir = t.TempDir()
		svc.Stage2Sleep = false
		svc.RunCommand = func(context.Context, io.Reader, ...string) (string, int, error) {
			return "ok\n", 0, nil
		}
		svc.Now = func() time.Time { return now.Add(-10 * time.Second) }
		if _, err := svc.ReadFile(context.Background(), "tenant-a", "k1", "notes.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Bash(context.Background(), "tenant-a", "k1", "echo hi"); err != nil {
			t.Fatal(err)
		}
		svc.Now = func() time.Time { return now }
		docker.calls = nil
		svc.ReapStage2(context.Background())
		if len(docker.calls) != 0 {
			t.Fatalf("meta turned reaping on: %#v", docker.calls)
		}
		(&reaper{maxIdle: Stage2Idle}).once(context.Background(), svc)
		if docker.count("stop --time") != 0 || !docker.running["maclaw-desktop-k1"] || !docker.running["maclaw-desktop-k2"] {
			t.Fatalf("recent file activity or the short idle was reaped: %#v", docker.calls)
		}

		docker = &reaperDocker{}
		svc = newReaperService(docker)
		svc.ComputerDir = t.TempDir()
		svc.RunCommand = func(context.Context, io.Reader, ...string) (string, int, error) {
			return "ok\n", 0, nil
		}
		svc.Now = func() time.Time { return now.Add(-6 * time.Minute) }
		if _, _, _, err := svc.HTTPGet(context.Background(), "tenant-a", "k1", "https://example.com"); err != nil {
			t.Fatal(err)
		}
		stamped, err := svc.ReadComputerMeta("tenant-a", "k1")
		if err != nil || stamped.LastActivity == "" {
			t.Fatalf("http did not stamp activity: %v %#v", err, stamped)
		}
		svc.Now = func() time.Time { return now }
		(&reaper{maxIdle: Stage2Idle}).once(context.Background(), svc)
		if docker.count("stop --time") != 1 || docker.running["maclaw-desktop-k1"] || !docker.running["maclaw-desktop-k2"] {
			t.Fatalf("idle limit changed: running=%v calls=%#v", docker.running, docker.calls)
		}
	})

	t.Run("gate_activity_refreshes_idle", func(t *testing.T) {
		out := runSupervisorPython(t, `
user = "ab" * 8
os.environ["MACLAW_DESKTOP_KEY"] = user
sup.save_registry({"users": {user: {"display": 20, "token": "aa" * 16, "last_used": 1}}})
sup.desktop_running = lambda key: True
import io
from contextlib import redirect_stdout
def idle():
    buf = io.StringIO()
    with redirect_stdout(buf):
        sup.print_idle(user)
    return json.loads(buf.getvalue())
before = idle()
sup.note_gate_activity()
after = idle()
activity = sup.ROOT / user / "activity"
text = activity.read_text(encoding="utf-8") if activity.exists() else ""
print(json.dumps({
    "before": before["idle_seconds"],
    "after": after["idle_seconds"],
    "known": after["known"],
    "token_in_file": ("aa" * 16) in text,
}))
`)
		for _, want := range []string{`"known": true`, `"token_in_file": false`} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %s in %s", want, out)
			}
		}
		var parsed struct {
			Before int  `json:"before"`
			After  int  `json:"after"`
			Known  bool `json:"known"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Before < 1000 || parsed.After > 5 || !parsed.Known {
			t.Fatalf("idle before=%d after=%d known=%v", parsed.Before, parsed.After, parsed.Known)
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
