package guiapp

// coding_subagent_experience.go implements experience extraction and persistence
// after CodingSubAgent task completion.
//
// Extraction flow:
// 1. Task completes (success or retry-success)
// 2. Build extraction prompt from task metadata + result summary
// 3. Lightweight LLM call to extract structured experiences
// 4. Dedup check against existing experiences
// 5. Save to coding knowledge store
//
// The extraction is best-effort: LLM timeout/failure/parse error -> skip silently.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	experienceExtractionTimeout = 15 * time.Second
	experienceMaxPerExtraction  = 2

	// Extraction is model-generated input. Bound every field before it reaches
	// SQLite/FTS: a malformed or over-verbose response must not bloat the store
	// or the later prompt/context budget.
	maxExtractedExperienceTitleRunes       = 200
	maxExtractedExperienceTriggerRunes     = 120
	maxExtractedExperienceContentRunes     = 4000
	maxExtractedExperienceCodeSnippetRunes = 2000
	maxExtractedExperienceListItems        = 8
	maxExtractedExperienceListItemRunes    = 300
	maxExtractedExperienceFrameworks       = 12
	maxExtractedExperienceLabels           = 20
)

// ExperienceSaveMode controls when experiences are auto-saved.
type ExperienceSaveMode string

const (
	ExperienceSaveModeObserve ExperienceSaveMode = "observe"
	ExperienceSaveModeAuto    ExperienceSaveMode = "auto"
	ExperienceSaveModeOff     ExperienceSaveMode = "off"
)

// ExperienceSaveStrategy controls which task outcomes trigger extraction.
type ExperienceSaveStrategy string

const (
	ExperienceStrategyAlways    ExperienceSaveStrategy = "always"
	ExperienceStrategyOnSuccess ExperienceSaveStrategy = "on_success"
	ExperienceStrategyOnRetry   ExperienceSaveStrategy = "on_retry_success"
	ExperienceStrategyOff       ExperienceSaveStrategy = "off"
)

// ---------------------------------------------------------------------------
// Extraction trigger (called from orchestrator)
// ---------------------------------------------------------------------------

func (r *SubAgentTaskRunner) codingExperienceSavePolicy() (ExperienceSaveMode, ExperienceSaveStrategy, corelib.AppConfig, bool) {
	if r == nil || r.handler == nil || r.handler.app == nil {
		return "", "", corelib.AppConfig{}, false
	}
	appCfg, err := r.handler.app.LoadConfig()
	if err != nil {
		return "", "", corelib.AppConfig{}, false
	}
	mode := ExperienceSaveMode(appCfg.CodingKnowledgeAutoSaveMode)
	strategy := ExperienceSaveStrategy(appCfg.CodingKnowledgeSaveStrategy)
	switch strategy {
	case ExperienceStrategyAlways, ExperienceStrategyOnSuccess, ExperienceStrategyOnRetry, ExperienceStrategyOff:
		// Explicit, valid policy.
	default:
		// Successful tasks are the default learning signal. An empty or
		// unrecognized value must not silently disable accumulation. The
		// record is still candidate-only until a reviewer (or the explicit
		// auto policy after repeated observations) promotes it.
		strategy = ExperienceStrategyOnSuccess
	}
	switch mode {
	case ExperienceSaveModeObserve, ExperienceSaveModeAuto, ExperienceSaveModeOff:
		// Explicit, valid policy.
	default:
		mode = ExperienceSaveModeObserve
	}
	return mode, strategy, appCfg, true
}

func (r *SubAgentTaskRunner) extractAndSaveExperience(
	task *TaskItem,
	result *CodingSubAgentResult,
	wasRetry bool,
) {
	if codingKnowledgeResultIsTransientFailure(result) {
		return
	}
	mode, strategy, appCfg, ok := r.codingExperienceSavePolicy()
	if !ok {
		return
	}
	wasRetry = wasRetry || codingResultLooksLikeRetrySuccess(result)
	if !codingExperienceShouldExtract(mode, strategy, result, wasRetry) {
		return
	}
	if r.codingKnowledgeStore() == nil {
		return
	}

	// Extraction output is always a candidate. `auto` additionally allows a
	// repeated, independently observed rule to be promoted through the
	// evidence path; a single extraction never enters a prompt.
	go r.doExtractAndSave(task, result, knowledge.CodingStatusCandidate, mode, appCfg, nil)
}

// extractAndSaveExperienceWithProvenance is the turn-evidence entry point for
// main-agent direct coding. The caller already holds durable turn provenance;
// no codingruntime task is required.
func (r *SubAgentTaskRunner) extractAndSaveExperienceWithProvenance(
	task *TaskItem,
	result *CodingSubAgentResult,
	wasRetry bool,
	provenance codingExperienceProvenance,
) {
	if codingKnowledgeResultIsTransientFailure(result) {
		return
	}
	mode, strategy, appCfg, ok := r.codingExperienceSavePolicy()
	if !ok {
		return
	}
	if strings.TrimSpace(provenance.TaskID) == "" || strings.TrimSpace(provenance.AttemptID) == "" || strings.TrimSpace(provenance.EvidenceDigest) == "" {
		return
	}
	wasRetry = wasRetry || codingResultLooksLikeRetrySuccess(result)
	if !codingExperienceShouldExtract(mode, strategy, result, wasRetry) {
		return
	}
	if r.codingKnowledgeStore() == nil {
		return
	}
	go r.doExtractAndSave(task, result, knowledge.CodingStatusCandidate, mode, appCfg, &provenance)
}

