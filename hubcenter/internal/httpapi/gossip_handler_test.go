package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type gossipTestEnv struct {
	handler http.Handler
	cache   *GossipCache
	token   string
}

func newGossipTestEnv(t *testing.T) *gossipTestEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gossip-test.db")
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	st := sqlite.NewStore(provider)
	adminSvc := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := &httpTestMailer{}
	hubSvc := hubs.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	entrySvc := entry.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)

	cachePath := filepath.Join(t.TempDir(), "gossip_snapshot.json.gz")
	gossipCache := NewGossipCache(st.Gossip, cachePath)

	handler := NewRouter(adminSvc, hubSvc, entrySvc, nil, nil, st.Gossip, gossipCache, nil)

	env := &gossipTestEnv{handler: handler, cache: gossipCache}

	// Setup admin and get token
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin", "password": "StrongPassword123!", "email": "admin@test.com",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin setup: %d %s", resp.Code, resp.Body.String())
	}
	resp = doJSONRequest(t, handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin", "password": "StrongPassword123!",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", resp.Code, resp.Body.String())
	}
	var loginData map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &loginData)
	env.token, _ = loginData["access_token"].(string)

	return env
}

func decodeJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode json: %v\nbody: %s", err, string(body))
	}
	return m
}

// TestGossipPublishBrowseCommentRateSnapshot covers the full gossip lifecycle:
// publish → browse → comment → rate → snapshot
func TestGossipPublishBrowseCommentRateSnapshot(t *testing.T) {
	env := newGossipTestEnv(t)

	// ── 1. Publish a post ──────────────────────────────────────────
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "test-machine-001",
		"user_email": "user@test.com",
		"content":    "老板又让加班了，MaClaw 替我写完了代码",
		"category":   "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", resp.Code, resp.Body.String())
	}
	pubData := decodeJSON(t, resp.Body.Bytes())
	post, _ := pubData["post"].(map[string]any)
	postID, _ := post["id"].(string)
	if postID == "" {
		t.Fatal("expected post id")
	}
	nickname, _ := post["nickname"].(string)
	if nickname == "" || len(nickname) < len("MaClaw-")+6 {
		t.Fatalf("expected valid nickname with 6-char hex suffix, got %q", nickname)
	}

	// ── 2. Browse posts ────────────────────────────────────────────
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", resp.Code, resp.Body.String())
	}
	browseData := decodeJSON(t, resp.Body.Bytes())
	posts, _ := browseData["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	total, _ := browseData["total"].(float64)
	if total != 1 {
		t.Fatalf("expected total=1, got %v", total)
	}

	// ── 3. Add a comment ───────────────────────────────────────────
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "test-machine-002",
		"post_id":    postID,
		"content":    "哈哈同感，我也是",
		"rating":     0,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", resp.Code, resp.Body.String())
	}

	// ── 4. Rate the post ───────────────────────────────────────────
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "test-machine-003",
		"post_id":    postID,
		"rating":     5,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("rate: %d %s", resp.Code, resp.Body.String())
	}

	// Verify score updated via browse
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse after rate: %d %s", resp.Code, resp.Body.String())
	}
	browseData = decodeJSON(t, resp.Body.Bytes())
	posts, _ = browseData["posts"].([]any)
	p0, _ := posts[0].(map[string]any)
	votes, _ := p0["votes"].(float64)
	if votes < 1 {
		t.Fatalf("expected votes >= 1, got %v", votes)
	}

	// ── 5. List comments ───────────────────────────────────────────
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/comments?post_id="+postID, nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("list comments: %d %s", resp.Code, resp.Body.String())
	}
	commData := decodeJSON(t, resp.Body.Bytes())
	comments, _ := commData["comments"].([]any)
	if len(comments) < 2 { // 1 comment + 1 rating comment
		t.Fatalf("expected >= 2 comments, got %d", len(comments))
	}

	// ── 6. Snapshot ────────────────────────────────────────────────
	// Snapshot is served from file, need to trigger cache refresh first via a new publish
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "test-machine-004",
		"content":    "第二条帖子",
		"category":   "news",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish 2: %d %s", resp.Code, resp.Body.String())
	}

	// Give async cache refresh a moment, then check snapshot
	// Since cache.Refresh is async (goroutine), we call it directly for test determinism
	// Instead, just verify the endpoint returns something (may be 503 if cache not ready)
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/snapshot", nil, "")
	// Accept 200 or 503 (cache may not have been generated yet in async goroutine)
	if resp.Code != http.StatusOK && resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("snapshot: %d %s", resp.Code, resp.Body.String())
	}
}

