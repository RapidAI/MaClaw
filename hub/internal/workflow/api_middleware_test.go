package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Mock TokenValidator ---

type mockTokenValidator struct {
	tokens map[string]string // token -> userID
}

func (m *mockTokenValidator) ValidateToken(_ context.Context, token string) (string, error) {
	if userID, ok := m.tokens[token]; ok {
		return userID, nil
	}
	return "", errors.New("invalid token")
}

// --- Auth Middleware Tests ---

func TestAuthMiddleware_ValidToken(t *testing.T) {
	validator := &mockTokenValidator{
		tokens: map[string]string{"valid-token-123": "user_abc"},
	}
	middleware := AuthMiddleware(validator)

	var capturedUserID string
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = getUserIDFromContext(r)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedUserID != "user_abc" {
		t.Errorf("expected user_abc, got %q", capturedUserID)
	}
}

func TestAuthMiddleware_MissingAuthHeader(t *testing.T) {
	validator := &mockTokenValidator{tokens: map[string]string{}}
	middleware := AuthMiddleware(validator)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	validator := &mockTokenValidator{
		tokens: map[string]string{"valid-token": "user1"},
	}
	middleware := AuthMiddleware(validator)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MalformedAuthHeader(t *testing.T) {
	validator := &mockTokenValidator{tokens: map[string]string{}}
	middleware := AuthMiddleware(validator)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	tests := []struct {
		name   string
		header string
	}{
		{"no prefix", "token-without-bearer-prefix"},
		{"basic auth", "Basic dXNlcjpwYXNz"},
		{"empty bearer", "Bearer "},
		{"bearer lowercase", "bearer valid-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_SetsXOwnerIDHeader(t *testing.T) {
	validator := &mockTokenValidator{
		tokens: map[string]string{"tok": "user_xyz"},
	}
	middleware := AuthMiddleware(validator)

	var ownerHeader string
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		ownerHeader = r.Header.Get("X-Owner-ID")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if ownerHeader != "user_xyz" {
		t.Errorf("expected X-Owner-ID=user_xyz, got %q", ownerHeader)
	}
}

// --- Rate Limiter Tests ---

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(100, 10)

	// First 10 requests should be allowed (burst).
	for i := 0; i < 10; i++ {
		if !rl.Allow("client1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_DeniesOverBurst(t *testing.T) {
	rl := NewRateLimiter(100, 10)

	// Exhaust the burst.
	for i := 0; i < 10; i++ {
		rl.Allow("client1")
	}

	// Next request should be denied.
	if rl.Allow("client1") {
		t.Fatal("request should be denied after burst exhausted")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(60, 5) // 1 token per second

	// Exhaust the burst.
	for i := 0; i < 5; i++ {
		rl.Allow("client1")
	}

	// Should be denied now.
	if rl.Allow("client1") {
		t.Fatal("should be denied immediately after burst")
	}

	// Manually advance the bucket's lastRefill to simulate time passing.
	rl.mu.Lock()
	rl.buckets["client1"].lastRefill = time.Now().Add(-2 * time.Second)
	rl.mu.Unlock()

	// After 2 seconds at 1 token/sec, should have ~2 tokens.
	if !rl.Allow("client1") {
		t.Fatal("should be allowed after refill")
	}
}

func TestRateLimiter_IsolatesClients(t *testing.T) {
	rl := NewRateLimiter(100, 3)

	// Exhaust client1's burst.
	for i := 0; i < 3; i++ {
		rl.Allow("client1")
	}

	// client1 should be denied.
	if rl.Allow("client1") {
		t.Fatal("client1 should be denied")
	}

	// client2 should still be allowed.
	if !rl.Allow("client2") {
		t.Fatal("client2 should be allowed")
	}
}

func TestRateLimiter_TokensCapAtBurst(t *testing.T) {
	rl := NewRateLimiter(6000, 5) // 100 tokens/sec, burst 5

	// Simulate a long idle period by setting lastRefill far in the past.
	rl.mu.Lock()
	rl.buckets["client1"] = &tokenBucket{
		tokens:     0,
		lastRefill: time.Now().Add(-1 * time.Hour),
	}
	rl.mu.Unlock()

	// Even after a long idle, tokens should cap at burst (5).
	for i := 0; i < 5; i++ {
		if !rl.Allow("client1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow("client1") {
		t.Fatal("6th request should be denied (capped at burst)")
	}
}

// --- Rate Limit Middleware Tests ---

func TestRateLimitMiddleware_AllowsRequest(t *testing.T) {
	limiter := NewRateLimiter(100, 10)
	middleware := RateLimitMiddleware(limiter)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = setUserIDInContext(req, "user1")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if !called {
		t.Fatal("handler should be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_Returns429WhenExceeded(t *testing.T) {
	limiter := NewRateLimiter(100, 2) // burst of 2
	middleware := RateLimitMiddleware(limiter)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Exhaust the burst.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = setUserIDInContext(req, "user1")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d should succeed", i+1)
		}
	}

	// Third request should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = setUserIDInContext(req, "user1")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After: 60, got %q", rec.Header().Get("Retry-After"))
	}
}

func TestRateLimitMiddleware_FallsBackToRemoteAddr(t *testing.T) {
	limiter := NewRateLimiter(100, 1)
	middleware := RateLimitMiddleware(limiter)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// First request without user ID in context — uses RemoteAddr.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request should succeed, got %d", rec.Code)
	}

	// Second request from same remote addr should be rate limited.
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rate limited, got %d", rec.Code)
	}
}

// --- Context Helper Tests ---

func TestContextHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// No user ID in context initially.
	if got := getUserIDFromContext(req); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Set user ID.
	req = setUserIDInContext(req, "user_123")
	if got := getUserIDFromContext(req); got != "user_123" {
		t.Errorf("expected user_123, got %q", got)
	}
}
