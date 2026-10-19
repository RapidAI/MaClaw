package guiapp

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
					{"bot_id": "bot_1", "run_id": "run_done", "task": "下载报表", "status": "succeeded", "reply": map[string]any{"text": "下载过程写在这里", "send_to_user": "报表已下载"}},
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
	if len(payloads) != 1 || !strings.Contains(payloads[0], `"delivery_report":true`) || !strings.Contains(payloads[0], `"send_to_user":"报表已下载"`) || strings.Contains(payloads[0], "不该再推一次") || strings.Contains(payloads[0], "下载过程") || strings.Contains(payloads[0], `"text"`) {
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

func TestDesktopBotDeliveryKeepsChoicesWithoutAQuestion(t *testing.T) {
	var payloads []string
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	t.Cleanup(func() { desktopBotReportSink = nil })
	app := &App{}
	kept := desktopBotDelivery{BotID: "bot_1", RunID: "run_choices", Status: "succeeded", Phase: "execute"}
	kept.Reply.Text = "无变化"
	kept.Reply.AskUserOptionsJSON = `["继续","停下"]`
	if !app.emitDesktopBotDelivery("local", kept) {
		t.Fatal("emit choices")
	}
	if len(payloads) != 1 || !strings.Contains(payloads[0], "ask_user_options_json") || !strings.Contains(payloads[0], "继续") || !strings.Contains(payloads[0], "停下") || !strings.Contains(payloads[0], `"phase":"execute"`) || strings.Contains(payloads[0], "无变化") || strings.Contains(payloads[0], `"text"`) {
		t.Fatalf("choices payload = %s", payloads)
	}
	payloads = nil
	skipped := desktopBotDelivery{BotID: "bot_1", RunID: "run_filler_choices", Status: "succeeded"}
	skipped.Reply.AskUserOptionsJSON = `["无变化", " "]`
	if !app.emitDesktopBotDelivery("local", skipped) {
		t.Fatal("emit filler choices")
	}
	if len(payloads) != 1 || strings.Contains(payloads[0], "ask_user_options_json") || strings.Contains(payloads[0], "无变化") {
		t.Fatalf("filler choices payload = %s", payloads)
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

func TestPullDesktopBotDeliveryUsesSendToUserWhenTheRunFailed(t *testing.T) {
	var payloads []string
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.HasSuffix(path, "/pending") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{
						"bot_id": "bot_1",
						"run_id": "run_fail_sentence",
						"task":   "打开首页",
						"status": "failed",
						"reply":  map[string]any{"text": "无变化", "send_to_user": "首页没打开。"},
						"error":  "",
					},
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
		delete(shownDesktopRuns, desktopRunKey("bot_1", "run_fail_sentence"))
		delete(ackedDesktopRuns, desktopRunKey("bot_1", "run_fail_sentence"))
		activeDesktopRunsMu.Unlock()
	})
	if err := (&App{}).PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	raw := payloads[0]
	if !strings.Contains(raw, `"send_to_user":"首页没打开。"`) || strings.Contains(raw, "无变化") || strings.Contains(raw, `"text"`) || strings.Contains(raw, `"error"`) {
		t.Fatalf("payload = %s", raw)
	}
}

func TestPullDesktopBotDeliveryKeepsAScreenshotWithoutCaption(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	var payloads []string
	shot := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.HasSuffix(path, "/pending") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{"bot_id": "bot_1", "run_id": "run_shot", "task": "截个图", "status": "succeeded", "reply": map[string]any{
						"text":   "",
						"images": []map[string]any{{"mime": "image/png", "data": shot, "attachment_id": "shot-1"}},
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
	if len(payloads) != 1 || strings.Contains(payloads[0], "截个图") || strings.Contains(payloads[0], `"error"`) || strings.Contains(payloads[0], `"send_to_user"`) || strings.Contains(payloads[0], `"text"`) {
		t.Fatalf("payload = %#v", payloads)
	}
	if !strings.Contains(payloads[0], `"desktop_attachment_ids"`) || !strings.Contains(payloads[0], "shot-1") || strings.Contains(payloads[0], `"desktop_shot_paths"`) || strings.Contains(payloads[0], shot) {
		t.Fatalf("screenshot id dropped: %s", payloads[0])
	}
	if _, err := os.Stat(filepath.Join(base, "data", "bot-files")); !os.IsNotExist(err) {
		t.Fatalf("screenshot was written on this machine: %v", err)
	}
}

func TestPullDesktopBotDeliveryKeepsFileAndScreenshotWithTheSentence(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	var payloads []string
	shot := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	doc := base64.StdEncoding.EncodeToString([]byte("doc-bytes"))
	desktopBotReportSink = func(raw string) { payloads = append(payloads, raw) }
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.HasSuffix(path, "/pending") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{
					{"bot_id": "bot_1", "run_id": "run_both", "task": "截个图", "status": "succeeded", "reply": map[string]any{
						"text":         "过程记录",
						"send_to_user": "文件和截图都在。",
						"images":       []map[string]any{{"mime": "image/png", "data": shot, "attachment_id": "shot-1"}},
						"files":        []map[string]any{{"name": "note.docx", "mime": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "data": doc, "attachment_id": "file-1"}},
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
		delete(shownDesktopRuns, desktopRunKey("bot_1", "run_both"))
		delete(ackedDesktopRuns, desktopRunKey("bot_1", "run_both"))
		activeDesktopRunsMu.Unlock()
	})
	if err := (&App{}).PullDesktopBotDeliveries(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	raw := payloads[0]
	if !strings.Contains(raw, `"send_to_user":"文件和截图都在。"`) || !strings.Contains(raw, `"desktop_attachment_ids"`) || !strings.Contains(raw, "shot-1") || !strings.Contains(raw, "file-1") || strings.Contains(raw, `"desktop_shot_paths"`) || strings.Contains(raw, `"local_file_paths"`) || strings.Contains(raw, shot) || strings.Contains(raw, doc) {
		t.Fatalf("payload = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(base, "data", "bot-files")); !os.IsNotExist(err) {
		t.Fatalf("delivery was written on this machine: %v", err)
	}
	if strings.Contains(raw, "截个图") || strings.Contains(raw, `"error"`) || strings.Contains(raw, "过程记录") || strings.Contains(raw, `"text"`) {
		t.Fatalf("payload = %s", raw)
	}
}
