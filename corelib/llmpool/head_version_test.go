package llmpool

import (
	"net/http"
	"testing"
	"time"
)

func TestHeadSlotsVersionsAndArchive(t *testing.T) {
	serving := EmptyHead(3, DefaultHeadTau)
	prev := EmptyHead(2, DefaultHeadTau)
	candidate := EmptyHead(4, DefaultHeadTau)
	retired := EmptyHead(1, DefaultHeadTau)
	old := VersionInfoFromHead(HeadRoleHistory, HeadSourceTrain, &retired)
	hist := ArchiveRetiredHead(nil, old)
	slots := &HeadSlots{
		Serving: &serving, Previous: &prev, Candidate: &candidate,
		ServingSource: HeadSourceTrain, PreviousSource: HeadSourcePull, CandidateSource: HeadSourceTrain,
	}
	got := slots.Versions(hist)
	if len(got) != 4 {
		t.Fatalf("versions=%d %#v", len(got), got)
	}
	if got[0].Role != HeadRoleServing || got[0].Version != 3 {
		t.Fatalf("serving=%#v", got[0])
	}
	if got[1].Role != HeadRolePrevious || got[1].Version != 2 {
		t.Fatalf("previous=%#v", got[1])
	}
	if got[2].Role != HeadRoleCandidate || got[2].Version != 4 {
		t.Fatalf("candidate=%#v", got[2])
	}
	if got[3].Role != HeadRoleHistory || got[3].Version != 1 || got[3].RetiredAt == "" {
		t.Fatalf("history=%#v", got[3])
	}
}

func TestHeadSlotsResolveSlot(t *testing.T) {
	serving := EmptyHead(3, DefaultHeadTau)
	prev := EmptyHead(2, DefaultHeadTau)
	candidate := EmptyHead(4, DefaultHeadTau)
	slots := &HeadSlots{Serving: &serving, Previous: &prev, Candidate: &candidate}
	role, head, _, err := slots.ResolveSlot("previous")
	if err != nil || role != HeadRolePrevious || head.Version != 2 {
		t.Fatalf("previous slot role=%s ver=%v err=%v", role, head, err)
	}
	role, head, _, err = slots.ResolveSlot("candidate")
	if err != nil || role != HeadRoleCandidate || head.Version != 4 {
		t.Fatalf("candidate slot role=%s ver=%v err=%v", role, head, err)
	}
	role, head, _, err = slots.ResolveSlot("3")
	if err != nil || role != HeadRoleServing || head.Version != 3 {
		t.Fatalf("version slot role=%s ver=%v err=%v", role, head, err)
	}
	if _, _, _, err := slots.ResolveSlot("1"); err == nil {
		t.Fatal("retired version should not score")
	}
}

func TestHeadSlotsAdoptMovesRunsAndKeepsTrainedAt(t *testing.T) {
	serving := EmptyHead(2, DefaultHeadTau)
	serving.TrainedAt = "2025-01-01T00:00:00Z"
	candidate := EmptyHead(3, DefaultHeadTau)
	candidate.TrainedAt = "2025-02-01T00:00:00Z"
	servingRun := &TrainRun{Version: 2, TrainedAt: serving.TrainedAt, SampleIDs: []string{"a"}}
	candidateRun := &TrainRun{Version: 3, TrainedAt: candidate.TrainedAt, SampleIDs: []string{"b"}}
	slots := &HeadSlots{Serving: &serving, Candidate: &candidate, ServingRun: servingRun, CandidateRun: candidateRun}
	if !slots.Adopt() {
		t.Fatal("adopt must succeed with a ready candidate")
	}
	if slots.Serving == nil || slots.Serving.Version != 3 || slots.Serving.TrainedAt != "2025-02-01T00:00:00Z" {
		t.Fatalf("serving=%#v", slots.Serving)
	}
	if slots.Previous == nil || slots.Previous.Version != 2 || slots.PreviousRun != servingRun {
		t.Fatalf("previous=%#v run=%#v", slots.Previous, slots.PreviousRun)
	}
	if slots.Candidate != nil || slots.CandidateRun != nil || slots.CandidateSource != "" {
		t.Fatal("candidate slot must be cleared after adopt")
	}
	if slots.ServingRun != candidateRun {
		t.Fatal("candidate TrainRun must move with the weights")
	}
	if !slots.Rollback() {
		t.Fatal("rollback must succeed with a previous slot")
	}
	if slots.Serving == nil || slots.Serving.Version != 2 || slots.Previous.Version != 3 {
		t.Fatalf("after rollback serving=%#v previous=%#v", slots.Serving, slots.Previous)
	}
	if slots.Candidate != nil {
		t.Fatal("rollback must not touch candidate")
	}
}

