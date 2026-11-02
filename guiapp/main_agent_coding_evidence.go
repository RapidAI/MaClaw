package guiapp

// main_agent_coding_evidence.go records a lightweight, durable evidence row
// for main-agent turns that directly used coding tools. It deliberately does
// not create a codingruntime task: that ledger participates in writer
// admission, recovery and the user-visible task list. This format only needs
// to let verifyCodingKnowledgeRecallOutcome answer one question: "did this
// turn really complete/fail after material work?"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const (
	maxMainAgentCodingEvidenceEvents = 32
	mainAgentCodingEvidenceTextRunes = 4096
	maxMainAgentCodingFiles          = 12
	maxMainAgentCodingCommands       = 8
	maxMainAgentCodingCommandRunes   = 200
)

// mainAgentTurnExtraction is a test seam for the extraction LLM call. The
// production value is the method below; tests replace it with a no-op or spy.
var mainAgentTurnExtraction = (*IMMessageHandler).extractMainAgentTurnExperience

type mainAgentCodingEvidenceState struct {
	generation              uint64
	ownerID                 string
	projectPath             string
	materialCount           int
	fileActivityCount       int
	failedVerificationCount int
	files                   []string
	commands                []CodingSubAgentCommandResult
	eventDigests            []string
	startedAt               time.Time
}

// mainAgentCodingToolEvent classifies successful material tool calls. Reads,
// searches and policy-rejected calls are deliberately excluded: they are not
// evidence that a direct coding turn changed the workspace.
func mainAgentCodingFileTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "write_file", "edit_file", "edit_lines", "apply_patch", "str_replace", "create_file", "delete_file", "remove_file",
		// Managed GUI / MaClawSrv adapters for the same stable prompt names.
		"semantic_write_trusted_file", "host_fs_write_local", "semantic_delete_trusted_file", "host_fs_delete_local":
		return true
	default:
		return false
	}
}

func mainAgentCodingVerificationTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "run_terminal", "shell", "powershell",
		"semantic_execute_trusted_shell", "host_shell_execute_local",
		"build_verify", "semantic_run_trusted_build_verify", "host_build_verify_local":
		return true
	default:
		return false
	}
}

func mainAgentCodingToolEvent(toolName string, result toolExecutionResult) (string, bool) {
	if result.Outcome != toolOutcomeSucceeded {
		return "", false
	}
	if mainAgentCodingFileTool(toolName) {
		return "file_activity", true
	}
	if mainAgentCodingVerificationTool(toolName) {
		return "verification", true
	}
	return "", false
}

// mainAgentCodingToolFailureEvent accepts only a command that actually ran and
// failed. Policy/approval/argument rejections never executed the command and
// must not penalize recalled guidance.
func mainAgentCodingToolFailureEvent(toolName string, result toolExecutionResult) bool {
	if result.Outcome != toolOutcomeFailed {
		return false
	}
	if result.FailureKind != toolFailureNone && result.FailureKind != toolFailureHandlerReported {
		return false
	}
	return mainAgentCodingVerificationTool(toolName)
}

