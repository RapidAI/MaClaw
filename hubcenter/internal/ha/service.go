package ha

import (
	"bytes"
	"context"
	"crypto/rsa"
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
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	EntityBlockedEmail        = "blocked_email"
	EntityBlockedIP           = "blocked_ip"
	EntityNewsArticle         = "news_article"
	EntityHubInstance         = "hub_instance"
	EntityHubDomainRoute      = "hub_domain_route"
	EntityHubUserLink         = "hub_user_link"
	EntitySystemSetting       = "system_setting"
	EntityGossipSnapshot      = "gossip_snapshot"
	EntitySkillHubSnapshot    = "skillhub_snapshot"
	EntitySkillMarketSnapshot = "skillmarket_snapshot"
	EntityLLMCardType         = "llm_card_type"
	EntityLLMTenantAuth       = "llm_tenant_authorization"
	EntityLLMCardOrder        = "llm_card_order"

	OpUpsert = "upsert"
	OpDelete = "delete"

	maxPendingPushOpsPerPeer = 4000
	haOpsFallbackPageSize    = 1000
)

var (
	opIDCounter          uint64
	fallbackHAHTTPClient = &http.Client{Timeout: 8 * time.Second}
)

type InvalidRemoteOpError struct {
	Reason string
}

func (e InvalidRemoteOpError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return "invalid remote op"
	}
	return e.Reason
}

type Service struct {
	nodeID        string
	nodeName      string
	advertiseURL  string
	publicURL     string
	clusterSecret string
	privateKey    *rsa.PrivateKey
	publicKeyPEM  string

	ops                      store.HASyncOpRepository
	cursors                  store.HAPeerCursorRepository
	versions                 store.HAEntityVersionRepository
	blockedEmails            store.BlockedEmailRepository
	blockedIPs               store.BlockedIPRepository
	news                     store.NewsRepository
	hubs                     store.HubRepository
	routes                   store.HubDomainRouteRepository
	links                    store.HubUserLinkRepository
	settings                 store.SystemSettingsRepository
	gossip                   store.GossipRepository
	skillStore               *skill.SkillStore
	skillMarket              *skillmarket.Store
	cardTypes                cardstore.CardTypeRepository
	cardOrders               cardstore.PurchaseOrderRepository
	llmAuthorizations        llmservice.TenantAuthorizationRepository
	heartbeatSync            store.HAHeartbeatSyncStateRepository
	heartbeatSyncMinInterval time.Duration

	mu             sync.RWMutex
	opMu           sync.Mutex
	peers          map[string]*PeerRuntimeState
	peerPublicKeys map[string]*rsa.PublicKey
	client         *http.Client
	pushSem        chan struct{}
	pushMu         sync.Mutex
	pushPending    map[string][]*store.HASyncOp
	pushRunning    map[string]bool
	pushDebounce   time.Duration
	snapshotMu     sync.Mutex
	snapshotHashes map[string]string
	refresher      interface{ Rebuild(context.Context) error }
	recorder       *diagnostics.FailureEventRecorder
}

func (s *Service) SetFailureEventRecorder(recorder *diagnostics.FailureEventRecorder) {
	s.recorder = recorder
}

func (s *Service) recordFailure(ctx context.Context, category, eventCode, message, entityID string, details map[string]any) {
	if s == nil || s.recorder == nil {
		return
	}
	tenantID := ""
	if raw, ok := details["tenant_id"]; ok {
		tenantID = strings.TrimSpace(fmt.Sprint(raw))
	}
	s.recorder.Record(ctx, diagnostics.FailureEventInput{
		TenantID:  tenantID,
		Category:  category,
		EventCode: eventCode,
		Message:   message,
		EntityID:  entityID,
		Details:   details,
	})
}

type localOpAppender interface {
	AppendLocalWithVersion(ctx context.Context, op *store.HASyncOp) (int64, error)
}

type remoteOpRecorder interface {
	AppendRemoteIfMissing(ctx context.Context, op *store.HASyncOp) error
}

type hubInstanceConflictReplacer interface {
	ReplaceConflictingHubInstance(ctx context.Context, hub *store.HubInstance) error
}

type historyPruner interface {
	PruneHistory(ctx context.Context, cutoff time.Time, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error)
}

func NewService(nodeID, nodeName, advertiseURL, clusterSecret string, peers []StaticPeer) *Service {
	nodeID = strings.TrimSpace(nodeID)
	peerMap := make(map[string]*PeerRuntimeState, len(peers))
	peerKeys := make(map[string]*rsa.PublicKey, len(peers))
	for _, peer := range peers {
		id := strings.TrimSpace(peer.NodeID)
		if id == "" || id == nodeID {
			continue
		}
		peerMap[id] = &PeerRuntimeState{
			NodeID:        id,
			NodeName:      strings.TrimSpace(peer.NodeName),
			BaseURL:       strings.TrimRight(strings.TrimSpace(peer.BaseURL), "/"),
			PublicKeyPEM:  strings.TrimSpace(peer.PublicKeyPEM),
			ServiceStatus: "unknown",
			ClusterStatus: "unknown",
		}
		if pub, err := parseRSAPublicKeyPEM(peer.PublicKeyPEM); err == nil && pub != nil {
			peerKeys[id] = pub
		}
	}

	return &Service{
		nodeID:                   nodeID,
		nodeName:                 strings.TrimSpace(nodeName),
		advertiseURL:             strings.TrimRight(strings.TrimSpace(advertiseURL), "/"),
		clusterSecret:            strings.TrimSpace(clusterSecret),
		peers:                    peerMap,
		peerPublicKeys:           peerKeys,
		client:                   &http.Client{Timeout: 8 * time.Second},
		pushSem:                  make(chan struct{}, 16),
		pushPending:              make(map[string][]*store.HASyncOp),
		pushRunning:              make(map[string]bool),
		pushDebounce:             50 * time.Millisecond,
		snapshotHashes:           make(map[string]string),
		heartbeatSyncMinInterval: 10 * time.Second,
	}
}

func (s *Service) SetPublicURL(publicURL string) {
	if s == nil {
		return
	}
	s.publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
}

// clientFacingURL returns the URL that should be exposed to external clients.
// Prefers publicURL over advertiseURL.
func (s *Service) clientFacingURL() string {
	if s.publicURL != "" {
		return s.publicURL
	}
	return s.advertiseURL
}

func (s *Service) SetPushDebounceInterval(d time.Duration) {
	if s == nil {
		return
	}
	if d < 0 {
		d = 0
	}
	s.pushMu.Lock()
	s.pushDebounce = d
	s.pushMu.Unlock()
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
	s.routes = st.HubDomainRoutes
	s.links = st.HubUserLinks
	s.settings = st.System
	s.gossip = st.Gossip
	s.heartbeatSync = st.HAHeartbeatSync
}

func (s *Service) AttachSkillStore(ss *skill.SkillStore) {
	if s == nil {
		return
	}
	s.skillStore = ss
}

func (s *Service) AttachSkillMarket(sm *skillmarket.Store) {
	if s == nil {
		return
	}
	s.skillMarket = sm
}

func (s *Service) AttachCardTypes(repo cardstore.CardTypeRepository) {
	if s == nil {
		return
	}
	s.cardTypes = repo
}

func (s *Service) AttachCardOrders(repo cardstore.PurchaseOrderRepository) {
	if s == nil {
		return
	}
	s.cardOrders = repo
}

func (s *Service) AttachLLMAuthorizations(repo llmservice.TenantAuthorizationRepository) {
	if s == nil {
		return
	}
	s.llmAuthorizations = repo
}

