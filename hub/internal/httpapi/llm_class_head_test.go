package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestRecordLLMClassHeadSampleSkipsOfficialFacade(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	meta := llmservice.OfficialForwardMeta{
		Preview:     "design a new architecture",
		RuleClass:   llmpool.WorkloadClassDesign,
		RuleSource:  llmpool.ClassSourceHint,
		Passthrough: false,
	}
	recordLLMClassHeadSample(sys, []string{llmpool.HubOfficialServiceGroupID}, meta)
	recordLLMClassHeadSample(sys, []string{llmpool.OfficialGroupID}, meta)
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if len(data.Samples) != 0 {
		t.Fatalf("official facade samples leaked: %#v", data.Samples)
	}
}

func TestRecordLLMClassHeadSampleKeepsTenantGold(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	recordLLMClassHeadSample(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		Preview:    "design a new architecture",
		RuleClass:  llmpool.WorkloadClassDesign,
		RuleSource: llmpool.ClassSourceHint,
	})
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "coding-auto")
	legacy := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if len(data.Samples) != 1 || !data.Samples[0].Gold || data.Samples[0].GoldClass != llmpool.WorkloadClassDesign || data.Samples[0].GroupID != "coding-auto" {
		t.Fatalf("sample = %#v", data.Samples)
	}
	if len(legacy.Samples) != 0 {
		t.Fatalf("tenant legacy key should stay empty: %#v", legacy.Samples)
	}
}

func TestTrainClassHeadNowUsesStoredEmbeddings(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	samples := make([]llmClassHeadSample, 0, len(llmpool.FrozenWorkloadClasses))
	for i, class := range llmpool.FrozenWorkloadClasses {
		vec := make([]float32, llmpool.HeadDim)
		vec[i] = 1
		samples = append(samples, llmClassHeadSample{
			ID:        class,
			At:        time.Now().UTC(),
			Preview:   class + " request",
			Embedding: vec,
			Gold:      true,
			GoldClass: class,
			RuleClass: class,
		})
	}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	data.Samples = samples
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	if err := trainClassHeadNow(sys, ""); err != nil {
		t.Fatal(err)
	}
	llmClassHeadMu.Lock()
	got := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if !classHeadArtifactReady(got) {
		t.Fatalf("artifact not ready: %#v", got.Current)
	}
	if llmpool.NormalizePipelineMode(got.Pipeline) != llmpool.PipelineOff || deriveClassHeadStatus(got) != llmpool.HeadStatusUnused {
		t.Fatalf("train must not auto-enter shadow: pipeline=%q status=%q", got.Pipeline, deriveClassHeadStatus(got))
	}
}

func TestPostLLMClassHeadPipelineBlockedWithoutGates(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	body, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineOn})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func seedReadyHubClassHead(t *testing.T, sys *stubSystemSettings) {
	t.Helper()
	llmClassHeadMu.Lock()
	defer llmClassHeadMu.Unlock()
	data := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		t.Fatal(err)
	}
}

