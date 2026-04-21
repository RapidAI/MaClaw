package intent

// ToTaskIntent maps a ClassificationResult to the legacy taskIntentResult
// fields used by gui/im_intent_classifier.go.
// Returns (intent string, matched string, evidence []string, reason string, confidence float64).
//
// Mapping:
//   - coding, bug_fix, maintenance → "coding"
//   - ssh → "ssh"
//   - non_coding, browser, search, document_delivery, office → "non_coding"
//   - ambiguous, unknown, continuation → "ambiguous"
func (r *ClassificationResult) ToTaskIntent() (intent, matched string, evidence []string, reason string, confidence float64) {
	switch r.Primary {
	case LabelCoding, LabelBugFix, LabelMaintenance:
		intent = "coding"
	case LabelSSH:
		intent = "ssh"
	case LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelOffice:
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
//   - coding (with creation signals) → "new_project"
//   - coding (without creation signals) → "maintenance"
//   - bug_fix → "bug_fix"
//   - maintenance → "maintenance"
//   - non_coding, search, document_delivery, office, browser → "non_coding"
//   - continuation → "continuation"
//   - ambiguous, unknown → "unknown"
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
	case LabelNonCoding, LabelSearch, LabelDocumentDelivery, LabelOffice, LabelBrowser:
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
// (non_coding, browser, search, document_delivery, or office).
func (r *ClassificationResult) IsNonCodingLike() bool {
	switch r.Primary {
	case LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelOffice:
		return true
	}
	return false
}

// hasCreationSignals checks whether the classification result indicates a
// creation-oriented coding task.
//
// Decision logic:
//  1. If CreationOriented is explicitly true (set by L1 keywords or fusion) → true.
//  2. If secondary contains bug_fix or maintenance → definitely not creation → false.
//  3. If the result came from Layer 1 (which has authoritative keyword data and
//     explicitly sets CreationOriented), trust its false value → false.
//  4. For Layer 2/fusion results where CreationOriented wasn't set, the absence
//     of counter-signals (bug_fix/maintenance) implies creation → true.
func hasCreationSignals(r *ClassificationResult) bool {
	if r.CreationOriented {
		return true
	}
	for _, s := range r.Secondary {
		if s == LabelBugFix || s == LabelMaintenance {
			return false
		}
	}
	// Layer 1 explicitly computed CreationOriented from keyword tags.
	// If it's false, trust it — the user's keywords were not creation-oriented.
	if r.Layer == 1 {
		return false
	}
	// Layer 2/3/fusion: no keyword-level creation data available.
	// Default to creation when no counter-signals exist.
	return true
}
