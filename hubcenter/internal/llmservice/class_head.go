package llmservice

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

const (
	OfficialClassHeadKey        = "llm_official_class_head_v1"
	officialClassHeadStoreVer   = 1
	officialClassHeadMaxSamples = 1500
	officialClassHeadMaxReviews = 2000
	officialClassHeadListLimit  = 30
)

var (
	officialClassHeadMu      sync.Mutex
	officialHeadEmbedderFn   func(string) ([]float32, error)
	officialHeadTrainOnce    sync.Once
	officialHeadTrainCh      chan officialHeadTrainJob
	officialHeadTrainQueued  sync.Mutex
	officialHeadTrainPending map[string]bool
)

type officialHeadTrainJob struct {
	svc     *Service
	groupID string
}

func officialClassHeadSettingKey(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || llmpool.IsOfficialConventionGroupID(groupID) {
		return OfficialClassHeadKey
	}
	return "llm_class_head_v1:" + groupID
}

func GroupIDFromClassHeadSettingKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == OfficialClassHeadKey {
		return OfficialGroupIDFallback(), true
	}
	prefix := "llm_class_head_v1:"
	if strings.HasPrefix(key, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(key, prefix)), true
	}
	return "", false
}

func OfficialGroupIDFallback() string {
	return llmpool.OfficialGroupID
}

func IsClassHeadSettingKey(key string) bool {
	_, ok := GroupIDFromClassHeadSettingKey(key)
	return ok
}

type OfficialClassHeadStore struct {
	Version          int                            `json:"version"`
	Pipeline         string                         `json:"pipeline"`
	Status           string                         `json:"status"`
	OverrideReason   string                         `json:"override_reason,omitempty"`
	Current          *llmpool.ClassificationHead    `json:"current,omitempty"`
	Samples          []OfficialClassHeadSample      `json:"samples,omitempty"`
	Reviews          []OfficialClassHeadReview      `json:"reviews,omitempty"`
	LastTrainAt      string                         `json:"last_train_at,omitempty"`
	LastTrainError   string                         `json:"last_train_error,omitempty"`
	TrainerNodeID    string                         `json:"trainer_node_id,omitempty"`
	Previous         *llmpool.ClassificationHead    `json:"previous,omitempty"`
	CurrentSource    string                         `json:"current_source,omitempty"`
	PreviousSource   string                         `json:"previous_source,omitempty"`
	History          []llmpool.ClassHeadVersionInfo `json:"history,omitempty"`
	DistributeStatus string                         `json:"distribute_status,omitempty"`
	DistributeAck    map[string]string              `json:"distribute_ack,omitempty"`
}

type OfficialClassHeadSample struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Preview    string    `json:"preview,omitempty"`
	Embedding  []float32 `json:"embedding,omitempty"`
	GroupID    string    `json:"group_id,omitempty"`
	RuleClass  string    `json:"rule_class,omitempty"`
	RuleSource string    `json:"rule_source,omitempty"`
	HeadClass  string    `json:"head_class,omitempty"`
	HeadMaxP   float64   `json:"head_max_p,omitempty"`
	Gold       bool      `json:"gold,omitempty"`
	GoldClass  string    `json:"gold_class,omitempty"`
}

type OfficialClassHeadReview struct {
	SampleID  string    `json:"sample_id,omitempty"`
	At        time.Time `json:"at"`
	GoldClass string    `json:"gold_class"`
	Predicted string    `json:"predicted,omitempty"`
	RuleClass string    `json:"rule_class,omitempty"`
}

type OfficialClassHeadView struct {
	Pipeline         string                         `json:"pipeline"`
	Status           string                         `json:"status"`
	Version          int                            `json:"version"`
	TrainedAt        string                         `json:"trained_at,omitempty"`
	Gates            llmpool.GateReport             `json:"gates"`
	Accuracy         float64                        `json:"accuracy"`
	PlanRecall       float64                        `json:"plan_recall"`
	RuleAgreement    float64                        `json:"rule_agreement"`
	Reviews          int                            `json:"reviews"`
	HumanReviews     int                            `json:"human_reviews"`
	ArtifactReady    bool                           `json:"artifact_ready"`
	EmbedderReady    bool                           `json:"embedder_ready"`
	Suggestion       string                         `json:"suggestion"`
	Samples          []OfficialClassHeadSample      `json:"samples,omitempty"`
	LastTrainError   string                         `json:"last_train_error,omitempty"`
	TrainerNodeID    string                         `json:"trainer_node_id,omitempty"`
	LocalNodeID      string                         `json:"local_node_id,omitempty"`
	ClusterNodes     []string                       `json:"cluster_nodes,omitempty"`
	PreviousVersion  int                            `json:"previous_version,omitempty"`
	StoreKey         string                         `json:"store_key,omitempty"`
	Versions         []llmpool.ClassHeadVersionInfo `json:"versions,omitempty"`
	DistributeStatus string                         `json:"distribute_status,omitempty"`
	DistributeAck    map[string]string              `json:"distribute_ack,omitempty"`
	GroupID          string                         `json:"group_id,omitempty"`
	SampleTotal      int                            `json:"sample_total"`
	SamplePage       int                            `json:"sample_page"`
	SampleLimit      int                            `json:"sample_limit"`
	SamplePages      int                            `json:"sample_pages"`
}