func (r *SubAgentTaskRunner) recordRecalledExperienceOutcomes(result *CodingSubAgentResult) {
	if r == nil || result == nil || result.HorizonOwned || len(result.RecalledExperienceIDs) == 0 {
		return
	}
	if codingKnowledgeResultIsTransientFailure(result) {
		return
	}
	if r.handler == nil || r.handler.app == nil {
		return
	}
	snapshot := &CodingSubAgentResult{
		Status:                result.Status,
		RuntimeTaskID:         result.RuntimeTaskID,
		RecalledExperienceIDs: append([]string(nil), result.RecalledExperienceIDs...),
	}
	go r.doRecordRecalledExperienceOutcomes(snapshot)
}

// recordMainAgentCodingKnowledgeOutcomes turns provisional main-agent recall
// evidence into verified outcomes when the same turn delegated a coding task.
// A non-coding turn has no ledger task; its recalls stay unrecorded, which is
// intentional: confidence changes require durable provenance.
func recordMainAgentCodingKnowledgeOutcomes(handler *IMMessageHandler, loop *LoopContext, result *CodingSubAgentResult, recallGeneration uint64) {
	if handler == nil || handler.app == nil || loop == nil || result == nil {
		return
	}
	if strings.TrimSpace(result.RuntimeTaskID) == "" {
		return
	}
	// A horizon episode and an interrupted turn have no knowledge-eligible
	// terminal outcome; keep the provisional IDs for a later delegation.
	if result.HorizonOwned || result.Status == TaskExecInterrupted {
		return
	}
	status, _ := normalizeSubAgentResultStatus(result)
	if status != TaskExecPassed && status != TaskExecFailed {
		return
	}
	// A provider failure is not evidence against the guidance the main agent
	// recalled. Keep the provisional IDs available for a later retry in the same
	// turn instead of consuming them as a failed outcome.
	if codingKnowledgeResultIsTransientFailure(result) {
		return
	}
	ids := loop.takeCodingKnowledgeRecallIDsForGeneration(recallGeneration)
	if len(ids) == 0 {
		return
	}
	runner := &SubAgentTaskRunner{handler: handler}
	runner.recordRecalledExperienceOutcomes(&CodingSubAgentResult{
		Status:                status,
		RuntimeTaskID:         result.RuntimeTaskID,
		Error:                 result.Error,
		RecalledExperienceIDs: ids,
	})
}

// codingKnowledgeFinalizationStatus is the shared eligibility gate for local
// post-task knowledge work. Waiting postures are not terminal outcomes, and a
// result outside the durable ledger has no provenance to bind an experience to.
func codingKnowledgeFinalizationStatus(result *CodingSubAgentResult) (TaskExecStatus, bool) {
	if result == nil || result.HorizonOwned || strings.TrimSpace(result.RuntimeTaskID) == "" {
		return "", false
	}
	status, _ := normalizeSubAgentResultStatus(result)
	switch status {
	case TaskExecPassed, TaskExecFailed, TaskExecSkipped:
		return status, true
	default:
		return status, false
	}
}

// codingKnowledgeProjectPath resolves the durable project identity used for
// knowledge scoping. Isolated worktree execution runs under an ephemeral
// checkout; its guidance must belong to the main project, not the worktree.
func codingKnowledgeProjectPath(projectPath string, runtimeOptions *guiCodingRuntimeOptions) string {
	projectPath = strings.TrimSpace(projectPath)
	if runtimeOptions != nil {
		if root := strings.TrimSpace(runtimeOptions.PolicyProjectRoot); root != "" {
			return root
		}
	}
	return projectPath
}

// codingKnowledgeResultIsTransientFailure reports whether a failed coding
// result is really an infrastructure failure (provider rate limit/timeout/5xx)
// rather than evidence against the recalled guidance or the workspace. Such
// results must not lower an experience's confidence or feed extraction.
func codingKnowledgeResultIsTransientFailure(result *CodingSubAgentResult) bool {
	if result == nil {
		return false
	}
	status, summary := normalizeSubAgentResultStatus(result)
	if status != TaskExecFailed {
		return false
	}
	return isSubAgentTransientProviderError(result.Error) || isSubAgentTransientProviderError(summary)
}

// finalizeLocalCodingKnowledge is the single post-task knowledge boundary for
// local coding execution. Every GUI local path (pure workbench, delegated
// task, trusted delegate, workflow step and retry) reaches
// runTaskWithSubAgentRuntimeOptions, so recall feedback and experience
// extraction must not depend on which planner produced the task.
func finalizeLocalCodingKnowledge(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	task *TaskItem,
	result *CodingSubAgentResult,
	projectPath string,
	knowledgeProjectPath string,
) {
	if result == nil || handler == nil || handler.app == nil {
		return
	}
	if result.knowledgeFinalized {
		return
	}
	status, eligible := codingKnowledgeFinalizationStatus(result)
	if !eligible {
		return
	}
	projectPath = strings.TrimSpace(projectPath)
	knowledgeProjectPath = strings.TrimSpace(knowledgeProjectPath)
	if knowledgeProjectPath == "" {
		knowledgeProjectPath = projectPath
	}
	normalized := *result
	normalized.Status = status
	if knowledgeProjectPath != "" && projectPath != "" && knowledgeProjectPath != projectPath {
		normalized.FilesModified = remapWorktreePaths(result.FilesModified, projectPath, knowledgeProjectPath)
		normalized.FilesCreated = remapWorktreePaths(result.FilesCreated, projectPath, knowledgeProjectPath)
	}
	runner := &SubAgentTaskRunner{
		handler:      handler,
		cfg:          cfg,
		httpClient:   httpClient,
		orchestrator: &TaskExecutionOrchestrator{ProjectPath: knowledgeProjectPath},
	}
	if normalized.Status == TaskExecPassed || normalized.Status == TaskExecFailed {
		runner.recordRecalledExperienceOutcomes(&normalized)
	}
	// Inquiry turns consume knowledge but do not produce reusable execution
	// experience; extracting them would only add LLM cost and Q&A noise.
	if codingKnowledgeExtractionAllowed(task) {
		wasRetry := task != nil && task.RetryCount > 0
		runner.extractAndSaveExperience(task, &normalized, wasRetry || codingResultLooksLikeRetrySuccess(result))
	}
	result.knowledgeFinalized = true
}

