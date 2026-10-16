package ha

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	pullOpsResponseBodyLimit = 128 << 20
	maxPullBatchSize         = 50000
	maxConcurrentPeerSyncs   = 16
	peerPullTimeout          = 120 * time.Second
)

// errPullResponseBodyTooLarge reports that the pull response body reached the
// client's read limit, so the JSON payload is truncated and undecodable. The
// caller treats it like a timeout: shrink the batch limit and retry. This is
// what used to deadlock hc-1 for ~25h (2026-09-25): a backlog of ~1MB
// llm_official_class_head ops pushed the response past the limit, the
// LimitReader cut it mid-JSON, the decoder returned "unexpected EOF", and the
// retry loop — which only downgraded on timeout errors — kept requesting the
// same oversized batch forever while the peer's prune window deleted the ops
// behind the stuck cursor.
var errPullResponseBodyTooLarge = errors.New("pull ops response body exceeded limit")

type PullOpsResponse struct {
	NodeID       string            `json:"node_id"`
	Ops          []*store.HASyncOp `json:"ops"`
	NextAfterSeq int64             `json:"next_after_seq"`
	HasMore      bool              `json:"has_more"`
	MaxSeq       int64             `json:"max_seq"`
	// MinSeq is the oldest seq still present in the peer's op log. A cursor
	// below it means some ops were pruned before this node pulled them. That
	// is convergence-safe (pruning never removes the newest op of an entity,
	// so the retained log still carries every entity's latest state), but it
	// is logged so operators can see which peer is falling behind.
	MinSeq int64 `json:"min_seq"`
}

type Syncer struct {
	svc       *Service
	client    *http.Client
	interval  time.Duration
	limit     int
	bodyLimit int64
	slots     chan struct{}
	mu        sync.Mutex
	running   map[string]bool
}

// newHAHTTPClient builds an HTTP client that cannot wedge on a half-dead
// HTTP/2 connection. Over https the default Transport negotiates h2 via ALPN;
// when the underlying connection dies silently (peer reboot, NAT timeout,
// cross-border path blackholed) h2 streams block forever — the request's
// Client.Timeout cancel does not reliably interrupt a wedged h2 roundTrip or
// body Read. Verified live 2026-09-25 (run18): two hubcenter syncer goroutines
// stuck >8min on one h2 connection with 45KB un-ACKed in the socket Send-Q,
// zero packets on the wire, no error surfaced anywhere.
// HTTP/1.1 uses one connection per request, so ResponseHeaderTimeout and
// Client.Timeout both fire deterministically and a dead connection fails
// fast instead of hanging the syncer goroutine (beginPeerSync then blocks
// that peer silently until the goroutine returns).
func newHAHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          8,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
			// Independent cap on waiting for response headers: covers the
			// "request sent, peer never answers" half-open case.
			ResponseHeaderTimeout: 60 * time.Second,
			// Pin ALPN to http/1.1: with a custom TLSClientConfig and
			// ForceAttemptHTTP2 unset, Go would otherwise still offer h2.
			TLSClientConfig: &tls.Config{
				NextProtos: []string{"http/1.1"},
			},
			ForceAttemptHTTP2: false,
		},
	}
}

func NewSyncer(svc *Service, interval time.Duration, limit int) *Syncer {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > maxPullBatchSize {
		limit = maxPullBatchSize
	}
	return &Syncer{
		svc:       svc,
		client:    newHAHTTPClient(peerPullTimeout),
		interval:  interval,
		limit:     limit,
		bodyLimit: pullOpsResponseBodyLimit,
		slots:     make(chan struct{}, maxConcurrentPeerSyncs),
		running:   make(map[string]bool),
	}
}

func (s *Syncer) Run(ctx context.Context) {
	if s == nil || s.svc == nil || s.svc.cursors == nil || s.svc.ops == nil {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.syncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAll(ctx)
		}
	}
}

func (s *Syncer) syncAll(ctx context.Context) {
	for _, peer := range s.svc.listPeerStates() {
		peer := peer
		if !s.acquireSyncSlot() {
			continue
		}
		if !s.beginPeerSync(peer.NodeID) {
			s.releaseSyncSlot()
			continue
		}
		go func() {
			defer s.releaseSyncSlot()
			s.syncPeer(ctx, peer)
		}()
	}
}