func TestGossipPublishValidation(t *testing.T) {
	env := newGossipTestEnv(t)

	tests := []struct {
		name string
		body map[string]any
		code int
	}{
		{"missing machine_id", map[string]any{"content": "hello"}, http.StatusBadRequest},
		{"empty content", map[string]any{"machine_id": "m1", "content": ""}, http.StatusBadRequest},
		{"invalid category", map[string]any{"machine_id": "m1", "content": "hi", "category": "invalid"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", tt.body, "")
			if resp.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestGossipCommentOnLockedPost(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "test post", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	pubData := decodeJSON(t, resp.Body.Bytes())
	postID := pubData["post"].(map[string]any)["id"].(string)

	// Admin locks the post
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/admin/gossip/lock", map[string]any{
		"id": postID, "locked": true,
	}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("lock: %d %s", resp.Code, resp.Body.String())
	}

	// Try to comment — should be forbidden
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "m2", "post_id": postID, "content": "should fail", "rating": 0,
	}, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked post comment, got %d: %s", resp.Code, resp.Body.String())
	}

	// Try to rate — should also be forbidden
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "m2", "post_id": postID, "rating": 3,
	}, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked post rate, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAdminGossipDeletePost(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "to be deleted", "category": "project",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	postID := decodeJSON(t, resp.Body.Bytes())["post"].(map[string]any)["id"].(string)

	// Admin delete
	resp = doJSONRequest(t, env.handler, http.MethodDelete, "/api/admin/gossip", map[string]any{"id": postID}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.Code, resp.Body.String())
	}

	// Browse should be empty
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	browseData := decodeJSON(t, resp.Body.Bytes())
	total, _ := browseData["total"].(float64)
	if total != 0 {
		t.Fatalf("expected 0 posts after delete, got %v", total)
	}
}

func TestGossipSnapshotETagSupport(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "etag test post", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}

	// Directly refresh cache for deterministic test (no time.Sleep)
	if err := env.cache.Refresh(context.Background()); err != nil {
		t.Fatalf("cache refresh: %v", err)
	}

	// First request — should get 200 with ETag
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/snapshot", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("snapshot first request: %d %s", resp.Code, resp.Body.String())
	}
	etag := resp.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header in snapshot response")
	}

	// Second request with If-None-Match — should get 304
	req := httptest.NewRequest(http.MethodGet, "/api/gossip/snapshot", nil)
	req.Header.Set("If-None-Match", etag)
	rr := httptest.NewRecorder()
	env.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotModified {
		t.Fatalf("expected 304 with matching ETag, got %d", rr.Code)
	}
}

func TestGossipDuplicateRatingPrevention(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "dup rating test", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	postID := decodeJSON(t, resp.Body.Bytes())["post"].(map[string]any)["id"].(string)

	// First rating — should succeed
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "rating": 4,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("first rate: %d %s", resp.Code, resp.Body.String())
	}

	// Second rating from same machine — should get 409
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "rating": 2,
	}, "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate rate, got %d: %s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	code, _ := body["code"].(string)
	if code != "ALREADY_RATED" {
		t.Fatalf("expected ALREADY_RATED code, got %q", code)
	}

	// Rating via comment endpoint with rating > 0 from same machine — should also get 409
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "content": "nice", "rating": 3,
	}, "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate comment-rating, got %d: %s", resp.Code, resp.Body.String())
	}

	// Comment without rating from same machine — should succeed
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "content": "just a comment", "rating": 0,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("comment without rating: %d %s", resp.Code, resp.Body.String())
	}

	// Different machine can still rate
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-002", "post_id": postID, "rating": 5,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("different machine rate: %d %s", resp.Code, resp.Body.String())
	}
}

func TestGossipRateLimiting(t *testing.T) {
	env := newGossipTestEnv(t)

	// The router uses 10 writes per 10 min per key.
	// We'll send 11 publish requests from the same IP to trigger 429.
	for i := 0; i < 10; i++ {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
			"machine_id": "rl-machine",
			"content":    "rate limit test " + time.Now().Format(time.RFC3339Nano),
			"category":   "news",
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("publish #%d: %d %s", i+1, resp.Code, resp.Body.String())
		}
	}

	// 11th request should be rate limited
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "rl-machine",
		"content":    "should be limited",
		"category":   "news",
	}, "")
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate-limited request, got %d: %s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	code, _ := body["code"].(string)
	if code != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED code, got %q", code)
	}
}
