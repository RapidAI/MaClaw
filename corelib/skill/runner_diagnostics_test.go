package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildArgsExampleCanonicalizesAndSorts(t *testing.T) {
	got := BuildArgsExample([]string{"Output File", "input-file", "input_file"})
	if got != "\"input_file\": \"<input_file value>\", \"output_file\": \"<output_file value>\"" {
		t.Fatalf("BuildArgsExample() = %q", got)
	}
}

func TestFormatMissingRequiredArgsMessageIsActionable(t *testing.T) {
	got := FormatMissingRequiredArgsMessage("demo", []string{"Input File"}, "Convert documents")
	for _, want := range []string{"missing required parameter", "input_file", "args={", "Description: Convert documents", "[action: provide_args]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q missing %q", got, want)
		}
	}
}

func TestFormatNoExecutableStepsMessageMentionsRunnerCapabilities(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "doc-only", RequiredArgs: []string{"input"}, Description: "Docs only", SkillDir: `/tmp/skill`}
	got := FormatNoExecutableStepsMessage("", entry, RunnerBackendTUI)
	for _, want := range []string{"doc-only", "no executable steps", "Required parameter", "Supported step actions", "bash", "[action: open_gui]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q missing %q", got, want)
		}
	}
}

func TestFormatNoExecutableStepsMessageKnowledgeSkill(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "guide", Type: "knowledge", Description: "Reference docs", SkillDir: `/tmp/guide`}
	got := FormatNoExecutableStepsMessage("", entry, RunnerBackendGUI)
	for _, want := range []string{"guide", "knowledge skill", "not directly executable", "Reference docs", "[action: inspect_skill]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "Supported step actions") {
		t.Fatalf("knowledge message should not suggest runner action compatibility: %q", got)
	}
}

func TestFormatRunnerWarningsCombinesRequirementAndFileWarnings(t *testing.T) {
	got := FormatRunnerWarnings([]Violation{{
		Requirement: Requirement{Type: "unknown", Name: "soft-check"},
		Message:     "soft-check needs review",
		Severity:    "warning",
	}}, []string{"step 1: script looks suspicious"})

	if len(got) != 2 {
		t.Fatalf("FormatRunnerWarnings() = %#v, want two warnings", got)
	}
	for _, want := range []string{"soft-check needs review", "[action: inspect_skill]", "script looks suspicious"} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Fatalf("warnings %#v missing %q", got, want)
		}
	}
}

func TestPrefixOutputWithWarnings(t *testing.T) {
	got := PrefixOutputWithWarnings("body", []string{" first ", "", "second"})
	if got != "[Warning] first\n[Warning] second\nbody" {
		t.Fatalf("PrefixOutputWithWarnings() = %q", got)
	}
	if got := PrefixOutputWithWarnings("", []string{"only"}); got != "[Warning] only" {
		t.Fatalf("PrefixOutputWithWarnings(empty) = %q", got)
	}
	if got := PrefixOutputWithWarnings("body", nil); got != "body" {
		t.Fatalf("PrefixOutputWithWarnings(no warnings) = %q", got)
	}
}
