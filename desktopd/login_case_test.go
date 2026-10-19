package desktopd

import (
	"os"
	"strings"
	"testing"
)

// Section 4.5. Offline: paths, clone, and write-back. No live Google login.
func TestDesktopBotLoginCases(t *testing.T) {
	t.Run("new_bot_sees_user_login", func(t *testing.T) {
		raw, err := os.ReadFile("image/desktop_supervisor.py")
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		start := text[strings.Index(text, "def start_bot_desktop"):]
		next := strings.Index(start, "\ndef ")
		if next < 0 || !strings.Contains(start[:next], "prepare_screen_profile") || strings.Contains(start[:next], `home / "profile"`) {
			t.Fatal("a bot still gets an empty profile that is never written back")
		}
		out := runSupervisorPython(t, `
import os
from pathlib import Path
user = "ab" * 8
bot = user + "11" * 8
bot2 = user + "22" * 8
other = "cd" * 8
os.environ["MACLAW_DESKTOP_KEY"] = user
sup.save_registry({"users": {
    user: {"display": 20, "token": "aa" * 16},
    bot: {"display": 21, "token": "bb" * 16, "bot": True},
    bot2: {"display": 22, "token": "cc" * 16, "bot": True},
}})
master = sup.master_profile(user)
master.mkdir(parents=True)
(master / "SingletonLock").write_text("held", encoding="utf-8")
work20 = sup.prepare_screen_profile(user, 20)
assert not (work20 / "SingletonLock").exists()
sup.release_profile_lock(work20)
assert (master / "SingletonLock").read_text(encoding="utf-8") == "held"
marker = b"screen-1-login"
(work20 / "Default").mkdir(parents=True)
(work20 / "Default" / "Cookies").write_bytes(marker)
(work20 / "Default" / "Login Data").write_bytes(marker + b"-data")
(work20 / "Default" / "Local Storage").mkdir()
(work20 / "Default" / "Local Storage" / "leveldb").write_text("screen-1-ls", encoding="utf-8")
assert sup.writeback_screen_login(user, 20) is True
assert (master / "Default" / "Cookies").read_bytes() == marker
assert (master / "Default" / "Login Data").read_bytes() == marker + b"-data"
assert (master / "Default" / "Local Storage" / "leveldb").read_text(encoding="utf-8") == "screen-1-ls"
assert (master / "SingletonLock").read_text(encoding="utf-8") == "held"
record_path = sup.screen_login_record(user, 20)
record20 = json.loads(record_path.read_text(encoding="utf-8"))
assert record20["cloned_from_master"] is True
assert record20["wrote_back"] is True
assert Path(record20["master"]) == master
assert Path(record20["work"]) == work20
assert marker.decode() not in record_path.read_text(encoding="utf-8")
work21 = sup.prepare_screen_profile(bot, 21)
assert work21 == sup.ROOT / user / "screens" / "21" / "profile"
assert master == sup.ROOT / user / "profile"
assert (work21 / "Default" / "Cookies").read_bytes() == marker
assert not (sup.ROOT / bot / "profile").exists()
work22 = sup.prepare_screen_profile(bot2, 22)
assert work22 != work21 and work22 != work20 and work21 != master and work20 != master
def udd(argv):
    for item in argv:
        if item.startswith("--user-data-dir="):
            return item.split("=", 1)[1]
    return ""
assert udd(sup.browser_argv(work20, 18020)) == str(work20)
assert udd(sup.browser_argv(work21, 18021, bot=True)) == str(work21)
assert udd(sup.browser_argv(work22, 18022, bot=True)) == str(work22)
opened = False
try:
    sup.prepare_screen_profile(other, 20)
    opened = True
except SystemExit as exc:
    assert "another user" in str(exc)
assert not opened
assert sup.profile_for(other) is None
assert sup.profile_path_allowed(other, master) is False
escape = master / ".." / ".." / other / "profile"
assert sup.profile_path_allowed(user, escape) is False
assert not (sup.ROOT / other).exists()
(master / "Default" / "Cookies").write_bytes(b"later-login")
live = str(work21.resolve())
real_pid = sup.browser_pid
def fake_pid(profile):
    try:
        if str(Path(profile).resolve()) == live:
            return 99
    except OSError:
        return 0
    return 0
sup.browser_pid = fake_pid
assert sup.prepare_screen_profile(bot, 21) == work21
assert (work21 / "Default" / "Cookies").read_bytes() == marker
assert sup.writeback_screen_login(bot, 21) is False
sup.browser_pid = real_pid
(work22 / "Default" / "Cookies").write_bytes(b"from-22")
assert sup.writeback_screen_login(bot2, 22) is True
assert (master / "Default" / "Cookies").read_bytes() == b"from-22"
assert (work21 / "Default" / "Cookies").read_bytes() == marker
assert (master / "SingletonLock").read_text(encoding="utf-8") == "held"
record22 = json.loads(sup.screen_login_record(user, 22).read_text(encoding="utf-8"))
assert record22["cloned_from_master"] is True and record22["wrote_back"] is True
assert "from-22" not in sup.screen_login_record(user, 22).read_text(encoding="utf-8")
if os.name == "posix":
    assert master.stat().st_mode & 0o777 == 0o700
    assert work21.stat().st_mode & 0o777 == 0o700
print("new_bot_sees_user_login ok")
`)
		if out != "new_bot_sees_user_login ok" {
			t.Fatalf("got:\n%s", out)
		}
	})
}
