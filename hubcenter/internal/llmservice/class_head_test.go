package llmservice

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func seedReadyOfficialHead(t *testing.T, svc *Service) {
	t.Helper()
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialClassHeadPipelineBlockedWithoutGates(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineOn, "", ""); !llmpool.IsPipelineHopBlocked(err) {
		t.Fatalf("off to on err = %v", err)
	}
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineShadow, "", ""); !llmpool.IsServingRequired(err) {
		t.Fatalf("off to shadow without serving err = %v", err)
	}
	seedReadyOfficialHead(t, svc)
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineShadow, "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineOn, "", "")
	if !llmpool.IsPromoteBlocked(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestOfficialClassHeadPipelineOverride(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineCanary, llmpool.PromoteOverride, "lab"); !llmpool.IsPipelineHopBlocked(err) {
		t.Fatalf("off to canary err=%v", err)
	}
	seedReadyOfficialHead(t, svc)
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineShadow, "", ""); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineCanary, llmpool.PromoteOverride, "lab")
	if err != nil {
		t.Fatal(err)
	}
	if view.Pipeline != llmpool.PipelineCanary {
		t.Fatalf("pipeline = %q", view.Pipeline)
	}
}

func TestTrainOfficialClassHeadNowUsesStoredEmbeddings(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	for i, class := range llmpool.FrozenWorkloadClasses {
		vec := make([]float32, llmpool.HeadDim)
		vec[i] = 1
		data.Samples = append(data.Samples, OfficialClassHeadSample{
			ID:        class,
			At:        time.Now().UTC(),
			Preview:   class,
			Embedding: vec,
			Gold:      true,
			GoldClass: class,
		})
	}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	if err := svc.TrainOfficialClassHeadNow(); err != nil {
		t.Fatal(err)
	}
	view := svc.OfficialClassHeadView()
	if !view.ArtifactReady {
		t.Fatal("artifact not ready")
	}
	if view.Pipeline != llmpool.PipelineOff || view.Status != llmpool.HeadStatusUnused {
		t.Fatalf("train must not auto-enter shadow: %#v", view)
	}
}

func TestOfficialClassHeadStatusDoesNotStickTraining(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Status = llmpool.HeadStatusTraining
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	view := svc.OfficialClassHeadView()
	if view.Status == llmpool.HeadStatusTraining {
		t.Fatalf("stale training bit must not keep the job badge after the worker finished: %#v", view)
	}
	if view.Pipeline != llmpool.PipelineOff || view.Status != llmpool.HeadStatusUnused {
		t.Fatalf("trained head still on rules should be unused, got %#v", view)
	}
}

func TestOfficialClassHeadViewShowsInFlightTraining(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialHeadTrainQueued.Lock()
	if officialHeadTrainPending == nil {
		officialHeadTrainPending = map[string]bool{}
	}
	officialHeadTrainPending[""] = true
	officialHeadTrainQueued.Unlock()
	t.Cleanup(func() {
		officialHeadTrainQueued.Lock()
		delete(officialHeadTrainPending, "")
		officialHeadTrainQueued.Unlock()
	})
	view := svc.OfficialClassHeadView()
	if view.Status != llmpool.HeadStatusTraining {
		t.Fatalf("in-flight train must show training, got %#v", view)
	}
}

func TestRecordOfficialClassHeadSamplePerGroup(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.RecordOfficialClassHeadSample("design a system", llmpool.WorkloadClassDesign, llmpool.ClassSourceHint, "", 0, "coding-auto", false)
	svc.RecordOfficialClassHeadSample("design a system", llmpool.WorkloadClassDesign, llmpool.ClassSourceHint, "", 0, llmpool.OfficialGroupID, false)
	officialClassHeadMu.Lock()
	official := svc.loadOfficialClassHeadLocked(t.Context(), "")
	coding := svc.loadOfficialClassHeadLocked(t.Context(), "coding-auto")
	officialClassHeadMu.Unlock()
	if len(coding.Samples) != 0 {
		t.Fatalf("per-group store must stay empty, got %#v", coding.Samples)
	}
	seen := map[string]bool{}
	for _, sample := range official.Samples {
		if sample.Preview == "" {
			t.Fatalf("official samples = %#v", official.Samples)
		}
		seen[sample.GroupID] = true
	}
	if len(official.Samples) != 1 || official.Samples[0].GroupID != llmpool.OfficialGroupID || official.Samples[0].ID != officialHeadPreviewID("design a system") {
		t.Fatalf("same preview must upsert one row, got %#v", official.Samples)
	}
	if seen["coding-auto"] {
		t.Fatalf("later group should replace last_seen group, got %#v", official.Samples)
	}
}