func TestHeadSlotsAdoptWithoutCandidateFails(t *testing.T) {
	slots := &HeadSlots{}
	if slots.Adopt() {
		t.Fatal("adopt without candidate must fail")
	}
	if slots.Rollback() {
		t.Fatal("rollback without previous must fail")
	}
	serving := EmptyHead(1, DefaultHeadTau)
	slots.Serving = &serving
	if slots.Rollback() {
		t.Fatal("rollback with only serving must fail")
	}
}

func TestHeadSlotsInstallCandidateNeverTouchesServing(t *testing.T) {
	serving := EmptyHead(1, DefaultHeadTau)
	slots := &HeadSlots{Serving: &serving, ServingSource: HeadSourceTrain}
	first := EmptyHead(2, DefaultHeadTau)
	slots.InstallCandidate(&first, HeadSourceTrain, &TrainRun{Version: 2, SampleIDs: []string{"x"}})
	second := EmptyHead(3, DefaultHeadTau)
	slots.InstallCandidate(&second, HeadSourcePull, nil)
	if slots.Serving.Version != 1 {
		t.Fatal("installing candidate must not replace serving")
	}
	if slots.Previous != nil {
		t.Fatal("re-train must not push old candidate into previous")
	}
	if slots.Candidate == nil || slots.Candidate.Version != 3 || slots.CandidateSource != HeadSourcePull {
		t.Fatalf("candidate=%#v", slots.Candidate)
	}
}

func TestHeadSlotsNextVersion(t *testing.T) {
	serving := EmptyHead(2, DefaultHeadTau)
	prev := EmptyHead(3, DefaultHeadTau)
	candidate := EmptyHead(5, DefaultHeadTau)
	slots := &HeadSlots{Serving: &serving, Previous: &prev, Candidate: &candidate}
	if got := slots.NextVersion([]ClassHeadVersionInfo{{Version: 1}}); got != 6 {
		t.Fatalf("next=%d want 6", got)
	}
	empty := &HeadSlots{}
	if got := empty.NextVersion(nil); got != 1 {
		t.Fatalf("empty next=%d want 1", got)
	}
}

func TestScoreHeadAgainstRulesReportsRewrite(t *testing.T) {
	head := EmptyHead(1, 0.01)
	pred := HeadPrediction{Class: WorkloadClassPlan, MaxP: 0.9, Probs: map[string]float64{WorkloadClassPlan: 0.9}}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, http.Header{}, body, HeadRoleServing, &head, pred)
	if got.RuleClass == "" || got.HeadClass != WorkloadClassPlan {
		t.Fatalf("score=%#v", got)
	}
	if got.IfLiveClass != WorkloadClassPlan || !got.IfLiveUsed || !got.HeadEligible || !got.WouldRewrite {
		t.Fatalf("live=%#v", got)
	}
}

func TestScoreHeadAgainstRulesKeepsHint(t *testing.T) {
	head := EmptyHead(1, 0.01)
	pred := HeadPrediction{Class: WorkloadClassCode, MaxP: 0.95}
	header := http.Header{}
	header.Set(WorkloadClassHeader, "plan")
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, header, body, HeadRoleServing, &head, pred)
	if got.RuleSource != ClassSourceHint || got.HeadEligible || got.WouldRewrite || got.IfLiveClass != WorkloadClassPlan {
		t.Fatalf("hint score=%#v", got)
	}
}

