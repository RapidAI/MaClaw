package httpapi

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
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmClassHeadKey         = "llm_class_head_v1"
	llmClassHeadStoreVer    = 1
	llmClassHeadMaxSamples  = 1500
	llmClassHeadMaxReviews  = 2000
	llmClassHeadListSamples = 30
)

var (
	llmClassHeadMu        sync.Mutex
	classHeadEmbedderFn   func(string) ([]float32, error)
	classHeadTrainOnce    sync.Once
	classHeadTrainCh      chan classHeadTrainJob
	classHeadTrainQueued  sync.Map
	classHeadSettingsBase store.SystemSettingsRepository
)

func bindClassHeadSettings(system store.SystemSettingsRepository) {
	classHeadSettingsBase = system
}

type classHeadTrainJob struct {
	TenantID string
	GroupID  string
	System   store.SystemSettingsRepository
}

func classHeadGroupID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if r.URL != nil {
		if id := strings.TrimSpace(r.URL.Query().Get("group_id")); id != "" {
			return id
		}
	}
	return ""
}

func llmClassHeadSettingKey(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return llmClassHeadKey
	}
	return llmClassHeadKey + ":" + groupID
}

func classHeadTrainQueueKey(tenantID, groupID string) string {
	return strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(groupID)
}

type llmClassHeadStore struct {
	Version          int                            `json:"version"`
	Pipeline         string                         `json:"pipeline"`
	Status           string                         `json:"status"`
	OverrideReason   string                         `json:"override_reason,omitempty"`
	Current          *llmpool.ClassificationHead    `json:"current,omitempty"`
	Previous         *llmpool.ClassificationHead    `json:"previous,omitempty"`
	CurrentSource    string                         `json:"current_source,omitempty"`
	PreviousSource   string                         `json:"previous_source,omitempty"`
	History          []llmpool.ClassHeadVersionInfo `json:"history,omitempty"`
	TrainerNodeID    string                         `json:"trainer_node_id,omitempty"`
	Samples          []llmClassHeadSample           `json:"samples,omitempty"`
	Reviews          []llmClassHeadReview           `json:"reviews,omitempty"`
	LastTrainAt      string                         `json:"last_train_at,omitempty"`
	LastTrainError   string                         `json:"last_train_error,omitempty"`
	DistributeStatus string                         `json:"distribute_status,omitempty"`
	DistributeAck    map[string]string              `json:"distribute_ack,omitempty"`
}

type llmClassHeadSample struct {
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

type llmClassHeadReview struct {
	SampleID  string    `json:"sample_id,omitempty"`
	At        time.Time `json:"at"`
	GoldClass string    `json:"gold_class"`
	Predicted string    `json:"predicted,omitempty"`
	RuleClass string    `json:"rule_class,omitempty"`
}

type llmClassHeadResponse struct {
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
	Samples          []llmClassHeadSample           `json:"samples,omitempty"`
	LastTrainError   string                         `json:"last_train_error,omitempty"`
	TrainerNodeID    string                         `json:"trainer_node_id,omitempty"`
	LocalNodeID      string                         `json:"local_node_id,omitempty"`
	ClusterNodes     []string                       `json:"cluster_nodes,omitempty"`
	PreviousVersion  int                            `json:"previous_version,omitempty"`
	StoreKey         string                         `json:"store_key,omitempty"`
	Versions         []llmpool.ClassHeadVersionInfo `json:"versions,omitempty"`
	DistributeStatus string                         `json:"distribute_status,omitempty"`
	DistributeAck    map[string]string              `json:"distribute_ack,omitempty"`
}

func skipOfficialFacadeGroups(ids []string) bool {
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if !llmpool.IsOfficialFacadeGroupID(id) {
			return false
		}
	}
	return true
}

func firstTenantHeadGroupID(ids []string) string {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !llmpool.IsOfficialFacadeGroupID(id) {
			return id
		}
	}
	return ""
}

