package guiapp

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPullDesktopBotDeliveriesSkipsTheRunThisProcessIsWaitingOn(t *testing.T) {
	var payloads []string
	var acks []string
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		switch {
		case method == http.MethodGet && strings.HasSuffix(path, "/pending"):
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{"bot_id": "bot_1", "run_id": "run_live", "task": "还在这台机器上等", "status": "succeeded", "reply": map[string]any{"text": "不该再推一次"}},
					{"bot_id": "bot_1", "run_id": "run_done", "task": "下载报表", "status": "succeeded", "reply": map[string]any{"text": "报表已下载"}},
				},
			})
			return http.StatusOK, nil
		case method == http.MethodPost && strings.Contains(path, "/ack"):
			acks = append(acks, path)
			return http.StatusOK, nil
		default:
			t.Fatalf("unexpected %s %s", method, path)
			return 0, nil
		}
	}
	app := &App{}
	app.registerActiveDesktopRun("bot_1", "req-live", "local:bot_1", "还在这台机器上等", "execute")
	app.setActiveDesktopRunID("bot_1", "req-live", "run_live")
	t.Cleanup(func() {
		desktopBotReportSink = nil
		desktopBotExchangeOverride = nil
		unregisterActiveDesktopRun("bot_1", "req-live", "run_live")
		activeDesktopRunsMu.Lock()
		delete(shownDesktopRuns, desktopRunKey("bot_1", "run_done"))
		delete(ackedDesktopRuns, desktopRunKey("bot_1", "run_done"))
		activeDesktopRunsMu.Unlock()
	})

	if err := app.PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || !strings.Contains(payloads[0], `"delivery_report":true`) || !strings.Contains(payloads[0], "报表已下载") || strings.Contains(payloads[0], "不该再推一次") {
		t.Fatalf("payloads = %#v", payloads)
	}
	if !strings.Contains(payloads[0], `"request_id":"desktop-bot-deliver-run_done"`) {
		t.Fatalf("payload = %s", payloads[0])
	}
	if len(acks) != 1 || !strings.Contains(acks[0], "/bots/bot_1/runs/run_done/ack") {
		t.Fatalf("acks = %#v", acks)
	}
	if err := app.PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || len(acks) != 1 {
		t.Fatalf("second pull repeated a shown run: payloads=%d acks=%#v", len(payloads), acks)
	}
}

func TestPullDesktopBotDeliveryMarksFailure(t *testing.T) {
	var payloads []string
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.HasSuffix(path, "/pending") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{"bot_id": "bot_1", "run_id": "run_fail", "task": "打开页面", "status": "failed", "reply": map[string]any{"text": "打不开页面"}, "error": "打不开页面"},
				},
			})
			return http.StatusOK, nil
		}
		if method == http.MethodPost && strings.Contains(path, "/ack") {
			return http.StatusOK, nil
		}
		t.Fatalf("unexpected %s %s", method, path)
		return 0, nil
	}
	t.Cleanup(func() {
		desktopBotReportSink = nil
		desktopBotExchangeOverride = nil
		activeDesktopRunsMu.Lock()
		delete(shownDesktopRuns, desktopRunKey("bot_1", "run_fail"))
		delete(ackedDesktopRuns, desktopRunKey("bot_1", "run_fail"))
		activeDesktopRunsMu.Unlock()
	})
	if err := (&App{}).PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0], `"text"`) || !strings.Contains(payloads[0], `"error"`) || !strings.Contains(payloads[0], "打不开页面") {
		t.Fatalf("payload = %#v", payloads)
	}
}

func TestPullDesktopBotDeliveryKeepsAScreenshotWithoutCaption(t *testing.T) {
	var payloads []string
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.HasSuffix(path, "/pending") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{"bot_id": "bot_1", "run_id": "run_shot", "task": "截个图", "status": "succeeded", "reply": map[string]any{
						"text":   "",
						"images": []map[string]any{{"mime": "image/png", "data": "not-a-picture"}},
					}},
				},
			})
			return http.StatusOK, nil
		}
		if method == http.MethodPost && strings.Contains(path, "/ack") {
			return http.StatusOK, nil
		}
		t.Fatalf("unexpected %s %s", method, path)
		return 0, nil
	}
	t.Cleanup(func() {
		desktopBotReportSink = nil
		desktopBotExchangeOverride = nil
		activeDesktopRunsMu.Lock()
		delete(shownDesktopRuns, desktopRunKey("bot_1", "run_shot"))
		delete(ackedDesktopRuns, desktopRunKey("bot_1", "run_shot"))
		activeDesktopRunsMu.Unlock()
	})
	if err := (&App{}).PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || !strings.Contains(payloads[0], `"text":"截个图"`) || strings.Contains(payloads[0], `"error"`) {
		t.Fatalf("payload = %#v", payloads)
	}
}
