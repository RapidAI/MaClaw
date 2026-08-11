package main

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
	"strings"
	"time"

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

func (r *SubAgentTaskRunner) extractAndSaveExperience(
	task *TaskItem,
	result *CodingSubAgentResult,
	wasRetry bool,
) {
	if r == nil || r.handler == nil || r.handler.app == nil {
		return
	}
	codingKB := r.codingKnowledgeStore()
	if codingKB == nil {
		return
	}
	appCfg, err := r.handler.app.LoadConfig()
	if err != nil {
		return
	}
	mode := ExperienceSaveMode(appCfg.CodingKnowledgeAutoSaveMode)
	strategy := ExperienceSaveStrategy(appCfg.CodingKnowledgeSaveStrategy)
	// Apply defaults for empty config values
	if mode == "" {
		mode = ExperienceSaveModeObserve
	}
	if strategy == "" {
		strategy = ExperienceStrategyOnRetry
	}
	if mode == ExperienceSaveModeOff || strategy == ExperienceStrategyOff {
		return
	}

	taskPassed := result != nil && result.Status == TaskExecPassed
	switch strategy {
	case ExperienceStrategyOnRetry:
		if !wasRetry || !taskPassed {
			return
		}
	case ExperienceStrategyOnSuccess:
		if !taskPassed {
			return
		}
	case ExperienceStrategyAlways:
		// extract for both success and failure
	default:
		return
	}

	// Extraction output is always a candidate. `auto` retains its historical
	// meaning of automatically extracting, not automatically injecting a new
	// LLM-derived rule into later coding prompts.
	_ = mode
	go r.doExtractAndSave(task, result, knowledge.CodingStatusCandidate)
}

func (r *SubAgentTaskRunner) doExtractAndSave(
	task *TaskItem,
	result *CodingSubAgentResult,
	initialStatus string,
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

	prompt := buildExperienceExtractionPrompt(task, result, r.orchestrator.ProjectPath)

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

	projectPath := r.orchestrator.ProjectPath
	language := inferLanguageFromTaskFiles(task.Files)
	provenance, provenanceErr := r.runtimeExperienceProvenance(result.RuntimeTaskID)
	if provenanceErr != nil {
		log.Printf("[coding-experience] skip runtime-derived extraction without provenance: %v", provenanceErr)
		return
	}

	for _, exp := range extracted {
		exp.SourceTaskTitle = task.Title
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
		if exp.Scope == "" {
			if language != "" {
				exp.Scope = knowledge.CodingScopeLanguage
				exp.Language = language
			} else {
				exp.Scope = knowledge.CodingScopeUniversal
			}
		}

		if isDuplicateExperience(saveCtx, codingKB, exp) {
			log.Printf("[coding-experience] skipped duplicate: %s", exp.Title)
			continue
		}

		saved, saveErr := codingKB.SaveRuntimeExperience(saveCtx, exp)
		if saveErr != nil {
			log.Printf("[coding-experience] save failed for %q: %v", exp.Title, saveErr)
			continue
		}
		log.Printf("[coding-experience] saved: %s (id=%s, scope=%s, status=%s)", saved.Title, saved.ID, saved.Scope, saved.Status)
	}

	// Trigger eviction check after saving (best-effort, don't block)
	if r.handler != nil && r.handler.app != nil {
		if evicted, err := r.handler.app.CodingKnowledgeEvict(); err == nil && evicted > 0 {
			log.Printf("[coding-experience] evicted %d experiences after save", evicted)
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
	return r.handler.app.codingKnowledgeStore
}

// ---------------------------------------------------------------------------
// LLM-based experience extraction
// ---------------------------------------------------------------------------

type extractedExperienceResponse struct {
	Experiences []extractedExperienceItem `json:"experiences"`
}

type extractedExperienceItem struct {
	Title            string   `json:"title"`
	Category         string   `json:"category"`
	Scope            string   `json:"scope,omitempty"`
	TriggerCondition string   `json:"trigger_condition"`
	Content          string   `json:"content"`
	CodeSnippet      string   `json:"code_snippet,omitempty"`
	FailedAttempts   []string `json:"failed_attempts,omitempty"`
	Labels           []string `json:"labels,omitempty"`
	ProjectSpecific  bool     `json:"project_specific,omitempty"`
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
	b.WriteString("- Record failed_attempts if applicable\n\n")

	b.WriteString(fmt.Sprintf("## Task\nTitle: %s\n", task.Title))
	if task.Description != "" {
		desc := task.Description
		if len([]rune(desc)) > 300 {
			desc = string([]rune(desc)[:300]) + "..."
		}
		b.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}
	if len(task.Files) > 0 {
		b.WriteString(fmt.Sprintf("Files: %s\n", strings.Join(task.Files, ", ")))
	}
	b.WriteString(fmt.Sprintf("Project: %s\n", projectPath))

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
			b.WriteString(fmt.Sprintf("\nError: %s\n", result.Error))
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
				b.WriteString(fmt.Sprintf("- [%s] %s\n", mark, compactSubAgentCommandText(cmd.Command)))
			}
		}
	}

	b.WriteString("\n## Output format\nReturn JSON (no markdown code blocks):\n")
	b.WriteString(`{"experiences": [{"title": "...", "category": "pattern|decision|pitfall|convention", "scope": "universal|language|project", "trigger_condition": "...", "content": "...", "code_snippet": "...", "failed_attempts": ["..."], "labels": ["..."], "project_specific": false}]}`)
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

	text = stripMarkdownCodeBlock(text)

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
			Title:            strings.TrimSpace(item.Title),
			Category:         normalizeCodingCategory(item.Category),
			TriggerCondition: strings.TrimSpace(item.TriggerCondition),
			Content:          strings.TrimSpace(item.Content),
			CodeSnippet:      strings.TrimSpace(item.CodeSnippet),
			FailedAttempts:   item.FailedAttempts,
			Labels:           item.Labels,
		}
		if item.ProjectSpecific {
			exp.Scope = knowledge.CodingScopeProject
		} else if item.Scope != "" {
			exp.Scope = item.Scope
		}
		experiences = append(experiences, exp)
	}

	return experiences, nil
}

// ---------------------------------------------------------------------------
// Dedup check
// ---------------------------------------------------------------------------

func isDuplicateExperience(ctx context.Context, store *knowledge.CodingKnowledgeStore, exp knowledge.CodingExperience) bool {
	if store == nil || exp.TriggerCondition == "" {
		return false
	}
	results, err := store.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:  exp.TriggerCondition + " " + exp.Title,
		Limit:  3,
		Status: []string{knowledge.CodingStatusCandidate, knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
	})
	if err != nil || len(results) == 0 {
		return false
	}
	for _, existing := range results {
		if isSimilarExperience(existing, exp) {
			return true
		}
	}
	return false
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
	existTrigger := strings.ToLower(strings.TrimSpace(existing.TriggerCondition))
	newTrigger := strings.ToLower(strings.TrimSpace(newExp.TriggerCondition))
	if existTrigger != "" && newTrigger != "" && existTrigger == newTrigger {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
	resp, err := doSimpleLLMRequest(ctx, cfg, ifaces, httpClient, experienceExtractionTimeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}