func codingKnowledgeExtractionAllowed(task *TaskItem) bool {
	return task == nil || !codingTaskLooksInquiry(task)
}

func (r *SubAgentTaskRunner) doExtractAndSave(
	task *TaskItem,
	result *CodingSubAgentResult,
	initialStatus string,
	mode ExperienceSaveMode,
	appCfg corelib.AppConfig,
	provenanceOverride *codingExperienceProvenance,
) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("[coding-experience] extraction panic: %v", p)
		}
	}()
	if task == nil || result == nil {
		log.Printf("[coding-experience] skip extraction without task result")
		return
	}

	codingKB := r.codingKnowledgeStore()
	if codingKB == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), experienceExtractionTimeout)
	defer cancel()

	// Resolve the durable evidence binding before spending an LLM call. A
	// cancelled/interrupted task (or a path that never entered the ledger) is
	// not knowledge-eligible, so it must be rejected here rather than after
	// extraction. Main-agent turn evidence arrives pre-resolved.
	var provenance codingExperienceProvenance
	if provenanceOverride != nil {
		provenance = *provenanceOverride
	} else {
		resolved, provenanceErr := r.runtimeExperienceProvenance(result.RuntimeTaskID)
		if provenanceErr != nil {
			log.Printf("[coding-experience] skip runtime-derived extraction without provenance: %v", provenanceErr)
			return
		}
		provenance = resolved
	}

	projectPath := ""
	if r.orchestrator != nil {
		projectPath = r.orchestrator.ProjectPath
	}
	prompt := buildExperienceExtractionPrompt(task, result, projectPath)

	extracted, err := r.callLLMForExperienceExtraction(ctx, prompt)
	if err != nil {
		log.Printf("[coding-experience] extraction LLM call failed: %v", err)
		return
	}
	if len(extracted) == 0 {
		return
	}

	// Use a separate context for persistence (LLM call may have consumed most of the timeout).
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()

	language := inferLanguageFromTaskFiles(task.Files)
	if language == "" {
		// Without task file extensions, fall back to the project manifest so
		// language-scoped guidance is still classified instead of degrading to
		// universal.
		language = inferCodingProjectLanguage(projectPath)
	}

	savedCount := 0
	for _, exp := range extracted {
		exp.SourceTaskTitle = truncateExtractedExperienceField(task.Title, 200)
		exp.CreatedBy = "runtime"
		exp.Status = initialStatus
		exp.SourceRuntimeTaskID = provenance.TaskID
		exp.SourceRuntimeAttemptID = provenance.AttemptID
		exp.EvidenceDigest = provenance.EvidenceDigest
		if exp.ProjectPath == "" && exp.Scope == knowledge.CodingScopeProject {
			exp.ProjectPath = projectPath
		}
		if exp.Language == "" && exp.Scope == knowledge.CodingScopeLanguage {
			exp.Language = language
		}
		if exp.Scope == knowledge.CodingScopeLanguage && strings.TrimSpace(exp.Language) == "" {
			// A language-scoped rule without an inferred language cannot be
			// validated; keep it reusable as universal rather than dropping the
			// whole extraction.
			exp.Scope = knowledge.CodingScopeUniversal
		}
		if exp.Scope == "" {
			if language != "" {
				exp.Scope = knowledge.CodingScopeLanguage
				exp.Language = language
			} else {
				exp.Scope = knowledge.CodingScopeUniversal
			}
		}

		if existing, ok := findDuplicateExperience(saveCtx, codingKB, exp); ok {
			r.recordDuplicateExperienceObservation(saveCtx, codingKB, existing, exp, provenance, mode, appCfg)
			continue
		}

		saved, saveErr := codingKB.SaveRuntimeExperience(saveCtx, exp)
		if saveErr != nil {
			log.Printf("[coding-experience] save failed for %q: %v", exp.Title, saveErr)
			continue
		}
		log.Printf("[coding-experience] saved: %s (id=%s, scope=%s, status=%s)", saved.Title, saved.ID, saved.Scope, saved.Status)
		savedCount++
	}

	if mode == ExperienceSaveModeAuto && savedCount > 0 {
		log.Printf("[coding-experience] auto mode: %d candidate(s) awaiting review", savedCount)
	}

	// Trigger eviction check after saving (best-effort, don't block)
	if r.handler != nil && r.handler.app != nil {
		if evicted, err := r.handler.app.CodingKnowledgeEvict(); err == nil && evicted > 0 {
			log.Printf("[coding-experience] evicted %d experiences after save", evicted)
		}
	}
}