func recordLLMClassHeadSample(system store.SystemSettingsRepository, serviceGroupIDs []string, meta llmservice.OfficialForwardMeta) {
	if system == nil || meta.Passthrough {
		return
	}
	if skipOfficialFacadeGroups(normalizeUsageStringSlice(serviceGroupIDs)) {
		return
	}
	preview := strings.TrimSpace(meta.Preview)
	if preview == "" {
		return
	}
	ruleClass := strings.TrimSpace(meta.RuleClass)
	if ruleClass == "" {
		ruleClass = strings.TrimSpace(meta.WorkloadClass)
	}
	ruleSource := strings.TrimSpace(meta.RuleSource)
	if ruleSource == "" {
		ruleSource = strings.TrimSpace(meta.ClassSource)
	}
	goldClass := ""
	gold := llmpool.IsGoldClassSource(ruleSource)
	if gold {
		goldClass = llmpool.NormalizeWorkloadClass(ruleClass)
		if goldClass == "" {
			gold = false
		}
	}
	sample := llmClassHeadSample{
		ID:         classHeadSampleID(preview, ruleClass, time.Now()),
		At:         time.Now().UTC(),
		Preview:    truncateRunes(preview, 400),
		GroupID:    firstTenantHeadGroupID(serviceGroupIDs),
		RuleClass:  ruleClass,
		RuleSource: ruleSource,
		HeadClass:  strings.TrimSpace(meta.HeadClass),
		HeadMaxP:   meta.HeadMaxP,
		Gold:       gold,
		GoldClass:  goldClass,
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	groupID := firstTenantHeadGroupID(serviceGroupIDs)
	data := loadLLMClassHeadExactLocked(context.Background(), system, groupID)
	data.Samples = append([]llmClassHeadSample{sample}, data.Samples...)
	if len(data.Samples) > llmClassHeadMaxSamples {
		data.Samples = pruneClassHeadSamples(data.Samples, llmClassHeadMaxSamples)
	}
	_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
}

func pruneClassHeadSamples(samples []llmClassHeadSample, max int) []llmClassHeadSample {
	if len(samples) <= max {
		return samples
	}
	kept := make([]llmClassHeadSample, 0, max)
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

func classHeadSampleID(preview, class string, at time.Time) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(preview) + "|" + strings.TrimSpace(class) + "|" + at.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:8])
}

func emptyLLMClassHeadStore() *llmClassHeadStore {
	return &llmClassHeadStore{Version: llmClassHeadStoreVer, Pipeline: llmpool.PipelineOff, Status: llmpool.HeadStatusUnused}
}

func parseLLMClassHeadStore(raw string) *llmClassHeadStore {
	data := emptyLLMClassHeadStore()
	if strings.TrimSpace(raw) == "" {
		return data
	}
	if json.Unmarshal([]byte(raw), data) != nil {
		return emptyLLMClassHeadStore()
	}
	data.Pipeline = llmpool.NormalizePipelineMode(data.Pipeline)
	if strings.TrimSpace(data.Status) == "" {
		data.Status = deriveClassHeadStatus(data)
	}
	return data
}

