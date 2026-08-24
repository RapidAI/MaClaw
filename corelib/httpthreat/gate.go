package httpthreat

import (
	"strings"
	"time"
)

type EvalWindow struct {
	Reviews  int            `json:"reviews"`
	ByClass  map[string]int `json:"by_class,omitempty"`
	Correct  int            `json:"correct"`
	RiskGold map[string]int `json:"risk_gold,omitempty"`
	RiskHit  map[string]int `json:"risk_hit,omitempty"`
	Agree    int            `json:"rule_agree"`
	Compared int            `json:"rule_compared"`
}

func (w EvalWindow) Accuracy() float64 {
	if w.Reviews <= 0 {
		return 0
	}
	return float64(w.Correct) / float64(w.Reviews)
}

func (w EvalWindow) RiskRecall(class string) float64 {
	if w.RiskGold[class] <= 0 {
		return 0
	}
	return float64(w.RiskHit[class]) / float64(w.RiskGold[class])
}

func (w EvalWindow) CoversTrainable() bool {
	if w.ByClass == nil {
		return false
	}
	for _, class := range TrainableClasses {
		if w.ByClass[class] <= 0 {
			return false
		}
	}
	return true
}

type GateReport struct {
	ReviewCoverage bool       `json:"review_coverage"`
	Accuracy       bool       `json:"accuracy"`
	Recall         bool       `json:"recall"`
	TwoWindows     bool       `json:"two_windows"`
	Artifact       bool       `json:"artifact"`
	CanPromote     bool       `json:"can_promote"`
	Ready          bool       `json:"ready"`
	Recent         EvalWindow `json:"recent"`
	Prior          EvalWindow `json:"prior"`
	Agreement      float64    `json:"agreement"`
}

func Evaluate(samples []Sample, trainedAt string, predict func(Sample) string, artifactReady, encoderReady bool, excludeIDs []string) GateReport {
	if !encoderReady {
		return GateReport{Ready: false, Artifact: artifactReady}
	}
	skip := map[string]struct{}{}
	for _, id := range excludeIDs {
		if id != "" {
			skip[id] = struct{}{}
		}
	}
	trained, _ := time.Parse(time.RFC3339, strings.TrimSpace(trainedAt))
	now := time.Now().UTC()
	recentFrom := now.Add(-7 * 24 * time.Hour)
	priorFrom := now.Add(-14 * 24 * time.Hour)
	var coverRows, accRecent, accPrior []Sample
	for _, s := range samples {
		if _, hit := skip[s.ID]; hit {
			continue
		}
		if s.GoldSource != GoldHuman || !IsTrainableClass(s.GoldClass) {
			continue
		}
		labeled, _ := time.Parse(time.RFC3339, s.LabeledAt)
		if !trained.IsZero() && !labeled.After(trained) {
			continue
		}
		coverRows = append(coverRows, s)
		if !HeadMayScore(s.RuleSource) {
			continue
		}
		if !labeled.Before(recentFrom) {
			accRecent = append(accRecent, s)
		} else if !labeled.Before(priorFrom) {
			accPrior = append(accPrior, s)
		}
	}
	recent := evalRows(accRecent, predict)
	prior := evalRows(accPrior, predict)
	cover := evalRows(coverRows, predict)
	recallOK := true
	for _, class := range HighRiskClasses {
		if recent.RiskRecall(class) < GateMinRecall || prior.RiskRecall(class) < GateMinRecall {
			recallOK = false
		}
	}
	rep := GateReport{
		ReviewCoverage: cover.Reviews >= GateMinReviews && cover.CoversTrainable(),
		Accuracy:       recent.Accuracy() >= GateMinAccuracy,
		Recall:         recallOK && recent.Reviews > 0 && prior.Reviews > 0,
		TwoWindows:     recent.Accuracy() >= GateMinAccuracy && prior.Accuracy() >= GateMinAccuracy && recallOK && recent.Reviews > 0 && prior.Reviews > 0,
		Artifact:       artifactReady,
		Recent:         recent,
		Prior:          prior,
		Agreement:      recent.ruleAgreeRate(),
		Ready:          true,
	}
	rep.CanPromote = rep.ReviewCoverage && rep.Accuracy && rep.Recall && rep.TwoWindows && rep.Artifact
	return rep
}

func (w EvalWindow) ruleAgreeRate() float64 {
	if w.Compared <= 0 {
		return 0
	}
	return float64(w.Agree) / float64(w.Compared)
}

func evalRows(rows []Sample, predict func(Sample) string) EvalWindow {
	out := EvalWindow{ByClass: map[string]int{}, RiskGold: map[string]int{}, RiskHit: map[string]int{}}
	for _, s := range rows {
		gold := s.GoldClass
		out.Reviews++
		out.ByClass[gold]++
		pred := ""
		if predict != nil {
			pred = predict(s)
		}
		if pred == gold {
			out.Correct++
		}
		for _, class := range HighRiskClasses {
			if gold == class {
				out.RiskGold[class]++
				if pred == class {
					out.RiskHit[class]++
				}
			}
		}
		if s.RuleClass != "" {
			out.Compared++
			if s.RuleClass == pred {
				out.Agree++
			}
		}
	}
	return out
}