func (s *Syncer) acquireSyncSlot() bool {
	if s == nil || s.slots == nil {
		return true
	}
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Syncer) releaseSyncSlot() {
	if s == nil || s.slots == nil {
		return
	}
	select {
	case <-s.slots:
	default:
	}
}

func (s *Syncer) beginPeerSync(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || nodeID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running = make(map[string]bool)
	}
	if s.running[nodeID] {
		return false
	}
	s.running[nodeID] = true
	return true
}

func (s *Syncer) endPeerSync(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if s == nil || nodeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, nodeID)
}

func (s *Syncer) syncPeer(ctx context.Context, peer *PeerRuntimeState) {
	defer func() {
		if peer != nil {
			s.endPeerSync(peer.NodeID)
		}
	}()
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" || peerTransportURL(peer) == "" {
		return
	}
	cursor, err := s.svc.cursors.Get(ctx, peer.NodeID)
	if err != nil {
		s.svc.markPeerError(peer.NodeID, err.Error())
		s.recordPeerCursorError(ctx, peer.NodeID, 0, nil, err.Error())
		return
	}
	afterSeq := int64(0)
	if cursor != nil {
		afterSeq = cursor.LastPulledSeq
	}
	for {
		resp, err := s.pullOps(ctx, peer, afterSeq)
		if err != nil {
			s.svc.markPeerError(peer.NodeID, err.Error())
			s.recordPeerCursorError(ctx, peer.NodeID, afterSeq, cursor, err.Error())
			return
		}
		now := time.Now().UTC()
		if afterSeq+1 < resp.MinSeq {
			log.Printf("[hubcenter][ha] peer %s op log pruned below cursor: after_seq=%d min_seq=%d (missed intermediate ops; newest op per entity is retained so state still converges)", peer.NodeID, afterSeq, resp.MinSeq)
		}
		if len(resp.Ops) == 0 {
			nextSeq := resp.NextAfterSeq
			if nextSeq < afterSeq {
				nextSeq = afterSeq
			}
			// Skip cursor write if position hasn't changed — reduces disk IO
			// in idle clusters where peers have no new ops.
			if cursor != nil && cursor.LastPulledSeq == nextSeq {
				backlog := int64(0)
				if resp.MaxSeq > nextSeq {
					backlog = resp.MaxSeq - nextSeq
				}
				s.svc.updatePeerSync(peer.NodeID, backlog)
				return
			}
			_ = s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{PeerNodeID: peer.NodeID, LastPulledSeq: nextSeq, LastPulledAt: &now, LastSuccessAt: &now, LastError: ""})
			backlog := int64(0)
			if resp.MaxSeq > nextSeq {
				backlog = resp.MaxSeq - nextSeq
			}
			s.svc.updatePeerSync(peer.NodeID, backlog)
			return
		}
		if err := s.svc.ApplyRemoteOps(ctx, resp.Ops); err != nil {
			// Persist and log: a failing apply aborts the whole batch and the
			// cursor stops advancing, so without this the peer silently falls
			// behind forever (hc-1 sat ~51k ops / ~10h behind for days).
			s.svc.markPeerError(peer.NodeID, err.Error())
			s.recordPeerCursorError(ctx, peer.NodeID, afterSeq, cursor, err.Error())
			log.Printf("[hubcenter][ha] apply pulled ops from %s after_seq=%d count=%d: %v", peer.NodeID, afterSeq, len(resp.Ops), err)
			return
		}
		lastSeq := resp.Ops[len(resp.Ops)-1].Seq
		if err := s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{PeerNodeID: peer.NodeID, LastPulledSeq: lastSeq, LastPulledAt: &now, LastSuccessAt: &now, LastError: ""}); err != nil {
			s.svc.markPeerError(peer.NodeID, err.Error())
			return
		}
		backlog := int64(0)
		if resp.MaxSeq > lastSeq {
			backlog = resp.MaxSeq - lastSeq
		}
		s.svc.updatePeerSync(peer.NodeID, backlog)
		afterSeq = lastSeq
		if !resp.HasMore {
			return
		}
	}
}