func (s *Service) SetRouteSnapshotRefresher(refresher interface{ Rebuild(context.Context) error }) {
	if s == nil {
		return
	}
	s.refresher = refresher
}
func (s *Service) NodeID() string {
	if s == nil {
		return ""
	}
	return s.nodeID
}

func (s *Service) SetHeartbeatSyncMinInterval(d time.Duration) {
	if s == nil {
		return
	}
	if d <= 0 {
		d = 10 * time.Second
	}
	s.heartbeatSyncMinInterval = d
}

func (s *Service) ClusterSecret() string {
	if s == nil {
		return ""
	}
	return s.clusterSecret
}

func (s *Service) SetNodeKeyMaterial(material *NodeKeyMaterial) {
	if s == nil || material == nil {
		return
	}
	s.privateKey = material.PrivateKey
	s.publicKeyPEM = strings.TrimSpace(material.PublicKeyPEM)
	if s.publicKeyPEM == "" && material.PrivateKey != nil {
		s.publicKeyPEM = encodeRSAPublicKeyPEM(&material.PrivateKey.PublicKey)
	}
}

func (s *Service) PublicKeyPEM() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.publicKeyPEM)
}

func (s *Service) SignPeerRequest(req *http.Request) error {
	if s == nil || req == nil || s.privateKey == nil {
		return nil
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	canonical := requestCanonicalPayload(req, s.nodeID, timestamp)
	signature, err := signHACanonicalRequest(s.privateKey, canonical)
	if err != nil {
		return err
	}
	req.Header.Set(haHeaderNodeID, s.nodeID)
	req.Header.Set(haHeaderTimestamp, timestamp)
	req.Header.Set(haHeaderSignature, signature)
	return nil
}

func (s *Service) AuthenticatePeerRequest(r *http.Request) error {
	if s == nil {
		return nil
	}
	if secret := strings.TrimSpace(s.clusterSecret); secret != "" {
		if got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); got != secret {
			return fmt.Errorf("invalid cluster secret")
		}
	}
	nodeID := strings.TrimSpace(r.Header.Get(haHeaderNodeID))
	timestamp := strings.TrimSpace(r.Header.Get(haHeaderTimestamp))
	signature := strings.TrimSpace(r.Header.Get(haHeaderSignature))
	if nodeID == "" || timestamp == "" || signature == "" {
		if len(s.peerPublicKeys) == 0 {
			return nil
		}
		return fmt.Errorf("missing ha signature headers")
	}
	if err := timestampWithinHABounds(timestamp, time.Now().UTC(), 5*time.Minute); err != nil {
		return err
	}
	pub := s.peerPublicKeys[nodeID]
	if pub == nil {
		if len(s.peerPublicKeys) == 0 {
			return nil
		}
		return fmt.Errorf("unknown peer public key for node %s", nodeID)
	}
	return verifyHACanonicalRequest(pub, requestCanonicalPayload(r, nodeID, timestamp), signature)
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

func (s *Service) AppendHubHeartbeat(ctx context.Context, hubID string) {
	if s == nil || s.hubs == nil || s.heartbeatSync == nil || strings.TrimSpace(hubID) == "" {
		return
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil || hub == nil || hub.LastSeenAt == nil {
		return
	}
	state, err := s.heartbeatSync.Get(ctx, hubID)
	if err != nil {
		return
	}
	if state != nil && state.LastSyncedSeenAt != nil {
		if !hub.LastSeenAt.After(state.LastSyncedSeenAt.Add(s.heartbeatSyncMinInterval)) {
			return
		}
	}
	s.AppendHubInstance(ctx, hub)
	_ = s.heartbeatSync.Upsert(ctx, &store.HAHeartbeatSyncState{HubID: hubID, LastSyncedSeenAt: hub.LastSeenAt})
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
	nodes := []EndpointView{{NodeID: s.nodeID, NodeName: s.nodeName, BaseURL: s.clientFacingURL(), ServiceStatus: self.ServiceStatus, QualityScore: self.QualityScore, Routable: self.Routable}}
	for _, peer := range s.listPeerStates() {
		nodes = append(nodes, EndpointView{NodeID: peer.NodeID, NodeName: peer.NodeName, BaseURL: peer.BaseURL, ServiceStatus: peer.ServiceStatus, QualityScore: peer.QualityScore, Routable: peer.Reachable && peer.QualityScore >= 50})
	}
	return &EndpointsView{OK: true, Nodes: nodes, TTLSeconds: 60, ServerTime: time.Now().UTC()}, nil
}

type adminSyncCategorySpec struct {
	Key         string
	Label       string
	EntityTypes map[string]struct{}
}

type adminSyncCategoryState struct {
	Spec      adminSyncCategorySpec
	LastOpSeq int64
	LastOpAt  *time.Time
}

type latestEntityTypeOpsLister interface {
	ListLatestByEntityTypes(ctx context.Context, entityTypes []string) ([]*store.HASyncOp, error)
}

type entityTypeOpsChecker interface {
	HasEntityTypeOps(ctx context.Context, entityTypes []string) (bool, error)
}

type localRecordCounter interface {
	Count(ctx context.Context) (int64, error)
}

type skillMarketRecordCounter interface {
	CountSnapshotRecords(ctx context.Context) (int64, error)
}

type skillHubRecordCounter interface {
	CountSnapshotRecords() int64
}

type gossipRecordCounter interface {
	CountSnapshotRecords(ctx context.Context) (int64, error)
}

func adminSyncCategorySpecs() []adminSyncCategorySpec {
	return []adminSyncCategorySpec{
		{Key: "routing", Label: "Hub Routing", EntityTypes: map[string]struct{}{EntityHubDomainRoute: {}, EntityHubInstance: {}, EntityHubUserLink: {}}},
		{Key: "system", Label: "System Settings", EntityTypes: map[string]struct{}{EntitySystemSetting: {}, EntityBlockedEmail: {}, EntityBlockedIP: {}}},
		{Key: "gossip", Label: "Gossip Wall", EntityTypes: map[string]struct{}{EntityGossipSnapshot: {}}},
		{Key: "skillhub", Label: "Skill Library", EntityTypes: map[string]struct{}{EntitySkillHubSnapshot: {}}},
		{Key: "skillmarket", Label: "Skill Market", EntityTypes: map[string]struct{}{EntitySkillMarketSnapshot: {}}},
		{Key: "compute_market", Label: "Compute Market", EntityTypes: map[string]struct{}{EntityLLMCardType: {}, EntityLLMTenantAuth: {}, EntityLLMCardOrder: {}}},
		{Key: "news", Label: "News", EntityTypes: map[string]struct{}{EntityNewsArticle: {}}},
	}
}

func countSkillMarketSnapshotRecords(snap *skillmarket.Snapshot) int64 {
	if snap == nil {
		return 0
	}
	return int64(len(snap.Users) + len(snap.Transactions) + len(snap.Submissions) + len(snap.Purchases) + len(snap.Ratings) + len(snap.Configs) + len(snap.Tiers) + len(snap.AuthTokens) + len(snap.Sessions) + len(snap.SessionRevocations) + len(snap.APIKeys) + len(snap.PendingKeyOrders) + len(snap.NotificationSequences))
}

func (s *Service) localSyncRecordCounts(ctx context.Context) map[string]int64 {
	counts := map[string]int64{}
	if s == nil {
		return counts
	}
	if s.hubs != nil {
		if counter, ok := s.hubs.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["routing"] += count
			}
		} else if items, err := s.hubs.ListAll(ctx); err == nil {
			counts["routing"] += int64(len(items))
		}
	}
	if s.routes != nil {
		if counter, ok := s.routes.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["routing"] += count
			}
		} else if items, err := s.routes.ListAll(ctx); err == nil {
			counts["routing"] += int64(len(items))
		}
	}
	if s.links != nil {
		if counter, ok := s.links.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["routing"] += count
			}
		} else if items, err := s.links.ListAll(ctx); err == nil {
			counts["routing"] += int64(len(items))
		}
	}
	if s.blockedEmails != nil {
		if counter, ok := s.blockedEmails.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["system"] += count
			}
		} else if items, err := s.blockedEmails.List(ctx); err == nil {
			counts["system"] += int64(len(items))
		}
	}
	if s.blockedIPs != nil {
		if counter, ok := s.blockedIPs.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["system"] += count
			}
		} else if items, err := s.blockedIPs.List(ctx); err == nil {
			counts["system"] += int64(len(items))
		}
	}
	if s.settings != nil {
		if counter, ok := s.settings.(localRecordCounter); ok {
			if count, err := counter.Count(ctx); err == nil {
				counts["system"] += count
			}
		} else if items, err := s.settings.List(ctx); err == nil {
			counts["system"] += int64(len(items))
		}
	}
	if s.gossip != nil {
		if counter, ok := s.gossip.(gossipRecordCounter); ok {
			if count, err := counter.CountSnapshotRecords(ctx); err == nil {
				counts["gossip"] += count
			}
		} else if posts, total, err := s.gossip.ListAllPosts(ctx, 0, 1); err == nil {
			if total > 0 {
				counts["gossip"] += int64(total)
			} else {
				counts["gossip"] += int64(len(posts))
			}
		}
	}
	if s.skillStore != nil {
		if counter, ok := any(s.skillStore).(skillHubRecordCounter); ok {
			counts["skillhub"] += counter.CountSnapshotRecords()
		} else if snap, err := s.skillStore.DumpSnapshot(); err == nil {
			counts["skillhub"] += int64(len(snap.Skills))
		}
	}
	if s.skillMarket != nil {
		if counter, ok := any(s.skillMarket).(skillMarketRecordCounter); ok {
			if count, err := counter.CountSnapshotRecords(ctx); err == nil {
				counts["skillmarket"] += count
			}
		} else if snap, err := s.skillMarket.DumpSnapshot(ctx); err == nil {
			counts["skillmarket"] += countSkillMarketSnapshotRecords(snap)
		}
	}
	if s.news != nil {
		if _, total, err := s.news.List(ctx, 0, 1); err == nil {
			counts["news"] += int64(total)
		}
	}
	return counts
}