func (r *SubAgentTaskRunner) doRecordRecalledExperienceOutcomes(result *CodingSubAgentResult) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("[coding-experience] recall outcome panic: %v", p)
		}
	}()
	if r == nil || result == nil || r.handler == nil || r.handler.app == nil {
		return
	}
	store := r.handler.app.ensureCodingKnowledgeStore()
	if store == nil {
		return
	}
	runtimeTaskID := result.RuntimeTaskID
	taskSucceeded := result.Status == TaskExecPassed
	ids := append([]string(nil), result.RecalledExperienceIDs...)
	provenance, err := codingExperienceRuntimeProvenance(r.handler.app, runtimeTaskID)
	if err != nil {
		log.Printf("[coding-experience] skip recall outcome without provenance: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome := knowledge.RecallOutcome{
		RuntimeTaskID:    provenance.TaskID,
		RuntimeAttemptID: provenance.AttemptID,
		EvidenceDigest:   provenance.EvidenceDigest,
		TaskSucceeded:    taskSucceeded,
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if recErr := store.RecordRecallOutcome(ctx, id, outcome, r.handler.app.verifyCodingKnowledgeRecallOutcome); recErr != nil && !isBenignCodingRecallOutcomeError(recErr) {
			log.Printf("[coding-experience] recall outcome %s: %v", id, recErr)
		}
	}
}

type codingExperienceProvenance struct {
	TaskID         string
	AttemptID      string
	EvidenceDigest string
}

func (r *SubAgentTaskRunner) runtimeExperienceProvenance(runtimeTaskID string) (codingExperienceProvenance, error) {
	if r == nil || r.handler == nil {
		return codingExperienceProvenance{}, fmt.Errorf("coding runtime application is unavailable")
	}
	return codingExperienceRuntimeProvenance(r.handler.app, runtimeTaskID)
}

// codingExperienceRuntimeProvenance resolves the durable evidence binding
// shared by every GUI-managed automatic knowledge writer. It intentionally
// accepts an App rather than a SubAgent implementation so local, remote and
// experiment flows cannot bypass the Runtime ledger when persisting knowledge.
func codingExperienceRuntimeProvenance(app *App, runtimeTaskID string) (codingExperienceProvenance, error) {
	if app == nil {
		return codingExperienceProvenance{}, fmt.Errorf("coding runtime application is unavailable")
	}
	store := app.ensureCodingRuntimeStore()
	if store == nil {
		return codingExperienceProvenance{}, fmt.Errorf("coding runtime store is unavailable")
	}
	provenance, err := codingruntime.ResolveExperienceProvenance(store, runtimeTaskID)
	if err != nil {
		return codingExperienceProvenance{}, err
	}
	return codingExperienceProvenance{TaskID: provenance.TaskID, AttemptID: provenance.AttemptID, EvidenceDigest: provenance.EvidenceDigest}, nil
}

// codingExperienceKnowledgeTerminalStatus remains a package-local shim for
// existing GUI tests; the actual cross-host policy lives in corelib.
func codingExperienceKnowledgeTerminalStatus(status codingruntime.TaskStatus) bool {
	return codingruntime.KnowledgeEligibleTerminalStatus(status)
}

// codingExperienceEvidenceDigest binds an extracted experience to the compact,
// durable facts from the exact completed Attempt. It intentionally hashes event
// digests instead of copying their payloads: the knowledge DB gets a stable
// provenance pointer without inheriting command output, model transcripts, or
// any host-private diagnostic text.
//
// A completed Attempt with only lifecycle events is not enough for automatic
// knowledge extraction. It would let a model turn an unsubstantiated success
// into reusable guidance, contrary to M3's evidence-driven contract.
func codingExperienceEvidenceDigest(store codingruntime.Store, task *codingruntime.Task, attempt *codingruntime.Attempt) (string, error) {
	return codingruntime.ExperienceEvidenceDigest(store, task, attempt)
}

func codingExperienceMaterialEvidenceEvent(eventType string) bool {
	return codingruntime.MaterialExperienceEvidenceEvent(eventType)
}

func (r *SubAgentTaskRunner) codingKnowledgeStore() *knowledge.CodingKnowledgeStore {
	if r == nil || r.handler == nil || r.handler.app == nil {
		return nil
	}
	return r.handler.app.ensureCodingKnowledgeStore()
}

// ---------------------------------------------------------------------------
// LLM-based experience extraction
// ---------------------------------------------------------------------------

type extractedExperienceResponse struct {
	Experiences []extractedExperienceItem `json:"experiences"`
}

type extractedExperienceItem struct {
	Title             string   `json:"title"`
	Category          string   `json:"category"`
	Scope             string   `json:"scope,omitempty"`
	TriggerCondition  string   `json:"trigger_condition"`
	Content           string   `json:"content"`
	CodeSnippet       string   `json:"code_snippet,omitempty"`
	FailedAttempts    []string `json:"failed_attempts,omitempty"`
	Contraindications []string `json:"contraindications,omitempty"`
	Frameworks        []string `json:"frameworks,omitempty"`
	Labels            []string `json:"labels,omitempty"`
	ProjectSpecific   bool     `json:"project_specific,omitempty"`
}

