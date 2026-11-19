package botmgmt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The chat's 验证完成，继续, the 交还 button and Hub's own resume after a
// signed-in page are one continuation: the second joins the first while it
// runs, and a later one after it finished starts again.
func TestDesktopContinuationJoinsTheOneInFlight(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_cont","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			if n == 1 {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_cont","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	svc := newTurnKeyService(t, srv)
	countPosts := func() int {
		mu.Lock()
		defer mu.Unlock()
		return posts
	}

	firstDone := make(chan BotAdmission, 1)
	go func() {
		adm, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-chat")
		if err != nil {
			t.Error(err)
		}
		firstDone <- adm
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first continuation did not reach the message post")
	}

	// Still posting: no run id yet. The hand-back button joins with a line.
	posting, err := svc.AdmitDesktopMessage(WithDesktopContinuation(context.Background()), "tenant-a", "alice", "bot_alice", "我已经在桌面上完成了需要我做的操作（登录/验证），桌面交还给你，请继续刚才的任务。", "execute", "turn-button")
	if err != nil || !posting.Accepted || !posting.Settled || !posting.Joined || posting.Kind != "continue" || posting.ClientMessageID != "turn-button" {
		t.Fatalf("joined while posting = %+v %v", posting, err)
	}
	unblock()
	first := <-firstDone
	if first.RunID != "run_cont" {
		t.Fatalf("first continuation = %+v", first)
	}

	// Running: Hub's own resume gets a line. It must not be handed the run
	// id, or the caller's client waits on it and shows a second task.
	resumed, err := svc.AdmitDesktopMessage(WithDesktopContinuation(context.Background()), "tenant-a", "alice", "bot_alice", "页面已显示登录成功，桌面已自动交还给你，请沿用这个登录状态继续刚才的任务；不要重新打开或刷新页面。", "execute", "handback-bot_alice-1")
	if err != nil || !resumed.Accepted || !resumed.Settled || !resumed.Joined || resumed.RunID != "" || resumed.Reply.Text != DesktopContinueJoinedLine || resumed.ClientMessageID != "handback-bot_alice-1" {
		t.Fatalf("joined while running = %+v %v", resumed, err)
	}
	if n := countPosts(); n != 1 {
		t.Fatalf("posts = %d, want one continuation", n)
	}

	// Finished: the next handoff's continuation is a new turn.
	svc.mu.Lock()
	if state, ok := svc.desktopContinueStateLocked("tenant-a", "alice", "bot_alice", "turn-chat"); ok {
		state.mu.Lock()
		state.done = true
		state.mu.Unlock()
	} else {
		svc.mu.Unlock()
		t.Fatal("continuation state missing")
	}
	svc.mu.Unlock()
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-chat-2"); err != nil {
		t.Fatal(err)
	}
	if n := countPosts(); n != 2 {
		t.Fatalf("posts after the first finished = %d", n)
	}
}

// A handoff is answered once. After its continuation finished, another 继续
// for the same handoff gets a line, not a second task; the bot's next ask is
// a new handoff and continues again. A takeover the person starts while the
// continuation runs is part of it, not a new handoff.
func TestDesktopContinuationIsOncePerHandoff(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_cont","status":"running"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_cont","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	countPosts := func() int {
		mu.Lock()
		defer mu.Unlock()
		return posts
	}
	seq := func() uint64 {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return svc.desktopHandoffSeq[desktopHandoffSeqKey("tenant-a", "alice", "bot_alice")]
	}
	finish := func(key string) {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		state, ok := svc.desktopContinueStateLocked("tenant-a", "alice", "bot_alice", key)
		if !ok {
			t.Fatalf("continuation state %s missing", key)
		}
		state.mu.Lock()
		state.done = true
		state.mu.Unlock()
	}
	ctx := context.Background()

	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice") // HoldDesktop, then the settle: one ask
	if got := seq(); got != 1 {
		t.Fatalf("handoff seq after one ask = %d", got)
	}
	if _, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-return", "", "return"); err != nil {
		t.Fatal(err)
	}
	svc.openDesktopTakeover("tenant-a", "alice", "bot_alice")
	if got := seq(); got != 1 {
		t.Fatalf("a takeover during the continuation opened handoff %d", got)
	}
	finish("turn-return")

	// The 交还 button for the same handoff, after the run finished: any text.
	late, err := svc.AdmitDesktopMessage(WithDesktopContinuation(ctx), "tenant-a", "alice", "bot_alice", "继续", "execute", "turn-late")
	if err != nil || !late.Settled || !late.Joined || late.Reply.Text != desktopContinueDoneLine || late.ClientMessageID != "turn-late" {
		t.Fatalf("late continuation = %+v %v", late, err)
	}
	if n := countPosts(); n != 1 {
		t.Fatalf("posts = %d, want one per handoff", n)
	}
	// Only the label's text, sent as a plain message: not swallowed.
	typed, err := svc.AdmitDesktopMessage(ctx, "tenant-a", "alice", "bot_alice", "验证完成，继续", "execute", "turn-typed")
	if err != nil || typed.Joined {
		t.Fatalf("typed label after the continuation = %+v %v", typed, err)
	}
	if n := countPosts(); n != 2 {
		t.Fatalf("posts after a typed label = %d", n)
	}
	// The bot asks again: a new handoff, continued again.
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if got := seq(); got != 2 {
		t.Fatalf("handoff seq after the second ask = %d", got)
	}
	if _, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-return-2", "", "return"); err != nil {
		t.Fatal(err)
	}
	if n := countPosts(); n != 3 {
		t.Fatalf("posts after the second handoff = %d", n)
	}
}