func buildAdminSyncDetails(ops []*store.HASyncOp, peers []AdminPeerView, localCounts map[string]int64) []AdminSyncCategoryView {
	states := make([]adminSyncCategoryState, 0, len(adminSyncCategorySpecs()))
	for _, spec := range adminSyncCategorySpecs() {
		states = append(states, adminSyncCategoryState{Spec: spec})
	}
	for _, op := range ops {
		if op == nil {
			continue
		}
		for i := range states {
			if _, ok := states[i].Spec.EntityTypes[op.EntityType]; !ok {
				continue
			}
			if op.Seq > states[i].LastOpSeq {
				states[i].LastOpSeq = op.Seq
				t := op.OccurredAt
				states[i].LastOpAt = &t
			}
		}
	}

	views := make([]AdminSyncCategoryView, 0, len(states))
	for _, state := range states {
		item := AdminSyncCategoryView{
			Key:          state.Spec.Key,
			Label:        state.Spec.Label,
			Status:       "idle",
			LocalRecords: localCounts[state.Spec.Key],
			LastOpSeq:    state.LastOpSeq,
			LastOpAt:     state.LastOpAt,
			Peers:        make([]AdminSyncCategoryPeerView, 0, len(peers)),
		}
		for _, peer := range peers {
			status := "synced"
			if state.LastOpSeq == 0 {
				if item.LocalRecords > 0 {
					status = "needs_seed"
				} else {
					status = "idle"
				}
			} else if strings.TrimSpace(peer.LastError) != "" {
				status = "error"
				item.ErrorPeers++
			}
			item.Peers = append(item.Peers, AdminSyncCategoryPeerView{
				NodeID:              peer.NodeID,
				NodeName:            peer.NodeName,
				Status:              status,
				PendingOps:          0,
				CursorLastPulledSeq: peer.CursorLastPulledSeq,
				CursorLastSuccessAt: peer.CursorLastSuccessAt,
				LastError:           peer.LastError,
			})
		}
		switch {
		case state.LastOpSeq == 0 && item.LocalRecords > 0:
			item.Status = "needs_seed"
		case state.LastOpSeq == 0:
			item.Status = "idle"
		case item.ErrorPeers > 0:
			item.Status = "error"
		case item.PendingPeers > 0:
			item.Status = "syncing"
		default:
			item.Status = "healthy"
		}
		views = append(views, item)
	}
	return views
}

