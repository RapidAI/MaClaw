package botmgmt

import "testing"

func TestHubStartedTurnsRunInTheirOwnSession(t *testing.T) {
	if got := desktopSessionKey("bot_a", "bot-turn-1"); got != "bot_a" {
		t.Fatalf("person's turn left the bot conversation: %q", got)
	}
	del, wake := desktopSessionKey("bot_a", "xdel-note_1"), desktopSessionKey("bot_a", "xwake-bot_a-3")
	if del == "bot_a" || wake == "bot_a" || del == wake {
		t.Fatalf("Hub turns share a session: %q %q", del, wake)
	}
}
