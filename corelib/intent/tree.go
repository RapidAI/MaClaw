package intent

import (
	"encoding/json"
	"fmt"
	"log"
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
	Label  IntentLabel
	Score  float64
	Reason string
}

// BuildIntentTreeText constructs a domain-grouped intent tree prompt from
// IntentDefinition entries. Within each domain, intents are listed together
// so the LLM naturally compares them. For domains with 2+ intents, a
// disambiguation note is auto-appended listing the sibling intents,
// so the LLM knows what alternatives exist without hardcoded "区别于 X" hints.
//
// Output format:
//
//	── Coding ──
//	  coding: user wants to create new software...
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
		// Auto-generate disambiguation note for domains with multiple intents.
		if len(e.labels) >= 2 {
			parts = append(parts, fmt.Sprintf("  (Note: these are mutually exclusive — pick the single best match among %s)",
				strings.Join(e.labels, "/")))
		}
		parts = append(parts, "")
	}

	return strings.Join(parts, "\n")
}

// intentTreePromptTemplate is the LLM prompt for tree reasoning.
// Uses chain-of-thought inside <think> tags, then outputs structured JSON.
const intentTreePromptTemplate = `You are an intent classifier. Given the user message, select the best matching intents from the intent tree below.

## Intent Tree
%s

## User Message
"%s"

## Instructions
First reason inside <think> tags:
1. What does the user want? (action + object)
2. Which domain is most likely?
3. Which intent in that domain fits best? What did you rule out?

Then output JSON:
{"top": [{"skill": "intent_name", "score": 0.0-1.0}, ...]}

Rules:
- Output exactly 3 candidates, sorted by score descending
- Intent names must exactly match the tree (no invention)
- Score guide: very confident 0.85-0.95, fairly confident 0.65-0.84, uncertain 0.40-0.64
- Focus on the ACTION (what the user wants to do) and the OBJECT (what they want to do it to)
- Short action phrases (≤5 chars) like "继续"/"开工"/"go ahead" → continuation
- When a message is genuinely ambiguous without context, give the top candidate a lower score (0.50-0.65) rather than forcing high confidence`

// BuildTreePrompt constructs the full LLM prompt for tree reasoning.
func BuildTreePrompt(treeText, message string) string {
	return fmt.Sprintf(intentTreePromptTemplate, treeText, message)
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
			Skill  string  `json:"skill"`
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
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
		score := item.Score
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		candidates = append(candidates, TreeCandidate{
			Label:  label,
			Score:  score,
			Reason: item.Reason,
		})
	}
	return candidates
}

// ClassifyByTree runs intent tree reasoning via a single LLM call.
// Returns up to 3 TreeCandidate objects sorted by score descending.
// The treeText parameter should be pre-built via BuildIntentTreeText
// to avoid rebuilding on every call.
func ClassifyByTree(llmFunc LLMClassifyFunc, treeText, message string) ([]TreeCandidate, error) {
	if llmFunc == nil {
		return nil, fmt.Errorf("LLM classify function is nil")
	}

	prompt := BuildTreePrompt(treeText, message)

	// Send the complete tree prompt as the "system" role and the raw user
	// message as the "user" role. The LLMClassifyFunc callback constructs
	// a [system, user] message pair. The tree prompt contains the intent
	// tree, instructions, and the quoted user message for CoT reasoning;
	// the user role carries the raw message for the LLM's attention.
	response, err := llmFunc(prompt, message)
	if err != nil {
		return nil, fmt.Errorf("tree reasoning LLM call failed: %w", err)
	}

	candidates := ParseTreeResponse(response)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("tree reasoning returned no valid candidates")
	}

	return candidates, nil
}
