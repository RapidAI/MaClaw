package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Intent Tree reasoning channel — ported from intent-fusion's tree.py.
//
// Builds a domain-grouped intent tree from IntentDefinition entries, then
// uses a single LLM call with chain-of-thought reasoning to select top
// candidates with scores.
//
// Complements the embedding channel:
//   - Embedding excels at surface/lexical similarity ("修复bug" → bug_fix)
//   - Intent tree excels at semantic inference ("我改主意了" → continuation)
// ---------------------------------------------------------------------------

// TreeCandidate is a candidate returned by the LLM tree reasoning channel.
type TreeCandidate struct {
	Label        IntentLabel
	Score        float64
	Reason       string
	WorkflowType string // non-empty when the intent maps to a workflow template
}

// TreeResponseProtocolError reports a successful transport response that did
// not implement the intent-classification response contract.  It is a control
// plane failure, not evidence that the user's request has an unknown intent.
// Keep the error body-free because model output can contain user context.
type TreeResponseProtocolError struct{}

func (*TreeResponseProtocolError) Error() string {
	return "intent tree response violated structured-output protocol"
}

// BuildIntentTreeText constructs a domain-grouped intent tree prompt from
// IntentDefinition entries. Within each domain, intents are listed together
// so the LLM naturally compares them. For domains with 2+ intents, a
// disambiguation note is auto-appended listing the sibling intents,
// so the LLM knows what alternatives exist without hardcoded "区别于 X" hints.
//
// When an IntentDefinition has WorkflowTypes, the tree text includes
// workflow_type guidance so the LLM can output the specific workflow
// template type in a single reasoning pass.
//
// Output format:
//
//	── Coding ──
//	  coding: user wants to create new software...
//	    → workflow_type: "coding"
//	  bug_fix: user wants to fix bugs...
//	  maintenance: user wants to refactor...
//	  (Note: choose the single best match among coding/bug_fix/maintenance)
func BuildIntentTreeText(defs []IntentDefinition) string {
	type domainEntry struct {
		lines  []string
		labels []string
	}
	domainMap := make(map[string]*domainEntry)
	domainOrder := make([]string, 0)

	for _, def := range defs {
		line := fmt.Sprintf("  %s: %s", def.Label, def.TreeText)
		// Append workflow_type guidance if this intent can trigger workflows.
		if len(def.WorkflowTypes) == 1 {
			line += fmt.Sprintf("\n    → workflow_type: %q (when applicable, otherwise \"\")", def.WorkflowTypes[0])
		} else if len(def.WorkflowTypes) > 1 {
			line += "\n    → workflow_type: choose from " + strings.Join(quoteAll(def.WorkflowTypes), ", ") + " (or \"\" if none applies)"
		}
		e, exists := domainMap[def.Domain]
		if !exists {
			e = &domainEntry{}
			domainMap[def.Domain] = e
			domainOrder = append(domainOrder, def.Domain)
		}
		e.lines = append(e.lines, line)
		e.labels = append(e.labels, string(def.Label))
	}

	var parts []string
	for _, domain := range domainOrder {
		e := domainMap[domain]
		parts = append(parts, fmt.Sprintf("── %s ──", domain))
		parts = append(parts, e.lines...)
		// Auto-generate a default disambiguation note for domains with multiple
		// intents.  Some explicitly declared cross-label dependency chains (for
		// example live_data + document_generate) intentionally span a domain, so
		// this must be a default rather than a blanket exclusivity constraint.
		if len(e.labels) >= 2 {
			parts = append(parts, fmt.Sprintf("  (Note: normally choose the single best match among %s; keep multiple labels only for an explicitly allowed composite.)",
				strings.Join(e.labels, "/")))
		}
		parts = append(parts, "")
	}

	return strings.Join(parts, "\n")
}