func TestRecordOfficialClassHeadSampleKeepsHumanGold(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.RecordOfficialClassHeadSample("write a launch plan", llmpool.WorkloadClassChat, llmpool.ClassSourceHeuristic, "", 0, llmpool.OfficialGroupID, false)
	id := officialHeadPreviewID("write a launch plan")
	if _, err := svc.ReviewOfficialClassHead(id, llmpool.WorkloadClassPlan); err != nil {
		t.Fatal(err)
	}
	svc.RecordOfficialClassHeadSample("write a launch plan", llmpool.WorkloadClassChat, llmpool.ClassSourceHeuristic, llmpool.WorkloadClassChat, 0.4, "coding-auto", false)
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	officialClassHeadMu.Unlock()
	if len(data.Samples) != 1 || data.Samples[0].ID != id || data.Samples[0].GoldClass != llmpool.WorkloadClassPlan || !data.Samples[0].Gold {
		t.Fatalf("human gold must survive upsert, got %#v", data.Samples)
	}
	if data.Samples[0].GroupID != "coding-auto" || data.Samples[0].HeadClass != llmpool.WorkloadClassChat {
		t.Fatalf("last_seen fields should update, got %#v", data.Samples[0])
	}
}

func TestHeadRuntimeForGroupUsesOfficialStore(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(3, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	runtime := svc.HeadRuntimeForGroup("coding-auto", "user-1")
	if runtime == nil || runtime.Head == nil || runtime.Head.Version != 3 {
		t.Fatalf("L1 must use the global official head, got %#v", runtime)
	}
}

func TestPublishedOfficialHeadOnlyWhenPromoted(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if svc.PublishedOfficialHead().Published {
		t.Fatal("empty head should not publish")
	}
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	if !svc.PublishedOfficialHead().Published {
		t.Fatal("promoted head should publish")
	}
}

func TestOfficialClassHeadSaveKeepsPreview(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	data.Samples = []OfficialClassHeadSample{{
		ID: "s1", Preview: "write a launch plan", Gold: true, GoldClass: llmpool.WorkloadClassPlan,
	}}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	reloaded := svc.loadOfficialClassHeadLocked(t.Context(), "")
	officialClassHeadMu.Unlock()
	if len(reloaded.Samples) != 1 || reloaded.Samples[0].Preview != "write a launch plan" {
		t.Fatalf("preview dropped: %#v", reloaded.Samples)
	}
}

func TestDistributeOfficialClassHeadAcksLocal(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	view, err := svc.DistributeOfficialClassHead("")
	if err != nil {
		t.Fatal(err)
	}
	if view.DistributeStatus != "local" || view.DistributeAck["local"] != "acked" {
		t.Fatalf("view = %#v", view)
	}
}

func TestEnqueueOfficialClassHeadTrainRefusesNonTrainer(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", []string{"node-b"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	data.TrainerNodeID = "node-b"
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	if err := svc.EnqueueOfficialClassHeadTrain(); err == nil {
		t.Fatal("expected non-trainer to be refused")
	}
}

func TestPromoteOfficialClassHeadSeedsPeerPending(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", []string{"node-b"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(3, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineShadow, "", ""); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineOn, llmpool.PromoteOverride, "lab")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != llmpool.HeadStatusDistributing {
		t.Fatalf("status = %q", view.Status)
	}
	if view.DistributeAck["node-a"] != "acked" || view.DistributeAck["node-b"] != "pending" {
		t.Fatalf("ack = %#v", view.DistributeAck)
	}
	view, err = svc.DistributeOfficialClassHead("node-b")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status after ack = %q", view.Status)
	}
	if view.DistributeAck["node-b"] != "acked" {
		t.Fatalf("ack after complete = %#v", view.DistributeAck)
	}
}

func TestOfficialHeadRuntimeUsesPreviousWhenLocalPending(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-b", []string{"node-a"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	rt := svc.OfficialHeadRuntime("u1")
	if rt == nil || rt.Head == nil || rt.Head.Version != 1 {
		t.Fatalf("want previous v1, got %#v", rt)
	}
	pub := svc.PublishedOfficialHead()
	if !pub.Published || pub.Head == nil || pub.Head.Version != 1 {
		t.Fatalf("published = %#v", pub)
	}
}