func buildExperienceExtractionPrompt(task *TaskItem, result *CodingSubAgentResult, projectPath string) string {
	var b strings.Builder
	b.WriteString("You are a coding experience extractor. Based on the following task execution, extract reusable programming experiences.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Only extract general, reusable programming knowledge\n")
	b.WriteString("- Do NOT extract task-specific implementation details\n")
	b.WriteString("- Return empty array if nothing worth recording\n")
	b.WriteString("- Maximum 2 experiences per extraction\n")
	b.WriteString("- trigger_condition: short keyword combo (<20 chars) for retrieval matching\n")
	b.WriteString("- category: pattern/decision/pitfall/convention\n")
	b.WriteString("- scope: universal/language/project\n")
	b.WriteString("- Record failed_attempts if applicable\n")
	b.WriteString("- Record contraindications when the guidance must NOT be applied\n")
	b.WriteString("- Record frameworks when the guidance is tied to a specific library/framework\n\n")

	b.WriteString(fmt.Sprintf("## Task\nTitle: %s\n", truncateRunesForSubAgent(task.Title, 200)))
	if task.Description != "" {
		desc := task.Description
		if len([]rune(desc)) > 300 {
			desc = string([]rune(desc)[:300]) + "..."
		}
		b.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}
	if len(task.Files) > 0 {
		b.WriteString(fmt.Sprintf("Files: %s\n", compactSubAgentFileList(task.Files, 12)))
	}
	b.WriteString(fmt.Sprintf("Project: %s\n", truncateExtractedExperienceField(projectPath, 512)))
	if frameworks := inferCodingFrameworks(projectPath, task.Files); len(frameworks) > 0 {
		b.WriteString("Detected frameworks: " + strings.Join(frameworks, ", ") + "\n")
	}

	if result != nil {
		b.WriteString(fmt.Sprintf("\n## Result\nStatus: %s\nIterations: %d\nTool calls: %d\n", result.Status, result.Iterations, result.ToolCalls))
		if result.Summary != "" {
			summary := result.Summary
			if len([]rune(summary)) > 800 {
				summary = string([]rune(summary)[:800]) + "..."
			}
			b.WriteString(fmt.Sprintf("\nSummary:\n%s\n", summary))
		}
		if result.Error != "" {
			b.WriteString(fmt.Sprintf("\nError: %s\n", truncateRunesForSubAgent(result.Error, 600)))
		}
		if len(result.FilesModified) > 0 {
			b.WriteString(fmt.Sprintf("Files modified: %s\n", compactSubAgentFileList(result.FilesModified, 8)))
		}
		if len(result.FilesCreated) > 0 {
			b.WriteString(fmt.Sprintf("Files created: %s\n", compactSubAgentFileList(result.FilesCreated, 8)))
		}
		if result.DiffStat != nil {
			b.WriteString(fmt.Sprintf("Diff: %s\n", result.DiffStat.Summary()))
		}
		if result.VerificationSummary != "" {
			b.WriteString(fmt.Sprintf("Verification: %s\n", truncateRunesForSubAgent(result.VerificationSummary, 240)))
		}
		if result.QualitySummary != "" {
			b.WriteString(fmt.Sprintf("Quality: %s\n", truncateRunesForSubAgent(result.QualitySummary, 240)))
		}
		if result.Localization != nil {
			loc := result.Localization
			if loc.RootCauseFile != "" || loc.RootCauseSymbol != "" {
				b.WriteString(fmt.Sprintf("Root cause: %s %s\n",
					truncateExtractedExperienceField(loc.RootCauseFile, 240),
					truncateExtractedExperienceField(loc.RootCauseSymbol, 160)))
			}
			if errors := compactExperiencePromptList(loc.Signal.ErrorStrings, 3, 160); len(errors) > 0 {
				b.WriteString("Error strings: " + strings.Join(errors, "; ") + "\n")
			}
			if rejected := compactExperiencePromptList(loc.RejectedHypotheses, 3, 160); len(rejected) > 0 {
				b.WriteString("Rejected hypotheses: " + strings.Join(rejected, "; ") + "\n")
			}
		}
		if len(result.CommandsRun) > 0 {
			b.WriteString("\n## Commands\n")
			shown := result.CommandsRun
			if len(shown) > 8 {
				shown = shown[:8]
			}
			for _, cmd := range shown {
				mark := "ok"
				if !cmd.Succeeded {
					mark = "FAIL"
				}
				line := fmt.Sprintf("- [%s] %s", mark, compactSubAgentCommandText(cmd.Command))
				// A failing verification's error line is the most reusable
				// pitfall signal; include a bounded excerpt for FAIL entries.
				if !cmd.Succeeded {
					if summary := compactCodingFailureSummary(cmd.Summary, 160); summary != "" {
						line += " — " + summary
					}
				}
				b.WriteString(line + "\n")
			}
		}
	}

	b.WriteString("\n## Output format\nReturn JSON (no markdown code blocks):\n")
	b.WriteString(`{"experiences": [{"title": "...", "category": "pattern|decision|pitfall|convention", "scope": "universal|language|project", "trigger_condition": "...", "content": "...", "code_snippet": "...", "failed_attempts": ["..."], "contraindications": ["..."], "frameworks": ["..."], "labels": ["..."], "project_specific": false}]}`)
	b.WriteString("\n\nIf nothing worth recording: {\"experiences\": []}\n")

	return b.String()
}

func (r *SubAgentTaskRunner) callLLMForExperienceExtraction(ctx context.Context, prompt string) ([]knowledge.CodingExperience, error) {
	cfg := r.cfg
	if cfg.URL == "" {
		return nil, fmt.Errorf("LLM not configured")
	}

	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}

	ctx = llm.WithRequestTraceIfMissing(ctx, "coding-experience-extract")
	resp, err := doExperienceLLMRequest(ctx, cfg, messages, r.httpClient)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	text := strings.TrimSpace(resp)
	if text == "" {
		return nil, nil
	}
	return parseExtractedExperienceResponse(text)
}