func loadLLMClassHeadExactLocked(ctx context.Context, system store.SystemSettingsRepository, groupID string) *llmClassHeadStore {
	if system == nil {
		return emptyLLMClassHeadStore()
	}
	raw, err := system.Get(ctx, llmClassHeadSettingKey(groupID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return emptyLLMClassHeadStore()
	}
	return parseLLMClassHeadStore(raw)
}

func classHeadStoreHasLocalData(data *llmClassHeadStore) bool {
	if data == nil {
		return false
	}
	return data.Current != nil || data.Previous != nil || len(data.History) > 0 || len(data.Samples) > 0 || len(data.Reviews) > 0
}

func loadLLMClassHeadLocked(ctx context.Context, system store.SystemSettingsRepository, groupID string) *llmClassHeadStore {
	return loadLLMClassHeadExactLocked(ctx, system, groupID)
}

func saveLLMClassHeadLocked(ctx context.Context, system store.SystemSettingsRepository, groupID string, data *llmClassHeadStore) error {
	if system == nil || data == nil {
		return nil
	}
	data.Version = llmClassHeadStoreVer
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return system.Set(ctx, llmClassHeadSettingKey(groupID), string(raw))
}

func deriveClassHeadStatus(data *llmClassHeadStore) string {
	if data == nil {
		return llmpool.HeadStatusUnused
	}
	if strings.EqualFold(strings.TrimSpace(data.Status), llmpool.HeadStatusTraining) {
		return llmpool.HeadStatusTraining
	}
	if strings.EqualFold(strings.TrimSpace(data.DistributeStatus), llmpool.HeadStatusDistributing) {
		return llmpool.HeadStatusDistributing
	}
	if strings.EqualFold(strings.TrimSpace(data.Status), llmpool.HeadStatusRolledBack) && llmpool.NormalizePipelineMode(data.Pipeline) != llmpool.PipelineOn {
		return llmpool.HeadStatusRolledBack
	}
	if strings.EqualFold(strings.TrimSpace(data.Status), llmpool.HeadStatusGatesFailed) && !classHeadArtifactReady(data) {
		return llmpool.HeadStatusGatesFailed
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

func classHeadArtifactReady(data *llmClassHeadStore) bool {
	return data != nil && data.Current != nil && data.Current.Ready()
}

func classHeadEvalWindows(data *llmClassHeadStore, now time.Time) (llmpool.EvalWindow, llmpool.EvalWindow) {
	recentCutoff := now.Add(-7 * 24 * time.Hour)
	prevCutoff := now.Add(-14 * 24 * time.Hour)
	var recentRows, prevRows []llmpool.GoldEvalRow
	if data != nil {
		for _, review := range data.Reviews {
			row := llmpool.GoldEvalRow{Gold: review.GoldClass, Predicted: review.Predicted, RuleClass: review.RuleClass}
			if !review.At.Before(recentCutoff) {
				recentRows = append(recentRows, row)
			} else if !review.At.Before(prevCutoff) {
				prevRows = append(prevRows, row)
			}
		}
		for _, sample := range data.Samples {
			if !sample.Gold || strings.TrimSpace(sample.GoldClass) == "" {
				continue
			}
			pred := strings.TrimSpace(sample.HeadClass)
			row := llmpool.GoldEvalRow{Gold: sample.GoldClass, Predicted: pred, RuleClass: sample.RuleClass}
			if !sample.At.Before(recentCutoff) {
				recentRows = append(recentRows, row)
			} else if !sample.At.Before(prevCutoff) {
				prevRows = append(prevRows, row)
			}
		}
	}
	return llmpool.AccumulateEvalWindow(recentRows), llmpool.AccumulateEvalWindow(prevRows)
}

func buildClassHeadResponse(data *llmClassHeadStore) llmClassHeadResponse {
	return buildClassHeadResponseFor("", data)
}

func rotateClassHead(data *llmClassHeadStore, next *llmpool.ClassificationHead, source string) {
	if data == nil {
		return
	}
	llmpool.RotateClassificationHead(&data.Current, &data.Previous, &data.CurrentSource, &data.PreviousSource, &data.History, next, source)
}

func buildClassHeadResponseFor(groupID string, data *llmClassHeadStore) llmClassHeadResponse {
	if data == nil {
		data = &llmClassHeadStore{Pipeline: llmpool.PipelineOff, Status: llmpool.HeadStatusUnused}
	}
	recent, previous := classHeadEvalWindows(data, time.Now())
	gates := llmpool.EvaluatePromoteGates(recent, previous, classHeadTrainerReady(data) && llmpool.DistributeComplete(data.DistributeAck))
	samples := append([]llmClassHeadSample(nil), data.Samples...)
	if len(samples) > llmClassHeadListSamples {
		samples = samples[:llmClassHeadListSamples]
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
	view := llmClassHeadResponse{
		Pipeline:         data.Pipeline,
		Status:           deriveClassHeadStatus(data),
		Version:          version,
		TrainedAt:        trainedAt,
		Gates:            gates,
		Accuracy:         recent.Accuracy(),
		PlanRecall:       recent.PlanRecall(),
		RuleAgreement:    recent.RuleAgreement(),
		Reviews:          recent.Reviews,
		HumanReviews:     len(data.Reviews),
		ArtifactReady:    classHeadArtifactReady(data),
		EmbedderReady:    classHeadEmbedderAvailable(),
		Suggestion:       gates.Suggestion,
		Samples:          samples,
		LastTrainError:   data.LastTrainError,
		TrainerNodeID:    data.TrainerNodeID,
		LocalNodeID:      localClassHeadNodeID(),
		ClusterNodes:     classHeadNodeIDs(),
		StoreKey:         llmClassHeadSettingKey(groupID),
		Versions:         llmpool.CollectHeadVersions(data.Current, data.Previous, data.CurrentSource, data.PreviousSource, data.History),
		DistributeStatus: firstNonEmpty(data.DistributeStatus, "local"),
		DistributeAck:    data.DistributeAck,
	}
	if data.Previous != nil {
		view.PreviousVersion = data.Previous.Version
	}
	return view
}

func classHeadEmbedderAvailable() bool {
	if classHeadEmbedderFn != nil {
		return true
	}
	return !embedding.IsNoop(embedding.SharedGemma256())
}

func embedClassHeadPreview(preview string) ([]float32, error) {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return nil, errors.New("empty preview")
	}
	if classHeadEmbedderFn != nil {
		return classHeadEmbedderFn(preview)
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

func goldTrainRows(data *llmClassHeadStore) []llmClassHeadSample {
	if data == nil {
		return nil
	}
	byID := map[string]string{}
	for _, review := range data.Reviews {
		if class := llmpool.NormalizeWorkloadClass(review.GoldClass); llmpool.IsWorkloadClass(class) {
			byID[review.SampleID] = class
		}
	}
	out := make([]llmClassHeadSample, 0, len(data.Samples))
	for _, sample := range data.Samples {
		label := strings.TrimSpace(byID[sample.ID])
		if label == "" && sample.Gold {
			label = llmpool.NormalizeWorkloadClass(sample.GoldClass)
		}
		if !llmpool.IsWorkloadClass(label) {
			continue
		}
		sample.GoldClass = label
		out = append(out, sample)
	}
	return out
}

func trainClassHeadNow(system store.SystemSettingsRepository, groupID string) error {
	if system == nil {
		return errors.New("missing system settings")
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(context.Background(), system, groupID)
	if !classHeadIsLocalTrainer(data) {
		data.LastTrainError = "this node is not the designated trainer"
		_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
		return errors.New(data.LastTrainError)
	}
	rows := goldTrainRows(data)
	labeled := make([]llmpool.LabeledEmbedding, 0, len(rows))
	for _, row := range rows {
		vec := row.Embedding
		if len(vec) < llmpool.HeadDim {
			got, err := embedClassHeadPreview(row.Preview)
			if err != nil {
				data.LastTrainError = err.Error()
				data.Status = deriveClassHeadStatus(data)
				_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
				return err
			}
			vec = got
			if idx := indexClassHeadSample(data.Samples, row.ID); idx >= 0 {
				data.Samples[idx].Embedding = got
			}
		}
		labeled = append(labeled, llmpool.LabeledEmbedding{Embedding: vec, Label: row.GoldClass})
	}
	if len(labeled) == 0 {
		data.LastTrainError = "no gold samples to train"
		_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
		return errors.New(data.LastTrainError)
	}
	version := llmpool.NextHeadVersion(data.Current, data.Previous, data.History)
	head, err := llmpool.TrainClassificationHead(labeled, version, llmpool.DefaultHeadTau)
	if err != nil {
		data.LastTrainError = err.Error()
		_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
		return err
	}
	rotateClassHead(data, &head, llmpool.HeadSourceTrain)
	data.LastTrainAt = head.TrainedAt
	data.LastTrainError = ""
	data.Status = deriveClassHeadStatus(data)
	return saveLLMClassHeadLocked(context.Background(), system, groupID, data)
}

func indexClassHeadSample(samples []llmClassHeadSample, id string) int {
	for i := range samples {
		if samples[i].ID == id {
			return i
		}
	}
	return -1
}

func startClassHeadTrainer() {
	classHeadTrainOnce.Do(func() {
		classHeadTrainCh = make(chan classHeadTrainJob, 16)
		go func() {
			for job := range classHeadTrainCh {
				key := classHeadTrainQueueKey(job.TenantID, job.GroupID)
				func() {
					defer classHeadTrainQueued.Delete(key)
					_ = trainClassHeadNow(job.System, job.GroupID)
				}()
			}
		}()
	})
}

func enqueueClassHeadTrain(tenantID, groupID string, system store.SystemSettingsRepository) error {
	if system == nil {
		return errors.New("missing system settings")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	groupID = strings.TrimSpace(groupID)
	queueKey := classHeadTrainQueueKey(tenantID, groupID)
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadExactLocked(context.Background(), system, groupID)
	if !classHeadIsLocalTrainer(data) {
		llmClassHeadMu.Unlock()
		return errors.New("this node is not the designated trainer")
	}
	llmClassHeadMu.Unlock()
	if _, loaded := classHeadTrainQueued.LoadOrStore(queueKey, true); loaded {
		return nil
	}
	llmClassHeadMu.Lock()
	data = loadLLMClassHeadExactLocked(context.Background(), system, groupID)
	data.Status = llmpool.HeadStatusTraining
	data.LastTrainError = ""
	_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
	llmClassHeadMu.Unlock()
	startClassHeadTrainer()
	classHeadTrainCh <- classHeadTrainJob{TenantID: tenantID, GroupID: groupID, System: system}
	return nil
}

func classHeadRuntime(system store.SystemSettingsRepository, userID, groupID string) *llmpool.HeadRuntime {
	if system == nil {
		return nil
	}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadExactLocked(context.Background(), system, groupID)
	head := servingClassHead(data)
	if ackClassHeadAfterSharedLoadLocked(data) {
		_ = saveLLMClassHeadLocked(context.Background(), system, groupID, data)
	}
	llmClassHeadMu.Unlock()
	mode := llmpool.NormalizePipelineMode(data.Pipeline)
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
			vec, err := embedClassHeadPreview(preview)
			if err != nil {
				return llmpool.HeadPrediction{Class: llmpool.WorkloadFallbackBalanced}
			}
			return head.Predict(vec)
		},
	}
}

func classHeadTenantID(r *http.Request) string {
	if r == nil {
		return store.DefaultTenantID
	}
	if id := strings.TrimSpace(store.TenantIDFromContext(r.Context())); id != "" {
		return id
	}
	return RequestTenantID(r)
}

func classHeadUserID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := r.Context().Value(llmClassHeadUserKey{}); value != nil {
		if id, ok := value.(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

type llmClassHeadUserKey struct{}

func withClassHeadUser(ctx context.Context, userID string) context.Context {
	if ctx == nil {
		return ctx
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, llmClassHeadUserKey{}, userID)
}

func GetLLMClassHeadHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := classHeadGroupID(r)
		llmClassHeadMu.Lock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		if ackClassHeadAfterSharedLoadLocked(data) {
			_ = saveLLMClassHeadLocked(r.Context(), system, groupID, data)
		}
		llmClassHeadMu.Unlock()
		writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
	}
}

func PostLLMClassHeadTrainHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		if err := enqueueClassHeadTrain(classHeadTenantID(r), classHeadGroupID(r), system); err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_TRAIN_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "status": llmpool.HeadStatusTraining})
	}
}

type llmClassHeadPipelineRequest struct {
	Mode     string `json:"mode"`
	Override string `json:"override"`
	Reason   string `json:"reason"`
}

func PostLLMClassHeadPipelineHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var req llmClassHeadPipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		next := llmpool.NormalizePipelineMode(req.Mode)
		groupID := classHeadGroupID(r)
		llmClassHeadMu.Lock()
		defer llmClassHeadMu.Unlock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		recent, previous := classHeadEvalWindows(data, time.Now())
		gates := llmpool.EvaluatePromoteGates(recent, previous, classHeadTrainerReady(data) && llmpool.DistributeComplete(data.DistributeAck))
		if err := llmpool.AllowPipelineChange(data.Pipeline, next, classHeadArtifactReady(data), llmpool.DistributeComplete(data.DistributeAck), gates, req.Override, req.Reason); err != nil {
			if !llmpool.IsPipelineRuleBlocked(err) {
				data.Status = llmpool.HeadStatusGatesFailed
				_ = saveLLMClassHeadLocked(r.Context(), system, groupID, data)
			}
			code := "LLM_CLASS_HEAD_PROMOTE_BLOCKED"
			if llmpool.IsPipelineHopBlocked(err) {
				code = "LLM_CLASS_HEAD_PIPELINE_HOP"
			} else if llmpool.IsServingRequired(err) {
				code = "LLM_CLASS_HEAD_SERVING_REQUIRED"
			} else if llmpool.IsDistributeIncomplete(err) {
				code = "LLM_CLASS_HEAD_DISTRIBUTE_INCOMPLETE"
			}
			writeError(w, http.StatusConflict, code, err.Error())
			return
		}
		data.Pipeline = next
		if strings.EqualFold(strings.TrimSpace(req.Override), llmpool.PromoteOverride) {
			data.OverrideReason = strings.TrimSpace(req.Reason)
		}
		if next == llmpool.PipelineOn {
			seedClassHeadDistribute(data)
		}
		data.Status = deriveClassHeadStatus(data)
		if err := saveLLMClassHeadLocked(r.Context(), system, groupID, data); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_CLASS_HEAD_SAVE_FAILED", err.Error())
			return
		}
		if next == llmpool.PipelineOn {
			go pushClassHeadToPeers(classHeadTenantID(r), groupID, data)
		}
		writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
	}
}

