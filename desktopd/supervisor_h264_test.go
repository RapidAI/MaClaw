package desktopd

import (
	"strings"
	"testing"
)

// The H.264 view stream is on unless MACLAW_DESKTOP_H264 is off/0/false/no, and
// only answers /h264 and /h264.html on the noVNC gate.
func TestSupervisorH264IsBehindItsFlag(t *testing.T) {
	out := runSupervisorPython(t, `
head = b"GET /h264 HTTP/1.1\r\nHost: x\r\n\r\n"
print(sup.h264_requested(head, {"MACLAW_DESKTOP_H264": "off"}), sup.h264_requested(head, {}))
print([sup.h264_enabled({"MACLAW_DESKTOP_H264": v}) for v in ("", "on", "1", "off", "0", "false", "no", " OFF ")])
print(sup.h264_requested(b"GET /vnc.html HTTP/1.1\r\n\r\n", {"MACLAW_DESKTOP_H264": "on"}), sup.h264_requested(b"GET /v1/desktops/tunnel/k/vnc/h264.html HTTP/1.1\r\n\r\n", {"MACLAW_DESKTOP_H264": "on"}))
print(sup.ws_accept_key("dGhlIHNhbXBsZSBub25jZQ=="))
`)
	want := "False True\n[True, True, True, False, False, False, False, False]\nFalse True\ns3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The gate token picks the screen: a bot's token its display, the owner's
// token the owner display, anything else nothing.
func TestSupervisorH264TokenSelectsDisplay(t *testing.T) {
	out := runSupervisorPython(t, `
bot = "ab" * 8 + "cd" * 8
(sup.ROOT / "registry.json").write_text(json.dumps({"users": {bot: {"token": "b0" * 16, "display": 23}}}))
print(sup.h264_display("b0" * 16, "aa" * 16), sup.h264_display("aa" * 16, "aa" * 16), sup.h264_display("ff" * 16, "aa" * 16), sup.h264_display("", "aa" * 16))
`)
	if out != "23 20 None None" {
		t.Fatalf("got %q", out)
	}
}

// The owner's own registry entry (ensure_gates writes its gate token there)
// still selects the owner screen with input rights; a bot token does not.
func TestSupervisorH264OwnerRegistryEntryKeepsInput(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
bot = "ab" * 8 + "cd" * 8
(sup.ROOT / "registry.json").write_text(json.dumps({"users": {"ab" * 8: {"token": "aa" * 16, "display": 20}, bot: {"token": "b0" * 16, "display": 23}}}))
o, b = sup.h264_screen("aa" * 16, "aa" * 16), sup.h264_screen("b0" * 16, "aa" * 16)
print(o[0], o[1] == "ab" * 8, o[2], b[0], b[1] == bot, b[2])
`)
	if out != "20 True True 23 True False" {
		t.Fatalf("got %q", out)
	}
}

// End to end over a socket pair with a stand-in ffmpeg: a wrong token is
// refused, the player page is served, and the stream upgrades, says hello
// and relays the encoder's bytes. ?mse=1 keeps the raw fragmented MP4.
func TestSupervisorH264StreamsEncoderOutput(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, stat
bin_dir = tmp / "bin"
bin_dir.mkdir()
fake = bin_dir / "ffmpeg"
fake.write_text("#!/bin/sh\nprintf 'FMP4' \n")
fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
os.environ["PATH"] = str(bin_dir) + os.pathsep + os.environ.get("PATH", "")
owner = "aa" * 16

def roundtrip(head):
    a, b = socket.socketpair()
    sup.serve_h264(a, head, owner)
    data = b""
    b.settimeout(5)
    while True:
        chunk = b.recv(65536)
        if not chunk:
            break
        data += chunk
    return data

print(roundtrip(b"GET /h264.html HTTP/1.1\r\nAuthorization: Bearer " + b"ff" * 16 + b"\r\n\r\n").split(b"\r\n")[0].decode())
page = roundtrip(b"GET /h264.html HTTP/1.1\r\nAuthorization: Bearer " + owner.encode() + b"\r\n\r\n")
print(page.split(b"\r\n")[0].decode(), b"VideoDecoder" in page and b"MediaSource" in page and b"optimizeForLatency" in page)
stream = roundtrip(b"GET /h264?mse=1 HTTP/1.1\r\nAuthorization: Bearer " + owner.encode() + b"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
head, _, body = stream.partition(b"\r\n\r\n")
print(head.split(b"\r\n")[0].decode(), body[0] == 0x81, b'"mode":"fmp4"' in body, body.endswith(bytes([0x82, 4]) + b"FMP4") or (bytes([0x82, 4]) + b"FMP4") in body)
`)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "403") || lines[1] != "HTTP/1.1 200 OK True" || lines[2] != "HTTP/1.1 101 Switching Protocols True True True" {
		t.Fatalf("got:\n%s", out)
	}
}

