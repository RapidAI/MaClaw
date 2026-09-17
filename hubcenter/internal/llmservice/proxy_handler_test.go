package llmservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestWriteProxyRequestErrorRetryAfterOnAllProvidersFailed(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProxyRequestError(rec, errors.New("all providers failed, last error: llmpool: provider opencode-3 circuit probe in flight"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want 5", got)
	}
}

func TestWriteProxyRequestErrorNoRetryAfterOnOtherErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProxyRequestError(rec, errors.New("billing reconciliation failed"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want unset", got)
	}
}

func TestProxyRetryAfterSecondsMatchesPoolState(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "probe in flight waits out probe budget",
			err:  fmt.Errorf("all providers failed, last error: %w", &llmpool.ResilienceError{ProviderID: "p1", State: "probe"}),
			want: "15",
		},
		{
			name: "open circuit hints next probe window",
			err:  fmt.Errorf("all providers failed, last error: %w", &llmpool.ResilienceError{ProviderID: "p1", State: "open", CooldownLeft: 12500 * time.Millisecond}),
			want: "13",
		},
		{
			name: "long cooldown is capped",
			err:  fmt.Errorf("all providers failed, last error: %w", &llmpool.ResilienceError{ProviderID: "p1", State: "open", CooldownLeft: 90 * time.Second}),
			want: "30",
		},
		{
			name: "unrelated failure keeps short backoff",
			err:  errors.New("all providers failed, last error: provider p1 is paused"),
			want: "5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := proxyRetryAfterSeconds(tc.err); got != tc.want {
				t.Fatalf("proxyRetryAfterSeconds = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteProxyRequestErrorRetryAfterFromResilienceState(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("all providers failed, last error: %w", &llmpool.ResilienceError{ProviderID: "p1", State: "probe"})
	writeProxyRequestError(rec, err)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "15" {
		t.Fatalf("Retry-After = %q, want 15", got)
	}
}

func TestProxyBeforeAttemptWaitsForSettledProbe(t *testing.T) {
	cfg := &ProxyConfig{Resilience: llmpool.NewResilienceController()}
	provider := &llmpool.ProviderConfig{ID: "p1", CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 40}
	cfg.Resilience.RecordFailureBackoff("p1", 1, 40, 200)
	// Wait out the cooldown; the first admitted attempt becomes the in-flight probe.
	deadline := time.Now().Add(2 * time.Second)
	for cfg.Resilience.BeforeAttempt("p1", 1, 40) != nil {
		if time.Now().After(deadline) {
			t.Fatal("cooldown did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A concurrent caller must observe the probe state.
	var re *llmpool.ResilienceError
	if err := cfg.Resilience.BeforeAttempt("p1", 1, 40); !errors.As(err, &re) || re.State != "probe" {
		t.Fatalf("expected probe-in-flight state, got %v", err)
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		cfg.Resilience.RecordSuccess("p1")
	}()
	start := time.Now()
	if err := proxyBeforeAttempt(context.Background(), cfg, provider); err != nil {
		t.Fatalf("proxyBeforeAttempt should settle after probe success: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("proxyBeforeAttempt took %v, expected prompt settle", elapsed)
	}
}