type llmClassHeadReviewRequest struct {
	SampleID  string `json:"sample_id"`
	GoldClass string `json:"gold_class"`
}

func PostLLMClassHeadReviewHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var req llmClassHeadReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		gold := llmpool.NormalizeWorkloadClass(req.GoldClass)
		if !llmpool.IsWorkloadClass(gold) {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_GOLD_INVALID", "gold_class must be a frozen workload class")
			return
		}
		groupID := classHeadGroupID(r)
		llmClassHeadMu.Lock()
		defer llmClassHeadMu.Unlock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		var sample *llmClassHeadSample
		for i := range data.Samples {
			if data.Samples[i].ID == strings.TrimSpace(req.SampleID) {
				sample = &data.Samples[i]
				break
			}
		}
		if sample == nil {
			writeError(w, http.StatusNotFound, "LLM_CLASS_HEAD_SAMPLE_NOT_FOUND", "unknown sample")
			return
		}
		sample.Gold = true
		sample.GoldClass = gold
		predicted := strings.TrimSpace(sample.HeadClass)
		if predicted == "" && classHeadArtifactReady(data) && len(sample.Embedding) >= llmpool.HeadDim {
			predicted = data.Current.Predict(sample.Embedding).Class
		}
		data.Reviews = append(data.Reviews, llmClassHeadReview{
			SampleID:  sample.ID,
			At:        time.Now().UTC(),
			GoldClass: gold,
			Predicted: predicted,
			RuleClass: sample.RuleClass,
		})
		if len(data.Reviews) > llmClassHeadMaxReviews {
			data.Reviews = data.Reviews[len(data.Reviews)-llmClassHeadMaxReviews:]
		}
		if err := saveLLMClassHeadLocked(r.Context(), system, groupID, data); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_CLASS_HEAD_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
	}
}

