package guiapp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotReadingQuotaExhaustedGoesStraightToHub(t *testing.T) {
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	var seen []desktopBotUnderstandRequest
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		seen = append(seen, req)
		if !req.hub {
			return "", errors.New(`WorkBuddy 429: {"error":{"data":{"code":14018,"msg":"额度已用尽"}}}`)
		}
		return `{"told":"我去打开。","instruction":"打开百度首页。","pace":"direct"}`, nil
	}
	got, err := (&App{}).UnderstandDesktopBotTask("打开百度", "plan", "zh-Hans", nil, "", "")
	if err != nil || got == nil || got.Instruction == "" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if len(seen) != 2 || seen[0].hub || !seen[1].hub {
		t.Fatalf("quota exhaustion was retried on the same provider: %#v", seen)
	}
}

func TestDesktopBotReadingFailureStillForwardsTheWords(t *testing.T) {
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	desktopBotUnderstandLLM = func(context.Context, *App, desktopBotUnderstandRequest) (string, error) {
		return "", nil // 200 with nothing in content or reasoning
	}
	got, err := (&App{}).UnderstandDesktopBotTask("把知乎回答发出去", "plan", "zh-Hans", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "direct" || got.Instruction != "把知乎回答发出去" || got.Fallback == "" {
		t.Fatalf("failed reading did not forward the words verbatim: %#v", got)
	}
}

func TestDesktopBotUnderstandingAcceptsAliasFields(t *testing.T) {
	got, err := parseDesktopBotUnderstanding("<think>…</think>\n```json\n{\"reply\":\"好的，我来发。\",\"order\":\"在知乎发布回答。\",\"pace\":\"direct\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != "好的，我来发。" || got.Instruction != "在知乎发布回答。" {
		t.Fatalf("%#v", got)
	}
	got, err = parseDesktopBotUnderstanding(`{"told":"好","steps":["打开知乎","发布回答"],"pace":"direct"}`)
	if err != nil || !strings.Contains(got.Instruction, "发布回答") {
		t.Fatalf("steps list not taken as the order: %#v %v", got, err)
	}
}

func TestDesktopBotRedactedSnippetMasks(t *testing.T) {
	out := desktopBotRedactedSnippet("key sk-abcdefghijklmnop phone 13812345678\n\n tail", 200)
	if strings.Contains(out, "abcdefghijkl") || strings.Contains(out, "13812345678") || !strings.Contains(out, "tail") {
		t.Fatalf("%q", out)
	}
}

func TestDesktopBotGatewayRetryable(t *testing.T) {
	if !desktopBotGatewayRetryable(http.StatusBadGateway, errors.New("MaClawSrv 不可达")) || !desktopBotGatewayRetryable(0, errors.New("MaClawSrv 不可达")) {
		t.Fatal("502 / unreachable not retried")
	}
	if desktopBotGatewayRetryable(http.StatusForbidden, errors.New("no")) || desktopBotGatewayRetryable(http.StatusBadGateway, nil) {
		t.Fatal("non-gateway failure retried")
	}
}

func TestDesktopBotCallRetriesGatewayThenSucceeds(t *testing.T) {
	prev, prevWaits := desktopBotExchangeOverride, desktopBotGatewayRetryWaits
	t.Cleanup(func() { desktopBotExchangeOverride, desktopBotGatewayRetryWaits = prev, prevWaits })
	desktopBotGatewayRetryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	calls := 0
	desktopBotExchangeOverride = func(context.Context, string, string, any, any, time.Duration, map[string]string) (int, error) {
		calls++
		if calls < 3 {
			return http.StatusBadGateway, errors.New("MaClawSrv 不可达")
		}
		return http.StatusOK, nil
	}
	if err := (&App{}).desktopBotCallRetryingGateway(context.Background(), http.MethodPost, "/api/v1/bots/b/desktop", nil, nil, time.Second); err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestHubReconnectJitterAndState(t *testing.T) {
	for i := 0; i < 50; i++ {
		d := hubReconnectJitter(10 * time.Second)
		if d < 8*time.Second || d > 12*time.Second {
			t.Fatalf("jitter out of range: %s", d)
		}
	}
	var nilClient *RemoteHubClient
	if nilClient.connectionState().State != "offline" {
		t.Fatal("nil client is offline")
	}
	c := &RemoteHubClient{reconnectCh: make(chan struct{}, 1)}
	c.allowReconnect.Store(true)
	c.nextRetryAt.Store(time.Now().Add(5 * time.Second).UnixMilli())
	st := c.connectionState()
	if st.State != "connecting" || st.NextRetryMs <= 0 {
		t.Fatalf("%#v", st)
	}
}