func TestScoreHeadAgainstRulesLowConfCountsAsRewrite(t *testing.T) {
	head := EmptyHead(1, 0.9)
	pred := HeadPrediction{Class: WorkloadFallbackBalanced, MaxP: 0.2}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "func main() {}"}}}
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, http.Header{}, body, HeadRoleServing, &head, pred)
	if !got.HeadEligible {
		t.Fatalf("heuristic should be eligible %#v", got)
	}
	if got.IfLiveClass != WorkloadFallbackBalanced || got.IfLiveUsed {
		t.Fatalf("low-conf live=%#v", got)
	}
	if got.RuleClass != WorkloadFallbackBalanced && !got.WouldRewrite {
		t.Fatalf("low-conf rewrite hidden %#v", got)
	}
}

func TestScoreRequestPreviewRejectsEmpty(t *testing.T) {
	if _, err := ScoreRequestPreview(map[string]any{"model": "auto"}); err == nil {
		t.Fatal("expected empty preview error")
	}
}

func TestEvaluateHeadGateRescoresAndFilters(t *testing.T) {
	now := time.Now().UTC()
	head := EmptyHead(7, 0.01)
	head.TrainedAt = now.Add(-time.Hour).Format(time.RFC3339)
	run := &TrainRun{Version: 7, TrainedAt: head.TrainedAt, SampleIDs: []string{"skip-me"}}
	embed := func(preview string) ([]float32, error) { return oneHotEmbedding(WorkloadClassPlan), nil }
	rows := []GateEvalSample{
		// auto gold never counts
		{ID: "auto", Preview: "a", GoldClass: WorkloadClassPlan, GoldSource: GoldSourceAuto, RuleSource: ClassSourceHeuristic, LabeledAt: now},
		// human but labeled before trained_at
		{ID: "old", Preview: "b", GoldClass: WorkloadClassPlan, GoldSource: GoldSourceHuman, RuleSource: ClassSourceHeuristic, LabeledAt: now.Add(-2 * time.Hour)},
		// human but inside sample_ids
		{ID: "skip-me", Preview: "c", GoldClass: WorkloadClassPlan, GoldSource: GoldSourceHuman, RuleSource: ClassSourceHeuristic, LabeledAt: now},
		// human, no preview: skipped, not counted
		{ID: "no-preview", GoldClass: WorkloadClassPlan, GoldSource: GoldSourceHuman, RuleSource: ClassSourceHeuristic, LabeledAt: now},
		// qualifying P3 human row, predicted plan via one-hot embedding
		{ID: "ok-p3", Preview: "write a plan", GoldClass: WorkloadClassPlan, GoldSource: GoldSourceHuman, RuleSource: ClassSourceHeuristic, LabeledAt: now},
		// qualifying human P0 row counts for coverage but not accuracy
		{ID: "ok-p0", Preview: "d", GoldClass: WorkloadClassCode, GoldSource: GoldSourceHuman, RuleSource: ClassSourceHint, LabeledAt: now},
	}
	report := EvaluateHeadGate(&head, run, true, rows, embed, true, now)
	if !report.Ready {
		t.Fatalf("report must be ready: %#v", report)
	}
	if report.Coverage.Reviews != 2 {
		t.Fatalf("coverage reviews=%d want 2 (human, fresh, outside sample_ids)", report.Coverage.Reviews)
	}
	if report.Recent.Reviews != 1 || report.Recent.Correct != 1 {
		t.Fatalf("quality window=%#v", report.Recent)
	}
}

func TestEvaluateHeadGateNotReady(t *testing.T) {
	head := EmptyHead(1, DefaultHeadTau)
	if got := EvaluateHeadGate(&head, nil, false, nil, nil, false, time.Now()); got.Ready || got.CanPromote || got.Passed != 0 {
		t.Fatalf("unreadable corpus must stay red: %#v", got)
	}
	if got := EvaluateHeadGate(nil, nil, true, nil, nil, false, time.Now()); got.Ready || got.NotReadyReason == "" {
		t.Fatalf("missing artifact must stay red: %#v", got)
	}
	rows := []GateEvalSample{{
		ID: "x", Preview: "needs embed", GoldClass: WorkloadClassPlan,
		GoldSource: GoldSourceHuman, RuleSource: ClassSourceHeuristic, LabeledAt: time.Now(),
	}}
	if got := EvaluateHeadGate(&head, nil, true, rows, nil, true, time.Now()); got.Ready || got.CanPromote {
		t.Fatalf("missing embedder must fail the whole report: %#v", got)
	}
}
