package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// TrajectoryEntry represents a single turn in an LLM conversation trajectory.
type TrajectoryEntry struct {
	Timestamp    string      `json:"timestamp"`
	Role         string      `json:"role"`                    // "system", "user", "assistant", "tool", "tool_result"
	Content      interface{} `json:"content"`                 // text or multimodal content
	ToolCalls    interface{} `json:"tool_calls,omitempty"`    // assistant tool calls
	ToolCallID   string      `json:"tool_call_id,omitempty"`  // tool result correlation
	ToolName     string      `json:"tool_name,omitempty"`     // tool name when known
	ToolOutcome  string      `json:"tool_outcome,omitempty"`  // succeeded/failed/... when known
	Reasoning    string      `json:"reasoning,omitempty"`     // reasoning_content if present
	FinishReason string      `json:"finish_reason,omitempty"` // stop/tool_calls/... when known
	Iteration    int         `json:"iteration,omitempty"`     // 1-based loop iteration when known
}

// TrajectorySession holds all entries for a single agent loop session.
type TrajectorySession struct {
	SessionID       string        `json:"session_id"`
	ParentSessionID string        `json:"parent_session_id,omitempty"`
	Kind            string        `json:"kind,omitempty"` // main | shared | coding_subagent | btw_subagent
	StartTime       string        `json:"start_time"`
	EndTime         string        `json:"end_time,omitempty"`
	Provider        string        `json:"provider"`
	Model           string        `json:"model"`
	Protocol        string        `json:"protocol"`
	UserID          string        `json:"user_id"`
	Platform        string        `json:"platform"`
	Tools           []interface{} `json:"tools,omitempty"`
	// Outcome metadata (filled via SetOutcome before Flush when available).
	Status        string            `json:"status,omitempty"` // success | error | cancelled | hard_exit | paused
	Error         string            `json:"error,omitempty"`
	Iterations    int               `json:"iterations,omitempty"`
	ToolCallCount int               `json:"tool_call_count,omitempty"`
	InputTokens   int               `json:"input_tokens,omitempty"`
	OutputTokens  int               `json:"output_tokens,omitempty"`
	Entries       []TrajectoryEntry `json:"entries"`
}

// TrajectoryRecorder records LLM interaction trajectories to disk.
type TrajectoryRecorder struct {
	mu               sync.Mutex
	dir              string // ~/.maclaw/trajectories
	session          *TrajectorySession
	pipeline         *SkillAutoSummaryPipeline
	currentIteration int // 1-based loop iteration stamped onto new entries
}

// safeFilenameRe strips characters that are invalid in file names.
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

// NewTrajectoryRecorder creates a recorder that writes to ~/.maclaw/trajectories.
func NewTrajectoryRecorder() *TrajectoryRecorder {
	return NewTrajectoryRecorderForBaseDir(corelib.MaclawBaseDir())
}

// NewTrajectoryRecorderForBaseDir creates a recorder rooted at an explicit
// Maclaw data directory. Tests and embedded runtimes pass their base directory
// through the same dependency boundary used by the rest of App.
func NewTrajectoryRecorderForBaseDir(baseDir string) *TrajectoryRecorder {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = corelib.MaclawBaseDir()
	}
	return &TrajectoryRecorder{
		dir: filepath.Join(baseDir, "trajectories"),
	}
}

// SetPipeline sets the skill auto-summary pipeline to be triggered on Flush.
// The pipeline can be nil (no-op if not set).
func (r *TrajectoryRecorder) SetPipeline(p *SkillAutoSummaryPipeline) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pipeline = p
}

// StartSession begins recording a new trajectory session.
func (r *TrajectoryRecorder) StartSession(sessionID, provider, model, protocol, userID, platform string, tools []map[string]interface{}) {
	r.StartSessionWithMeta(sessionID, provider, model, protocol, userID, platform, "", "", tools)
}

// StartSessionWithMeta is like StartSession but also sets session kind and parent.
func (r *TrajectoryRecorder) StartSessionWithMeta(sessionID, provider, model, protocol, userID, platform, kind, parentSessionID string, tools []map[string]interface{}) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var toolsCopy []interface{}
	if n := len(tools); n > 0 {
		toolsCopy = make([]interface{}, 0, n)
		for _, t := range tools {
			toolsCopy = append(toolsCopy, t)
		}
	}

	r.session = &TrajectorySession{
		SessionID:       sessionID,
		ParentSessionID: strings.TrimSpace(parentSessionID),
		Kind:            strings.TrimSpace(kind),
		StartTime:       time.Now().Format(time.RFC3339),
		Provider:        provider,
		Model:           model,
		Protocol:        protocol,
		UserID:          userID,
		Platform:        platform,
		Tools:           toolsCopy,
		Entries:         make([]TrajectoryEntry, 0, 16),
	}
}

