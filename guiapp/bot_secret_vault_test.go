package guiapp

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestBotSecretStaysInTheUserKeyring(t *testing.T) {
	keyring.MockInit()
	const secret = "s3cret-value"
	if err := saveBotSecret("alice", "SITE_PASSWORD", secret); err != nil {
		t.Fatal(err)
	}
	if !recallBotSecret("alice", "SITE_PASSWORD") {
		t.Fatal("saved secret was not recalled as present")
	}
	if recallBotSecret("alice", "OTHER_PASSWORD") {
		t.Fatal("a missing name was reported as stored")
	}
	stored, err := keyring.Get("MaClaw Bot Secret", "alice/SITE_PASSWORD")
	if err != nil || stored != secret {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
	if _, err := keyring.Get("MaClaw Bot Secret", "bot_1/SITE_PASSWORD"); err == nil {
		t.Fatal("secret was stored under a bot id")
	}
	if _, err := keyring.Get("MaClaw Remote SSH", "alice/SITE_PASSWORD"); err == nil {
		t.Fatal("secret was stored in the remote SSH vault")
	}
	value, ok := loadBotSecret("alice", "SITE_PASSWORD")
	if !ok || value != secret {
		t.Fatal("load failed")
	}
}
