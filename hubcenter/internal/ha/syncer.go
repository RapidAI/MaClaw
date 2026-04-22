package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
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
}

func NewSyncer(svc *Service, interval time.Duration, limit int) *Syncer {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	return &Syncer{svc: svc, client: &http.Client{Timeout: 3 * time.Second}, interval: interval, limit: limit}
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
		go s.syncPeer(ctx, peer)
	}
}

func (s *Syncer) syncPeer(ctx context.Context, peer *PeerRuntimeState) {
	if peer == nil || strings.TrimSpace(peer.NodeID) == "" || strings.TrimSpace(peer.BaseURL) == "" {
		return
	}
	cursor, err := s.svc.cursors.Get(ctx, peer.NodeID)
	if err != nil {
		s.svc.markPeerError(peer.NodeID, err.Error())
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
			return
		}
		now := time.Now().UTC()
		if len(resp.Ops) == 0 {
			_ = s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{PeerNodeID: peer.NodeID, LastPulledSeq: afterSeq, LastPulledAt: &now, LastSuccessAt: &now, LastError: ""})
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

func (s *Syncer) pullOps(ctx context.Context, peer *PeerRuntimeState, afterSeq int64) (*PullOpsResponse, error) {
	u := strings.TrimRight(peer.BaseURL, "/") + "/api/internal/ha/ops?after_seq=" + strconv.FormatInt(afterSeq, 10) + "&limit=" + strconv.Itoa(s.limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if secret := strings.TrimSpace(s.svc.ClusterSecret()); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	req.Header.Set("X-HubCenter-Node", s.svc.nodeID)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull ops failed: %s", resp.Status)
	}
	var out PullOpsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