func adminSyncDetailEntityTypes() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, spec := range adminSyncCategorySpecs() {
		for entityType := range spec.EntityTypes {
			if _, ok := seen[entityType]; ok {
				continue
			}
			seen[entityType] = struct{}{}
			out = append(out, entityType)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) listAdminSyncDetailOps(ctx context.Context) ([]*store.HASyncOp, error) {
	if s == nil || s.ops == nil {
		return nil, nil
	}
	if lister, ok := s.ops.(latestEntityTypeOpsLister); ok {
		return lister.ListLatestByEntityTypes(ctx, adminSyncDetailEntityTypes())
	}
	return s.listLatestAdminSyncDetailOpsFallback(ctx)
}

func (s *Service) listLatestAdminSyncDetailOpsFallback(ctx context.Context) ([]*store.HASyncOp, error) {
	want := map[string]struct{}{}
	for _, entityType := range adminSyncDetailEntityTypes() {
		want[entityType] = struct{}{}
	}
	latest := map[string]*store.HASyncOp{}
	afterSeq := int64(0)
	for {
		ops, err := s.ops.ListAfterSeq(ctx, afterSeq, haOpsFallbackPageSize)
		if err != nil {
			return nil, err
		}
		if len(ops) == 0 {
			break
		}
		maxSeq := afterSeq
		for _, op := range ops {
			if op == nil {
				continue
			}
			if op.Seq > maxSeq {
				maxSeq = op.Seq
			}
			if _, ok := want[op.EntityType]; !ok {
				continue
			}
			current := latest[op.EntityType]
			if current == nil || op.Seq > current.Seq || (op.Seq == current.Seq && op.OccurredAt.After(current.OccurredAt)) {
				cp := *op
				latest[op.EntityType] = &cp
			}
		}
		if len(ops) < haOpsFallbackPageSize || maxSeq <= afterSeq {
			break
		}
		afterSeq = maxSeq
	}
	out := make([]*store.HASyncOp, 0, len(latest))
	for _, op := range latest {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (s *Service) GetAdminStatus(ctx context.Context) (*AdminStatusView, error) {
	if s == nil {
		return &AdminStatusView{Enabled: false, GeneratedAt: time.Now().UTC()}, nil
	}

	quality, err := s.GetClientQuality(ctx)
	if err != nil {
		return nil, err
	}

	maxSeq := int64(0)
	var ops []*store.HASyncOp
	if s.ops != nil {
		maxSeq, err = s.ops.GetMaxSeq(ctx)
		if err != nil {
			return nil, err
		}
		ops, err = s.listAdminSyncDetailOps(ctx)
		if err != nil {
			return nil, err
		}
	}

	peers := s.listPeerStates()
	view := &AdminStatusView{
		Enabled:       true,
		NodeID:        s.nodeID,
		NodeName:      s.nodeName,
		AdvertiseURL:  s.advertiseURL,
		ServiceStatus: quality.ServiceStatus,
		QualityScore:  quality.QualityScore,
		Routable:      quality.Routable,
		Peers:         make([]AdminPeerView, 0, len(peers)),
		GeneratedAt:   time.Now().UTC(),
	}
	view.Cluster = AdminClusterView{
		ReachableNodes: quality.Cluster.ReachableNodes,
		TotalNodes:     quality.Cluster.TotalNodes,
		QuorumSize:     (quality.Cluster.TotalNodes / 2) + 1,
		Status:         quality.Cluster.Status,
	}
	view.Sync = AdminSyncView{
		Enabled:                         len(peers) > 0,
		MaxOpSeq:                        maxSeq,
		PushDebounceSeconds:             int64(s.currentPushDebounce().Seconds()),
		HeartbeatSyncMinIntervalSeconds: int64(s.heartbeatSyncMinInterval.Seconds()),
		LastSuccessAt:                   quality.Sync.LastSuccessAt,
	}

	for _, peer := range peers {
		item := AdminPeerView{
			NodeID:        peer.NodeID,
			NodeName:      peer.NodeName,
			BaseURL:       peer.BaseURL,
			Reachable:     peer.Reachable,
			RTTMs:         peer.RTTMs,
			QualityScore:  peer.QualityScore,
			ServiceStatus: peer.ServiceStatus,
			ClusterStatus: peer.ClusterStatus,
			LagSeconds:    peer.LagSeconds,
			Backlog:       peer.Backlog,
			LastCheckedAt: peer.LastCheckedAt,
			LastSuccessAt: peer.LastSuccessAt,
			LastError:     peer.LastError,
		}
		if s.cursors != nil {
			cursor, err := s.cursors.Get(ctx, peer.NodeID)
			if err != nil {
				return nil, err
			}
			if cursor != nil {
				item.CursorLastPulledSeq = cursor.LastPulledSeq
				item.CursorLastPulledAt = cursor.LastPulledAt
				item.CursorLastSuccessAt = cursor.LastSuccessAt
				if item.LastSuccessAt == nil {
					item.LastSuccessAt = cursor.LastSuccessAt
				}
				if strings.TrimSpace(item.LastError) == "" {
					item.LastError = cursor.LastError
				}
			}
		}
		view.Peers = append(view.Peers, item)
	}
	view.Sync.Details = buildAdminSyncDetails(ops, view.Peers, s.localSyncRecordCounts(ctx))

	return view, nil
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

func (s *Service) HasEntityVersion(ctx context.Context, entityType, entityID string) (bool, error) {
	if s == nil || s.versions == nil {
		return false, nil
	}
	item, err := s.versions.Get(ctx, entityType, entityID)
	if err != nil {
		return false, err
	}
	return item != nil, nil
}

func (s *Service) HasEntityTypeOps(ctx context.Context, entityTypes ...string) (bool, error) {
	if s == nil || s.ops == nil {
		return false, nil
	}
	if len(entityTypes) == 0 {
		return false, nil
	}
	want := make(map[string]struct{}, len(entityTypes))
	for _, entityType := range entityTypes {
		entityType = strings.TrimSpace(entityType)
		if entityType == "" {
			continue
		}
		want[entityType] = struct{}{}
	}
	if len(want) == 0 {
		return false, nil
	}
	cleaned := make([]string, 0, len(want))
	for entityType := range want {
		cleaned = append(cleaned, entityType)
	}
	sort.Strings(cleaned)
	if checker, ok := s.ops.(entityTypeOpsChecker); ok {
		return checker.HasEntityTypeOps(ctx, cleaned)
	}
	afterSeq := int64(0)
	for {
		ops, err := s.ops.ListAfterSeq(ctx, afterSeq, haOpsFallbackPageSize)
		if err != nil {
			return false, err
		}
		if len(ops) == 0 {
			return false, nil
		}
		maxSeq := afterSeq
		for _, op := range ops {
			if op == nil {
				continue
			}
			if op.Seq > maxSeq {
				maxSeq = op.Seq
			}
			if _, ok := want[op.EntityType]; ok {
				return true, nil
			}
		}
		if len(ops) < haOpsFallbackPageSize || maxSeq <= afterSeq {
			return false, nil
		}
		afterSeq = maxSeq
	}
}

func (s *Service) PruneHistory(ctx context.Context, retention time.Duration, maxRetainedOps, batchSize int64) (*store.HAPruneResult, error) {
	if s == nil || s.ops == nil {
		return &store.HAPruneResult{}, nil
	}
	pruner, ok := s.ops.(historyPruner)
	if !ok {
		return &store.HAPruneResult{}, nil
	}
	var cutoff time.Time
	if retention > 0 {
		cutoff = time.Now().UTC().Add(-retention)
	}
	return pruner.PruneHistory(ctx, cutoff, maxRetainedOps, batchSize)
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
	payloadJSON, payloadHash, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	op := &store.HASyncOp{OpID: newOpID(s.nodeID, entityType, entityID, updatedAt), SourceNodeID: s.nodeID, EntityType: entityType, EntityID: entityID, OpType: opType, OccurredAt: updatedAt, PayloadJSON: payloadJSON, PayloadHash: payloadHash}
	if err := validateLocalOp(op); err != nil {
		return err
	}
	if appender, ok := s.ops.(localOpAppender); ok {
		version, err := appender.AppendLocalWithVersion(ctx, op)
		if err == nil {
			if version > 0 {
				s.broadcastLocalOp(op)
			}
		}
		return err
	}

	// Fallback for tests or alternate store backends that have not implemented
	// atomic local append yet. The mutex still prevents duplicate local versions
	// within this process.
	s.opMu.Lock()
	defer s.opMu.Unlock()
	version, err := s.nextEntityVersion(ctx, entityType, entityID, updatedAt)
	if err != nil {
		return err
	}
	op.EntityVersion = version
	if err := s.ops.Append(ctx, op); err != nil {
		return err
	}
	s.broadcastLocalOp(op)
	return nil
}

func (s *Service) broadcastLocalOp(op *store.HASyncOp) {
	if s == nil || op == nil {
		return
	}
	peers := s.listPeerStates()
	if len(peers) == 0 {
		return
	}
	for _, peer := range peers {
		peer := peer
		if peer == nil || strings.TrimSpace(peer.BaseURL) == "" {
			continue
		}
		opCopy := *op
		s.enqueuePushOp(peer, &opCopy)
	}
}

func (s *Service) enqueuePushOp(peer *PeerRuntimeState, op *store.HASyncOp) {
	if s == nil || peer == nil || op == nil {
		return
	}
	nodeID := strings.TrimSpace(peer.NodeID)
	if nodeID == "" {
		return
	}
	s.pushMu.Lock()
	if s.pushPending == nil {
		s.pushPending = make(map[string][]*store.HASyncOp)
	}
	if s.pushRunning == nil {
		s.pushRunning = make(map[string]bool)
	}
	if !s.replacePendingPushOpLocked(nodeID, op) {
		s.pushPending[nodeID] = append(s.pushPending[nodeID], op)
	}
	deferred := false
	if len(s.pushPending[nodeID]) > maxPendingPushOpsPerPeer {
		s.pushPending[nodeID] = append([]*store.HASyncOp(nil), s.pushPending[nodeID][len(s.pushPending[nodeID])-maxPendingPushOpsPerPeer:]...)
		deferred = true
	}
	if s.pushRunning[nodeID] {
		s.pushMu.Unlock()
		if deferred {
			s.markPeerPushDeferred(nodeID, "push queue trimmed; waiting for pull sync")
		}
		return
	}
	s.pushRunning[nodeID] = true
	s.pushMu.Unlock()
	if deferred {
		s.markPeerPushDeferred(nodeID, "push queue trimmed; waiting for pull sync")
	}
	go s.runPeerPushQueue(*peer)
}

func (s *Service) replacePendingPushOpLocked(nodeID string, op *store.HASyncOp) bool {
	if s == nil || op == nil || strings.TrimSpace(op.EntityType) == "" || strings.TrimSpace(op.EntityID) == "" {
		return false
	}
	pending := s.pushPending[nodeID]
	for i, existing := range pending {
		if existing == nil || existing.EntityType != op.EntityType || existing.EntityID != op.EntityID {
			continue
		}
		if shouldReplaceQueuedPushOp(existing, op) {
			pending[i] = op
			s.pushPending[nodeID] = pending
		}
		return true
	}
	return false
}

func shouldReplaceQueuedPushOp(existing, incoming *store.HASyncOp) bool {
	if existing == nil {
		return true
	}
	if incoming == nil {
		return false
	}
	if incoming.EntityVersion != existing.EntityVersion {
		return incoming.EntityVersion > existing.EntityVersion
	}
	if !incoming.OccurredAt.Equal(existing.OccurredAt) {
		return incoming.OccurredAt.After(existing.OccurredAt)
	}
	return strings.TrimSpace(incoming.OpID) > strings.TrimSpace(existing.OpID)
}

func (s *Service) runPeerPushQueue(peer PeerRuntimeState) {
	delay := s.currentPushDebounce()
	if delay > 0 {
		timer := time.NewTimer(delay)
		<-timer.C
		timer.Stop()
	}
	for {
		ops := s.takePeerPushBatch(peer.NodeID, 2000)
		if len(ops) == 0 {
			return
		}
		if !s.pushOpsToPeer(&peer, ops) {
			s.deferPeerPushBatch(peer.NodeID, ops, "push failed; waiting for pull sync")
			return
		}
	}
}

func (s *Service) currentPushDebounce() time.Duration {
	if s == nil {
		return 0
	}
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	return s.pushDebounce
}

func (s *Service) takePeerPushBatch(nodeID string, limit int) []*store.HASyncOp {
	if s == nil {
		return nil
	}
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	pending := s.pushPending[nodeID]
	if len(pending) == 0 {
		if s.pushRunning != nil {
			delete(s.pushRunning, nodeID)
		}
		return nil
	}
	if limit <= 0 || limit > len(pending) {
		limit = len(pending)
	}
	out := append([]*store.HASyncOp(nil), pending[:limit]...)
	if limit == len(pending) {
		delete(s.pushPending, nodeID)
	} else {
		remaining := append([]*store.HASyncOp(nil), pending[limit:]...)
		s.pushPending[nodeID] = remaining
	}
	return out
}

func (s *Service) pushOpsToPeer(peer *PeerRuntimeState, ops []*store.HASyncOp) bool {
	if s == nil || peer == nil || len(ops) == 0 {
		return false
	}
	if s.pushSem != nil {
		select {
		case s.pushSem <- struct{}{}:
			defer func() { <-s.pushSem }()
		default:
			return false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{"ops": ops})
	if err != nil {
		return false
	}
	url := strings.TrimRight(peer.BaseURL, "/") + "/api/internal/ha/ops/apply"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := strings.TrimSpace(s.ClusterSecret()); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if err := s.SignPeerRequest(req); err != nil {
		log.Printf("[hubcenter][ha] sign push ops to %s: %v", peer.NodeID, err)
		return false
	}
	client := s.client
	if client == nil {
		client = fallbackHAHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		s.markPeerError(peer.NodeID, err.Error())
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.markPeerError(peer.NodeID, fmt.Sprintf("push ops failed: %s", resp.Status))
		return false
	}
	s.updatePeerPushSuccess(peer.NodeID)
	return true
}

func (s *Service) deferPeerPushBatch(nodeID string, ops []*store.HASyncOp, msg string) {
	if s == nil || strings.TrimSpace(nodeID) == "" || len(ops) == 0 {
		return
	}
	s.pushMu.Lock()
	if s.pushPending == nil {
		s.pushPending = make(map[string][]*store.HASyncOp)
	}
	combined := append([]*store.HASyncOp(nil), ops...)
	combined = append(combined, s.pushPending[nodeID]...)
	if len(combined) > maxPendingPushOpsPerPeer {
		combined = combined[:maxPendingPushOpsPerPeer]
	}
	s.pushPending[nodeID] = combined
	if s.pushRunning != nil {
		delete(s.pushRunning, nodeID)
	}
	s.pushMu.Unlock()
	s.markPeerPushDeferred(nodeID, msg)
}

func (s *Service) markPeerPushDeferred(nodeID, msg string) {
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
	peer.LastCheckedAt = &now
	peer.LastError = msg
	if peer.Backlog < 1 {
		peer.Backlog = 1
	}
}

func (s *Service) updatePeerPushSuccess(nodeID string) {
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
	peer.LagSeconds = 0
	peer.LastCheckedAt = &now
	peer.LastSuccessAt = &now
	if peer.Backlog == 0 {
		peer.LastError = ""
	}
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
		if err := validateRemoteOp(op); err != nil {
			return err
		}
	}
	needsRouteRebuild := false
	for _, op := range ops {
		applied, err := s.applyRemoteOp(ctx, op, false)
		if err != nil {
			return err
		}
		needsRouteRebuild = needsRouteRebuild || (applied && entityAffectsRouteSnapshot(op.EntityType))
	}
	if needsRouteRebuild && s.refresher != nil {
		_ = s.refresher.Rebuild(ctx)
	}
	return nil
}

func (s *Service) ApplyRemoteOp(ctx context.Context, op *store.HASyncOp) error {
	_, err := s.applyRemoteOp(ctx, op, true)
	return err
}

func (s *Service) applyRemoteOp(ctx context.Context, op *store.HASyncOp, rebuildRouteSnapshot bool) (bool, error) {
	if s == nil || op == nil || s.ops == nil || s.versions == nil {
		return false, nil
	}
	if err := validateRemoteOp(op); err != nil {
		return false, err
	}
	applied, err := s.ops.HasApplied(ctx, op.OpID)
	if err != nil {
		return false, err
	}
	if applied {
		return false, nil
	}
	current, err := s.versions.Get(ctx, op.EntityType, op.EntityID)
	if err != nil {
		return false, err
	}
	if recorder, ok := s.ops.(remoteOpRecorder); ok {
		if err := recorder.AppendRemoteIfMissing(ctx, op); err != nil {
			return false, err
		}
	}
	if current != nil && !shouldApplyRemoteVersion(current, op) {
		return false, s.markApplied(ctx, op)
	}
	if err := s.applyEntityOp(ctx, op); err != nil {
		return false, err
	}
	if rebuildRouteSnapshot && entityAffectsRouteSnapshot(op.EntityType) && s.refresher != nil {
		_ = s.refresher.Rebuild(ctx)
	}
	if err := s.versions.Upsert(ctx, &store.HAEntityVersion{EntityType: op.EntityType, EntityID: op.EntityID, Version: op.EntityVersion, UpdatedAt: op.OccurredAt, UpdatedByNodeID: op.SourceNodeID}); err != nil {
		return false, err
	}
	return true, s.markApplied(ctx, op)
}

func entityAffectsRouteSnapshot(entityType string) bool {
	switch entityType {
	case EntityBlockedEmail, EntityBlockedIP, EntityHubInstance, EntityHubDomainRoute, EntityHubUserLink:
		return true
	default:
		return false
	}
}

func validateRemoteOp(op *store.HASyncOp) error {
	if op == nil {
		return InvalidRemoteOpError{Reason: "missing op"}
	}
	if strings.TrimSpace(op.OpID) == "" {
		return InvalidRemoteOpError{Reason: "missing op id"}
	}
	if strings.TrimSpace(op.SourceNodeID) == "" {
		return InvalidRemoteOpError{Reason: "missing source node id"}
	}
	if strings.TrimSpace(op.EntityType) == "" || strings.TrimSpace(op.EntityID) == "" {
		return InvalidRemoteOpError{Reason: "missing entity identity"}
	}
	if !isSupportedEntityType(op.EntityType) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported entity type: %s", op.EntityType)}
	}
	if op.OpType != OpUpsert && op.OpType != OpDelete {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported op type: %s", op.OpType)}
	}
	if !isSupportedEntityOpType(op.EntityType, op.OpType) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported %s op for entity type: %s", op.OpType, op.EntityType)}
	}
	if op.EntityVersion <= 0 {
		return InvalidRemoteOpError{Reason: "invalid entity version"}
	}
	if op.OccurredAt.IsZero() {
		return InvalidRemoteOpError{Reason: "missing occurred_at"}
	}
	sum := sha256.Sum256([]byte(op.PayloadJSON))
	if !strings.EqualFold(strings.TrimSpace(op.PayloadHash), hex.EncodeToString(sum[:])) {
		return InvalidRemoteOpError{Reason: "payload hash mismatch"}
	}
	if err := validateRemoteOpPayloadIdentity(op); err != nil {
		return err
	}
	return nil
}

func isSupportedEntityType(entityType string) bool {
	switch entityType {
	case EntityBlockedEmail, EntityBlockedIP, EntityNewsArticle, EntityHubInstance, EntityHubDomainRoute, EntityHubUserLink, EntitySystemSetting, EntityGossipSnapshot, EntitySkillHubSnapshot, EntitySkillMarketSnapshot, EntityLLMCardType, EntityLLMTenantAuth, EntityLLMCardOrder:
		return true
	default:
		return false
	}
}

func isSupportedEntityOpType(entityType, opType string) bool {
	if opType == OpUpsert {
		return true
	}
	switch entityType {
	case EntityBlockedEmail, EntityBlockedIP, EntityNewsArticle, EntityHubInstance, EntityHubDomainRoute, EntityHubUserLink, EntityLLMCardType, EntityLLMCardOrder:
		return opType == OpDelete
	default:
		return false
	}
}

func validateLocalOp(op *store.HASyncOp) error {
	if op == nil {
		return InvalidRemoteOpError{Reason: "missing op"}
	}
	if !isSupportedEntityType(op.EntityType) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported entity type: %s", op.EntityType)}
	}
	if op.OpType != OpUpsert && op.OpType != OpDelete {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported op type: %s", op.OpType)}
	}
	if !isSupportedEntityOpType(op.EntityType, op.OpType) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("unsupported %s op for entity type: %s", op.OpType, op.EntityType)}
	}
	return validateRemoteOpPayloadIdentity(op)
}

