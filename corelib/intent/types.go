// Package intent provides a unified intent classification service for user messages.
// It replaces scattered keyword-based classification modules with semantic
// embedding and LLM classification. Keyword registries may still support
// diagnostics or candidate recall, but they are not an execution-route authority.
package intent

import "context"

// IntentLabel represents a classified user intent.
type IntentLabel string

const (
	LabelCoding           IntentLabel = "coding"
	LabelSSH              IntentLabel = "ssh"
	LabelNonCoding        IntentLabel = "non_coding"
	LabelBrowser          IntentLabel = "browser"
	LabelSearch           IntentLabel = "search"
	LabelLiveData         IntentLabel = "live_data"
	LabelDocumentDelivery IntentLabel = "document_delivery"
	LabelBusinessData     IntentLabel = "business_data"
	LabelBugFix           IntentLabel = "bug_fix"
	LabelContinuation     IntentLabel = "continuation"
	LabelMaintenance      IntentLabel = "maintenance"
	LabelOffice           IntentLabel = "office"
	LabelComputerUse      IntentLabel = "computer_use"
	LabelKnowledgeWrite   IntentLabel = "knowledge_write"
	LabelCurrentTime      IntentLabel = "current_time"
	LabelWorkflowTask     IntentLabel = "workflow_task"
	LabelAmbiguous        IntentLabel = "ambiguous"
	LabelUnknown          IntentLabel = "unknown"
)

// AllLabels returns the complete set of valid intent labels.
func AllLabels() []IntentLabel {
	return []IntentLabel{
		LabelCoding,
		LabelSSH,
		LabelNonCoding,
		LabelBrowser,
		LabelSearch,
		LabelLiveData,
		LabelDocumentDelivery,
		LabelBusinessData,
		LabelBugFix,
		LabelContinuation,
		LabelMaintenance,
		LabelOffice,
		LabelComputerUse,
		LabelKnowledgeWrite,
		LabelCurrentTime,
		LabelWorkflowTask,
		LabelAmbiguous,
		LabelUnknown,
	}
}

// validLabels is a pre-computed set for O(1) IsValid lookups.
var validLabels = func() map[IntentLabel]bool {
	m := make(map[IntentLabel]bool, len(AllLabels()))
	for _, l := range AllLabels() {
		m[l] = true
	}
	return m
}()

// IsValid returns true if the label is in the taxonomy.
func (l IntentLabel) IsValid() bool {
	return validLabels[l]
}

// KeywordStrength indicates how strongly a keyword can annotate recall evidence.
type KeywordStrength int

const (
	Strong KeywordStrength = iota // strong recall evidence; not an execution-route decision
	Weak                          // weak recall evidence; requires semantic confirmation
)

// KeywordEntry is a single entry in the keyword registry.
type KeywordEntry struct {
	Keyword  string
	Label    IntentLabel
	Strength KeywordStrength
	Creation bool // true for creation-oriented coding recall evidence
}

// ClassificationResult is the structured output of the UIC.
type ClassificationResult struct {
	Primary          IntentLabel   // exactly one primary intent
	Confidence       float64       // [0, 1]
	Secondary        []IntentLabel // zero or more secondary intents
	ToolNames        []string      // tool names to activate (from Tool Affinity)
	Layer            int           // 1, 2, or 3 (23 = fusion of L2+L3)
	Reason           string        // human-readable explanation
	CreationOriented bool          // true when the coding intent is creation-oriented (new project/feature)

	// WorkflowType is the workflow template type determined by L3 tree reasoning
	// or inferred from IntentDefinition in degraded mode.
	// Non-empty when the intent maps to a multi-phase workflow (e.g., "coding",
	// "presentation_design", "product_design"). Empty string means no workflow.
	// This eliminates the need for a separate IUM LLM call to determine workflow type.
	WorkflowType string

	// Degraded is true when the classification was produced in degraded mode
	// (one or both fusion channels failed). Consumers can use this to adjust
	// confidence thresholds.
	Degraded bool
}

// MessageContext is the input to the classifier.
type MessageContext struct {
	Text          string   // current user message text
	UserID        string   // for conversation context lookup
	RecentHistory []string // recent conversation messages (for continuation detection)
}

// LLMClassifyFunc is a callback for Layer 3 LLM classification.
// The caller (gui/) provides this based on their LLM config.
// Must respect the provided timeout via context.
type LLMClassifyFunc func(systemPrompt, userText string) (string, error)

// LLMClassifyContextFunc lets latency-sensitive callers cancel the underlying
// LLM transport when a classification deadline has expired.
type LLMClassifyContextFunc func(ctx context.Context, systemPrompt, userText string) (string, error)