// SetKind sets the session kind (main/shared/coding_subagent/...).
func (r *TrajectoryRecorder) SetKind(kind string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	r.session.Kind = strings.TrimSpace(kind)
}

// SetParentSessionID links this trajectory to a parent loop session.
func (r *TrajectoryRecorder) SetParentSessionID(parentID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	r.session.ParentSessionID = strings.TrimSpace(parentID)
}

// SetCurrentIteration sets the 1-based iteration stamped on subsequent entries.
// Pass a 0-based loop index (as used by agent loops); it is stored as index+1.
func (r *TrajectoryRecorder) SetCurrentIteration(zeroBased int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if zeroBased < 0 {
		r.currentIteration = 0
		return
	}
	r.currentIteration = zeroBased + 1
}

// SetOutcome records loop-level outcome metadata before Flush.
func (r *TrajectoryRecorder) SetOutcome(status, errMsg string, iterations, toolCalls, inputTokens, outputTokens int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	if s := strings.TrimSpace(status); s != "" {
		r.session.Status = s
	}
	r.session.Error = strings.TrimSpace(errMsg)
	// Always write counters when caller supplies them (including zero after a
	// failed first-iteration cancel) so outcome is not stale from partial fills.
	if iterations >= 0 {
		r.session.Iterations = iterations
	}
	if toolCalls >= 0 {
		r.session.ToolCallCount = toolCalls
	}
	if inputTokens >= 0 {
		r.session.InputTokens = inputTokens
	}
	if outputTokens >= 0 {
		r.session.OutputTokens = outputTokens
	}
}

// HasOutcome reports whether the current session already has a status stamp
// (e.g. from RecordLoopResult). Used to avoid clobbering richer outcomes with
// a sparse IM response on shared-loop exit.
func (r *TrajectoryRecorder) HasOutcome() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session != nil && strings.TrimSpace(r.session.Status) != ""
}

// isTrajectoryCancelledSignal reports cancel outcomes without matching bare
// "cancel" substrings (e.g. unrelated "cancel" wording in error text).
func isTrajectoryCancelledSignal(errMsg, text string) bool {
	errLower := strings.ToLower(strings.TrimSpace(errMsg))
	textLower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(errLower, "cancelled") ||
		strings.Contains(errLower, "canceled") ||
		strings.HasPrefix(textLower, "task cancelled") ||
		strings.HasPrefix(textLower, "task canceled")
}

// trajectoryEntryIsToolResult reports whether an entry already closes a tool call.
// Live path uses role=tool_result; legacy skill drafts used role=tool + non-map content.
func trajectoryEntryIsToolResult(e TrajectoryEntry) bool {
	if strings.TrimSpace(e.ToolCallID) == "" {
		return false
	}
	switch e.Role {
	case "tool_result":
		return true
	case "tool":
		_, isCall := e.Content.(map[string]interface{})
		return !isCall
	default:
		return false
	}
}

// classifyIMAgentResponseOutcome maps a legacy IMAgentResponse into trajectory
// status + error fields. Shared by logging, SetOutcomeFromIMResponse, and
// unpaired-close reason selection so cancel/pause/hard_exit never drift.
func classifyIMAgentResponseOutcome(resp *IMAgentResponse) (status, errMsg string) {
	if resp == nil {
		return "success", ""
	}
	errMsg = strings.TrimSpace(resp.Error)
	switch {
	case isTrajectoryCancelledSignal(errMsg, resp.Text):
		// cancelledExitResponse uses Text only (no Error field).
		if errMsg == "" {
			errMsg = "cancelled"
		}
		return "cancelled", errMsg
	case resp.HardExit:
		return "hard_exit", errMsg
	case errMsg != "":
		return "error", errMsg
	case resp.ResponseSource == imResponseSourceRecordAudio.String() ||
		resp.ResponseSource == imResponseSourceAskUser.String():
		return "paused", ""
	default:
		return "success", ""
	}
}