func validateRemoteOpPayloadIdentity(op *store.HASyncOp) error {
	if op == nil {
		return nil
	}
	entityID := strings.TrimSpace(op.EntityID)
	switch op.OpType {
	case OpUpsert:
		return validateUpsertPayloadIdentity(op, entityID)
	case OpDelete:
		return validateDeletePayloadIdentity(op, entityID)
	}
	return nil
}

func validateUpsertPayloadIdentity(op *store.HASyncOp, entityID string) error {
	field, value, err := remoteOpPayloadIdentity(op)
	if err != nil || field == "" {
		return err
	}
	if strings.TrimSpace(value) == "" {
		if op.EntityType == EntityHubInstance {
			return nil
		}
		return InvalidRemoteOpError{Reason: fmt.Sprintf("%s payload %s is missing", op.EntityType, field)}
	}
	if !remotePayloadIdentityEqual(op.EntityType, value, entityID) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("%s payload %s does not match op entity id", op.EntityType, field)}
	}
	return nil
}

func validateDeletePayloadIdentity(op *store.HASyncOp, entityID string) error {
	field, value, err := remoteOpDeletePayloadIdentity(op)
	if err != nil || field == "" {
		return err
	}
	if strings.TrimSpace(value) == "" {
		if op.EntityType == EntityHubInstance {
			return nil
		}
		return InvalidRemoteOpError{Reason: fmt.Sprintf("%s delete payload %s is missing", op.EntityType, field)}
	}
	if !remotePayloadIdentityEqual(op.EntityType, value, entityID) {
		return InvalidRemoteOpError{Reason: fmt.Sprintf("%s delete payload %s does not match op entity id", op.EntityType, field)}
	}
	return nil
}

