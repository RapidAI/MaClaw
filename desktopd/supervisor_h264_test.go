package desktopd

import (
	"runtime"
	"strings"
	"testing"
)

// The H.264 view stream is on unless MACLAW_DESKTOP_H264 is off/0/false/no, and
// only answers /h264 and /h264.html on the noVNC gate.
func TestSupervisorGateBoundsConnectionThreads(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, threading, time
sup._gate_connections = threading.BoundedSemaphore(1)
release = threading.Event()
sup.gate_connection = lambda *args: release.wait(1)
a, b = socket.socketpair()
print(sup.start_gate_connection(a, 6080, 6081, "token"))
print(sup.start_gate_connection(b, 6080, 6081, "token"))
release.set()
time.sleep(0.1)
print(sup.start_gate_connection(b, 6080, 6081, "token"))
a.close()
b.close()
`)
	if out != "True\nFalse\nTrue" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorInputRejectsNonFiniteCoordinates(t *testing.T) {
	out := runSupervisorPython(t, `
s = object.__new__(sup.ScreenInput)
class Conn:
    width = 1280
    height = 800
print(s._point(Conn(), {"x": float("nan"), "y": 0.5}))
print(s._point(Conn(), {"x": float("inf"), "y": 0.5}))
print(s._point(Conn(), {"x": 0.5, "y": float("-nan")}))
`)
	if out != "None\nNone\nNone" {
		t.Fatalf("got %q", out)
	}
}

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
	if runtime.GOOS == "windows" {
		// The stand-in encoder is a shebang shell script on PATH; Windows
		// cannot exec it, so the relayed encoder bytes never arrive.
		t.Skip("the fake ffmpeg needs a POSIX exec")
	}
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
func TestSupervisorH264MSEClientBoundsQueue(t *testing.T) {
	out := runSupervisorPython(t, `
print("mq.length>=8" in sup.H264_PLAYER, "ws.onclose=()=>{mq=[];" in sup.H264_PLAYER)
print('"stream-resize"' in sup.H264_PLAYER, '"stream-ping"' in sup.H264_PLAYER, "renegotiating=true" in sup.H264_PLAYER, 'Q.set("w"' in sup.H264_PLAYER)
`)
	if out != "True True\nTrue True True True" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorH264MSEBoundsSlowViewerQueue(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
v = sup.H264Viewer(socket.socketpair()[0], 20, "", True, mse=True)
for i in range(sup.H264_MAX_QUEUED):
    print(v.offer_mse(b"x"))
print(len(v.queue), v.offer_mse(b"overflow"), len(v.queue), v.closed.is_set(), v.close_reason)
`)
	if out != "True\nTrue\nTrue\n3 False 3 True slow_viewer" {
		t.Fatalf("got %q", out)
	}
}

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
v.want_input = True
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
func TestSupervisorH264ViewOnlyRejectsDirectInput(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
owner = "ab" * 8
os.environ["MACLAW_DESKTOP_KEY"] = owner
v = sup.H264Viewer(socket.socketpair()[0], 20, owner, True)
calls = []
class Fake:
    def apply(self, viewer, events):
        calls.append(events); return ""
    def release(self, viewer): pass
v.screen = Fake()
sent = []
v.send_json = sent.append
sup.clipboard_set = lambda display, text: calls.append(("set", text)) or True
sup.clipboard_get = lambda display: calls.append(("get", display)) or "secret"
v.handle({"type": "input", "seq": 0, "events": [{"t": "move", "x": 0, "y": 0}]})
v.handle({"type": "clipboard_set", "text": "changed"})
v.handle({"type": "clipboard_get"})
print(calls, [m.get("code") for m in sent], v.input_allowed())
`)
	if out != "[] ['view_only', 'view_only'] False" {
		t.Fatalf("got %q", out)
	}
}

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

// A watch-only viewer on a slow link (mobile ?w=) keeps the full-res grab but
// encodes a narrower stream: scale sits in front of mpdecimate, and the same
// size passed twice never grows a scale filter.
func TestSupervisorH264ScaledEncoderArgs(t *testing.T) {
	out := runSupervisorPython(t, `
a = sup.h264_ffmpeg_argv(21, "1440x900", 10, out_size="960x600")
b = sup.h264_ffmpeg_argv(21, "1440x900", 15, out_size="1440x900")
vf_a, vf_b = a[a.index("-vf") + 1], b[b.index("-vf") + 1]
print(a[a.index("-video_size") + 1], vf_a.startswith("scale=960:600,"), "mpdecimate" in vf_a,
      "scale" in vf_b, a[a.index("-framerate") + 1])
`)
	if out != "1440x900 True True False 10" {
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
# str(Path(...)) is backslashed on Windows; compare with separators folded.
sup.Path.exists = lambda self: True if str(self).replace("\\", "/").startswith("/tmp/.X11-unix") else real_exists(self)
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

func TestSupervisorPageUnstickClosesAndReopens(t *testing.T) {
	out := runSupervisorPython(t, `
pages = [{"id":"A","type":"page","url":"https://www.zhihu.com/","webSocketDebuggerUrl":"ws://x/a"},
         {"id":"B","type":"page","url":"https://zhuanlan.zhihu.com/write","webSocketDebuggerUrl":"ws://x/b"}]
sup.http_json_any = lambda h, p, path: pages
sup.chrome_debug_port = lambda k: 18030
calls = []
def dev(port, method, path):
    calls.append(method + " " + path)
    return b"HTTP/1.1 200 OK\r\n\r\n{\"id\":\"C\"}"
sup.http_devtools = dev
sup.log_event = lambda *a, **k: None
sup.page_alive = lambda t, timeout=8: {"state": "complete", "filled": True}
print(sup.page_unstick("k", "https://zhuanlan.zhihu.com/write"), calls)
sup.page_alive = lambda t, timeout=8: {"state": "loading", "filled": False}
print(sup.page_unstick("k", "https://zhuanlan.zhihu.com/write")["why"], calls)
sup.page_alive = lambda t, timeout=8: None
r = sup.page_unstick("k", "https://zhuanlan.zhihu.com/write")
print(r["done"], calls)
print(sup.pick_hung_page(pages, "")["id"], sup.pick_hung_page(pages, "https://zhuanlan.zhihu.com/p/1")["id"])
`)
	want := "{'done': False, 'why': 'alive_with_content'} []\nalive_slow []\nTrue ['GET /json/close/B', 'PUT /json/new?https://zhuanlan.zhihu.com/write', 'GET /json/activate/C']\nA B"
	if out != want {
		t.Fatalf("got %q", out)
	}
}

// A takeover viewer drops unchanged frames too and runs at 20 fps, not 30:
// 10-31 one watching viewer held a bot container at ~107% CPU.
func TestH264InteractiveViewerIsLight(t *testing.T) {
	out := runSupervisorPython(t, `
a = sup.h264_ffmpeg_argv(30, "1280x800", sup.H264_INTERACTIVE_FPS, interactive=True)
print(a[a.index("-framerate") + 1], "mpdecimate" in a[a.index("-vf") + 1], sup.TAKEOVER_IDLE_SECONDS)
`)
	if out != "20 True 90" {
		t.Fatalf("got %s", out)
	}
}

// One encoder per display, shared by every Annex B viewer: the first viewer
// starts it, a later one starts from the cached GOP (keyframe first), and it
// stops when the last viewer leaves.
func TestSupervisorH264SharedEncoder(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, subprocess, sys, time
spawned = []
def spawn(self, argv, env):
    spawned.append(argv)
    return subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"], stdout=subprocess.PIPE)
sup.H264SharedEncoder.spawn = spawn
sup.desktop_geometry = lambda: "1280x800x24"
a = sup.H264Viewer(socket.socketpair()[0], 31, "", True)
b = sup.H264Viewer(socket.socketpair()[0], 31, "", True)
e1 = sup.h264_shared_encoder(31, 1280, 800, 15)
a.shared = e1; e1.subscribe(a)
time.sleep(0.3)
e1.fan_out(b"K1", True); e1.fan_out(b"d1", False)
e2 = sup.h264_shared_encoder(31, 1280, 800, 15)
b.shared = e2; e2.subscribe(b)
time.sleep(0.2)
print(e1 is e2, len(spawned), [f for _, f, _ in a.queue], [f for _, f, _ in b.queue])
e1.unsubscribe(a)
print(e1.stopped.is_set(), (31, 1280, 800, 15) in sup._h264_encoders)
e1.unsubscribe(b)
time.sleep(0.5)
print(e1.stopped.is_set(), (31, 1280, 800, 15) in sup._h264_encoders, e1.proc is None)
`)
	want := "True 1 [b'K1', b'd1'] [b'K1', b'd1']\nFalse True\nTrue False True"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Consumer desynchronization is local to that consumer: the shared encoder is
