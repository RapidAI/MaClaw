package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// workflowFormRecallProvider implements v2.RecallProvider by delegating to
// memory.Store.RecallDynamic() and knowledge.SQLiteStore.Search().
// It uses the full retrieval pipeline (BM25 + embedding + graph expand for memory;
// FTS + vector + entity graph + fact triples for knowledge).
type workflowFormRecallProvider struct {
	memStore       *memory.Store
	knowledgeStore knowledgeSearcher // interface to decouple from concrete knowledge.SQLiteStore
	userID         string
	projectPath    string
}

// knowledgeSearcher abstracts knowledge base search for testability.
type knowledgeSearcher interface {
	Search(ctx context.Context, opts knowledge.SearchOptions) ([]knowledge.SearchResult, error)
}

func (p *workflowFormRecallProvider) RecallForField(ctx context.Context, query string, maxResults int) []v2.RecallResult {
	if query == "" {
		return nil
	}

	var results []v2.RecallResult

	// 1. Recall from memory (BM25 + embedding + graph expand + temporal scoring)
	if p.memStore != nil {
		ownerArgs := []string{}
		if p.userID != "" {
			ownerArgs = append(ownerArgs, p.userID)
		}
		entries := p.memStore.RecallDynamic(query, "", p.projectPath, ownerArgs...)
		for _, e := range entries {
			if len(results) >= maxResults*2 { // collect more, rank later
				break
			}
			results = append(results, v2.RecallResult{
				Content:    e.Content,
				Category:   string(e.Category),
				Source:     "memory",
				SourceID:   e.ID,
				Score:      0.8, // RecallDynamic doesn't expose scores; use category-based confidence later
				SourceDesc: memorySourceDesc(e),
			})
		}
	}

	// 2. Search knowledge base (FTS + vector + entity graph + fact triples)
	if p.knowledgeStore != nil {
		kResults, err := p.knowledgeStore.Search(ctx, knowledge.SearchOptions{
			Query:   query,
			OwnerID: p.userID,
			Limit:   maxResults,
		})
		if err == nil {
			for _, r := range kResults {
				snippet := r.Snippet
				if snippet == "" {
					snippet = r.Summary
				}
				if snippet == "" {
					continue
				}
				results = append(results, v2.RecallResult{
					Content:    snippet,
					Category:   "knowledge_" + r.ResultType,
					Source:     "knowledge",
					SourceID:   r.Source.ID,
					Score:      r.Score,
					SourceDesc: knowledgeSourceDesc(r),
				})
			}
		}
	}

	// Limit to maxResults (memory + knowledge combined, keep interleaved order)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func memorySourceDesc(e memory.Entry) string {
	cat := string(e.Category)
	switch e.Category {
	case memory.CategoryUserFact:
		return "来自记忆(用户事实)"
	case memory.CategoryProjectKnowledge:
		return "来自记忆(项目知识)"
	case memory.CategoryTaskArtifact:
		return "来自记忆(任务产出)"
	default:
		return "来自记忆(" + cat + ")"
	}
}

func knowledgeSourceDesc(r knowledge.SearchResult) string {
	title := r.Source.Title
	if title == "" {
		title = r.Source.URI
	}
	if title != "" {
		return "来自知识库: " + title
	}
	return "来自知识库"
}

// prefillWorkflowFormFields collects prefill data from context + memory + knowledge.
// Called before emitting the AG UI form to frontend.
// Returns nil if no fields could be prefilled.
func (h *IMMessageHandler) prefillWorkflowFormFields(userID string, phase *v2.Phase, userMessage string) map[string]*v2.PrefilledValue {
	if phase == nil || phase.InputSchema == nil || len(phase.InputSchema.Fields) == 0 {
		return nil
	}

	// Phase 1: Extract from dialogue context (instant, < 5ms)
	contextTexts := h.getRecentConversationTextsForPrefill(userID, 10)
	result := v2.PrefillFromContext(phase.InputSchema, userMessage, contextTexts)

	// Phase 2: Recall from memory + knowledge base (< 100ms per field)
	provider := h.buildRecallProvider(userID)
	if provider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result = v2.PrefillFromRecall(ctx, phase.InputSchema, result, provider)
	}

	if len(result) > 0 {
		log.Printf("[workflow-v2-prefill] prefilled %d/%d fields for phase=%s",
			len(result), len(phase.InputSchema.Fields), phase.ID)
	}
	return result
}

