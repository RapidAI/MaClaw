package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistCraftedSkill_Basic(t *testing.T) {
	root := t.TempDir()

	result, err := PersistCraftedSkill(root,
		"Convert markdown to PDF using weasyprint",
		"import weasyprint\nweasyprint.HTML('input.md').write_pdf('output.pdf')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillName == "" {
		t.Error("SkillName should not be empty")
	}
	if result.IsUpdate {
		t.Error("first persist should not be an update")
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(result.SkillDir, "skill.yaml")); err != nil {
		t.Errorf("skill.yaml should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.SkillDir, "main.py")); err != nil {
		t.Errorf("main.py should exist: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(result.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), result.SkillDir) || !strings.Contains(string(data), "{baseDir}/main.py") {
		t.Fatalf("skill.yaml command should use portable {baseDir} script path, got:\n%s", string(data))
	}
}

func TestPersistCraftedSkill_WritesExtractedParamsIntoCommand(t *testing.T) {
	root := t.TempDir()
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("--input", required=True)
parser.add_argument("--format", default="pdf")
parser.add_argument("--verbose", action="store_true")
`

	result, err := PersistCraftedSkill(root, "Convert file", script, "python")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(result.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := sf.Steps[0].Params["command"].(string)
	if !strings.Contains(command, "--input {{input}}") || !strings.Contains(command, "--format {{format}}") {
		t.Fatalf("persisted command = %q, want extracted argparse placeholders", command)
	}
	if strings.Contains(command, "verbose") {
		t.Fatalf("persisted command = %q, store_true flag should not become a value placeholder", command)
	}
	byName := map[string]SkillYAMLParam{}
	for _, param := range sf.Params {
		byName[param.Name] = param
	}
	if !byName["input"].Required || byName["input"].CLIFlag != "--input" {
		t.Fatalf("persisted params = %#v, want required input CLI param", sf.Params)
	}
	if byName["format"].Required || byName["format"].CLIFlag != "--format" {
		t.Fatalf("persisted params = %#v, want optional format CLI param", sf.Params)
	}
	if _, ok := byName["verbose"]; ok {
		t.Fatalf("persisted params = %#v, store_true should not become a value param", sf.Params)
	}
}

func TestPersistCraftedSkill_WritesExtractedRequires(t *testing.T) {
	root := t.TempDir()
	script := `
import os
import requests
from bs4 import BeautifulSoup
`

	result, err := PersistCraftedSkill(root, "Fetch and parse HTML", script, "python")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(result.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if sf.Requires == nil || len(sf.Requires.Python) != 2 || sf.Requires.Python[0] != "requests" || sf.Requires.Python[1] != "beautifulsoup4" {
		t.Fatalf("persisted requires = %#v, want requests + beautifulsoup4", sf.Requires)
	}
}

func TestPersistCraftedSkill_WritesExtractedRequiredEnv(t *testing.T) {
	root := t.TempDir()
	script := `
import os
print(os.environ["OPENAI_API_KEY"])
print(os.getenv("OPENAI_BASE_URL"))
`

	result, err := PersistCraftedSkill(root, "Call OpenAI", script, "python")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(result.SkillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sf.RequiredEnv) != 2 || sf.RequiredEnv[0] != "OPENAI_API_KEY" || sf.RequiredEnv[1] != "OPENAI_BASE_URL" {
		t.Fatalf("persisted required_env = %#v, want OpenAI env vars", sf.RequiredEnv)
	}
	entry, _, err := loadSkillFromDir(result.SkillDir, filepath.Base(result.SkillDir))
	if err != nil {
		t.Fatal(err)
	}
	guiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendTUI)
	if !guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || !guiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("GUI check context = %#v, want OpenAI env provided by proxy", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || tuiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("TUI check context = %#v, should not mark GUI proxy env provided", tuiCtx.ProvidedEnvVars)
	}
}

func TestPersistCraftedSkill_LoadsAndResolvesPersistedParamContract(t *testing.T) {
	root := t.TempDir()
	script := `
import argparse
parser = argparse.ArgumentParser()
parser.add_argument("--input", required=True)
parser.add_argument("--format")
parser.add_argument("--verbose", action="store_true")
`

	result, err := PersistCraftedSkill(root, "Convert file with optional format", script, "python")
	if err != nil {
		t.Fatal(err)
	}

	entry, _, err := loadSkillFromDir(result.SkillDir, filepath.Base(result.SkillDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Steps) != 1 {
		t.Fatalf("loaded steps = %d, want 1", len(entry.Steps))
	}
	params := CompleteParamsForRunner(entry.Params, entry.Steps, entry.RequiredArgs)
	if missing := MissingRunRequiredArgs(entry.RequiredArgs, params, nil); len(missing) != 1 || missing[0] != "input" {
		t.Fatalf("MissingRunRequiredArgs() = %#v, want [input]", missing)
	}

	resolved, err := ResolveStep(entry.Steps[0], map[string]string{"input": "report.md"}, entry.SkillDir, params, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	command, _ := resolved.Step.Params["command"].(string)
	if strings.Contains(command, "{baseDir}") || !strings.Contains(filepath.ToSlash(command), filepath.ToSlash(result.SkillDir)) {
		t.Fatalf("resolved command = %q, want baseDir placeholder resolved to skill dir", command)
	}
	if !strings.Contains(command, "--input report.md") {
		t.Fatalf("resolved command = %q, want required input substituted", command)
	}
	if strings.Contains(command, "--format") || strings.Contains(command, "verbose") || strings.Contains(command, "{{") {
		t.Fatalf("resolved command = %q, want omitted optional placeholders stripped and no switch param", command)
	}
	if strings.Count(command, "--input") != 1 {
		t.Fatalf("resolved command = %q, want CLI flag consumed once", command)
	}
}

func TestPersistCraftedSkill_Deduplication(t *testing.T) {
	root := t.TempDir()

	// First persist.
	r1, err := PersistCraftedSkill(root,
		"Convert markdown file to PDF document using weasyprint library",
		"print('v1')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Add a few more skills to make BM25 scoring meaningful (IDF needs corpus).
	PersistCraftedSkill(root, "Generate QR code from URL string", "print('qr')", "python")
	PersistCraftedSkill(root, "Resize image to thumbnail size", "print('img')", "python")

	// Second persist with very similar description — should update.
	r2, err := PersistCraftedSkill(root,
		"Convert markdown file to PDF document using weasyprint",
		"print('v2')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !r2.IsUpdate {
		t.Error("second persist with similar description should be an update")
	}
	if r2.SkillDir != r1.SkillDir {
		t.Errorf("update should use same dir: got %q, want %q", r2.SkillDir, r1.SkillDir)
	}

	// Verify script was updated.
	content, _ := os.ReadFile(filepath.Join(r2.SkillDir, "main.py"))
	if string(content) != "print('v2')" {
		t.Errorf("script should be updated to v2, got %q", string(content))
	}
}

func TestPersistCraftedSkill_DifferentTasks(t *testing.T) {
	root := t.TempDir()

	r1, err := PersistCraftedSkill(root,
		"Convert markdown to PDF",
		"print('pdf')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := PersistCraftedSkill(root,
		"Generate QR code from URL",
		"print('qr')",
		"python",
	)
	if err != nil {
		t.Fatal(err)
	}

	if r2.IsUpdate {
		t.Error("completely different task should not be an update")
	}
	if r2.SkillDir == r1.SkillDir {
		t.Error("different tasks should have different dirs")
	}
}

func TestPersistCraftedSkill_EmptyInputs(t *testing.T) {
	root := t.TempDir()

	_, err := PersistCraftedSkill("", "desc", "code", "python")
	if err == nil {
		t.Error("empty skillsRoot should error")
	}

	_, err = PersistCraftedSkill(root, "", "code", "python")
	if err == nil {
		t.Error("empty taskDescription should error")
	}

	_, err = PersistCraftedSkill(root, "desc", "", "python")
	if err == nil {
		t.Error("empty scriptContent should error")
	}
}

func TestPersistCraftedSkill_ScriptLanguages(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		lang    string
		wantExt string
	}{
		{"python", ".py"},
		{"py", ".py"},
		{"node", ".js"},
		{"javascript", ".js"},
		{"bash", ".sh"},
		{"powershell", ".ps1"},
		{"ps1", ".ps1"},
		{"unknown", ".py"}, // default
	}

	for _, tt := range tests {
		result, err := PersistCraftedSkill(root,
			"task for "+tt.lang,
			"echo hello",
			tt.lang,
		)
		if err != nil {
			t.Errorf("lang=%s: %v", tt.lang, err)
			continue
		}
		scriptPath := filepath.Join(result.SkillDir, "main"+tt.wantExt)
		if _, err := os.Stat(scriptPath); err != nil {
			t.Errorf("lang=%s: expected main%s to exist", tt.lang, tt.wantExt)
		}
	}
}

func TestBuildCraftedScriptCommandPowershell(t *testing.T) {
	command := buildCraftedScriptCommand("powershell", `C:\skills\demo\main.ps1`)
	if !strings.Contains(command, "powershell") ||
		!strings.Contains(command, "-ExecutionPolicy Bypass") ||
		!strings.Contains(command, `main.ps1`) {
		t.Fatalf("buildCraftedScriptCommand(powershell) = %q", command)
	}
}

func TestCraftedSkillName(t *testing.T) {
	tests := []struct {
		desc      string
		wantExact string // exact match when non-empty; empty means just check non-empty
	}{
		{"Convert markdown to PDF", "Convert-markdown-to-PDF"},
		{"", ""}, // empty desc → timestamp-based fallback, can't predict exact value
		{"a very long description that exceeds the forty character limit for skill names", "a-very-long-description-that-exceeds-the"},
	}

	for _, tt := range tests {
		got := craftedSkillName(tt.desc)
		if tt.desc == "" {
			if got == "" {
				t.Error("empty desc should produce timestamp-based name")
			}
			continue
		}
		if got == "" {
			t.Errorf("desc=%q: name should not be empty", tt.desc)
		}
		if tt.wantExact != "" && got != tt.wantExact {
			t.Errorf("craftedSkillName(%q) = %q, want %q", tt.desc, got, tt.wantExact)
		}
		if len([]rune(got)) > 80 {
			t.Errorf("desc=%q: name too long: %d runes", tt.desc, len([]rune(got)))
		}
	}
}

func TestIsRepairableError(t *testing.T) {
	tests := []struct {
		errorClass string
		want       bool
	}{
		{"file_not_found", true},
		{"command_not_found", true},
		{"timeout", true},
		{"session_not_found", true},
		{"unknown", true},
		{"", true},
		// External/transient errors — not fixable by modifying skill steps.
		{"rate_limit", false},
		{"network_error", false},
		{"auth_error", false},
		{"missing_env_var", false},
		{"missing_dependency", false},
	}

	for _, tt := range tests {
		got := IsRepairableError(tt.errorClass)
		if got != tt.want {
			t.Errorf("IsRepairableError(%q) = %v, want %v", tt.errorClass, got, tt.want)
		}
	}
}