// A continuation that ended in an error is not "already done": the same
// handoff can be continued again. One that is still running answers with a
// line and no run id, so the caller's client cannot wait on it as a second task.
func TestDesktopContinuationRetriesAfterItsRunFailed(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"async":true,"run":{"id":"run_%d","status":"running"}}`, n)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	ctx := context.Background()
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if _, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-fail", "", "return"); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	state, ok := svc.desktopContinueStateLocked("tenant-a", "alice", "bot_alice", "turn-fail")
	svc.mu.Unlock()
	if !ok {
		t.Fatal("failed continuation has no state")
	}
	state.mu.Lock()
	state.done = true
	state.err = fmt.Errorf("run ended as failed")
	state.mu.Unlock()

	again, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-retry", "", "return")
	if err != nil || again.Joined || again.RunID != "run_2" {
		t.Fatalf("retry after failure = %+v %v", again, err)
	}
	mu.Lock()
	n := posts
	mu.Unlock()
	if n != 2 {
		t.Fatalf("posts = %d, want the retry to start", n)
	}
}

// The client acks a failed result and the admit record is deleted. The
// continuation mark used to say only "admitted", so the next 继续 was told
// the handoff had already finished and the same login could not be tried
// again. The failure stays on the mark, and the press starts once more.
func TestDesktopContinuationRetriesAfterTheFailedRecordIsGone(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"async":true,"run":{"id":"run_%d","status":"running"}}`, n)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	ctx := context.Background()
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if _, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-fail", "", "return"); err != nil {
		t.Fatal(err)
	}
	svc.noteDesktopContinueFailed("tenant-a", "alice", "bot_alice", "turn-fail")
	svc.mu.Lock()
	for key := range svc.desktopAdmits {
		delete(svc.desktopAdmits, key)
	}
	svc.mu.Unlock()

	again, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-retry", "", "return")
	if err != nil || again.Joined || again.RunID != "run_2" {
		t.Fatalf("retry after the failed record was acked = %+v %v", again, err)
	}
	mu.Lock()
	n := posts
	mu.Unlock()
	if n != 2 {
		t.Fatalf("posts = %d, want the retry to start", n)
	}
}

// Auto-resume parks while the person still has the keyboard. 验证完成，继续
// joins that parked continuation and gives the keyboard back; the parked
// task has to start on that press, not on the next 30s sweep, and the press
// itself must not start a second one.
func TestDesktopContinuationReturnStartsTheParkedOne(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprintf(w, `{"async":true,"run":{"id":"run_%d","status":"running"}}`, n)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	ctx := context.Background()
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	parked, err := svc.AdmitDesktopMessage(WithDesktopContinuation(ctx), "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-parked")
	if err != nil || !parked.Queued || parked.RunID != "" {
		t.Fatalf("parked continuation = %+v %v", parked, err)
	}
	mu.Lock()
	if posts != 0 {
		mu.Unlock()
		t.Fatalf("posts while the keyboard is held = %d", posts)
	}
	mu.Unlock()
	pressed, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-press", "", "return")
	if err != nil || !pressed.Joined || pressed.RunID != "" {
		t.Fatalf("return joining the parked continuation = %+v %v", pressed, err)
	}
	mu.Lock()
	n := posts
	mu.Unlock()
	if n != 1 {
		t.Fatalf("posts after the return = %d, want the parked task started once", n)
	}
	// The run record is gone (it expired); the queue still says "running",
	// which is only its poll record. The handoff was continued once.
	svc.mu.Lock()
	for key := range svc.desktopAdmits {
		delete(svc.desktopAdmits, key)
	}
	svc.mu.Unlock()
	again, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-again", "", "return")
	if err != nil || !again.Joined || again.Reply.Text != desktopContinueDoneLine {
		t.Fatalf("press after the run record expired = %+v %v", again, err)
	}
	mu.Lock()
	n = posts
	mu.Unlock()
	if n != 1 {
		t.Fatalf("posts after the expired run = %d", n)
	}
	svc.mu.Lock()
	pin := svc.desktopHoldTurn[desktopViewKey("tenant-a", "alice")]
	svc.mu.Unlock()
	if pin != "turn-parked" {
		t.Fatalf("pin owner after the return = %q, want the parked continuation", pin)
	}
}

// A second 继续 joins the run already going. The pin stays with that run:
// this press never settles, so taking the pin would leave it uncleared.
func TestDesktopContinuationJoinLeavesThePinWithTheRunningTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_pin","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	ctx := context.Background()
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if _, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-run", "", "return"); err != nil {
		t.Fatal(err)
	}
	joined, err := svc.AdmitDesktopMessageAct(ctx, "tenant-a", "alice", "bot_alice", "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "execute", "turn-join", "", "return")
	if err != nil || !joined.Joined {
		t.Fatalf("second press = %+v %v", joined, err)
	}
	svc.mu.Lock()
	pin := svc.desktopHoldTurn[desktopViewKey("tenant-a", "alice")]
	svc.mu.Unlock()
	if pin != "turn-run" {
		t.Fatalf("pin owner = %q, want the running continuation", pin)
	}
}

func TestDesktopContinuationOnlyMatchesHandoffSentences(t *testing.T) {
	for _, text := range []string{"继续", "打开报表", "帮我登录一下", "登录完成，继续发帖", "验证完成，继续把标题改短一点"} {
		if desktopContinueText(text) {
			t.Fatalf("%q is not a handoff continuation", text)
		}
	}
	for _, text := range []string{"登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。", "验证完成，继续", " 登入完成，繼續 ", "Login done, continue."} {
		if !desktopContinueText(text) {
			t.Fatalf("%q is a handoff continuation", text)
		}
	}
}