// quoteAll wraps each string in double quotes.
func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// intentTreePromptTemplate is the LLM prompt for tree reasoning. The transport
// contract is a JSON schema, so this prompt must not ask the model to emit
// reasoning tags or prose alongside that JSON. Some OpenAI-compatible relays
// otherwise treat the request as ordinary chat and return a 200 prose reply.
const intentTreePromptTemplate = `You are an intent classifier. Given the user message, select the best matching intents from the intent tree below.

## Intent Tree
%s

## User Message
"%s"

## Instructions
Determine the intent internally. Return ONLY this JSON object, with no markdown,
reasoning tags, or text before or after it:
{"top": [{"skill": "intent_name", "score": 0.0-1.0, "workflow_type": "type_or_empty"}, ...]}

Rules:
- Output 1 to 3 candidates, sorted by score descending
- Intent names must exactly match the tree (no invention)
- workflow_type: copy from the tree annotation if the intent has one; use "" if none
- Score guide: very confident 0.85-0.95, fairly confident 0.65-0.84, uncertain 0.40-0.64
- Focus on the ACTION (what the user wants to do) and the OBJECT (what they want to do it to)
- Key test: does the output require DESIGN DECISIONS (audience, structure, style)? If yes → workflow. If the output is deterministic from the input (translate, summarize, convert) → no workflow.
- "基于文档" does NOT mean "content processing" — "基于文档做PPT" still needs audience targeting + content architecture + visual design → office (workflow_type="presentation_design")
- Short action phrases (≤5 chars) like "继续"/"开工"/"go ahead" → continuation
- "生成/制作/设计 PPT" or "基于X做PPT" = needs design decisions → office (workflow_type="presentation_design")
- "打开桌面上的 PPT/PDF" = open an existing local document → document_open (no workflow)
- "把已有文件发到指定邮箱/路径" = document_delivery; viewing attached content → document_read
- web_fetch requires a concrete URL in the user message. Never use web_fetch for a city, weather, price, or other open-ended request without a URL; those current facts are live_data.
- A composite request may keep a lookup label (search / live_data) together with document_generate when the user wants current facts rendered as a PDF. Same-domain exclusivity still applies (document_generate vs opening an existing file; document_generate vs workflow_task).
- Current externally acquired facts rendered as a PDF → live_data + document_generate, workflow_type=""
- "帮我写一份研究报告" → workflow_task (multi-phase research), not document_generate
- When a message is genuinely ambiguous without context, give the top candidate a lower score (0.50-0.65) rather than forcing high confidence`

// BuildTreePrompt constructs the full LLM prompt for tree reasoning.
func BuildTreePrompt(treeText, message string) string {
	return fmt.Sprintf(intentTreePromptTemplate, treeText, message)
}

// TreeResponseFormat returns the OpenAI Chat Completions JSON-schema contract
// for Layer 3 tree classification.  It is derived from the intent taxonomy so
// every host sends the same machine-readable protocol and provider tool names
// can never be mistaken for intent labels.
//
// The response uses the historical "skill" field for wire compatibility with
// ParseTreeResponse; its values are taxonomy labels, not executable tool IDs.
func TreeResponseFormat() map[string]interface{} {
	labels := make([]string, 0)
	seenLabels := make(map[string]struct{})
	workflowTypesByLabel := make(map[string][]string)

	for _, def := range DefaultDefinitions() {
		if def.Label.IsValid() {
			label := string(def.Label)
			if _, exists := seenLabels[label]; !exists {
				seenLabels[label] = struct{}{}
				labels = append(labels, label)
			}
			workflowTypes := []string{""}
			seenWorkflowTypes := map[string]struct{}{"": {}}
			for _, workflowType := range def.WorkflowTypes {
				workflowType = strings.TrimSpace(workflowType)
				if _, exists := seenWorkflowTypes[workflowType]; workflowType == "" || exists {
					continue
				}
				seenWorkflowTypes[workflowType] = struct{}{}
				workflowTypes = append(workflowTypes, workflowType)
			}
			sort.Strings(workflowTypes)
			workflowTypesByLabel[label] = workflowTypes
		}
	}
	sort.Strings(labels)

	// JSON Schema has no simple cross-field enum.  A global workflow_type enum
	// would accept an invalid pairing such as skill="coding" with
	// workflow_type="contract_review".  Encode each valid pair as a branch so
	// structured-output-capable providers enforce the same invariant as the
	// parser below.
	itemAlternatives := make([]interface{}, 0, len(labels))
	for _, label := range labels {
		workflowTypes := workflowTypesByLabel[label]
		itemAlternatives = append(itemAlternatives, map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"skill", "score", "workflow_type"},
			"properties": map[string]interface{}{
				"skill":         map[string]interface{}{"type": "string", "enum": []string{label}},
				"score":         map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
				"workflow_type": map[string]interface{}{"type": "string", "enum": workflowTypes},
			},
		})
	}

	return map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "intent_tree_candidates",
			"strict": true,
			"schema": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"top"},
				"properties": map[string]interface{}{
					"top": map[string]interface{}{
						"type":     "array",
						"minItems": 1,
						"maxItems": 3,
						"items":    map[string]interface{}{"anyOf": itemAlternatives},
					},
				},
			},
		},
	}
}

