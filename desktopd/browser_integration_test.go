package desktopd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runSupervisorPython imports image/desktop_supervisor.py as a module with
// ROOT pointed at a temporary directory and runs code against it.
func runSupervisorPython(t *testing.T, code string, env ...string) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	supervisor, err := filepath.Abs("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	prelude := `
import importlib.util, json, os, sys
from pathlib import Path
spec = importlib.util.spec_from_file_location("sup", sys.argv[1])
sup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sup)
tmp = Path(sys.argv[2])
sup.ROOT = tmp / "desktops"
sup.ROOT.mkdir()
`
	cmd := exec.Command(python, "-c", prelude+code, supervisor, t.TempDir())
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestBrowserIntegrationRoutesEveryBrowserEntryToTheSupervisedBrowser(t *testing.T) {
	out := runSupervisorPython(t, `
root = tmp / "rootfs"
(root / "etc/chromium.d").mkdir(parents=True)
(root / "etc/xdg/xfce4").mkdir(parents=True)
(root / "usr/share/applications").mkdir(parents=True)
(root / "usr/share/applications/chromium.desktop").write_text("[Desktop Entry]\n")
(root / "etc/xdg/xfce4/helpers.rc").write_text("# comment\nWebBrowser=debian-sensible-browser\nMailReader=thunderbird\n")
(root / "etc/xdg/mimeapps.list").write_text("[Added Associations]\ntext/plain=mousepad.desktop\n[Default Applications]\ntext/html=firefox.desktop\n")
sup.install_browser_integration(root)
first = {p: (root / p).read_text() for p in ("usr/bin/maclaw-browser", "etc/chromium.d/zz-maclaw-shared-browser", "etc/xdg/xfce4/helpers.rc", "etc/xdg/mimeapps.list", "usr/share/applications/mimeapps.list", "etc/chromium/policies/managed/maclaw-browser-signin.json")}
sup.install_browser_integration(root)
again = {p: (root / p).read_text() for p in first}
print(json.dumps({"same": first == again, "files": first, "mode": oct((root / "usr/bin/maclaw-browser").stat().st_mode & 0o777)}))
`)
	for _, want := range []string{
		`"same": true`,
		`"mode": "0o755"`,
		`exec python3 /desktop_supervisor.py browser`,
		`exec /usr/bin/maclaw-browser`,
		`MACLAW_SUPERVISED_BROWSER`,
		`# comment\nWebBrowser=chromium\nMailReader=thunderbird\n`,
		`text/plain=mousepad.desktop`,
		`[Default Applications]\ntext/html=chromium.desktop`,
		`x-scheme-handler/https=chromium.desktop`,
		// Browser sign-in off: with it on, Chromium without Google's API
		// keys signed the person out of Google websites on every start.
		`{\"BrowserSignin\": 0}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "firefox.desktop") || strings.Contains(out, "debian-sensible-browser") {
		t.Errorf("old default browser kept: %s", out)
	}
}

func TestBrowserIntegrationSkipsXFCEOnTheLegacyImage(t *testing.T) {
	out := runSupervisorPython(t, `
root = tmp / "rootfs"
(root / "etc/chromium.d").mkdir(parents=True)
sup.install_browser_integration(root)
print(sorted(str(p.relative_to(root)) for p in root.rglob("*") if p.is_file()))
`)
	if out != "['etc/chromium.d/zz-maclaw-shared-browser', 'etc/chromium/policies/managed/maclaw-browser-signin.json', 'usr/bin/maclaw-browser']" {
		t.Fatalf("files = %s", out)
	}
}

func TestSharedBrowserFindsThisDisplaysDesktop(t *testing.T) {
	code := `
(sup.ROOT / "registry.json").write_text(json.dumps({"users": {"aaaa": {"display": 20}, "bbbb": {"display": 21}}}))
print(sup.shared_browser_key({"DISPLAY": ":21.0"}), sup.shared_browser_key({"DISPLAY": ":20", "MACLAW_DESKTOP_KEY": "aaaa"}),
      sup.shared_browser_key({"DISPLAY": ":20", "MACLAW_DESKTOP_KEY": "cccc"}), repr(sup.shared_browser_key({"DISPLAY": ":30"})),
      repr(sup.shared_browser_key({})))
`
	if out := runSupervisorPython(t, code); out != "bbbb aaaa aaaa '' ''" {
		t.Fatalf("keys = %s", out)
	}
}

func TestWatchKeepsTheScreenFromBlanking(t *testing.T) {
	out := runSupervisorPython(t, `
