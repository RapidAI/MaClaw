package skill

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestAppendRunParamPlaceholders(t *testing.T) {
	got := AppendRunParamPlaceholders("python main.py", []corelib.NLSkillParam{
		{Name: "input", CLIFlag: "--input"},
		{Name: "format", CLIFlag: "--format="},
		{Name: "output"},
	})
	want := "python main.py --input {{input}} --format={{format}} {{output}}"
	if got != want {
		t.Fatalf("AppendRunParamPlaceholders() = %q, want %q", got, want)
	}
}

func TestAppendRunParamPlaceholdersSkipsExistingReferencesAndAliases(t *testing.T) {
	got := AppendRunParamPlaceholders("python main.py {{Input-File}}", []corelib.NLSkillParam{
		{Name: "input_file"},
		{Name: "input-file"},
		{Name: "output", CLIFlag: "/out:"},
	})
	want := "python main.py {{Input-File}} /out:{{output}}"
	if got != want {
		t.Fatalf("AppendRunParamPlaceholders() = %q, want %q", got, want)
	}
}