// not killed, and recovery is the complete cached keyframe chain. Sequence
// numbers are assigned only to access units actually queued for that viewer.
func TestSupervisorH264ResyncDoesNotRestartSharedEncoder(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
e = sup.H264SharedEncoder(31, 1280, 800, 15)
e.gop = [(b"K", True), (b"d1", False), (b"d2", False)]
restarts = []
e.request_restart = lambda force=False: restarts.append(force)
v = sup.H264Viewer(socket.socketpair()[0], 31, "", True)
v.shared = e
v.need_idr = True
v._resynchronize()
print(restarts, [f for _, f, _ in v.queue], v.need_idr, [n for n, _, _ in v.queue])
v.queue = []
v.sent, v.acked = 30, 10
v.offer(b"late", False)
print([n for n, _, _ in v.queue], v.sequence, v.need_idr)
v.need_idr = True
v.handle({"type": "request_keyframe", "reason": "decoder_busy"})
print([f for _, f, _ in v.queue], restarts, v.need_idr)
`)
	want := "[] [b'K', b'd1', b'd2'] False [1, 2, 3]\n[] 3 True\n[] [] True"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The replay window is one whole encoder GOP, not a partial chain that would
// break again on the next live delta; live fan-out happens under the same
// encoder lock as the copy-and-replay so the chain is contiguous.
func TestSupervisorH264ResyncReplaysOneWholeGOPAtomically(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
e = sup.H264SharedEncoder(31, 1280, 800, 15)
v = sup.H264Viewer(socket.socketpair()[0], 31, "", True)
v.shared = e
for n in (sup.H264_RESYNC_REPLAY_MAX, sup.H264_RESYNC_REPLAY_MAX + 1):
    e.gop = [(b"K", True)] + [(b"d", False)] * (n - 1)
    v.queue = []
    v.sequence = 0
    v.need_idr = True
    v.last_resync = 0
    print(n, e.resync(v), len(v.queue), bool(v.queue and v.queue[0][2]), v.need_idr)
e.viewers.append(v)
seen = []
v.offer = lambda frame, key: seen.append((frame, key, e.lock.locked()))
e.fan_out(b"live", False)
print(seen)
e.gop = [(b"K2", True), (b"d2", False)]
locked_during_replay = []
real_replay = v.replay_gop
def replay(gop):
    locked_during_replay.append(e.lock.locked())
    return real_replay(gop)
v.replay_gop = replay
v.last_resync = 0
print(e.resync(v), locked_during_replay)
`)
	want := "60 True 60 True False\n61 False 0 False True\n[(b'live', False, True)]\nTrue [True]"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Once the X capture source is gone, an encoder exit is terminal for that
