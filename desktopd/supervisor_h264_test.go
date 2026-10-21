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
// refused, the player page is served, and the stream upgrades and relays
// the encoder's bytes as binary WebSocket frames.
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
print(page.split(b"\r\n")[0].decode(), b"MediaSource" in page)
stream = roundtrip(b"GET /h264 HTTP/1.1\r\nAuthorization: Bearer " + owner.encode() + b"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
head, _, body = stream.partition(b"\r\n\r\n")
print(head.split(b"\r\n")[0].decode(), body == bytes([0x82, 4]) + b"FMP4")
`)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "403") || lines[1] != "HTTP/1.1 200 OK True" || lines[2] != "HTTP/1.1 101 Switching Protocols True" {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSupervisorH264EncoderArgs(t *testing.T) {
	out := runSupervisorPython(t, `
argv = sup.h264_ffmpeg_argv(21, "1440x900")
print(argv[argv.index("-i") + 1], "mpdecimate" in argv[argv.index("-vf") + 1], argv[argv.index("-fps_mode") + 1], argv[argv.index("-profile:v") + 1])
`)
	if out != ":21 True vfr baseline" {
		t.Fatalf("got %q", out)
	}
}
