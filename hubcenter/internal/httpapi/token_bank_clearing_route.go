package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Routing money operations to one node.
//
// The ledger is replicated asynchronously — a batch is published every 200 rows
// or 15 seconds, whichever comes first — so at any instant two nodes hold two
// different views of the same balance. Idempotency does not close that gap: two
// hubs pulling with two different request ids produce two different ledger rows,
// and both insert succeeds, so the credits are over-issued and nothing reports
// an error. The only thing in this cluster that can serialise them is deciding
// them in one place.
//
// So every Token Bank operation with "exactly once" semantics is routed to a
// single clearing node:
//
//   - creating a gift link (it freezes credits, and its row is not replicated,
//     so it has to live where the settlement will look for it);
//   - claiming one (already routed to the link's origin node — and because the
//     link is now created on the clearing node, that is the same node);
//   - withdrawing, including the gift settlement that runs first.
//
// Everything else — settlement credits, share publishing, usage queries — stays
// local and asynchronous, because it happens once per proxied request and
// routing it would put the whole proxy path behind one node.
//
// An empty clearing node disables all of this. That is the default and it is
// deliberate: a single-node deployment has nothing to serialise against, and a
// cluster whose operator never configured one keeps behaving exactly as it does
// today rather than failing every pull.

const (
	tokenBankClearingProxyBodyLimit = 64 << 10
	// The response limit is a different animal from the request limit. The
	// routed surface includes the list endpoints (withdrawals, gift links,
	// admin credit shares), and those cap at 500 rows of ~350 bytes each —
	// roughly 200KB of JSON. Capping the response at the request's 64KB would
	// truncate a full list page and hand the caller a 200 with half a JSON
	// document: worse than an error, because the status says it worked.
	tokenBankClearingProxyResponseLimit = 4 << 20
	tokenBankClearingProxyTimeout       = 15 * time.Second
)

// readBounded reads at most limit+1 bytes so a stream that exactly fills the
// limit is distinguishable from one that overflows it.
func readBounded(r io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) > limit, nil
}

// tokenBankClearingNode answers which node must decide money operations, or ""
// when none is configured.
//
// The setting lives in the shared settings blob rather than in this node's own
// config file because it has to agree across the cluster: two nodes that
// disagree about where the clearing node is would each consider themselves
// local, which is the same as not routing at all.
func (h *SkillMarketHandlers) tokenBankClearingNode(ctx context.Context) string {
	if h == nil {
		return ""
	}
	settings, err := h.loadTokenBankSettings(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(settings.ClearingNodeID)
}

// tokenBankRouteToClearing forwards the request when this node is not the one
// that must decide it. It returns true when the response has already been
// written, so a handler can simply `if ... { return }`.
//
// It has to run before the handler reads the body: the body is forwarded byte
// for byte rather than re-encoded from a decoded struct, so a field this build
// does not know about still reaches the clearing node and is validated there.
func (h *SkillMarketHandlers) tokenBankRouteToClearing(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || r == nil {
		return false
	}
	// A trusted hop means a peer already routed this and it must run here.
	if h.tokenBankHopTrusted(r) {
		return false
	}
	target := h.tokenBankClearingNode(r.Context())
	if target == "" || strings.EqualFold(target, h.tokenBankNodeID()) {
		return false
	}
	dir := h.tokenBankOrigin
	if dir == nil {
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return true
	}
	base, reachable, _ := dir.AccessPeer(target)
	if !reachable || strings.TrimSpace(base) == "" {
		// Refusing is the point. Answering locally would be the over-issue bug
		// this routing exists to prevent, dressed up as high availability.
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return true
	}
	h.tokenBankProxyToPeer(w, r, dir, strings.TrimRight(strings.TrimSpace(base), "/"))
	return true
}

// tokenBankProxyToPeer replays one request onto a peer node and copies its
// answer back verbatim, so the client cannot tell it was proxied.
func (h *SkillMarketHandlers) tokenBankProxyToPeer(w http.ResponseWriter, r *http.Request, dir tokenBankOriginDirectory, base string) {
	body, oversized, err := readBounded(r.Body, tokenBankClearingProxyBodyLimit)
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return
	}
	if oversized {
		// Forwarding a truncated body would come back as a misleading 400
		// from the clearing node's JSON decoder. Refuse here, where the
		// actual cause is visible.
		tbError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "token bank request body exceeds the clearing proxy limit")
		return
	}
	target := base + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return
	}
	// The caller's credentials have to travel with the request: the peer
	// re-authenticates it exactly as this node would have. A hop that carried
	// no credentials would let an unauthenticated caller reach the clearing node
	// by bouncing off any replica.
	for name, values := range r.Header {
		switch strings.ToLower(name) {
		case "content-length", "accept-encoding", "connection", "host":
			continue
		}
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	req.Header.Set(tokenBankPeerHopHeader, "1")
	if secret := dir.ClusterSecret(); secret != "" {
		req.Header.Set(tokenBankPeerSecretHeader, secret)
	}

	client := &http.Client{
		Timeout: tokenBankClearingProxyTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return
	}
	defer resp.Body.Close()
	answer, oversized, err := readBounded(resp.Body, tokenBankClearingProxyResponseLimit)
	if err != nil {
		tbError(w, http.StatusServiceUnavailable, "clearing_unreachable", tokenBankWithdrawRetryMessage)
		return
	}
	if oversized {
		// A truncated 200 is the worst way to fail: the status tells the
		// caller it succeeded and the body is garbage. Answer with an
		// infrastructure error instead and let the client retry.
		tbError(w, http.StatusBadGateway, "clearing_response_truncated", "clearing node response exceeds the proxy limit")
		return
	}
	// Copy the peer's headers across, not just its body. Dropping them is not
	// cosmetic: the gift landing page sets Content-Security-Policy and
	// Cache-Control: no-store, and a proxied response that lost them would be
	// served without its XSS defence and cached by whoever is in front.
	//
	// Hop-by-hop and framing headers are skipped because they describe this
	// connection, not the payload, and re-emitting them would corrupt the
	// response (Go sets Content-Length itself from what we write).
	for name, values := range resp.Header {
		switch strings.ToLower(name) {
		case "content-length", "transfer-encoding", "connection", "keep-alive",
			"upgrade", "trailer", "te":
			continue
		}
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(answer)
}
