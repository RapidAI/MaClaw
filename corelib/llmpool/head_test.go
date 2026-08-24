package llmpool

import (
	"net/http"
	"testing"
)

func oneHotEmbedding(class string) []float32 {
	emb := make([]float32, HeadDim)
	idx := classIndex(class)
	if idx >= 0 {
		emb[idx] = 1
	}
	return emb
}

func TestHeadPredictLowConfidenceIsBalanced(t *testing.T) {
	head := EmptyHead(1, 0.9)
	pred := head.Predict(make([]float32, HeadDim))
	if pred.Class != WorkloadFallbackBalanced {
		t.Fatalf("class=%s want balanced", pred.Class)
	}
}

func TestClassificationHeadCloneIsolatesWeights(t *testing.T) {
	head := EmptyHead(4, DefaultHeadTau)
	head.Weights[0][0] = 1.5
	cloned := head.Clone()
	if cloned == nil || cloned.Version != 4 || cloned.Weights[0][0] != 1.5 {
		t.Fatalf("clone = %#v", cloned)
	}
	head.Weights[0][0] = 9
	if cloned.Weights[0][0] != 1.5 {
		t.Fatalf("clone aliased weights: %v", cloned.Weights[0][0])
	}
}

func TestTrainClassificationHeadSeparatesOneHotClasses(t *testing.T) {
	var samples []LabeledEmbedding
	for _, class := range FrozenWorkloadClasses {
		samples = append(samples, LabeledEmbedding{Embedding: oneHotEmbedding(class), Label: class})
	}
	head, err := TrainClassificationHead(samples, 2, 0.3)
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range FrozenWorkloadClasses {
		pred := head.Predict(oneHotEmbedding(class))
		if pred.Class != class {
			t.Fatalf("class=%s pred=%s p=%.3f", class, pred.Class, pred.MaxP)
		}
	}
}

func TestApplyHeadPipelineModes(t *testing.T) {
	pred := HeadPrediction{Class: WorkloadClassPlan, MaxP: 0.91}
	class, source, used := ApplyHeadPipeline(PipelineOff, "u1", WorkloadClassCode, ClassSourceHeuristic, pred)
	if used || class != WorkloadClassCode || source != ClassSourceHeuristic {
		t.Fatalf("off used=%v class=%s source=%s", used, class, source)
	}
	class, source, used = ApplyHeadPipeline(PipelineShadow, "u1", WorkloadClassCode, ClassSourceHeuristic, pred)
	if used || class != WorkloadClassCode {
		t.Fatalf("shadow used=%v class=%s", used, class)
	}
	class, _, used = ApplyHeadPipeline(PipelineOn, "u1", WorkloadClassCode, ClassSourceHeuristic, pred)
	if !used || class != WorkloadClassPlan {
		t.Fatalf("on used=%v class=%s", used, class)
	}
	class, source, used = ApplyHeadPipeline(PipelineOn, "u1", WorkloadClassCode, ClassSourceHeuristic, HeadPrediction{Class: WorkloadFallbackBalanced, MaxP: 0.2})
	if used || class != WorkloadFallbackBalanced || source != ClassSourceFallback {
		t.Fatalf("on low-conf class=%s source=%s used=%v", class, source, used)
	}
	class, source, used = ApplyHeadPipeline(PipelineOn, "u1", WorkloadClassPlan, ClassSourceHint, pred)
	if used || class != WorkloadClassPlan || source != ClassSourceHint {
		t.Fatalf("hint must stay class=%s source=%s used=%v", class, source, used)
	}
	class, source, used = ApplyHeadPipeline(PipelineOn, "u1", WorkloadClassDocWrite, ClassSourceWorkflow, pred)
	if used || class != WorkloadClassDocWrite || source != ClassSourceWorkflow {
		t.Fatalf("workflow must stay class=%s source=%s used=%v", class, source, used)
	}
	class, source, used = ApplyHeadPipeline(PipelineOn, "u1", WorkloadClassChat, ClassSourceTaskType, pred)
	if used || class != WorkloadClassChat || source != ClassSourceTaskType {
		t.Fatalf("task_type must stay class=%s source=%s used=%v", class, source, used)
	}
}

func TestCanarySelectedIsStableAndSparse(t *testing.T) {
	if CanarySelected("") {
		t.Fatal("empty user must not enter canary")
	}
	hits := 0
	for i := 0; i < 200; i++ {
		if CanarySelected("user-" + string(rune('a'+i%26)) + string(rune('0'+i%10))) {
			hits++
		}
	}
	if hits == 0 || hits > 40 {
		t.Fatalf("canary hits=%d want a small share", hits)
	}
}

