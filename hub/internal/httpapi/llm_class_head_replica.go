package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var (
	classHeadLocal         = "local"
	classHeadPeers         []string
	classHeadPeerURLs      = map[string]string{}
	classHeadReplicaSecret string
)

func bindClassHeadRoster(local string, peers []string, peerURLs map[string]string, secret string) {
	local = strings.TrimSpace(local)
	if local == "" {
		local = "local"
	}
	classHeadLocal = local
	classHeadPeers = append([]string(nil), peers...)
	classHeadPeerURLs = map[string]string{}
	for id, url := range peerURLs {
		id = strings.TrimSpace(id)
		url = strings.TrimSpace(url)
		if id == "" || url == "" {
			continue
		}
		classHeadPeerURLs[id] = url
	}
	classHeadReplicaSecret = strings.TrimSpace(secret)
}

func localClassHeadNodeID() string {
	if strings.TrimSpace(classHeadLocal) != "" {
		return strings.TrimSpace(classHeadLocal)
	}
	return "local"
}

func classHeadNodeIDs() []string {
	local := localClassHeadNodeID()
	seen := map[string]struct{}{local: {}}
	out := []string{local}
	for _, id := range classHeadPeers {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func seedClassHeadDistribute(data *llmClassHeadStore) {
	if data == nil {
		return
	}
	ack := map[string]string{}
	pending := false
	local := localClassHeadNodeID()
	for _, id := range classHeadNodeIDs() {
		if id == local {
			ack[id] = "acked"
			continue
		}
		ack[id] = "pending"
		pending = true
	}
	data.DistributeAck = ack
	if pending {
		data.DistributeStatus = llmpool.HeadStatusDistributing
		data.Status = llmpool.HeadStatusDistributing
		return
	}
	data.DistributeStatus = "local"
}

func localHasLatestClassHead(data *llmClassHeadStore) bool {
	if data == nil || data.DistributeAck == nil || len(data.DistributeAck) == 0 {
		return true
	}
	status, ok := data.DistributeAck[localClassHeadNodeID()]
	if !ok {
		return true
	}
	return status == "acked"
}

func servingClassHead(data *llmClassHeadStore) *llmpool.ClassificationHead {
	if data == nil {
		return nil
	}
	if !localHasLatestClassHead(data) && data.Previous != nil && data.Previous.Ready() {
		return data.Previous
	}
	return data.Current
}

func classHeadTrainerReady(data *llmClassHeadStore) bool {
	if !classHeadArtifactReady(data) {
		return false
	}
	trainer := ""
	if data != nil {
		trainer = strings.TrimSpace(data.TrainerNodeID)
	}
	if trainer == "" {
		return true
	}
	for _, id := range classHeadNodeIDs() {
		if id == trainer {
			return true
		}
	}
	return trainer == localClassHeadNodeID()
}

func classHeadIsLocalTrainer(data *llmClassHeadStore) bool {
	if data == nil {
		return true
	}
	trainer := strings.TrimSpace(data.TrainerNodeID)
	if trainer == "" {
		return true
	}
	return trainer == localClassHeadNodeID()
}

func setClassHeadTrainer(ctx context.Context, system store.SystemSettingsRepository, groupID, nodeID string) (llmClassHeadResponse, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID != "" {
		found := false
		for _, id := range classHeadNodeIDs() {
			if id == nodeID {
				found = true
				break
			}
		}
		if !found {
			return llmClassHeadResponse{}, errors.New("trainer node is not in the replica roster")
		}
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(ctx, system, groupID)
	data.TrainerNodeID = nodeID
	if err := saveLLMClassHeadLocked(ctx, system, groupID, data); err != nil {
		return llmClassHeadResponse{}, err
	}
	return buildClassHeadResponseFor(groupID, data), nil
}

func finishClassHeadAckLocked(data *llmClassHeadStore) {
	if data == nil {
		return
	}
	pending := false
	for _, status := range data.DistributeAck {
		if status != "acked" {
			pending = true
		}
	}
	wasDistributing := strings.EqualFold(strings.TrimSpace(data.DistributeStatus), llmpool.HeadStatusDistributing) || strings.EqualFold(strings.TrimSpace(data.Status), llmpool.HeadStatusDistributing)
	if pending {
		data.DistributeStatus = llmpool.HeadStatusDistributing
		data.Status = llmpool.HeadStatusDistributing
		return
	}
	data.DistributeStatus = "local"
	if wasDistributing && llmpool.NormalizePipelineMode(data.Pipeline) == llmpool.PipelineShadow {
		data.Status = llmpool.HeadStatusRolledBack
		return
	}
	data.Status = deriveClassHeadStatus(data)
}

func ackClassHeadAfterSharedLoadLocked(data *llmClassHeadStore) bool {
	if data == nil || !classHeadArtifactReady(data) || data.DistributeAck == nil || len(data.DistributeAck) == 0 {
		return false
	}
	local := localClassHeadNodeID()
	if data.DistributeAck[local] == "acked" {
		return false
	}
	data.DistributeAck[local] = "acked"
	finishClassHeadAckLocked(data)
	return true
}

func ackClassHeadAfterSharedLoad(ctx context.Context, system store.SystemSettingsRepository, groupID string) {
	if system == nil {
		return
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(ctx, system, groupID)
	if !ackClassHeadAfterSharedLoadLocked(data) {
		return
	}
	_ = saveLLMClassHeadLocked(ctx, system, groupID, data)
}

func distributeClassHead(ctx context.Context, system store.SystemSettingsRepository, groupID, nodeID string) (llmClassHeadResponse, error) {
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(ctx, system, groupID)
	if !classHeadArtifactReady(data) {
		return buildClassHeadResponseFor(groupID, data), errors.New("artifact not ready")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		nodeID = localClassHeadNodeID()
	}
	if data.DistributeAck == nil {
		data.DistributeAck = map[string]string{}
	}
	data.DistributeAck[nodeID] = "acked"
	finishClassHeadAckLocked(data)
	if err := saveLLMClassHeadLocked(ctx, system, groupID, data); err != nil {
		return llmClassHeadResponse{}, err
	}
	return buildClassHeadResponseFor(groupID, data), nil
}

type classHeadReplicaArtifact struct {
	TenantID       string                      `json:"tenant_id"`
	GroupID        string                      `json:"group_id,omitempty"`
	Pipeline       string                      `json:"pipeline"`
	Current        *llmpool.ClassificationHead `json:"current,omitempty"`
	Previous       *llmpool.ClassificationHead `json:"previous,omitempty"`
	CurrentSource  string                      `json:"current_source,omitempty"`
	PreviousSource string                      `json:"previous_source,omitempty"`
}

func applyClassHeadReplicaArtifact(ctx context.Context, system store.SystemSettingsRepository, art classHeadReplicaArtifact) (llmClassHeadResponse, error) {
	if art.Current == nil || !art.Current.Ready() {
		return llmClassHeadResponse{}, errors.New("replica artifact is not ready")
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	groupID := strings.TrimSpace(art.GroupID)
	data := loadLLMClassHeadExactLocked(ctx, system, groupID)
	incomingPrevVer := 0
	if art.Previous != nil {
		incomingPrevVer = art.Previous.Version
	}
	if data.Previous != nil && data.Previous.Ready() && data.Previous.Version != art.Current.Version && data.Previous.Version != incomingPrevVer {
		data.History = llmpool.ArchiveRetiredHead(data.History, llmpool.VersionInfoFromHead(llmpool.HeadRoleHistory, data.PreviousSource, data.Previous))
	}
	if data.Current != nil && data.Current.Ready() && data.Current.Version != art.Current.Version {
		if art.Previous == nil || art.Previous.Version != data.Current.Version {
			data.Previous = data.Current.Clone()
			data.PreviousSource = data.CurrentSource
		}
	}
	data.Current = art.Current.Clone()
	if src := strings.TrimSpace(art.CurrentSource); src != "" {
		data.CurrentSource = src
	} else {
		data.CurrentSource = llmpool.HeadSourceReplica
	}
	if art.Previous != nil && art.Previous.Ready() {
		data.Previous = art.Previous.Clone()
		if src := strings.TrimSpace(art.PreviousSource); src != "" {
			data.PreviousSource = src
		} else {
			data.PreviousSource = llmpool.HeadSourceReplica
		}
	}
	if mode := llmpool.NormalizePipelineMode(art.Pipeline); mode != "" {
		data.Pipeline = mode
	}
	if data.DistributeAck == nil {
		data.DistributeAck = map[string]string{}
	}
	data.DistributeAck[localClassHeadNodeID()] = "acked"
	pending := false
	for _, status := range data.DistributeAck {
		if status != "acked" {
			pending = true
		}
	}
	if pending {
		data.DistributeStatus = llmpool.HeadStatusDistributing
		data.Status = llmpool.HeadStatusDistributing
	} else {
		data.DistributeStatus = "local"
		data.Status = deriveClassHeadStatus(data)
	}
	if err := saveLLMClassHeadLocked(ctx, system, groupID, data); err != nil {
		return llmClassHeadResponse{}, err
	}
	return buildClassHeadResponseFor(groupID, data), nil
}

func pushClassHeadToPeers(tenantID, groupID string, data *llmClassHeadStore) {
	if data == nil || data.Current == nil || !data.Current.Ready() {
		return
	}
	if strings.TrimSpace(classHeadReplicaSecret) == "" || len(classHeadPeerURLs) == 0 {
		return
	}
	art := classHeadReplicaArtifact{
		TenantID:       strings.TrimSpace(tenantID),
		GroupID:        strings.TrimSpace(groupID),
		Pipeline:       data.Pipeline,
		Current:        data.Current,
		Previous:       data.Previous,
		CurrentSource:  data.CurrentSource,
		PreviousSource: data.PreviousSource,
	}
	raw, err := json.Marshal(art)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	for id, url := range classHeadPeerURLs {
		if id == localClassHeadNodeID() {
			continue
		}
		endpoint := strings.TrimRight(url, "/") + "/api/internal/llm/class-head/apply"
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			log.Printf("[class-head] replica push %s: %v", id, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Replica-Token", classHeadReplicaSecret)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[class-head] replica push %s: %v", id, err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("[class-head] replica push %s: status %d", id, resp.StatusCode)
		}
	}
}

func replicaTokenOK(r *http.Request) bool {
	if r == nil || strings.TrimSpace(classHeadReplicaSecret) == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-MaClaw-Replica-Token"))
	return subtle.ConstantTimeCompare([]byte(got), []byte(classHeadReplicaSecret)) == 1
}

func PostLLMClassHeadTrainerHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var req struct {
			NodeID string `json:"node_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		view, err := setClassHeadTrainer(r.Context(), system, classHeadGroupID(r), req.NodeID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_TRAINER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func PostLLMClassHeadDistributeHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		view, err := distributeClassHead(r.Context(), system, classHeadGroupID(r), r.URL.Query().Get("node_id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_DISTRIBUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func PostInternalClassHeadApplyHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !replicaTokenOK(r) {
			writeError(w, http.StatusUnauthorized, "LLM_CLASS_HEAD_REPLICA_DENIED", "replica token rejected")
			return
		}
		var art classHeadReplicaArtifact
		if err := json.NewDecoder(r.Body).Decode(&art); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		scopedSystem := system
		if strings.TrimSpace(art.TenantID) != "" {
			scopedSystem = scopedSystemSettingsForTenant(art.TenantID, system)
		}
		view, err := applyClassHeadReplicaArtifact(r.Context(), scopedSystem, art)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_APPLY_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
