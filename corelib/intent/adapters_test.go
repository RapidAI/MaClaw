package intent

import "testing"

func TestToTaskIntent_CodingLabels(t *testing.T) {
	for _, label := range []IntentLabel{LabelCoding, LabelBugFix, LabelMaintenance} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.92,
			Secondary:  []IntentLabel{LabelCoding},
			Layer:      1,
			Reason:     "keyword match",
		}
		intent, matched, evidence, reason, conf := r.ToTaskIntent()
		if intent != "coding" {
			t.Errorf("ToTaskIntent(%s): got intent=%q, want %q", label, intent, "coding")
		}
		if matched != string(label) {
			t.Errorf("ToTaskIntent(%s): got matched=%q, want %q", label, matched, string(label))
		}
		if len(evidence) != 1 || evidence[0] != string(LabelCoding) {
			t.Errorf("ToTaskIntent(%s): got evidence=%v, want [%q]", label, evidence, string(LabelCoding))
		}
		if reason != "keyword match" {
			t.Errorf("ToTaskIntent(%s): got reason=%q, want %q", label, reason, "keyword match")
		}
		if conf != 0.92 {
			t.Errorf("ToTaskIntent(%s): got confidence=%f, want 0.92", label, conf)
		}
	}
}

func TestToTaskIntent_SSH(t *testing.T) {
	r := &ClassificationResult{
		Primary:    LabelSSH,
		Confidence: 0.95,
		Layer:      1,
		Reason:     "ssh keyword",
	}
	intent, matched, evidence, _, _ := r.ToTaskIntent()
	if intent != "ssh" {
		t.Errorf("got intent=%q, want %q", intent, "ssh")
	}
	if matched != "ssh" {
		t.Errorf("got matched=%q, want %q", matched, "ssh")
	}
	if len(evidence) != 0 {
		t.Errorf("got evidence=%v, want empty", evidence)
	}
}

func TestToTaskIntent_LowConfidenceExecutableLabelsBecomeAmbiguous(t *testing.T) {
	for _, label := range []IntentLabel{LabelCoding, LabelBugFix, LabelMaintenance, LabelSSH} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.50,
			Layer:      1,
			Reason:     "weak keyword only",
		}
		intent, matched, _, _, conf := r.ToTaskIntent()
		if intent != "ambiguous" {
			t.Errorf("ToTaskIntent(%s): got intent=%q, want ambiguous", label, intent)
		}
		if matched != string(label) {
			t.Errorf("ToTaskIntent(%s): got matched=%q, want %q", label, matched, string(label))
		}
		if conf != 0.50 {
			t.Errorf("ToTaskIntent(%s): got confidence=%f, want 0.50", label, conf)
		}
	}
}

func TestToTaskIntent_NonCodingLabels(t *testing.T) {
	for _, label := range []IntentLabel{LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelOffice, LabelCurrentTime, LabelLiveData} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.88,
			Layer:      2,
			Reason:     "embedding match",
		}
		intent, _, _, _, _ := r.ToTaskIntent()
		if intent != "non_coding" {
			t.Errorf("ToTaskIntent(%s): got intent=%q, want %q", label, intent, "non_coding")
		}
	}
}

func TestToTaskIntent_AmbiguousLabels(t *testing.T) {
	for _, label := range []IntentLabel{LabelAmbiguous, LabelUnknown, LabelContinuation} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.50,
			Layer:      1,
			Reason:     "low confidence",
		}
		intent, _, _, _, _ := r.ToTaskIntent()
		if intent != "ambiguous" {
			t.Errorf("ToTaskIntent(%s): got intent=%q, want %q", label, intent, "ambiguous")
		}
	}
}

func TestToGateIntent_CodingWithCreation(t *testing.T) {
	r := &ClassificationResult{
		Primary:          LabelCoding,
		Confidence:       0.92,
		Secondary:        []IntentLabel{},
		Layer:            1,
		Reason:           "keyword match: coding (strong=2, weak=0)",
		CreationOriented: true,
	}
	intent, conf, gap, layer, reason := r.ToGateIntent()
	if intent != "new_project" {
		t.Errorf("got intent=%q, want %q", intent, "new_project")
	}
	if conf != 0.92 {
		t.Errorf("got confidence=%f, want 0.92", conf)
	}
	if gap != 0 {
		t.Errorf("got gap=%f, want 0", gap)
	}
	if layer != 1 {
		t.Errorf("got layer=%d, want 1", layer)
	}
	if reason != "keyword match: coding (strong=2, weak=0)" {
		t.Errorf("got reason=%q, want %q", reason, "keyword match: coding (strong=2, weak=0)")
	}
}