// The encoder's FLV becomes Annex B access units with exact frame
// boundaries: SPS/PPS go in front of each IDR, deltas pass alone, and a
// frame split across reads comes out once complete.
func TestSupervisorH264FlvToAnnexB(t *testing.T) {
	out := runSupervisorPython(t, `
import struct
sps = bytes([0x67, 0x42, 0xC0, 0x1F, 0xAA])
pps = bytes([0x68, 0xCE, 0x3C])
avcc = bytes([1, 0x42, 0xC0, 0x1F, 0xFF, 0xE1]) + struct.pack(">H", len(sps)) + sps + bytes([1]) + struct.pack(">H", len(pps)) + pps
def tag(kind, body):
    return bytes([kind]) + len(body).to_bytes(3, "big") + bytes(7) + body + struct.pack(">I", 11 + len(body))
def video(key, packet, payload):
    return tag(9, bytes([(0x10 if key else 0x20) | 7, packet, 0, 0, 0]) + payload)
def nals(*items):
    return b"".join(struct.pack(">I", len(n)) + n for n in items)
stream = b"FLV\x01\x01" + struct.pack(">I", 9) + bytes(4) + tag(18, b"meta") + video(True, 0, avcc)
idr = video(True, 1, nals(bytes([0x65, 1, 2, 3])))
delta = video(False, 1, nals(bytes([0x41, 9]), bytes([0x41, 8])))
s = sup.FlvToAnnexB()
frames = s.feed(stream + idr[:7])
print(s.codec, len(frames))
frames = s.feed(idr[7:] + delta)
start = b"\x00\x00\x00\x01"
print(frames[0] == (start + sps + start + pps + start + bytes([0x65, 1, 2, 3]), True))
print(frames[1] == (start + bytes([0x41, 9]) + start + bytes([0x41, 8]), False))
msg = sup.h264_video_message({"seq": 1}, b"xy")
print(msg.hex())
`)
	want := "avc1.42C01F 0\nTrue\nTrue\n00000009000000027b22736571223a317d7879"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A viewer that falls behind drops deltas until the next IDR, and an IDR
// clears whatever was still queued.
func TestSupervisorH264DropsToNextKeyframe(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
a, b = socket.socketpair()
v = sup.H264Viewer(a, 20, "", True)
v.offer(b"k", True)
for i in range(5):
    v.offer(b"d%d" % i, False)
print([f for _, f, _ in v.queue], v.need_idr)
v.offer(b"k2", True)
print([f for _, f, _ in v.queue], v.need_idr)
v.queue = []
v.sent, v.acked = 30, 10
v.offer(b"late", False)
print([f for _, f, _ in v.queue], v.need_idr)
`)
	want := "[b'k'] True\n[b'k2'] False\n[] True"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Input is accepted only on the owner's screen or a bot's screen under
// takeover; sequences must be contiguous; the flag-off player URL lands on
// noVNC, and only a sibling vnc.html is a redirect target.
func TestSupervisorH264InputPolicy(t *testing.T) {
	out := runSupervisorPython(t, `
bot = "ab" * 8 + "cd" * 8
print(sup.h264_input_allowed("", True), sup.h264_input_allowed(bot, False))
(sup.ROOT / bot).mkdir()
(sup.ROOT / bot / "takeover.json").write_text("{}")
print(sup.h264_input_allowed(bot, False))
print(repr(sup.h264_fallback_location(b"GET /p/h264.html?view_only=0&vnc=vnc.html%3Fautoconnect%3D1%26path%3Dx HTTP/1.1\r\n\r\n")))
print(repr(sup.h264_fallback_location(b"GET /h264.html?vnc=https://evil/x HTTP/1.1\r\n\r\n")), repr(sup.h264_fallback_location(b"GET /h264.html?vnc=vnc.html//evil HTTP/1.1\r\n\r\n")))
import socket
a, b = socket.socketpair()
v = sup.H264Viewer(a, 20, bot, False)
sent = []
v.send_json = sent.append
class Fake:
    calls = []
    def apply(self, viewer, events):
        self.calls.append(events); return ""
    def release(self, viewer): pass
    def holder_now(self): return None
v.screen = Fake()
v.handle({"type": "input", "seq": 7, "events": [{"t": "move", "x": 0, "y": 0}]})
v.handle({"type": "input", "seq": 9, "events": []})
v.handle({"type": "input", "seq": 8, "events": []})
print(len(Fake.calls), [m.get("code") or m.get("type") for m in sent])
(sup.ROOT / bot / "takeover.json").unlink()
v.handle({"type": "input", "seq": 9, "events": []})
print(len(Fake.calls), sent[-1]["code"])
`)
	want := "True False\nTrue\n'vnc.html?autoconnect=1&path=x'\n'' ''\n2 ['lease', 'sequence_gap']\n2 view_only"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// bot_state reports the input lease: the bot takes it when it asks to act,
// the person holds it during takeover or right after typing.
func TestSupervisorBotStateLease(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
bot = "ab" * 8 + "cd" * 8
sup.desktop_running = lambda key: True
s = sup.ctl_bot_state({"key": bot})
print(s["lease"], s["yield"], sup.bot_file_age(bot, "bot_input") is not None)
sup.touch_bot_file(bot, "human_input", force=True)
s = sup.ctl_bot_state({"key": bot})
print(s["lease"], s["yield"], s["lease_ttl"])
`)
	if out != "bot False True\nhuman True 5" {
		t.Fatalf("got:\n%s", out)
	}
}

