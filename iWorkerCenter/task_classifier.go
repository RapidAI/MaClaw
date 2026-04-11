package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Work Type constants define the classification labels for LLM requests.
const (
	WorkTypeDocumentWriting  = "document_writing"
	WorkTypeDataAnalysis     = "data_analysis"
	WorkTypeQualityReport    = "quality_report"
	WorkTypeProductionReport = "production_report"
	WorkTypeTableFormatting  = "table_formatting"
	WorkTypeLongTextSummary  = "long_text_summary"
	WorkTypeSimpleQA         = "simple_qa"
)

// Cost Tier constants define the model cost/capability levels.
const (
	CostTierHigh   = "high"
	CostTierMedium = "medium"
	CostTierLow    = "low"
)

// ClassifyInput holds the data extracted from a request for classification.
type ClassifyInput struct {
	TaskType       string // from request body "task_type" field, may be empty
	MessageContent string // concatenated user message text
	ColleagueName  string // optional, extracted from request context
}

// ClassificationResult holds the output of the classification process.
type ClassificationResult struct {
	WorkType string        // e.g. "document_writing", "simple_qa"
	CostTier string        // "high", "medium", "low"
	Latency  time.Duration // classification processing time
	Method   string        // "task_type_match", "keyword_match", "default"
}

// classifyByTaskType checks if the taskType string contains any keyword
// from any work type's keyword list. When multiple work types match,
// the one with the longest matching keyword wins (deterministic tiebreak).
func classifyByTaskType(taskType string, keywords map[string][]string) (string, bool) {
	bestType := ""
	bestLen := 0
	for workType, kwList := range keywords {
		for _, kw := range kwList {
			if kw != "" && strings.Contains(taskType, kw) {
				kwLen := len([]rune(kw))
				if kwLen > bestLen || (kwLen == bestLen && workType < bestType) {
					bestType = workType
					bestLen = kwLen
				}
			}
		}
	}
	if bestType != "" {
		return bestType, true
	}
	return "", false
}

// classifyByKeywords scans content against all keyword lists and returns
// the work type with the most keyword hits. Returns "" if no keywords match.
func classifyByKeywords(content string, keywords map[string][]string) string {
	bestType := ""
	bestCount := 0
	for workType, kwList := range keywords {
		count := 0
		for _, kw := range kwList {
			if kw != "" && strings.Contains(content, kw) {
				count++
			}
		}
		if count > bestCount {
			bestCount = count
			bestType = workType
		}
	}
	return bestType
}

// Classify determines the WorkType and CostTier for a request.
// It orchestrates the classification flow:
//  1. Record start time
//  2. If TaskType is not empty and not "自由输入", try classifyByTaskType
//  3. If no task_type match, try classifyByKeywords on MessageContent
//  4. If no keyword match, default to rules.DefaultWorkType (simple_qa)
//  5. Look up CostTier via rules.LookupTier
//  6. Record Latency and Method
func Classify(input ClassifyInput, rules RoutingRules) ClassificationResult {
	start := time.Now()

	var workType string
	var method string

	// Step 1: Try task_type match if TaskType is present and not "自由输入"
	if input.TaskType != "" && input.TaskType != "自由输入" {
		if wt, ok := classifyByTaskType(input.TaskType, rules.WorkTypeKeywords); ok {
			workType = wt
			method = "task_type_match"
		}
	}

	// Step 2: Try keyword match on MessageContent
	if workType == "" {
		if wt := classifyByKeywords(input.MessageContent, rules.WorkTypeKeywords); wt != "" {
			workType = wt
			method = "keyword_match"
		}
	}

	// Step 3: Default fallback
	if workType == "" {
		workType = rules.DefaultWorkType
		method = "default"
	}

	return ClassificationResult{
		WorkType: workType,
		CostTier: rules.LookupTier(workType),
		Latency:  time.Since(start),
		Method:   method,
	}
}

// FormatTaskRouteLog formats a structured audit log line for a classification decision.
// The summary is truncated to the first 200 runes to handle multi-byte characters correctly.
func FormatTaskRouteLog(result ClassificationResult, reqID string, providerID string, summary string) string {
	if utf8.RuneCountInString(summary) > 200 {
		runes := []rune(summary)
		summary = string(runes[:200])
	}
	// Escape double quotes in summary to preserve log format integrity
	summary = strings.ReplaceAll(summary, `"`, `\"`)
	return fmt.Sprintf(
		`[TaskRoute] ts=%s req_id=%s work_type=%s cost_tier=%s provider=%s latency_ms=%d method=%s summary="%s"`,
		time.Now().UTC().Format(time.RFC3339),
		reqID,
		result.WorkType,
		result.CostTier,
		providerID,
		result.Latency.Milliseconds(),
		result.Method,
		summary,
	)
}
