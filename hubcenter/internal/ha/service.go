package ha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	EntityBlockedEmail = "blocked_email"
	EntityBlockedIP    = "blocked_ip"
	EntityNewsArticle  = "news_article"
	EntityHubInstance  = "hub_instance"
	EntityHubUserLink  = "hub_user_link"

	OpUpsert = "upsert"
	OpDelete = "delete"
)

type Service struct {
	nodeID        string
	nodeName      string
	advertiseURL  string
	clusterSecret string

	ops           store.HASyncOpRepository
	cursors       store.HAPeerCursorRepository
	versions      store.HAEntityVersionRepository
	blockedEmails store.BlockedEmailRepository
	blockedIPs    store.BlockedIPRepository
	news          store.NewsRepository
	hubs          store.HubRepository
	links         store.HubUserLinkRepository

	mu     sync.RWMutex
	peers  map[string]*PeerRuntimeState
	client *http.Client
}

func NewService(nodeID, nodeName, advertiseURL, clusterSecret string, peers []StaticPeer) *Service {
	peerMap := make(map[string]*PeerRuntimeState, len(peers))
	for _, peer := range peers {
		id := strings.TrimSpace(peer.NodeID)
		if id == "" {
			continue
		}
		peerMap[id] = &PeerRuntimeState{
			NodeID:        id,
			NodeName:      strings.TrimSpace(peer.NodeName),
			BaseURL:       strings.TrimRight(strings.TrimSpace(peer.BaseURL), "/"),
			ServiceStatus: "unknown",
			ClusterStatus: "unknown",
		}
	}

	return &Service{
		nodeID:        strings.TrimSpace(nodeID),
		nodeName:      strings.TrimSpace(nodeName),
		advertiseURL:  strings.TrimRight(strings.TrimSpace(advertiseURL), "/"),
		clusterSecret: strings.TrimSpace(clusterSecret),
		peers:         peerMap,
		client:        &http.Client{Timeout: 2 * time.Second},
	}
}

func (s *Service) AttachStore(st *store.Store) {
	if s == nil || st == nil {
		return
	}
	s.ops = st.HASyncOps
	s.cursors = st.HAPeerCursors
	s.versions = st.HAEntityVersions
	s.blockedEmails = st.BlockedEmails
	s.blockedIPs = st.BlockedIPs
	s.news = st.News
	s.hubs = st.Hubs
	s.links = st.HubUserLinks
}

func (s *Service) NodeID() string {
	if s == nil {
		return ""
	}
	return s.nodeID
}

func (s *Service) ClusterSecret() string {
	if s == nil {
		return ""
	}
	return s.clusterSecret
}

