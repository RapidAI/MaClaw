package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FeedbackLoop handles test failure classification and repair strategy
// decisions. It tracks round counts and enforces the maximum round limit.
type FeedbackLoop struct {
	llmConfig MaclawLLMConfig
	maxRounds int
	round     int
	history   []SwarmRound
}

// NewFeedbackLoop creates a FeedbackLoop with the given configuration.
func NewFeedbackLoop(cfg MaclawLLMConfig, maxRounds int) *FeedbackLoop {
	if maxRounds <= 0 {
		maxRounds = 5
	}
	return &FeedbackLoop{
		llmConfig: cfg,
		maxRounds: maxRounds,
	}
}

// ClassifyFailures uses the configured LLM to classify each test failure
// into Bug, FeatureGap, or RequirementDeviation.
func (f *FeedbackLoop) ClassifyFailures(failures []TestFailure) ([]ClassifiedFailure, error) {
	if len(failures) == 0 {
		return nil, nil
	}

	prompt := buildClassificationPrompt(failures)
	body, err := f.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM classification: %w", err)
	}

	var raw []struct {
		TestName string `json:"test_name"`
		Type     string `json:"type"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(extractJSONArray(body), &raw); err != nil {
		return nil, fmt.Errorf("parse classification: %w", err)
	}

	// Build lookup for original failures
	failMap := make(map[string]TestFailure)
	for _, f := range failures {
		failMap[f.TestName] = f
	}

	result := make([]ClassifiedFailure, 0, len(raw))
	for _, r := range raw {
		tf, ok := failMap[r.TestName]
		if !ok {
			tf = TestFailure{TestName: r.TestName}
		}
		ft := FailureType(r.Type)
		if ft != FailureTypeBug && ft != FailureTypeFeatureGap && ft != FailureTypeRequirementDeviation {
			ft = FailureTypeBug // default to bug if unrecognised
		}
		result = append(result, ClassifiedFailure{
			TestFailure: tf,
			Type:        ft,
			Reason:      r.Reason,
		})
	}
	return result, nil
}

// ShouldContinue returns true if the feedback loop has not yet reached the
// maximum number of rounds.
func (f *FeedbackLoop) ShouldContinue() bool {
	return f.round < f.maxRounds
}

// NextRound increments the round counter and records the reason.
func (f *FeedbackLoop) NextRound(reason string) {
	f.round++
	f.history = append(f.history, SwarmRound{
		Number:    f.round,
		Reason:    reason,
		StartedAt: time.Now(),
	})
}

// Round returns the current round number.
func (f *FeedbackLoop) Round() int { return f.round }

// MaxRounds returns the configured maximum.
func (f *FeedbackLoop) MaxRounds() int { return f.maxRounds }

// History returns the recorded round history.
func (f *FeedbackLoop) History() []SwarmRound { return f.history }

// DetermineStrategy returns the repair strategy for a classified failure.
func DetermineStrategy(cf ClassifiedFailure) string {
	switch cf.Type {
	case FailureTypeBug:
		return "maintenance_round"
	case FailureTypeFeatureGap:
		return "mini_greenfield"
	case FailureTypeRequirementDeviation:
		return "pause_for_user"
	default:
		return "maintenance_round"
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func buildClassificationPrompt(failures []TestFailure) string {
	var sb strings.Builder
	sb.WriteString(`Classify each test failure into one of three types:
- "bug": code defect that can be fixed
- "feature_gap": missing functionality that needs new development
- "requirement_deviation": the implementation doesn't match requirements (needs user clarification)

Test failures:
`)
	for _, f := range failures {
		fmt.Fprintf(&sb, "- %s: %s\n", f.TestName, f.ErrorOutput)
	}
	sb.WriteString("\nRespond ONLY with a JSON array of objects with fields: test_name, type, reason.")
	return sb.String()
}

func (f *FeedbackLoop) callLLM(prompt string) ([]byte, error) {
	return swarmCallLLM(f.llmConfig, prompt, 0.1, 60*time.Second)
}

func extractJSONArray(data []byte) []byte {
	return extractJSON(data)
}
