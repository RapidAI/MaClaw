package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func appendPreflightSkillYAML(t *testing.T, dir, extra string) {
	t.Helper()
	path := filepath.Join(dir, "skill.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(extra)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

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
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	cmd, _ := parsed.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd, "{baseDir}/scripts/run.py") {
		t.Fatalf("command not rewritten with baseDir: %q", cmd)
	}
}

func TestPrepareSkillForUploadWithOptionsCanDisableAutoFix(t *testing.T) {
	dir := t.TempDir()
	absScript := filepath.Join(dir, "scripts", "run.py")
	writePreflightSkill(t, dir, "python "+absScript)

	result, err := PrepareSkillForUploadWithOptions(dir, UploadPreflightOptions{AutoFix: false})
	if err != nil {
		t.Fatalf("PrepareSkillForUploadWithOptions() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result without auto-fix, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "{baseDir}") {
		t.Fatalf("auto-fix disabled but skill.yaml was rewritten:\n%s", string(data))
	}
}

func TestPrepareSkillForUpload_BlocksMachineSpecificPath(t *testing.T) {
	dir := t.TempDir()
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
		if strings.Contains(p.Path, machinePath) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected blocking path for %q, got %+v", machinePath, result.BlockingPaths)
	}

	report := FormatUploadPreflight(result)
	if !strings.Contains(report, "Upload blocked") || !strings.Contains(report, machinePath) {
		t.Fatalf("formatted report missing block details:\n%s", report)
	}
	if !strings.Contains(report, "{baseDir}") {
		t.Fatalf("formatted report should explain the {baseDir} macro:\n%s", report)
	}
}

func TestPrepareSkillForUpload_ReportsMissingBundledFile(t *testing.T) {
	dir := t.TempDir()
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
	report := FormatUploadPreflight(result)
	if !strings.Contains(report, "Missing bundled files") {
		t.Fatalf("formatted report should mention missing files:\n%s", report)
	}
}

func TestPrepareSkillForUpload_ReportsMissingParamDefaultFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "params:\n  - name: input_file\n    default: data.csv\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing param default file, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "data.csv") {
		t.Fatalf("missing-file report should reference param default file, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_ReportsMissingBaseDirEnvParamDefaultFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "params:\n  - name: input_file\n    default: $BASE_DIR/data.csv\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing base-dir env param default, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "data.csv") {
		t.Fatalf("missing-file report should reference base-dir env param default, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_AllowsMissingOutputDefaultFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py --output {{output_file}}")
	appendPreflightSkillYAML(t, dir, "params:\n  - name: output_file\n    default: result.pdf\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected missing output default to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_AllowsURLAndTemplatePathDefaults(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "params:\n  - name: input_url\n    default: https://example.test/data.csv\n  - name: input_path\n    default: \"{{input_path}}\"\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected URL/template defaults to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_ReportsMissingPathLikeStepParamFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: parse\n    params:\n      input_file: config.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing step path param file, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "config.json") {
		t.Fatalf("missing-file report should reference step path param file, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_ReportsMissingBaseDirEnvStepParamFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: parse\n    params:\n      input_file: $BASE_DIR/data/config.json\n      config_path: ${BASE_DIR}/data/schema.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing base-dir env refs, got %+v", result)
	}
	joined := strings.Join(result.MissingFiles, "\n")
	if !strings.Contains(joined, "data/config.json") {
		t.Fatalf("missing-file report should reference $BASE_DIR file, got %q", joined)
	}
	if !strings.Contains(joined, "data/schema.json") {
		t.Fatalf("missing-file report should reference ${BASE_DIR} file, got %q", joined)
	}
}

func TestPrepareSkillForUpload_AllowsBundledBaseDirEnvStepParamFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "schema.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendPreflightSkillYAML(t, dir, "  - action: parse\n    params:\n      input_file: $BASE_DIR/data/config.json\n      config_path: ${BASE_DIR}/data/schema.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected bundled base-dir env refs to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_AllowsURLAndTemplateStepPathParams(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: fetch\n    params:\n      input_path: https://example.test/input.json\n      config_path: \"{{config_path}}\"\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected URL/template step path params to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_AllowsMissingOutputStepParamFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: render\n    params:\n      output_file: result.pdf\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected missing output step param to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_ReportsMissingInputDirParam(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: load\n    params:\n      input_dir: fixtures\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing input_dir, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "fixtures") {
		t.Fatalf("missing-file report should reference input_dir, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_AllowsBundledInputDirParam(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	appendPreflightSkillYAML(t, dir, "  - action: load\n    params:\n      input_dir: fixtures\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected bundled input_dir to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_AllowsMissingOutputDirParam(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "  - action: render\n    params:\n      output_dir: dist\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected missing output_dir to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_ReportsInvalidCredentialFileRef(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "required_credential_files:\n  - ../secret.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for invalid credential file ref, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "../secret.json") {
		t.Fatalf("missing-file report should reference invalid credential file ref, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_ReportsMissingCredentialFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "required_credential_files:\n  - credentials/api.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing credential file, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "credentials/api.json") {
		t.Fatalf("missing-file report should reference credential file, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_AllowsBundledCredentialFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	credentialPath := filepath.Join(dir, "credentials", "api.json")
	if err := os.MkdirAll(filepath.Dir(credentialPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendPreflightSkillYAML(t, dir, "required_credential_files:\n  - credentials/api.json\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected bundled credential file to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

func TestPrepareSkillForUpload_AllowsRuntimeCredentialFileRefs(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "required_credential_files:\n  - $HOME/.config/api.json\n  - \"{{credential_file}}\"\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected runtime credential refs to be allowed, got missing=%v blocking=%v",
			result.MissingFiles, result.BlockingPaths)
	}
}

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
	if !strings.Contains(report, "Upload preflight passed") {
		t.Fatalf("expected pass message, got:\n%s", report)
	}
}

func TestPrepareSkillForUpload_KeepsNonBlockingInfoWarnings(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "pip install rich && python {baseDir}/scripts/run.py")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected runtime-install info to be non-blocking, got blocking=%v missing=%v",
			result.BlockingPaths, result.MissingFiles)
	}
	joined := strings.Join(result.Warnings, "\n")
	if !strings.Contains(joined, "runtime_install") {
		t.Fatalf("expected runtime_install info in non-blocking warnings, got %q", joined)
	}
	report := FormatUploadPreflight(result)
	if !strings.Contains(report, "Non-blocking warnings") || !strings.Contains(report, "runtime_install") {
		t.Fatalf("formatted report should include runtime_install info:\n%s", report)
	}
}

func TestPrepareSkillForUpload_ReportsAllMissingBundledFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
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
		t.Fatalf("missing-file report should reference second.js, got %q", joined)
	}
}

func TestPrepareSkillForUpload_ReportsMissingPipelineParamFile(t *testing.T) {
	dir := t.TempDir()
	writePreflightSkill(t, dir, "python {baseDir}/scripts/run.py")
	appendPreflightSkillYAML(t, dir, "mode: pipeline\npipeline:\n  - skill: child\n    params:\n      input_path: \"{baseDir}/data/source.json\"\n")

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload() error = %v", err)
	}
	if result.Portable() {
		t.Fatalf("expected non-portable result for missing pipeline param file, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.MissingFiles, "\n"), "data/source.json") {
		t.Fatalf("missing-file report should reference pipeline param file, got %q", strings.Join(result.MissingFiles, "\n"))
	}
}

func TestPrepareSkillForUpload_EmptyDir(t *testing.T) {
	if _, err := PrepareSkillForUpload("   "); err == nil {
		t.Fatal("expected error for empty skill directory")
	}
}
