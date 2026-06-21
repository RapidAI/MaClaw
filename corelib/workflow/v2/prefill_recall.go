package v2

import (
	"context"
	"strings"
)

// RecallResult represents a single result from memory or knowledge base recall.
// This is the common interface used by the prefill system — both memory entries
// and knowledge search results are mapped to this structure by the consumer layer.
type RecallResult struct {
	Content    string  // the text content of the memory/knowledge entry
	Category   string  // "user_fact" / "project_knowledge" / "task_artifact" / "knowledge_card" / "knowledge_fact" etc.
	Source     string  // provenance: "memory" or "knowledge"
	SourceID   string  // entry ID for traceability
	Score      float64 // relevance score from recall
	SourceDesc string  // human-readable source description (e.g. "来自知识库: AI论文.pdf")
}

// RecallProvider is the interface for retrieving information from memory and
// knowledge bases. The GUI/TUI layer implements this interface by delegating
// to memory.Store.RecallDynamic() and knowledge.SQLiteStore.Search().
//
// This abstraction exists because corelib/workflow/v2 cannot import corelib/memory
// or corelib/knowledge directly (layering constraint).
type RecallProvider interface {
	// RecallForField searches memory and knowledge base for information relevant
	// to the given field. The query is constructed from the field's semantics.
	// Returns results sorted by relevance score (highest first).
	// maxResults limits the number of results returned (typically 3-5).
	RecallForField(ctx context.Context, query string, maxResults int) []RecallResult
}

// PrefillFromRecall enriches the prefill map with values from memory and knowledge base.
// It only fills fields that are NOT already populated (by PrefillFromContext).
// Only values with clear provenance are used — no LLM inference.
//
// Strategy: two-pass recall.
//   - Pass 1 ("bulk recall"): one comprehensive query using all field labels joined together.
//     This retrieves the most relevant CV/profile paragraphs holistically (e.g. the "基本信息"
//     section of a resume). All unfilled fields are then extracted from these shared results.
//   - Pass 2 ("per-field recall"): for fields still unfilled after Pass 1, issue targeted
//     per-field queries with field-specific metadata (Placeholder, Description) for higher precision.
//
// This two-pass design reduces total recall calls from N to 1+M (M << N) and improves
// extraction coherence (multiple fields from the same paragraph can cross-validate).
//
// Parameters:
//   - schema: the phase's InputSchema defining expected fields
//   - existing: already-populated prefill values (from PrefillFromContext), may be nil
//   - provider: the recall provider implementation (memory + knowledge)
//   - ctx: context for cancellation
//
// Returns the enriched map (same map if existing is non-nil, new map otherwise).
func PrefillFromRecall(ctx context.Context, schema *PhaseInputSchema, existing map[string]*PrefilledValue, provider RecallProvider) map[string]*PrefilledValue {
	if schema == nil || len(schema.Fields) == 0 || provider == nil {
		return existing
	}

	if existing == nil {
		existing = make(map[string]*PrefilledValue)
	}

	// Collect fields that need recall
	schemaHasReusable := SchemaHasReusableFields(schema)
	var needRecall []PhaseInputField
	for _, field := range schema.Fields {
		if _, ok := existing[field.Name]; ok {
			continue
		}
		if !ShouldRecallPrefill(field, schemaHasReusable) {
			continue
		}
		// Gate: skip required textarea fields that are likely creative/task-specific
		// content (e.g. "core_question", "hypothesis"). However, if the field explicitly
		// declares Reusable=true, it has already passed ShouldRecallPrefill — the
		// template author has explicitly said "this textarea is factual and recallable",
		// so we trust that declaration over the legacy isFactualTextareaField whitelist.
		if field.Type == "textarea" && field.Required && !field.Reusable && !isFactualTextareaField(field.Name) {
			continue
		}
		needRecall = append(needRecall, field)
	}
	if len(needRecall) == 0 {
		return existing
	}

	// --- Pass 1: Bulk recall with joined field labels ---
	select {
	case <-ctx.Done():
		return existing
	default:
	}

	bulkQuery := buildBulkRecallQuery(needRecall)
	bulkResults := provider.RecallForField(ctx, bulkQuery, 5) // more results for bulk

	// Try to extract all fields from bulk results
	if len(bulkResults) > 0 {
		for i := range needRecall {
			if _, ok := existing[needRecall[i].Name]; ok {
				continue // filled by a previous field in this loop
			}
			if pv := extractValueFromRecallResults(needRecall[i], bulkResults); pv != nil {
				existing[needRecall[i].Name] = pv
			}
		}
	}

	// --- Pass 2: Per-field targeted recall for still-unfilled fields ---
	for _, field := range needRecall {
		if _, ok := existing[field.Name]; ok {
			continue
		}
		select {
		case <-ctx.Done():
			return existing
		default:
		}

		query := buildRecallQuery(field)
		if query == "" {
			continue
		}

		results := provider.RecallForField(ctx, query, 3)
		if len(results) == 0 {
			continue
		}

		if pv := extractValueFromRecallResults(field, results); pv != nil {
			existing[field.Name] = pv
		}
	}

	return existing
}