// display generation. It is not retried as an encoder failure, and viewers
// get display_stopped rather than a black video-chain close.
func TestSupervisorH264EncoderDoesNotRestartAfterDisplayStops(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, subprocess, sys, time
spawned = []
def spawn(self, argv, env):
    spawned.append(argv)
    return subprocess.Popen([sys.executable, "-c", ""], stdout=subprocess.PIPE)
sup.H264SharedEncoder.spawn = spawn
sup.x_display_running = lambda key, display: False
sup.desktop_geometry = lambda: "1280x800x24"
sa, sb = socket.socketpair()
v = sup.H264Viewer(sa, 31, "", True)
e = sup.H264SharedEncoder(31, 1280, 800, 15)
v.shared = e
e.subscribe(v)
time.sleep(0.6)
print(len(spawned), e.proc is None, v.closed.is_set(), getattr(v, "close_reason", ""))
sa.close(); sb.close()
`)
	if out != "1 True True display_stopped" {
		t.Fatalf("got %q", out)
	}
}

// The stream's display state is the X screen itself: a missing browser does
// not make a running X screen stopped, and a missing X screen is never video.
func TestSupervisorH264DisplayStateIsTheXScreen(t *testing.T) {
	out := runSupervisorPython(t, fakeProcPrelude+`
proc(1, 0, 5000, ["/bin/sh", "-c", "hold"])
proc(44, 1, 5100, ["Xvfb", ":31", "-screen", "0", "1280x800x24"])
print(sup.x_display_running(key, 31), sup.x_display_running(key, 32))
`)
	if out != "True False" {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorH264DisplayStopClosesLiveStream(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
states = [False]
sup.x_display_running = lambda key, display: states[0]
a, b = socket.socketpair()
v = sup.H264Viewer(a, 31, "ab" * 8, False)
sent = []
v.send_json = sent.append
v.display_watch_loop()
print(sent, v.closed.is_set(), v.close_reason)
a.close(); b.close()
`)
	want := "[{'type': 'display_state', 'state': 'display_stopped'}] True display_stopped"
	if out != want {
		t.Fatalf("got %q", out)
	}
}

func TestSupervisorH264StoppedDisplayDoesNotStartEncoder(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
sup.x_display_running = lambda key, display: False
called = []
def shared(*args):
    called.append(args)
    raise AssertionError("encoder started")
sup.h264_shared_encoder = shared
a, b = socket.socketpair()
v = sup.H264Viewer(a, 31, "ab" * 8, False)
sent = []
v.send_json = sent.append
v.run()
print(sent[0]["display_state"], called, v.closed.is_set())
a.close(); b.close()
`)
	if out != "display_stopped [] True" {
		t.Fatalf("got %q", out)
	}
}

// A black canvas is not a valid stream state: the player only treats a frame
// as received when it is rendered, reconnects when rendering stalls, and puts
// an explanatory overlay (or the noVNC fallback) in front of repeated failure.
func TestSupervisorH264PlayerNeverPresentsUnexplainedBlack(t *testing.T) {
	out := runSupervisorPython(t, `
p = sup.H264_PLAYER
print("function watchVideo()" in p, "markRender()" in p, "视频流异常" in p, "streamStopped=false;const qs" in p)
print("display_state" in p, "桌面画面未运行" in p, "function onVideo(buf){const{h,data}=parseVideo(buf);gotFrame" in p)
`)
	if out != "True True True True\nTrue True False" {
		t.Fatalf("got %q", out)
	}
}
