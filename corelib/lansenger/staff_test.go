package lansenger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetStaffBasicInfoAndResolveCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/apptoken/create") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/v1/staffs/") && strings.HasSuffix(r.URL.Path, "/fetch") {
			hits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"name": "李四", "orgId": "1", "orgName": "Org", "status": 1},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := NewGateway(Config{AppID: "a", AppSecret: "s", ApiGatewayURL: srv.URL}, nil)
	info, err := gw.GetStaffBasicInfo(context.Background(), "staff-abc")
	if err != nil {
		t.Fatalf("GetStaffBasicInfo: %v", err)
	}
	if info.Name != "李四" || info.StaffID != "staff-abc" {
		t.Fatalf("info = %+v", info)
	}

	if got := gw.ResolveStaffDisplayName(context.Background(), "staff-abc"); got != "李四" {
		t.Fatalf("resolve = %q", got)
	}
	// Second resolve must hit cache (no extra HTTP).
	before := hits.Load()
	if got := gw.ResolveStaffDisplayName(context.Background(), "staff-abc"); got != "李四" {
		t.Fatalf("cached resolve = %q", got)
	}
	if hits.Load() != before {
		t.Fatalf("expected cache hit, hits %d -> %d", before, hits.Load())
	}

	msg, ok := gw.EnrichIncomingSenderName(context.Background(), IncomingMessage{FromUserID: "staff-abc"})
	if !ok || msg.SenderName != "李四" {
		t.Fatalf("enrich = %#v ok=%v", msg, ok)
	}
	// Already has usable name: no change.
	msg2, ok2 := gw.EnrichIncomingSenderName(context.Background(), IncomingMessage{FromUserID: "staff-abc", SenderName: "王五"})
	if ok2 || msg2.SenderName != "王五" {
		t.Fatalf("should not overwrite existing name: %#v ok=%v", msg2, ok2)
	}
}

func TestResolveStaffDisplayNameSingleflight(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{})
	var startOnce sync.Once
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/apptoken/create") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/v1/staffs/") {
			hits.Add(1)
			startOnce.Do(func() { close(started) })
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"name": "赵六"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := NewGateway(Config{AppID: "a", AppSecret: "s", ApiGatewayURL: srv.URL}, nil)
	const n = 8
	results := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			results <- gw.ResolveStaffDisplayName(context.Background(), "staff-sf")
		}()
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("lookup did not start")
	}
	// Let goroutines pile onto singleflight before the HTTP handler returns.
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < n; i++ {
		if got := <-results; got != "赵六" {
			t.Fatalf("result %d = %q", i, got)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 staff HTTP hit via singleflight, got %d", hits.Load())
	}
}

func TestResolveStaffDisplayNameNegativeCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/apptoken/create") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/v1/staffs/") {
			hits.Add(1)
			// Stable business denial (not token/network): must negative-cache.
			// Avoid codes treated as token-expired (40001/40014/42001).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 10003, "errMsg": "staff not found",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := NewGateway(Config{AppID: "a", AppSecret: "s", ApiGatewayURL: srv.URL}, nil)
	if got := gw.ResolveStaffDisplayName(context.Background(), "missing"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	before := hits.Load()
	if got := gw.ResolveStaffDisplayName(context.Background(), "missing"); got != "" {
		t.Fatalf("neg cache want empty, got %q", got)
	}
	if hits.Load() != before {
		t.Fatalf("negative cache should skip HTTP, hits %d -> %d", before, hits.Load())
	}
	// Force expiry of neg entry and ensure we can try again.
	gw.staffNames.mu.Lock()
	for k, ent := range gw.staffNames.m {
		ent.at = time.Now().Add(-staffNameCacheNegTTL - time.Second)
		gw.staffNames.m[k] = ent
	}
	gw.staffNames.mu.Unlock()
	_ = gw.ResolveStaffDisplayName(context.Background(), "missing")
	if hits.Load() <= before {
		t.Fatalf("after neg TTL expiry expected another HTTP hit, hits=%d", hits.Load())
	}
}

func TestUsableStaffDisplayName(t *testing.T) {
	if got := usableStaffDisplayName("s1", "张三"); got != "张三" {
		t.Fatalf("got %q", got)
	}
	if got := usableStaffDisplayName("s1", "s1"); got != "" {
		t.Fatalf("echo id should reject, got %q", got)
	}
	if got := usableStaffDisplayName("s1", "  "); got != "" {
		t.Fatalf("blank should reject, got %q", got)
	}
}

func TestIsTransientStaffLookupError(t *testing.T) {
	if !isTransientStaffLookupError(context.DeadlineExceeded) {
		t.Fatal("deadline should be transient")
	}
	if isTransientStaffLookupError(&APIError{Code: 403, Msg: "no permission"}) {
		t.Fatal("business API error should not be transient")
	}
	if !isTransientStaffLookupError(fmt.Errorf("lansenger API HTTP 503: unavailable")) {
		t.Fatal("5xx should be transient")
	}
}

func TestResolveStaffDisplayNameDoesNotNegCacheTransient(t *testing.T) {
	var hits atomic.Int32
	// Fail token issuance so GetStaffBasicInfo errors without multi-retry 503 burn.
	// Token/network failures are transient and must not negative-cache.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/apptoken/create") {
			hits.Add(1)
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := NewGateway(Config{AppID: "a", AppSecret: "s", ApiGatewayURL: srv.URL}, nil)
	// Bound each attempt so the test stays fast (token path still retries internally).
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if got := gw.ResolveStaffDisplayName(ctx, "staff-t"); got != "" {
		t.Fatalf("want empty on token blip, got %q", got)
	}
	before := hits.Load()
	if before == 0 {
		t.Fatal("expected at least one token attempt")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel2()
	if got := gw.ResolveStaffDisplayName(ctx2, "staff-t"); got != "" {
		t.Fatalf("want empty again, got %q", got)
	}
	if hits.Load() <= before {
		t.Fatalf("transient failure must not neg-cache; hits %d -> %d", before, hits.Load())
	}
}

func TestResolveStaffDisplayNameSkipsWhenUnconfigured(t *testing.T) {
	gw := NewGateway(Config{}, nil)
	if got := gw.ResolveStaffDisplayName(context.Background(), "staff-1"); got != "" {
		t.Fatalf("unconfigured gateway must not invent names, got %q", got)
	}
	msg, ok := gw.EnrichIncomingSenderName(context.Background(), IncomingMessage{FromUserID: "staff-1"})
	if ok || msg.SenderName != "" {
		t.Fatalf("enrich must no-op when unconfigured: %#v ok=%v", msg, ok)
	}
}

func TestProcessEventEnrichesGroupSenderNameOnly(t *testing.T) {
	var staffHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v1/apptoken/create"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"appToken": "tok", "expiresIn": 7200},
			})
		case strings.Contains(r.URL.Path, "/v1/staffs/"):
			staffHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 0, "errMsg": "ok",
				"data": map[string]any{"name": "周七"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var got IncomingMessage
	gw := NewGateway(Config{AppID: "a", AppSecret: "s", ApiGatewayURL: srv.URL}, func(msg IncomingMessage) {
		got = msg
	})

	// Group: missing senderName → directory lookup.
	gw.handleWSMessage([]byte(`{"events":[{"eventType":"bot_group_message","data":{"from":"staff-g","conversationId":"g1","msgType":"text","msgData":{"text":{"content":"hi"}}}}]}`))
	if got.SenderName != "周七" {
		t.Fatalf("group should enrich name, got %#v hits=%d", got, staffHits.Load())
	}
	if staffHits.Load() != 1 {
		t.Fatalf("expected 1 staff hit, got %d", staffHits.Load())
	}

	// Private: skip staff lookup (no group quote header).
	before := staffHits.Load()
	gw.handleWSMessage([]byte(`{"events":[{"eventType":"bot_private_message","data":{"from":"staff-p","msgType":"text","msgData":{"text":{"content":"hi"}}}}]}`))
	if got.FromUserID != "staff-p" {
		t.Fatalf("expected private message, got %#v", got)
	}
	if got.SenderName != "" {
		t.Fatalf("private must not enrich senderName, got %q", got.SenderName)
	}
	if staffHits.Load() != before {
		t.Fatalf("private must not call staff API, hits %d -> %d", before, staffHits.Load())
	}
}