func (s *Service) listPeerStates() []*PeerRuntimeState {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*PeerRuntimeState, 0, len(s.peers))
	for _, peer := range s.peers {
		cp := *peer
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func (s *Service) markPeerError(nodeID, msg string) {
	if s == nil {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.peers[nodeID]
	if peer == nil {
		return
	}
	peer.Reachable = false
	peer.LastCheckedAt = &now
	peer.LastError = msg
	peer.ServiceStatus = "isolated"
	peer.ClusterStatus = "isolated"
	peer.QualityScore = 0
}

func (s *Service) updatePeerFromQuality(nodeID string, rttMs int64, q *ClientQualityView) {
	if s == nil || q == nil {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.peers[nodeID]
	if peer == nil {
		return
	}
	peer.Reachable = true
	peer.RTTMs = rttMs
	peer.QualityScore = q.QualityScore
	peer.ServiceStatus = q.ServiceStatus
	peer.ClusterStatus = q.Cluster.Status
	peer.LagSeconds = q.Sync.LagSeconds
	peer.Backlog = q.Sync.Backlog
	peer.LastCheckedAt = &now
	peer.LastSuccessAt = &now
	peer.LastError = ""
}

func (s *Service) updatePeerSync(nodeID string, backlog int64) {
	if s == nil {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.peers[nodeID]
	if peer == nil {
		return
	}
	peer.Reachable = true
	peer.Backlog = backlog
	peer.LagSeconds = 0
	peer.LastCheckedAt = &now
	peer.LastSuccessAt = &now
	peer.LastError = ""
}

func (s *Service) GetClientQuality(ctx context.Context) (*ClientQualityView, error) {
	_ = ctx
	peers := s.listPeerStates()
	totalNodes := 1 + len(peers)
	reachablePeers := 0
	var maxLag int64
	var maxBacklog int64
	var lastSuccess *time.Time
	recentSyncError := false
	for _, peer := range peers {
		if peer.Reachable {
			reachablePeers++
		}
		lag := peer.LagSeconds
		if peer.LastSuccessAt != nil {
			elapsed := int64(time.Since(*peer.LastSuccessAt).Seconds())
			if elapsed > lag {
				lag = elapsed
			}
		}
		if lag > maxLag {
			maxLag = lag
		}
		if peer.Backlog > maxBacklog {
			maxBacklog = peer.Backlog
		}
		if peer.LastSuccessAt != nil && (lastSuccess == nil || peer.LastSuccessAt.After(*lastSuccess)) {
			t := *peer.LastSuccessAt
			lastSuccess = &t
		}
		if strings.TrimSpace(peer.LastError) != "" {
			recentSyncError = true
		}
	}
	score, status, routable := computeQuality(totalNodes, reachablePeers, maxLag, maxBacklog, recentSyncError)
	view := &ClientQualityView{OK: true, NodeID: s.nodeID, NodeName: s.nodeName, ServiceStatus: status, QualityScore: score, Routable: routable, ServerTime: time.Now().UTC(), TTLSeconds: 15}
	view.Cluster.ReachableNodes = 1 + reachablePeers
	view.Cluster.TotalNodes = totalNodes
	view.Cluster.Status = status
	view.Sync.Enabled = len(peers) > 0
	view.Sync.LagSeconds = maxLag
	view.Sync.Backlog = maxBacklog
	view.Sync.LastSuccessAt = lastSuccess
	view.Features.CanRegister = true
	view.Features.CanHeartbeat = true
	view.Features.CanResolve = true
	return view, nil
}

func (s *Service) ListClientEndpoints(ctx context.Context) (*EndpointsView, error) {
	self, err := s.GetClientQuality(ctx)
	if err != nil {
		return nil, err
	}
	nodes := []EndpointView{{NodeID: s.nodeID, NodeName: s.nodeName, BaseURL: s.advertiseURL, ServiceStatus: self.ServiceStatus, QualityScore: self.QualityScore, Routable: self.Routable}}
	for _, peer := range s.listPeerStates() {
		nodes = append(nodes, EndpointView{NodeID: peer.NodeID, NodeName: peer.NodeName, BaseURL: peer.BaseURL, ServiceStatus: peer.ServiceStatus, QualityScore: peer.QualityScore, Routable: peer.Reachable && peer.QualityScore >= 50})
	}
	return &EndpointsView{OK: true, Nodes: nodes, TTLSeconds: 60, ServerTime: time.Now().UTC()}, nil
}

func (s *Service) ListOpsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*store.HASyncOp, error) {
	if s == nil || s.ops == nil {
		return nil, errors.New("ha sync store not configured")
	}
	return s.ops.ListAfterSeq(ctx, afterSeq, limit)
}

func (s *Service) MaxOpSeq(ctx context.Context) (int64, error) {
	if s == nil || s.ops == nil {
		return 0, errors.New("ha sync store not configured")
	}
	return s.ops.GetMaxSeq(ctx)
}

func (s *Service) AppendUpsert(ctx context.Context, entityType, entityID string, payload any, updatedAt time.Time) error {
	return s.appendOp(ctx, entityType, entityID, OpUpsert, payload, updatedAt)
}

func (s *Service) AppendDelete(ctx context.Context, entityType, entityID string, payload any, updatedAt time.Time) error {
	return s.appendOp(ctx, entityType, entityID, OpDelete, payload, updatedAt)
}

func (s *Service) appendOp(ctx context.Context, entityType, entityID, opType string, payload any, updatedAt time.Time) error {
	if s == nil || s.ops == nil || s.versions == nil {
		return nil
	}
	if strings.TrimSpace(entityType) == "" || strings.TrimSpace(entityID) == "" {
		return fmt.Errorf("entity type and entity id are required")
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	version, err := s.nextEntityVersion(ctx, entityType, entityID, updatedAt)
	if err != nil {
		return err
	}
	payloadJSON, payloadHash, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	return s.ops.Append(ctx, &store.HASyncOp{OpID: newOpID(s.nodeID, updatedAt), SourceNodeID: s.nodeID, EntityType: entityType, EntityID: entityID, OpType: opType, EntityVersion: version, OccurredAt: updatedAt, PayloadJSON: payloadJSON, PayloadHash: payloadHash})
}

func (s *Service) nextEntityVersion(ctx context.Context, entityType, entityID string, updatedAt time.Time) (int64, error) {
	current, err := s.versions.Get(ctx, entityType, entityID)
	if err != nil {
		return 0, err
	}
	next := int64(1)
	if current != nil {
		next = current.Version + 1
	}
	if err := s.versions.Upsert(ctx, &store.HAEntityVersion{EntityType: entityType, EntityID: entityID, Version: next, UpdatedAt: updatedAt, UpdatedByNodeID: s.nodeID}); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Service) ApplyRemoteOps(ctx context.Context, ops []*store.HASyncOp) error {
	for _, op := range ops {
		if err := s.ApplyRemoteOp(ctx, op); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ApplyRemoteOp(ctx context.Context, op *store.HASyncOp) error {
	if s == nil || op == nil || s.ops == nil || s.versions == nil {
		return nil
	}
	applied, err := s.ops.HasApplied(ctx, op.OpID)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	current, err := s.versions.Get(ctx, op.EntityType, op.EntityID)
	if err != nil {
		return err
	}
	if current != nil && op.EntityVersion <= current.Version {
		return s.markApplied(ctx, op)
	}
	if err := s.applyEntityOp(ctx, op); err != nil {
		return err
	}
	if err := s.versions.Upsert(ctx, &store.HAEntityVersion{EntityType: op.EntityType, EntityID: op.EntityID, Version: op.EntityVersion, UpdatedAt: op.OccurredAt, UpdatedByNodeID: op.SourceNodeID}); err != nil {
		return err
	}
	return s.markApplied(ctx, op)
}

func (s *Service) applyEntityOp(ctx context.Context, op *store.HASyncOp) error {
	switch op.EntityType {
	case EntityBlockedEmail:
		return s.applyBlockedEmailOp(ctx, op)
	case EntityBlockedIP:
		return s.applyBlockedIPOp(ctx, op)
	case EntityNewsArticle:
		return s.applyNewsArticleOp(ctx, op)
	case EntityHubInstance:
		return s.applyHubInstanceOp(ctx, op)
	case EntityHubUserLink:
		return s.applyHubUserLinkOp(ctx, op)
	default:
		return fmt.Errorf("unsupported entity type: %s", op.EntityType)
	}
}

func (s *Service) applyBlockedEmailOp(ctx context.Context, op *store.HASyncOp) error {
	if s.blockedEmails == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.BlockedEmail
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.blockedEmails.GetByEmail(ctx, item.Email)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := s.blockedEmails.DeleteByEmail(ctx, item.Email); err != nil {
				return err
			}
		}
		return s.blockedEmails.Create(ctx, &item)
	case OpDelete:
		var payload struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.blockedEmails.DeleteByEmail(ctx, payload.Email)
	default:
		return fmt.Errorf("unsupported blocked email op: %s", op.OpType)
	}
}

func (s *Service) applyBlockedIPOp(ctx context.Context, op *store.HASyncOp) error {
	if s.blockedIPs == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.BlockedIP
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.blockedIPs.GetByIP(ctx, item.IP)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := s.blockedIPs.DeleteByIP(ctx, item.IP); err != nil {
				return err
			}
		}
		return s.blockedIPs.Create(ctx, &item)
	case OpDelete:
		var payload struct {
			IP string `json:"ip"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.blockedIPs.DeleteByIP(ctx, payload.IP)
	default:
		return fmt.Errorf("unsupported blocked ip op: %s", op.OpType)
	}
}

func (s *Service) applyNewsArticleOp(ctx context.Context, op *store.HASyncOp) error {
	if s.news == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.NewsArticle
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.news.GetByID(ctx, item.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			return s.news.Create(ctx, &item)
		}
		return s.news.Update(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.news.Delete(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported news op: %s", op.OpType)
	}
}

func (s *Service) applyHubInstanceOp(ctx context.Context, op *store.HASyncOp) error {
	if s.hubs == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.HubInstance
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		var existing *store.HubInstance
		var err error
		if strings.TrimSpace(item.InstallationID) != "" {
			existing, err = s.hubs.GetByInstallationID(ctx, item.InstallationID)
			if err != nil {
				return err
			}
		}
		if existing == nil {
			existing, err = s.hubs.GetByID(ctx, item.ID)
			if err != nil {
				return err
			}
		}
		if existing == nil {
			return s.hubs.Create(ctx, &item)
		}
		return s.hubs.UpdateRegistration(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.hubs.DeleteByID(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported hub instance op: %s", op.OpType)
	}
}

func (s *Service) applyHubUserLinkOp(ctx context.Context, op *store.HASyncOp) error {
	if s.links == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.HubUserLink
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		if err := s.links.DeleteByHubID(ctx, item.HubID); err != nil {
			return err
		}
		return s.links.Create(ctx, &item)
	case OpDelete:
		var payload struct {
			HubID string `json:"hub_id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.links.DeleteByHubID(ctx, payload.HubID)
	default:
		return fmt.Errorf("unsupported hub user link op: %s", op.OpType)
	}
}

func (s *Service) markApplied(ctx context.Context, op *store.HASyncOp) error {
	return s.ops.MarkApplied(ctx, &store.HAAppliedOp{OpID: op.OpID, SourceNodeID: op.SourceNodeID, EntityType: op.EntityType, EntityID: op.EntityID, AppliedAt: time.Now().UTC()})
}

func (s *Service) AppendBlockedEmail(ctx context.Context, item *store.BlockedEmail) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityBlockedEmail, item.Email, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append blocked email: %v", err)
	}
}

func (s *Service) DeleteBlockedEmail(ctx context.Context, email string) {
	if err := s.AppendDelete(ctx, EntityBlockedEmail, email, map[string]string{"email": email}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete blocked email: %v", err)
	}
}

func (s *Service) AppendBlockedIP(ctx context.Context, item *store.BlockedIP) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityBlockedIP, item.IP, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append blocked ip: %v", err)
	}
}

