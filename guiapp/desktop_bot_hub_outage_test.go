package guiapp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A Hub redeploy (papers 2026-11-11 20:27:43–20:28:03) answered Bot 3's turn
// 「服务端连不上」 although nothing had failed: the turn never reached Hub. It
// is now sent again under the same key until Hub is back.
func TestDesktopBotAdmitWaitsOutAHubRestart(t *testing.T) {
	resetActiveDesktopRuns(t)
	resetDesktopAdmitCancel()
	prevWait := desktopBotOutageWait
	desktopBotOutageWait = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { desktopBotOutageWait = prevWait; desktopBotExchangeOverride = nil })
	posts := 0
	var keys []string
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodPost && strings.HasSuffix(path, "/messages") {
			posts++
			if m, ok := body.(map[string]any); ok {
				k, _ := m["client_message_id"].(string)
				keys = append(keys, k)
			}
			switch {
			case posts <= 4:
				return 0, desktopHubNotReachedError{} // connection refused
			case posts == 5:
				return http.StatusBadGateway, desktopHubNotReachedError{} // the proxy's own page
			}
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-back", "status": "running", "client_message_id": "turn-hub-restart"})
			return http.StatusAccepted, nil
		}
		fillDesktopBotDest(t, dest, map[string]any{"text": "做好了", "status": "succeeded"})
		return http.StatusOK, nil
	}
	got, err := (&App{}).relayDesktopBot(context.Background(), "bot_3", "做图文综述", "execute", "turn-hub-restart")
	if err != nil || got.Text != "做好了" {
		t.Fatalf("text=%q err=%v posts=%d", got.Text, err, posts)
	}
	for _, k := range keys {
		if k != "turn-hub-restart" {
			t.Fatalf("a resend changed the turn key: %v", keys)
		}
	}
}

func TestDesktopBotAdmitReportsAnOutageThatOutlastsTheWindow(t *testing.T) {
	resetActiveDesktopRuns(t)
	resetDesktopAdmitCancel()
	prevWait, prevWindow := desktopBotOutageWait, desktopBotAdmitOutageWindow
	desktopBotOutageWait = func(int) time.Duration { return 5 * time.Millisecond }
	desktopBotAdmitOutageWindow = 40 * time.Millisecond
	t.Cleanup(func() {
		desktopBotOutageWait, desktopBotAdmitOutageWindow = prevWait, prevWindow
		desktopBotExchangeOverride = nil
	})
	posts := 0
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
		posts++
		return 0, desktopHubNotReachedError{}
	}
	_, err := (&App{}).relayDesktopBot(context.Background(), "bot_3", "做图文综述", "execute", "turn-down")
	if err == nil || !desktopBotNotReached(err) || posts < 3 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestDesktopDialFailedOnlyForConnectionsThatNeverOpened(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	if !desktopDialFailed(refused) || !desktopDialFailed(&net.DNSError{Name: "hub"}) {
		t.Fatal("dial failure not recognized")
	}
	if desktopDialFailed(&net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}) || desktopDialFailed(context.Canceled) {
		t.Fatal("a request that may have been sent was treated as never sent")
	}
}

func TestRelayDesktopBotSendsContinueSession(t *testing.T) {
	resetActiveDesktopRuns(t)
	resetDesktopAdmitCancel()
	t.Cleanup(func() {
		desktopBotExchangeOverride = nil
		clearDesktopContinueSession("turn-continue")
	})
	setDesktopContinueSession("turn-continue", "bot_3#t-k-b")
	var gotBody map[string]any
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodPost && strings.HasSuffix(path, "/messages") {
			if m, ok := body.(map[string]any); ok {
				gotBody = m
			}
			fillDesktopBotDest(t, dest, map[string]any{
				"accepted": true, "run_id": "run-c", "status": "running",
				"client_message_id": "turn-continue", "session": "bot_3#t-k-b",
			})
			return http.StatusAccepted, nil
		}
		fillDesktopBotDest(t, dest, map[string]any{"text": "做好了", "status": "succeeded"})
		return http.StatusOK, nil
	}
	got, err := (&App{}).relayDesktopBot(context.Background(), "bot_3", "把日期也加上", "execute", "turn-continue")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if gotBody["continue_session"] != "bot_3#t-k-b" {
		t.Fatalf("body = %#v", gotBody)
	}
	if got.Session != "bot_3#t-k-b" {
		t.Fatalf("result session = %q", got.Session)
	}
}

// TestRelayDesktopBotPassesServerRoute: the page names no conversation; Hub
// routed the turn and says so, and the page is told right after admission.
func TestRelayDesktopBotPassesServerRoute(t *testing.T) {
	resetActiveDesktopRuns(t)
	resetDesktopAdmitCancel()
	var routed []map[string]any
	desktopBotRoutedSink = func(p map[string]any) { routed = append(routed, p) }
	t.Cleanup(func() {
		desktopBotExchangeOverride = nil
		desktopBotRoutedSink = nil
	})
	var gotBody map[string]any
	desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodPost && strings.HasSuffix(path, "/messages") {
			gotBody, _ = body.(map[string]any)
			fillDesktopBotDest(t, dest, map[string]any{
				"accepted": true, "run_id": "run-r", "status": "running", "client_message_id": "turn-routed",
				"session": "bot_3#t-old", "routed_via": "content", "routed_title": "整理上周订单报表",
			})
			return http.StatusAccepted, nil
		}
		fillDesktopBotDest(t, dest, map[string]any{"text": "改好了", "status": "succeeded"})
		return http.StatusOK, nil
	}
	got, err := (&App{}).relayDesktopBot(context.Background(), "bot_3", "把报表金额改成两位小数", "execute", "turn-routed")
	if err != nil {
		t.Fatal(err)
	}
	if _, named := gotBody["continue_session"]; named {
		t.Fatalf("page decided a conversation: %#v", gotBody)
	}
	if got.Session != "bot_3#t-old" || got.RoutedVia != "content" || got.RoutedTitle != "整理上周订单报表" {
		t.Fatalf("result = %+v", got)
	}
	if len(routed) != 1 || routed[0]["request_id"] != "turn-routed" || routed[0]["title"] != "整理上周订单报表" {
		t.Fatalf("routed notice = %v", routed)
	}
}