// SetOutcomeFromIMResponse maps a legacy agent-loop IMAgentResponse + telemetry
// into session outcome metadata. Safe with nil inputs.
// iterations / toolCalls use -1 to leave the field unchanged when unknown.
// Token fields of 0 leave prior values unchanged (shared path may re-apply after
// RecordLoopResult already recorded usage).
func (r *TrajectoryRecorder) SetOutcomeFromIMResponse(resp *IMAgentResponse, telemetry *agentLoopTelemetry, iterations, toolCalls int) {
	if r == nil {
		return
	}
	status, errMsg := classifyIMAgentResponseOutcome(resp)
	inTok, outTok := -1, -1
	if telemetry != nil {
		// Prefer full-loop totals when available; fall back to last-round.
		if telemetry.TotalLLMInputTokens > 0 {
			inTok = telemetry.TotalLLMInputTokens
		} else if telemetry.LastLLMInputTokens > 0 {
			inTok = telemetry.LastLLMInputTokens
		}
		if telemetry.TotalLLMOutputTokens > 0 {
			outTok = telemetry.TotalLLMOutputTokens
		} else if telemetry.LastLLMOutputTokens > 0 {
			outTok = telemetry.LastLLMOutputTokens
		}
	}
	if resp != nil {
		// Prefer response token fields when they look like full-loop totals
		// (already aggregated by the host). Otherwise keep telemetry totals.
		if resp.InputTokens > inTok && resp.InputTokens > 0 {
			inTok = resp.InputTokens
		} else if inTok < 0 && resp.InputTokens > 0 {
			inTok = resp.InputTokens
		}
		if resp.OutputTokens > outTok && resp.OutputTokens > 0 {
			outTok = resp.OutputTokens
		} else if outTok < 0 && resp.OutputTokens > 0 {
			outTok = resp.OutputTokens
		}
	}
	r.SetOutcome(status, errMsg, iterations, toolCalls, inTok, outTok)
}

// SetOutcomeFromLoopResult maps a shared/corelib LoopResult into session metadata.
func (r *TrajectoryRecorder) SetOutcomeFromLoopResult(result agent.LoopResult) {
	if r == nil {
		return
	}
	status := "success"
	switch {
	case isTrajectoryCancelledSignal(result.Error, ""):
		status = "cancelled"
	case result.HardExit:
		status = "hard_exit"
	case strings.TrimSpace(result.Error) != "":
		status = "error"
	case result.RecordAudio != nil || result.AskUser != nil:
		// Interactive pause — host UI / user reply required before the loop continues.
		status = "paused"
	}
	// Token counters: only write when the loop reported usage. Zero zeros would
	// otherwise clobber values already stamped by a prior richer SetOutcome.
	inTok, outTok := -1, -1
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		inTok = result.Usage.InputTokens
		outTok = result.Usage.OutputTokens
	}
	model := strings.TrimSpace(result.Usage.Model)
	provider := strings.TrimSpace(result.Usage.Provider)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	if s := strings.TrimSpace(status); s != "" {
		r.session.Status = s
	}
	r.session.Error = strings.TrimSpace(result.Error)
	if result.Iterations >= 0 {
		r.session.Iterations = result.Iterations
	}
	if result.ToolCalls >= 0 {
		r.session.ToolCallCount = result.ToolCalls
	}
	if inTok >= 0 {
		r.session.InputTokens = inTok
	}
	if outTok >= 0 {
		r.session.OutputTokens = outTok
	}
	// Prefer the model that actually ran the loop (routing / escalation).
	if model != "" {
		r.session.Model = model
	}
	if provider != "" {
		r.session.Provider = provider
	}
}

// Record appends a conversation entry to the current session.
func (r *TrajectoryRecorder) Record(role string, content interface{}, toolCalls interface{}, toolCallID string, reasoning string) {
	r.RecordEntry(TrajectoryEntry{
		Role:       role,
		Content:    content,
		ToolCalls:  toolCalls,
		ToolCallID: toolCallID,
		Reasoning:  reasoning,
	})
}

// RecordEntry appends a fully-specified trajectory entry.
func (r *TrajectoryRecorder) RecordEntry(entry TrajectoryEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	if strings.TrimSpace(entry.Timestamp) == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339Nano)
	}
	if entry.Iteration <= 0 && r.currentIteration > 0 {
		entry.Iteration = r.currentIteration
	}
	if entry.Role == "tool_result" {
		entry.ToolOutcome = normalizeTrajectoryToolOutcome(entry.ToolOutcome)
	}
	r.session.Entries = append(r.session.Entries, entry)
}

