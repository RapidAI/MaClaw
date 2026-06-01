package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePreflightSkill writes a minimal directory-backed skill with a single
// bash step whose command is provided by the caller, plus a bundled script.
func writePreflightSkill(t *testing.T, dir, command string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	data := []byte("name: preflight-demo\ndescription: A skill used for upload preflight tests.\ntriggers:\n  - preflight-demo\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: " + command + "\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
}

// TestPrepareSkillForUpload_AutoFixesInsideDirPath verifies that an absolute
// path pointing inside the skill directory is auto-fixed to {baseDir}/... and
// the result is portable (no manual agent intervention required).
func TestPrepareSkillForUpload_AutoFixesInsideDirPath(t *testing.T) {
	dir := t.TempDir()
	absScript := filepath.Join(dir, "scripts", "run.py")
	writePreflightSkill(t, dir, "python "+absScript)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if len(result.AutoFixed) == 0 {
		t.Fatal("expected auto-fix to rewrite the in-dir absolute path")
	}
	if !result.Portable() {
		t.Fatalf("expected skill to be portable after auto-fix, got blocking=%v missing=%v",
			result.BlockingPaths, result.MissingFiles)
	}
	// Verify the on-disk definition was persisted with the baseDir macro.
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	cmd, _ := parsed.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd, "{baseDir}/scripts/run.py") {
		t.Fatalf("command not rewritten with baseDir: %q", cmd)
	}
}

// TestPrepareSkillForUpload_BlocksMachineSpecificPath verifies that an absolute
// path outside the skill dir and outside $HOME (a machine-specific path) cannot
// be auto-fixed and is reported as a blocking path so the agent must convert it.
func TestPrepareSkillForUpload_BlocksMachineSpecificPath(t *testing.T) {
	dir := t.TempDir()
	// Use a path that is neither inside the skill dir nor under the user's home.
	machinePath := "/opt/acme/secret/config.json"
	writePreflightSkill(t, dir, "cat "+machinePath)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for machine-specific path, got %+v", result)
	}
	found := false
	for _, p := range result.BlockingPaths {
		if strings.Contains(p.Path, "/opt/acme/secret/config.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected blocking path for %q, got %+v", machinePath, result.BlockingPaths)
	}

	report := FormatUploadPreflight(result)
	if !strings.Contains(report, "上传被阻止") || !strings.Contains(report, machinePath) {
		t.Fatalf("formatted report missing block details:\n%s", report)
	}
	if !strings.Contains(report, "{baseDir}") {
		t.Fatalf("formatted report should explain the {baseDir} macro:\n%s", report)
	}
}

// TestPrepareSkillForUpload_ReportsMissingBundledFile verifies that a relative
// script reference that is not bundled inside the skill directory is reported
// as a missing file (the skill would fail on another machine).
func TestPrepareSkillForUpload_ReportsMissingBundledFile(t *testing.T) {
	dir := t.TempDir()
	// Reference a script via {baseDir} that does NOT exist in the package.
	writePreflightSkill(t, dir, "python {baseDir}/scripts/missing.py")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing bundled file, got %+v", result)
	}
	if len(result.MissingFiles) == 0 {
		t.Fatalf("expected a missing-file report, got none; warnings=%v", result.Warnings)
	}
	joined := strings.Join(result.MissingFiles, "\n")
	if !strings.Contains(joined, "missing.py") {
		t.Fatalf("missing-file report should reference missing.py, got %q", joined)
	}
}

// TestPrepareSkillForUpload_CleanSkillIsPortable verifies a well-formed skill
// with a bundled, baseDir-referenced script passes the gate.
func TestPrepareSkillForUpload_CleanSkillIsPortable(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected clean skill to be portable, got blocking=%v missing=%v",
			result.BlockingPaths, result.MissingFiles)
	}
	report := FormatUploadPreflight(result)
	if !strings.Contains(report, "可移植性检查通过") {
		t.Fatalf("expected pass message, got:\n%s", report)
	}
}

// TestPrepareSkillForUpload_ReportsAllMissingBundledFiles verifies that when a
// skill references multiple missing bundled files across steps, the preflight
// reports every one of them (not just the first) so the agent can fix them in
// a single pass instead of repeated fix/retry round trips.
func TestPrepareSkillForUpload_ReportsAllMissingBundledFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	// Two steps, each referencing a different missing bundled script.
	data := []byte("name: multi-missing\ndescription: A skill referencing multiple missing bundled scripts.\ntriggers:\n  - multi-missing\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: python {baseDir}/scripts/first.py\n  - action: bash\n    params:\n      command: node {baseDir}/scripts/second.js\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result, got %+v", result)
	}
	joined := strings.Join(result.MissingFiles, "\n")
	if !strings.Contains(joined, "first.py") {
		t.Fatalf("missing-file report should reference first.py, got %q", joined)
	}
	if !strings.Contains(joined, "second.js") {
		t.Fatalf("missing-file report should reference second.js (all missing files, not just the first), got %q", joined)
	}
}

// TestPrepareSkillForUpload_EmptyDir validates the guard for an empty path.
func TestPrepareSkillForUpload_EmptyDir(t *testing.T) {
	if _, err := PrepareSkillForUpload("   "); err == nil {
		t.Fatal("expected error for empty skill directory")
	}
}