func remoteOpPayloadIdentity(op *store.HASyncOp) (string, string, error) {
	switch op.EntityType {
	case EntityBlockedEmail:
		var payload struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "email", payload.Email, nil
	case EntityBlockedIP:
		var payload struct {
			IP string `json:"ip"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "ip", payload.IP, nil
	case EntityNewsArticle, EntityHubInstance, EntityHubDomainRoute, EntityHubUserLink, EntityLLMCardType, EntityLLMTenantAuth:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "id", payload.ID, nil
	case EntityLLMCardOrder:
		var payload struct {
			OrderNo string `json:"order_no"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "order_no", payload.OrderNo, nil
	case EntitySystemSetting:
		var payload struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "key", payload.Key, nil
	default:
		return "", "", nil
	}
}

func remoteOpDeletePayloadIdentity(op *store.HASyncOp) (string, string, error) {
	switch op.EntityType {
	case EntityBlockedEmail:
		var payload struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "email", payload.Email, nil
	case EntityBlockedIP:
		var payload struct {
			IP string `json:"ip"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "ip", payload.IP, nil
	case EntityNewsArticle, EntityHubInstance, EntityHubDomainRoute, EntityHubUserLink, EntityLLMCardType:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "id", payload.ID, nil
	case EntityLLMCardOrder:
		var payload struct {
			OrderNo string `json:"order_no"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return "", "", err
		}
		return "order_no", payload.OrderNo, nil
	default:
		return "", "", nil
	}
}

func remotePayloadIdentityEqual(entityType, payloadID, entityID string) bool {
	payloadID = strings.TrimSpace(payloadID)
	entityID = strings.TrimSpace(entityID)
	switch entityType {
	case EntityBlockedEmail:
		return strings.EqualFold(payloadID, entityID)
	default:
		return payloadID == entityID
	}
}

func shouldApplyRemoteVersion(current *store.HAEntityVersion, op *store.HASyncOp) bool {
	if op == nil {
		return false
	}
	if current == nil {
		return true
	}
	if op.EntityVersion != current.Version {
		return op.EntityVersion > current.Version
	}
	if !op.OccurredAt.Equal(current.UpdatedAt) {
		return op.OccurredAt.After(current.UpdatedAt)
	}
	return strings.TrimSpace(op.SourceNodeID) > strings.TrimSpace(current.UpdatedByNodeID)
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
	case EntityHubDomainRoute:
		return s.applyHubDomainRouteOp(ctx, op)
	case EntityHubUserLink:
		return s.applyHubUserLinkOp(ctx, op)
	case EntitySystemSetting:
		return s.applySystemSettingOp(ctx, op)
	case EntityGossipSnapshot:
		return s.applyGossipSnapshotOp(ctx, op)
	case EntitySkillHubSnapshot:
		return s.applySkillHubSnapshotOp(ctx, op)
	case EntitySkillMarketSnapshot:
		return s.applySkillMarketSnapshotOp(ctx, op)
	case EntityLLMCardType:
		return s.applyLLMCardTypeOp(ctx, op)
	case EntityLLMTenantAuth:
		return s.applyLLMTenantAuthOp(ctx, op)
	case EntityLLMCardOrder:
		return s.applyLLMCardOrderOp(ctx, op)
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
		if err := normalizeHubPayloadID(&item, op.EntityID); err != nil {
			return err
		}
		if replacer, ok := s.hubs.(hubInstanceConflictReplacer); ok {
			return replacer.ReplaceConflictingHubInstance(ctx, &item)
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
			existing, err = s.hubs.GetByEndpoint(ctx, item.Host, item.Port, item.BaseURL)
			if err != nil {
				return err
			}
		}
		if existing == nil {
			return s.hubs.Create(ctx, &item)
		}
		item.ID = existing.ID
		return s.hubs.UpdateRegistration(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		if strings.TrimSpace(payload.ID) == "" {
			payload.ID = op.EntityID
		} else if strings.TrimSpace(payload.ID) != strings.TrimSpace(op.EntityID) {
			return InvalidRemoteOpError{Reason: "hub payload id does not match op entity id"}
		}
		return s.hubs.DeleteByID(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported hub instance op: %s", op.OpType)
	}
}

func normalizeHubPayloadID(item *store.HubInstance, entityID string) error {
	if item == nil {
		return InvalidRemoteOpError{Reason: "missing hub payload"}
	}
	item.ID = strings.TrimSpace(item.ID)
	entityID = strings.TrimSpace(entityID)
	if item.ID == "" {
		item.ID = entityID
		return nil
	}
	if item.ID != entityID {
		return InvalidRemoteOpError{Reason: "hub payload id does not match op entity id"}
	}
	return nil
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
		normalizeRoutingTenantIDs(&item)
		return s.links.Upsert(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.links.DeleteByID(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported hub user link op: %s", op.OpType)
	}
}

func (s *Service) applyHubDomainRouteOp(ctx context.Context, op *store.HASyncOp) error {
	if s.routes == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item store.HubDomainRoute
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		normalizeRoutingTenantIDs(&item)
		return s.routes.Upsert(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.routes.DeleteByID(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported hub domain route op: %s", op.OpType)
	}
}

type systemSettingPayload struct {
	Key       string `json:"key"`
	ValueJSON string `json:"value_json"`
}

type GossipSnapshot struct {
	Posts    []*store.GossipPost    `json:"posts"`
	Comments []*store.GossipComment `json:"comments"`
}

func (s *Service) applySystemSettingOp(ctx context.Context, op *store.HASyncOp) error {
	if s.settings == nil || op.OpType != OpUpsert {
		return nil
	}
	var payload systemSettingPayload
	if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
		return err
	}
	return s.settings.Set(ctx, payload.Key, payload.ValueJSON)
}

func (s *Service) applyGossipSnapshotOp(ctx context.Context, op *store.HASyncOp) error {
	if s.gossip == nil || op.OpType != OpUpsert {
		return nil
	}
	var snap GossipSnapshot
	if err := json.Unmarshal([]byte(op.PayloadJSON), &snap); err != nil {
		return err
	}
	return s.gossip.ReplaceAll(ctx, snap.Posts, snap.Comments)
}

func (s *Service) applySkillHubSnapshotOp(ctx context.Context, op *store.HASyncOp) error {
	if s.skillStore == nil || op.OpType != OpUpsert {
		return nil
	}
	var snap skill.Snapshot
	if err := json.Unmarshal([]byte(op.PayloadJSON), &snap); err != nil {
		return err
	}
	return s.skillStore.LoadSnapshot(&snap)
}

func (s *Service) applySkillMarketSnapshotOp(ctx context.Context, op *store.HASyncOp) error {
	if s.skillMarket == nil || op.OpType != OpUpsert {
		return nil
	}
	var snap skillmarket.Snapshot
	if err := json.Unmarshal([]byte(op.PayloadJSON), &snap); err != nil {
		return err
	}
	return s.skillMarket.LoadSnapshot(ctx, &snap)
}

func (s *Service) applyLLMCardTypeOp(ctx context.Context, op *store.HASyncOp) error {
	if s.cardTypes == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item cardstore.CardType
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.cardTypes.GetByID(ctx, item.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			return s.cardTypes.Create(ctx, &item)
		}
		return s.cardTypes.Update(ctx, &item)
	case OpDelete:
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.cardTypes.Delete(ctx, payload.ID)
	default:
		return fmt.Errorf("unsupported llm card type op: %s", op.OpType)
	}
}

func (s *Service) applyLLMTenantAuthOp(ctx context.Context, op *store.HASyncOp) error {
	if s.llmAuthorizations == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item llmservice.TenantAuthorization
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.llmAuthorizations.GetByID(ctx, item.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			return s.llmAuthorizations.Create(ctx, &item)
		}
		return s.llmAuthorizations.Update(ctx, &item)
	default:
		return fmt.Errorf("unsupported llm tenant authorization op: %s", op.OpType)
	}
}

func (s *Service) applyLLMCardOrderOp(ctx context.Context, op *store.HASyncOp) error {
	if s.cardOrders == nil {
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var item cardstore.PurchaseOrder
		if err := json.Unmarshal([]byte(op.PayloadJSON), &item); err != nil {
			return err
		}
		existing, err := s.cardOrders.GetByOrderNo(ctx, item.OrderNo)
		if err != nil {
			return err
		}
		if existing == nil {
			return s.cardOrders.Create(ctx, &item)
		}
		return s.cardOrders.Update(ctx, &item)
	case OpDelete:
		var payload struct {
			OrderNo string `json:"order_no"`
		}
		if err := json.Unmarshal([]byte(op.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.cardOrders.Delete(ctx, payload.OrderNo)
	default:
		return fmt.Errorf("unsupported llm card order op: %s", op.OpType)
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
		s.recordFailure(ctx, "ha_sync", "append_blocked_email_failed", err.Error(), item.Email, nil)
	}
}

func (s *Service) DeleteBlockedEmail(ctx context.Context, email string) {
	if err := s.AppendDelete(ctx, EntityBlockedEmail, email, map[string]string{"email": email}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete blocked email: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_blocked_email_failed", err.Error(), email, nil)
	}
}

func (s *Service) AppendBlockedIP(ctx context.Context, item *store.BlockedIP) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityBlockedIP, item.IP, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append blocked ip: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_blocked_ip_failed", err.Error(), item.IP, nil)
	}
}

func (s *Service) DeleteBlockedIP(ctx context.Context, ip string) {
	if err := s.AppendDelete(ctx, EntityBlockedIP, ip, map[string]string{"ip": ip}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete blocked ip: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_blocked_ip_failed", err.Error(), ip, nil)
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

func (s *Service) AppendLLMCardType(ctx context.Context, item *cardstore.CardType) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityLLMCardType, item.ID, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append llm card type: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_llm_card_type_failed", err.Error(), item.ID, nil)
	}
}

func (s *Service) AppendLLMAuthorization(ctx context.Context, item *llmservice.TenantAuthorization) {
	if item == nil {
		return
	}
	updatedAt := item.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if err := s.AppendUpsert(ctx, EntityLLMTenantAuth, item.ID, item, updatedAt); err != nil {
		log.Printf("[hubcenter][ha] append llm tenant authorization: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_llm_tenant_authorization_failed", err.Error(), item.ID, map[string]any{"tenant_id": item.TenantID, "hub_id": item.HubID})
	}
}

func (s *Service) AppendLLMCardOrder(ctx context.Context, item *cardstore.PurchaseOrder) {
	if item == nil {
		return
	}
	updatedAt := item.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	payload := llmCardOrderSyncPayload(item)
	if err := s.AppendUpsert(ctx, EntityLLMCardOrder, item.OrderNo, payload, updatedAt); err != nil {
		log.Printf("[hubcenter][ha] append llm card order: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_llm_card_order_failed", err.Error(), item.OrderNo, map[string]any{"tenant_id": item.TenantID, "hub_id": item.HubID})
	}
}

func llmCardOrderSyncPayload(item *cardstore.PurchaseOrder) *cardstore.PurchaseOrder {
	if item == nil {
		return nil
	}
	payload := *item
	payload.AuthorizationID = ""
	payload.AuthorizationStatus = ""
	payload.AuthorizationStartsAt = nil
	payload.AuthorizationExpiresAt = nil
	payload.CreditsUsed = nil
	payload.CreditsRemaining = nil
	return &payload
}

func (s *Service) DeleteLLMCardOrder(ctx context.Context, orderNo string) {
	if err := s.AppendDelete(ctx, EntityLLMCardOrder, orderNo, map[string]string{"order_no": orderNo}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete llm card order: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_llm_card_order_failed", err.Error(), orderNo, nil)
	}
}

func (s *Service) DeleteLLMCardType(ctx context.Context, id string) {
	if err := s.AppendDelete(ctx, EntityLLMCardType, id, map[string]string{"id": id}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete llm card type: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_llm_card_type_failed", err.Error(), id, nil)
	}
}

func (s *Service) AppendHubInstance(ctx context.Context, item *store.HubInstance) {
	if item == nil {
		return
	}
	if err := s.AppendUpsert(ctx, EntityHubInstance, item.ID, item, item.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append hub instance: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_hub_instance_failed", err.Error(), item.ID, nil)
	}
}

func (s *Service) DeleteHubInstance(ctx context.Context, hubID string) {
	if err := s.AppendDelete(ctx, EntityHubInstance, hubID, map[string]string{"id": hubID}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete hub instance: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_hub_instance_failed", err.Error(), hubID, nil)
	}
}

func (s *Service) AppendHubUserLink(ctx context.Context, item *store.HubUserLink) {
	if item == nil {
		return
	}
	link := *item
	normalizeRoutingTenantIDs(&link)
	if err := s.AppendUpsert(ctx, EntityHubUserLink, link.ID, &link, link.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append hub user link: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_hub_user_link_failed", err.Error(), link.ID, nil)
	}
}

func (s *Service) DeleteHubUserLink(ctx context.Context, linkID string) {
	if err := s.AppendDelete(ctx, EntityHubUserLink, linkID, map[string]string{"id": linkID}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete hub user link: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_hub_user_link_failed", err.Error(), linkID, nil)
	}
}

func (s *Service) AppendHubDomainRoute(ctx context.Context, item *store.HubDomainRoute) {
	if item == nil {
		return
	}
	route := *item
	normalizeRoutingTenantIDs(&route)
	if err := s.AppendUpsert(ctx, EntityHubDomainRoute, route.ID, &route, route.UpdatedAt); err != nil {
		log.Printf("[hubcenter][ha] append hub domain route: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_hub_domain_route_failed", err.Error(), route.ID, nil)
	}
}

func normalizeRoutingTenantIDs(item any) {
	switch v := item.(type) {
	case *store.HubUserLink:
		if v != nil {
			v.TenantID = normalizeRoutingTenantID(v.TenantID)
		}
	case *store.HubDomainRoute:
		if v != nil {
			v.TenantID = normalizeRoutingTenantID(v.TenantID)
		}
	}
}

func normalizeRoutingTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func (s *Service) DeleteHubDomainRoute(ctx context.Context, routeID string) {
	if err := s.AppendDelete(ctx, EntityHubDomainRoute, routeID, map[string]string{"id": routeID}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] delete hub domain route: %v", err)
		s.recordFailure(ctx, "ha_sync", "delete_hub_domain_route_failed", err.Error(), routeID, nil)
	}
}

