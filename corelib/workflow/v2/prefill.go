package v2

import "strings"

// PrefilledValue represents a pre-filled form field value with provenance tracking.
// Only values with a verifiable source are populated — no LLM inference/hallucination.
type PrefilledValue struct {
	Value        interface{} `json:"value"`                   // the pre-filled value
	Source       string      `json:"source"`                  // "context" | "memory" | "knowledge" | "resume" | "web"
	SourceDetail string      `json:"source_detail,omitempty"` // memory entry summary / URL / quote from message
	Confidence   float64     `json:"confidence"`              // 0-1, for UI display hints
	NeedsConfirm bool        `json:"needs_confirm"`           // true = user must explicitly confirm (web source)
}

// noPrefillFieldNames lists field names that should never be auto-prefilled.
// These are either: security-sensitive, file paths, or core creative fields
// that only the user can provide for this specific task.
var noPrefillFieldNames = map[string]bool{
	// File/material input fields
	"material_path": true, "material_text": true,
	"contract_path": true, "contract_text": true,
	"bid_doc_path":  true, "bid_doc_text": true,
	// Sensitive
	"ssh_password": true,
	// Task-specific creative fields that must be user-authored
	"project_title":    true,
	"paper_topic":      true,
	"core_question":    true,
	"key_contribution": true,
	"hypothesis":       true,
	"paper_title":      true,
	"paper_url":        true,
	"work_dir":         true,
}

// ShouldPrefill returns whether a field is eligible for auto-prefill.
// A field is eligible if it is not in the noPrefillFieldNames blacklist.
// For recall-based prefill (Phase 2), use ShouldRecallPrefill which additionally
// checks the Reusable flag.
func ShouldPrefill(fieldName string) bool {
	return !noPrefillFieldNames[fieldName]
}

// ShouldRecallPrefill returns whether a field should be actively recalled from
// memory/knowledge for prefill. This is a stricter check than ShouldPrefill:
// the field must not be blacklisted AND must be declared Reusable by the template.
//
// Design: templates are the single source of truth for what constitutes "stable
// personal information worth recalling". This replaces the old hardcoded
// sedimentableFields whitelist in the GUI layer.
//
// For backward compatibility: if no field in the schema has Reusable=true
// (legacy templates not yet annotated), all non-blacklisted fields remain
// eligible for recall (old behavior). This ensures existing templates don't
// regress while new templates get precise control.
func ShouldRecallPrefill(field PhaseInputField, schemaHasAnyReusable bool) bool {
	if noPrefillFieldNames[field.Name] {
		return false
	}
	// If the schema has at least one Reusable field, use Reusable as the gate.
	// Otherwise (legacy schema), allow all non-blacklisted fields (old behavior).
	if schemaHasAnyReusable {
		return field.Reusable
	}
	return true
}

// SchemaHasReusableFields checks if any field in the schema declares Reusable=true.
// Used to distinguish annotated templates from legacy unannotated ones.
func SchemaHasReusableFields(schema *PhaseInputSchema) bool {
	if schema == nil {
		return false
	}
	for _, f := range schema.Fields {
		if f.Reusable {
			return true
		}
	}
	return false
}

// ShouldSediment returns whether a field's value should be sedimented to long-term
// memory after form submission. Same logic as ShouldRecallPrefill — the Reusable
// flag is the single source of truth for both directions (recall ← → sediment).
func ShouldSediment(field PhaseInputField, schemaHasAnyReusable bool) bool {
	return ShouldRecallPrefill(field, schemaHasAnyReusable)
}

// PrefillFromContext extracts form field values from the user's message text
// and recent conversation context. Only exact matches are extracted — no inference.
//
// Parameters:
//   - schema: the phase's InputSchema defining expected fields
//   - userMessage: the current user message that triggered the workflow
//   - contextTexts: recent conversation history texts (user + assistant, last N turns)
//
// Returns a map of field name → PrefilledValue for fields that were matched.
// Fields not matched are absent from the map (not set to nil/empty).
func PrefillFromContext(schema *PhaseInputSchema, userMessage string, contextTexts []string) map[string]*PrefilledValue {
	if schema == nil || len(schema.Fields) == 0 {
		return nil
	}

	// Combine all context into a single searchable text block
	var sb strings.Builder
	if userMessage != "" {
		sb.WriteString(userMessage)
	}
	for _, t := range contextTexts {
		if t != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(t)
		}
	}
	allContext := sb.String()
	if allContext == "" {
		return nil
	}

	result := make(map[string]*PrefilledValue)

	for _, field := range schema.Fields {
		if !ShouldPrefill(field.Name) {
			continue
		}

		// Try to extract a value for this field from the context
		if pv := extractFieldFromContext(field, allContext); pv != nil {
			result[field.Name] = pv
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