// normalizeTrajectoryToolOutcome maps host/loop outcome strings onto the
// trajectory vocabulary used by the main agent loop and CloseUnpairedToolCalls
// (succeeded/failed/cancelled/paused/uncertain). Shared RunLoop emits ok/error/timeout.
func normalizeTrajectoryToolOutcome(outcome string) string {
	raw := strings.TrimSpace(outcome)
	if raw == "" {
		return ""
	}
	switch strings.ToLower(raw) {
	case "succeeded", "success", "ok", "pass", "passed":
		return "succeeded"
	case "failed", "fail", "failure", "error", "timeout", "timed_out", "timedout":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	case "paused", "pause":
		return "paused"
	case "uncertain", "unknown":
		return "uncertain"
	default:
		return raw
	}
}

// CloseUnpairedToolCalls appends synthetic tool_result entries for any tool
// calls that never received a result (cancel mid-batch, early stop, panic).
// reason becomes the tool_result content (e.g. "cancelled").
func (r *TrajectoryRecorder) CloseUnpairedToolCalls(reason string) {
	if r == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unpaired tool call closed"
	}
	outcome := "failed"
	reasonLower := strings.ToLower(reason)
	if strings.Contains(reasonLower, "cancelled") || strings.Contains(reasonLower, "canceled") {
		outcome = "cancelled"
	} else if strings.Contains(reasonLower, "pause") {
		outcome = "paused"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil || len(r.session.Entries) == 0 {
		return
	}
	// Single pass: mark results, collect open calls, and cache iterations.
	haveResult := make(map[string]bool, 8)
	iterByID := make(map[string]int, 8)
	type pending struct {
		id, name string
	}
	var open []pending
	seen := make(map[string]bool, 8)
	addOpen := func(id, name string, iter int) {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" || haveResult[id] {
			return
		}
		if seen[id] {
			// Upgrade empty name / iteration when a later expanded tool row is richer.
			if name != "" || iter > 0 {
				for i := range open {
					if open[i].id != id {
						continue
					}
					if open[i].name == "" && name != "" {
						open[i].name = name
					}
					break
				}
			}
			if iter > 0 {
				if prev, ok := iterByID[id]; !ok || prev <= 0 {
					iterByID[id] = iter
				}
			}
			return
		}
		seen[id] = true
		open = append(open, pending{id: id, name: name})
		if iter > 0 {
			iterByID[id] = iter
		}
	}
	for _, e := range r.session.Entries {
		if trajectoryEntryIsToolResult(e) {
			id := strings.TrimSpace(e.ToolCallID)
			haveResult[id] = true
			if e.Iteration > 0 {
				iterByID[id] = e.Iteration
			}
			continue
		}
		switch e.Role {
		case "tool":
			// Call shape only (map content); string/legacy results handled above.
			id := strings.TrimSpace(e.ToolCallID)
			if id == "" {
				continue
			}
			m, isCall := e.Content.(map[string]interface{})
			if !isCall {
				continue
			}
			name := e.ToolName
			if name == "" {
				if n, ok := m["name"].(string); ok {
					name = n
				}
			}
			addOpen(id, name, e.Iteration)
		case "assistant":
			if e.ToolCalls == nil {
				continue
			}
			for _, tc := range extractTrajectoryToolCalls(e.ToolCalls) {
				addOpen(tc.ID, tc.Name, e.Iteration)
			}
		}
	}
	// Drop any open ids that also received a result later in the same session.
	if len(open) == 0 {
		return
	}
	filtered := open[:0]
	for _, p := range open {
		if haveResult[p.id] {
			continue
		}
		filtered = append(filtered, p)
	}
	open = filtered
	if len(open) == 0 {
		return
	}
	now := time.Now().Format(time.RFC3339Nano)
	for _, p := range open {
		entryIter := iterByID[p.id]
		if entryIter <= 0 {
			entryIter = r.currentIteration
		}
		r.session.Entries = append(r.session.Entries, TrajectoryEntry{
			Timestamp:   now,
			Role:        "tool_result",
			Content:     reason,
			ToolCallID:  p.id,
			ToolName:    p.name,
			ToolOutcome: outcome,
			Iteration:   entryIter,
		})
	}
}

// RecordHistory records prior multi-turn history that the model will see.
// Assistant tool_calls are expanded into separate role=tool entries for parity
// with the live legacy agent-loop recorder.
func (r *TrajectoryRecorder) RecordHistory(history []agent.ConversationEntry) {
	r.recordConversationEntries(history, true, false)
}

