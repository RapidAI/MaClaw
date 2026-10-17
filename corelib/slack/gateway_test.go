package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewaySkipsMessagesAlreadyInHistory(t *testing.T) {
	ts := fmt.Sprintf("%d.000000", time.Now().Add(-30*time.Second).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"D1"}]}`)
		case "/conversations.history":
			_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"`+ts+`","user":"U2","text":"already there"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got := make(chan IncomingMessage, 1)
	gw := NewGateway("xoxb-test", "", func(msg IncomingMessage) {
		select {
		case got <- msg:
		default:
		}
	})
	gw.apiBase = srv.URL
	gw.pollEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	select {
	case msg := <-got:
		t.Fatalf("replayed history = %#v", msg)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestGatewayDeliversNewDirectMessage(t *testing.T) {
	ts := fmt.Sprintf("%d.000000", time.Now().Add(time.Minute).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"D1"}]}`)
		case "/conversations.history":
			_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"`+ts+`","user":"U2","text":"hello &amp; slack"},{"ts":"1.000000","user":"UOLD","text":"old"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got := make(chan IncomingMessage, 1)
	gw := NewGateway("xoxb-test", "", func(msg IncomingMessage) {
		select {
		case got <- msg:
		default:
		}
	})
	gw.apiBase = srv.URL
	gw.pollEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	select {
	case msg := <-got:
		if msg.ChannelID != "D1" || msg.UserID != "U2" || msg.Text != "hello & slack" {
			t.Fatalf("msg = %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slack message")
	}
}

func TestGatewayKeepsWatermarksPerChannel(t *testing.T) {
	newer := fmt.Sprintf("%d.000000", time.Now().Add(2*time.Minute).Unix())
	older := fmt.Sprintf("%d.000000", time.Now().Add(30*time.Second).Unix())
	var d1 atomic.Int32
	var d2 atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"D1"},{"id":"D2"}]}`)
		case "/conversations.history":
			switch r.URL.Query().Get("channel") {
			case "D1":
				if d1.Add(1) == 1 {
					_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"1.000000","user":"UOLD","text":"old"}]}`)
					return
				}
				_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"`+newer+`","user":"U2","text":"from-d1"}]}`)
			case "D2":
				n := d2.Add(1)
				if n == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if n == 2 {
					_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"1.000000","user":"UOLD","text":"old"}]}`)
					return
				}
				_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"`+older+`","user":"U3","text":"from-d2"}]}`)
			default:
				_, _ = io.WriteString(w, `{"ok":true,"messages":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var mu sync.Mutex
	got := map[string]IncomingMessage{}
	gw := NewGateway("xoxb-test", "", func(msg IncomingMessage) {
		mu.Lock()
		got[msg.Text] = msg
		mu.Unlock()
	})
	gw.apiBase = srv.URL
	gw.pollEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		_, ok1 := got["from-d1"]
		_, ok2 := got["from-d2"]
		mu.Unlock()
		if ok1 && ok2 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("messages = %#v", got)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestGatewayDeliversThreadReply(t *testing.T) {
	replyTS := fmt.Sprintf("%d.000000", time.Now().Add(time.Minute).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"D1"}]}`)
		case "/conversations.history":
			if r.URL.Query().Get("oldest") == "" {
				_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"10.000000","user":"U2","text":"parent","reply_count":1}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"messages":[]}`)
		case "/conversations.replies":
			_, _ = io.WriteString(w, `{"ok":true,"messages":[{"ts":"10.000000","thread_ts":"10.000000","user":"U2","text":"parent"},{"ts":"`+replyTS+`","thread_ts":"10.000000","user":"U2","text":"thread reply"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got := make(chan IncomingMessage, 1)
	gw := NewGateway("xoxb-test", "", func(msg IncomingMessage) {
		select {
		case got <- msg:
		default:
		}
	})
	gw.apiBase = srv.URL
	gw.pollEvery = 20 * time.Millisecond
	gw.threadScanEvery = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	select {
	case msg := <-got:
		if msg.ChannelID != "D1" || msg.Text != "thread reply" || msg.ThreadTS != "10.000000" {
			t.Fatalf("msg = %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for thread reply")
	}
}

func TestGatewayBacksOffWhenRateLimited(t *testing.T) {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			lists.Add(1)
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw := NewGateway("xoxb-test", "", nil)
	gw.apiBase = srv.URL
	gw.pollEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	gw.Stop()
	if got := lists.Load(); got > 2 {
		t.Fatalf("list calls = %d, want backoff after the first 429", got)
	}
}

func TestGatewayStopsRetryingADeadToken(t *testing.T) {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			lists.Add(1)
			_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw := NewGateway("xoxb-test", "", nil)
	gw.apiBase = srv.URL
	gw.pollEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	gw.Stop()
	if got := lists.Load(); got > 2 {
		t.Fatalf("list calls = %d, want a long pause after invalid_auth", got)
	}
	if gw.LastError() != "Slack 令牌已失效，请重新登录" {
		t.Fatalf("last error = %q", gw.LastError())
	}
}

func TestGatewaySurfacesMissingScope(t *testing.T) {
	var histories atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT"}`)
		case "/conversations.list":
			_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"D1"}]}`)
		case "/conversations.history":
			histories.Add(1)
			_, _ = io.WriteString(w, `{"ok":false,"error":"missing_scope"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw := NewGateway("xoxb-test", "", nil)
	gw.apiBase = srv.URL
	gw.pollEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := gw.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	gw.Stop()
	if got := histories.Load(); got > 2 {
		t.Fatalf("history calls = %d, want a long pause after missing_scope", got)
	}
	if gw.LastError() != "Slack 权限不足，请断开后重新登录" {
		t.Fatalf("last error = %q", gw.LastError())
	}
}

func TestSendTextPostsPlainChunks(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	gw := NewGateway("xoxb-test", "UBOT", nil)
	gw.apiBase = srv.URL
	text := strings.Repeat("a", slackTextLimit) + "\n" + strings.Repeat("b", 10)
	if err := gw.SendText(context.Background(), "D1", text); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(bodies) != 2 {
		t.Fatalf("posts = %d, bodies = %v", len(bodies), bodies)
	}
	for _, body := range bodies {
		if !strings.Contains(body, `"mrkdwn":false`) || strings.Contains(body, `"thread_ts"`) {
			t.Fatalf("body = %s", body)
		}
	}
	mu.Unlock()
	if err := gw.SendThreadText(context.Background(), "D1", "10.000000", "in thread"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	last := bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(last, `"thread_ts":"10.000000"`) {
		t.Fatalf("thread body = %s", last)
	}
}

func TestSplitSlackTextKeepsShortMessages(t *testing.T) {
	if got := splitSlackText("hello"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("split = %#v", got)
	}
}
