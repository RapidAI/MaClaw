package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

const sampleScript = `import sys
input_file = sys.argv[1]
# Convert to PDF
from pdfplumber import open as pdf_open
doc = pdf_open(input_file)
print("Done")
`

const sameStructureDifferentParams = `import sys
input_file = sys.argv[1]
# Convert to PDF
from pdfplumber import open as pdf_open
doc = pdf_open(input_file)
print("Finished")
`

const differentStructure = `import subprocess
# Use pandoc instead
subprocess.run(["pandoc", sys.argv[1], "-o", "output.pdf"])
`

func TestRecordCraftSuccess_CreatesCandidate(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "convert {{input}} to PDF"}},
		},
	}

	modified := RecordCraftSuccess(skill, 0, "/tmp/script.py", sampleScript, "python")
	if !modified {
		t.Fatal("expected modified=true")
	}
	if len(skill.SolidificationCandidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(skill.SolidificationCandidates))
	}
	c := skill.SolidificationCandidates[0]
	if c.SuccessCount != 1 {
		t.Errorf("expected success_count=1, got %d", c.SuccessCount)
	}
	if c.Signature == "" || c.Signature == "empty" {
		t.Error("expected non-empty signature")
	}
}

func TestRecordCraftSuccess_SameStructureIncrementsStreak(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "do stuff"}},
		},
	}

	// Two scripts with same structure but different string literals.
	RecordCraftSuccess(skill, 0, "/tmp/v1.py", sampleScript, "python")
	RecordCraftSuccess(skill, 0, "/tmp/v2.py", sameStructureDifferentParams, "python")
	RecordCraftSuccess(skill, 0, "/tmp/v3.py", sampleScript, "python")

	c := skill.SolidificationCandidates[0]
	if c.SuccessCount != 3 {
		t.Errorf("expected success_count=3 (same structure), got %d", c.SuccessCount)
	}
}

func TestRecordCraftSuccess_DifferentStructureResetsStreak(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "do stuff"}},
		},
	}

	RecordCraftSuccess(skill, 0, "/tmp/v1.py", sampleScript, "python")
	RecordCraftSuccess(skill, 0, "/tmp/v2.py", sampleScript, "python")
	// Third call uses a completely different script structure.
	RecordCraftSuccess(skill, 0, "/tmp/v3.py", differentStructure, "python")

	c := skill.SolidificationCandidates[0]
	if c.SuccessCount != 1 {
		t.Errorf("expected success_count=1 (streak reset by different structure), got %d", c.SuccessCount)
	}
}

func TestRecordCraftSuccess_IgnoresNonCraftStep(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}},
		},
	}

	modified := RecordCraftSuccess(skill, 0, "/tmp/script.py", "echo hi", "bash")
	if modified {
		t.Error("should not modify for non-craft_tool step")
	}
}

func TestTrySolidify_PromotesAtThreshold(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "convert {{input}}"}},
		},
	}

	for i := 0; i < SolidificationThreshold; i++ {
		RecordCraftSuccess(skill, 0, "/tmp/script.py", sampleScript, "python")
	}

	modified := TrySolidify(skill)
	if !modified {
		t.Fatal("expected TrySolidify to return true")
	}

	step := skill.Steps[0]
	if step.Action != "bash" {
		t.Errorf("expected action=bash after promotion, got %q", step.Action)
	}
	cmd, _ := step.Params["command"].(string)
	if cmd == "" {
		t.Error("expected non-empty command after promotion")
	}
	if step.FallbackStep == nil {
		t.Error("expected FallbackStep to be preserved")
	}
	if step.FallbackStep.Action != "craft_tool" {
		t.Errorf("expected FallbackStep.Action=craft_tool, got %q", step.FallbackStep.Action)
	}
}

func TestTrySolidify_DoesNotPromoteBelowThreshold(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "do stuff"}},
		},
	}

	RecordCraftSuccess(skill, 0, "/tmp/script.py", sampleScript, "python")
	RecordCraftSuccess(skill, 0, "/tmp/script.py", sampleScript, "python")

	modified := TrySolidify(skill)
	if modified {
		t.Error("should not promote below threshold")
	}
}

func TestTrySolidify_DoesNotPromoteUnstableStructure(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "do stuff"}},
		},
	}

	// Alternate between two different structures — streak never reaches threshold.
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			RecordCraftSuccess(skill, 0, "/tmp/v1.py", sampleScript, "python")
		} else {
			RecordCraftSuccess(skill, 0, "/tmp/v2.py", differentStructure, "python")
		}
	}

	modified := TrySolidify(skill)
	if modified {
		t.Error("should not promote with unstable structure (alternating signatures)")
	}
}