// buildBulkRecallQuery constructs a comprehensive query from all unfilled field labels.
// This retrieves the user's profile/CV section that is most relevant to the form as a whole.
// Truncated to ~80 runes to avoid BM25 over-dilution with too many terms.
func buildBulkRecallQuery(fields []PhaseInputField) string {
	seen := make(map[string]bool)
	var parts []string
	totalRunes := 0
	const maxRunes = 80

	for _, f := range fields {
		// Prefer Label (natural language, matches CV text)
		token := f.Label
		if token == "" {
			token = f.Name
		}
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		tokenRunes := len([]rune(token))
		if totalRunes+tokenRunes > maxRunes {
			break
		}
		parts = append(parts, token)
		totalRunes += tokenRunes + 1 // +1 for space separator
	}
	return strings.Join(parts, " ")
}

// buildRecallQuery constructs a search query from a field's metadata.
// Uses all available field metadata (Name, Label, Placeholder, Description)
// to build a rich query that maximizes recall from both memory and knowledge base.
//
// Design principle: the field's own metadata (Label, Placeholder) already contains
// the best synonyms and examples. No need for a hardcoded switch-case synonym table.
// This approach automatically works for any new field in any template — zero maintenance.
func buildRecallQuery(field PhaseInputField) string {
	if field.Label == "" && field.Name == "" {
		return ""
	}

	var parts []string

	// Always include field Name (matches sedimented entries: "institution/...：value")
	if field.Name != "" {
		parts = append(parts, field.Name)
	}

	// Include Label if different from Name (matches natural language in memory/knowledge)
	if field.Label != "" && field.Label != field.Name {
		parts = append(parts, field.Label)
	}

	// Include Placeholder — often contains examples or alternative phrasings.
	// Strip common placeholder prefixes and limit length to avoid BM25 dilution.
	if ph := cleanPlaceholder(field.Placeholder); ph != "" {
		parts = append(parts, ph)
	}

	// Include Description if short enough (longer descriptions dilute BM25 scoring)
	if field.Description != "" && len([]rune(field.Description)) <= 30 {
		parts = append(parts, field.Description)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// cleanPlaceholder extracts useful search terms from a Placeholder string.
// Strips "如：", "如:", "例如：", "例如:", "请填写", "请输入", "按时间顺序列出" etc.
// Truncates to first 40 runes to avoid BM25 score dilution from long examples.
var placeholderPrefixStrip = []string{
	"如：", "如:", "例如：", "例如:", "例：", "例:",
	"请填写", "请输入", "请提供", "按时间顺序列出：", "按时间顺序列出:",
}

func cleanPlaceholder(placeholder string) string {
	if placeholder == "" {
		return ""
	}
	s := placeholder
	for _, prefix := range placeholderPrefixStrip {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Take only the first line (multi-line placeholders are verbose examples)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		s = s[:idx]
	}
	// Truncate to avoid diluting BM25 scoring with too many tokens
	runes := []rune(s)
	if len(runes) > 40 {
		s = string(runes[:40])
	}
	return s
}

// extractValueFromRecallResults tries to find a suitable value for the field
// from the recall results. Uses rule-based extraction — no LLM inference.
func extractValueFromRecallResults(field PhaseInputField, results []RecallResult) *PrefilledValue {
	// For select fields: delegate to extractSelectField which has proper context-awareness
	// for short option values (≤2 runes like "男"/"女" require identity markers).
	if field.Type == "select" && len(field.Options) > 0 {
		for _, r := range results {
			if r.Content == "" {
				continue
			}
			if pv := extractSelectField(field, r.Content); pv != nil {
				pv.Source = r.Source
				pv.SourceDetail = truncateSourceDesc(r.SourceDesc, 80)
				pv.Confidence = recallConfidence(r)
				return pv
			}
		}
		return nil
	}

	// For factual textarea fields (education, research_direction, etc.):
	// If knowledge base returns a relevant paragraph (>20 runes, <500 runes),
	// use it directly as the pre-fill value. These fields expect multi-line
	// factual content that maps naturally to CV paragraphs.
	// Additional validation: the content must contain at least one field-specific
	// signal keyword to avoid filling "education" with a research paper abstract
	// that happened to mention "博士" somewhere.
	if field.Type == "textarea" && isFactualTextareaField(field.Name) {
		for _, r := range results {
			content := strings.TrimSpace(r.Content)
			runes := []rune(content)
			if len(runes) < 20 || len(runes) > 500 {
				continue
			}
			if r.Score <= 0.4 {
				continue
			}
			// Validate content has field-specific signals (not just a tangential mention)
			if !hasTextareaFieldSignal(field.Name, content) {
				continue
			}
			return &PrefilledValue{
				Value:        content,
				Source:       r.Source,
				SourceDetail: truncateSourceDesc(r.SourceDesc, 80),
				Confidence:   recallConfidence(r),
				NeedsConfirm: true, // user should verify multi-line content
			}
		}
	}

	// For text/other fields: try extraction from recall content in order of precision:
	// 1. Label-anchor extraction ("Label：Value" format) — highest precision for structured recall
	// 2. Field name "/" separator (sedimented format: "institution/现工作单位：北京大学")
	// 3. Specialized extractors via extractFieldFromContext (free-form CV text)
	for _, r := range results {
		if r.Content == "" {
			continue
		}

		var pv *PrefilledValue

		// Try label-anchor first (precise for structured recall content)
		pv = extractByLabelAnchor(field, r.Content)

		// Try field name with "/" separator (sedimented format)
		if pv == nil && field.Name != "" {
			nameSlashAnchor := field.Name + "/"
			if idx := strings.LastIndex(r.Content, nameSlashAnchor); idx >= 0 {
				afterSlash := r.Content[idx+len(nameSlashAnchor):]
				for _, sep := range []string{"：", ":"} {
					if sepIdx := strings.Index(afterSlash, sep); sepIdx >= 0 {
						afterValue := afterSlash[sepIdx+len(sep):]
						if v := extractValueAfterAnchor(afterValue); v != "" {
							pv = &PrefilledValue{
								Value:        v,
								Source:       "context",
								SourceDetail: "提取自: " + field.Name + "/" + afterSlash[:sepIdx] + sep + v,
								Confidence:   0.75,
							}
							break
						}
					}
				}
			}
		}

		// Try specialized extractors (person name, institution, discipline, title)
		// which work on free-form CV text without requiring "Label：" format.
		if pv == nil {
			pv = extractFieldFromContext(field, r.Content)
		}

		if pv != nil {
			// Override source to reflect recall provenance
			pv.Source = r.Source
			pv.SourceDetail = truncateSourceDesc(r.SourceDesc, 80)
			pv.Confidence = recallConfidence(r)
			return pv
		}
	}

	// For short factual fields (name, h_index, etc.): if recall returns a
	// short entry (≤50 runes) with high score, use it directly.
	// But first strip any "Label：" prefix that may exist from sedimentation format
	// (sedimentFormDataToMemory stores "H指数：42", we want just "42").
	// Accepts both memory (user_fact/preference) and knowledge base sources —
	// a CV imported into the knowledge base should be as authoritative as sedimented memory.
	//
	// IMPORTANT: Only accept content that is either:
	// - A bare value (no "Label：" prefix from another field), OR
	// - Prefixed with THIS field's Label/Name (strippable)
	// This prevents bulk recall results from being cross-assigned to wrong fields.
	if isShortFactField(field.Name) {
		for _, r := range results {
			content := r.Content
			runes := []rune(content)
			if len(runes) == 0 || len(runes) > 50 {
				continue
			}
			if r.Score <= 0.5 {
				continue
			}
			// Accept user_fact, preference (memory), and knowledge sources (CV import)
			if r.Category != "user_fact" && r.Category != "preference" && r.Source != "knowledge" {
				continue
			}

			// Strip THIS field's "Label：" or "Name：" prefix if present.
			// Also handle common label variants (e.g. "h-index:" for field Name "h_index").
			stripped := false
			for _, labelOrName := range []string{field.Label, field.Name} {
				if labelOrName == "" {
					continue
				}
				for _, sep := range []string{"：", ":"} {
					prefix := labelOrName + sep
					if strings.HasPrefix(content, prefix) {
						content = strings.TrimSpace(content[len(prefix):])
						stripped = true
						break
					}
				}
				if stripped {
					break
				}
			}

			// If not stripped by exact prefix match, try to extract value after any
			// colon separator where the label portion overlaps with this field's semantics.
			if !stripped && looksLikeLabeledEntry(content) {
				extracted := tryExtractValueFromLabeledContent(field, content)
				if extracted != "" {
					content = extracted
					stripped = true
				} else {
					// Content has a label prefix from another field — not for us.
					continue
				}
			}

			if content != "" {
				return &PrefilledValue{
					Value:        content,
					Source:       r.Source,
					SourceDetail: truncateSourceDesc(r.SourceDesc, 80),
					Confidence:   recallConfidence(r),
				}
			}
		}
	}

	return nil
}

// isShortFactField returns true for fields that typically hold short factual values
// (e.g. name, h_index, birth_date) where a recall entry's entire content could be the answer.
var shortFactFields = map[string]bool{
	"name": true, "h_index": true, "birth_date": true,
	"total_citations": true, "total_papers": true,
	"phd_year": true, "nationality": true,
	"discipline_code": true, "funding_amount": true,
	"duration": true,
}

func isShortFactField(name string) bool {
	return shortFactFields[name]
}

// isFactualTextareaField returns true for textarea fields that contain factual
// biographical information (extractable from CVs) rather than creative content
// that only the user can author for this specific task.
var factualTextareaFields = map[string]bool{
	"education":            true, // 教育背景 — CV standard section
	"research_direction":   true, // 主要研究方向 — CV standard section
	"key_achievements":     true, // 主要学术亮点 — CV standard section
	"work_experience":      true, // 工作经历
	"academic_service":     true, // 学术服务
	"awards":               true, // 获奖情况
	"representative_works": true, // 代表性成果
	"funded_projects":      true, // 主持项目
}

func isFactualTextareaField(name string) bool {
	return factualTextareaFields[name]
}

// textareaFieldSignals defines required signal keywords for each factual textarea field.
// Content from recall must contain at least ONE of these signals to be accepted as a
// valid pre-fill value. This prevents filling "education" with unrelated content that
// happened to score well on BM25 due to overlapping tokens like "博士"/"研究".
var textareaFieldSignals = map[string][]string{
	"education":            {"本科", "硕士", "博士", "学士", "毕业", "学位", "大学", "学院"},
	"research_direction":   {"研究", "方向", "领域", "从事", "课题", "专注"},
	"key_achievements":     {"论文", "项目", "奖", "专利", "成果", "发表", "基金", "获得"},
	"work_experience":      {"任职", "工作", "就职", "担任", "年"},
	"academic_service":     {"审稿", "编委", "评审", "委员", "学会", "期刊"},
	"awards":               {"奖", "荣誉", "表彰", "获得", "一等", "二等"},
	"representative_works": {"论文", "著作", "专著", "发表", "出版", "期刊"},
	"funded_projects":      {"项目", "基金", "资助", "课题", "经费", "主持"},
}

// hasTextareaFieldSignal checks if content contains at least one signal keyword
// for the given field. Returns true if no signals are defined (permissive fallback).
func hasTextareaFieldSignal(fieldName, content string) bool {
	signals, ok := textareaFieldSignals[fieldName]
	if !ok {
		return true // no signals defined → permissive
	}
	for _, sig := range signals {
		if strings.Contains(content, sig) {
			return true
		}
	}
	return false
}

// recallConfidence maps recall result properties to a confidence score.
func recallConfidence(r RecallResult) float64 {
	switch {
	case r.Source == "knowledge" && r.Score > 0.8:
		return 0.90 // high-scoring knowledge base hit
	case r.Source == "knowledge":
		return 0.80 // any knowledge base hit
	case r.Category == "user_fact":
		return 0.85 // user facts are reliable
	case r.Category == "project_knowledge":
		return 0.80
	case r.Category == "task_artifact":
		return 0.75
	default:
		return 0.65
	}
}

// containsWord checks if text contains the word (simple substring for CJK).
func containsWord(text, word string) bool {
	return word != "" && strings.Contains(text, word)
}

func truncateSourceDesc(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// looksLikeLabeledEntry returns true if content appears to be a "Label：Value" entry
// from another field (e.g. "研究领域：自然语言处理"). This prevents cross-assignment
// of bulk recall results to unrelated shortFactFields.
func looksLikeLabeledEntry(content string) bool {
	// Check for Chinese or English colon within the first 15 runes (labels are short)
	runes := []rune(content)
	limit := 15
	if len(runes) < limit {
		limit = len(runes)
	}
	prefix := string(runes[:limit])
	return strings.Contains(prefix, "：") || strings.Contains(prefix, ":")
}

// tryExtractValueFromLabeledContent handles "label: value" content where the label
// might be a variant of the current field's name (e.g. "h-index: 35" for field "h_index").
// Returns the extracted value if the label portion is semantically related to the field,
// or empty string if it's from a different field.
func tryExtractValueFromLabeledContent(field PhaseInputField, content string) string {
	// Find the first colon (Chinese or English)
	var labelPart, valuePart string
	for _, sep := range []string{"：", ":"} {
		idx := strings.Index(content, sep)
		if idx > 0 {
			labelPart = strings.TrimSpace(content[:idx])
			valuePart = strings.TrimSpace(content[idx+len(sep):])
			break
		}
	}
	if labelPart == "" || valuePart == "" {
		return ""
	}

	// Check if labelPart overlaps with field Name or Label (case-insensitive, dash/underscore normalized)
	normalizedLabel := normalizeFieldToken(labelPart)
	if normalizedLabel == "" {
		return ""
	}

	fieldTokens := []string{normalizeFieldToken(field.Name), normalizeFieldToken(field.Label)}
	for _, ft := range fieldTokens {
		if ft == "" {
			continue
		}
		// Exact match or substring containment (both directions)
		if ft == normalizedLabel || strings.Contains(ft, normalizedLabel) || strings.Contains(normalizedLabel, ft) {
			return valuePart
		}
	}

	return ""
}

// normalizeFieldToken lowercases and normalizes separators for label comparison.
// "H-index" → "hindex", "h_index" → "hindex", "H指数" → "h指数"
func normalizeFieldToken(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}