func (s *Syncer) recordPeerCursorError(ctx context.Context, nodeID string, lastSeq int64, cursor *store.HAPeerCursor, msg string) {
	if s == nil || s.svc == nil || s.svc.cursors == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	now := time.Now().UTC()
	if cursor == nil {
		current, err := s.svc.cursors.Get(ctx, nodeID)
		if err == nil {
			cursor = current
		}
	}
	if cursor != nil {
		if cursor.LastPulledSeq > lastSeq {
			lastSeq = cursor.LastPulledSeq
		}
		_ = s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{
			PeerNodeID:    nodeID,
			LastPulledSeq: lastSeq,
			LastPulledAt:  &now,
			LastSuccessAt: cursor.LastSuccessAt,
			LastError:     strings.TrimSpace(msg),
		})
		return
	}
	_ = s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{
		PeerNodeID:    nodeID,
		LastPulledSeq: lastSeq,
		LastPulledAt:  &now,
		LastError:     strings.TrimSpace(msg),
	})
}

func (s *Syncer) pullOps(ctx context.Context, peer *PeerRuntimeState, afterSeq int64) (*PullOpsResponse, error) {
	limit := s.limit
	if limit <= 0 {
		limit = 200
	}
	for {
		out, err := s.pullOpsWithLimit(ctx, peer, afterSeq, limit)
		if err == nil {
			return out, nil
		}
		if ctx.Err() != nil || !isHAPullShrinkable(err) || limit <= 1 {
			return nil, err
		}
		next := limit / 4
		if next < 1 {
			next = 1
		}
		if errors.Is(err, errPullResponseBodyTooLarge) {
			log.Printf("[hubcenter][ha] pull from %s response body too large (batch limit=%d), shrinking to %d", peer.NodeID, limit, next)
		}
		limit = next
	}
}

func (s *Syncer) pullOpsWithLimit(ctx context.Context, peer *PeerRuntimeState, afterSeq int64, limit int) (*PullOpsResponse, error) {
	base := peerTransportURL(peer)
	if base == "" {
		return nil, fmt.Errorf("no URL for hubcenter node %s", peer.NodeID)
	}
	u := base + "/api/internal/ha/ops?after_seq=" + strconv.FormatInt(afterSeq, 10) + "&limit=" + strconv.Itoa(limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if secret := strings.TrimSpace(s.svc.ClusterSecret()); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if err := s.svc.SignPeerRequest(req); err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull ops failed: %s", resp.Status)
	}
	// Fast path: a declared Content-Length over the limit proves truncation
	// without reading anything. Our own server and nginx both set the header
	// for buffered responses, so the oversized-pull case (138MB observed via
	// nginx, 2026-09-25) fails here instead of after downloading 128MiB.
	// Chunked responses (-1) fall through to readBodyWithLimit below.
	if resp.ContentLength > s.pullBodyLimit() {
		return nil, fmt.Errorf("%w: content-length=%d limit=%d batch=%d", errPullResponseBodyTooLarge, resp.ContentLength, s.pullBodyLimit(), limit)
	}
	// Read the body with explicit truncation detection: if the response
	// reaches the byte limit there is more data behind it, so the JSON is
	// incomplete. Instead of letting the decoder fail with a bare
	// "unexpected EOF" (which the retry loop cannot distinguish from a
	// genuine protocol error), surface a typed error the loop can act on.
	body, truncated, err := readBodyWithLimit(resp.Body, s.pullBodyLimit())
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, fmt.Errorf("%w: limit=%d batch=%d", errPullResponseBodyTooLarge, s.pullBodyLimit(), limit)
	}
	var out PullOpsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// readBodyWithLimit reads r fully but reports whether the limit cut the
// stream short. It reads up to limit+1 bytes so a body exactly at the limit
// is not misclassified as truncated.
func readBodyWithLimit(r io.Reader, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		limit = pullOpsResponseBodyLimit
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func (s *Syncer) pullBodyLimit() int64 {
	if s == nil || s.bodyLimit <= 0 {
		return pullOpsResponseBodyLimit
	}
	return s.bodyLimit
}

// isHAPullShrinkable reports whether retrying the pull with a smaller batch
// could succeed. Timeouts and oversized responses both qualify: a smaller
// batch produces a smaller payload and a shorter transfer. Plain HTTP errors
// (401, 404) and refused connections do not — shrinking cannot fix those.
// "unexpected EOF" is kept as a belt-and-braces match for intermediaries
// (nginx, proxies) cutting the stream before our own limit triggers.
func isHAPullShrinkable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errPullResponseBodyTooLarge) || isHAPullTimeout(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected eof")
}

func isHAPullTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
