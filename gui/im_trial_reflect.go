package main

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type trialReflectState struct {
	enabled            bool
	pendingNote        string
	lastObservation    string
	failedActionCounts map[string]int
}

type trialReflectOutcome int

const (
	trialReflectOutcomeNone trialReflectOutcome = iota
	trialReflectOutcomeSucceeded
	trialReflectOutcomeFailed
	trialReflectOutcomeUncertain
)

type toolUsageFollowUp int

const (
	toolUsageFollowUpContinue toolUsageFollowUp = iota
	toolUsageFollowUpRetry
	toolUsageFollowUpAbandon
)

type trialReflectObservation struct {
	Outcome          trialReflectOutcome
	Text             string
	ToolOutcomes     []TraceToolObservation
	RepeatedFailures []string
}

func (o trialReflectOutcome) String() string {
	switch o {
	case trialReflectOutcomeSucceeded:
		return "succeeded"
	case trialReflectOutcomeFailed:
		return "failed"
	case trialReflectOutcomeUncertain:
		return "uncertain"
	default:
		return ""
	}
}

func (f toolUsageFollowUp) String() string {
	switch f {
	case toolUsageFollowUpRetry:
		return "retry"
	case toolUsageFollowUpAbandon:
		return "abandon"
	default:
		return "continue"
	}
}

func newTrialReflectState(enabled bool) *trialReflectState {
	return &trialReflectState{
		enabled:            enabled,
		failedActionCounts: make(map[string]int),
	}
}

func trialActionSignature(name, args string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(args)))
	return fmt.Sprintf("%s#%x", strings.TrimSpace(name), hash[:4])
}

func buildTrialReflectNote(toolNames []string, observations []string, repeatedFailures []string) string {
	if len(toolNames) == 0 || len(observations) == 0 {
		return ""
	}
	toolSummary := strings.Join(toolNames, ", ")
	observation := strings.Join(observations, "; ")
	var b strings.Builder
	b.WriteString("[Trial reflection]\n")
	b.WriteString("Previous attempt: ")
	b.WriteString(toolSummary)
	b.WriteString("\n")
	b.WriteString("Observation: ")
	b.WriteString(observation)
	b.WriteString("\n")
	b.WriteString("Next round: adjust the approach based on these results before continuing; do not repeat the same failed attempt.")
	if len(repeatedFailures) > 0 {
		sort.Strings(repeatedFailures)
		b.WriteString("\nAvoid repeating: ")
		b.WriteString(strings.Join(repeatedFailures, ", "))
	}
	return b.String()
}

func (s *trialReflectState) observeIteration(toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) trialReflectObservation {
	if s == nil || !s.enabled || len(toolCalls) == 0 || len(toolCalls) != len(toolResults) || len(toolCalls) != len(toolOutcomes) {
		return trialReflectObservation{}
	}
	toolNames := make([]string, 0, len(toolCalls))
	observations := make([]string, 0, len(toolCalls))
	traceOutcomes := make([]TraceToolObservation, 0, len(toolCalls))
	repeatedFailures := make([]string, 0)
	overall := trialReflectOutcomeSucceeded
	for i, tc := range toolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		toolNames = append(toolNames, name)
		outcome := toolOutcomes[i]
		summary := truncateTraceText(strings.TrimSpace(toolResults[i]), 120)
		if summary == "" {
			summary = "no clear output"
		}
		observations = append(observations, fmt.Sprintf("%s=%s (%s)", name, outcome.String(), summary))
		traceOutcomes = append(traceOutcomes, TraceToolObservation{
			ToolName: name,
			Outcome:  outcome.String(),
		})
		sig := trialActionSignature(name, tc.Function.Arguments)
		switch outcome {
		case toolOutcomeFailed:
			s.failedActionCounts[sig]++
			if s.failedActionCounts[sig] >= 1 {
				repeatedFailures = append(repeatedFailures, name)
			}
			overall = trialReflectOutcomeFailed
		case toolOutcomeUncertain:
			if overall == trialReflectOutcomeSucceeded {
				overall = trialReflectOutcomeUncertain
			}
		default:
			delete(s.failedActionCounts, sig)
		}
	}
	note := buildTrialReflectNote(toolNames, observations, repeatedFailures)
	s.pendingNote = note
	s.lastObservation = strings.Join(observations, "; ")
	return trialReflectObservation{
		Outcome:          overall,
		Text:             s.lastObservation,
		ToolOutcomes:     traceOutcomes,
		RepeatedFailures: repeatedFailures,
	}
}

func (h *IMMessageHandler) observeAgentLoopTrialIteration(ctx *LoopContext, trialState *trialReflectState, phase *agentLoopPhase, userText string, toolCalls []llm.ToolCall, toolResults []string, toolOutcomes []toolOutcome) {
	if trialState == nil || !trialState.enabled {
		return
	}
	trialObservation := trialState.observeIteration(toolCalls, toolResults, toolOutcomes)

	if trialObservation.Outcome == trialReflectOutcomeFailed && phase != nil && phase.Stage != agentStageRecover {
		enterRecoverPhase(phase, agentRecoverTrialFailed, buildTrialFailureRecoverPrompt(trialObservation.Text, trialObservation.RepeatedFailures))
	}
	if phase != nil && phase.Stage != agentStageRecover {
		phase.Stage = agentStageConverge
	}
	if trialObservation.Text != "" && h.traceService != nil && ctx.RunID != "" {
		h.appendTraceEventWithToolOutcomes(ctx, TraceEvent{
			Kind:         traceEventKindTrialObserved.String(),
			Severity:     "info",
			Title:        "Trial outcome",
			Summary:      truncateTraceText(trialObservation.Text, 220),
			ToolOutcomes: trialObservation.ToolOutcomes,
		})
		h.appendTraceEvidence(ctx, traceSourceKindTrialReflect.String(), trialObservation.Outcome.String(), "trial observation", truncateTraceText(trialObservation.Text, 400), "", "")
	}
	if strings.TrimSpace(trialState.pendingNote) == "" || h.traceService == nil || ctx.RunID == "" {
		return
	}
	severity := "info"
	if trialObservation.Outcome == trialReflectOutcomeFailed {
		severity = "warn"
	}
	h.appendTraceEvent(ctx, "trial.reflected", severity, "Trial reflection", truncateTraceText(trialState.pendingNote, 220), "", "")
	if len(trialObservation.RepeatedFailures) > 0 {
		h.appendTraceEvidence(ctx, traceSourceKindTrialReflect.String(), string(traceEvidenceCategoryRepeatGuard), "avoid repeating failed actions", strings.Join(trialObservation.RepeatedFailures, ", "), "", "")
	}
}

func (h *IMMessageHandler) recordAgentLoopToolUsage(ctx *LoopContext, userText string, toolCall llm.ToolCall, outcome toolOutcome, followUp toolUsageFollowUp) {
	if h.usageTracker == nil {
		return
	}
	var msgTokens []string
	if userText != "" {
		msgTokens = bm25.Tokenize(userText)
		if len(msgTokens) > 5 {
			msgTokens = msgTokens[:5]
		}
	}
	name := strings.TrimSpace(toolCall.Function.Name)
	h.usageTracker.RecordExperience(coretool.ToolExperience{
		ToolName:     name,
		QueryTokens:  msgTokens,
		Success:      outcome == toolOutcomeSucceeded,
		FollowUp:     followUp.String(),
		FinalOutcome: outcome.String(),
		EventContext: experienceContextFromLoop(ctx),
	})
}