func TestEvaluatePromoteGatesAndOverride(t *testing.T) {
	recent := EvalWindow{Reviews: 200, Correct: 180, PlanGold: 20, PlanHit: 17, DesignGold: 20, DesignHit: 17, ByClass: map[string]int{}}
	for _, class := range FrozenWorkloadClasses {
		recent.ByClass[class] = 1
	}
	prev := recent
	gates := EvaluatePromoteGates(recent, prev, true)
	if !gates.CanPromote || gates.Passed != GateCount {
		t.Fatalf("want green gates %#v", gates)
	}
	if err := AllowPipelineChange(PipelineShadow, PipelineOn, true, true, gates, "", ""); err != nil {
		t.Fatalf("green promote: %v", err)
	}
	blocked := EvaluatePromoteGates(EvalWindow{}, EvalWindow{}, false)
	if err := AllowPipelineChange(PipelineShadow, PipelineOn, true, true, blocked, "", ""); !IsPromoteBlocked(err) {
		t.Fatalf("expected blocked, got %v", err)
	}
	if err := AllowPipelineChange(PipelineShadow, PipelineOn, true, true, blocked, PromoteOverride, "incident"); err != nil {
		t.Fatalf("override: %v", err)
	}
	if err := AllowPipelineChange(PipelineOff, PipelineShadow, false, true, blocked, "", ""); !IsServingRequired(err) {
		t.Fatalf("off to shadow without serving: %v", err)
	}
	if err := AllowPipelineChange(PipelineOff, PipelineShadow, true, true, blocked, "", ""); err != nil {
		t.Fatalf("off to shadow with serving: %v", err)
	}
	if err := AllowPipelineChange(PipelineOff, PipelineCanary, true, true, gates, "", ""); !IsPipelineHopBlocked(err) {
		t.Fatalf("off to canary must hop via shadow, got %v", err)
	}
	if err := AllowPipelineChange(PipelineOff, PipelineOn, true, true, gates, PromoteOverride, "lab"); !IsPipelineHopBlocked(err) {
		t.Fatalf("PROMOTE must not skip off to on, got %v", err)
	}
	if err := AllowPipelineChange(PipelineShadow, PipelineOn, false, true, gates, PromoteOverride, "lab"); !IsServingRequired(err) {
		t.Fatalf("canary/on without serving: %v", err)
	}
	if err := AllowPipelineChange(PipelineShadow, PipelineOn, true, false, gates, PromoteOverride, "lab"); !IsDistributeIncomplete(err) {
		t.Fatalf("PROMOTE must not skip unfinished distribute: %v", err)
	}
	if err := AllowPipelineChange(PipelineOn, PipelineCanary, true, false, blocked, "", ""); err != nil {
		t.Fatalf("downgrade must not wait for ACK: %v", err)
	}
	if !DistributeComplete(nil) || !DistributeComplete(map[string]string{"a": "acked"}) {
		t.Fatal("empty or all-acked must be complete")
	}
	if DistributeComplete(map[string]string{"a": "acked", "b": "pending"}) {
		t.Fatal("pending peer must not be complete")
	}
}

func TestClassifyAndRouteWithHeadOnUsesHeadClass(t *testing.T) {
	group := officialDynamicFixture()
	header := http.Header{}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"content": "hello"}}}
	dec := ClassifyAndRouteWithHead(header, body, group, "auto", &HeadRuntime{
		Mode:   PipelineOn,
		UserID: "u1",
		Predict: func(string) HeadPrediction {
			return HeadPrediction{Class: WorkloadClassPlan, MaxP: 0.93}
		},
	})
	if !dec.HeadUsed || dec.Class != WorkloadClassPlan || dec.Source != ClassSourceHead {
		t.Fatalf("dec=%#v", dec)
	}
	if dec.ResolvedModel != OfficialTierHigh {
		t.Fatalf("resolved=%s", dec.ResolvedModel)
	}
}

func TestClassifyAndRouteWithHeadOnSkipsProtectedSources(t *testing.T) {
	group := officialDynamicFixture()
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	called := false
	dec := ClassifyAndRouteWithHead(header, map[string]any{"model": "auto", "messages": []any{map[string]any{"content": "hello"}}}, group, "auto", &HeadRuntime{
		Mode: PipelineOn,
		Predict: func(string) HeadPrediction {
			called = true
			return HeadPrediction{Class: WorkloadClassCode, MaxP: 0.99}
		},
	})
	if called {
		t.Fatal("P0 must not embed")
	}
	if dec.HeadUsed || dec.Class != WorkloadClassPlan || dec.Source != ClassSourceHint {
		t.Fatalf("dec=%#v", dec)
	}
}

func TestIsGoldClassSource(t *testing.T) {
	if !IsGoldClassSource(ClassSourceHint) || !IsGoldClassSource(ClassSourceWorkflow) {
		t.Fatal("P0/P1 should be gold")
	}
	if IsGoldClassSource(ClassSourceHeuristic) || IsGoldClassSource(ClassSourceHead) {
		t.Fatal("P3 and head self-pred must not be gold")
	}
}
