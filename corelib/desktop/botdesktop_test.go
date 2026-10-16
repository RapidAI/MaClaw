package desktop

import "testing"

func TestBotDesktopUserRoundTrip(t *testing.T) {
	u := BotDesktopUser("alice", "bot-1")
	if u != "alice::desk::bot-1" || !ValidUserID(u) {
		t.Fatalf("desktop user %q", u)
	}
	if owner, bot := SplitDesktopUser(u); owner != "alice" || bot != "bot-1" {
		t.Fatalf("split %q %q", owner, bot)
	}
	if owner, bot := SplitDesktopUser("alice"); owner != "alice" || bot != "" {
		t.Fatalf("plain split %q %q", owner, bot)
	}
	if BotDesktopUser("alice", "") != "alice" {
		t.Fatal("empty bot must be the owner desktop")
	}
	if BotDesktopUser(u, "bot-2") != "alice::desk::bot-2" {
		t.Fatal("re-wrapping must not nest")
	}
	if owner, bot := SplitDesktopUser("alice::desk::bad id"); bot != "" || owner != "alice::desk::bad id" {
		t.Fatalf("malformed bot accepted: %q %q", owner, bot)
	}
}

func TestBotKeyBelongsToOwner(t *testing.T) {
	owner, _ := UserKey("t", "alice")
	key, err := BotKey("t", "alice", "bot-1")
	if err != nil || len(key) != 32 || key[:16] != owner || !ValidDesktopKey(key) || ValidUserKey(key) {
		t.Fatalf("bot key %q %v", key, err)
	}
	other, _ := BotKey("t", "alice", "bot-2")
	if other == key {
		t.Fatal("two bots share a key")
	}
	viaUser, _ := DesktopKey("t", BotDesktopUser("alice", "bot-1"))
	plain, _ := DesktopKey("t", "alice")
	if viaUser != key || plain != owner {
		t.Fatalf("DesktopKey %q %q", viaUser, plain)
	}
	if _, err := BotKey("t", "alice", "bad id"); err == nil {
		t.Fatal("invalid bot id accepted")
	}
}