var (
	officialHeadPullFn           func(context.Context) (*llmpool.ClassificationHead, error)
	errClassHeadGroupRequired    = errors.New("group_id is required")
	errClassHeadOfficialFacade   = errors.New("official facade cannot pull official head")
	errClassHeadAlreadyReady     = errors.New("group head already ready; refuse overwrite")
	errClassHeadOfficialNotReady = errors.New("official head is not ready")
)

func pullOfficialClassHeadNow(ctx context.Context, system store.SystemSettingsRepository, groupID string) error {
	if system == nil {
		return errors.New("missing system settings")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return errClassHeadGroupRequired
	}
	if llmpool.IsOfficialFacadeGroupID(groupID) {
		return errClassHeadOfficialFacade
	}
	fetch := officialHeadPullFn
	if fetch == nil {
		fetch = defaultPullOfficialClassHead
	}
	head, err := fetch(ctx)
	if err != nil {
		return err
	}
	if head == nil || !head.Ready() {
		return errClassHeadOfficialNotReady
	}
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(ctx, system, groupID)
	if classHeadArtifactReady(data) {
		return errClassHeadAlreadyReady
	}
	rotateClassHead(data, head.Clone(), llmpool.HeadSourcePull)
	if data.Current != nil {
		data.LastTrainAt = data.Current.TrainedAt
	}
	data.LastTrainError = ""
	data.Status = deriveClassHeadStatus(data)
	return saveLLMClassHeadLocked(ctx, system, groupID, data)
}

