package main

import (
	"context"
	"fmt"
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
		for i, e := range entries {
			if len(results) >= maxResults*2 { // collect more, rank later
				break
			}
			// RecallDynamic returns results pre-sorted by relevance.
			// Approximate a score from rank position: top result gets 0.95, decays linearly.
			rankScore := 0.95 - float64(i)*0.08
			if rankScore < 0.50 {
				rankScore = 0.50
			}
			results = append(results, v2.RecallResult{
				Content:    e.Content,
				Category:   string(e.Category),
				Source:     "memory",
				SourceID:   e.ID,
				Score:      rankScore,
				SourceDesc: memorySourceDesc(e),
			})
		}
	}

	// 2. Search knowledge base — fact-first strategy.
	// Facts (structured triples like "张三 职称 教授") are the highest-precision
	// source for form prefill. Cards (paragraph summaries) are lower precision
	// but broader coverage. Query facts first, then supplement with cards.
	if p.knowledgeStore != nil {
		// 2a. Facts first (structured triples — ideal for field extraction)
		factResults, err := p.knowledgeStore.Search(ctx, knowledge.SearchOptions{
			Query:       query,
			OwnerID:     p.userID,
			ResultTypes: []string{"fact"},
			Limit:       maxResults,
		})
		if err == nil {
			for _, r := range factResults {
				snippet := r.Snippet
				if snippet == "" {
					snippet = strings.TrimSpace(r.Subject + " " + r.Predicate + " " + r.Object)
				}
				if snippet == "" {
					continue
				}
				results = append(results, v2.RecallResult{
					Content:    snippet,
					Category:   "knowledge_fact",
					Source:     "knowledge",
					SourceID:   r.Source.ID,
					Score:      r.Score + 0.1, // slight boost over cards at same score
					SourceDesc: knowledgeSourceDesc(r),
				})
			}
		}

		// 2b. Cards/nodes (paragraph summaries — broader coverage for textarea fields)
		remaining := maxResults - len(results)
		if remaining > 0 {
			cardResults, err := p.knowledgeStore.Search(ctx, knowledge.SearchOptions{
				Query:   query,
				OwnerID: p.userID,
				Limit:   remaining + 2, // fetch a few extra to account for empty snippets
			})
			if err == nil {
				for _, r := range cardResults {
					if len(results) >= maxResults*2 {
						break
					}
					snippet := r.Snippet
					if snippet == "" {
						snippet = r.Summary
					}
					if snippet == "" {
						continue
					}
					// Skip if we already have this source+content from the fact query
					if r.ResultType == "fact" {
						continue // already included above
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
	if phase == nil || phase.InputSchema == nil {
		return nil
	}
	// Check if schema has any fields at all (including inside Variants).
	// Academic templates put fields inside Variants, leaving top-level Fields empty.
	if len(phase.InputSchema.Fields) == 0 && len(phase.InputSchema.Variants) == 0 {
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
		totalFields := len(phase.InputSchema.Fields)
		for _, v := range phase.InputSchema.Variants {
			totalFields += len(v.Fields)
		}
		log.Printf("[workflow-v2-prefill] prefilled %d/%d fields for phase=%s",
			len(result), totalFields, phase.ID)
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
	} else if err != nil {
		log.Printf("[workflow-v2-prefill] knowledge store unavailable (recall will use memory only): %v", err)
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

// sedimentableFields is a legacy hardcoded whitelist kept for backward compatibility
// with templates that have not yet been annotated with Reusable=true on their fields.
// For annotated templates (SchemaHasReusableFields=true), the field's Reusable flag
// is the single source of truth — this map is not consulted.
//
// Deprecated: annotate template fields with Reusable:true instead of adding names here.
var sedimentableFields = map[string]bool{
	"name": true, "gender": true, "birth_date": true,
	"institution": true, "title": true, "nationality": true,
	"discipline": true, "discipline_code": true, "research_field": true,
	"h_index": true, "total_citations": true, "total_papers": true,
	"phd_year": true, "degree": true,
	"organization": true,
	"education":          true,
	"research_direction": true,
}

// sedimentFormDataToMemory persists confirmed form field values to long-term memory
// so future workflows can auto-prefill from memory recall (Phase 2).
// Only fields declared Reusable by the template (or in the legacy whitelist for
// unannotated templates) are saved — task-specific creative content is never persisted.
//
// Called asynchronously after successful form submission.
func (h *IMMessageHandler) sedimentFormDataToMemory(userID, phaseID string, data map[string]interface{}, state *v2.WorkflowState) {
	if h == nil || h.memoryStore == nil || len(data) == 0 {
		return
	}

	// Find the phase's InputSchema to get field labels and Reusable flags
	var schema *v2.PhaseInputSchema
	if state != nil {
		if phase := state.ActivePhase(); phase != nil && phase.InputSchema != nil {
			schema = phase.InputSchema
		}
	}

	schemaHasReusable := v2.SchemaHasReusableFields(schema)

	// Build a field lookup for label and Reusable check (including variant fields)
	fieldMap := make(map[string]v2.PhaseInputField)
	if schema != nil {
		for _, f := range schema.Fields {
			fieldMap[f.Name] = f
		}
		for _, variant := range schema.Variants {
			for _, f := range variant.Fields {
				if _, exists := fieldMap[f.Name]; !exists {
					fieldMap[f.Name] = f
				}
			}
		}
	}

	var sedimentLines []string
	for fieldName, value := range data {
		// Determine if this field should be sedimented
		if f, ok := fieldMap[fieldName]; ok {
			if !v2.ShouldSediment(f, schemaHasReusable) {
				continue
			}
		} else {
			// Field not in schema (e.g. hidden routing field) — use legacy whitelist
			if !sedimentableFields[fieldName] {
				continue
			}
		}
		strValue := ""
		switch v := value.(type) {
		case string:
			strValue = strings.TrimSpace(v)
		case float64:
			// JSON numbers arrive as float64; format without trailing zeros
			if v == float64(int64(v)) {
				strValue = fmt.Sprintf("%d", int64(v))
			} else {
				strValue = fmt.Sprintf("%g", v)
			}
		case int:
			strValue = fmt.Sprintf("%d", v)
		case bool:
			// Skip boolean values — not useful for factual memory
			continue
		default:
			continue
		}
		if strValue == "" {
			continue
		}

		// Get the label for a more readable memory entry
		label := fieldName
		if f, ok := fieldMap[fieldName]; ok && f.Label != "" {
			label = f.Label
		}
		// Use field NAME as the stable key (not the per-template label which varies:
		// "现工作单位" vs "依托单位" vs "单位" for the same field "institution").
		// Include both name and label so BM25 recall hits on either.
		line := fieldName + "：" + strValue
		if label != fieldName {
			// e.g. "institution/现工作单位：北京大学" — BM25 hits on both "institution" and "现工作单位"
			line = fieldName + "/" + label + "：" + strValue
		}
		sedimentLines = append(sedimentLines, line)
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

	// Use SaveWithContext to leverage substring dedup (#64) — if the user already
	// has a memory entry with the same facts, it will be merged rather than duplicated.
	if err := h.memoryStore.SaveWithContext(entry, "workflow form data: "+phaseID); err != nil {
		log.Printf("[workflow-v2-prefill] sediment form data failed: %v", err)
	} else {
		log.Printf("[workflow-v2-prefill] sedimented %d fields to memory for user=%s phase=%s",
			len(sedimentLines), userID, phaseID)
	}
}
