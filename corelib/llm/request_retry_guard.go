package llm

import (
	"context"
	"net/http"
)

// transparentRequestRetryContextKey marks a request boundary whose owner must
// see every model attempt. It is deliberately context-scoped rather than a
// provider configuration field: the caller that owns a request-local tool
// surface decides whether an SDK compatibility retry may silently create a
// successor request.
//
// In particular, the uncorrelated Coding compatibility belt must not let
// OpenAI-compat repair paths (tool-less/compact/max-token retries) bypass the
// shared RunLoop's render, delivery, quarantine, and telemetry lifecycle.
type transparentRequestRetryContextKey struct{}

// WithTransparentRequestRetriesDisabled returns a child context that disables
// SDK-local compatibility retries. The original request is still sent and its
// real provider error is returned to the caller, which may then apply its own
// request-bound policy. It does not manufacture a delivery proof or a retry
// identity.
func WithTransparentRequestRetriesDisabled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, transparentRequestRetryContextKey{}, true)
}

// TransparentRequestRetriesDisabled reports whether an owning request
// lifecycle has forbidden SDK-local successor requests.
func TransparentRequestRetriesDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(transparentRequestRetryContextKey{}).(bool)
	return disabled
}

// HTTPClientForRequestContext returns the client that may be used for one
// request-owner governed model attempt. The default net/http redirect policy
// is itself a hidden successor-request mechanism: a 307/308 can resend the
// original POST body to a different URL without returning to the owner that
// rendered the tool surface.
//
// When transparent retries are disabled, preserve the redirect response for
// the caller to classify, but never automatically follow it. This is scoped
// to the same request-owner policy as compatibility retries; ordinary callers
// retain their configured redirect behavior. A stopped redirect is not a
// proof that the first request was not delivered, and it does not create any
// transport correlation.
func HTTPClientForRequestContext(ctx context.Context, client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	if !TransparentRequestRetriesDisabled(ctx) {
		return client
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}