func (s *Service) loadOfficialClassHeadLocked(ctx context.Context, groupID string) *OfficialClassHeadStore {
	data := &OfficialClassHeadStore{Version: officialClassHeadStoreVer, Pipeline: llmpool.PipelineOff, Status: llmpool.HeadStatusUnused}
	if s == nil || s.system == nil {
		return data
	}
	raw, err := s.system.Get(ctx, officialClassHeadSettingKey(groupID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return data
	}
	if json.Unmarshal([]byte(raw), data) != nil {
		return &OfficialClassHeadStore{Version: officialClassHeadStoreVer, Pipeline: llmpool.PipelineOff, Status: llmpool.HeadStatusUnused}
	}
	data.Pipeline = llmpool.NormalizePipelineMode(data.Pipeline)
	return data
}

func (s *Service) saveOfficialClassHeadLocked(ctx context.Context, groupID string, data *OfficialClassHeadStore) error {
	if s == nil || s.system == nil || data == nil {
		return nil
	}
	data.Version = officialClassHeadStoreVer
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.system.Set(ctx, officialClassHeadSettingKey(groupID), string(raw))
}

func (s *Service) SetOfficialHeadRoster(local string, peers []string) {
	if s == nil {
		return
	}
	s.headLocal = strings.TrimSpace(local)
	s.headPeers = append([]string(nil), peers...)
}

func (s *Service) localOfficialHeadNodeID() string {
	if s != nil && strings.TrimSpace(s.headLocal) != "" {
		return strings.TrimSpace(s.headLocal)
	}
	return "local"
}

func (s *Service) officialHeadNodeIDs() []string {
	local := s.localOfficialHeadNodeID()
	seen := map[string]struct{}{local: {}}
	out := []string{local}
	if s == nil {
		return out
	}
	for _, id := range s.headPeers {
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

func (s *Service) seedOfficialHeadDistribute(data *OfficialClassHeadStore) {
	if data == nil {
		return
	}
	ack := map[string]string{}
	pending := false
	local := s.localOfficialHeadNodeID()
	for _, id := range s.officialHeadNodeIDs() {
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

func (s *Service) localHasLatestOfficialHead(data *OfficialClassHeadStore) bool {
	if data == nil || data.DistributeAck == nil || len(data.DistributeAck) == 0 {
		return true
	}
	status, ok := data.DistributeAck[s.localOfficialHeadNodeID()]
	if !ok {
		return true
	}
	return status == "acked"
}

func (s *Service) servingOfficialHead(data *OfficialClassHeadStore) *llmpool.ClassificationHead {
	if data == nil {
		return nil
	}
	if !s.localHasLatestOfficialHead(data) && data.Previous != nil && data.Previous.Ready() {
		return data.Previous
	}
	return data.Current
}

func (s *Service) officialHeadTrainerReady(data *OfficialClassHeadStore) bool {
	if !officialHeadArtifactReady(data) {
		return false
	}
	trainer := ""
	if data != nil {
		trainer = strings.TrimSpace(data.TrainerNodeID)
	}
	if trainer == "" {
		return true
	}
	for _, id := range s.officialHeadNodeIDs() {
		if id == trainer {
			return true
		}
	}
	return trainer == s.localOfficialHeadNodeID()
}

func officialHeadArtifactReady(data *OfficialClassHeadStore) bool {
	return data != nil && data.Current != nil && data.Current.Ready()
}

func officialHeadTrainInFlight(groupID string) bool {
	officialHeadTrainQueued.Lock()
	defer officialHeadTrainQueued.Unlock()
	return officialHeadTrainPending[strings.TrimSpace(groupID)]
}

func deriveOfficialHeadStatus(data *OfficialClassHeadStore) string {
	if data == nil {
		return llmpool.HeadStatusUnused
	}
	if strings.EqualFold(strings.TrimSpace(data.DistributeStatus), llmpool.HeadStatusDistributing) {
		return llmpool.HeadStatusDistributing
	}
	if strings.EqualFold(strings.TrimSpace(data.Status), llmpool.HeadStatusRolledBack) && llmpool.NormalizePipelineMode(data.Pipeline) != llmpool.PipelineOn {
		return llmpool.HeadStatusRolledBack
	}
	switch llmpool.NormalizePipelineMode(data.Pipeline) {
	case llmpool.PipelineOn:
		return llmpool.HeadStatusPromoted
	case llmpool.PipelineCanary:
		return llmpool.HeadStatusCanary
	case llmpool.PipelineShadow:
		return llmpool.HeadStatusShadow
	default:
		return llmpool.HeadStatusUnused
	}
}

func officialHeadEvalWindows(data *OfficialClassHeadStore, now time.Time) (llmpool.EvalWindow, llmpool.EvalWindow) {
	recentCutoff := now.Add(-7 * 24 * time.Hour)
	prevCutoff := now.Add(-14 * 24 * time.Hour)
	var recentRows, prevRows []llmpool.GoldEvalRow
	if data != nil {
		reviewed := map[string]bool{}
		for _, review := range data.Reviews {
			if id := strings.TrimSpace(review.SampleID); id != "" {
				reviewed[id] = true
			}
			row := llmpool.GoldEvalRow{Gold: review.GoldClass, Predicted: review.Predicted, RuleClass: review.RuleClass}
			if !review.At.Before(recentCutoff) {
				recentRows = append(recentRows, row)
			} else if !review.At.Before(prevCutoff) {
				prevRows = append(prevRows, row)
			}
		}
		for _, sample := range data.Samples {
			if !sample.Gold || strings.TrimSpace(sample.GoldClass) == "" || reviewed[sample.ID] {
				continue
			}
			row := llmpool.GoldEvalRow{Gold: sample.GoldClass, Predicted: sample.HeadClass, RuleClass: sample.RuleClass}
			if !sample.At.Before(recentCutoff) {
				recentRows = append(recentRows, row)
			} else if !sample.At.Before(prevCutoff) {
				prevRows = append(prevRows, row)
			}
		}
	}
	return llmpool.AccumulateEvalWindow(recentRows), llmpool.AccumulateEvalWindow(prevRows)
}

func officialHeadEmbedderAvailable() bool {
	if officialHeadEmbedderFn != nil {
		return true
	}
	return embedding.SharedGemmaReady()
}

func embedOfficialHeadPreview(preview string) ([]float32, error) {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return nil, errors.New("empty preview")
	}
	if officialHeadEmbedderFn != nil {
		return officialHeadEmbedderFn(preview)
	}
	emb := embedding.SharedGemma256()
	if embedding.IsNoop(emb) {
		return nil, errors.New("embedding model not ready")
	}
	vec, err := emb.Embed(preview)
	if err != nil {
		return nil, err
	}
	if len(vec) < llmpool.HeadDim {
		return nil, errors.New("embedding dimension too small")
	}
	return vec, nil
}

func (s *Service) OfficialClassHeadView() OfficialClassHeadView {
	return s.OfficialClassHeadViewFor("")
}

func (s *Service) OfficialClassHeadViewFor(groupID string) OfficialClassHeadView {
	return s.OfficialClassHeadViewPage(groupID, 1)
}

func (s *Service) OfficialClassHeadViewPage(groupID string, page int) OfficialClassHeadView {
	officialClassHeadMu.Lock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	officialClassHeadMu.Unlock()
	return buildOfficialClassHeadViewPage(s, groupID, data, page)
}

func pageOfficialHeadSamples(samples []OfficialClassHeadSample, page, limit int) ([]OfficialClassHeadSample, int, int, int) {
	if limit < 1 {
		limit = officialClassHeadListLimit
	}
	total := len(samples)
	pages := 1
	if total > 0 {
		pages = (total + limit - 1) / limit
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if total == 0 {
		return nil, 0, page, limit
	}
	start := (page - 1) * limit
	end := start + limit
	if end > total {
		end = total
	}
	out := make([]OfficialClassHeadSample, end-start)
	copy(out, samples[start:end])
	return out, total, page, limit
}

func buildOfficialClassHeadView(s *Service, groupID string, data *OfficialClassHeadStore) OfficialClassHeadView {
	return buildOfficialClassHeadViewPage(s, groupID, data, 1)
}

func buildOfficialClassHeadViewPage(s *Service, groupID string, data *OfficialClassHeadStore, page int) OfficialClassHeadView {
	if data == nil {
		data = &OfficialClassHeadStore{Pipeline: llmpool.PipelineOff, Status: llmpool.HeadStatusUnused}
	}
	artifactOK := officialHeadArtifactReady(data) && llmpool.DistributeComplete(data.DistributeAck)
	if s != nil {
		artifactOK = s.officialHeadTrainerReady(data) && llmpool.DistributeComplete(data.DistributeAck)
	}
	recent, previous := officialHeadEvalWindows(data, time.Now())
	gates := llmpool.EvaluatePromoteGates(recent, previous, artifactOK)
	samples, sampleTotal, samplePage, sampleLimit := pageOfficialHeadSamples(data.Samples, page, officialClassHeadListLimit)
	samplePages := 1
	if sampleTotal > 0 && sampleLimit > 0 {
		samplePages = (sampleTotal + sampleLimit - 1) / sampleLimit
	}
	for i := range samples {
		samples[i].Embedding = nil
	}
	version := 0
	trainedAt := data.LastTrainAt
	if data.Current != nil {
		version = data.Current.Version
		if trainedAt == "" {
			trainedAt = data.Current.TrainedAt
		}
	}
	status := deriveOfficialHeadStatus(data)
	if officialHeadTrainInFlight(groupID) {
		status = llmpool.HeadStatusTraining
	}
	view := OfficialClassHeadView{
		GroupID:          firstNonEmptyOfficial(groupID, OfficialGroupIDFallback()),
		Pipeline:         data.Pipeline,
		Status:           status,
		Version:          version,
		TrainedAt:        trainedAt,
		Gates:            gates,
		Accuracy:         recent.Accuracy(),
		PlanRecall:       recent.PlanRecall(),
		RuleAgreement:    recent.RuleAgreement(),
		Reviews:          recent.Reviews,
		HumanReviews:     len(data.Reviews),
		ArtifactReady:    officialHeadArtifactReady(data),
		EmbedderReady:    officialHeadEmbedderAvailable(),
		Suggestion:       gates.Suggestion,
		Samples:          samples,
		LastTrainError:   data.LastTrainError,
		TrainerNodeID:    data.TrainerNodeID,
		PreviousVersion:  0,
		StoreKey:         officialClassHeadSettingKey(groupID),
		Versions:         llmpool.CollectHeadVersions(data.Current, data.Previous, data.CurrentSource, data.PreviousSource, data.History),
		DistributeStatus: firstNonEmptyOfficial(data.DistributeStatus, "local"),
		DistributeAck:    data.DistributeAck,
		SampleTotal:      sampleTotal,
		SamplePage:       samplePage,
		SampleLimit:      sampleLimit,
		SamplePages:      samplePages,
	}
	if s != nil {
		view.LocalNodeID = s.localOfficialHeadNodeID()
		view.ClusterNodes = s.officialHeadNodeIDs()
	}
	if data.Previous != nil {
		view.PreviousVersion = data.Previous.Version
	}
	return view
}

func rotateOfficialHead(data *OfficialClassHeadStore, next *llmpool.ClassificationHead, source string) {
	if data == nil {
		return
	}
	llmpool.RotateClassificationHead(&data.Current, &data.Previous, &data.CurrentSource, &data.PreviousSource, &data.History, next, source)
}

func firstNonEmptyOfficial(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) OfficialHeadRuntime(userID string) *llmpool.HeadRuntime {
	return s.HeadRuntimeForGroup(llmpool.OfficialGroupID, userID)
}

func (s *Service) HeadRuntimeForGroup(groupID, userID string) *llmpool.HeadRuntime {
	_ = groupID
	officialClassHeadMu.Lock()
	data := s.loadOfficialClassHeadLocked(context.Background(), "")
	officialClassHeadMu.Unlock()
	mode := llmpool.NormalizePipelineMode(data.Pipeline)
	head := s.servingOfficialHead(data)
	if mode == llmpool.PipelineOff || head == nil || !head.Ready() {
		return nil
	}
	return &llmpool.HeadRuntime{
		Mode:   mode,
		UserID: strings.TrimSpace(userID),
		Head:   head,
		Predict: func(preview string) llmpool.HeadPrediction {
			if head == nil || !head.Ready() {
				return llmpool.HeadPrediction{Class: llmpool.WorkloadFallbackBalanced}
			}
			vec, err := embedOfficialHeadPreview(preview)
			if err != nil {
				return llmpool.HeadPrediction{Class: llmpool.WorkloadFallbackBalanced}
			}
			return head.Predict(vec)
		},
	}
}

func (s *Service) RecordOfficialClassHeadSample(preview, ruleClass, ruleSource, headClass string, headMaxP float64, groupID string, passthrough bool) {
	if s == nil || passthrough || strings.TrimSpace(groupID) == "" {
		return
	}
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return
	}
	gold := llmpool.IsGoldClassSource(ruleSource)
	goldClass := ""
	if gold {
		goldClass = llmpool.NormalizeWorkloadClass(ruleClass)
		if goldClass == "" {
			gold = false
		}
	}
	id := officialHeadPreviewID(preview)
	now := time.Now().UTC()
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), "")
	idx := -1
	for i := range data.Samples {
		if data.Samples[i].ID == id || strings.TrimSpace(data.Samples[i].Preview) == preview {
			idx = i
			break
		}
	}
	if idx >= 0 {
		sample := data.Samples[idx]
		if sample.ID != id {
			migrateOfficialHeadReviewIDs(data.Reviews, sample.ID, id)
			sample.ID = id
		}
		human := officialHeadHasReview(data.Reviews, id)
		sample.At = now
		sample.Preview = preview
		if groupID != "" {
			sample.GroupID = groupID
		}
		if strings.TrimSpace(headClass) != "" {
			sample.HeadClass = headClass
			sample.HeadMaxP = headMaxP
		}
		if !human {
			if gold {
				sample.Gold = true
				if goldClass != "" {
					sample.GoldClass = goldClass
				}
				sample.RuleClass = ruleClass
				sample.RuleSource = ruleSource
			} else if !sample.Gold {
				sample.RuleClass = ruleClass
				sample.RuleSource = ruleSource
			}
		}
		data.Samples = append(append([]OfficialClassHeadSample{sample}, data.Samples[:idx]...), data.Samples[idx+1:]...)
	} else {
		data.Samples = append([]OfficialClassHeadSample{{
			ID:         id,
			At:         now,
			Preview:    preview,
			GroupID:    groupID,
			RuleClass:  ruleClass,
			RuleSource: ruleSource,
			HeadClass:  headClass,
			HeadMaxP:   headMaxP,
			Gold:       gold,
			GoldClass:  goldClass,
		}}, data.Samples...)
	}
	if len(data.Samples) > officialClassHeadMaxSamples {
		data.Samples = pruneOfficialHeadSamples(data.Samples, officialClassHeadMaxSamples)
	}
	_ = s.saveOfficialClassHeadLocked(context.Background(), "", data)
}

func pruneOfficialHeadSamples(samples []OfficialClassHeadSample, max int) []OfficialClassHeadSample {
	if len(samples) <= max {
		return samples
	}
	kept := make([]OfficialClassHeadSample, 0, max)
	for _, sample := range samples {
		if sample.Gold || strings.TrimSpace(sample.GoldClass) != "" {
			kept = append(kept, sample)
		}
	}
	for _, sample := range samples {
		if len(kept) >= max {
			break
		}
		if sample.Gold || strings.TrimSpace(sample.GoldClass) != "" {
			continue
		}
		kept = append(kept, sample)
	}
	if len(kept) > max {
		return kept[:max]
	}
	return kept
}

func officialHeadPreviewID(preview string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(preview)))
	return hex.EncodeToString(sum[:])
}