func TestToGateIntent_CodingWithoutCreation(t *testing.T) {
	// When secondary contains bug_fix or maintenance, it's not creation
	r := &ClassificationResult{
		Primary:    LabelCoding,
		Confidence: 0.85,
		Secondary:  []IntentLabel{LabelBugFix},
		Layer:      2,
		Reason:     "embedding match",
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "maintenance" {
		t.Errorf("got intent=%q, want %q", intent, "maintenance")
	}
}

func TestToGateIntent_BugFix(t *testing.T) {
	r := &ClassificationResult{
		Primary:    LabelBugFix,
		Confidence: 0.90,
		Layer:      1,
		Reason:     "bug fix keywords",
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "bug_fix" {
		t.Errorf("got intent=%q, want %q", intent, "bug_fix")
	}
}

func TestToGateIntent_Maintenance(t *testing.T) {
	r := &ClassificationResult{
		Primary:    LabelMaintenance,
		Confidence: 0.88,
		Layer:      1,
		Reason:     "maintenance keywords",
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "maintenance" {
		t.Errorf("got intent=%q, want %q", intent, "maintenance")
	}
}

func TestToGateIntent_NonCodingLabels(t *testing.T) {
	for _, label := range []IntentLabel{LabelNonCoding, LabelSearch, LabelDocumentDelivery, LabelOffice, LabelBrowser, LabelCurrentTime, LabelLiveData} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.80,
			Layer:      2,
			Reason:     "test",
		}
		intent, _, _, _, _ := r.ToGateIntent()
		if intent != "non_coding" {
			t.Errorf("ToGateIntent(%s): got intent=%q, want %q", label, intent, "non_coding")
		}
	}
}

func TestToGateIntent_Continuation(t *testing.T) {
	r := &ClassificationResult{
		Primary:    LabelContinuation,
		Confidence: 0.75,
		Layer:      1,
		Reason:     "continuation phrase",
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "continuation" {
		t.Errorf("got intent=%q, want %q", intent, "continuation")
	}
}

func TestToGateIntent_AmbiguousUnknown(t *testing.T) {
	for _, label := range []IntentLabel{LabelAmbiguous, LabelUnknown} {
		r := &ClassificationResult{
			Primary:    label,
			Confidence: 0.40,
			Layer:      3,
			Reason:     "llm uncertain",
		}
		intent, _, _, _, _ := r.ToGateIntent()
		if intent != "unknown" {
			t.Errorf("ToGateIntent(%s): got intent=%q, want %q", label, intent, "unknown")
		}
	}
}

func TestIsCodingLike(t *testing.T) {
	codingLabels := []IntentLabel{LabelCoding, LabelBugFix, LabelMaintenance}
	for _, label := range codingLabels {
		r := &ClassificationResult{Primary: label}
		if !r.IsCodingLike() {
			t.Errorf("IsCodingLike(%s): got false, want true", label)
		}
	}

	nonCodingLabels := []IntentLabel{LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelOffice, LabelCurrentTime, LabelLiveData, LabelSSH, LabelContinuation, LabelAmbiguous, LabelUnknown}
	for _, label := range nonCodingLabels {
		r := &ClassificationResult{Primary: label}
		if r.IsCodingLike() {
			t.Errorf("IsCodingLike(%s): got true, want false", label)
		}
	}
}

func TestIsNonCodingLike(t *testing.T) {
	nonCodingLabels := []IntentLabel{LabelNonCoding, LabelBrowser, LabelSearch, LabelDocumentDelivery, LabelOffice, LabelCurrentTime, LabelLiveData}
	for _, label := range nonCodingLabels {
		r := &ClassificationResult{Primary: label}
		if !r.IsNonCodingLike() {
			t.Errorf("IsNonCodingLike(%s): got false, want true", label)
		}
	}

	otherLabels := []IntentLabel{LabelCoding, LabelBugFix, LabelMaintenance, LabelSSH, LabelContinuation, LabelAmbiguous, LabelUnknown}
	for _, label := range otherLabels {
		r := &ClassificationResult{Primary: label}
		if r.IsNonCodingLike() {
			t.Errorf("IsNonCodingLike(%s): got true, want false", label)
		}
	}
}

func TestHasCreationSignals_CreationOrientedTrue(t *testing.T) {
	r := &ClassificationResult{
		Primary:          LabelCoding,
		Layer:            1,
		CreationOriented: true,
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "new_project" {
		t.Errorf("got intent=%q, want %q (CreationOriented=true)", intent, "new_project")
	}
}

func TestHasCreationSignals_NoBugFixOrMaintenance_DefaultsToCreation(t *testing.T) {
	// Without counter-signals (bug_fix/maintenance in secondary),
	// hasCreationSignals defaults to true regardless of layer.
	// This is the conservative behavior — the gate activates for ambiguous
	// coding tasks.
	r := &ClassificationResult{
		Primary:          LabelCoding,
		Layer:            1,
		CreationOriented: false,
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "new_project" {
		t.Errorf("got intent=%q, want %q (no counter-signals → default to creation)", intent, "new_project")
	}
}

func TestHasCreationSignals_Layer2DefaultsToCreation(t *testing.T) {
	// Layer 2 doesn't set CreationOriented — no counter-signals → default to creation
	r := &ClassificationResult{
		Primary:   LabelCoding,
		Secondary: []IntentLabel{LabelSearch},
		Layer:     2,
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "new_project" {
		t.Errorf("got intent=%q, want %q (Layer 2, no counter-signals)", intent, "new_project")
	}
}

func TestHasCreationSignals_SecondaryHasMaintenance(t *testing.T) {
	r := &ClassificationResult{
		Primary:   LabelCoding,
		Secondary: []IntentLabel{LabelMaintenance},
		Reason:    "coding keywords",
	}
	intent, _, _, _, _ := r.ToGateIntent()
	if intent != "maintenance" {
		t.Errorf("got intent=%q, want %q (maintenance in secondary)", intent, "maintenance")
	}
}