// parseExtractedExperienceResponse is split out from the LLM call so the
// structured-field mapping (failed attempts, contraindications, frameworks)
// is directly testable.
func parseExtractedExperienceResponse(text string) ([]knowledge.CodingExperience, error) {
	text = stripMarkdownCodeBlock(strings.TrimSpace(text))
	if text == "" {
		return nil, nil
	}

	var response extractedExperienceResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		if idx := strings.Index(text, `{"experiences"`); idx >= 0 {
			text = text[idx:]
			if endIdx := strings.LastIndex(text, "}"); endIdx >= 0 {
				text = text[:endIdx+1]
			}
			if err2 := json.Unmarshal([]byte(text), &response); err2 != nil {
				return nil, fmt.Errorf("parse response: %w", err)
			}
		} else {
			return nil, fmt.Errorf("parse response: %w", err)
		}
	}

	if len(response.Experiences) == 0 {
		return nil, nil
	}

	experiences := make([]knowledge.CodingExperience, 0, experienceMaxPerExtraction)
	for i, item := range response.Experiences {
		if i >= experienceMaxPerExtraction {
			break
		}
		if strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		exp := knowledge.CodingExperience{
			Title:             truncateExtractedExperienceField(item.Title, maxExtractedExperienceTitleRunes),
			Category:          normalizeCodingCategory(item.Category),
			TriggerCondition:  truncateExtractedExperienceField(item.TriggerCondition, maxExtractedExperienceTriggerRunes),
			Content:           truncateExtractedExperienceField(item.Content, maxExtractedExperienceContentRunes),
			CodeSnippet:       truncateExtractedExperienceField(item.CodeSnippet, maxExtractedExperienceCodeSnippetRunes),
			FailedAttempts:    boundExtractedExperienceStrings(item.FailedAttempts, maxExtractedExperienceListItems, maxExtractedExperienceListItemRunes),
			Contraindications: boundExtractedExperienceStrings(item.Contraindications, maxExtractedExperienceListItems, maxExtractedExperienceListItemRunes),
			Frameworks:        boundExtractedExperienceStrings(item.Frameworks, maxExtractedExperienceFrameworks, maxExtractedExperienceListItemRunes),
			Labels:            boundExtractedExperienceStrings(item.Labels, maxExtractedExperienceLabels, maxExtractedExperienceListItemRunes),
		}
		switch strings.ToLower(strings.TrimSpace(item.Scope)) {
		case knowledge.CodingScopeUniversal, knowledge.CodingScopeLanguage, knowledge.CodingScopeProject:
			exp.Scope = strings.ToLower(strings.TrimSpace(item.Scope))
		default:
			exp.Scope = ""
		}
		if item.ProjectSpecific {
			exp.Scope = knowledge.CodingScopeProject
		}
		experiences = append(experiences, exp)
	}

	return experiences, nil
}

