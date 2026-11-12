package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestWebFetchWakesAStoppedContainerAndReturnsThePage(t *testing.T) {
	woke := 0
	calls := 0
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		woke++
		return desktopEndpoint{CDP: "http://127.0.0.1:9", Display: ":20"}, nil
	}
	desktopRemoteHTTP = func(context.Context, string, string, string) (string, int, string, error) {
		calls++
		if calls == 1 {
			return "", 0, "", errors.New(`docker: container is not running`)
		}
		return "<html>daily paper</html>", 200, "text/html", nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteHTTP = nil
	})
	body, status, kind, err := desktopContainerHTTP(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err != nil || status != 200 || kind != "text/html" || !strings.Contains(body, "daily paper") {
		t.Fatalf("body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	if woke != 1 || calls != 2 {
		t.Fatalf("woke=%d calls=%d", woke, calls)
	}
}

func TestWebFetchWakeFailureIsAFaultNotAQuestion(t *testing.T) {
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{}, errors.New("pool exhausted")
	}
	desktopRemoteHTTP = func(context.Context, string, string, string) (string, int, string, error) {
		return "", 0, "", errors.New("desktop_asleep")
	}
	t.Cleanup(func() {
		desktopRemoteSession = nil
		desktopRemoteHTTP = nil
	})
	_, _, _, err := desktopContainerHTTP(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err == nil || !strings.Contains(err.Error(), "系统故障") || strings.Contains(err.Error(), "要不要开机") || strings.Contains(err.Error(), "ask_user") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(desktopbot.WakeSystemFault, "系统故障") {
		t.Fatal(desktopbot.WakeSystemFault)
	}
}