func (s *Service) AppendSystemSetting(ctx context.Context, key, valueJSON string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	if err := s.AppendUpsert(ctx, EntitySystemSetting, key, systemSettingPayload{Key: key, ValueJSON: valueJSON}, time.Now().UTC()); err != nil {
		log.Printf("[hubcenter][ha] append system setting: %v", err)
	}
}

func (s *Service) appendSnapshotUpsertIfChanged(ctx context.Context, entityType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	key := entityType + ":" + entityID
	s.snapshotMu.Lock()
	if s.snapshotHashes == nil {
		s.snapshotHashes = make(map[string]string)
	}
	if s.snapshotHashes[key] == hash {
		s.snapshotMu.Unlock()
		return nil
	}
	s.snapshotHashes[key] = hash
	s.snapshotMu.Unlock()
	if err := s.AppendUpsert(ctx, entityType, entityID, payload, time.Now().UTC()); err != nil {
		s.snapshotMu.Lock()
		if s.snapshotHashes[key] == hash {
			delete(s.snapshotHashes, key)
		}
		s.snapshotMu.Unlock()
		return err
	}
	return nil
}

func (s *Service) AppendGossipSnapshot(ctx context.Context, snap *GossipSnapshot) {
	if snap == nil {
		return
	}
	if err := s.appendSnapshotUpsertIfChanged(ctx, EntityGossipSnapshot, "gossip", snap); err != nil {
		log.Printf("[hubcenter][ha] append gossip snapshot: %v", err)
	}
}

