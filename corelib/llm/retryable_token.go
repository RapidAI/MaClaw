package llm

import (
	"errors"
	"net/http"
	"strings"
)

// IsTransientTokenValidationError reports OAuth/access-token validation
// failures that are worth retrying with backoff (and a credential refresh).
//
// Providers (notably xAI OAuth) sometimes return HTTP 401/403
// "The OAuth2 access token could not be validated" during token rotation,
// clock skew, or a brief auth-service glitch. Permanent denials (no
// entitlement, wrong model, generic forbidden) stay non-retryable.
func IsTransientTokenValidationError(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		if !isRetryableTokenValidationStatus(httpErr.StatusCode) {
			return false
		}
		return tokenValidationTextLooksTransient(string(httpErr.Body))
	}
	return tokenValidationTextLooksTransient(err.Error())
}

func isRetryableTokenValidationStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func tokenValidationTextLooksTransient(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	fields := extractLLMErrorFields([]byte(s))
	if tokenValidationLooksLikeRefreshFailure(s) || tokenValidationLooksLikeRefreshFailure(fields.Message) {
		return false
	}
	if tokenValidationCodeLooksTransient(fields.Code) || tokenValidationCodeLooksTransient(fields.Type) {
		return true
	}
	return tokenValidationMessageLooksTransient(fields.Message) || tokenValidationMessageLooksTransient(s)
}

func tokenValidationLooksLikeRefreshFailure(s string) bool {
	s = strings.ToLower(s)
	if !strings.Contains(s, "refresh token") && !strings.Contains(s, "refresh_token") {
		return false
	}
	return strings.Contains(s, "expired") || strings.Contains(s, "invalid_grant")
}

func tokenValidationCodeLooksTransient(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_token", "token_expired", "expired_token":
		return true
	default:
		return false
	}
}

func tokenValidationMessageLooksTransient(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, phrase := range []string{
		"oauth2 access token could not be validated",
		"access token could not be validated",
		"token could not be validated",
		"could not validate the access token",
		"could not validate access token",
		"token has expired",
		"access token expired",
		"expired access token",
		"invalid access token",
	} {
		if strings.Contains(s, phrase) {
			return true
		}
	}
	return strings.Contains(s, "invalid_token") && !strings.Contains(s, "invalid_token_")
}
