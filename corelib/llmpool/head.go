package llmpool

import (
	"hash/fnv"
	"math"
	"net/http"
	"strings"
)

const (
	HeadDim         = 256
	DefaultHeadTau  = 0.55
	CanaryPercent   = 5
	PipelineOff     = "off"
	PipelineShadow  = "shadow"
	PipelineCanary  = "canary"
	PipelineOn      = "on"
	ClassSourceHead = "head"
	PromoteOverride = "PROMOTE"

	HeadStatusUnused       = "unused"
	HeadStatusTraining     = "training"
	HeadStatusShadow       = "shadow"
	HeadStatusCanary       = "canary"
	HeadStatusPromoted     = "promoted"
	HeadStatusGatesFailed  = "gates_failed"
	HeadStatusDistributing = "distributing"
	HeadStatusRolledBack   = "rolled_back"
)

// ClassificationHead is logits = W e + b with W shaped 8x256.
type ClassificationHead struct {
	Version   int         `json:"version"`
	Weights   [][]float64 `json:"weights"`
	Bias      []float64   `json:"bias"`
	Tau       float64     `json:"tau"`
	TrainedAt string      `json:"trained_at,omitempty"`
}

// HeadPrediction is one softmax result over FrozenWorkloadClasses.
type HeadPrediction struct {
	Class string             `json:"class"`
	MaxP  float64            `json:"max_p"`
	Probs map[string]float64 `json:"probs,omitempty"`
}

// HeadRuntime is optional V2 input for L1. Nil keeps V1 rules only.
type HeadRuntime struct {
	Mode    string
	UserID  string
	Head    *ClassificationHead
	Predict func(preview string) HeadPrediction
}

func NormalizePipelineMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PipelineShadow, PipelineCanary, PipelineOn:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return PipelineOff
	}
}

func IsGoldClassSource(source string) bool {
	switch strings.TrimSpace(source) {
	case ClassSourceHint, ClassSourceWorkflow:
		return true
	default:
		return false
	}
}

func (h *ClassificationHead) Ready() bool {
	if h == nil || len(h.Weights) != len(FrozenWorkloadClasses) || len(h.Bias) != len(FrozenWorkloadClasses) {
		return false
	}
	for i := range h.Weights {
		if len(h.Weights[i]) != HeadDim {
			return false
		}
	}
	return true
}

// Clone copies weights and bias so a pulled official head cannot alias another group's store.
func (h *ClassificationHead) Clone() *ClassificationHead {
	if h == nil {
		return nil
	}
	out := *h
	if h.Weights != nil {
		out.Weights = make([][]float64, len(h.Weights))
		for i, row := range h.Weights {
			if row == nil {
				continue
			}
			cp := make([]float64, len(row))
			copy(cp, row)
			out.Weights[i] = cp
		}
	}
	if h.Bias != nil {
		out.Bias = make([]float64, len(h.Bias))
		copy(out.Bias, h.Bias)
	}
	return &out
}

func (h *ClassificationHead) EffectiveTau() float64 {
	if h == nil || h.Tau <= 0 || h.Tau >= 1 {
		return DefaultHeadTau
	}
	return h.Tau
}

func (h *ClassificationHead) Predict(embedding []float32) HeadPrediction {
	out := HeadPrediction{Class: WorkloadFallbackBalanced, Probs: map[string]float64{}}
	if !h.Ready() || len(embedding) < HeadDim {
		return out
	}
	logits := make([]float64, len(FrozenWorkloadClasses))
	maxLogit := math.Inf(-1)
	for i := range FrozenWorkloadClasses {
		sum := h.Bias[i]
		row := h.Weights[i]
		for j := 0; j < HeadDim; j++ {
			sum += row[j] * float64(embedding[j])
		}
		logits[i] = sum
		if sum > maxLogit {
			maxLogit = sum
		}
	}
	var denom float64
	exps := make([]float64, len(logits))
	for i, logit := range logits {
		exps[i] = math.Exp(logit - maxLogit)
		denom += exps[i]
	}
	if denom == 0 {
		return out
	}
	best := 0
	for i, class := range FrozenWorkloadClasses {
		p := exps[i] / denom
		out.Probs[class] = p
		if p > out.Probs[FrozenWorkloadClasses[best]] {
			best = i
		}
	}
	out.Class = FrozenWorkloadClasses[best]
	out.MaxP = out.Probs[out.Class]
	if out.MaxP < h.EffectiveTau() {
		out.Class = WorkloadFallbackBalanced
	}
	return out
}

