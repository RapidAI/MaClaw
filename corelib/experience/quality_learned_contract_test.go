package experience

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Regression guards for learned-skill generation quality: verbatim session
// recordings with misread placeholders or many unreplaced paths must not pass
// the quality gate (the craft-generate-pdf failure class).

func TestEvaluatePatternQuality_MisreadSingleLetterArgsPenalized(t *testing.T) {
	p := Pattern{
		Name:        "convert-book",
		Description: "Convert a markdown book into a professionally typeset PDF via pandoc and PyMuPDF.",
		Triggers:    []string{"convert book", "make pdf", "typeset book"},
		Steps: []Step{
			{Action: "bash", Params: map[string]interface{}{
				"command": `pwsh -Command "\"{0}: {1}\" -f $a, $b"`,
			}},
			{Action: "bash", Params: map[string]interface{}{
				"command": `for f in *.md; do pandoc "{{i}}" -o out.pdf; done`,
			}},
		},
	}
	report := EvaluatePatternQuality(p)
	for _, reason := range report.Reasons {
		if strings.Contains(reason, "misread shell syntax") {
			return
		}
	}
	t.Fatalf("expected misread-shell-syntax penalty, got %+v", report)
}

func TestEvaluatePatternQuality_SessionSnapshotFailsGate(t *testing.T) {
	p := Pattern{
		Name:        "make-pdf",
		Description: "Generate a PDF from the math handbook sources with figures and bookmarks.",
		Triggers:    []string{"make pdf", "generate pdf", "build pdf"},
		Steps: []Step{
			{Action: "bash", Params: map[string]interface{}{"command": `pandoc C:\Users\ma139\books\math\ch1.md -o C:\Users\ma139\books\math\out.pdf`}},
			{Action: "bash", Params: map[string]interface{}{"command": `python C:\Users\ma139\books\math\post.py --src C:\Users\ma139\books\math\out.pdf`}},
			{Action: "bash", Params: map[string]interface{}{"command": `copy C:\Users\ma139\books\math\out.pdf D:\exports\math\final.pdf`}},
		},
	}
	report := EvaluatePatternQuality(p)
	foundSnapshot := false
	for _, reason := range report.Reasons {
		if strings.Contains(reason, "session snapshot") {
			foundSnapshot = true
		}
	}
	if !foundSnapshot {
		t.Fatalf("expected session-snapshot penalty, got %+v", report)
	}
	if report.Passes() {
		t.Fatalf("verbatim multi-path recording must not pass the gate: %+v", report)
	}
}

func TestEvaluatePatternQuality_TemplatizedWorkflowStillPasses(t *testing.T) {
	p := Pattern{
		Name:        "convert-book-pdf",
		Description: "Convert a markdown book directory into a typeset PDF via pandoc and a post-processing script.",
		Triggers:    []string{"convert book", "make pdf", "typeset book"},
		Steps: []Step{
			{Action: "bash", Params: map[string]interface{}{
				"command": `pandoc "{{input_md}}" -o "{{output_pdf}}"`,
			}},
			{Action: "bash", Params: map[string]interface{}{
				"command": `python postprocess.py --src "{{output_pdf}}"`,
			}},
		},
	}
	report := EvaluatePatternQuality(p)
	if !report.Passes() {
		t.Fatalf("clean templatized workflow must pass: %+v", report)
	}
}

func TestSynthesizeSkillParams_FallbackNeverRequired(t *testing.T) {
	// SynthesizeParams drops ${i}-style keys entirely; the fallback must not
	// resurrect them as required params.
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": `for f in *.md; do pandoc "${i}" -o out.pdf; done`,
		}},
	}
	params := synthesizeSkillParams(steps, []string{"i"})
	for _, p := range params {
		if p.Required {
			t.Fatalf("inferred param %q must not be required", p.Name)
		}
	}
}
