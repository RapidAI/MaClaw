package guiapp

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotPollPublishesActivity(t *testing.T) {
	previous := desktopBotPollInterval
	desktopBotPollInterval = time.Millisecond
	var published []string
	desktopBotActivitySink = func(botID, runID, activity string) {
		published = append(published, botID+"\t"+runID+"\t"+activity)
	}
	t.Cleanup(func() {
		desktopBotPollInterval = previous
		desktopBotExchangeOverride = nil
		desktopBotActivitySink = nil
	})

	var calls int
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method != http.MethodGet || !strings.Contains(path, "/runs/run_1") {
			t.Fatalf("poll = %s %s", method, path)
		}
		calls++
		switch calls {
		case 1:
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running", "activity": "正在输入"})
			return http.StatusAccepted, nil
		case 2:
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running", "activity": "正在输入"})
			return http.StatusAccepted, nil
		case 3:
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running", "activity": "正在点击"})
			return http.StatusAccepted, nil
		case 4, 5:
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running", "activity": "正在点击 secret-password"})
			return http.StatusAccepted, nil
		default:
			fillDesktopBotDest(t, dest, map[string]any{"text": "写好了", "status": "succeeded"})
			return http.StatusOK, nil
		}
	}

	got, err := (&App{}).waitDesktopBotRun(context.Background(), "bot_1", "run_1")
	if err != nil || got.Text != "写好了" {
		t.Fatalf("text=%q err=%v", got.Text, err)
	}
	want := []string{"bot_1\trun_1\t正在输入", "bot_1\trun_1\t正在点击", "bot_1\trun_1\t"}
	if len(published) != len(want) {
		t.Fatalf("published %v", published)
	}
	for i := range want {
		if published[i] != want[i] {
			t.Fatalf("published %v", published)
		}
		if strings.Contains(published[i], "secret") {
			t.Fatalf("password published: %v", published)
		}
	}
}

func TestDesktopBotPollClearsActivityWhenTheRunFinishes(t *testing.T) {
	previous := desktopBotPollInterval
	desktopBotPollInterval = time.Millisecond
	var published []string
	desktopBotActivitySink = func(botID, runID, activity string) {
		published = append(published, botID+"\t"+runID+"\t"+activity)
	}
	t.Cleanup(func() {
		desktopBotPollInterval = previous
		desktopBotExchangeOverride = nil
		desktopBotActivitySink = nil
	})
	var calls int
	desktopBotExchangeOverride = func(_ context.Context, _ string, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		calls++
		if calls == 1 {
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running", "activity": "正在点击"})
			return http.StatusAccepted, nil
		}
		fillDesktopBotDest(t, dest, map[string]any{"text": "点过了", "status": "succeeded"})
		return http.StatusOK, nil
	}
	got, err := (&App{}).waitDesktopBotRun(context.Background(), "bot_1", "run_9")
	if err != nil || got.Text != "点过了" {
		t.Fatalf("text=%q err=%v", got.Text, err)
	}
	want := []string{"bot_1\trun_9\t正在点击", "bot_1\trun_9\t"}
	if len(published) != len(want) {
		t.Fatalf("published %v", published)
	}
	for i := range want {
		if published[i] != want[i] {
			t.Fatalf("published %v", published)
		}
	}
}

func TestPublishDesktopBotActivityIsNotABubble(t *testing.T) {
	var activity []string
	var results int
	desktopBotActivitySink = func(botID, runID, phrase string) {
		activity = append(activity, botID+"\t"+runID+"\t"+phrase)
	}
	desktopBotResultSink = func(*IMAgentResponse) { results++ }
	t.Cleanup(func() {
		desktopBotActivitySink = nil
		desktopBotResultSink = nil
	})
	(&App{}).publishDesktopBotActivity("bot_1", "run_1", "正在点击")
	if results != 0 {
		t.Fatalf("activity published a reply: %d", results)
	}
	if len(activity) != 1 || activity[0] != "bot_1\trun_1\t正在点击" {
		t.Fatalf("activity = %#v", activity)
	}
}