// RecordHistoryDelta records entries produced by a RunLoop turn.
// When skipLeadingUser is true, the first role=user entry is skipped (already
// recorded at session start by the host).
// Assistant turns advance a 1-based iteration counter so shared/subagent
// replays get per-round iteration stamps without a live SetCurrentIteration.
func (r *TrajectoryRecorder) RecordHistoryDelta(delta []agent.ConversationEntry, skipLeadingUser bool) {
	if len(delta) == 0 {
		return
	}
	if skipLeadingUser && delta[0].Role == "user" {
		delta = delta[1:]
	}
	r.recordConversationEntries(delta, true, true)
}

// RecordLoopResult records HistoryDelta (skipping the leading user) and outcome.
// On cancel / hard-exit / error, unpaired tool calls are closed synthetically
// so tool_call ↔ tool_result pairing stays complete for training consumers.
func (r *TrajectoryRecorder) RecordLoopResult(result agent.LoopResult) {
	if r == nil {
		return
	}
	r.RecordHistoryDelta(result.HistoryDelta, true)
	if reason := unpairedCloseReasonFromLoopResult(result); reason != "" {
		r.CloseUnpairedToolCalls(reason)
	}
	r.SetOutcomeFromLoopResult(result)
}

func unpairedCloseReasonFromLoopResult(result agent.LoopResult) string {
	switch {
	case isTrajectoryCancelledSignal(result.Error, ""):
		return "cancelled"
	case result.HardExit:
		if strings.TrimSpace(result.Error) != "" {
			return result.Error
		}
		return "hard_exit"
	case strings.TrimSpace(result.Error) != "":
		return result.Error
	default:
		// Interactive pause: host adds the primary tool_result separately;
		// remaining unpaired tools are closed after that via CloseUnpairedToolCalls("loop_paused").
		return ""
	}
}

// unpairedCloseReasonFromIMResponse maps a legacy IMAgentResponse into a synthetic
// tool_result close reason. Empty means leave unpaired tools alone (success/paused
// already pair via normal recording paths).
func unpairedCloseReasonFromIMResponse(resp *IMAgentResponse) string {
	if resp == nil {
		return "aborted"
	}
	status, errMsg := classifyIMAgentResponseOutcome(resp)
	switch status {
	case "cancelled":
		return "cancelled"
	case "hard_exit":
		if errMsg != "" {
			return errMsg
		}
		return "hard_exit"
	case "error":
		if errMsg != "" {
			return errMsg
		}
		return "error"
	default:
		// success / paused — pairing handled in-loop.
		return ""
	}
}

// RecordEarlyStopToolResult appends a synthetic tool_result for interactive
// early-stops (ask_user / record_audio) where HistoryDelta lacks the result.
// Any remaining unpaired tool calls are closed as paused.
// Empty content still closes siblings (pairing integrity over content richness).
// Idempotent for the primary toolCallID: a second call only closes remaining siblings.
func (r *TrajectoryRecorder) RecordEarlyStopToolResult(toolCallID, toolName, content string) {
	if r == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		if toolName == "" {
			toolName = "early_stop"
		}
		toolCallID = toolName + "_early_stop"
	}
	content = strings.TrimSpace(content)
	if content == "" {
		if toolName != "" {
			content = toolName + " paused"
		} else {
			content = "loop_paused"
		}
	}
	if !r.hasToolResultID(toolCallID) {
		r.RecordEntry(TrajectoryEntry{
			Role:        "tool_result",
			Content:     content,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			ToolOutcome: "paused",
		})
	}
	// Close siblings from the same parallel tool batch that never ran.
	r.CloseUnpairedToolCalls("loop_paused")
}

// hasToolResultID reports whether a tool_result for id is already recorded.
func (r *TrajectoryRecorder) hasToolResultID(toolCallID string) bool {
	if r == nil {
		return false
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return false
	}
	for _, e := range r.session.Entries {
		if trajectoryEntryIsToolResult(e) && strings.TrimSpace(e.ToolCallID) == toolCallID {
			return true
		}
	}
	return false
}

