package desktopd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Section 4.2. The control plane asks the in-container command service to
// run a shell, a screenshot, a click, and ctl. Offline: no browser, no docker run.
func TestDesktopBotCommandCases(t *testing.T) {
	t.Run("command_service_inside_the_container", func(t *testing.T) {
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
		var shells [][]string
		bashSvc := &Service{RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
			shells = append(shells, append([]string(nil), args...))
			return "hi\n", 0, nil
		}}
		text, err = bashSvc.Bash(context.Background(), "tenant", "alice", "echo hi")
		if err != nil || text != "hi" {
			t.Fatalf("bash=%q err=%v", text, err)
		}
		shell := commandPayload(t, shells[0])
		if shells[0][1] != name || shell.Op != "shell" || shell.Command != "echo hi" || shell.Display != "" {
			t.Fatalf("shell=%+v args=%v", shell, shells[0])
		}
		for _, arg := range shells[0] {
			switch arg {
			case "bash", "sh", "xdotool":
				t.Fatalf("host built %s: %v", arg, shells[0])
			}
		}
		if _, err := bashSvc.Bash(context.Background(), "tenant", "alice", "echo\x00hi"); err == nil || len(shells) != 1 {
			t.Fatalf("nul command calls=%d err=%v", len(shells), err)
		}

		botShells := [][]string{}
		bot := &Service{
			Run: func(_ context.Context, args ...string) (string, error) {
				if strings.Contains(strings.Join(args, " "), " display ") {
					return `{"display":":21","running":true}`, nil
				}
				t.Fatalf("bot docker %v", args)
				return "", nil
			},
			RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
				botShells = append(botShells, append([]string(nil), args...))
				return "hi\n", 0, nil
			},
		}
		if _, err := bot.Bash(context.Background(), "tenant", "alice::desk::bot_1", "echo hi"); err != nil {
			t.Fatal(err)
		}
		botReq := commandPayload(t, botShells[0])
		if botReq.Display != ":21" || strings.Contains(strings.Join(botShells[0], " "), "DISPLAY=") {
			t.Fatalf("bot shell=%+v args=%v", botReq, botShells[0])
		}

		var clicks [][]string
		appSvc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			clicks = append(clicks, append([]string(nil), args...))
			return "ok", nil
		}}
		if _, err := appSvc.App(context.Background(), "tenant", "alice", ":20", []string{"key", "Return"}); err != nil {
			t.Fatal(err)
		}
		click := commandPayload(t, clicks[0])
		if click.Op != "click" || click.Display != ":20" || strings.Join(click.Args, " ") != "key Return" {
			t.Fatalf("click=%+v", click)
		}
		before := len(clicks)
		if _, err := appSvc.App(context.Background(), "tenant", "alice", ":20", []string{"type", "a\nb"}); err == nil || len(clicks) != before {
			t.Fatalf("newline click calls=%d err=%v", len(clicks)-before, err)
		}

		var shots [][]string
		shotSvc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			shots = append(shots, append([]string(nil), args...))
			return "abc\n", nil
		}}
		// DecodeShot rejects a non-image. The exec itself is what this check records.
		_, _, _ = shotSvc.CaptureShot(context.Background(), "tenant", "alice", ":20", "", desktop.ShotOptions{})
		shot := commandPayload(t, shots[0])
		if shots[0][1] != name || shot.Op != "shot" || shot.Display != ":20" || !strings.Contains(shot.Script, "import -window root") || !strings.Contains(shot.Script, "scrot") {
			t.Fatalf("shot=%+v args=%v", shot, shots[0])
		}
		for _, arg := range shots[0] {
			if arg == "sh" || arg == "-c" {
				t.Fatalf("shot script is still a docker arg: %v", shots[0])
			}
		}

		var ctls [][]string
		ctlSvc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			ctls = append(ctls, append([]string(nil), args...))
			return `{"ok":true}`, nil
		}}
		ctlOut, err := ctlSvc.Ctl(context.Background(), "tenant", "alice", ":20", map[string]any{"op": "screen"})
		if err != nil || ctlOut["ok"] != true {
			t.Fatalf("ctl=%v err=%v", ctlOut, err)
		}
		ctl := commandPayload(t, ctls[0])
		decoded, decErr := base64.StdEncoding.DecodeString(ctl.Body)
		if decErr != nil || ctl.Op != "ctl" || ctl.Display != ":20" || !strings.Contains(string(decoded), `"op":"screen"`) {
			t.Fatalf("ctl=%+v body=%s err=%v", ctl, decoded, decErr)
		}

		var opened []string
		openSvc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
			opened = append([]string(nil), args...)
			return "", nil
		}}
		if _, err := openSvc.Open(context.Background(), "tenant", "alice", ":20", "xterm", nil); err != nil {
			t.Fatal(err)
		}
		if opened[0] != "exec" || opened[1] != "-d" || !strings.Contains(strings.Join(opened, " "), "xterm") || strings.Contains(strings.Join(opened, " "), " command ") {
			t.Fatalf("open changed: %v", opened)
		}

		out := runSupervisorPython(t, `
import base64
def enc(obj):
    return base64.b64encode(json.dumps(obj).encode()).decode()
shell = sup.command_plan(sup.command_request(enc({"op": "shell", "command": "echo hi"})))
print("shell " + " ".join(shell["argv"]))
print("home " + shell["env"]["HOME"])
print("cwd " + (shell["cwd"] or ""))
print("nodisplay " + ("1" if "DISPLAY" not in shell["env"] else "0"))
shown = sup.command_plan(sup.command_request(enc({"op": "shell", "command": "echo hi", "display": ":21"})))
print("display " + shown["env"].get("DISPLAY", ""))
click = sup.command_plan(sup.command_request(enc({"op": "click", "display": ":20", "args": ["mousemove", "10", "10", "click", "1"]})))
print("click " + " ".join(click["argv"]))
print("clickdisplay " + click["env"]["DISPLAY"])
try:
    sup.command_plan(sup.command_request(enc({"op": "click", "display": ":20", "args": ["type", "a\nb"]})))
    print("newline accepted")
except ValueError:
    print("newline rejected")
shot = sup.command_plan(sup.command_request(enc({"op": "shot", "display": ":20", "script": "import -window root"})))
print("shot0 " + shot["argv"][0])
print("shot1 " + shot["argv"][1])
print("shot2 " + shot["argv"][2])
ctl = sup.command_plan(sup.command_request(enc({"op": "ctl", "display": ":20", "body": "aaaa"})))
print("ctl " + ctl["op"])
print("ctldisplay " + ctl["display"])
print("bad " + str(sup.run_command(["py", "command"])))
print("badop " + str(sup.run_command(["py", "command", enc({"op": "firecracker"})])))
print("registry " + ("1" if (sup.ROOT / "registry.json").exists() else "0"))
`)
		for _, want := range []string{
			"shell bash -c echo hi",
			"home /home/desktop",
			"cwd /home/desktop",
			"nodisplay 1",
			"display :21",
			"click xdotool mousemove 10 10 click 1",
			"clickdisplay :20",
			"newline rejected",
			"shot0 sh",
			"shot1 -c",
			"shot2 import -window root",
			"ctl ctl",
			"ctldisplay :20",
			"bad 2",
			"badop 2",
			"registry 0",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("supervisor missing %q\n%s", want, out)
			}
		}
	})
}

func commandPayload(t *testing.T, args []string) commandRequest {
	t.Helper()
	if len(args) != 6 || args[0] != "exec" || args[2] != "python3" || args[3] != "/desktop_supervisor.py" || args[4] != "command" {
		t.Fatalf("command args=%v", args)
	}
	raw, err := base64.StdEncoding.DecodeString(args[5])
	if err != nil {
		t.Fatalf("command payload: %v", err)
	}
	var req commandRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if args[1] == "" {
		t.Fatal("command is missing its container")
	}
	return req
}