// buildRecallProvider constructs the recall provider with available memory and knowledge stores.
func (h *IMMessageHandler) buildRecallProvider(userID string) v2.RecallProvider {
	if h == nil || h.app == nil {
		return nil
	}

	var memStore *memory.Store
	if h.memoryStore != nil {
		memStore = h.memoryStore
	}

	var ks knowledgeSearcher
	// Open knowledge store (may fail if not configured — that's fine, we just skip)
	if store, err := h.app.openKnowledgeStore(); err == nil && store != nil {
		ks = store
		// Note: SQLiteStore implements knowledgeSearcher via its Search method.
		// The store is opened per-call; connection pooling is handled internally by SQLite.
	}

	if memStore == nil && ks == nil {
		return nil
	}

	projectPath := ""
	if h.app != nil {
		projectPath = h.app.GetCurrentProjectPath()
	}

	return &workflowFormRecallProvider{
		memStore:       memStore,
		knowledgeStore: ks,
		userID:         userID,
		projectPath:    projectPath,
	}
}

// getRecentConversationTextsForPrefill returns recent user+assistant texts for context extraction.
func (h *IMMessageHandler) getRecentConversationTextsForPrefill(userID string, maxTurns int) []string {
	if h == nil || h.memory == nil {
		return nil
	}
	entries := h.memory.Load(userID)
	if len(entries) == 0 {
		return nil
	}

	var texts []string
	count := 0
	// Walk backwards from most recent
	for i := len(entries) - 1; i >= 0 && count < maxTurns; i-- {
		e := entries[i]
		if e.Role == "user" || e.Role == "assistant" {
			text := ""
			if s, ok := e.Content.(string); ok {
				text = strings.TrimSpace(s)
			}
			if text != "" && len([]rune(text)) < 500 { // skip very long entries
				texts = append(texts, text)
				count++
			}
		}
	}
	return texts
}


// --- Phase 4: Post-submission memory sedimentation ---

// sedimentableFields are field names whose values represent stable user facts
// (not task-specific) and should be persisted to memory for future prefill reuse.
var sedimentableFields = map[string]bool{
	"name": true, "gender": true, "birth_date": true,
	"institution": true, "title": true, "nationality": true,
	"discipline_code": true, "research_field": true,
	"h_index": true, "total_citations": true, "total_papers": true,
	"phd_year": true, "degree": true,
	"organization": true,
}

// sedimentFormDataToMemory persists confirmed form field values to long-term memory
// so future workflows can auto-prefill from memory recall (Phase 2).
// Only sedimentable fields (stable user facts) are saved — task-specific creative
// content is never persisted.
//
// Called asynchronously after successful form submission.
func (h *IMMessageHandler) sedimentFormDataToMemory(userID, phaseID string, data map[string]interface{}, state *v2.WorkflowState) {
	if h == nil || h.memoryStore == nil || len(data) == 0 {
		return
	}

	// Find the phase's InputSchema to get field labels
	var schema *v2.PhaseInputSchema
	if state != nil {
		if phase := state.ActivePhase(); phase != nil && phase.InputSchema != nil {
			schema = phase.InputSchema
		}
	}

	var sedimentLines []string
	for fieldName, value := range data {
		if !sedimentableFields[fieldName] {
			continue
		}
		strValue := ""
		switch v := value.(type) {
		case string:
			strValue = strings.TrimSpace(v)
		default:
			continue
		}
		if strValue == "" {
			continue
		}

		// Get the label for a more readable memory entry
		label := fieldName
		if schema != nil {
			for _, f := range schema.Fields {
				if f.Name == fieldName {
					label = f.Label
					break
				}
			}
		}
		sedimentLines = append(sedimentLines, label+"："+strValue)
	}

	if len(sedimentLines) == 0 {
		return
	}

	// Build a composite memory entry with all sedimentable fields
	content := strings.Join(sedimentLines, "\n")

	// Add workflow type as context tag
	tags := []string{"form_data", "workflow_prefill"}
	if state != nil {
		tags = append(tags, state.Type)
	}

	entry := memory.Entry{
		Content:  content,
		Category: memory.CategoryUserFact,
		Tags:     tags,
		OwnerID:  userID,
	}

	if err := h.memoryStore.Save(entry); err != nil {
		log.Printf("[workflow-v2-prefill] sediment form data failed: %v", err)
	} else {
		log.Printf("[workflow-v2-prefill] sedimented %d fields to memory for user=%s phase=%s",
			len(sedimentLines), userID, phaseID)
	}
}
