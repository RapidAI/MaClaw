package desktopd

import (
	"os/exec"
	"strings"
	"testing"
)

// The merge engine of the live login sync, driven with fake screens.
func TestLoginSyncMerge(t *testing.T) {
	out := runSupervisorPython(t, `
now = [1000.0]
data = {}
writes = []
def read(s): return [dict(c) for c in data.get(s, [])] if s in data else None
def write(s, params):
    writes.append((s, len(params)))
    cur = {(c["name"], c.get("domain") or "www.x.test"): c for c in data[s]}
    for p in params:
        dom = p.get("domain") or p["url"].split("/")[2]
        cur.pop((p["name"], dom), None)
        if p.get("expires") != 1:
            c = {k: v for k, v in p.items() if k != "url"}; c["domain"] = dom; c["session"] = "expires" not in p; cur[(p["name"], dom)] = c
    data[s] = list(cur.values())
    return True
def ck(name, value, domain=".x.test"): return {"name": name, "value": value, "domain": domain, "path": "/", "secure": True, "session": True}
def names(s): return sorted((c["name"], c["value"]) for c in data[s])
e = sup.LoginSync(read, write, now=lambda: now[0])
data[20] = [ck("a", "1"), ck("b", "1"), ck("c", "1"), ck("d", "1")]
data[21] = []
e.tick({20: 100, 21: 200})
print("seed", names(21) == names(20))
n = len(writes); e.tick({20: 100, 21: 200}); print("no echo", len(writes) == n)
now[0] += 1; data[21] = [c for c in data[21] if c["name"] != "a"]
e.tick({20: 100, 21: 200}); print("delete", names(20))
now[0] += 1; data[20] = [ck("b", "2") if c["name"] == "b" else c for c in data[20]]
data[21] = [ck("b", "3") if c["name"] == "b" else c for c in data[21]] + [ck("e", "1")]
e.tick({20: 100, 21: 200}); print("lww", names(20) == names(21))
now[0] += 1; data[21] = []
e.tick({20: 100, 21: 201}); print("pid change keeps", names(20), names(21) == names(20))
now[0] += 1; data[21] = []
e.tick({20: 100, 21: 201}); print("mass held", names(20))
now[0] += 1; e.tick({20: 100, 21: 201}); print("mass confirmed", names(20))
st = e.state(); sup.loginsync_save("ab"*8, st)
print("roundtrip", sup.loginsync_load("ab"*8)["version"] == st["version"])
p = sup.loginsync_state_path("ab"*8); raw = p.read_text(); p.write_text(raw.replace('"t": "', '"t": "A'))
print("tamper", sup.loginsync_load("ab"*8))
print(sup.loginsync_set_param({"name": "h", "value": "v", "domain": "www.x.test", "path": "/p", "secure": True, "session": True}))
`)
	want := strings.Join([]string{
		"seed True",
		"no echo True",
		"delete [('b', '1'), ('c', '1'), ('d', '1')]",
		"lww True",
		"pid change keeps [('b', '3'), ('c', '1'), ('d', '1'), ('e', '1')] True",
		"mass held [('b', '3'), ('c', '1'), ('d', '1'), ('e', '1')]",
		"mass confirmed []",
		"roundtrip True",
		"tamper None",
		"{'name': 'h', 'value': 'v', 'path': '/p', 'url': 'https://www.x.test/p', 'secure': True}",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Two real Chromes on two screens: a login set on one shows up in the other
// within five seconds without a restart, a logout comes back, kill -9 is not a
// logout, and a new screen is logged in when it is handed over.
func TestLoginSyncRealChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Chrome")
	}
	chrome := ""
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			chrome = p
			break
		}
	}
	if chrome == "" {
		t.Skip("no chromium")
	}
	out := runSupervisorPython(t, `import subprocess, threading, time, signal
owner = "ab"*8
bot1, bot2 = owner+"11"*8, owner+"22"*8
os.environ["MACLAW_DESKTOP_KEY"] = owner
sup.save_registry({"users": {owner: {"display": 20}, bot1: {"display": 21}, bot2: {"display": 22}}})
CHROME = os.environ["LS_CHROME"]
procs = {}
def start(display):
    prof = sup.screen_profile(owner, display); prof.mkdir(parents=True, exist_ok=True)
    p = subprocess.Popen([CHROME, "--headless=new", "--no-sandbox", "--disable-gpu", "--user-data-dir=%s" % prof,
        "--remote-debugging-port=%d" % sup.chrome_cdp_port(display), "--no-first-run", "about:blank"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    procs[display] = p
    for _ in range(100):
        if sup.loginsync_cdp_read(display) is not None and sup.browser_pid(prof): return
        time.sleep(0.1)
    raise SystemExit("chrome %d did not start" % display)
def cookies(display):
    return {c["name"]: c["value"] for c in (sup.loginsync_cdp_read(display) or []) if c.get("domain","").endswith("example.test")}
def setc(display, name, value, expires=None, host=False):
    c = {"name": name, "value": value, "path": "/", "httpOnly": True, "secure": True}
    if host: c["url"] = "https://www.example.test/"
    else: c["domain"] = ".example.test"
    if expires is not None: c["expires"] = expires
    assert sup.loginsync_cdp_write(display, [c])
def wait(pred, limit=5.0):
    t0 = time.time()
    while time.time() - t0 < limit:
        if pred(): return round(time.time() - t0, 1)
        time.sleep(0.1)
    return None
try:
    start(20); start(21)
    pid21 = sup.browser_pid(sup.screen_profile(owner, 21))
    threading.Thread(target=sup.run_loginsync, args=(owner,), daemon=True).start()
    time.sleep(1.5)
    setc(20, "SESSDATA", "v1", expires=int(time.time())+86400); setc(20, "tok", "h1", host=True)
    t = wait(lambda: cookies(21).get("SESSDATA") == "v1" and cookies(21).get("tok") == "h1")
    print("login propagated", t is not None, "samepid", sup.browser_pid(sup.screen_profile(owner, 21)) == pid21)
    time.sleep(2.5)
    print("stable", cookies(20) == cookies(21), sorted(cookies(20)))
    setc(21, "SESSDATA", "", expires=1)
    t = wait(lambda: "SESSDATA" not in cookies(20))
    print("logout propagated", t is not None, "tok kept", cookies(20).get("tok"))
    setc(20, "SESSDATA", "v2", expires=int(time.time())+86400)
    wait(lambda: cookies(21).get("SESSDATA") == "v2")
    procs[21].send_signal(signal.SIGKILL); procs[21].wait()
    time.sleep(4)
    print("kill -9 no logout", cookies(20).get("SESSDATA") == "v2")
    start(22)
    sup.ensure_loginsync = lambda _user: None
    sup.loginsync_on_browser_start(bot2, sup.chrome_cdp_port(22))
    print("new screen logged in at handoff", cookies(22).get("SESSDATA") == "v2", cookies(22).get("tok") == "h1")
    st = sup.loginsync_load(owner)
    print("state encrypted", b"v2" not in sup.loginsync_state_path(owner).read_bytes(), st is not None, "no echo loop", st["version"] < 12)
finally:
    for p in procs.values():
        try: p.kill(); p.wait(10)
        except Exception: pass
    pf = sup.loginsync_pidfile(owner)
    if pf.exists():
        try: os.kill(int(pf.read_text().split()[0]), 9)
        except Exception: pass
    time.sleep(1)
`, "LS_CHROME="+chrome)
	want := strings.Join([]string{
		"login propagated True samepid True",
		"stable True ['SESSDATA', 'tok']",
		"logout propagated True tok kept h1",
		"kill -9 no logout True",
		"new screen logged in at handoff True True",
		"state encrypted True True no echo loop True",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The debug port keeps its socket open after answering; the CDP lookup must
// return once the body is complete, and loginsync caches the browser URL per
// screen until a call on it fails.
func TestLoginSyncCDPLookupFast(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, threading, time
srv = socket.socket(); srv.bind(("127.0.0.1", 0)); srv.listen(8); port = srv.getsockname()[1]
hits = [0]
def serve():
    while True:
        c, _ = srv.accept(); c.recv(4096); hits[0] += 1
        body = b'{"webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/browser/x"}'
        if hits[0] == 2:
            c.sendall(b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n" % len(body) + body + b"\r\n0\r\n\r\n")
        else:
            c.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n" % len(body) + body)
        threading.Timer(5, c.close).start()  # never closes in time
threading.Thread(target=serve, daemon=True).start()
t = time.time(); v = sup.http_json("127.0.0.1", port, "/json/version"); print("content-length fast", time.time() - t < 0.5, v["webSocketDebuggerUrl"].endswith("/x"))
t = time.time(); sup.http_json("127.0.0.1", port, "/json/version"); print("chunked fast", time.time() - t < 0.5)
sup.chrome_cdp_port = lambda d: port
calls = []
class Conn:
    def __init__(self, ws): self.ws = ws
    def __enter__(self):
        calls.append(self.ws)
        if len(calls) == 3: raise OSError("chrome restarted")
        return self
    def __exit__(self, *a): return False
    def call(self, m, p): return {"cookies": [{"name": "a"}]}
sup.cdp_connection = Conn
n = hits[0]
print("read", sup.loginsync_cdp_read(20) == [{"name": "a"}], "lookups", hits[0] - n)
sup.loginsync_cdp_read(20); print("cached lookups", hits[0] - n)
print("stale retried", sup.loginsync_cdp_read(20) == [{"name": "a"}], "lookups", hits[0] - n)
print("write", sup.loginsync_cdp_write(20, [{"name": "a"}]))
`)
	want := strings.Join([]string{
		"content-length fast True True",
		"chunked fast True",
		"read True lookups 1",
		"cached lookups 1",
		"stale retried True lookups 2",
		"write True",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A container stop writes every screen back in turn. With live sync the
// master Cookies is merged (newest row per cookie, minus live-sync logouts),
// so a partial or stale last screen cannot shrink it, and a logout still
// reaches it.
func TestLoginSyncWritebackMergesMaster(t *testing.T) {
	out := runSupervisorPython(t, `
import sqlite3, time
user = "ab" * 8
os.environ["MACLAW_DESKTOP_KEY"] = user
sup.save_registry({"users": {user: {"display": 20, "token": "aa" * 16}}})
OFF = sup.CHROME_EPOCH_OFFSET
def us(t): return int((t + OFF) * 1e6)
def db(path, rows):
    path.parent.mkdir(parents=True, exist_ok=True)
    c = sqlite3.connect(str(path))
    c.execute("CREATE TABLE cookies(creation_utc INTEGER, host_key TEXT, top_frame_site_key TEXT, name TEXT, value TEXT, encrypted_value BLOB, path TEXT, last_update_utc INTEGER)")
    c.execute("CREATE UNIQUE INDEX cookies_unique_index ON cookies(host_key, top_frame_site_key, name, path)")
    for host, name, val, t in rows:
        c.execute("INSERT INTO cookies VALUES(?,?,?,?,?,?,?,?)", (us(t), host, "", name, val, b"", "/", us(t)))
    c.commit(); c.close()
def rows(path):
    c = sqlite3.connect(str(path)); r = {(h, n): v for h, n, v in c.execute("select host_key,name,value from cookies")}; c.close(); return r
now = time.time()
master = sup.master_profile(user) / "Default" / "Cookies"
db(master, [(".a.com", "sid", "a1", now - 500), (".b.com", "sid", "b1", now - 500), (".c.com", "sid", "c1", now - 500)])
s20 = sup.screen_profile(user, 20) / "Default" / "Cookies"
s21 = sup.screen_profile(user, 21) / "Default" / "Cookies"
db(s20, [(".a.com", "sid", "a2", now - 100), (".b.com", "sid", "b1", now - 500), (".d.com", "sid", "d1", now - 100)])
db(s21, [(".a.com", "sid", "a1", now - 500)])
# live sync saw a logout on b.com after its cookie was written
sup.loginsync_save(user, {"version": 3, "seen": {"20": 3, "21": 1}, "cookies": {
    "sid\t.b.com\t/\t": {"c": None, "t": now - 50},
    "sid\t.a.com\t/\t": {"c": {"name": "sid"}, "t": now - 100}}})
print("w20", sup.writeback_screen_login(user, 20))
print("w21", sup.writeback_screen_login(user, 21))
r = rows(master)
print("master", sorted(r.items()))
# a later login on b.com (newer than the tombstone) comes back
db(sup.screen_profile(user, 22) / "Default" / "Cookies", [(".b.com", "sid", "b2", now)])
print("w22", sup.writeback_screen_login(user, 22), rows(master).get((".b.com", "sid")))
`)
	for _, want := range []string{
		"w20 True",
		"w21 False",
		"master [(('.a.com', 'sid'), 'a2'), (('.c.com', 'sid'), 'c1'), (('.d.com', 'sid'), 'd1')]",
		"w22 True b2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Before the sync loop saved any state, a partial screen still cannot shrink
// the master; with sync off the old whole-file copy stays.
func TestLoginSyncWritebackWithoutState(t *testing.T) {
	out := runSupervisorPython(t, `
import sqlite3
user = "cd" * 8
os.environ["MACLAW_DESKTOP_KEY"] = user
def db(path, names):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists(): path.unlink()
    c = sqlite3.connect(str(path))
    c.execute("CREATE TABLE cookies(creation_utc INTEGER, host_key TEXT, top_frame_site_key TEXT, name TEXT, value TEXT, path TEXT, last_update_utc INTEGER)")
    c.execute("CREATE UNIQUE INDEX u ON cookies(host_key, top_frame_site_key, name, path)")
    for n in names: c.execute("INSERT INTO cookies VALUES(1,?,?,?,?,?,1)", (".x.com", "", n, "v", "/"))
    c.commit(); c.close()
def count(path):
    c = sqlite3.connect(str(path)); n = c.execute("select count(*) from cookies").fetchone()[0]; c.close(); return n
master = sup.master_profile(user) / "Default" / "Cookies"
db(master, ["a", "b", "c"])
db(sup.screen_profile(user, 20) / "Default" / "Cookies", ["a"])
sup.writeback_screen_login(user, 20)
print("shared", count(master))
(sup.ROOT / user / "login-sync").write_text("off", encoding="utf-8")
sup.writeback_screen_login(user, 20)
print("off", count(master))
`)
	for _, want := range []string{"shared 3", "off 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