calls = []
def fake_call(argv, **kwargs):
    calls.append((list(argv), (kwargs.get("env") or {}).get("DISPLAY")))
    return 0
sup.subprocess.call = fake_call
sup.shutil.which = lambda name: "/usr/bin/" + name if name == "xset" else None
sup.keep_screen_awake(20)
sup.shutil.which = lambda name: None
sup.keep_screen_awake(20)
print("\n".join("%s %s" % (" ".join(argv), display) for argv, display in calls))
print("calls", len(calls))
`)
	for _, want := range []string{"xset s off :20", "xset s noblank :20", "xset s reset :20", "xset -dpms :20"} {
		if !strings.Contains(out, want) {
			t.Fatalf("screen saver command %s missing from %s", want, out)
		}
	}
	if !strings.Contains(out, "calls 4") {
		t.Fatalf("missing xset still tried to change the display: %s", out)
	}
	raw, err := os.ReadFile("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := text[strings.Index(text, "def start_desktop("):]
	start = start[:strings.Index(start, "\ndef ")]
	xvfb := strings.Index(start, `"Xvfb"`)
	awake := strings.Index(start, "keep_screen_awake(")
	if xvfb < 0 || awake < 0 || awake < xvfb || !strings.Contains(start, `"-s", "0"`) {
		t.Fatal("a new display still starts with the ten-minute blank")
	}
	watch := text[strings.Index(text, "def watch_vnc("):]
	watch = watch[:strings.Index(watch, "\ndef ")]
	if !strings.Contains(watch, "keep_screen_awake(") {
		t.Fatal("the watcher does not keep the screen awake")
	}
}

func TestClosedBrowserDoesNotStartASecondDesktopSession(t *testing.T) {
	code := `
me = os.getpid()
print(sup.session_alive({"xvfb": me, "fluxbox": me}, 987), sup.session_alive({"xvfb": 0, "fluxbox": me}, 987))
`
	if out := runSupervisorPython(t, code); out != "False False" {
		t.Fatalf("session_alive without an X socket = %s", out)
	}
	raw, err := os.ReadFile("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := text[strings.Index(text, "def start_desktop("):]
	start = start[:strings.Index(start, "\ndef ")]
	reuse := strings.Index(start, "session_alive(")
	xvfb := strings.Index(start, `"Xvfb"`)
	if reuse < 0 || xvfb < 0 || reuse > xvfb || !strings.Contains(start, "restart_browser(") {
		t.Fatal("a closed browser restarts the whole desktop session")
	}
	if !strings.Contains(text, `env[SUPERVISED_BROWSER_ENV] = "1"`) {
		t.Fatal("the supervisor's own browser start is routed back to maclaw-browser")
	}
}

func TestChromiumHookLetsTheSupervisorAndExplicitProfilesThrough(t *testing.T) {
	out := runSupervisorPython(t, `print(sup.CHROMIUM_HOOK)`)
	hook := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(hook, []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// /usr/bin/maclaw-browser does not exist on the test machine either, so
	// every case must fall through; the cases check the guards parse.
	for _, tc := range []struct {
		env  []string
		args string
	}{
		{[]string{"MACLAW_SUPERVISED_BROWSER=1", "DISPLAY=:20"}, ""},
		{[]string{"DISPLAY=:20"}, "--user-data-dir=/tmp/x"},
		{[]string{"DISPLAY="}, "https://example.com"},
	} {
		cmd := exec.Command("sh", "-c", `. "$0"; echo through`, hook)
		if tc.args != "" {
			cmd = exec.Command("sh", "-c", `set -- "$1"; . "$0"; echo through`, hook, tc.args)
		}
		cmd.Env = append(os.Environ(), tc.env...)
		got, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(got)) != "through" {
			t.Fatalf("%v %q: %v %s", tc.env, tc.args, err, got)
		}
	}
}

func TestPanelLauncherRaisesAMinimizedBrowser(t *testing.T) {
	raw, err := os.ReadFile("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	// xdotool --onlyvisible skips minimized windows, so the launcher would
	// open a second window instead of bringing the person's one back.
	if !strings.Contains(string(raw), `"_NET_CLIENT_LIST_STACKING"`) {
		t.Fatal("browser windows are not taken from the window manager's client list")
	}
}