func defaultPullOfficialClassHead(ctx context.Context) (*llmpool.ClassificationHead, error) {
	mod := GetMaClawModule()
	if mod == nil || mod.Client == nil {
		return nil, errors.New("hub is not bound to HubCenter")
	}
	return mod.Client.PullPublishedOfficialHead(ctx)
}

func PostLLMClassHeadPullOfficialHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := classHeadGroupID(r)
		if err := pullOfficialClassHeadNow(r.Context(), system, groupID); err != nil {
			status := http.StatusConflict
			if errors.Is(err, errClassHeadGroupRequired) || errors.Is(err, errClassHeadOfficialFacade) {
				status = http.StatusBadRequest
			}
			writeError(w, status, "LLM_CLASS_HEAD_PULL_FAILED", err.Error())
			return
		}
		llmClassHeadMu.Lock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		llmClassHeadMu.Unlock()
		writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
	}
}

func PostLLMClassHeadRollbackHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := classHeadGroupID(r)
		llmClassHeadMu.Lock()
		defer llmClassHeadMu.Unlock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		if data.Previous == nil || !data.Previous.Ready() {
			writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
			return
		}
		if !llmpool.DistributeComplete(data.DistributeAck) {
			writeError(w, http.StatusConflict, "LLM_CLASS_HEAD_DISTRIBUTE_INCOMPLETE", llmpool.ErrDistributeIncomplete.Error())
			return
		}
		prev := data.Previous
		prevSrc := data.PreviousSource
		data.Previous = data.Current
		data.PreviousSource = data.CurrentSource
		data.Current = prev
		data.CurrentSource = prevSrc
		seedClassHeadDistribute(data)
		data.OverrideReason = ""
		if strings.EqualFold(strings.TrimSpace(data.DistributeStatus), llmpool.HeadStatusDistributing) {
			data.Status = llmpool.HeadStatusDistributing
		} else {
			data.Status = llmpool.HeadStatusRolledBack
		}
		if err := saveLLMClassHeadLocked(r.Context(), system, groupID, data); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_CLASS_HEAD_SAVE_FAILED", err.Error())
			return
		}
		go pushClassHeadToPeers(classHeadTenantID(r), groupID, data)
		writeJSON(w, http.StatusOK, buildClassHeadResponseFor(groupID, data))
	}
}

func PostLLMClassHeadScoreHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		groupID := classHeadGroupID(r)
		var req struct {
			Slot    string            `json:"slot"`
			Text    string            `json:"text"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		body := req.Body
		if body == nil {
			body = map[string]any{}
		}
		if strings.TrimSpace(req.Text) != "" {
			if _, ok := body["messages"]; !ok {
				body["messages"] = []any{map[string]any{"role": "user", "content": req.Text}}
			}
			if _, ok := body["model"]; !ok {
				body["model"] = "auto"
			}
		}
		header := http.Header{}
		for key, value := range req.Headers {
			header.Set(key, value)
		}
		llmClassHeadMu.Lock()
		data := loadLLMClassHeadExactLocked(r.Context(), system, groupID)
		llmClassHeadMu.Unlock()
		role, head, err := llmpool.ResolveHeadSlot(req.Slot, data.Current, data.Previous)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_SCORE_FAILED", err.Error())
			return
		}
		preview, err := llmpool.ScoreRequestPreview(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_SCORE_FAILED", err.Error())
			return
		}
		vec, err := embedClassHeadPreview(preview)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LLM_CLASS_HEAD_SCORE_FAILED", err.Error())
			return
		}
		pred := head.Predict(vec)
		var group *llmpool.ServiceGroup
		if reg, loadErr := loadCachedLLMServiceRegistry(r.Context(), system); loadErr == nil && reg != nil {
			if found := reg.FindModelServiceGroup(groupID); found != nil {
				pool := found.ToPoolGroup()
				group = &pool
			}
		}
		if group == nil {
			group = &llmpool.ServiceGroup{ID: groupID, Kind: "dynamic"}
		}
		report := llmpool.ScoreHeadAgainstRules(group, header, body, role, head, pred)
		report.StoreKey = llmClassHeadSettingKey(groupID)
		report.EmbedderReady = classHeadEmbedderAvailable()
		writeJSON(w, http.StatusOK, report)
	}
}