func TestRollbackOfficialClassHeadSwapsPrevious(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", nil)
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	view, err := svc.RollbackOfficialClassHead()
	if err != nil {
		t.Fatal(err)
	}
	if view.Pipeline != llmpool.PipelineOn {
		t.Fatalf("pipeline = %q", view.Pipeline)
	}
	if view.Version != 1 || view.PreviousVersion != 2 {
		t.Fatalf("versions current=%d previous=%d", view.Version, view.PreviousVersion)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status = %q", view.Status)
	}
}

func TestRollbackOfficialClassHeadBlockedWhileDistributing(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", []string{"node-b"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	prev := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	curr := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Previous = &prev
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	data.DistributeStatus = llmpool.HeadStatusDistributing
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	view, err := svc.RollbackOfficialClassHead()
	if !llmpool.IsDistributeIncomplete(err) {
		t.Fatalf("err = %v", err)
	}
	if view.Version != 2 || view.PreviousVersion != 1 || view.Pipeline != llmpool.PipelineOn {
		t.Fatalf("pending rollback mutated %#v", view)
	}
}

func TestOfficialClassHeadPipelineBlockedWhileDistributing(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", []string{"node-b"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineShadow
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	data.DistributeStatus = llmpool.HeadStatusDistributing
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	if _, err := svc.SetOfficialClassHeadPipeline(llmpool.PipelineOn, llmpool.PromoteOverride, "lab"); !llmpool.IsDistributeIncomplete(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestAckOfficialClassHeadAfterRemoteApplyAcksPending(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-b", []string{"node-a"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.Status = llmpool.HeadStatusDistributing
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	svc.AckOfficialClassHeadAfterRemoteApply()
	view := svc.OfficialClassHeadView()
	if view.DistributeAck["node-b"] != "acked" {
		t.Fatalf("ack = %#v", view.DistributeAck)
	}
	if view.Status != llmpool.HeadStatusPromoted {
		t.Fatalf("status = %q", view.Status)
	}
}

func TestAckOfficialClassHeadAfterRemoteApplyNoopWhenAlreadyAcked(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-a", []string{"node-b"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	svc.AckOfficialClassHeadAfterRemoteApply()
	view := svc.OfficialClassHeadView()
	if view.DistributeAck["node-b"] != "pending" || view.Status != llmpool.HeadStatusDistributing {
		t.Fatalf("view = %#v", view)
	}
}

func TestAckOfficialClassHeadAfterRemoteApplyIgnoresSampleOnly(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-b", []string{"node-a"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &head
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	svc.AckOfficialClassHeadAfterRemoteApply()
	view := svc.OfficialClassHeadView()
	if len(view.DistributeAck) != 0 {
		t.Fatalf("sample-only apply should not invent ACK: %#v", view.DistributeAck)
	}
}

func TestAckOfficialClassHeadAfterRemoteApplyIgnoresPerGroupKey(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	svc.SetOfficialHeadRoster("node-b", []string{"node-a"})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(2, llmpool.DefaultHeadTau)
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.Status = llmpool.HeadStatusDistributing
	data.DistributeStatus = llmpool.HeadStatusDistributing
	data.DistributeAck = map[string]string{"node-a": "acked", "node-b": "pending"}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	svc.AckOfficialClassHeadAfterRemoteApplyKey("llm_class_head_v1:coding-auto")
	view := svc.OfficialClassHeadView()
	if view.DistributeAck["node-b"] != "pending" {
		t.Fatalf("per-group key must not ack official head: %#v", view.DistributeAck)
	}
}

func TestSeedGroupFromPublishedOfficialIsolatesGroups(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	if _, err := svc.SeedGroupFromPublishedOfficial(""); err == nil {
		t.Fatal("empty group_id should fail")
	}
	if _, err := svc.SeedGroupFromPublishedOfficial(llmpool.OfficialGroupID); err == nil {
		t.Fatal("official group should not pull itself")
	}
	if _, err := svc.SeedGroupFromPublishedOfficial("coding-auto"); err == nil {
		t.Fatal("unpublished official should fail")
	}

	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	head := llmpool.EmptyHead(6, llmpool.DefaultHeadTau)
	head.Weights[0][2] = 0.31
	data.Current = &head
	data.Pipeline = llmpool.PipelineOn
	data.Samples = []OfficialClassHeadSample{{ID: "official-sample", GroupID: llmpool.OfficialGroupID}}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	writing := svc.loadOfficialClassHeadLocked(t.Context(), "writing-auto")
	writing.Samples = []OfficialClassHeadSample{{ID: "writing-sample", GroupID: "writing-auto"}}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "writing-auto", writing); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()

	view, err := svc.SeedGroupFromPublishedOfficial("coding-auto")
	if err != nil {
		t.Fatal(err)
	}
	if view.Version != 6 || view.Status != llmpool.HeadStatusUnused || view.Pipeline != llmpool.PipelineOff {
		t.Fatalf("coding view = %#v", view)
	}
	if _, err := svc.SeedGroupFromPublishedOfficial("coding-auto"); err == nil {
		t.Fatal("overwrite should be refused")
	}

	officialClassHeadMu.Lock()
	coding := svc.loadOfficialClassHeadLocked(t.Context(), "coding-auto")
	writing = svc.loadOfficialClassHeadLocked(t.Context(), "writing-auto")
	official := svc.loadOfficialClassHeadLocked(t.Context(), "")
	officialClassHeadMu.Unlock()
	if !officialHeadArtifactReady(coding) || coding.Current.Version != 6 || len(coding.Samples) != 0 {
		t.Fatalf("coding store = %#v", coding)
	}
	if officialHeadArtifactReady(writing) || len(writing.Samples) != 1 || writing.Samples[0].ID != "writing-sample" {
		t.Fatalf("writing leaked: %#v", writing)
	}
	if len(official.Samples) != 1 || official.Current == nil || official.Current.Version != 6 {
		t.Fatalf("official mutated: %#v", official)
	}
	head.Weights[0][2] = 9
	if coding.Current.Weights[0][2] != 0.31 {
		t.Fatalf("seeded head aliased weights: %v", coding.Current.Weights[0][2])
	}
}

func seedGoldEmbeddings(t *testing.T, svc *Service, groupID string) {
	t.Helper()
	officialClassHeadMu.Lock()
	defer officialClassHeadMu.Unlock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), groupID)
	data.Samples = nil
	for i, class := range llmpool.FrozenWorkloadClasses {
		vec := make([]float32, llmpool.HeadDim)
		vec[i] = 1
		data.Samples = append(data.Samples, OfficialClassHeadSample{
			ID:        class,
			At:        time.Now().UTC(),
			Preview:   class,
			Embedding: vec,
			Gold:      true,
			GoldClass: class,
		})
	}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), groupID, data); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialClassHeadKeepsVersionHistory(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	view := svc.OfficialClassHeadViewFor("coding-auto")
	if view.StoreKey != "llm_class_head_v1:coding-auto" {
		t.Fatalf("store_key=%q", view.StoreKey)
	}
	if view.Version != 3 || view.PreviousVersion != 2 {
		t.Fatalf("current=%d previous=%d", view.Version, view.PreviousVersion)
	}
	if len(view.Versions) < 3 {
		t.Fatalf("versions=%#v", view.Versions)
	}
	if view.Versions[0].Role != llmpool.HeadRoleCurrent || view.Versions[1].Role != llmpool.HeadRolePrevious || view.Versions[2].Role != llmpool.HeadRoleHistory {
		t.Fatalf("roles=%#v", view.Versions)
	}
}