// ParseTreeResponse parses the LLM response into TreeCandidate list.
// Handles: <think>...</think> + JSON, plain JSON, JSON in prose.
func ParseTreeResponse(text string) []TreeCandidate {
	// Strip <think> block if present.
	if idx := strings.LastIndex(text, "</think>"); idx >= 0 {
		text = text[idx+len("</think>"):]
	}
	text = strings.TrimSpace(text)

	// Try direct parse.
	if candidates := tryParseTreeJSON(text); len(candidates) > 0 {
		return candidates
	}

	// Try extracting JSON object from prose.
	if start := strings.Index(text, "{"); start >= 0 {
		// Find matching closing brace.
		depth := 0
		for i := start; i < len(text); i++ {
			switch text[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					if candidates := tryParseTreeJSON(text[start : i+1]); len(candidates) > 0 {
						return candidates
					}
				}
			}
		}
	}

	log.Printf("[intent-tree] failed to parse LLM response: %.200s", text)
	return nil
}

// tryParseTreeJSON attempts to parse a JSON string into TreeCandidate list.
func tryParseTreeJSON(s string) []TreeCandidate {
	var data struct {
		Top []struct {
			Skill        string  `json:"skill"`
			Score        float64 `json:"score"`
			Reason       string  `json:"reason"`
			WorkflowType string  `json:"workflow_type"`
		} `json:"top"`
	}

	if err := json.Unmarshal([]byte(s), &data); err != nil {
		return nil
	}

	candidates := make([]TreeCandidate, 0, len(data.Top))
	for _, item := range data.Top {
		label := IntentLabel(item.Skill)
		if !label.IsValid() {
			continue
		}
		workflowType := strings.TrimSpace(item.WorkflowType)
		if !workflowTypeAllowed(label, workflowType) {
			continue
		}
		score := item.Score
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		candidates = append(candidates, TreeCandidate{
			Label:        label,
			Score:        score,
			Reason:       item.Reason,
			WorkflowType: workflowType,
		})
	}
	return candidates
}

func workflowTypeAllowed(label IntentLabel, workflowType string) bool {
	if workflowType == "" {
		return true
	}
	for _, def := range DefaultDefinitions() {
		if def.Label != label {
			continue
		}
		for _, allowed := range def.WorkflowTypes {
			if workflowType == allowed {
				return true
			}
		}
		return false
	}
	return false
}

// ClassifyByTree runs intent tree reasoning via a single LLM call.
// Returns up to 3 TreeCandidate objects sorted by score descending.
// The treeText parameter should be pre-built via BuildIntentTreeText
// to avoid rebuilding on every call.
func ClassifyByTree(llmFunc LLMClassifyFunc, treeText, message string) ([]TreeCandidate, error) {
	return ClassifyByTreeContext(context.Background(), nil, llmFunc, treeText, message)
}

// ClassifyByTreeContext runs tree reasoning with a caller-owned cancellation
// context. The original callback remains supported for compatibility.
func ClassifyByTreeContext(ctx context.Context, llmContextFunc LLMClassifyContextFunc, llmFunc LLMClassifyFunc, treeText, message string) ([]TreeCandidate, error) {
	if llmContextFunc == nil && llmFunc == nil {
		return nil, fmt.Errorf("LLM classify function is nil")
	}

	prompt := BuildTreePrompt(treeText, message)

	// Send the complete tree prompt as the "system" role and the raw user
	// message as the "user" role. The LLMClassifyFunc callback constructs
	// a [system, user] message pair. The tree prompt contains the intent
	// tree, instructions, and the quoted user message for CoT reasoning;
	// the user role carries the raw message for the LLM's attention.
	var response string
	var err error
	if llmContextFunc != nil {
		response, err = llmContextFunc(ctx, prompt, message)
	} else {
		response, err = llmFunc(prompt, message)
	}
	if err != nil {
		return nil, fmt.Errorf("tree reasoning LLM call failed: %w", err)
	}

	candidates := ParseTreeResponse(response)
	if len(candidates) == 0 {
		return nil, &TreeResponseProtocolError{}
	}

	return candidates, nil
}