func truncateExtractedExperienceField(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func boundExtractedExperienceStrings(items []string, maxItems, maxRunes int) []string {
	if maxItems <= 0 || len(items) == 0 {
		return nil
	}
	capacity := len(items)
	if capacity > maxItems {
		capacity = maxItems
	}
	out := make([]string, 0, capacity)
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = truncateExtractedExperienceField(item, maxRunes)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Dedup check
// ---------------------------------------------------------------------------

// findDuplicateExperience returns the stored experience that the new
// extraction duplicates. Scope is part of identity: a project rule in project
// A must not suppress (or later merge evidence with) a rule in project B, and
// a language rule must not merge with another language's rule.
func findDuplicateExperience(ctx context.Context, store *knowledge.CodingKnowledgeStore, exp knowledge.CodingExperience) (knowledge.CodingExperience, bool) {
	if store == nil {
		return knowledge.CodingExperience{}, false
	}
	queries := codingExperienceDedupQueries(exp)
	if len(queries) == 0 {
		return knowledge.CodingExperience{}, false
	}
	searchOpts := knowledge.CodingSearchOptions{
		Scope:  exp.Scope,
		Limit:  3,
		Status: []string{knowledge.CodingStatusCandidate, knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
	}
	switch exp.Scope {
	case knowledge.CodingScopeProject:
		searchOpts.ProjectPath = strings.TrimSpace(exp.ProjectPath)
	case knowledge.CodingScopeLanguage:
		searchOpts.Language = strings.TrimSpace(exp.Language)
	}
	var candidateMatch *knowledge.CodingExperience
	for _, query := range queries {
		opts := searchOpts
		opts.Query = query
		results, err := store.SearchExperiences(ctx, opts)
		if err != nil || len(results) == 0 {
			continue
		}
		for _, existing := range results {
			if existing.Scope != exp.Scope {
				continue
			}
			if exp.Scope == knowledge.CodingScopeProject && !knowledge.CodingProjectPathsEqual(existing.ProjectPath, exp.ProjectPath) {
				continue
			}
			if exp.Scope == knowledge.CodingScopeLanguage && existing.Language != exp.Language {
				continue
			}
			if !isSimilarExperience(existing, exp) {
				continue
			}
			// An already reviewed version is the authoritative rule. Do not merge
			// evidence into a pending duplicate or promote a second active copy.
			if existing.Status == knowledge.CodingStatusActive || existing.Status == knowledge.CodingStatusVerified {
				return existing, true
			}
			if existing.Status == knowledge.CodingStatusCandidate && candidateMatch == nil {
				candidate := existing
				candidateMatch = &candidate
			}
		}
		// A candidate is already a valid merge target; do not pay for a second
		// content query just to look for an active copy.
		if candidateMatch != nil {
			return *candidateMatch, true
		}
	}
	return knowledge.CodingExperience{}, false
}

// codingExperienceDedupQueries provides a second, content-based lookup. Title
// and trigger wording varies between extractions of the same guidance, while
// the body usually stays recognizable.
func codingExperienceDedupQueries(exp knowledge.CodingExperience) []string {
	queries := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	add := func(query string) {
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		if _, ok := seen[query]; ok {
			return
		}
		seen[query] = struct{}{}
		queries = append(queries, query)
	}
	add(exp.TriggerCondition + " " + exp.Title)
	content := strings.TrimSpace(exp.Content)
	if runes := []rune(content); len(runes) > 80 {
		content = string(runes[:80])
	}
	// Tiny boilerplate bodies would only add noise; the primary title/trigger
	// query already covers them.
	if len([]rune(content)) >= 20 {
		add(content)
	}
	return queries
}

// recordDuplicateExperienceObservation turns a repeated sighting into durable
// evidence. Only an automatic runtime candidate can be merged; manually
// authored, imported, revised, active or verified guidance is left untouched.
// In auto mode the second independent observation may promote the candidate.
func (r *SubAgentTaskRunner) recordDuplicateExperienceObservation(
	ctx context.Context,
	store *knowledge.CodingKnowledgeStore,
	existing knowledge.CodingExperience,
	incoming knowledge.CodingExperience,
	provenance codingExperienceProvenance,
	mode ExperienceSaveMode,
	appCfg corelib.AppConfig,
) {
	if store == nil || r == nil || r.handler == nil || r.handler.app == nil {
		return
	}
	if existing.Status != knowledge.CodingStatusCandidate ||
		existing.CreatedBy != "runtime" ||
		strings.TrimSpace(existing.SourceRuntimeTaskID) == "" ||
		strings.TrimSpace(existing.SourceRuntimeAttemptID) == "" ||
		strings.TrimSpace(existing.EvidenceDigest) == "" {
		log.Printf("[coding-experience] skipped duplicate: %s (status=%s origin=%s)", incoming.Title, existing.Status, existing.CreatedBy)
		return
	}
	// Horizon rounds and bug-localization records have their own provenance and
	// review semantics. A generic extraction must not merge observations into
	// them or promote them through this path.
	if strings.HasPrefix(strings.TrimSpace(existing.SourceRuntimeTaskID), horizonExperienceProvenancePrefix) {
		log.Printf("[coding-experience] skipped duplicate into horizon record: %s", incoming.Title)
		return
	}
	for _, label := range existing.Labels {
		if strings.EqualFold(strings.TrimSpace(label), "bug-localization") {
			log.Printf("[coding-experience] skipped duplicate into localization record: %s", incoming.Title)
			return
		}
	}
	updated, err := store.RecordCandidateObservation(ctx, existing.ID, knowledge.CodingExperienceObservation{
		RuntimeTaskID:     provenance.TaskID,
		RuntimeAttemptID:  provenance.AttemptID,
		EvidenceDigest:    provenance.EvidenceDigest,
		FailedAttempts:    incoming.FailedAttempts,
		Contraindications: incoming.Contraindications,
		Frameworks:        incoming.Frameworks,
		Labels:            incoming.Labels,
		SourceTaskTitle:   incoming.SourceTaskTitle,
	}, r.handler.app.verifyRuntimeCodingExperience)
	if err != nil {
		log.Printf("[coding-experience] record observation failed for %q: %v", existing.Title, err)
		return
	}
	log.Printf("[coding-experience] merged observation: %s (id=%s, observations=%d)", updated.Title, updated.ID, updated.ObservationCount)
	if mode != ExperienceSaveModeAuto {
		return
	}
	threshold := resolveCodingKnowledgeAutoPromoteObservations(appCfg)
	if updated.ObservationCount < threshold {
		return
	}
	promoted, err := store.PromoteCandidateFromObservations(
		ctx,
		updated.ID,
		threshold,
		codingKnowledgeReviewedProjectBudget(appCfg),
		r.handler.app.verifyRuntimeCodingExperience,
	)
	if err != nil {
		log.Printf("[coding-experience] auto promotion skipped for %q: %v", updated.Title, err)
		return
	}
	if promoted {
		log.Printf("[coding-experience] auto promoted candidate: %s (id=%s, observations=%d, threshold=%d)", updated.Title, updated.ID, updated.ObservationCount, threshold)
	}
}

func isSimilarExperience(existing, newExp knowledge.CodingExperience) bool {
	existTitle := strings.ToLower(strings.TrimSpace(existing.Title))
	newTitle := strings.ToLower(strings.TrimSpace(newExp.Title))
	if existTitle == newTitle {
		return true
	}
	if len(existTitle) > 10 && len(newTitle) > 10 {
		if strings.Contains(existTitle, newTitle) || strings.Contains(newTitle, existTitle) {
			return true
		}
	}
	existTrigger := normalizeCodingExperienceTrigger(existing.TriggerCondition)
	newTrigger := normalizeCodingExperienceTrigger(newExp.TriggerCondition)
	if existTrigger != "" && existTrigger == newTrigger {
		return true
	}
	existContent := strings.ToLower(strings.TrimSpace(existing.Content))
	newContent := strings.ToLower(strings.TrimSpace(newExp.Content))
	if existContent != "" && newContent != "" && existContent == newContent {
		return true
	}
	if codingExperienceContentNearDuplicate(existContent, newContent) {
		return true
	}
	return false
}

// normalizeCodingExperienceTrigger canonicalizes a short recall trigger so
// "HTTP client, reuse" and "reuse http-client" are the same identity. Title and
// body wording drift between extractions; the trigger is the stable key.
func normalizeCodingExperienceTrigger(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(fields) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(fields))
	unique := fields[:0]
	for _, field := range fields {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		unique = append(unique, field)
	}
	sort.Strings(unique)
	return strings.Join(unique, " ")
}

func codingExperienceContentNearDuplicate(existing, incoming string) bool {
	if existing == "" || incoming == "" {
		return false
	}
	shorter, longer := existing, incoming
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if len(shorter) < 40 {
		return false
	}
	if len(shorter)*2 < len(longer) {
		return false
	}
	return strings.Contains(longer, shorter)
}

func codingExperienceShouldExtract(mode ExperienceSaveMode, strategy ExperienceSaveStrategy, result *CodingSubAgentResult, wasRetry bool) bool {
	if mode == ExperienceSaveModeOff || strategy == ExperienceStrategyOff {
		return false
	}
	if result == nil || result.HorizonOwned {
		return false
	}
	// A pause for user input is not a completed task and its summary is a
	// question, not reusable guidance.
	if strings.TrimSpace(result.AskQuestion) != "" {
		return false
	}
	taskPassed := result.Status == TaskExecPassed
	switch strategy {
	case ExperienceStrategyOnRetry:
		return wasRetry && taskPassed
	case ExperienceStrategyOnSuccess:
		return taskPassed
	case ExperienceStrategyAlways:
		return true
	default:
		return false
	}
}

func codingResultLooksLikeRetrySuccess(result *CodingSubAgentResult) bool {
	if result == nil {
		return false
	}
	failed := map[string]struct{}{}
	for _, cmd := range result.CommandsRun {
		token := codingCommandFirstToken(cmd.Command)
		if token == "" {
			continue
		}
		if !cmd.Succeeded {
			failed[token] = struct{}{}
			continue
		}
		if _, ok := failed[token]; ok {
			return true
		}
	}
	return false
}

func codingCommandFirstToken(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func isBenignCodingRecallOutcomeError(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "already recorded") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "requires a reviewed") ||
		strings.Contains(msg, "deprecated experience")
}

func finalizeRemoteCodingKnowledge(agent *RemoteCodingSubAgent, taskDescription string, result *RemoteCodingSubAgentResult, readOnlyInquiry bool) {
	if agent == nil || result == nil || agent.handler == nil || agent.handler.app == nil {
		return
	}
	localResult := remoteResultAsCodingSubAgentResult(result)
	runner := &SubAgentTaskRunner{
		handler:      agent.handler,
		cfg:          agent.cfg,
		httpClient:   agent.httpClient,
		orchestrator: &TaskExecutionOrchestrator{ProjectPath: agent.projectDir},
	}
	files := append(append([]string{}, result.FilesModified...), result.FilesCreated...)
	task := &TaskItem{Title: strings.TrimSpace(taskDescription), Files: files}
	wasRetry := codingResultLooksLikeRetrySuccess(localResult)
	if strings.EqualFold(result.Status, "success") || strings.EqualFold(result.Status, "failed") {
		runner.recordRecalledExperienceOutcomes(localResult)
		// A read-only inquiry consumes knowledge but its answer is not
		// reusable execution experience.
		if !readOnlyInquiry {
			runner.extractAndSaveExperience(task, localResult, wasRetry)
		}
	}
}

func remoteResultAsCodingSubAgentResult(result *RemoteCodingSubAgentResult) *CodingSubAgentResult {
	if result == nil {
		return nil
	}
	status := TaskExecFailed
	if strings.EqualFold(result.Status, "success") {
		status = TaskExecPassed
	}
	return &CodingSubAgentResult{
		Status:                status,
		Summary:               result.Summary,
		Error:                 result.Error,
		RuntimeTaskID:         result.RuntimeTaskID,
		FilesModified:         result.FilesModified,
		FilesCreated:          result.FilesCreated,
		CommandsRun:           result.CommandsRun,
		QualitySummary:        result.QualitySummary,
		VerificationSummary:   result.VerificationSummary,
		RecalledExperienceIDs: result.RecalledExperienceIDs,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// compactCodingFailureSummary prefers the lines that actually name a failure
// (error/panic/failed test/traceback) over command headers. This gives the
// extractor the reusable pitfall instead of boilerplate output.
func compactCodingFailureSummary(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	markers := []string{
		"error", "fail", "panic", "undefined", "exception", "traceback",
		"cannot", "not found", "fatal", "exit status", "assert",
		"expected", "received", "mismatch", "got ", "want ",
		"timed out", "timeout", "segmentation fault", "nil pointer", "null pointer",
		"permission denied", "no such file", "connection refused", "unable to",
		"-->",
	}
	lines := strings.Split(text, "\n")
	picked := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				picked = append(picked, truncateRunesForSubAgent(line, 160))
				break
			}
		}
		if len(picked) >= 3 {
			break
		}
	}
	if len(picked) == 0 {
		return truncateRunesForSubAgent(text, maxRunes)
	}
	return truncateRunesForSubAgent(strings.Join(picked, " | "), maxRunes)
}