// The app's 人类接管 opens h264.html?view_only=0 without calling takeover_begin.
// That player (?input=1 on the socket) holds a takeover on the bot's screen
// while it is connected: input is accepted and bot_state tells the bot to
// yield. A watch-only player does not, and the hold lapses once it leaves.
func TestSupervisorH264InteractiveViewerTakesOver(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
bot = "ab" * 8 + "cd" * 8
sup.desktop_running = lambda key: True
import socket
a, b = socket.socketpair()
w = sup.H264Viewer(a, 23, bot, False)
print(w.input_allowed(), sup.ctl_bot_state({"key": bot})["lease"])
c, d = socket.socketpair()
v = sup.H264Viewer(c, 23, bot, False, want_input=True)
s = sup.ctl_bot_state({"key": bot})
print(v.input_allowed(), s["lease"], s["takeover"], s["reason"], sup.bot_busy(bot))
o = sup.H264Viewer(socket.socketpair()[0], 20, "ab" * 8, True, want_input=True)
print(o.takes_over)
old = sup.time.time() - sup.VIEWER_TAKEOVER_SECONDS - 1
os.utime(sup.ROOT / bot / "viewer_takeover", (old, old))
print(v.input_allowed(), sup.takeover_state(bot))
`)
	if out != "False bot\nTrue human True 人类接管 True\nFalse\nFalse None" {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSupervisorH264EncoderArgs(t *testing.T) {
	out := runSupervisorPython(t, `
argv = sup.h264_ffmpeg_argv(21, "1440x900")
print(argv[argv.index("-i") + 1], "mpdecimate" in argv[argv.index("-vf") + 1], argv[argv.index("-fps_mode") + 1], argv[argv.index("-profile:v") + 1], argv[-2], sup.h264_ffmpeg_argv(21, "1440x900", 15, True)[-2])
`)
	if out != ":21 True vfr baseline flv mp4" {
		t.Fatalf("got %q", out)
	}
}

// With the flag explicitly off the gate answers the new player URL with a redirect to
// the noVNC page the GUI named, so the picture still opens.
func TestSupervisorH264FlagOffRedirectsToVNC(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
os.environ["MACLAW_DESKTOP_H264"] = "off"
a, b = socket.socketpair()
b.sendall(b"GET /api/v1/desktop-handoff/t/h264.html?view_only=0&vnc=vnc.html%3Fautoconnect%3D1 HTTP/1.1\r\nHost: x\r\n\r\n")
sup.gate_connection(a, sup.VNC_GATE_PORT, 1, "aa" * 16)
reply = b.recv(4096).decode()
print(reply.split("\r\n")[0], "Location: vnc.html?autoconnect=1" in reply)
`)
	if out != "HTTP/1.1 302 Found True" {
		t.Fatalf("got %q", out)
	}
}

