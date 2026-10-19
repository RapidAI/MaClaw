package guiapp

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestDesktopBotSplitCases(t *testing.T) {
	t.Run("gui_close_run_continues", func(t *testing.T) {
		abandonDesktopSurface()
		t.Cleanup(func() {
			abandonDesktopSurface()
			desktopBotRelayOverride = nil
			desktopBotExchangeOverride = nil
			desktopBotResultSink = nil
		})
		var mu sync.Mutex
		var calls []string
		var replied *IMAgentResponse
		entered := make(chan struct{})
		desktopBotResultSink = func(resp *IMAgentResponse) {
			mu.Lock()
			replied = resp
			mu.Unlock()
		}
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			mu.Lock()
			calls = append(calls, method+" "+path)
			mu.Unlock()
			return http.StatusOK, nil
		}
		desktopBotRelayOverride = func(ctx context.Context, _, _, _ string) (desktopBotRelayResult, error) {
			close(entered)
			<-ctx.Done()
			return desktopBotRelayResult{}, ctx.Err()
		}
		done := make(chan struct{})
		go func() {
			(&App{}).finishDesktopBotTask("desktop-bot-close", "alice:bot_1", "bot_1", "打开首页", "execute")
			close(done)
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("the surface wait did not start")
		}
		abandonDesktopSurface()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("closing the window left this surface waiting")
		}
		mu.Lock()
		gotReply := replied
		gotCalls := append([]string(nil), calls...)
		mu.Unlock()
		if gotReply != nil {
			t.Fatalf("closing the window emitted a reply: %#v", gotReply)
		}
		for _, call := range gotCalls {
			if strings.Contains(call, "/cancel") || strings.Contains(call, "/ack") {
				t.Fatalf("closing the window posted %s", call)
			}
		}
		next := desktopSurfaceContext()
		select {
		case <-next.Done():
			t.Fatal("the next open inherited the closed window")
		default:
		}
		if err := (&App{}).CancelDesktopBotRun("bot_1", "run-1"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		gotCalls = append([]string(nil), calls...)
		mu.Unlock()
		var cancels int
		for _, call := range gotCalls {
			if call == http.MethodPost+" /api/v1/bots/bot_1/runs/run-1/cancel" {
				cancels++
			}
		}
		if cancels != 1 {
			t.Fatalf("the person saying stop posted %v", gotCalls)
		}
	})

	t.Run("schedule_fires_without_gui", func(t *testing.T) {
		relays := 0
		reports := 0
		desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
			relays++
			return desktopBotRelayResult{Text: "早报好了。", SendToUser: "早报好了。"}, nil
		}
		desktopBotReportSink = func(string) { reports++ }
		t.Cleanup(func() {
			desktopBotRelayOverride = nil
			desktopBotReportSink = nil
		})
		text, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
			ID:              "sched-0800",
			DesktopBotID:    "bot_1",
			DesktopBotOwner: "alice",
			Action:          "整理早间简报。",
			Hour:            8,
			Minute:          0,
		})
		if err != nil || text != "" || relays != 0 || reports != 0 {
			t.Fatalf("GUI started the 08:00 round text=%q relays=%d reports=%d err=%v", text, relays, reports, err)
		}
	})
}
