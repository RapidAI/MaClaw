package botmgmt

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopModeDefaultsToShared(t *testing.T) {
	for in, want := range map[string]string{"": "", "shared": "", " Isolated ": "isolated"} {
		got, err := NormalizeDesktopMode(in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q %v", in, got, err)
		}
	}
	if _, err := NormalizeDesktopMode("own"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestIsolatedBotHasItsOwnDesktopUser(t *testing.T) {
	shared := Bot{ID: "bot_a"}
	isolated := Bot{ID: "bot_b", DesktopMode: desktop.BotDesktopModeIsolated}
	if got := desktopUserForBot("alice", shared); got != "alice" {
		t.Fatalf("shared %q", got)
	}
	got := desktopUserForBot("alice", isolated)
	if got != desktop.BotDesktopUser("alice", "bot_b") {
		t.Fatalf("isolated %q", got)
	}
	// Re-deriving from the bot's own desktop user stays stable.
	if again := desktopUserForBot(got, isolated); again != got {
		t.Fatalf("not stable %q", again)
	}
	if owner, bot := desktop.SplitDesktopUser(got); owner != "alice" || bot != "bot_b" {
		t.Fatalf("split %q %q", owner, bot)
	}
}