func CanarySelected(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	return h.Sum32()%100 < CanaryPercent
}

// HeadMayRewriteSource reports whether the live head may replace a rule class.
// Frozen design: only P3 heuristic and P4 fallback are rewriteable. P0/P1/P2 stay.
func HeadMayRewriteSource(source string) bool {
	switch strings.TrimSpace(source) {
	case ClassSourceHeuristic, ClassSourceFallback, "":
		return true
	default:
		return false
	}
}

func ApplyHeadPipeline(mode, userID, ruleClass, ruleSource string, pred HeadPrediction) (class, source string, used bool) {
	mode = NormalizePipelineMode(mode)
	ruleClass = strings.TrimSpace(ruleClass)
	if ruleClass == "" {
		ruleClass = WorkloadFallbackBalanced
	}
	if mode == PipelineOff || mode == PipelineShadow {
		return ruleClass, ruleSource, false
	}
	if !HeadMayRewriteSource(ruleSource) {
		return ruleClass, ruleSource, false
	}
	headClass := strings.TrimSpace(pred.Class)
	if headClass == "" || pred.MaxP <= 0 {
		if mode == PipelineOn {
			return WorkloadFallbackBalanced, ClassSourceFallback, false
		}
		return ruleClass, ruleSource, false
	}
	if mode == PipelineCanary && !CanarySelected(userID) {
		return ruleClass, ruleSource, false
	}
	if mode == PipelineOn && headClass == WorkloadFallbackBalanced {
		return WorkloadFallbackBalanced, ClassSourceFallback, false
	}
	if !IsWorkloadClass(headClass) && headClass != WorkloadFallbackBalanced {
		if mode == PipelineOn {
			return WorkloadFallbackBalanced, ClassSourceFallback, false
		}
		return ruleClass, ruleSource, false
	}
	return headClass, ClassSourceHead, true
}

func ClassifyAndRouteWithHead(header http.Header, body map[string]any, group *ServiceGroup, requestedModel string, runtime *HeadRuntime) WorkloadDecision {
	dec := ClassifyAndRouteModel(header, body, group, requestedModel)
	dec.RuleClass = dec.Class
	dec.RuleSource = dec.Source
	if runtime == nil || dec.Passthrough || NormalizePipelineMode(runtime.Mode) == PipelineOff {
		return finishDecision(dec, group, requestedModel)
	}
	if !HeadMayRewriteSource(dec.Source) {
		return finishDecision(dec, group, requestedModel)
	}
	pred := HeadPrediction{Class: WorkloadFallbackBalanced}
	if runtime.Predict != nil {
		pred = runtime.Predict(RequestTextPreview(body, 400))
	}
	dec.HeadClass = pred.Class
	dec.HeadMaxP = pred.MaxP
	class, source, used := ApplyHeadPipeline(runtime.Mode, runtime.UserID, dec.Class, dec.Source, pred)
	dec.HeadUsed = used
	if !used {
		return finishDecision(dec, group, requestedModel)
	}
	dec.Class = class
	dec.Source = source
	routedClass, model, quality := RouteWorkloadClass(group, class)
	dec.RoutedClass = routedClass
	dec.ResolvedModel = model
	dec.Quality = quality
	return finishDecision(dec, group, requestedModel)
}
