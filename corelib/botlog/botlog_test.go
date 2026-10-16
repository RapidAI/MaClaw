package botlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteKeepsOneFilePerBotAndRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_BOT_LOG_DIR", dir)
	Write("bot_alice", "hub.post_message", nil,
		"status", "200",
		"access_token", "super-secret",
		"body", `{"access_token":"abc","error":"no"} Bearer rawtoken sk-abcdef012345 /api/v1/desktop-handoff/handofftoken`,
	)
	Write("", "hub.skip", nil, "status", "500")
	Write("../bot_alice", "hub.other", nil, "status", "1")

	body, err := os.ReadFile(filepath.Join(dir, "bot_alice.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "stage=hub.post_message") || !strings.Contains(text, "bot=bot_alice") || !strings.Contains(text, "status=200") {
		t.Fatalf("line = %s", text)
	}
	if strings.Contains(text, "super-secret") || strings.Contains(text, "sk-abcdef012345") || strings.Contains(text, "handofftoken") || strings.Contains(text, `"abc"`) {
		t.Fatalf("secret leaked: %s", text)
	}
	if !strings.Contains(text, "access_token=[redacted]") || !strings.Contains(text, "Bearer [redacted]") || !strings.Contains(text, "sk-[redacted]") || !strings.Contains(text, "/api/v1/desktop-handoff/[redacted]") {
		t.Fatalf("redaction missing: %s", text)
	}
	escaped, err := os.ReadFile(filepath.Join(dir, ".._bot_alice.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(escaped), "stage=hub.other") {
		t.Fatalf("escaped name was not isolated: %s", escaped)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("files = %d, want the two bot files only", len(entries))
	}
}

func TestWriteOnceSuppressesTheSamePollError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_BOT_LOG_DIR", dir)
	WriteOnce("bot_poll", "gui.desktop_poll", errString("dial tcp refused"), "path", "/desktop")
	WriteOnce("bot_poll", "gui.desktop_poll", errString("dial tcp refused"), "path", "/desktop")
	body, err := os.ReadFile(filepath.Join(dir, "bot_poll.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(body), "stage=gui.desktop_poll"); got != 1 {
		t.Fatalf("poll lines = %d\n%s", got, body)
	}
}

func TestHostDropsUserinfoAndQuery(t *testing.T) {
	if got := Host("https://user:secret@hub.example:8443/v1?token=abc"); got != "hub.example:8443" {
		t.Fatalf("host = %q", got)
	}
	if got := Host(""); got != "" {
		t.Fatalf("empty host = %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