func TestRevertSolidification(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Name: "test-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: map[string]interface{}{"task": "convert {{input}}"}},
		},
	}

	for i := 0; i < SolidificationThreshold; i++ {
		RecordCraftSuccess(skill, 0, "/tmp/script.py", sampleScript, "python")
	}
	TrySolidify(skill)

	if skill.Steps[0].Action != "bash" {
		t.Fatal("precondition: step should be bash after promotion")
	}

	modified := RevertSolidification(skill, 0)
	if !modified {
		t.Fatal("expected RevertSolidification to return true")
	}

	step := skill.Steps[0]
	if step.Action != "craft_tool" {
		t.Errorf("expected action=craft_tool after revert, got %q", step.Action)
	}
	if step.FallbackStep != nil {
		t.Error("FallbackStep should be nil after revert")
	}

	for _, c := range skill.SolidificationCandidates {
		if c.StepIndex == 0 {
			if c.SuccessCount != 0 {
				t.Errorf("expected success_count=0 after revert, got %d", c.SuccessCount)
			}
			if c.Signature != "" {
				t.Errorf("expected empty signature after revert, got %q", c.Signature)
			}
		}
	}
}

func TestRevertSolidification_NoFallback(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}},
		},
	}
	if RevertSolidification(skill, 0) {
		t.Error("should not modify step without FallbackStep")
	}
}

func TestHasFallbackStep(t *testing.T) {
	original := corelib.NLSkillStep{Action: "craft_tool"}
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", FallbackStep: &original},
			{Action: "bash"},
		},
	}
	if !HasFallbackStep(skill, 0) {
		t.Error("step 0 should have fallback")
	}
	if HasFallbackStep(skill, 1) {
		t.Error("step 1 should not have fallback")
	}
}

func TestComputeStructuralSignature_SameStructure(t *testing.T) {
	sig1 := computeStructuralSignature(sampleScript, "python")
	sig2 := computeStructuralSignature(sameStructureDifferentParams, "python")
	if sig1 != sig2 {
		t.Errorf("same structure should produce same signature: %s vs %s", sig1, sig2)
	}
}

func TestComputeStructuralSignature_DifferentStructure(t *testing.T) {
	sig1 := computeStructuralSignature(sampleScript, "python")
	sig2 := computeStructuralSignature(differentStructure, "python")
	if sig1 == sig2 {
		t.Error("different structure should produce different signatures")
	}
}

func TestComputeStructuralSignature_Empty(t *testing.T) {
	sig := computeStructuralSignature("", "python")
	if sig != "empty" {
		t.Errorf("expected 'empty' for empty script, got %q", sig)
	}
}

func TestComputeStructuralSignature_HashInString(t *testing.T) {
	// A '#' inside a string literal must not be treated as a comment.
	// If normalization strips comments before replacing strings, the '#'
	// eats the rest of the line including the closing quote.
	script1 := `color = "#ff0000"
print(color)`
	script2 := `color = "#00ff00"
print(color)`
	sig1 := computeStructuralSignature(script1, "python")
	sig2 := computeStructuralSignature(script2, "python")
	if sig1 != sig2 {
		t.Errorf("scripts differing only in string content should have same signature: %s vs %s", sig1, sig2)
	}
	// Verify the signature is not "empty" (which would mean normalization
	// ate everything).
	if sig1 == "empty" {
		t.Error("signature should not be empty for non-empty script")
	}
}

func TestBuildSolidifiedCommand(t *testing.T) {
	tests := []struct {
		language     string
		script       string
		params       []string
		wantContains string
	}{
		{"python", "/tmp/script.py", nil, "python /tmp/script.py"},
		{"node", "/tmp/script.js", []string{"input"}, "node /tmp/script.js {{input}}"},
		{"bash", "/tmp/run.sh", nil, "bash /tmp/run.sh"},
		{"", "/tmp/unknown", nil, "/tmp/unknown"},
	}

	for _, tt := range tests {
		c := corelib.SolidificationCandidate{
			ScriptPath: tt.script,
			Language:   tt.language,
			ParamSlots: tt.params,
		}
		got := buildSolidifiedCommand(c)
		if got == "" {
			t.Errorf("buildSolidifiedCommand(%+v) returned empty", c)
			continue
		}
		if !strings.Contains(got, tt.wantContains) {
			t.Errorf("buildSolidifiedCommand(%+v) = %q, want contains %q", c, got, tt.wantContains)
		}
	}
}
