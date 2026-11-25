package llm

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

type failoverTimeoutError struct{}

func (failoverTimeoutError) Error() string { return "request timed out" }
func (failoverTimeoutError) Timeout() bool { return true }

type failoverStatusTextError struct {
	status int
	text   string
}

func (e failoverStatusTextError) Error() string       { return e.text }
func (e failoverStatusTextError) HTTPStatusCode() int { return e.status }

func TestProviderFailoverEligible(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "rate limited", err: &HTTPStatusError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "gateway unavailable", err: &HTTPStatusError{StatusCode: http.StatusServiceUnavailable}, want: true},
		{name: "request timeout status", err: &HTTPStatusError{StatusCode: http.StatusRequestTimeout}, want: true},
		{name: "deterministic bad request", err: &HTTPStatusError{StatusCode: http.StatusBadRequest}, want: false},
		{name: "unauthorized stays on auth refresh", err: &HTTPStatusError{StatusCode: http.StatusUnauthorized}, want: false},
		{name: "dial refused", err: errors.New("dial tcp 127.0.0.1:1: connectex: connection refused"), want: true},
		{name: "connection reset", err: errors.New("read tcp: connection reset by peer"), want: true},
		{name: "windows forcibly closed", err: errors.New("wsarecv: An existing connection was forcibly closed by the remote host."), want: true},
		{name: "http2 connection lost", err: errors.New("http2: client connection lost"), want: true},
		{name: "wrapped text 503", err: errors.New("remote returned HTTP 503: service unavailable"), want: true},
		{name: "wrapped text rate limit", err: errors.New("rate limit exceeded"), want: true},
		{name: "wrapped text 400", err: errors.New("HTTP 400: invalid request"), want: false},
		{name: "typed 400 with misleading text", err: failoverStatusTextError{status: 400, text: "HTTP 400: rate limit header missing"}, want: false},
		{name: "bare EOF", err: errors.New("EOF"), want: true},
		{name: "wrapped EOF", err: errors.New("Post https://example/v1: EOF"), want: true},
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "unrelated eof substring", err: errors.New("thereof"), want: false},
		{name: "net timeout", err: failoverTimeoutError{}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProviderFailoverEligible(tt.err); got != tt.want {
				t.Fatalf("ProviderFailoverEligible(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