func TestOfficialClassHeadRetrainAfterRollbackKeepsRetiredVersion(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RollbackOfficialClassHeadFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	view := svc.OfficialClassHeadViewFor("coding-auto")
	if view.Version != 3 || view.PreviousVersion != 1 {
		t.Fatalf("current=%d previous=%d versions=%#v", view.Version, view.PreviousVersion, view.Versions)
	}
	foundRetired := false
	for _, item := range view.Versions {
		if item.Role == llmpool.HeadRoleHistory && item.Version == 2 {
			foundRetired = true
		}
	}
	if !foundRetired {
		t.Fatalf("rolled-back v2 missing from history %#v", view.Versions)
	}
}

func TestRollbackOfficialClassHeadNoopsWithoutPrevious(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	curr := llmpool.EmptyHead(1, llmpool.DefaultHeadTau)
	data.Current = &curr
	data.Pipeline = llmpool.PipelineOn
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()
	view, err := svc.RollbackOfficialClassHead()
	if err != nil {
		t.Fatal(err)
	}
	if view.Pipeline != llmpool.PipelineOn || view.Version != 1 || view.PreviousVersion != 0 {
		t.Fatalf("noop rollback mutated %#v", view)
	}
}

func TestScoreOfficialClassHeadForUsesSelectedSlot(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialHeadEmbedderFn = func(string) ([]float32, error) {
		vec := make([]float32, llmpool.HeadDim)
		vec[0] = 1
		return vec, nil
	}
	t.Cleanup(func() { officialHeadEmbedderFn = nil })
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "write a launch plan"}}}
	got, err := svc.ScoreOfficialClassHeadFor(t.Context(), "", "previous", http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot != llmpool.HeadRolePrevious || got.Version != 1 {
		t.Fatalf("score=%#v", got)
	}
	if got.RuleClass == "" || got.HeadClass == "" {
		t.Fatalf("missing classes %#v", got)
	}
	if !got.EmbedderReady {
		t.Fatal("embedder should be ready with test hook")
	}
}