// verify-after-act reads the deepest focused field in an active non-browser
// window. A button label, an accessible name, a panel, and a background
// window are not that field. Chromium staying active does not hide a
// focused native field. A long buffer is read around the caret.
func TestSupervisorA11yFocusedText(t *testing.T) {
	out := runSupervisorPython(t, `
class Ext:
    x = y = 0
    width = height = 100
class Comp:
    def getExtents(self, coord): return Ext()
class St:
    def __init__(self, flags): self.flags = flags
    def contains(self, s): return s in self.flags
class T:
    def __init__(self, text):
        self.text, self.characterCount, self.caretOffset = text, len(text), len(text)
    def getText(self, a, b): return self.text[a:b]
class N:
    def __init__(self, flags, kids=(), name="", text=None, role="window"):
        self.flags, self.kids, self.name, self.text, self.role = set(flags), list(kids), name, text, role
    def getState(self):
        flags = set(self.flags)
        if "H" not in flags:
            flags |= {"S", "V"}
        return St(flags)
    @property
    def childCount(self): return len(self.kids)
    def getChildAtIndex(self, i): return self.kids[i]
    def queryText(self):
        if self.text is None: raise RuntimeError("no text")
        return T(self.text)
    def queryComponent(self): return Comp()
    def getRoleName(self): return self.role
class App:
    def __init__(self, name, wins): self.name, self.wins = name, list(wins)
    @property
    def childCount(self): return len(self.wins)
    def getChildAtIndex(self, i): return self.wins[i]
class Desk:
    def __init__(self, apps): self.apps = list(apps)
    @property
    def childCount(self): return len(self.apps)
    def getChildAtIndex(self, i): return self.apps[i]
class P:
    STATE_FOCUSED, STATE_ACTIVE = "F", "A"
    STATE_SHOWING, STATE_VISIBLE, STATE_EDITABLE = "S", "V", "E"
    DESKTOP_COORDS = 0
    class Registry:
        desktop = None
        @staticmethod
        def getDesktop(i): return P.Registry.desktop
def desk(*apps):
    P.Registry.desktop = Desk(apps)
    return sup.a11y_focused_text({}, P)
show = {"A", "S", "V"}
button = N({"F"}, name="Save", text="Save", role="push button")
entry = N({"F", "E"}, [button], text="hello", role="entry")
panel = N({"F", "E"}, [entry], text="Title", role="panel")
print(desk(App("gedit", [N(show, [panel])])))
print(desk(App("gedit", [N(show, [N({"F", "E"}, name="Save", role="entry")])])))
print(desk(App("gedit", [N(show, [N({"F"}, text="Save", role="push button")])])))
gwin = N(show, [N({"F", "E"}, text="hello", role="entry")])
print(desk(App("gedit", [gwin]), App("Chromium", [N(show)])))
print(desk(App("gedit", [N({"S", "V"}, [N({"F", "E"}, text="hello", role="entry")])])))
print(desk(App("xfce4-terminal", [N(show, [N({"F"}, text="hello", role="terminal")])])))
print(desk(App("gedit", [N(show, [N({"E"}, text="hello", role="entry")])])))
print(desk(App("gedit", [N(show, [N({"F", "E"}, [N({"F"}, text="Save", role="push button")], text="Title", role="panel")])])))
print(desk(App("gedit", [gwin]), App("mousepad", [N(show, [N({"F", "E"}, text="other", role="entry")])])))
print(desk(App("gedit", [N(show, [N({"F", "E"}, [N({"F", "E", "H"}, text="secret", role="entry")], text="hello", role="entry")])])))
print(desk(App("gedit", [N(show, [N({"F", "E"}, [N({"F", "E"}, role="entry")], text="hello already", role="text")])])))
print(desk(App("gedit", [N(show, [N({"F", "E"}, text="secret", role="password text")])])))
class Long:
    def __init__(self):
        self.body = ("x" * 60000) + "hello" + ("y" * 60000)
        self.characterCount = len(self.body)
        self.caretOffset = 60005
    def queryText(self): return self
    def getText(self, a, b): return self.body[a:b]
print("hello" in sup.a11y_node_value(Long()))
`)
	want := "{'ok': True, 'focused_text': 'hello', 'role': 'entry'}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_text': 'hello', 'role': 'entry'}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_text': 'hello', 'role': 'terminal'}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_texts': ['hello', 'other']}\n{'ok': True, 'focused_text': 'hello', 'role': 'entry'}\n{'ok': True, 'focused_text': ''}\n{'ok': True, 'focused_text': ''}\nTrue"
	if out != want {
		t.Fatalf("got:\n%s", out)
	}
}

