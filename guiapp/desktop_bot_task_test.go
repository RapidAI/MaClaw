package guiapp

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDesktopBotInstancesStayOnOneUser(t *testing.T) {
	user1, first, err := desktopBotIdentity("alice", "bot-1")
	if err != nil {
		t.Fatal(err)
	}
	user2, second, err := desktopBotIdentity("alice", "bot-2")
	if err != nil {
		t.Fatal(err)
	}
	if user1 != "alice" || user2 != "alice" {
		t.Fatalf("users = %q %q", user1, user2)
	}
	if first == second || first != "bot-1" || second != "bot-2" {
		t.Fatalf("instances = %q %q", first, second)
	}
	if desktopBotLocalSessionKey(user1, first) == desktopBotLocalSessionKey(user2, second) {
		t.Fatal("two instances resolved to one local session")
	}
	if _, _, err := desktopBotIdentity("alice", ""); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
	if _, _, err := desktopBotIdentity("alice", "bad id"); err == nil {
		t.Fatal("bot id with a space should be rejected")
	}
}

func TestUnreachableBotStaysAFailure(t *testing.T) {
	if _, ok := (&App{}).desktopTimeoutHandoff("bot_1", fmt.Errorf("MaClawSrv 不可达")); ok {
		t.Fatal("an unreachable instance was turned into a login handoff")
	}
	timeout := fmt.Errorf("MaClawSrv 没有在时限内返回结果")
	if !desktopTimeoutHandsOff(timeout, "https://hub.example/vnc", true) {
		t.Fatal("a login handoff that outlived the reply was not shown")
	}
	if desktopTimeoutHandsOff(timeout, "https://hub.example/vnc", false) {
		t.Fatal("a slow task was given the keyboard")
	}
	if desktopTimeoutHandsOff(fmt.Errorf("MaClawSrv 不可达"), "https://hub.example/vnc", true) {
		t.Fatal("an unreachable instance was turned into a login handoff")
	}
	if !keepLoginDesktopAfterFailure(fmt.Errorf("instance returned no result"), "https://hub.example/vnc", true) {
		t.Fatal("a failed continuation hid the logged-in desktop")
	}
	if keepLoginDesktopAfterFailure(fmt.Errorf("MaClawSrv 不可达"), "https://hub.example/vnc", true) {
		t.Fatal("an unreachable instance kept a login desktop")
	}
	if keepLoginDesktopAfterFailure(fmt.Errorf("instance returned no result"), "https://hub.example/vnc", false) {
		t.Fatal("a normal failure was shown as a login desktop")
	}
	if !desktopCommandTimedOut(fmt.Errorf("MaClawSrv 没有在时限内返回结果")) {
		t.Fatal("timeout was not recognized")
	}
	if desktopCommandTimedOut(fmt.Errorf("MaClawSrv 不可达")) {
		t.Fatal("unreachable was treated as a timeout")
	}
}

func TestSendDesktopBotTaskReachesTheInstance(t *testing.T) {
	got := make(chan string, 1)
	desktopBotRelayOverride = func(_ context.Context, botID, text string) (string, string, bool, error) {
		got <- botID + ":" + text
		return "done", "", false, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	resp, err := (&App{}).SendDesktopBotTask("bot_1", "open site")
	if err != nil || resp == nil || !resp.Deferred || resp.RequestID == "" {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	select {
	case body := <-got:
		if body != "bot_1:open site" {
			t.Fatalf("relayed %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message was not sent to the instance")
	}
}