func mainAgentCodingEventDigest(toolName, argsJSON, resultText string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(strings.TrimSpace(toolName)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(truncateRunesForSubAgent(argsJSON, mainAgentCodingEvidenceTextRunes)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(truncateRunesForSubAgent(resultText, mainAgentCodingEvidenceTextRunes)))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func mainAgentCodingCommandText(argsJSON string) string {
	var args map[string]any
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return ""
	}
	for _, key := range []string{"command", "cmd", "script", "input"} {
		if value, ok := args[key].(string); ok {
			if text := strings.TrimSpace(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func appendMainAgentCodingFile(files []string, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return files
	}
	for _, existing := range files {
		if existing == path {
			return files
		}
	}
	files = append(files, path)
	if len(files) > maxMainAgentCodingFiles {
		files = files[len(files)-maxMainAgentCodingFiles:]
	}
	return files
}

func appendMainAgentCodingCommand(commands []CodingSubAgentCommandResult, command CodingSubAgentCommandResult) []CodingSubAgentCommandResult {
	command.Command = truncateRunesForSubAgent(strings.TrimSpace(command.Command), maxMainAgentCodingCommandRunes)
	command.Summary = truncateRunesForSubAgent(strings.TrimSpace(command.Summary), 200)
	if command.Command == "" {
		return commands
	}
	for _, existing := range commands {
		if existing.Command == command.Command && existing.Succeeded == command.Succeeded {
			return commands
		}
	}
	commands = append(commands, command)
	if len(commands) > maxMainAgentCodingCommands {
		commands = commands[len(commands)-maxMainAgentCodingCommands:]
	}
	return commands
}

// noteMainAgentCodingToolEvidence accumulates turn-local material evidence.
// The projection only needs digests and a count; raw args/output stay in the
// existing trajectory/audit path.
func noteMainAgentCodingToolEvidence(loop *LoopContext, generation uint64, ownerID, toolName, argsJSON string, result toolExecutionResult, projectPath string) {
	if loop == nil {
		return
	}
	if generation != loop.SemanticTurnGeneration() {
		// The tool call started in a turn that has already been replaced.
		return
	}
	eventType, ok := mainAgentCodingToolEvent(toolName, result)
	failedVerification := false
	if !ok {
		if mainAgentCodingToolFailureEvent(toolName, result) {
			eventType, ok = "verification", true
			failedVerification = true
		}
	}
	if !ok {
		return
	}
	digest := mainAgentCodingEventDigest(toolName, argsJSON, result.Text)

	loop.mainAgentCodingMu.Lock()
	defer loop.mainAgentCodingMu.Unlock()
	if loop.mainAgentCodingEvidence == nil || loop.mainAgentCodingEvidence.generation != generation {
		loop.mainAgentCodingEvidence = &mainAgentCodingEvidenceState{
			generation:  generation,
			ownerID:     strings.TrimSpace(ownerID),
			projectPath: strings.TrimSpace(projectPath),
			startedAt:   time.Now().UTC(),
		}
	}
	state := loop.mainAgentCodingEvidence
	state.materialCount++
	if eventType == "file_activity" {
		state.fileActivityCount++
	}
	if failedVerification {
		state.failedVerificationCount++
	}
	if mainAgentCodingFileTool(toolName) {
		for _, path := range acpPathsFromToolArgs(toolName, argsJSON) {
			state.files = appendMainAgentCodingFile(state.files, path)
		}
	}
	if mainAgentCodingVerificationTool(toolName) {
		state.commands = appendMainAgentCodingCommand(state.commands, CodingSubAgentCommandResult{
			Command:   mainAgentCodingCommandText(argsJSON),
			Succeeded: result.Outcome == toolOutcomeSucceeded,
			Summary:   result.Text,
		})
	}
	state.eventDigests = append(state.eventDigests, digest)
	if len(state.eventDigests) > maxMainAgentCodingEvidenceEvents {
		state.eventDigests = state.eventDigests[len(state.eventDigests)-maxMainAgentCodingEvidenceEvents:]
	}
}

func (h *IMMessageHandler) noteMainAgentCodingToolResult(loop *LoopContext, toolGeneration uint64, toolName, argsJSON string, result toolExecutionResult) {
	if h == nil || loop == nil {
		return
	}
	if _, ok := mainAgentCodingToolEvent(toolName, result); !ok {
		return
	}
	if toolGeneration != loop.SemanticTurnGeneration() {
		return
	}
	projectPath := h.effectiveWorkingDirForUser(loop.UserID)
	noteMainAgentCodingToolEvidence(loop, toolGeneration, loop.UserID, toolName, argsJSON, result, projectPath)
}

func mainAgentTurnEvidenceDigest(state *mainAgentCodingEvidenceState, status string) string {
	h := sha256.New()
	_, _ = h.Write([]byte("main-agent-turn"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(status))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(state.projectPath)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(state.startedAt.UTC().Format(time.RFC3339Nano)))
	for _, digest := range state.eventDigests {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(digest))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// finalizeMainAgentCodingTurn is called at shared-agent-loop turn end. It only
// writes turn evidence when the turn actually recalled coding knowledge and
// performed material work; otherwise it is a cheap no-op.
func (h *IMMessageHandler) finalizeMainAgentCodingTurn(loop *LoopContext, turnStatus string, turnGeneration uint64, outcomeErr, userText, finalText string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[coding-experience] main-agent turn evidence panic: %v", r)
		}
	}()
	if h == nil || loop == nil || h.app == nil {
		return
	}
	currentGeneration := loop.SemanticTurnGeneration()
	loop.mainAgentCodingMu.Lock()
	state := loop.mainAgentCodingEvidence
	if turnGeneration != currentGeneration {
		// This turn was replaced. Clear only the replaced turn's own state; a
		// newer turn's accumulator must survive.
		if state != nil && state.generation == turnGeneration {
			loop.mainAgentCodingEvidence = nil
		}
		loop.mainAgentCodingMu.Unlock()
		return
	}
	if state == nil || state.generation != turnGeneration {
		loop.mainAgentCodingMu.Unlock()
		return
	}
	loop.mainAgentCodingEvidence = nil
	loop.mainAgentCodingMu.Unlock()
	if state.materialCount == 0 {
		return
	}
	// A success needs an actual file write/edit/create/delete: a successful
	// `ls` is not coding evidence. A failure is only trustworthy when a command
	// that actually ran failed; unrelated tool/policy/provider errors are not
	// evidence against the recalled guidance.
	var status string
	succeeded := false
	switch turnStatus {
	case "success":
		if state.fileActivityCount == 0 {
			return
		}
		status = knowledge.CodingTurnStatusCompleted
		succeeded = true
	case "error":
		if state.failedVerificationCount == 0 || isSubAgentTransientProviderError(outcomeErr) {
			return
		}
		status = knowledge.CodingTurnStatusFailed
	default:
		return
	}
	ids := loop.takeCodingKnowledgeRecallIDsForGeneration(state.generation)
	store := h.app.ensureCodingKnowledgeStore()
	if store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	evidence, err := store.RecordTurnEvidence(ctx, knowledge.CodingTurnEvidence{
		OwnerID:        state.ownerID,
		ProjectPath:    state.projectPath,
		Status:         status,
		MaterialCount:  state.materialCount,
		EvidenceDigest: mainAgentTurnEvidenceDigest(state, status),
	})
	if err != nil {
		log.Printf("[coding-experience] record main-agent turn evidence failed: %v", err)
		return
	}
	if len(ids) > 0 {
		go recordMainAgentTurnRecallOutcomes(h, evidence, ids, succeeded)
	}
	mainAgentTurnExtraction(h, evidence, state, status, userText, finalText, outcomeErr)
}

// extractMainAgentTurnExperience reuses the CodingSubAgent extractor with the
// turn's own durable provenance. The prompt receives only bounded user text,
// final answer, file paths and command summaries; the evidence table still
// stores digests only.
func (h *IMMessageHandler) extractMainAgentTurnExperience(evidence knowledge.CodingTurnEvidence, state *mainAgentCodingEvidenceState, status, userText, finalText, outcomeErr string) {
	if h == nil || h.app == nil || state == nil || strings.TrimSpace(userText) == "" {
		return
	}
	result := &CodingSubAgentResult{
		Status:              TaskExecPassed,
		Summary:             truncateRunesForSubAgent(strings.TrimSpace(finalText), 800),
		Error:               truncateRunesForSubAgent(strings.TrimSpace(outcomeErr), 240),
		FilesModified:       append([]string(nil), state.files...),
		CommandsRun:         append([]CodingSubAgentCommandResult(nil), state.commands...),
		ToolCalls:           state.materialCount,
		VerificationSummary: fmt.Sprintf("%d material tool(s), %d failed verification(s)", state.materialCount, state.failedVerificationCount),
	}
	if status != knowledge.CodingTurnStatusCompleted {
		result.Status = TaskExecFailed
	}
	task := &TaskItem{
		Title:       truncateRunesForSubAgent(strings.TrimSpace(userText), 120),
		Description: truncateRunesForSubAgent(strings.TrimSpace(userText), 300),
		Files:       append([]string(nil), state.files...),
	}
	if result.Status == TaskExecPassed && state.failedVerificationCount > 0 {
		// A failed verification followed by success is the retry-success signal
		// the on_retry_success policy uses.
		task.RetryCount = 1
	}
	runner := &SubAgentTaskRunner{
		handler:      h,
		cfg:          h.getCodingLLMConfig(),
		httpClient:   h.client,
		orchestrator: &TaskExecutionOrchestrator{ProjectPath: state.projectPath},
	}
	runner.extractAndSaveExperienceWithProvenance(task, result, task.RetryCount > 0, codingExperienceProvenance{
		TaskID:         knowledge.CodingTurnEvidenceTaskPrefix + evidence.ID,
		AttemptID:      knowledge.CodingTurnEvidenceAttemptPrefix + evidence.ID,
		EvidenceDigest: evidence.EvidenceDigest,
	})
}

func recordMainAgentTurnRecallOutcomes(h *IMMessageHandler, evidence knowledge.CodingTurnEvidence, ids []string, succeeded bool) {
	if h == nil || h.app == nil || len(ids) == 0 {
		return
	}
	store := h.app.ensureCodingKnowledgeStore()
	if store == nil {
		return
	}
	outcome := knowledge.RecallOutcome{
		RuntimeTaskID:    knowledge.CodingTurnEvidenceTaskPrefix + evidence.ID,
		RuntimeAttemptID: knowledge.CodingTurnEvidenceAttemptPrefix + evidence.ID,
		EvidenceDigest:   evidence.EvidenceDigest,
		TaskSucceeded:    succeeded,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, id := range ids {
		if recErr := store.RecordRecallOutcome(ctx, id, outcome, h.app.verifyCodingKnowledgeRecallOutcome); recErr != nil && !isBenignCodingRecallOutcomeError(recErr) {
			log.Printf("[coding-experience] main-agent turn recall outcome %s: %v", id, recErr)
		}
	}
}
