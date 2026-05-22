package intent

// ToTaskIntent maps a ClassificationResult to the legacy taskIntentResult
// fields used by gui/im_intent_classifier.go.
// Returns (intent string, matched string, evidence []string, reason string, confidence float64).
//
// Mapping:
//   - coding, bug_fix, maintenance -> "coding"
//   - ssh -> "ssh"
//   - non_coding, browser, search, document_delivery, business_data, office, knowledge_write -> "non_coding"
//   - ambiguous, unknown, continuation -> "ambiguous"
func (r *ClassificationResult) ToTaskIntent() (intent, matched string, evidence []string, reason string, confidence float64) {
	const executableIntentThreshold = 0.90

	switch r.Primary {
	case LabelCoding, LabelBugFix, LabelMaintenance:
		if r.Confidence >= executableIntentThreshold {
			intent = "coding"
		} else {
			intent = "ambiguous"
		}
	case LabelSSH:
		if r.Confidence >= executableIntentThreshold {
			intent = "ssh"
		} else {
			intent = "ambiguous"
		}
	case LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelBusinessData, LabelOffice, LabelKnowledgeWrite:
		intent = "non_coding"
	default:
		// ambiguous, unknown, continuation
		intent = "ambiguous"
	}

	matched = string(r.Primary)

	evidence = make([]string, 0, len(r.Secondary))
	for _, s := range r.Secondary {
		evidence = append(evidence, string(s))
	}

	reason = r.Reason
	confidence = r.Confidence
	return
}

// ToGateIntent maps a ClassificationResult to the legacy GateIntentResult
// fields used by gui/gate_intent_classifier.go.
// Returns (intent string, confidence float64, gap float64, layer int, reason string).
//
// Mapping:
//   - coding (with creation signals) -> "new_project"
//   - coding (without creation signals) -> "maintenance"
//   - bug_fix -> "bug_fix"
//   - maintenance -> "maintenance"
//   - non_coding, search, document_delivery, business_data, office, knowledge_write, browser, ssh -> "non_coding"
//   - continuation -> "continuation"
//   - ambiguous, unknown -> "unknown"
func (r *ClassificationResult) ToGateIntent() (intent string, confidence float64, gap float64, layer int, reason string) {
	switch r.Primary {
	case LabelCoding:
		if hasCreationSignals(r) {
			intent = "new_project"
		} else {
			intent = "maintenance"
		}
	case LabelBugFix:
		intent = "bug_fix"
	case LabelMaintenance:
		intent = "maintenance"
	case LabelNonCoding, LabelSearch, LabelDocumentDelivery, LabelBusinessData, LabelOffice, LabelKnowledgeWrite, LabelBrowser, LabelSSH:
		intent = "non_coding"
	case LabelContinuation:
		intent = "continuation"
	default:
		// ambiguous, unknown
		intent = "unknown"
	}

	confidence = r.Confidence
	gap = 0 // not available from single result
	layer = r.Layer
	reason = r.Reason
	return
}

// IsCodingLike returns true if the primary intent indicates a coding task
// (coding, bug_fix, or maintenance).
func (r *ClassificationResult) IsCodingLike() bool {
	switch r.Primary {
	case LabelCoding, LabelBugFix, LabelMaintenance:
		return true
	}
	return false
}

// IsNonCodingLike returns true if the primary intent indicates a non-coding task
// (non_coding, browser, search, document_delivery, business_data, office, or knowledge_write).
func (r *ClassificationResult) IsNonCodingLike() bool {
	switch r.Primary {
	case LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelBusinessData, LabelOffice, LabelKnowledgeWrite:
		return true
	}
	return false
}

// hasCreationSignals checks whether the classification result indicates a
// creation-oriented coding task.
//
// Decision logic:
//  1. If CreationOriented is explicitly true (set by fusion from WorkflowType), return true.
//  2. If secondary contains bug_fix or maintenance, return false.
//  3. For fusion results (Layer 23) or tree-only (Layer 3), CreationOriented is
//     derived from WorkflowType="coding". If it was not set, check counter-signals.
//  4. For degraded mode, no reliable creation detection is available,
//     so default to true when no counter-signals exist.
func hasCreationSignals(r *ClassificationResult) bool {
	if r.CreationOriented {
		return true
	}
	for _, s := range r.Secondary {
		if s == LabelBugFix || s == LabelMaintenance {
			return false
		}
	}
	// No explicit creation signal and no counter-signals.
	// Default to true so the gate activates for ambiguous coding tasks.
	return true
}
