package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExtractCommandFileReferencesHandlesQuotedPathsAndFlagAssignments(t *testing.T) {
	got := ExtractCommandFileReferences(`python "scripts/my tool.py" --helper=./helper.js https://cdn.example/app.js --out result.txt`)
	want := []string{"scripts/my tool.py", "./helper.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesHandlesEscapedSpaces(t *testing.T) {
	got := ExtractCommandFileReferences(`python scripts/my\ tool.py --helper=./helper\ script.js`)
	want := []string{"scripts/my tool.py", "./helper script.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesSkipsShellComments(t *testing.T) {
	got := ExtractCommandFileReferences(`python run.py # helper.py
# missing.py
node ./helper#tag.js`)
	want := []string{"run.py", "./helper#tag.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesSkipsOutputFlagTargets(t *testing.T) {
	got := ExtractCommandFileReferences(`python generate.py --output=generated.py --helper=./helper.js -o result.js --write-to final.mjs`)
	want := []string{"generate.py", "./helper.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesHandlesSlashStyleFlags(t *testing.T) {
	got := ExtractCommandFileReferences(`render.exe /out:generated.py /output result.js /input:source.py /config=./helper.js`)
	want := []string{"source.py", "./helper.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesSkipsOutputRedirectionTargets(t *testing.T) {
	got := ExtractCommandFileReferences(`python generate.py > output.py 2> errors.py`)
	want := []string{"generate.py"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
}

func TestExtractCommandFileReferencesJoinsContinuationLines(t *testing.T) {
	got := ExtractCommandFileReferences("python \\\n  scripts/run.py \\\n  --helper=./helper.js")
	want := []string{"scripts/run.py", "./helper.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesSkipsHeredocBodies(t *testing.T) {
	got := ExtractCommandFileReferences("cat > generated.py <<'PY'\nprint('not_a_reference.py')\nPY\npython run.py")
	want := []string{"run.py"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
}

func TestExtractCommandFileReferencesSkipsBackslashQuotedHeredocBodies(t *testing.T) {
	got := ExtractCommandFileReferences("cat <<\\PY\nprint('not_a_reference.py')\nPY\npython run.py")
	want := []string{"run.py"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
}

func TestExtractCommandFileReferencesSkipsInlineInterpreterCode(t *testing.T) {
	got := ExtractCommandFileReferences(`python -c "missing.py" && node --eval="helper.js" && bash -c "run.sh" && pwsh -Command "tool.ps1"`)
	if len(got) != 0 {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want no inline-code refs", got)
	}
}

func TestExtractCommandFileReferencesDoesNotSkipToolConfigFlag(t *testing.T) {
	got := ExtractCommandFileReferences(`my-tool -c config.py --eval helper.js`)
	want := []string{"config.py", "helper.js"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesRecognizesModernScriptExtensions(t *testing.T) {
	got := ExtractCommandFileReferences(`npx tsx scripts/run.ts && node view.jsx && go run main.go`)
	want := []string{"scripts/run.ts", "view.jsx", "main.go"}
	if len(got) != len(want) {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractCommandFileReferences()[%d] = %q, want %q (all=%#v)", i, got[i], want[i], got)
		}
	}
}

func TestExtractCommandFileReferencesSkipsGlobsAndGoPackagePatterns(t *testing.T) {
	got := ExtractCommandFileReferences(`go test ./... && python -m compileall *.py && node scripts/*.js`)
	if len(got) != 0 {
		t.Fatalf("ExtractCommandFileReferences() = %#v, want no literal glob/package refs", got)
	}
}

func TestCheckStepFileReferencesAllowsInlineCodeWithScriptLikeString(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "inline-code",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": `python -c "missing.py"`},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesSkipsMissingSlashOutputTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "source.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "slash-output",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": `render.exe /input:source.py /out:generated.py`},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesSkipsExpectedPositionalOutputTarget(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "convert.mjs")
	if err := os.WriteFile(script, []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("WriteFile(script): %v", err)
	}
	input := filepath.Join(dir, "input.md")
	if err := os.WriteFile(input, []byte("# demo"), 0o644); err != nil {
		t.Fatalf("WriteFile(input): %v", err)
	}
	output := filepath.Join(dir, "output.pdf")
	entry := &corelib.NLSkillEntry{
		Name:     "positional-output",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": fmt.Sprintf("node %q %q %q", script, input, output),
			},
		}},
	}
	if _, err := CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs(entry, []string{output}); err != nil {
		t.Fatalf("CheckStepFileReferencesWithDiagnosticsAndExpectedOutputs() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesAllowsEscapedSpaceScript(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "my tool.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "escaped-space-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": `python scripts/my\ tool.py`},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesReportsMissingScriptDespiteUnresolvedArgs(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "missing-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python run.py {{input}}"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "run.py")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing run.py", err)
	}
}

func TestCheckStepFileReferencesSkipsOnlyPlaceholderToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "placeholder-arg",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python run.py {{input_file}}.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesResolvesWorkingDir(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "working-dir-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"working_dir": "scripts", "command": "python run.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesTracksDirectoryChanges(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "cd-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts && python run.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesTracksDirectoryChangesAcrossLines(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "multiline-cd-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts\npython run.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesTracksDirectoryChangesWithContinuation(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "continued-cd-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts && \\\n  python run.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesTracksPowerShellContinuation(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "powershell-continuation",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"preferred_shell": "powershell",
				"command":         "python `\n  scripts/run.py",
			},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesReportsMissingScriptAfterPowerShellContinuation(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "powershell-continuation-missing",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"preferred_shell": "pwsh",
				"command":         "python `\n  scripts/run.py",
			},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "scripts", "run.py")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing script after PowerShell continuation", err)
	}
}

func TestCheckStepFileReferencesReportsMissingScriptAfterMultilineDirectoryChange(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "multiline-missing-after-cd",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts\npython run.py"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), filepath.Join(scriptsDir, "run.py")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing script under multiline cd directory", err)
	}
}

func TestCheckStepFileReferencesReportsMissingDirectoryChange(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "missing-cd",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts && python run.py"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), "missing directory") || !strings.Contains(err.Error(), filepath.Join(dir, "scripts")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing cd directory", err)
	}
}

func TestCheckStepFileReferencesResolvesFilesAfterDirectoryChange(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "missing-after-cd",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "cd scripts && python run.py"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), filepath.Join(scriptsDir, "run.py")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing script under cd directory", err)
	}
}

func TestCheckStepFileReferencesTracksPushdPopd(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.py"), []byte("print('root')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "pushd-script",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "pushd scripts && python run.py && popd && python root.py"},
		}},
	}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesReportsMissingWorkingDir(t *testing.T) {
	dir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "missing-working-dir",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"working_dir": "missing", "command": "echo ok"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), "working_dir") || !strings.Contains(err.Error(), filepath.Join(dir, "missing")) {
		t.Fatalf("CheckStepFileReferences() error = %v, want missing working_dir", err)
	}
}

