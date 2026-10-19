package desktopd

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Section 4.2. The file service answers one screen. Callers do not add a port
// to the display. Offline: no browser, no docker run.
func TestDesktopBotScreenCases(t *testing.T) {
	t.Run("screen_asked_not_guessed", func(t *testing.T) {
		assertNoChromePortFormula(t)

		ownerKey, err := desktop.UserKey("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		botKey, err := desktop.BotKey("tenant", "alice", "bot_1")
		if err != nil {
			t.Fatal(err)
		}
		token := strings.Repeat("bb", 16)
		profile := "/desktops/" + ownerKey + "/screens/21/profile"
		// Port 9 is not a display formula. The parser must keep the address
		// the file service sent and must not fill one in when it is absent.
		odd, err := desktop.ParseScreen(`{"display":":21","cdp":"http://127.0.0.1:9","profile":"` + profile + `","token":"` + token + `"}`)
		if err != nil || odd.CDP != "http://127.0.0.1:9" || odd.Display != ":21" || odd.Profile != profile || odd.Token != token {
			t.Fatalf("parse = %+v %v", odd.Display, err)
		}
		missing := `{"display":":21","profile":"` + profile + `","token":"` + token + `"}`
		if _, err := desktop.ParseScreen(missing); err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "18000") {
			t.Fatalf("missing cdp err=%v", err)
		}
		for _, bad := range []string{
			`{"display":":21","cdp":"http://127.0.0.1:18021","profile":"/desktops/` + ownerKey + `/profile","token":"` + token + `"}`,
			`{"display":":21","cdp":"http://127.0.0.1:18021","profile":"/desktops/` + ownerKey + `/screens/20/profile","token":"` + token + `"}`,
			`{"display":":21","cdp":"http://desktop:` + token + `@127.0.0.1:18021","profile":"` + profile + `","token":"` + token + `"}`,
			`{"display":":21","cdp":"http://127.0.0.1:18021","profile":"/desktops/` + ownerKey + `/screens/21/../profile","token":"` + token + `"}`,
			`{"display":":21","cdp":"http://127.0.0.1","profile":"` + profile + `","token":"` + token + `"}`,
		} {
			if _, err := desktop.ParseScreen(bad); err == nil || strings.Contains(err.Error(), token) {
				t.Fatalf("accepted %s err=%v", bad, err)
			}
		}

		answered := `{"display":":21","cdp":"http://127.0.0.1:18021","profile":"` + profile + `","token":"` + token + `"}`
		foreign := `{"display":":21","cdp":"http://127.0.0.1:18021","profile":"/desktops/0000000000000000/screens/21/profile","token":"` + token + `"}`
		mode := "ok"
		var calls []string
		root := t.TempDir()
		metaDir := filepath.Join(root, "computers", "tenant", ownerKey)
		if err := os.MkdirAll(metaDir, 0o700); err != nil {
			t.Fatal(err)
		}
		metaPath := filepath.Join(metaDir, "meta.json")
		before := []byte(`{"state":"running","runtime":"docker","snapshot_id":"","generation":0,"last_activity":"2026-10-18T04:00:00Z","screens":[{"screen":2,"display":":21","agent":"bot_1"}]}`)
		if err := os.WriteFile(metaPath, before, 0o600); err != nil {
			t.Fatal(err)
		}
		svc := &Service{
			ComputerDir: root,
			Run: func(_ context.Context, args ...string) (string, error) {
				calls = append(calls, strings.Join(args, " "))
				switch mode {
				case "ok":
					return answered, nil
				case "foreign":
					return foreign, nil
				case "missing":
					return missing, nil
				default:
					t.Fatalf("unexpected docker %s", strings.Join(args, " "))
					return "", nil
				}
			},
		}
		got, err := svc.Screen(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"})
		if err != nil || got.CDP != "http://127.0.0.1:18021" || got.Profile != profile || got.Display != ":21" || got.Token != token {
			t.Fatalf("screen = %+v %v", got.Display, err)
		}
		if len(calls) != 1 || !strings.Contains(calls[0], "exec maclaw-desktop-"+ownerKey+" python3 /desktop_supervisor.py screen "+botKey) {
			t.Fatalf("exec = %v", calls)
		}
		for _, call := range calls {
			if strings.Contains(call, " run ") || strings.Contains(call, " rm ") || strings.Contains(call, " ensure ") {
				t.Fatalf("screen changed the container: %s", call)
			}
		}
		mode = "foreign"
		if _, err := svc.Screen(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"}); err == nil || strings.Contains(err.Error(), token) {
			t.Fatalf("foreign profile err=%v", err)
		}
		mode = "missing"
		if _, err := svc.Screen(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"}); err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "18000") {
			t.Fatalf("missing cdp through screen err=%v", err)
		}
		after, err := os.ReadFile(metaPath)
		if err != nil || string(after) != string(before) || strings.Contains(string(after), token) {
			t.Fatal("meta.json changed or gained a token")
		}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), token) {
				t.Fatalf("token written to %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		out := runSupervisorPython(t, `
import io, os, sys
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
user = "ab" * 8
bot = user + "11" * 8
other = "cd" * 8
missing = "ee" * 8
os.environ["MACLAW_DESKTOP_KEY"] = user
token = "aa" * 16
bot_token = "bb" * 16
sup.save_registry({"users": {
    user: {"display": 20, "token": token, "last_used": 5},
    bot: {"display": 21, "token": bot_token, "bot": True, "last_used": 6},
}})
master = sup.master_profile(user)
master.mkdir(parents=True)
(master / "SingletonLock").write_text("held", encoding="utf-8")
before = (sup.ROOT / "registry.json").read_text(encoding="utf-8")
record = sup.screen_record(user)
assert record["display"] == ":20"
assert record["cdp"] == "http://127.0.0.1:%d" % sup.chrome_cdp_port(20)
assert Path(record["profile"]) == sup.screen_profile(user, 20)
assert record["token"] == token
assert Path(record["profile"]) != master
assert not Path(record["profile"]).exists()
assert (master / "SingletonLock").read_text(encoding="utf-8") == "held"
bot_record = sup.screen_record(bot)
assert bot_record["display"] == ":21"
assert bot_record["cdp"] == "http://127.0.0.1:%d" % sup.chrome_cdp_port(21)
assert Path(bot_record["profile"]) == sup.screen_profile(user, 21)
assert bot_record["token"] == bot_token
assert not (sup.ROOT / bot / "profile").exists()
assert sup.screen_record(other) is None
assert (sup.ROOT / "registry.json").read_text(encoding="utf-8") == before
os.environ["MACLAW_DESKTOP_KEY"] = missing
assert sup.screen_record(missing) is None
os.environ["MACLAW_DESKTOP_KEY"] = user
text = Path(sys.argv[1]).read_text(encoding="utf-8")
assert text.count("18000 +") == 1
assert "18000+" not in text
buf, err = io.StringIO(), io.StringIO()
with redirect_stdout(buf), redirect_stderr(err):
    code = sup.main(["desktop_supervisor.py", "screen", bot])
assert code == 0
assert bot_token not in err.getvalue()
line = json.loads(buf.getvalue())
assert line["cdp"] == bot_record["cdp"] and line["token"] == bot_token
assert line["display"] == ":21"
assert Path(line["profile"]) == sup.screen_profile(user, 21)
buf, err = io.StringIO(), io.StringIO()
with redirect_stdout(buf), redirect_stderr(err):
    code = sup.main(["desktop_supervisor.py", "screen", other])
assert code == 2 and buf.getvalue() == "" and "another user" in err.getvalue() and bot_token not in err.getvalue()
buf, err = io.StringIO(), io.StringIO()
with redirect_stdout(buf), redirect_stderr(err):
    os.environ["MACLAW_DESKTOP_KEY"] = missing
    code = sup.main(["desktop_supervisor.py", "screen", missing])
assert code == 1 and buf.getvalue() == "" and "never opened" in err.getvalue() and token not in err.getvalue()
print("screen_asked_not_guessed ok")
`)
		if out != "screen_asked_not_guessed ok" {
			t.Fatalf("got:\n%s", out)
		}
	})
}

func assertNoChromePortFormula(t *testing.T) {
	t.Helper()
	roots := []string{".", filepath.Join("..", "corelib", "desktop"), filepath.Join("..", "MaClawSrv")}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(raw)
			if strings.Contains(text, "18000+") || strings.Contains(text, "18000 +") {
				t.Fatalf("%s guesses the chrome port", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
