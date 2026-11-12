package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestWebFetchRetriesASlowWakeWithoutStartingTheDesktop(t *testing.T) {
	woke := 0
	calls := 0
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		woke++
		return desktopEndpoint{CDP: "http://127.0.0.1:9", Display: ":20"}, nil
	}
	desktopRemoteHTTP = func(context.Context, string, string, string) (string, int, string, error) {
		calls++
		if calls == 1 {
			return "", 0, "", desktopbot.WakingError{}
		}
		return "<html>daily paper</html>", 200, "text/html", nil
	}
	prevGap, prevBudget := desktopWakeRetryGap, desktopWakeRetryBudget
	desktopWakeRetryGap = time.Millisecond
	desktopWakeRetryBudget = time.Second
	t.Cleanup(func() {
		desktopRemoteSession, desktopRemoteHTTP = nil, nil
		desktopWakeRetryGap, desktopWakeRetryBudget = prevGap, prevBudget
	})
	body, status, kind, err := desktopContainerHTTP(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err != nil || status != 200 || kind != "text/html" || !strings.Contains(body, "daily paper") || woke != 0 || calls != 2 {
		t.Fatalf("body=%q status=%d kind=%q woke=%d calls=%d err=%v", body, status, kind, woke, calls, err)
	}
}

func TestWebFetchAsleepDoesNotWakeTheGUI(t *testing.T) {
	woke := 0
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		woke++
		return desktopEndpoint{}, nil
	}
	desktopRemoteHTTP = func(context.Context, string, string, string) (string, int, string, error) {
		return "", 0, "", desktopbot.AsleepError{}
	}
	t.Cleanup(func() {
		desktopRemoteSession, desktopRemoteHTTP = nil, nil
	})
	_, _, _, err := desktopContainerHTTP(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err == nil || woke != 0 || !strings.Contains(err.Error(), "系统故障") {
		t.Fatalf("woke=%d err=%v", woke, err)
	}
}