// TruncateToolCallArguments shortens oversized tool-call argument strings on
// the matching role=tool entry and nested assistant tool_calls. Used when the
// live loop truncates failed oversized args so trajectory does not retain multi-KB blobs.
func (r *TrajectoryRecorder) TruncateToolCallArguments(toolCallID, summary string) {
	if r == nil {
		return
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" || summary == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	for i := range r.session.Entries {
		e := &r.session.Entries[i]
		if e.Role == "tool" && e.ToolCallID == toolCallID {
			if m, ok := e.Content.(map[string]interface{}); ok {
				cp := make(map[string]interface{}, len(m)+1)
				for k, v := range m {
					cp[k] = v
				}
				cp["arguments"] = summary
				e.Content = cp
			}
		}
		if e.Role == "assistant" && e.ToolCalls != nil {
			e.ToolCalls = rewriteToolCallsArguments(e.ToolCalls, toolCallID, summary)
		}
	}
}

// rewriteToolCallsArguments replaces Arguments for the matching tool call id.
func rewriteToolCallsArguments(toolCalls interface{}, toolCallID, summary string) interface{} {
	switch calls := toolCalls.(type) {
	case []llm.ToolCall:
		out := make([]llm.ToolCall, len(calls))
		copy(out, calls)
		for i := range out {
			if out[i].ID == toolCallID {
				out[i].Function.Arguments = summary
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(calls))
		for i, raw := range calls {
			out[i] = rewriteOneToolCallArguments(raw, toolCallID, summary)
		}
		return out
	default:
		data, err := json.Marshal(toolCalls)
		if err != nil {
			return toolCalls
		}
		var generic []interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return toolCalls
		}
		for i, raw := range generic {
			generic[i] = rewriteOneToolCallArguments(raw, toolCallID, summary)
		}
		return generic
	}
}

func rewriteOneToolCallArguments(raw interface{}, toolCallID, summary string) interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		cp := make(map[string]interface{}, len(v)+1)
		for k, val := range v {
			cp[k] = val
		}
		if id, ok := cp["id"].(string); ok && id == toolCallID {
			if fn, ok := cp["function"].(map[string]interface{}); ok {
				fnCopy := make(map[string]interface{}, len(fn)+1)
				for k, val := range fn {
					fnCopy[k] = val
				}
				fnCopy["arguments"] = summary
				cp["function"] = fnCopy
			} else {
				cp["arguments"] = summary
			}
		}
		return cp
	case llm.ToolCall:
		if v.ID == toolCallID {
			v.Function.Arguments = summary
		}
		return v
	default:
		return raw
	}
}

// RewriteToolCallID updates matching tool / tool_result entries and nested
// assistant tool_calls ids so redacted conversation ids stay paired everywhere
// (live tool rows, tool results, and assistant.tool_calls used by skill draft).
func (r *TrajectoryRecorder) RewriteToolCallID(from, to string) {
	if r == nil {
		return
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	for i := range r.session.Entries {
		e := &r.session.Entries[i]
		if e.ToolCallID == from && (e.Role == "tool" || e.Role == "tool_result") {
			e.ToolCallID = to
		}
		if e.Role == "assistant" && e.ToolCalls != nil {
			e.ToolCalls = rewriteToolCallsID(e.ToolCalls, from, to)
		}
	}
}

// rewriteToolCallsID best-effort rewrites id fields inside a tool_calls payload.
// Returns the original value when the shape is not recognized.
func rewriteToolCallsID(toolCalls interface{}, from, to string) interface{} {
	switch calls := toolCalls.(type) {
	case []llm.ToolCall:
		out := make([]llm.ToolCall, len(calls))
		copy(out, calls)
		for i := range out {
			if out[i].ID == from {
				out[i].ID = to
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(calls))
		for i, raw := range calls {
			out[i] = rewriteOneToolCallID(raw, from, to)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(calls))
		for i, m := range calls {
			out[i] = rewriteToolCallMapID(m, from, to)
		}
		return out
	default:
		// Typed slices via JSON round-trip.
		data, err := json.Marshal(toolCalls)
		if err != nil {
			return toolCalls
		}
		var generic []interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return toolCalls
		}
		for i, raw := range generic {
			generic[i] = rewriteOneToolCallID(raw, from, to)
		}
		return generic
	}
}

func rewriteOneToolCallID(raw interface{}, from, to string) interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return rewriteToolCallMapID(v, from, to)
	case llm.ToolCall:
		if v.ID == from {
			v.ID = to
		}
		return v
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return raw
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return raw
		}
		return rewriteToolCallMapID(m, from, to)
	}
}