func officialHeadHasReview(reviews []OfficialClassHeadReview, sampleID string) bool {
	sampleID = strings.TrimSpace(sampleID)
	if sampleID == "" {
		return false
	}
	for _, review := range reviews {
		if strings.TrimSpace(review.SampleID) == sampleID {
			return true
		}
	}
	return false
}

func migrateOfficialHeadReviewIDs(reviews []OfficialClassHeadReview, from, to string) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return
	}
	for i := range reviews {
		if reviews[i].SampleID == from {
			reviews[i].SampleID = to
		}
	}
}

func (s *Service) officialHeadIsLocalTrainer(data *OfficialClassHeadStore) bool {
	if data == nil {
		return true
	}
	trainer := strings.TrimSpace(data.TrainerNodeID)
	if trainer == "" {
		return true
	}
	return trainer == s.localOfficialHeadNodeID()
}

func (s *Service) SetOfficialClassHeadTrainer(nodeID string) (OfficialClassHeadView, error) {
	return s.SetOfficialClassHeadTrainerFor("", nodeID)
}

func (s *Service) SetOfficialClassHeadTrainerFor(groupID, nodeID string) (OfficialClassHeadView, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID != "" {
		found := false
		for _, id := range s.officialHeadNodeIDs() {
			if id == nodeID {
				found = true
				break
			}
		}
		if !found {
			return OfficialClassHeadView{}, errors.New("trainer node is not in the cluster roster")
		}
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	data.TrainerNodeID = nodeID
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) TrainOfficialClassHeadNow() error {
	return s.TrainOfficialClassHeadNowFor("")
}

func (s *Service) TrainOfficialClassHeadNowFor(groupID string) error {
	if s == nil {
		return errors.New("missing llm service")
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	if !s.officialHeadIsLocalTrainer(data) {
		data.LastTrainError = "this node is not the designated trainer"
		_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
		return errors.New(data.LastTrainError)
	}
	labeled := make([]llmpool.LabeledEmbedding, 0, len(data.Samples))
	for i := range data.Samples {
		sample := &data.Samples[i]
		label := llmpool.NormalizeWorkloadClass(sample.GoldClass)
		if !sample.Gold || !llmpool.IsWorkloadClass(label) {
			continue
		}
		vec := sample.Embedding
		if len(vec) < llmpool.HeadDim {
			got, err := embedOfficialHeadPreview(sample.Preview)
			if err != nil {
				data.LastTrainError = err.Error()
				_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
				return err
			}
			sample.Embedding = got
			vec = got
		}
		labeled = append(labeled, llmpool.LabeledEmbedding{Embedding: vec, Label: label})
	}
	if len(labeled) == 0 {
		data.LastTrainError = "no gold samples to train"
		_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
		return errors.New(data.LastTrainError)
	}
	version := llmpool.NextHeadVersion(data.Current, data.Previous, data.History)
	head, err := llmpool.TrainClassificationHead(labeled, version, llmpool.DefaultHeadTau)
	if err != nil {
		data.LastTrainError = err.Error()
		_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
		return err
	}
	rotateOfficialHead(data, &head, llmpool.HeadSourceTrain)
	data.LastTrainAt = head.TrainedAt
	data.LastTrainError = ""
	data.Status = deriveOfficialHeadStatus(data)
	return s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
}

func (s *Service) EnqueueOfficialClassHeadTrain() error {
	return s.EnqueueOfficialClassHeadTrainFor("")
}

func (s *Service) EnqueueOfficialClassHeadTrainFor(groupID string) error {
	if s == nil {
		return errors.New("missing llm service")
	}
	groupID = strings.TrimSpace(groupID)
	officialClassHeadMu.Lock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	if !s.officialHeadIsLocalTrainer(data) {
		officialClassHeadMu.Unlock()
		return errors.New("this node is not the designated trainer")
	}
	officialClassHeadMu.Unlock()
	officialHeadTrainQueued.Lock()
	if officialHeadTrainPending == nil {
		officialHeadTrainPending = map[string]bool{}
	}
	if officialHeadTrainPending[groupID] {
		officialHeadTrainQueued.Unlock()
		return nil
	}
	officialHeadTrainPending[groupID] = true
	officialHeadTrainQueued.Unlock()
	officialClassHeadMu.Lock()
	data = s.loadOfficialClassHeadLocked(context.Background(), groupID)
	data.Status = llmpool.HeadStatusTraining
	_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
	officialClassHeadMu.Unlock()
	officialHeadTrainOnce.Do(func() {
		officialHeadTrainCh = make(chan officialHeadTrainJob, 4)
		go func() {
			for job := range officialHeadTrainCh {
				func(job officialHeadTrainJob) {
					defer func() {
						officialHeadTrainQueued.Lock()
						if officialHeadTrainPending != nil {
							delete(officialHeadTrainPending, job.groupID)
						}
						officialHeadTrainQueued.Unlock()
					}()
					_ = job.svc.TrainOfficialClassHeadNowFor(job.groupID)
				}(job)
			}
		}()
	})
	officialHeadTrainCh <- officialHeadTrainJob{svc: s, groupID: groupID}
	return nil
}

func (s *Service) SetOfficialClassHeadPipeline(mode, override, reason string) (OfficialClassHeadView, error) {
	return s.SetOfficialClassHeadPipelineFor("", mode, override, reason)
}

func (s *Service) SetOfficialClassHeadPipelineFor(groupID, mode, override, reason string) (OfficialClassHeadView, error) {
	next := llmpool.NormalizePipelineMode(mode)
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	recent, previous := officialHeadEvalWindows(data, time.Now())
	gates := llmpool.EvaluatePromoteGates(recent, previous, s.officialHeadTrainerReady(data) && llmpool.DistributeComplete(data.DistributeAck))
	if err := llmpool.AllowPipelineChange(data.Pipeline, next, officialHeadArtifactReady(data), llmpool.DistributeComplete(data.DistributeAck), gates, override, reason); err != nil {
		if !llmpool.IsPipelineRuleBlocked(err) {
			data.Status = llmpool.HeadStatusGatesFailed
			_ = s.saveOfficialClassHeadLocked(context.Background(), groupID, data)
		}
		return buildOfficialClassHeadView(s, groupID, data), err
	}
	data.Pipeline = next
	if strings.EqualFold(strings.TrimSpace(override), llmpool.PromoteOverride) {
		data.OverrideReason = strings.TrimSpace(reason)
	}
	if next == llmpool.PipelineOn {
		s.seedOfficialHeadDistribute(data)
	}
	data.Status = deriveOfficialHeadStatus(data)
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func dropOfficialHeadReviews(reviews []OfficialClassHeadReview, sampleID string) []OfficialClassHeadReview {
	if len(reviews) == 0 {
		return reviews
	}
	out := make([]OfficialClassHeadReview, 0, len(reviews))
	for _, review := range reviews {
		if review.SampleID == sampleID {
			continue
		}
		out = append(out, review)
	}
	return out
}

func upsertOfficialHeadReview(reviews []OfficialClassHeadReview, next OfficialClassHeadReview) []OfficialClassHeadReview {
	for i := range reviews {
		if reviews[i].SampleID == next.SampleID {
			reviews[i] = next
			return reviews
		}
	}
	return append(reviews, next)
}

func (s *Service) ReviewOfficialClassHead(sampleID, goldClass string) (OfficialClassHeadView, error) {
	return s.ReviewOfficialClassHeadFor("", sampleID, goldClass)
}

func (s *Service) ReviewOfficialClassHeadFor(groupID, sampleID, goldClass string) (OfficialClassHeadView, error) {
	sampleID = strings.TrimSpace(sampleID)
	if sampleID == "" {
		return OfficialClassHeadView{}, errors.New("sample_id is required")
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	var sample *OfficialClassHeadSample
	for i := range data.Samples {
		if data.Samples[i].ID == sampleID {
			sample = &data.Samples[i]
			break
		}
	}
	if sample == nil {
		return OfficialClassHeadView{}, errors.New("unknown sample")
	}
	if strings.TrimSpace(goldClass) == "" {
		sample.Gold = false
		sample.GoldClass = ""
		data.Reviews = dropOfficialHeadReviews(data.Reviews, sampleID)
		if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
			return OfficialClassHeadView{}, err
		}
		return buildOfficialClassHeadView(s, groupID, data), nil
	}
	gold := llmpool.NormalizeWorkloadClass(goldClass)
	if !llmpool.IsWorkloadClass(gold) {
		return OfficialClassHeadView{}, errors.New("gold_class must be a frozen workload class")
	}
	sample.Gold = true
	sample.GoldClass = gold
	data.Reviews = upsertOfficialHeadReview(data.Reviews, OfficialClassHeadReview{
		SampleID:  sample.ID,
		At:        time.Now().UTC(),
		GoldClass: gold,
		Predicted: sample.HeadClass,
		RuleClass: sample.RuleClass,
	})
	if len(data.Reviews) > officialClassHeadMaxReviews {
		data.Reviews = data.Reviews[len(data.Reviews)-officialClassHeadMaxReviews:]
	}
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) DeleteOfficialClassHeadSample(sampleID string) (OfficialClassHeadView, error) {
	return s.DeleteOfficialClassHeadSampleFor("", sampleID)
}

func (s *Service) DeleteOfficialClassHeadSampleFor(groupID, sampleID string) (OfficialClassHeadView, error) {
	sampleID = strings.TrimSpace(sampleID)
	if sampleID == "" {
		return OfficialClassHeadView{}, errors.New("sample_id is required")
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	next := make([]OfficialClassHeadSample, 0, len(data.Samples))
	found := false
	for _, sample := range data.Samples {
		if sample.ID == sampleID {
			found = true
			continue
		}
		next = append(next, sample)
	}
	if !found {
		return OfficialClassHeadView{}, errors.New("unknown sample")
	}
	data.Samples = next
	data.Reviews = dropOfficialHeadReviews(data.Reviews, sampleID)
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) RollbackOfficialClassHead() (OfficialClassHeadView, error) {
	return s.RollbackOfficialClassHeadFor("")
}

func (s *Service) RollbackOfficialClassHeadFor(groupID string) (OfficialClassHeadView, error) {
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	if data.Previous == nil || !data.Previous.Ready() {
		return buildOfficialClassHeadView(s, groupID, data), nil
	}
	if !llmpool.DistributeComplete(data.DistributeAck) {
		return buildOfficialClassHeadView(s, groupID, data), llmpool.ErrDistributeIncomplete
	}
	prev := data.Previous
	prevSrc := data.PreviousSource
	data.Previous = data.Current
	data.PreviousSource = data.CurrentSource
	data.Current = prev
	data.CurrentSource = prevSrc
	s.seedOfficialHeadDistribute(data)
	data.OverrideReason = ""
	if strings.EqualFold(strings.TrimSpace(data.DistributeStatus), llmpool.HeadStatusDistributing) {
		data.Status = llmpool.HeadStatusDistributing
	} else {
		data.Status = llmpool.HeadStatusRolledBack
	}
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) finishOfficialHeadAckLocked(data *OfficialClassHeadStore) {
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
	data.Status = deriveOfficialHeadStatus(data)
}

func (s *Service) AckOfficialClassHeadAfterRemoteApply() {
	s.AckOfficialClassHeadAfterRemoteApplyKey(OfficialClassHeadKey)
}

func (s *Service) AckOfficialClassHeadAfterRemoteApplyKey(key string) {
	if s == nil || strings.TrimSpace(key) != OfficialClassHeadKey {
		return
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), "")
	if !officialHeadArtifactReady(data) || data.DistributeAck == nil || len(data.DistributeAck) == 0 {
		return
	}
	local := s.localOfficialHeadNodeID()
	if data.DistributeAck[local] == "acked" {
		return
	}
	data.DistributeAck[local] = "acked"
	s.finishOfficialHeadAckLocked(data)
	_ = s.saveOfficialClassHeadLocked(context.Background(), "", data)
}

func (s *Service) DistributeOfficialClassHead(nodeID string) (OfficialClassHeadView, error) {
	return s.DistributeOfficialClassHeadFor("", nodeID)
}

func (s *Service) DistributeOfficialClassHeadFor(groupID, nodeID string) (OfficialClassHeadView, error) {
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	if !officialHeadArtifactReady(data) {
		return buildOfficialClassHeadView(s, groupID, data), errors.New("artifact not ready")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		nodeID = s.localOfficialHeadNodeID()
	}
	if data.DistributeAck == nil {
		data.DistributeAck = map[string]string{}
	}
	data.DistributeAck[nodeID] = "acked"
	s.finishOfficialHeadAckLocked(data)
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) SeedGroupFromPublishedOfficial(groupID string) (OfficialClassHeadView, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return OfficialClassHeadView{}, errors.New("group_id is required")
	}
	if llmpool.IsOfficialConventionGroupID(groupID) {
		return OfficialClassHeadView{}, errors.New("official group publishes the head; it cannot pull itself")
	}
	if s == nil {
		return OfficialClassHeadView{}, errors.New("missing llm service")
	}
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	src := s.loadOfficialClassHeadLocked(context.Background(), "")
	mode := llmpool.NormalizePipelineMode(src.Pipeline)
	head := s.servingOfficialHead(src)
	if head == nil || !head.Ready() || (mode != llmpool.PipelineCanary && mode != llmpool.PipelineOn) {
		return OfficialClassHeadView{}, errors.New("official head is not published")
	}
	data := s.loadOfficialClassHeadLocked(context.Background(), groupID)
	if officialHeadArtifactReady(data) {
		return buildOfficialClassHeadView(s, groupID, data), errors.New("group head already ready; refuse overwrite")
	}
	cloned := head.Clone()
	rotateOfficialHead(data, cloned, llmpool.HeadSourcePull)
	if data.Current != nil {
		data.LastTrainAt = data.Current.TrainedAt
	}
	data.LastTrainError = ""
	data.Status = deriveOfficialHeadStatus(data)
	if err := s.saveOfficialClassHeadLocked(context.Background(), groupID, data); err != nil {
		return OfficialClassHeadView{}, err
	}
	return buildOfficialClassHeadView(s, groupID, data), nil
}

func (s *Service) resolveClassHeadScoreGroup(ctx context.Context, groupID string) *llmpool.ServiceGroup {
	groupID = strings.TrimSpace(groupID)
	stub := &llmpool.ServiceGroup{ID: groupID, Kind: "dynamic"}
	if s == nil {
		return stub
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil || reg == nil {
		return stub
	}
	var first, official *llmpool.ServiceGroup
	for i := range reg.ServiceGroups {
		id := strings.TrimSpace(reg.ServiceGroups[i].ID)
		if id == "" {
			continue
		}
		copied := reg.ServiceGroups[i]
		if groupID != "" && strings.EqualFold(id, groupID) {
			return &copied
		}
		if llmpool.IsOfficialConventionGroupID(id) {
			if official == nil {
				official = &copied
			}
			continue
		}
		if !llmpool.IsDynamicKind(copied.Kind) {
			continue
		}
		if first == nil || (len(first.Routes) == 0 && len(copied.Routes) > 0) {
			first = &copied
		}
	}
	if groupID != "" {
		return stub
	}
	if first != nil {
		return first
	}
	if official != nil {
		return official
	}
	return stub
}

func (s *Service) ScoreOfficialClassHeadFor(ctx context.Context, groupID, slot string, header http.Header, body map[string]any) (llmpool.ClassHeadScoreReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	officialClassHeadMu.Lock()
	data := s.loadOfficialClassHeadLocked(ctx, "")
	officialClassHeadMu.Unlock()
	role, head, err := llmpool.ResolveHeadSlot(slot, data.Current, data.Previous)
	if err != nil {
		return llmpool.ClassHeadScoreReport{}, err
	}
	preview, err := llmpool.ScoreRequestPreview(body)
	if err != nil {
		return llmpool.ClassHeadScoreReport{}, err
	}
	vec, err := embedOfficialHeadPreview(preview)
	if err != nil {
		return llmpool.ClassHeadScoreReport{}, err
	}
	pred := head.Predict(vec)
	group := s.resolveClassHeadScoreGroup(ctx, groupID)
	report := llmpool.ScoreHeadAgainstRules(group, header, body, role, head, pred)
	report.StoreKey = OfficialClassHeadKey
	if group != nil {
		report.GroupID = strings.TrimSpace(group.ID)
	}
	report.EmbedderReady = officialHeadEmbedderAvailable()
	return report, nil
}

type PublishedOfficialHead struct {
	Published bool                        `json:"published"`
	Pipeline  string                      `json:"pipeline"`
	Head      *llmpool.ClassificationHead `json:"head,omitempty"`
}

func (s *Service) PublishedOfficialHead() PublishedOfficialHead {
	officialClassHeadMu.Lock()
	data := s.loadOfficialClassHeadLocked(context.Background(), "")
	officialClassHeadMu.Unlock()
	mode := llmpool.NormalizePipelineMode(data.Pipeline)
	out := PublishedOfficialHead{Pipeline: mode}
	head := s.servingOfficialHead(data)
	if head != nil && head.Ready() && (mode == llmpool.PipelineCanary || mode == llmpool.PipelineOn) {
		out.Published = true
		out.Head = head
	}
	return out
}