func (s *Service) AppendSkillHubSnapshot(ctx context.Context, snap *skill.Snapshot) {
	if snap == nil {
		return
	}
	if err := s.appendSnapshotUpsertIfChanged(ctx, EntitySkillHubSnapshot, "skillhub", snap); err != nil {
		log.Printf("[hubcenter][ha] append skillhub snapshot: %v", err)
	}
}

func (s *Service) AppendSkillMarketSnapshot(ctx context.Context, snap *skillmarket.Snapshot) {
	if snap == nil {
		return
	}
	if err := s.appendSnapshotUpsertIfChanged(ctx, EntitySkillMarketSnapshot, "skillmarket", snap); err != nil {
		log.Printf("[hubcenter][ha] append skillmarket snapshot: %v", err)
	}
}

func (s *Service) SyncHubHeartbeat(ctx context.Context, hubID string) {
	if s == nil || s.hubs == nil || s.heartbeatSync == nil || strings.TrimSpace(hubID) == "" {
		return
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil || hub == nil || hub.LastSeenAt == nil {
		return
	}
	state, err := s.heartbeatSync.Get(ctx, hubID)
	if err != nil {
		return
	}
	if state != nil && state.LastSyncedSeenAt != nil {
		if !hub.LastSeenAt.After(state.LastSyncedSeenAt.Add(s.heartbeatSyncMinInterval)) {
			return
		}
	}
	s.AppendHubInstance(ctx, hub)
	_ = s.heartbeatSync.Upsert(ctx, &store.HAHeartbeatSyncState{HubID: hubID, LastSyncedSeenAt: hub.LastSeenAt})
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

	// In hot-standby mode, short-lived sync lag without backlog is common and should
	// not immediately downgrade a fully reachable cluster. Keep lag penalties light
	// until the delay is sustained for several minutes.
	if maxLagSeconds > 60 {
		score -= 5
	}
	if maxLagSeconds > 180 {
		score -= 10
	}
	if maxLagSeconds > 600 {
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

func newOpID(nodeID, entityType, entityID string, occurredAt time.Time) string {
	seq := atomic.AddUint64(&opIDCounter, 1)
	now := time.Now().UTC().UnixNano()
	entitySum := sha256.Sum256([]byte(entityType + "\x00" + entityID))
	return fmt.Sprintf("op_%s_%d_%d_%d_%x", nodeID, occurredAt.UTC().UnixNano(), now, seq, entitySum[:4])
}
