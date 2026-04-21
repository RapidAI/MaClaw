// Package intent provides a unified intent classification service for user messages.
// It replaces scattered keyword-based classification modules with a single three-layer
// pipeline (keywords → embedding cosine → LLM).
package intent

// IntentLabel represents a classified user intent.
type IntentLabel string

const (
	LabelCoding           IntentLabel = "coding"
	LabelSSH              IntentLabel = "ssh"
	LabelNonCoding        IntentLabel = "non_coding"
	LabelBrowser          IntentLabel = "browser"
	LabelSearch           IntentLabel = "search"
	LabelDocumentDelivery IntentLabel = "document_delivery"
	LabelBugFix           IntentLabel = "bug_fix"
	LabelContinuation     IntentLabel = "continuation"
	LabelMaintenance      IntentLabel = "maintenance"
	LabelOffice           IntentLabel = "office"
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
		LabelDocumentDelivery,
		LabelBugFix,
		LabelContinuation,
		LabelMaintenance,
		LabelOffice,
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

// KeywordStrength indicates how strongly a keyword signals an intent.
type KeywordStrength int

const (
	Strong KeywordStrength = iota // single match → high confidence
	Weak                          // needs additional signal or Layer 2 confirmation
)

// KeywordEntry is a single entry in the keyword registry.
type KeywordEntry struct {
	Keyword  string
	Label    IntentLabel
	Strength KeywordStrength
	Creation bool // true for creation-oriented coding keywords (开发/创建/实现 etc.)
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
