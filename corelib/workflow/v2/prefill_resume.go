package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ResumeParseRequest contains the resume text and the target form schema.
// The LLM extracts structured field values from the resume text,
// guided by the schema's field names, labels, and types.
type ResumeParseRequest struct {
	ResumeText string           // the extracted text content of the uploaded resume/CV
	Schema     *PhaseInputSchema // the target form schema to populate
}

// ResumeParseResult contains the extracted field values from a resume.
type ResumeParseResult struct {
	// Fields maps field Name → extracted value (string or []string for multiselect).
	Fields map[string]interface{} `json:"fields"`
	// Confidence maps field Name → extraction confidence (0.0-1.0).
	Confidence map[string]float64 `json:"confidence"`
	// SourceQuotes maps field Name → the original text snippet the value was extracted from.
	SourceQuotes map[string]string `json:"source_quotes"`
}

// ResumeLLMCaller is the interface for making LLM calls to parse resumes.
// The GUI layer implements this by delegating to the configured LLM provider.
type ResumeLLMCaller interface {
	// CallLLMForResumeParse sends the system+user prompt to the LLM and returns
	// the raw response text. The caller is responsible for constructing the prompt.
	CallLLMForResumeParse(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// ParseResumeForSchema uses LLM to extract form field values from resume text.
// This is a structured extraction task — the LLM is given the exact field schema
// and asked to map resume content to field values. No hallucination is possible
// because the LLM can only fill values it finds in the text (instructed to leave
// fields empty if not found).
//
// Returns nil if parsing fails or no fields could be extracted.
func ParseResumeForSchema(ctx context.Context, req ResumeParseRequest, llm ResumeLLMCaller) (*ResumeParseResult, error) {
	if req.Schema == nil || len(req.Schema.Fields) == 0 || req.ResumeText == "" || llm == nil {
		return nil, fmt.Errorf("invalid request: schema, resume text, and LLM caller are all required")
	}

	systemPrompt := buildResumeParseSystemPrompt(req.Schema)
	userPrompt := buildResumeParseUserPrompt(req.ResumeText)

	raw, err := llm.CallLLMForResumeParse(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	result, err := parseResumeResponse(raw, req.Schema)
	if err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	return result, nil
}

// ResumeParseResultToPrefilled converts ResumeParseResult to the standard
// prefill map used by the form rendering system.
// Only fields that exist in the schema are included — LLM hallucinated field names are dropped.
func ResumeParseResultToPrefilled(result *ResumeParseResult, schema *PhaseInputSchema) map[string]*PrefilledValue {
	if result == nil || len(result.Fields) == 0 {
		return nil
	}

	// Build valid field name set from schema
	validFields := make(map[string]bool)
	if schema != nil {
		for _, f := range schema.Fields {
			validFields[f.Name] = true
		}
	}

	prefilled := make(map[string]*PrefilledValue, len(result.Fields))
	for name, value := range result.Fields {
		// Skip fields not in schema (LLM hallucination)
		if schema != nil && !validFields[name] {
			continue
		}
		// Skip empty values
		if strVal, ok := value.(string); ok && strings.TrimSpace(strVal) == "" {
			continue
		}

		conf := 0.85 // default confidence for LLM extraction
		if c, ok := result.Confidence[name]; ok {
			conf = c
		}

		sourceDetail := "从简历中提取"
		if quote, ok := result.SourceQuotes[name]; ok && quote != "" {
			sourceDetail = "简历原文: " + truncateRunes(quote, 60)
		}

		prefilled[name] = &PrefilledValue{
			Value:        value,
			Source:       "resume",
			SourceDetail: sourceDetail,
			Confidence:   conf,
			NeedsConfirm: false, // resume is user-provided, high trust
		}
	}

	if len(prefilled) == 0 {
		return nil
	}
	return prefilled
}

// --- Prompt construction ---

func buildResumeParseSystemPrompt(schema *PhaseInputSchema) string {
	var sb strings.Builder
	sb.WriteString(`你是一个简历/CV信息提取专家。你的任务是从用户提供的简历文本中，精确提取指定字段的值。

规则：
1. 只提取简历中明确存在的信息，绝不推测或编造
2. 如果简历中找不到某个字段的信息，该字段值设为空字符串 ""
3. 对于 select 类型的字段，值必须是给定选项之一，否则设为 ""
4. 对于 textarea 字段，保持原文格式（换行符等）
5. 数字字段（如H指数、论文数）提取纯数字
6. 日期字段保持简历中的原始格式（如"1980年5月"）

请以 JSON 格式返回，结构如下：
{
  "fields": { "字段name": "提取的值", ... },
  "confidence": { "字段name": 0.95, ... },
  "source_quotes": { "字段name": "简历中的原始片段", ... }
}

需要提取的字段：
`)

	for _, f := range schema.Fields {
		// Skip fields that should never be prefilled (task-specific creative fields
		// like project_title, core_question — these can't exist in a resume).
		if noPrefillFieldNames[f.Name] {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s (%s): %s", f.Name, f.Type, f.Label))
		if f.Placeholder != "" {
			sb.WriteString(fmt.Sprintf(" [示例: %s]", f.Placeholder))
		}
		if len(f.Options) > 0 {
			opts := make([]string, len(f.Options))
			for i, o := range f.Options {
				opts[i] = o.Value
			}
			sb.WriteString(fmt.Sprintf(" [可选值: %s]", strings.Join(opts, "/")))
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}

func buildResumeParseUserPrompt(resumeText string) string {
	// Truncate extremely long resumes to avoid context overflow
	const maxRunes = 8000
	runes := []rune(resumeText)
	if len(runes) > maxRunes {
		resumeText = string(runes[:maxRunes]) + "\n...(简历内容过长，已截断)"
	}
	return "以下是用户的简历/CV内容，请提取上述字段的值：\n\n" + resumeText
}

// --- Response parsing ---

func parseResumeResponse(raw string, schema *PhaseInputSchema) (*ResumeParseResult, error) {
	// Strip markdown code fence if present
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			raw = lines[1]
		}
		if idx := strings.LastIndex(raw, "```"); idx > 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}

	var result ResumeParseResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		// Try to find the JSON object containing "fields" key.
		// Look for the opening brace that precedes "fields" — this handles cases
		// where the LLM outputs preamble text before the JSON.
		extracted := extractJSONWithFieldsKey(raw)
		if extracted == "" {
			return nil, fmt.Errorf("no JSON with 'fields' key found in response: %w (raw=%s)", err, truncateRunes(raw, 200))
		}
		if err2 := json.Unmarshal([]byte(extracted), &result); err2 != nil {
			return nil, fmt.Errorf("cannot parse extracted JSON: %w (raw=%s)", err2, truncateRunes(extracted, 200))
		}
	}

	if result.Fields == nil {
		result.Fields = make(map[string]interface{})
	}
	if result.Confidence == nil {
		result.Confidence = make(map[string]float64)
	}
	if result.SourceQuotes == nil {
		result.SourceQuotes = make(map[string]string)
	}

	// Validate select fields — value must be one of the options
	for _, f := range schema.Fields {
		if f.Type != "select" || len(f.Options) == 0 {
			continue
		}
		val, ok := result.Fields[f.Name]
		if !ok {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		valid := false
		for _, opt := range f.Options {
			if opt.Value == strVal {
				valid = true
				break
			}
		}
		if !valid {
			delete(result.Fields, f.Name)
		}
	}

	return &result, nil
}

// extractJSONWithFieldsKey finds the outermost JSON object that contains a "fields" key.
// This is more robust than naive first-{/last-} matching when the LLM outputs
// preamble text containing braces.
// String-aware: braces inside JSON string values (quoted) are not counted.
func extractJSONWithFieldsKey(raw string) string {
	// Find the position of "fields" keyword
	fieldsIdx := strings.Index(raw, `"fields"`)
	if fieldsIdx < 0 {
		return ""
	}
	// Walk backwards from "fields" to find the opening brace
	openIdx := -1
	for i := fieldsIdx - 1; i >= 0; i-- {
		if raw[i] == '{' {
			openIdx = i
			break
		}
	}
	if openIdx < 0 {
		return ""
	}
	// Find the matching closing brace by counting brace depth.
	// Skip content inside JSON strings (between unescaped double quotes).
	depth := 0
	inString := false
	for i := openIdx; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if ch == '\\' && i+1 < len(raw) {
				i++ // skip escaped character
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[openIdx : i+1]
			}
		}
	}
	return ""
}

// truncateRunes truncates a string to maxRunes runes.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