func TestPostLLMClassHeadPipelineNeedsServing(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	body, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineShadow})
	rec := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(rec, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LLM_CLASS_HEAD_SERVING_REQUIRED") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostLLMClassHeadPipelineOverridePromotes(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	hop, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineCanary, Override: llmpool.PromoteOverride, Reason: "lab override"})
	blocked := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(blocked, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(hop)))
	if blocked.Code != http.StatusConflict {
		t.Fatalf("off to canary status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	seedReadyHubClassHead(t, sys)
	shadow, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineShadow})
	PostLLMClassHeadPipelineHandler(sys)(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(shadow)))
	body, _ := json.Marshal(llmClassHeadPipelineRequest{
		Mode:     llmpool.PipelineCanary,
		Override: llmpool.PromoteOverride,
		Reason:   "lab override",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got llmClassHeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pipeline != llmpool.PipelineCanary {
		t.Fatalf("pipeline = %q", got.Pipeline)
	}
}

func TestPullOfficialClassHeadRefusesOverwrite(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadExactLocked(t.Context(), sys, "coding-auto")
	data.Current = &head
	if err := saveLLMClassHeadLocked(t.Context(), sys, "coding-auto", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	officialHeadPullFn = func(context.Context) (*llmpool.ClassificationHead, error) {
		pulled := llmpool.EmptyHead(9, llmpool.DefaultHeadTau)
		return &pulled, nil
	}
	t.Cleanup(func() { officialHeadPullFn = nil })
	if err := pullOfficialClassHeadNow(t.Context(), sys, "coding-auto"); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestPullOfficialClassHeadColdStart(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	pulled := llmpool.EmptyHead(4, llmpool.DefaultHeadTau)
	officialHeadPullFn = func(context.Context) (*llmpool.ClassificationHead, error) {
		return &pulled, nil
	}
	t.Cleanup(func() { officialHeadPullFn = nil })
	if err := pullOfficialClassHeadNow(t.Context(), sys, "coding-auto"); err != nil {
		t.Fatal(err)
	}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadExactLocked(t.Context(), sys, "coding-auto")
	legacy := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	writing := loadLLMClassHeadExactLocked(t.Context(), sys, "writing-auto")
	llmClassHeadMu.Unlock()
	if !classHeadArtifactReady(data) || data.Current.Version != 4 {
		t.Fatalf("current = %#v", data.Current)
	}
	if llmpool.NormalizePipelineMode(data.Pipeline) != llmpool.PipelineOff || deriveClassHeadStatus(data) != llmpool.HeadStatusUnused {
		t.Fatalf("pull must not auto-enter shadow: pipeline=%q status=%q", data.Pipeline, deriveClassHeadStatus(data))
	}
	if classHeadArtifactReady(legacy) {
		t.Fatalf("tenant key should stay empty: %#v", legacy.Current)
	}
	if classHeadArtifactReady(writing) {
		t.Fatalf("writing-auto leaked pull: %#v", writing.Current)
	}
}

func TestGetLLMClassHeadHandlerEmpty(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-head", nil)
	rec := httptest.NewRecorder()
	GetLLMClassHeadHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got llmClassHeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pipeline != llmpool.PipelineOff || got.Gates.Total != llmpool.GateCount {
		t.Fatalf("got = %#v", got)
	}
}

func TestEnqueueClassHeadTrainRefusesNonTrainer(t *testing.T) {
	bindClassHeadRoster("node-a", []string{"node-b"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	data.TrainerNodeID = "node-b"
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	if err := enqueueClassHeadTrain("tenant-a", "", sys); err == nil {
		t.Fatal("expected non-trainer to be refused")
	}
}

func TestPromoteClassHeadSeedsPeerPendingAndPromotesOnAck(t *testing.T) {
	bindClassHeadRoster("node-a", []string{"node-b"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(3, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	shadow, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineShadow})
	PostLLMClassHeadPipelineHandler(sys)(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(shadow)))
	body, _ := json.Marshal(llmClassHeadPipelineRequest{
		Mode:     llmpool.PipelineOn,
		Override: llmpool.PromoteOverride,
		Reason:   "lab",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var view llmClassHeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != llmpool.HeadStatusDistributing {
		t.Fatalf("status = %q", view.Status)
	}
	if view.DistributeAck["node-a"] != "acked" || view.DistributeAck["node-b"] != "pending" {
		t.Fatalf("ack = %#v", view.DistributeAck)
	}
	view, err := distributeClassHead(t.Context(), sys, "", "node-b")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status after ack = %q", view.Status)
	}
}

func TestClassHeadRuntimeUsesPreviousWhenLocalPending(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	rt := classHeadRuntime(sys, "u1", "")
	if rt == nil || rt.Head == nil || rt.Head.Version != 1 {
		t.Fatalf("want previous v1, got %#v", rt)
	}
}

func TestRollbackClassHeadSwapsPrevious(t *testing.T) {
	bindClassHeadRoster("node-a", nil, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/rollback", nil)
	rec := httptest.NewRecorder()
	PostLLMClassHeadRollbackHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var view llmClassHeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Pipeline != llmpool.PipelineOn || view.Version != 1 || view.PreviousVersion != 2 {
		t.Fatalf("view = %#v", view)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status = %q", view.Status)
	}
}

func TestRollbackClassHeadBlockedWhileDistributing(t *testing.T) {
	bindClassHeadRoster("node-a", []string{"node-b"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	data.DistributeStatus = llmpool.HeadStatusDistributing
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/rollback", nil)
	rec := httptest.NewRecorder()
	PostLLMClassHeadRollbackHandler(sys)(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LLM_CLASS_HEAD_DISTRIBUTE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostLLMClassHeadPipelineNeedsDistribute(t *testing.T) {
	bindClassHeadRoster("node-a", []string{"node-b"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineShadow
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	data.DistributeStatus = llmpool.HeadStatusDistributing
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	body, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineOn, Override: llmpool.PromoteOverride, Reason: "lab"})
	rec := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(rec, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "LLM_CLASS_HEAD_DISTRIBUTE_INCOMPLETE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDistributeClassHeadAcksLocal(t *testing.T) {
	bindClassHeadRoster("local", nil, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	view, err := distributeClassHead(t.Context(), sys, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.DistributeStatus != "local" || view.DistributeAck["local"] != "acked" {
		t.Fatalf("view = %#v", view)
	}
}

func TestApplyClassHeadReplicaArtifactAcksLocal(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	head := llmpool.EmptyHead(7, llmpool.DefaultHeadTau)
	view, err := applyClassHeadReplicaArtifact(t.Context(), sys, classHeadReplicaArtifact{
		Pipeline: llmpool.PipelineOn,
		Current:  &head,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Version != 7 || view.DistributeAck["node-b"] != "acked" {
		t.Fatalf("view = %#v", view)
	}
}

func TestApplyClassHeadReplicaArtifactArchivesLocalHistory(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "coding-auto")
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &curr
	data.Previous = &prev
	data.CurrentSource = llmpool.HeadSourceTrain
	data.PreviousSource = llmpool.HeadSourceTrain
	if err := saveLLMClassHeadLocked(t.Context(), sys, "coding-auto", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	next := llmpool.EmptyHead(3, llmpool.DefaultHeadTau)
	oldCurr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	view, err := applyClassHeadReplicaArtifact(t.Context(), sys, classHeadReplicaArtifact{
		GroupID:  "coding-auto",
		Pipeline: llmpool.PipelineOn,
		Current:  &next,
		Previous: &oldCurr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Version != 3 || view.PreviousVersion != 2 {
		t.Fatalf("view=%#v", view)
	}
	foundHist := false
	for _, item := range view.Versions {
		if item.Role == llmpool.HeadRoleHistory && item.Version == 1 {
			foundHist = true
		}
	}
	if !foundHist {
		t.Fatalf("history missing %#v", view.Versions)
	}
	if view.StoreKey != "llm_class_head_v1:coding-auto" {
		t.Fatalf("store_key=%q", view.StoreKey)
	}
	foundReplicaSrc := false
	for _, item := range view.Versions {
		if item.Role == llmpool.HeadRoleCurrent && item.Source == llmpool.HeadSourceReplica {
			foundReplicaSrc = true
		}
	}
	if !foundReplicaSrc {
		t.Fatalf("replica source missing %#v", view.Versions)
	}
}

func TestAckClassHeadAfterSharedLoadAcksPending(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.Status = llmpool.HeadStatusDistributing
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	ackClassHeadAfterSharedLoad(t.Context(), sys, "")
	view := buildClassHeadResponse(func() *llmClassHeadStore {
		llmClassHeadMu.Lock()
		defer llmClassHeadMu.Unlock()
		return loadLLMClassHeadLocked(t.Context(), sys, "")
	}())
	if view.DistributeAck["node-b"] != "acked" {
		t.Fatalf("ack = %#v", view.DistributeAck)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status = %q", view.Status)
	}
}

func TestAckClassHeadAfterSharedLoadNoopWhenAcked(t *testing.T) {
	bindClassHeadRoster("node-a", []string{"node-b"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	ackClassHeadAfterSharedLoad(t.Context(), sys, "")
	llmClassHeadMu.Lock()
	got := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if got.DistributeAck["node-b"] != "pending" {
		t.Fatalf("acked node should not rewrite peers: %#v", got.DistributeAck)
	}
}

func TestGetLLMClassHeadHandlerAcksSharedReplica(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	head := llmpool.EmptyHead(4, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-head", nil)
	rec := httptest.NewRecorder()
	GetLLMClassHeadHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var view llmClassHeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.DistributeAck["node-b"] != "acked" || view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("view = %#v", view)
	}
}

func TestClassHeadRuntimeAcksAfterServingPrevious(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	data := loadLLMClassHeadLocked(t.Context(), sys, "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", data); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()
	rt := classHeadRuntime(sys, "u1", "")
	if rt == nil || rt.Head == nil || rt.Head.Version != 1 {
		t.Fatalf("first request should keep previous, got %#v", rt)
	}
	llmClassHeadMu.Lock()
	got := loadLLMClassHeadLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if got.DistributeAck["node-b"] != "acked" {
		t.Fatalf("should ACK after serving previous: %#v", got.DistributeAck)
	}
	rt = classHeadRuntime(sys, "u1", "")
	if rt == nil || rt.Head == nil || rt.Head.Version != 2 {
		t.Fatalf("second request should use current, got %#v", rt)
	}
}

func TestRecordLLMClassHeadSampleIsolatesGroups(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	recordLLMClassHeadSample(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		Preview: "design a service", RuleClass: llmpool.WorkloadClassDesign, RuleSource: llmpool.ClassSourceHint,
	})
	recordLLMClassHeadSample(sys, []string{"writing-auto"}, llmservice.OfficialForwardMeta{
		Preview: "write the weekly report", RuleClass: llmpool.WorkloadClassDocWrite, RuleSource: llmpool.ClassSourceHint,
	})
	llmClassHeadMu.Lock()
	coding := loadLLMClassHeadLocked(t.Context(), sys, "coding-auto")
	writing := loadLLMClassHeadLocked(t.Context(), sys, "writing-auto")
	llmClassHeadMu.Unlock()
	if len(coding.Samples) != 1 || coding.Samples[0].GroupID != "coding-auto" {
		t.Fatalf("coding = %#v", coding.Samples)
	}
	if len(writing.Samples) != 1 || writing.Samples[0].GroupID != "writing-auto" {
		t.Fatalf("writing = %#v", writing.Samples)
	}
}

func TestPullOfficialClassHeadRequiresGroupAndIsolates(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	legacy := llmpool.EmptyHead(3, llmpool.DefaultHeadTau)
	llmClassHeadMu.Lock()
	legacyStore := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	legacyStore.Current = &legacy
	legacyStore.Samples = []llmClassHeadSample{{ID: "tenant-sample", GroupID: "", Preview: "tenant only"}}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", legacyStore); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	writingStore := loadLLMClassHeadExactLocked(t.Context(), sys, "writing-auto")
	writingStore.Samples = []llmClassHeadSample{{ID: "writing-sample", GroupID: "writing-auto", Preview: "keep me"}}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "writing-auto", writingStore); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()

	pulled := llmpool.EmptyHead(7, llmpool.DefaultHeadTau)
	pulled.Weights[0][1] = 0.42
	officialHeadPullFn = func(context.Context) (*llmpool.ClassificationHead, error) {
		return &pulled, nil
	}
	t.Cleanup(func() { officialHeadPullFn = nil })

	if err := pullOfficialClassHeadNow(t.Context(), sys, ""); err == nil {
		t.Fatal("empty group_id should fail")
	}
	if err := pullOfficialClassHeadNow(t.Context(), sys, llmpool.HubOfficialServiceGroupID); err == nil {
		t.Fatal("official facade should fail")
	}
	if err := pullOfficialClassHeadNow(t.Context(), sys, "coding-auto"); err != nil {
		t.Fatal(err)
	}

	llmClassHeadMu.Lock()
	coding := loadLLMClassHeadExactLocked(t.Context(), sys, "coding-auto")
	writing := loadLLMClassHeadExactLocked(t.Context(), sys, "writing-auto")
	tenant := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if !classHeadArtifactReady(coding) || coding.Current.Version != 7 {
		t.Fatalf("coding current = %#v", coding.Current)
	}
	if len(coding.Samples) != 0 {
		t.Fatalf("coding imported samples: %#v", coding.Samples)
	}
	if classHeadArtifactReady(writing) || len(writing.Samples) != 1 || writing.Samples[0].ID != "writing-sample" {
		t.Fatalf("writing leaked: %#v", writing)
	}
	if tenant.Current == nil || tenant.Current.Version != 3 || len(tenant.Samples) != 1 {
		t.Fatalf("tenant key mutated: %#v", tenant)
	}
	pulled.Weights[0][1] = 9
	if coding.Current.Weights[0][1] != 0.42 {
		t.Fatalf("pulled head aliased weights: %v", coding.Current.Weights[0][1])
	}
}

func TestClassHeadGroupIDHandlersIsolateMutations(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	pulled := llmpool.EmptyHead(5, llmpool.DefaultHeadTau)
	officialHeadPullFn = func(context.Context) (*llmpool.ClassificationHead, error) {
		return &pulled, nil
	}
	t.Cleanup(func() { officialHeadPullFn = nil })

	missing := httptest.NewRecorder()
	PostLLMClassHeadPullOfficialHandler(sys)(missing, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pull-official", strings.NewReader("{}")))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing group_id status=%d body=%s", missing.Code, missing.Body.String())
	}

	pull := httptest.NewRecorder()
	PostLLMClassHeadPullOfficialHandler(sys)(pull, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pull-official?group_id=coding-auto", strings.NewReader("{}")))
	if pull.Code != http.StatusOK {
		t.Fatalf("pull status=%d body=%s", pull.Code, pull.Body.String())
	}

	shadow, _ := json.Marshal(llmClassHeadPipelineRequest{Mode: llmpool.PipelineShadow})
	PostLLMClassHeadPipelineHandler(sys)(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline?group_id=coding-auto", bytes.NewReader(shadow)))
	body, _ := json.Marshal(llmClassHeadPipelineRequest{
		Mode:     llmpool.PipelineCanary,
		Override: llmpool.PromoteOverride,
		Reason:   "lab",
	})
	pipe := httptest.NewRecorder()
	PostLLMClassHeadPipelineHandler(sys)(pipe, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline?group_id=coding-auto", bytes.NewReader(body)))
	if pipe.Code != http.StatusOK {
		t.Fatalf("pipeline status=%d body=%s", pipe.Code, pipe.Body.String())
	}

	get := func(groupID string) llmClassHeadResponse {
		t.Helper()
		path := "/api/admin/llm/class-head"
		if groupID != "" {
			path += "?group_id=" + groupID
		}
		out := httptest.NewRecorder()
		GetLLMClassHeadHandler(sys)(out, httptest.NewRequest(http.MethodGet, path, nil))
		if out.Code != http.StatusOK {
			t.Fatalf("get %q status=%d body=%s", groupID, out.Code, out.Body.String())
		}
		var got llmClassHeadResponse
		if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	coding := get("coding-auto")
	writing := get("writing-auto")
	legacy := get("")
	if coding.Version != 5 || coding.Pipeline != llmpool.PipelineCanary {
		t.Fatalf("coding = %#v", coding)
	}
	if writing.Version != 0 || writing.Pipeline != llmpool.PipelineOff {
		t.Fatalf("writing leaked: %#v", writing)
	}
	if legacy.Version != 0 || legacy.Pipeline != llmpool.PipelineOff {
		t.Fatalf("legacy leaked: %#v", legacy)
	}

	rb := httptest.NewRecorder()
	PostLLMClassHeadRollbackHandler(sys)(rb, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/rollback?group_id=coding-auto", nil))
	if rb.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", rb.Code, rb.Body.String())
	}
	if get("writing-auto").Pipeline != llmpool.PipelineOff {
		t.Fatalf("rollback mutated writing: %#v", get("writing-auto"))
	}
}

func TestGetClassHeadAckDoesNotCopyTenantStore(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	head := llmpool.EmptyHead(9, llmpool.DefaultHeadTau)
	llmClassHeadMu.Lock()
	tenant := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	tenant.Current = &head
	tenant.Pipeline = llmpool.PipelineOn
	tenant.DistributeStatus = llmpool.HeadStatusDistributing
	tenant.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", tenant); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()

	out := httptest.NewRecorder()
	GetLLMClassHeadHandler(sys)(out, httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-head?group_id=writing-auto", nil))
	if out.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	var view llmClassHeadResponse
	if err := json.Unmarshal(out.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.StoreKey != "llm_class_head_v1:writing-auto" || view.Version != 0 {
		t.Fatalf("empty group must not inherit tenant head: %#v", view)
	}
	if rt := classHeadRuntime(sys, "u1", "writing-auto"); rt != nil {
		t.Fatalf("runtime must not inherit tenant head: %#v", rt)
	}

	llmClassHeadMu.Lock()
	writing := loadLLMClassHeadExactLocked(t.Context(), sys, "writing-auto")
	gotTenant := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if classHeadStoreHasLocalData(writing) {
		t.Fatalf("GET copied tenant store into writing-auto: %#v", writing)
	}
	if gotTenant.DistributeAck["node-b"] != "pending" {
		t.Fatalf("writing-auto must not ACK the tenant store: %#v", gotTenant.DistributeAck)
	}
}

func TestApplyReplicaDoesNotCopyTenantSamples(t *testing.T) {
	bindClassHeadRoster("node-b", []string{"node-a"}, nil, "")
	t.Cleanup(func() { bindClassHeadRoster("local", nil, nil, "") })
	sys := &stubSystemSettings{data: map[string]string{}}
	llmClassHeadMu.Lock()
	tenant := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	legacy := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	tenant.Current = &legacy
	tenant.Samples = []llmClassHeadSample{{ID: "tenant-only", Preview: "do not copy"}}
	if err := saveLLMClassHeadLocked(t.Context(), sys, "", tenant); err != nil {
		llmClassHeadMu.Unlock()
		t.Fatal(err)
	}
	llmClassHeadMu.Unlock()

	next := llmpool.EmptyHead(4, llmpool.DefaultHeadTau)
	view, err := applyClassHeadReplicaArtifact(t.Context(), sys, classHeadReplicaArtifact{
		GroupID:  "writing-auto",
		Pipeline: llmpool.PipelineShadow,
		Current:  &next,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Version != 4 || view.StoreKey != "llm_class_head_v1:writing-auto" {
		t.Fatalf("view=%#v", view)
	}
	llmClassHeadMu.Lock()
	writing := loadLLMClassHeadExactLocked(t.Context(), sys, "writing-auto")
	gotTenant := loadLLMClassHeadExactLocked(t.Context(), sys, "")
	llmClassHeadMu.Unlock()
	if len(writing.Samples) != 0 {
		t.Fatalf("replica copied tenant samples: %#v", writing.Samples)
	}
	if len(gotTenant.Samples) != 1 || gotTenant.Samples[0].ID != "tenant-only" {
		t.Fatalf("tenant samples mutated: %#v", gotTenant.Samples)
	}
}
