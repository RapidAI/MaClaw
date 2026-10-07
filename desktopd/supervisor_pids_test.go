package desktopd

import (
	"strings"
	"testing"
)

// fakeProcPrelude builds a /proc look-alike under tmp and points the
// supervisor at it. proc(pid, ppid, start, argv, display) adds one process.
const fakeProcPrelude = `
sup.PROC = tmp / "proc"
sup.PROC.mkdir()
def proc(pid, ppid, start, argv, display=None, state="S"):
    d = sup.PROC / str(pid)
    d.mkdir()
    fields = [state, str(ppid)] + ["0"] * 17 + [str(start)] + ["0"] * 10
    (d / "stat").write_text("%d (%s) %s\n" % (pid, argv[0].rsplit("/", 1)[-1][:15], " ".join(fields)))
    (d / "cmdline").write_bytes(b"\0".join(a.encode() for a in argv) + b"\0")
    env = ["PATH=/usr/bin"] + (["DISPLAY=:%d" % display] if display is not None else [])
    (d / "environ").write_bytes(b"\0".join(e.encode() for e in env) + b"\0")
key = "ab" * 8
`

func TestSupervisorAdoptsDesktopPartsNobodyRecorded(t *testing.T) {
	out := runSupervisorPython(t, fakeProcPrelude+`
proc(1, 0, 5000, ["/bin/sh", "-c", "hold"])
# What a cut-off first ensure leaves: X, session, VNC and gates, no pids.json.
proc(14, 1, 5100, ["Xvfb", ":20", "-screen", "0", "1440x900x24"])
proc(15, 1, 5110, ["x11vnc", "-display", ":20", "-rfbport", "5900"])
proc(16, 1, 5111, ["/usr/bin/python3", "/usr/bin/websockify", "--web", "/usr/share/novnc", "127.0.0.1:6081", "localhost:5900"])
proc(17, 1, 5112, ["/usr/bin/python3", "/desktop_supervisor.py", "gate", "6080", "6081", "tok"])
proc(18, 1, 5120, ["dbus-daemon", "--session", "--nofork", "--nopidfile", "--address=unix:path=/tmp/.maclaw-dbus-20"])
proc(19, 1, 5121, ["/bin/sh", "/usr/bin/startxfce4"], display=20)
proc(25, 19, 5122, ["xfce4-session"], display=20)
# A websockify child serving one connection, and a dead x11vnc.
proc(300, 16, 6000, ["/usr/bin/python3", "/usr/bin/websockify", "--web", "/usr/share/novnc", "127.0.0.1:6081", "localhost:5900"])
proc(301, 1, 6001, ["x11vnc", "-display", ":20", "-rfbport", "5900"], state="Z")
# Another display's X server is not this desktop.
proc(40, 1, 5200, ["Xvfb", ":21"])
# pids.json from the second start: its pids are dead or now other processes.
(sup.ROOT / key).mkdir()
(sup.ROOT / key / "pids.json").write_text(json.dumps({"xvfb": 443, "x11vnc": 301, "vnc": 17, "fluxbox": 25}))
pids = sup.current_pids(key, 20)
print(json.dumps({k: pids.get(k) for k in ("xvfb", "x11vnc", "vnc", "vgate", "dbus", "fluxbox", "proxy")}, sort_keys=True))
print(sup.vnc_pair_alive(pids))
saved = json.loads((sup.ROOT / key / "pids.json").read_text())
print(saved["boot"], saved["x11vnc"])
`)
	lines := strings.Split(out, "\n")
	want := `{"dbus": 18, "fluxbox": 25, "proxy": null, "vgate": 17, "vnc": 16, "x11vnc": 15, "xvfb": 14}`
	if len(lines) != 3 || lines[0] != want || lines[1] != "True" || lines[2] != "b5000 15" {
		t.Fatalf("adopted pids:\n%s\nwant first line %s", out, want)
	}
}

