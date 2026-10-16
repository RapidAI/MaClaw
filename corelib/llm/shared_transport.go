package llm

import (
	"net/http"
	"time"
)

// sharedLLMTransport is the process-wide connection pool for LLM API hosts.
//
// http.DefaultTransport (and every &http.Transport{TLSClientConfig: ...}
// literal in this codebase) keeps at most 2 idle connections per host. This
// app sustains many concurrent calls to the same hub (agent streams, intent
// classification, memory maintenance, health pings), so the idle pool churns
// constantly and nearly every call pays a fresh TCP+TLS handshake — measured
// hub latency is already seconds per call, so an extra ~0.3–1s of handshake
// on most requests is a real regression. Cloning DefaultTransport preserves
// its proxy, HTTP/2, and timeout defaults while enlarging the per-host idle
// pool so a finished call leaves its connection behind for the next one.
var sharedLLMTransport = newSharedLLMTransport()

func newSharedLLMTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	t := base.Clone()
	if t.MaxIdleConnsPerHost < 32 {
		t.MaxIdleConnsPerHost = 32
	}
	return t
}

// SharedHTTPClient is a client over sharedLLMTransport for call sites that
// would otherwise fall back to http.DefaultClient. It carries no Timeout and
// no Jar — the same semantics as &http.Client{} and http.DefaultClient — so
// per-request deadlines stay context-scoped. Callers that mutate the client
// (CheckRedirect etc.) must clone it first; HTTPClientForRequestContext
// already does.
var SharedHTTPClient = &http.Client{Transport: sharedLLMTransport}

// NewSharedHTTPClient returns a fresh http.Client wrapper over the shared
// LLM transport. Use it where a call site creates its own &http.Client{} just
// to pass an explicit client: the connection pool is shared, so client-local
// mutations can never poison other callers.
func NewSharedHTTPClient() *http.Client {
	return &http.Client{Transport: sharedLLMTransport}
}