func TestCheckStepFileReferencesReportsWorkingDirFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-dir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "working-dir-file",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"working_dir": "not-dir", "command": "echo ok"},
		}},
	}

	err := CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("CheckStepFileReferences() error = %v, want working_dir file error", err)
	}
}

func TestCheckStepFileReferencesValidatesResolvedWorkingDir(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	params := []corelib.NLSkillParam{{Name: "project_dir", Required: true}}
	steps := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"working_dir": "{{project_dir}}", "command": "python run.py"},
	}}
	resolved, err := ResolveStepsForRunnerPrecheck(steps, map[string]string{"project_dir": projectDir}, dir, params)
	if err != nil {
		t.Fatalf("ResolveStepsForRunnerPrecheck() error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "resolved-working-dir", SkillDir: dir, Steps: resolved}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesReportsResolvedMissingWorkingDir(t *testing.T) {
	dir := t.TempDir()
	params := []corelib.NLSkillParam{{Name: "project_dir", Required: true}}
	missingDir := filepath.Join(dir, "missing-project")
	steps := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"working_dir": "{{project_dir}}", "command": "echo ok"},
	}}
	resolved, err := ResolveStepsForRunnerPrecheck(steps, map[string]string{"project_dir": missingDir}, dir, params)
	if err != nil {
		t.Fatalf("ResolveStepsForRunnerPrecheck() error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "resolved-missing-working-dir", SkillDir: dir, Steps: resolved}

	err = CheckStepFileReferences(entry)

	if err == nil || !strings.Contains(err.Error(), missingDir) {
		t.Fatalf("CheckStepFileReferences() error = %v, want resolved missing working_dir", err)
	}
}

func TestResolveStepsForRunnerPrecheckQuotesSpacedFileReference(t *testing.T) {
	dir := t.TempDir()
	scriptDir := filepath.Join(dir, "scripts with spaces")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptDir, "run.py")
	if err := os.WriteFile(scriptPath, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	params := []corelib.NLSkillParam{{Name: "script_path", Required: true}}
	steps := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "python {{script_path}}"},
	}}
	resolved, err := ResolveStepsForRunnerPrecheck(steps, map[string]string{"script_path": scriptPath}, dir, params)
	if err != nil {
		t.Fatalf("ResolveStepsForRunnerPrecheck() error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "spaced-script-path", SkillDir: dir, Steps: resolved}

	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() unexpected error: %v", err)
	}
}

func TestCheckStepFileReferencesWithDiagnosticsWarnsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte{0xff, 0xfe, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "invalid-utf8",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python run.py"},
		}},
	}

	diagnostics, err := CheckStepFileReferencesWithDiagnostics(entry)

	if err != nil {
		t.Fatalf("CheckStepFileReferencesWithDiagnostics() error = %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "not valid UTF-8") {
		t.Fatalf("diagnostics = %#v, want invalid UTF-8 warning", diagnostics)
	}
	if err := CheckStepFileReferences(entry); err != nil {
		t.Fatalf("CheckStepFileReferences() should not block encoding warnings: %v", err)
	}
}

func TestCheckStepFileReferencesWithDiagnosticsWarnsMojibake(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte("# \xef\xbf\xbd\nprint('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "mojibake",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python run.py"},
		}},
	}

	diagnostics, err := CheckStepFileReferencesWithDiagnostics(entry)

	if err != nil {
		t.Fatalf("CheckStepFileReferencesWithDiagnostics() error = %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "mojibake") {
		t.Fatalf("diagnostics = %#v, want mojibake warning", diagnostics)
	}
	formatted := FormatStepFileDiagnostics(diagnostics)
	if len(formatted) != 1 || !strings.Contains(formatted[0], "step 1:") {
		t.Fatalf("formatted = %#v, want step-prefixed warning", formatted)
	}
}
