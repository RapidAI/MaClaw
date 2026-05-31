package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const userIDContextKey contextKey = "user_id"

// TokenValidator validates Bearer tokens and returns the associated user ID.
// Implementations should check the token against the Hub's token store.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (userID string, err error)
}

// AuthMiddleware wraps an http.HandlerFunc with Bearer token authentication.
// It extracts the token from the Authorization header, validates it using the
// TokenValidator, and injects the user_id into the request context on success.
// On failure, it returns HTTP 401 with {"error": "invalid credentials"}.
func AuthMiddleware(validator TokenValidator) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeAuthError(w)
				return
			}

			userID, err := validator.ValidateToken(r.Context(), token)
			if err != nil || userID == "" {
				writeAuthError(w)
				return
			}

			// Inject user_id into request context and X-Owner-ID header
			// (for compatibility with existing handlers that read X-Owner-ID).
			r = setUserIDInContext(r, userID)
			r.Header.Set("X-Owner-ID", userID)
			next(w, r)
		}
	}
}

// extractBearerToken extracts the token from "Authorization: Bearer <token>" header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

// writeAuthError writes a 401 response with the standard error format.
func writeAuthError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
}

// --- Context helpers ---

// getUserIDFromContext extracts the authenticated user ID from the request context.
func getUserIDFromContext(r *http.Request) string {
	val := r.Context().Value(userIDContextKey)
	if val == nil {
		return ""
	}
	userID, _ := val.(string)
	return userID
}

// setUserIDInContext returns a new request with the user ID injected into its context.
func setUserIDInContext(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), userIDContextKey, userID)
	return r.WithContext(ctx)
}

// WithUserID returns a copy of ctx carrying the authenticated user ID under the
// package's private context key. It is the exported bridge that lets external
// auth middleware (e.g. the Hub's workflowUserAuth, which authenticates a VE
// machine and sets the X-Owner-ID header) establish the same authenticated
// identity that the context-based handlers — RuntimeAPI's
// handleInitiateWorkflow / handleConfirm / directory views, which read the
// caller via getUserIDFromContext — depend on.
//
// Without this, registering RuntimeAPI behind a header-only middleware would
// leave every context-reading handler seeing an empty user and returning 401.
// The header-reading handlers (InstanceAPI, DecisionAPI) continue to read
// X-Owner-ID unchanged, so populating both conventions is purely additive.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// --- Rate Limiter ---

// tokenBucket implements a single client's token bucket.
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimiter implements a per-client token bucket rate limiter.
// It allows a sustained rate of `rate` requests per minute with a burst of `burst`.
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*tokenBucket
	rate      int // tokens per minute
	burst     int // max burst (bucket capacity)
	gcCounter int
	gcEvery   int // run GC every N calls to Allow()
}

// NewRateLimiter creates a new RateLimiter with the specified rate (requests per minute)
// and burst (maximum tokens that can accumulate).
func NewRateLimiter(ratePerMinute, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    ratePerMinute,
		burst:   burst,
		gcEvery: 100,
	}
}

// Allow checks whether the client identified by clientID is allowed to make a request.
// Returns true if the request is allowed (a token was consumed), false if rate limited.
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Periodic GC: every gcEvery calls, remove idle buckets.
	rl.gcCounter++
	if rl.gcCounter >= rl.gcEvery {
		rl.gcCounter = 0
		rl.gcBuckets(now)
	}
	bucket, exists := rl.buckets[clientID]
	if !exists {
		bucket = &tokenBucket{
			tokens:     float64(rl.burst),
			lastRefill: now,
		}
		rl.buckets[clientID] = bucket
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	tokensToAdd := elapsed * (float64(rl.rate) / 60.0) // rate is per minute
	bucket.tokens += tokensToAdd
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastRefill = now

	// Try to consume a token.
	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}
	return false
}

// gcBuckets removes buckets that haven't been accessed in 5+ minutes.
// Must be called with rl.mu held.
func (rl *RateLimiter) gcBuckets(now time.Time) {
	const maxIdleTime = 5 * time.Minute
	for id, bucket := range rl.buckets {
		if now.Sub(bucket.lastRefill) > maxIdleTime {
			delete(rl.buckets, id)
		}
	}
}

// RateLimitMiddleware wraps an http.HandlerFunc with per-client rate limiting.
// It uses the authenticated user ID (from context) as the client identifier.
// On rate limit exceeded, it returns HTTP 429 with {"error": "rate limit exceeded"}.
func RateLimitMiddleware(limiter *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			clientID := getUserIDFromContext(r)
			if clientID == "" {
				// Fallback to remote address if no user ID in context.
				clientID = r.RemoteAddr
			}

			if !limiter.Allow(clientID) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}

			next(w, r)
		}
	}
}
