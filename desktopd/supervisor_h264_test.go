package desktopd

import (
	"strings"
	"testing"
)

// The H.264 view stream is off unless MACLAW_DESKTOP_H264 is on, and then
// only answers /h264 and /h264.html on the noVNC gate.
func TestSupervisorH264IsBehindItsFlag(t *testing.T) {
	out := runSupervisorPython(t, `
head = b"GET /h264 HTTP/1.1\r\nHost: x\r\n\r\n"
print(sup.h264_requested(head, {}), sup.h264_requested(head, {"MACLAW_DESKTOP_H264": "on"}))
print(sup.h264_requested(b"GET /vnc.html HTTP/1.1\r\n\r\n", {"MACLAW_DESKTOP_H264": "on"}), sup.h264_requested(b"GET /v1/desktops/tunnel/k/vnc/h264.html HTTP/1.1\r\n\r\n", {"MACLAW_DESKTOP_H264": "on"}))
print(sup.ws_accept_key("dGhlIHNhbXBsZSBub25jZQ=="))
`)
	want := "False True\nFalse True\ns3pPLMBiTxaQ9kYGzzhZRbK+xOo="
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

func TestSupervisorH264EncoderArgs(t *testing.T) {
	out := runSupervisorPython(t, `
argv = sup.h264_ffmpeg_argv(21, "1440x900")
print(argv[argv.index("-i") + 1], "mpdecimate" in argv[argv.index("-vf") + 1], argv[argv.index("-fps_mode") + 1], argv[argv.index("-profile:v") + 1], argv[-2], sup.h264_ffmpeg_argv(21, "1440x900", 15, True)[-2])
`)
	if out != ":21 True vfr baseline flv mp4" {
		t.Fatalf("got %q", out)
	}
}

// With the flag off the gate answers the new player URL with a redirect to
// the noVNC page the GUI named, so the picture still opens.
func TestSupervisorH264FlagOffRedirectsToVNC(t *testing.T) {
	out := runSupervisorPython(t, `
import socket
os.environ.pop("MACLAW_DESKTOP_H264", None)
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
