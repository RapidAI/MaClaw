package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type PullOpsResponse struct {
	NodeID       string            `json:"node_id"`
	Ops          []*store.HASyncOp `json:"ops"`
	NextAfterSeq int64             `json:"next_after_seq"`
	HasMore      bool              `json:"has_more"`
	MaxSeq       int64             `json:"max_seq"`
}

type Syncer struct {
	svc      *Service
	client   *http.Client
	interval time.Duration
	limit    int
	slots    chan struct{}
	mu       sync.Mutex
	running  map[string]bool
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
		svc:      svc,
		client:   &http.Client{Timeout: peerPullTimeout},
		interval: interval,
		limit:    limit,
		slots:    make(chan struct{}, maxConcurrentPeerSyncs),
		running:  make(map[string]bool),
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
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" || strings.TrimSpace(peer.BaseURL) == "" {
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
			s.svc.markPeerError(peer.NodeID, err.Error())
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
	u := strings.TrimRight(peer.BaseURL, "/") + "/api/internal/ha/ops?after_seq=" + strconv.FormatInt(afterSeq, 10) + "&limit=" + strconv.Itoa(s.limit)
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
	var out PullOpsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, pullOpsResponseBodyLimit)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
