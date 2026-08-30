package llm

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsTransientTokenValidationError(t *testing.T) {
	oauthBody := []byte(`{"error":{"message":"The OAuth2 access token could not be validated."}}`)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{
			name: "http 403 oauth body",
			err:  &HTTPStatusError{StatusCode: http.StatusForbidden, Body: oauthBody},
			want: true,
		},
		{
			name: "http 401 invalid_token code",
			err:  &HTTPStatusError{StatusCode: http.StatusUnauthorized, Body: []byte(`{"error":"invalid_token"}`)},
			want: true,
		},
		{
			name: "user-facing wrapped 403",
			err:  errors.New("LLM call failed: 模型服务拒绝访问，账号可能被限制、额度不足或无权使用该模型 (HTTP 403): The OAuth2 access token could not be validated."),
			want: true,
		},
		{
			name: "generic 403 forbidden is not retryable",
			err:  &HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"code":"LLM_MODEL_FORBIDDEN","message":"no active model service entitlement"}`)},
			want: false,
		},
		{
			name: "generic 401 invalid api key is not retryable",
			err:  &HTTPStatusError{StatusCode: http.StatusUnauthorized, Body: []byte(`{"message":"invalid api key"}`)},
			want: false,
		},
		{
			name: "http 500 with token text still uses status gate",
			err:  &HTTPStatusError{StatusCode: http.StatusInternalServerError, Body: oauthBody},
			want: false,
		},
		{
			name: "oauth+invalid without token-validation phrasing",
			err:  errors.New("invalid oauth client configuration: missing token endpoint"),
			want: false,
		},
		{
			name: "refresh token expiry is not an access-token glitch",
			err:  errors.New("token refresh failed: refresh token has expired, please re-login"),
			want: false,
		},
		{
			name: "invalid_token code with refresh-token expiry message",
			err:  errors.New(`{"code":"invalid_token","message":"refresh token has expired"}`),
			want: false,
		},
		{
			name: "plain token-has-expired access token",
			err:  errors.New("HTTP 401: access token has expired"),
			want: true,
		},
		{
			name: "plain timeout is not a token error",
			err:  errors.New("context deadline exceeded"),
			want: false,
		},
		{
			name: "fmt wrap of HTTPStatusError",
			err:  fmt.Errorf("responses stream: %w", &HTTPStatusError{StatusCode: http.StatusForbidden, Body: oauthBody}),
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTransientTokenValidationError(tc.err); got != tc.want {
				t.Fatalf("IsTransientTokenValidationError() = %v, want %v (err=%v)", got, tc.want, tc.err)
			}
		})
	}
}