func compactExperiencePromptList(items []string, maxItems, maxRunes int) []string {
	if maxItems <= 0 || len(items) == 0 {
		return nil
	}
	out := make([]string, 0, maxItems)
	for _, item := range items {
		item = truncateRunesForSubAgent(strings.TrimSpace(item), maxRunes)
		if item == "" {
			continue
		}
		out = append(out, item)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func normalizeCodingCategory(cat string) string {
	switch strings.TrimSpace(strings.ToLower(cat)) {
	case "pattern":
		return knowledge.CodingCategoryPattern
	case "decision":
		return knowledge.CodingCategoryDecision
	case "pitfall":
		return knowledge.CodingCategoryPitfall
	case "convention":
		return knowledge.CodingCategoryConvention
	default:
		return knowledge.CodingCategoryPattern
	}
}

func stripMarkdownCodeBlock(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
	}
	return strings.TrimSpace(text)
}

// doExperienceLLMRequest performs a non-streaming LLM request for experience extraction.
func doExperienceLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []map[string]string, httpClient *http.Client) (string, error) {
	if cfg.URL == "" {
		return "", fmt.Errorf("LLM URL not configured")
	}
	ifaces := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		ifaces = append(ifaces, m)
	}
	resp, err := doSimpleLLMRequest(ctx, attachLightweightHubHint(cfg, llm.TaskSummary), ifaces, httpClient, experienceExtractionTimeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}
