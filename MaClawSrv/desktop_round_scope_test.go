package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// One bot's round ending must not drop the browser binding of another bot,
// nor the shared binding while another round of the same desktop still runs.
func TestFinishDesktopRoundScopesBinding(t *testing.T) {
	const tenantID, owner = "tenant-scope", "alice-scope"
	ownerKey, err := desktop.UserKey(tenantID, owner)
	if err != nil {
		t.Fatal(err)
	}
	botKey, err := desktop.UserKey(tenantID, desktop.BotDesktopUser(owner, "bot1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		desktopBindings.Delete(ownerKey)
		desktopBindings.Delete(botKey)
		desktopRoundWakes.Delete("scope-a")
		desktopRoundWakes.Delete("scope-b")
		desktopRoundWakes.Delete("scope-bot")
	})
	desktopBindings.Store(ownerKey, &desktopBinding{session: &browser.BrowserAgentSession{}, addr: "http://10.0.0.8:1"})
	desktopBindings.Store(botKey, &desktopBinding{session: &browser.BrowserAgentSession{}, addr: "http://10.0.0.8:2"})
	desktopRoundWakes.Store("scope-a", &desktopRoundWake{userKey: ownerKey})
	desktopRoundWakes.Store("scope-b", &desktopRoundWake{userKey: ownerKey})
	desktopRoundWakes.Store("scope-bot", &desktopRoundWake{userKey: botKey})

	finishDesktopRound(tenantID, owner, "scope-bot")
	if _, ok := desktopBindings.Load(botKey); ok {
		t.Fatal("isolated bot binding stayed after its round")
	}
	if _, ok := desktopBindings.Load(ownerKey); !ok {
		t.Fatal("isolated bot round dropped the owner's shared binding")
	}
	finishDesktopRound(tenantID, owner, "scope-a")
	if _, ok := desktopBindings.Load(ownerKey); !ok {
		t.Fatal("shared binding dropped while round scope-b still runs")
	}
	finishDesktopRound(tenantID, owner, "scope-b")
	if _, ok := desktopBindings.Load(ownerKey); ok {
		t.Fatal("shared binding stayed after the last round")
	}
}