func (s *Service) DeleteBlockedIP(ctx context.Context, ip string) {
	if err := s.AppendDelete(ctx, EntityBlockedIP, ip, map[string]string{"ip": ip}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete blocked ip: %v", err)
	}
}

func (s *Service) AppendNewsArticle(ctx context.Context, item *store.NewsArticle) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityNewsArticle, item.ID, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append news: %v", err)
	}
}

func (s *Service) DeleteNewsArticle(ctx context.Context, id string) {
	if err := s.AppendDelete(ctx, EntityNewsArticle, id, map[string]string{"id": id}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete news: %v", err)
	}
}

func (s *Service) AppendHubInstance(ctx context.Context, item *store.HubInstance) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityHubInstance, item.ID, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append hub instance: %v", err)
	}
}

func (s *Service) DeleteHubInstance(ctx context.Context, hubID string) {
	if err := s.AppendDelete(ctx, EntityHubInstance, hubID, map[string]string{"id": hubID}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete hub instance: %v", err)
	}
}

func (s *Service) AppendHubUserLink(ctx context.Context, item *store.HubUserLink) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityHubUserLink, item.HubID, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append hub user link: %v", err)
	}
}

func (s *Service) DeleteHubUserLink(ctx context.Context, hubID string) {
	if err := s.AppendDelete(ctx, EntityHubUserLink, hubID, map[string]string{"hub_id": hubID}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete hub user link: %v", err)
	}
}

func computeQuality(totalNodes int, reachablePeers int, maxLagSeconds int64, maxBacklog int64, recentSyncError bool) (int, string, bool) {
	score := 100
	expectedPeers := totalNodes - 1
	missingPeers := expectedPeers - reachablePeers
	if missingPeers == 1 {
		score -= 15
	}
	if missingPeers >= 2 {
		score -= 50
	}
	if maxLagSeconds > 10 {
		score -= 10
	}
	if maxLagSeconds > 30 {
		score -= 15
	}
	if maxBacklog > 100 {
		score -= 10
	}
	if maxBacklog > 1000 {
		score -= 10
	}
	if recentSyncError {
		score -= 10
	}
	if score < 0 {
		score = 0
	}
	switch {
	case score >= 80:
		return score, "healthy", true
	case score >= 50:
		return score, "degraded", true
	default:
		return score, "isolated", false
	}
}

func marshalPayload(payload any) (string, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(data)
	return string(data), hex.EncodeToString(sum[:]), nil
}

func newOpID(nodeID string, now time.Time) string {
	return fmt.Sprintf("op_%s_%d", nodeID, now.UnixNano())
}