func TestSupervisorIgnoresPidsFromBeforeTheContainerRestarted(t *testing.T) {
	out := runSupervisorPython(t, fakeProcPrelude+`
proc(1, 0, 9000, ["/bin/sh", "-c", "hold"])
proc(14, 1, 9100, ["/usr/bin/some-other-process"])
(sup.ROOT / key).mkdir()
f = sup.ROOT / key / "pids.json"
f.write_text(json.dumps({"xvfb": 14, "boot": "b5000"}))
print(sup.read_pids(key))
f.write_text(json.dumps({"xvfb": 14}))
print(sup.read_pids(key))
sup.write_pids(key, {"xvfb": 14, "boot": "b1"})
print(sup.read_pids(key), json.loads(f.read_text())["boot"])
print(sup.current_pids(key, 20))
`)
	want := "{}\n{'xvfb': 14}\n{'xvfb': 14} b9000\n{}"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestBrowserProxyFlagsFollowTheContainerProxy(t *testing.T) {
	out := runSupervisorPython(t, `
print(sup.browser_proxy_flags())
`, "HTTPS_PROXY=http://user:pw@172.17.0.1:18083", "NO_PROXY=localhost,127.0.0.1", "https_proxy=", "http_proxy=", "HTTP_PROXY=", "no_proxy=")
	want := "['--proxy-server=http://172.17.0.1:18083', '--proxy-bypass-list=localhost;127.0.0.1;<local>']"
	if out != want {
		t.Fatalf("got %s want %s", out, want)
	}
	out = runSupervisorPython(t, `
print(sup.browser_proxy_flags())
`, "HTTPS_PROXY=", "HTTP_PROXY=", "https_proxy=", "http_proxy=")
	if out != "[]" {
		t.Fatalf("no proxy: got %s", out)
	}
}

func TestCDPVersionRequestCarriesThePort(t *testing.T) {
	// Chromium builds webSocketDebuggerUrl from the Host header. Without the
	// port every CDP call before a stop went to port 80 and failed.
	out := runSupervisorPython(t, `
import socket, threading
srv = socket.socket()
srv.bind(("127.0.0.1", 0))
srv.listen(1)
port = srv.getsockname()[1]
def serve():
    conn, _ = srv.accept()
    req = conn.recv(4096).decode()
    host = [l.split(": ", 1)[1] for l in req.split("\r\n") if l.lower().startswith("host:")][0]
    body = json.dumps({"webSocketDebuggerUrl": "ws://%s/devtools/browser/x" % host}).encode()
    conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n" % len(body) + body)
    conn.close()
threading.Thread(target=serve, daemon=True).start()
url = sup.http_json("127.0.0.1", port, "/json/version")["webSocketDebuggerUrl"]
print(sup.local_cdp_ws(url) == "ws://127.0.0.1:%d/devtools/browser/x" % port)
`)
	if out != "True" {
		t.Fatalf("CDP websocket URL lost its port: %s", out)
	}
}

func TestGoogleSignInCountOnDiskOnlyCountsSignInCookies(t *testing.T) {
	out := runSupervisorPython(t, `
import sqlite3
key = "ab" * 8
d = sup.ROOT / key / "profile" / "Default"
d.mkdir(parents=True)
c = sqlite3.connect(str(d / "Cookies"))
c.execute("create table cookies (host_key text, name text, encrypted_value blob)")
rows = [(".google.com", "SID"), (".google.com", "__Secure-1PSIDTS"), (".google.com", "NID"), ("accounts.google.com", "LSID"), (".example.com", "SID")]
c.executemany("insert into cookies values (?, ?, x'763130')", rows)
c.commit(); c.close()
print(sup.google_signin_on_disk(key), sup.google_signin_on_disk("cd" * 8))
`)
	if out != "2 0" {
		t.Fatalf("got %q", out)
	}
}

func TestBrowserStartTurnsOffBrowserSignIn(t *testing.T) {
	out := runSupervisorPython(t, `
prof = tmp / "profile"
(prof / "Default").mkdir(parents=True)
(prof / "Default" / "Preferences").write_text(json.dumps({"signin": {"allowed": True}, "session": {"restore_on_startup": 4}}))
sup.keep_website_login(prof)
d = json.loads((prof / "Default" / "Preferences").read_text())
print(d["signin"]["allowed"], d["signin"]["allowed_on_next_startup"], d["session"]["restore_on_startup"])
`)
	if out != "False False 1" {
		t.Fatalf("got %q", out)
	}
}