func TestResolveClassHeadScoreGroupPrefersNonOfficial(t *testing.T) {
	raw, err := json.Marshal(Registry{ServiceGroups: []llmpool.ServiceGroup{
		{ID: "legacy-static", Kind: "static", Routes: []llmpool.WorkloadRoute{{Class: "plan", Model: "static-plan"}}},
		{ID: llmpool.OfficialGroupID, Kind: "dynamic", Routes: []llmpool.WorkloadRoute{{Class: "plan", Model: "official-high"}}},
		{ID: "empty-dyn", Kind: "dynamic"},
		{ID: "coding-auto", Kind: "dynamic", Routes: []llmpool.WorkloadRoute{{Class: "plan", Model: "coding-plan"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(&mockSystemSettings{data: map[string]string{RegistrySettingKey: string(raw)}})
	got := svc.resolveClassHeadScoreGroup(t.Context(), "")
	if got == nil || got.ID != "coding-auto" {
		t.Fatalf("empty group id = %#v", got)
	}
	got = svc.resolveClassHeadScoreGroup(t.Context(), llmpool.OfficialGroupID)
	if got == nil || got.ID != llmpool.OfficialGroupID {
		t.Fatalf("explicit official = %#v", got)
	}
}

func TestScoreOfficialClassHeadForReportsRequestedGroup(t *testing.T) {
	raw, err := json.Marshal(Registry{ServiceGroups: []llmpool.ServiceGroup{
		{ID: "coding-auto", Kind: "dynamic", Routes: []llmpool.WorkloadRoute{{Class: "plan", Model: "coding-plan"}}},
		{ID: "writing-auto", Kind: "dynamic", Routes: []llmpool.WorkloadRoute{{Class: "plan", Model: "writing-plan"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(&mockSystemSettings{data: map[string]string{RegistrySettingKey: string(raw)}})
	officialHeadEmbedderFn = func(string) ([]float32, error) {
		vec := make([]float32, llmpool.HeadDim)
		vec[0] = 1
		return vec, nil
	}
	t.Cleanup(func() { officialHeadEmbedderFn = nil })
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "write a launch plan"}}}
	header := http.Header{}
	header.Set(llmpool.TaskTypeHeader, "plan")
	got, err := svc.ScoreOfficialClassHeadFor(t.Context(), "writing-auto", "current", header, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StoreKey != OfficialClassHeadKey || got.GroupID != "writing-auto" {
		t.Fatalf("score=%#v", got)
	}
	if got.ResolvedModel != "writing-plan" {
		t.Fatalf("resolved_model=%q", got.ResolvedModel)
	}
}

func TestScoreOfficialClassHeadForEmptyGroupUsesOfficialStore(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialHeadEmbedderFn = func(string) ([]float32, error) {
		vec := make([]float32, llmpool.HeadDim)
		vec[0] = 1
		return vec, nil
	}
	t.Cleanup(func() { officialHeadEmbedderFn = nil })
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "write a launch plan"}}}
	got, err := svc.ScoreOfficialClassHeadFor(t.Context(), "", "current", http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StoreKey != OfficialClassHeadKey {
		t.Fatalf("store=%q", got.StoreKey)
	}
	if got.RuleClass == "" || got.HeadClass == "" {
		t.Fatalf("missing classes %#v", got)
	}
}

func TestScoreOfficialClassHeadForIgnoresPerGroupStore(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialHeadEmbedderFn = func(string) ([]float32, error) {
		vec := make([]float32, llmpool.HeadDim)
		vec[0] = 1
		return vec, nil
	}
	t.Cleanup(func() { officialHeadEmbedderFn = nil })
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	seedGoldEmbeddings(t, svc, "coding-auto")
	if err := svc.TrainOfficialClassHeadNowFor("coding-auto"); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "write a launch plan"}}}
	got, err := svc.ScoreOfficialClassHeadFor(t.Context(), "coding-auto", "current", http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if got.StoreKey != OfficialClassHeadKey || got.Version != 2 {
		t.Fatalf("score must use official store, got %#v", got)
	}
}

func TestScoreOfficialClassHeadForRejectsEmptyText(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialHeadEmbedderFn = func(string) ([]float32, error) {
		return make([]float32, llmpool.HeadDim), nil
	}
	t.Cleanup(func() { officialHeadEmbedderFn = nil })
	seedGoldEmbeddings(t, svc, "")
	if err := svc.TrainOfficialClassHeadNowFor(""); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ScoreOfficialClassHeadFor(t.Context(), "", "current", http.Header{}, map[string]any{"model": "auto"})
	if err == nil || !strings.Contains(err.Error(), "enter text") {
		t.Fatalf("empty score err=%v", err)
	}
}

func TestOfficialClassHeadReviewReplaceUnlabelAndDelete(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	data.Samples = []OfficialClassHeadSample{{
		ID:        "s1",
		At:        time.Now().UTC(),
		Preview:   "write a launch plan",
		RuleClass: llmpool.WorkloadClassChat,
	}}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()

	view, err := svc.ReviewOfficialClassHead("s1", llmpool.WorkloadClassPlan)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Samples) != 1 || view.Samples[0].GoldClass != llmpool.WorkloadClassPlan || view.HumanReviews != 1 || view.Reviews != 1 {
		t.Fatalf("first label = %#v", view)
	}

	view, err = svc.ReviewOfficialClassHead("s1", llmpool.WorkloadClassCode)
	if err != nil {
		t.Fatal(err)
	}
	if view.Samples[0].GoldClass != llmpool.WorkloadClassCode || view.HumanReviews != 1 {
		t.Fatalf("relabel must replace the review, got %#v", view)
	}

	view, err = svc.ReviewOfficialClassHead("s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Samples[0].Gold || view.Samples[0].GoldClass != "" || view.HumanReviews != 0 {
		t.Fatalf("unlabel must drop gold and the review, got %#v", view)
	}

	view, err = svc.DeleteOfficialClassHeadSample("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Samples) != 0 {
		t.Fatalf("delete must drop the sample, got %#v", view.Samples)
	}
	if _, err := svc.DeleteOfficialClassHeadSample("s1"); err == nil {
		t.Fatal("second delete must fail")
	}
}

func TestOfficialClassHeadSamplePage(t *testing.T) {
	svc := NewService(&mockSystemSettings{data: map[string]string{}})
	officialClassHeadMu.Lock()
	data := svc.loadOfficialClassHeadLocked(t.Context(), "")
	for i := 0; i < 35; i++ {
		data.Samples = append(data.Samples, OfficialClassHeadSample{
			ID:      "s" + strconv.Itoa(i),
			Preview: "sample " + strconv.Itoa(i),
		})
	}
	if err := svc.saveOfficialClassHeadLocked(t.Context(), "", data); err != nil {
		officialClassHeadMu.Unlock()
		t.Fatal(err)
	}
	officialClassHeadMu.Unlock()

	first := svc.OfficialClassHeadViewPage("", 1)
	if first.SampleTotal != 35 || first.SampleLimit != 30 || first.SamplePage != 1 || first.SamplePages != 2 || first.GroupID != llmpool.OfficialGroupID || len(first.Samples) != 30 || first.Samples[0].ID != "s0" {
		t.Fatalf("page 1 = %#v", first)
	}
	second := svc.OfficialClassHeadViewPage("", 2)
	if second.SamplePage != 2 || second.SamplePages != 2 || len(second.Samples) != 5 || second.Samples[0].ID != "s30" {
		t.Fatalf("page 2 = %#v", second)
	}
	clamped := svc.OfficialClassHeadViewPage("", 99)
	if clamped.SamplePage != 2 || len(clamped.Samples) != 5 {
		t.Fatalf("overflow page must clamp, got %#v", clamped)
	}
}
