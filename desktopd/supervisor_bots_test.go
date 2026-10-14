package desktopd

import (
	"strings"
	"testing"
)

const botOwner = "MACLAW_DESKTOP_KEY=abababababababab"

// A bot key belongs to the container owner; ensure gives it its own display
// above :20, routes CDP through the owner's 19020 gate, and keeps the owner's
// registry entry.
func TestSupervisorBotDisplaysAreSeparateFromTheOwner(t *testing.T) {
	out := runSupervisorPython(t, `
owner = "ab" * 8
bot1, bot2 = owner + "11" * 8, owner + "22" * 8
print(sup.is_bot_key(bot1), sup.is_bot_key(owner), sup.is_bot_key("cd" * 16), sup.is_bot_key(owner + "zz" * 8))
started = []
running = set()
sup.ensure_gates = lambda: None
sup.ensure_watch = lambda key, display: None
sup.desktop_running = lambda key: key in running
def start(key, display):
    started.append((key[-2:], display)); running.add(key)
sup.start_bot_desktop = start
sup.save_registry({"users": {owner: {"display": 20, "token": "aa" * 16, "last_used": 1}}})
a = sup.ensure(bot1)
b = sup.ensure(bot2)
again = sup.ensure(bot1)
print(a["display"], b["display"], again["display"], a["cdp_port"], a["token"] != b["token"], a["token"] == again["token"])
print(started)
users = sup.load_registry()["users"]
print(sorted(k[-2:] for k in users), users[owner]["display"])
`, botOwner)
	want := "True False False False\n:21 :22 :21 19020 True True\n[('11', 21), ('22', 22)]\n['11', '22', 'ab'] 20"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The owner's gates are part of display :20 only. Stopping a bot display must
// never adopt or kill them, and its window manager is xfwm4.
func TestSupervisorBotPartsLeaveTheOwnerGatesAlone(t *testing.T) {
	out := runSupervisorPython(t, fakeProcPrelude+`
bot = key + "11" * 8
proc(1, 0, 5000, ["/bin/sh", "-c", "hold"])
proc(17, 1, 5112, ["/usr/bin/python3", "/desktop_supervisor.py", "gate", "6080", "6081", "tok"])
proc(18, 1, 5113, ["/usr/bin/python3", "/desktop_supervisor.py", "gate", "19020", "18020", "tok"])
proc(30, 1, 5200, ["Xvfb", ":21", "-screen", "0", "1280x800x24"])
proc(31, 1, 5201, ["xfwm4", "--compositor=off"], display=21)
proc(32, 1, 5202, ["x11vnc", "-display", ":21", "-rfbport", "5901"])
proc(33, 1, 5203, ["/usr/bin/python3", "/usr/bin/websockify", "--web", "/usr/share/novnc", "127.0.0.1:6082", "localhost:5901"])
proc(34, 1, 5204, ["fcitx5"], display=21)
pids = sup.current_pids(bot, 21)
print(json.dumps({k: pids.get(k) for k in ("xvfb", "fluxbox", "x11vnc", "vnc", "fcitx", "vgate", "proxy")}, sort_keys=True))
main = sup.current_pids(key, 20)
print(main.get("vgate"), main.get("proxy"), main.get("vnc"))
`, botOwner)
	want := `{"fcitx": 34, "fluxbox": 31, "proxy": null, "vgate": null, "vnc": 33, "x11vnc": 32, "xvfb": 30}` + "\n17 18 None"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The owner's gates route a bot's token to that bot's display, refuse
// unknown tokens, and refuse a stopped bot's CDP.
func TestSupervisorGateRoutesBotTokens(t *testing.T) {
	out := runSupervisorPython(t, `
owner = "ab" * 8
bot = owner + "11" * 8
other = "cd" * 8 + "11" * 8
running = {bot}
sup.desktop_running = lambda key: key in running
sup.ensure_bot_vnc = lambda key, display: 6081 + display - 20
sup.save_registry({"users": {
    owner: {"display": 20, "token": "aa" * 16},
    bot: {"display": 23, "token": "bb" * 16, "bot": True},
    other: {"display": 24, "token": "cc" * 16, "bot": True},
}})
print(sup.bot_gate_route(19020, "bb" * 16), sup.bot_gate_route(6080, "bb" * 16))
print(sup.bot_gate_route(19020, "cc" * 16), sup.bot_gate_route(19020, "aa" * 16), sup.bot_gate_route(19020, ""), sup.bot_gate_route(5555, "bb" * 16))
head = b"GET /json HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer " + b"bb" * 16 + b"\r\n\r\n"
print(sup.request_bearer(head) == "bb" * 16, sup.request_authorized(head, "aa" * 16))
running.clear()
print(sup.bot_gate_route(19020, "bb" * 16))
`, botOwner)
	want := "(18023, '" + strings.Repeat("ab", 8) + strings.Repeat("11", 8) + "') (6084, '" + strings.Repeat("ab", 8) + strings.Repeat("11", 8) + "')\nNone None None None\nTrue False\n(18023, '" + strings.Repeat("ab", 8) + strings.Repeat("11", 8) + "')"
	// A stopped bot's CDP route still resolves; the connect then fails and
	// the gate closes the client. What matters is that a token never routes
	// to another user's or the owner's display.
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Person input on a bot's noVNC connection (key, click, paste) is detected;
// the viewer's own screen-update requests and mouse moves are not input.
func TestSupervisorBotTrafficDetectsPersonInput(t *testing.T) {
	out := runSupervisorPython(t, `
import struct, os
bot = "ab" * 8 + "11" * 8
(sup.ROOT / bot).mkdir()
def frame(payload):
    mask = os.urandom(4)
    body = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    n = len(payload)
    head = bytes([0x82]) + (bytes([0x80 | n]) if n < 126 else bytes([0x80 | 126]) + struct.pack(">H", n))
    return head + mask + body
w = sup.BotTraffic(bot, sniff_input=True)
hand = b"RFB 003.008\n" + b"\x01" + b"\x01"
setenc = struct.pack(">BxH", 2, 3) + struct.pack(">iii", 0, 1, -223)
fbreq = struct.pack(">BBHHHH", 3, 1, 0, 0, 1280, 800)
move = struct.pack(">BBHH", 5, 0, 10, 10)
data = frame(hand) + frame(setenc) + frame(fbreq) + frame(move)
# split across reads at awkward places
w.feed(data[:7]); w.feed(data[7:30]); w.feed(data[30:])
print(sup.bot_file_age(bot, "human_input") is None, w.sniff, w.stage)
key = struct.pack(">BBxxI", 4, 1, 0x61)
w.feed(frame(key))
print(sup.bot_file_age(bot, "human_input") is not None, w.sniff)
(sup.ROOT / bot / "human_input").unlink()
w.feed(frame(struct.pack(">BBHH", 5, 1, 50, 60)))
print(sup.bot_file_age(bot, "human_input") is not None)
`, botOwner)
	want := "True True 3\nTrue True\nTrue"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Over the cap, the least recently used idle bot is stopped; a busy bot
// (recent traffic or the person has it) is never evicted.
func TestSupervisorBotEvictionSkipsBusyBots(t *testing.T) {
	out := runSupervisorPython(t, `
import time
owner = "ab" * 8
keys = [owner + ("%02d" % i) * 8 for i in (11, 22, 33, 44)]
now = int(time.time())
users = {owner: {"display": 20}}
for i, k in enumerate(keys):
    users[k] = {"display": 21 + i, "token": "%02d" % i * 16, "bot": True, "last_used": now - 3600 + i}
sup.save_registry({"users": users})
running = set(keys[:3])
stopped = []
sup.desktop_running = lambda key: key in running
def stop(key):
    stopped.append(key[-2:]); running.discard(key)
sup.stop_desktop = stop
# keys[0] is oldest but the person has it; keys[1] is next oldest and idle.
(sup.ROOT / keys[0]).mkdir()
(sup.ROOT / keys[0] / "takeover.json").write_text('{"reason": "login"}')
sup.evict_bots(keys[3])
print(stopped)
# Everybody busy: run over the cap instead of killing a task.
(sup.ROOT / keys[2]).mkdir()
sup.touch_bot_file(keys[2], "activity", force=True)
running.add(keys[1])
stopped.clear()
users = sup.load_registry()["users"]; users[keys[1]]["last_used"] = int(time.time()); sup.save_registry({"users": users})
sup.evict_bots(keys[3])
print(stopped)
`, botOwner, "MACLAW_DESKTOP_MAX_BOTS=3")
	want := "['22']\n[]"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// request_takeover pauses the bot: bot_state says yield until hand back.
// Recent person input on the display also yields. Only bot keys are accepted.
func TestSupervisorTakeoverState(t *testing.T) {
	out := runSupervisorPython(t, `
bot = "ab" * 8 + "11" * 8
sup.save_registry({"users": {bot: {"display": 21, "token": "bb" * 16, "bot": True}}})
sup.desktop_running = lambda key: True
fcitx = []
sup.start_bot_fcitx = lambda key, display: fcitx.append(display)
s = sup.ctl_bot_state({"key": bot})
print(s["yield"], s["takeover"], s["human_input_age"])
print(sup.ctl_takeover_begin({"key": bot, "reason": "请登录 Google"}), fcitx)
s = sup.ctl_bot_state({"key": bot})
print(s["yield"], s["takeover"], s["reason"])
print(sup.ctl_takeover_end({"key": bot}))
print(sup.ctl_bot_state({"key": bot})["yield"])
sup.touch_bot_file(bot, "human_input", force=True)
print(sup.ctl_bot_state({"key": bot})["yield"])
for bad in ({"key": "ab" * 8}, {"key": "cd" * 16}, {}):
    try:
        sup.ctl_bot_state(bad); print("accepted")
    except ValueError:
        print("refused")
`, botOwner)
	want := "False False -1\n{'ok': True, 'takeover': True, 'reason': '请登录 Google'} [21]\nTrue True 请登录 Google\n{'ok': True, 'takeover': False}\nFalse\nTrue\nrefused\nrefused\nrefused"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Opening a browser on a bot display joins that bot's profile, not the
// owner's; on the owner display it is still the owner's.
func TestSharedBrowserKeyFollowsTheDisplay(t *testing.T) {
	out := runSupervisorPython(t, `
owner = "ab" * 8
bot = owner + "11" * 8
sup.save_registry({"users": {owner: {"display": 20}, bot: {"display": 21}}})
env = {"MACLAW_DESKTOP_KEY": owner}
print(sup.shared_browser_key(dict(env, DISPLAY=":21")) == bot, sup.shared_browser_key(dict(env, DISPLAY=":20")) == owner, sup.shared_browser_key(dict(env, DISPLAY=":29")) == owner)
`, botOwner)
	if out != "True True True" {
		t.Fatalf("got %s", out)
	}
}

// Bot browsers get the memory-saving flags merged into one --disable-features;
// the owner's browser flags are unchanged.
func TestBotBrowserFlags(t *testing.T) {
	out := runSupervisorPython(t, `
main = sup.browser_argv("/p", 18020)
bot = sup.browser_argv("/q", 18021, bot=True)
print(sum(a.startswith("--disable-features=") for a in bot), "--renderer-process-limit=2" in bot, "--renderer-process-limit=2" in main)
print([a for a in main if a.startswith("--disable-features=")][0])
print("IsolateOrigins" in [a for a in bot if a.startswith("--disable-features=")][0])
`)
	want := "1 True False\n--disable-features=TrackingProtection3pcd,ThirdPartyStoragePartitioning\nTrue"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
