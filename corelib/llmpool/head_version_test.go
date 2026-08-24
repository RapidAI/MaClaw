package llmpool

import (
	"net/http"
	"testing"
)

func TestCollectAndArchiveHeadVersions(t *testing.T) {
	curr := EmptyHead(3, DefaultHeadTau)
	prev := EmptyHead(2, DefaultHeadTau)
	retired := EmptyHead(1, DefaultHeadTau)
	old := VersionInfoFromHead(HeadRoleHistory, HeadSourceTrain, &retired)
	hist := ArchiveRetiredHead(nil, old)
	got := CollectHeadVersions(&curr, &prev, HeadSourceTrain, HeadSourcePull, hist)
	if len(got) != 3 {
		t.Fatalf("versions=%d %#v", len(got), got)
	}
	if got[0].Role != HeadRoleCurrent || got[0].Version != 3 {
		t.Fatalf("current=%#v", got[0])
	}
	if got[1].Role != HeadRolePrevious || got[1].Version != 2 {
		t.Fatalf("previous=%#v", got[1])
	}
	if got[2].Role != HeadRoleHistory || got[2].Version != 1 || got[2].RetiredAt == "" {
		t.Fatalf("history=%#v", got[2])
	}
}

func TestResolveHeadSlot(t *testing.T) {
	curr := EmptyHead(3, DefaultHeadTau)
	prev := EmptyHead(2, DefaultHeadTau)
	role, head, err := ResolveHeadSlot("previous", &curr, &prev)
	if err != nil || role != HeadRolePrevious || head.Version != 2 {
		t.Fatalf("previous slot role=%s ver=%v err=%v", role, head, err)
	}
	role, head, err = ResolveHeadSlot("3", &curr, &prev)
	if err != nil || role != HeadRoleCurrent || head.Version != 3 {
		t.Fatalf("version slot role=%s ver=%v err=%v", role, head, err)
	}
	if _, _, err := ResolveHeadSlot("1", &curr, &prev); err == nil {
		t.Fatal("retired version should not score")
	}
}

func TestScoreHeadAgainstRulesReportsRewrite(t *testing.T) {
	head := EmptyHead(1, 0.01)
	pred := HeadPrediction{Class: WorkloadClassPlan, MaxP: 0.9, Probs: map[string]float64{WorkloadClassPlan: 0.9}}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, http.Header{}, body, HeadRoleCurrent, &head, pred)
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
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, header, body, HeadRoleCurrent, &head, pred)
	if got.RuleSource != ClassSourceHint || got.HeadEligible || got.WouldRewrite || got.IfLiveClass != WorkloadClassPlan {
		t.Fatalf("hint score=%#v", got)
	}
}

func TestScoreHeadAgainstRulesLowConfCountsAsRewrite(t *testing.T) {
	head := EmptyHead(1, 0.9)
	pred := HeadPrediction{Class: WorkloadFallbackBalanced, MaxP: 0.2}
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "func main() {}"}}}
	got := ScoreHeadAgainstRules(&ServiceGroup{ID: "coding-auto", Kind: "dynamic"}, http.Header{}, body, HeadRoleCurrent, &head, pred)
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

func TestNextHeadVersionSkipsRolledBackNumber(t *testing.T) {
	curr := EmptyHead(2, DefaultHeadTau)
	prev := EmptyHead(3, DefaultHeadTau)
	hist := []ClassHeadVersionInfo{{Version: 1}}
	if got := NextHeadVersion(&curr, &prev, hist); got != 4 {
		t.Fatalf("after rollback next=%d want 4", got)
	}
	if got := NextHeadVersion(nil, nil, nil); got != 1 {
		t.Fatalf("empty next=%d want 1", got)
	}
}

func TestRotateClassificationHeadClonesWeights(t *testing.T) {
	curr := EmptyHead(2, DefaultHeadTau)
	curr.Weights[0][0] = 1.5
	var current, previous *ClassificationHead
	current = &curr
	currentSrc, previousSrc := HeadSourceTrain, ""
	var history []ClassHeadVersionInfo
	next := EmptyHead(3, DefaultHeadTau)
	RotateClassificationHead(&current, &previous, &currentSrc, &previousSrc, &history, &next, HeadSourceTrain)
	if previous == nil || previous.Version != 2 || previous.Weights[0][0] != 1.5 {
		t.Fatalf("previous=%#v", previous)
	}
	current.Weights[0][0] = 9
	if previous.Weights[0][0] != 1.5 {
		t.Fatal("rotate aliased weights")
	}
}