func rewriteToolCallMapID(m map[string]interface{}, from, to string) map[string]interface{} {
	if m == nil {
		return m
	}
	// Shallow copy so we do not mutate shared maps still referenced elsewhere.
	cp := make(map[string]interface{}, len(m)+1)
	for k, v := range m {
		cp[k] = v
	}
	if id, ok := cp["id"].(string); ok && id == from {
		cp["id"] = to
	}
	return cp
}

// recordConversationEntries builds entries unlocked then appends under one lock
// to avoid N mutex round-trips on long multi-turn histories.
// When stampByAssistantRound is true, each assistant message advances a 1-based
// iteration counter applied to that assistant and following tool/system rows
// until the next assistant (used for shared/subagent HistoryDelta replay).
func (r *TrajectoryRecorder) recordConversationEntries(entries []agent.ConversationEntry, expandToolCalls, stampByAssistantRound bool) {
	if r == nil || len(entries) == 0 {
		return
	}
	built := buildTrajectoryEntriesFromConversation(entries, expandToolCalls)
	if len(built) == 0 {
		return
	}
	if stampByAssistantRound {
		stampTrajectoryEntriesByAssistantRounds(built)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil {
		return
	}
	if r.currentIteration > 0 {
		for i := range built {
			if built[i].Iteration <= 0 {
				built[i].Iteration = r.currentIteration
			}
		}
	}
	r.session.Entries = append(r.session.Entries, built...)
}

// stampTrajectoryEntriesByAssistantRounds assigns 1-based iteration numbers
// based on assistant turns (shared RunLoop HistoryDelta has no live iteration).
func stampTrajectoryEntriesByAssistantRounds(entries []TrajectoryEntry) {
	iter := 0
	for i := range entries {
		if entries[i].Role == "assistant" {
			iter++
		}
		if entries[i].Iteration <= 0 && iter > 0 {
			entries[i].Iteration = iter
		}
	}
}

func buildTrajectoryEntriesFromConversation(entries []agent.ConversationEntry, expandToolCalls bool) []TrajectoryEntry {
	if len(entries) == 0 {
		return nil
	}
	now := time.Now().Format(time.RFC3339Nano)
	out := make([]TrajectoryEntry, 0, len(entries)+4)
	for _, entry := range entries {
		role := strings.TrimSpace(entry.Role)
		if role == "" {
			continue
		}
		switch role {
		case "assistant":
			out = append(out, TrajectoryEntry{
				Timestamp:    now,
				Role:         "assistant",
				Content:      entry.Content,
				ToolCalls:    entry.ToolCalls,
				Reasoning:    entry.ReasoningContent,
				FinishReason: strings.TrimSpace(entry.FinishReason),
			})
			if expandToolCalls && entry.ToolCalls != nil {
				for _, tc := range extractTrajectoryToolCalls(entry.ToolCalls) {
					out = append(out, TrajectoryEntry{
						Timestamp:  now,
						Role:       "tool",
						Content:    map[string]interface{}{"name": tc.Name, "arguments": tc.Args},
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
					})
				}
			}
		case "tool", "tool_result":
			// History uses OpenAI-style role=tool for results; trajectory uses tool_result.
			out = append(out, TrajectoryEntry{
				Timestamp:   now,
				Role:        "tool_result",
				Content:     entry.Content,
				ToolCallID:  entry.ToolCallID,
				ToolName:    entry.ToolName,
				ToolOutcome: normalizeTrajectoryToolOutcome(entry.ToolOutcome),
			})
		default:
			out = append(out, TrajectoryEntry{
				Timestamp:  now,
				Role:       role,
				Content:    entry.Content,
				ToolCalls:  entry.ToolCalls,
				ToolCallID: entry.ToolCallID,
				ToolName:   entry.ToolName,
				Reasoning:  entry.ReasoningContent,
			})
		}
	}
	return out
}

type trajectoryToolCall struct {
	ID   string
	Name string
	Args string
}

// extractTrajectoryToolCalls normalizes tool_calls payloads from history/LLM layers.
func extractTrajectoryToolCalls(toolCalls interface{}) []trajectoryToolCall {
	if toolCalls == nil {
		return nil
	}
	switch calls := toolCalls.(type) {
	case []llm.ToolCall:
		if len(calls) == 0 {
			return nil
		}
		out := make([]trajectoryToolCall, 0, len(calls))
		for _, tc := range calls {
			if tc.Function.Name == "" && tc.ID == "" {
				continue
			}
			out = append(out, trajectoryToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		return out
	case []interface{}:
		out := make([]trajectoryToolCall, 0, len(calls))
		for _, raw := range calls {
			if tc, ok := trajectoryToolCallFromAny(raw); ok {
				out = append(out, tc)
			}
		}
		return out
	case []map[string]interface{}:
		out := make([]trajectoryToolCall, 0, len(calls))
		for _, m := range calls {
			if tc, ok := trajectoryToolCallFromMap(m); ok {
				out = append(out, tc)
			}
		}
		return out
	default:
		// Best-effort JSON round-trip for other typed slices.
		data, err := json.Marshal(toolCalls)
		if err != nil {
			return nil
		}
		var generic []interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return nil
		}
		out := make([]trajectoryToolCall, 0, len(generic))
		for _, raw := range generic {
			if tc, ok := trajectoryToolCallFromAny(raw); ok {
				out = append(out, tc)
			}
		}
		return out
	}
}

func trajectoryToolCallFromAny(raw interface{}) (trajectoryToolCall, bool) {
	switch v := raw.(type) {
	case map[string]interface{}:
		return trajectoryToolCallFromMap(v)
	case llm.ToolCall:
		if v.Function.Name == "" && v.ID == "" {
			return trajectoryToolCall{}, false
		}
		return trajectoryToolCall{ID: v.ID, Name: v.Function.Name, Args: v.Function.Arguments}, true
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return trajectoryToolCall{}, false
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return trajectoryToolCall{}, false
		}
		return trajectoryToolCallFromMap(m)
	}
}

func trajectoryToolCallFromMap(m map[string]interface{}) (trajectoryToolCall, bool) {
	if m == nil {
		return trajectoryToolCall{}, false
	}
	tc := trajectoryToolCall{}
	if id, ok := m["id"].(string); ok {
		tc.ID = id
	}
	fn, _ := m["function"].(map[string]interface{})
	if fn == nil {
		// Flat shape: {name, arguments, id}
		if name, ok := m["name"].(string); ok {
			tc.Name = name
		}
		tc.Args = trajectoryArgsToString(m["arguments"])
	} else {
		if name, ok := fn["name"].(string); ok {
			tc.Name = name
		}
		tc.Args = trajectoryArgsToString(fn["arguments"])
	}
	if tc.Name == "" && tc.ID == "" {
		return trajectoryToolCall{}, false
	}
	return tc, true
}

func trajectoryArgsToString(args interface{}) string {
	switch v := args.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}

// Flush writes the current session to a JSON file and resets state.
// Safe to call multiple times; no-op if session is nil or empty.
func (r *TrajectoryRecorder) Flush() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == nil || len(r.session.Entries) == 0 {
		return
	}
	r.session.EndTime = time.Now().Format(time.RFC3339)

	if err := os.MkdirAll(r.dir, 0755); err != nil {
		log.Printf("[Trajectory] failed to create dir %s: %v", r.dir, err)
		r.session = nil
		return
	}

	// Millisecond timestamp + sanitized session id + short unique suffix so
	// concurrent flushes (parallel tabs / subagents) never clobber each other.
	ts := time.Now().Format("2006-01-02_15-04-05.000")
	safeID := safeFilenameRe.ReplaceAllString(r.session.SessionID, "_")
	if len(safeID) > 32 {
		safeID = safeID[:32]
	}
	uniq := fmt.Sprintf("%x", time.Now().UnixNano())
	filename := fmt.Sprintf("%s_%s_%s.json", ts, safeID, uniq)
	path := filepath.Join(r.dir, filename)

	data, err := json.MarshalIndent(r.session, "", "  ")
	if err != nil {
		log.Printf("[Trajectory] marshal error: %v", err)
		r.session = nil
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[Trajectory] write error: %v", err)
		r.session = nil
		return
	}
	log.Printf("[Trajectory] saved %s (%d entries)", path, len(r.session.Entries))

	// Trigger skill auto-summary pipeline in background on an isolated copy so
	// the goroutine cannot observe later session mutations.
	if r.pipeline != nil {
		sessionCopy := cloneTrajectorySession(r.session)
		pipeline := r.pipeline
		go pipeline.RunPipeline(sessionCopy)
	}

	r.session = nil
}

func cloneTrajectorySession(src *TrajectorySession) *TrajectorySession {
	if src == nil {
		return nil
	}
	cp := *src
	if src.Tools != nil {
		cp.Tools = append([]interface{}(nil), src.Tools...)
	}
	if src.Entries != nil {
		cp.Entries = append([]TrajectoryEntry(nil), src.Entries...)
	}
	return &cp
}