// Structured desktop events go to stderr and /desktops/events.jsonl with the
// bot part of the key split out.
func TestSupervisorLogEvent(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
sup.log_event("display_start", key="ab" * 8 + "cd" * 8, display=23, kind="bot", skip=None)
rec = json.loads((sup.ROOT / "events.jsonl").read_text().splitlines()[-1])
print(rec["event"], rec["display"], rec["bot"], "skip" in rec, "ts" in rec)
`)
	if out != "display_start 23 "+strings.Repeat("cd", 8)+" False True" {
		t.Fatalf("got %q", out)
	}
}

// ctl bot_state with MaClawSrv ids tags later events with srv_bot / srv_run.
func TestSupervisorLogEventSrvIDs(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
key = "ab" * 8 + "cd" * 8
sup.note_srv_ids(key, {"srv_bot": "bot_625eda1bbe86a157", "srv_run": "run_x-1", "op": "bot_state"})
sup.note_srv_ids(key, {"srv_bot": "bad id; rm", "srv_run": ""})
sup.log_event("lease_change", key=key, display=30, holder="bot")
rec = json.loads((sup.ROOT / "events.jsonl").read_text().splitlines()[-1])
print(rec["bot"], rec["srv_bot"], rec["srv_run"])
`)
	if out != strings.Repeat("cd", 8)+" bot_625eda1bbe86a157 run_x-1" {
		t.Fatalf("got %q", out)
	}
}

// restore_page_login records how long a start waited for it.
func TestSupervisorPageLoginRestoreTimed(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
key = "ab" * 8 + "cd" * 8
sup._restore_page_login = lambda k: sup.time.sleep(0.6)
sup.restore_page_login(key)
rec = json.loads((sup.ROOT / "events.jsonl").read_text().splitlines()[-1])
print(rec["event"], rec["seconds"] >= 0.5)
`)
	if out != "page_login_restore True" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorHealsHiddenBrowserWindow(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
key = "ab" * 8 + "cd" * 8
calls = []
state = {"map": "IsUnMapped"}
def xdo(display, *args, timeout=3):
    calls.append(args[0])
    if args[0] == "search": return 0, "111\n222\n"
    if args[0] == "getwindowname": return 0, "知乎 - Chromium\n"
    if args[0] == "windowmap": state["map"] = "IsViewable"
    return 0, ""
sup._xdo = xdo
sup._window_state = lambda d, w: (state["map"], 1280 if w == "222" else 10)
sup.shutil.which = lambda n: "/usr/bin/" + n
real_exists = sup.Path.exists
sup.Path.exists = lambda self: True if str(self).startswith("/tmp/.X11-unix") else real_exists(self)
first = sup.heal_browser_window(key, 30, "watch", wait_ticks=1)
second = sup.heal_browser_window(key, 30, "watch", wait_ticks=1)
third = sup.heal_browser_window(key, 30, "watch", wait_ticks=1)
rec = json.loads((sup.ROOT / "events.jsonl").read_text().splitlines()[-1])
print(first, second, third, rec["event"], rec["source"], "windowactivate" in calls)
`)
	if out != "False True False window_restored watch True" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorPageLoginRestoreRunsInBackground(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_DESKTOP_KEY"] = "ab" * 8
key = "ab" * 8 + "cd" * 8
(sup.ROOT / key).mkdir(parents=True, exist_ok=True)
sup.page_login_path(key).write_text("[]")
started = []
class P:
    def __init__(self, argv, **kw): started.append(argv[-2:])
sup.subprocess.Popen = P
sup.restore_page_login_background(key)
pending = sup.restore_pending(key)
os.environ["MACLAW_RESTORE_WAIT_SECONDS"] = "1"
t0 = sup.time.time(); sup.wait_restore_done(key); waited = sup.time.time() - t0
sup.run_restore_login(key)
print(started[0], pending, waited >= 0.9, sup.restore_pending(key))
`)
	if out != "['restore-login', '"+strings.Repeat("ab", 8)+strings.Repeat("cd", 8)+"'] True True False" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorPageLoginSiteBounded(t *testing.T) {
	out := runSupervisorPython(t, `
os.environ["MACLAW_PAGE_LOGIN_SITE_SECONDS"] = "1"
sup.apply_page_login = lambda k, p, i, r: sup.time.sleep(3) or True
t0 = sup.time.time()
print(sup._apply_page_login_bounded("k", [], {}, set()), sup.time.time() - t0 < 2)
`)
	if out != "(False, False) True" {
		t.Fatalf("got %q", out)
	}
}
