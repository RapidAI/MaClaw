package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePortabilityCompatSkill(t *testing.T, dir, command string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	data := []byte("name: run-action-portability\ndescription: A downloaded community skill using run action aliases.\ntriggers:\n  - run-action-portability\nplatforms:\n  - universal\nsteps:\n  - action: run\n    params:\n      cmd: " + command + "\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
}

func TestValidateSkillPortabilityChecksRunActionAliases(t *testing.T) {
	dir := t.TempDir()
	absScript := filepath.Join(dir, "scripts", "run.py")
	writePortabilityCompatSkill(t, dir, "python "+absScript)

	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	var found bool
	for _, issue := range report.Issues {
		if issue.Category == "missing_basedir" && strings.Contains(issue.Message, filepath.Base(absScript)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing_basedir for run action was not reported: %+v", report.Issues)
	}
}

func TestAutoFixPortabilityFixesRunActionAliases(t *testing.T) {
	dir := t.TempDir()
	absScript := filepath.Join(dir, "scripts", "run.py")
	writePortabilityCompatSkill(t, dir, "python "+absScript)

	changes, err := AutoFixPortability(dir)
	if err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("AutoFixPortability() made no changes for run action alias")
	}
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	cmd, _ := parsed.Steps[0].Params["cmd"].(string)
	if !strings.Contains(cmd, "{baseDir}/scripts/run.py") {
		t.Fatalf("cmd was not rewritten with baseDir: %q", cmd)
	}
	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() after fix error = %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Category == "missing_basedir" || issue.Category == "hardcoded_path" {
			t.Fatalf("path issue remained after fix: %+v", issue)
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return data
}

func TestAutoFixPortabilityFixesStructuredCommandArray(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	absScript := filepath.Join(dir, "scripts", "run.py")
	data := []byte("name: array-command-portability\ndescription: A downloaded skill using array command syntax.\ntriggers:\n  - array-command-portability\nplatforms:\n  - universal\nsteps:\n  - action: run\n    params:\n      command:\n        - python\n        - " + absScript + "\n        - hello world\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	if report.Summary.Errors == 0 {
		t.Fatalf("structured command path issue was not detected: %+v", report.Issues)
	}
	changes, err := AutoFixPortability(dir)
	if err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("AutoFixPortability() made no changes for structured command")
	}
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	cmd, ok := parsed.Steps[0].Params["command"].([]interface{})
	if !ok || len(cmd) != 3 {
		t.Fatalf("structured command should remain an array, got %#v", parsed.Steps[0].Params["command"])
	}
	if cmd[0] != "python" || cmd[1] != "{baseDir}/scripts/run.py" || cmd[2] != "hello world" {
		t.Fatalf("structured command was not fixed in place: %#v", cmd)
	}
}

func TestAutoFixPortabilityPreservesCommandObjectShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	absScript := filepath.Join(dir, "scripts", "run.py")
	data := []byte("name: object-command-portability\ndescription: A downloaded skill using object command syntax.\ntriggers:\n  - object-command-portability\nplatforms:\n  - universal\nsteps:\n  - action: run\n    params:\n      command:\n        program: python\n        args:\n          - " + absScript + "\n          - --retries\n          - 3\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	if _, err := AutoFixPortability(dir); err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	cmd, ok := parsed.Steps[0].Params["command"].(map[string]interface{})
	if !ok {
		t.Fatalf("command object should remain a map, got %#v", parsed.Steps[0].Params["command"])
	}
	args, ok := cmd["args"].([]interface{})
	if !ok || len(args) != 3 {
		t.Fatalf("command args should remain an array, got %#v", cmd["args"])
	}
	if cmd["program"] != "python" || args[0] != "{baseDir}/scripts/run.py" || args[1] != "--retries" || args[2] != 3 {
		t.Fatalf("command object was not fixed in place without type loss: %#v", cmd)
	}
}

func TestAutoFixPortabilityFixesPythonScriptAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	absScript := filepath.Join(dir, "scripts", "run.py")
	data := []byte("name: python-script-portability\ndescription: A downloaded skill using python action script alias.\ntriggers:\n  - python-script-portability\nplatforms:\n  - universal\nsteps:\n  - action: python\n    params:\n      script: " + absScript + "\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	changes, err := AutoFixPortability(dir)
	if err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("AutoFixPortability() made no changes for python script alias")
	}
	parsed, err := ParseSkillYAMLFile(mustReadFile(t, filepath.Join(dir, "skill.yaml")))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	script, _ := parsed.Steps[0].Params["script"].(string)
	if script != "{baseDir}/scripts/run.py" {
		t.Fatalf("script alias was not fixed in place: %q", script)
	}
}

func TestValidateSkillPortabilityAcceptsReadmeDocumentation(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.ToSlash(filepath.Join(dir, "scripts", "run.py"))
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# README skill\n\n```bash\npython " + absPath + "\n```\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	var found bool
	for _, issue := range report.Issues {
		if issue.File == "README.md" && issue.Category == "missing_basedir" {
			found = true
		}
	}
	if !found {
		t.Fatalf("README.md portability issue not reported with README.md file name: %+v", report.Issues)
	}
}

func TestValidateSkillPortabilityAcceptsLowercaseReadmeDocumentation(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.ToSlash(filepath.Join(dir, "scripts", "run.py"))
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# readme skill\n\n```bash\npython " + absPath + "\n```\n"
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	var found bool
	for _, issue := range report.Issues {
		if issue.File == "readme.md" && issue.Category == "missing_basedir" {
			found = true
		}
	}
	if !found {
		t.Fatalf("readme.md portability issue not reported with readme.md file name: %+v", report.Issues)
	}
}
func TestAutoFixPortabilityIgnoresJSONDefinition(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"json-portability"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := AutoFixPortability(dir)
	if err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("skill.json should not be fixed as a skill definition: %+v", changes)
	}
	if _, err := os.Stat(filepath.Join(dir, "skill.json.bak")); !os.IsNotExist(err) {
		t.Fatalf("AutoFixPortability should not back up skill.json; stat err=%v", err)
	}
}

func TestAutoFixPortabilityPreservesYMLDefinitionFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	absScript := filepath.ToSlash(filepath.Join(dir, "scripts", "run.py"))
	data := []byte("name: yml-portability\ndescription: A YML skill with a local script path.\ntriggers:\n  - yml-portability\nsteps:\n  - action: run\n    params:\n      command: python " + absScript + "\n")
	ymlPath := filepath.Join(dir, "skill.yml")
	if err := os.WriteFile(ymlPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	changes, err := AutoFixPortability(dir)
	if err != nil {
		t.Fatalf("AutoFixPortability() error = %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("AutoFixPortability() made no changes for skill.yml")
	}
	for _, change := range changes {
		if change.File != "skill.yml" {
			t.Fatalf("change file = %q, want skill.yml (change=%+v)", change.File, change)
		}
	}
	fixed := mustReadFile(t, ymlPath)
	if !strings.Contains(string(fixed), "{baseDir}/scripts/run.py") {
		t.Fatalf("skill.yml was not fixed with baseDir: %s", fixed)
	}
	if _, err := os.Stat(filepath.Join(dir, "skill.yaml")); !os.IsNotExist(err) {
		t.Fatalf("AutoFixPortability should not create skill.yaml for skill.yml definitions; stat err=%v", err)
	}
	if _, err := os.Stat(ymlPath + ".bak"); err != nil {
		t.Fatalf("skill.yml backup missing: %v", err)
	}
}

func TestValidateSkillPortabilityReportsYMLMetadataFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte("name: yml-metadata\ndescription: short\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ValidateSkillPortability(dir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability() error = %v", err)
	}
	var found bool
	for _, issue := range report.Issues {
		if issue.File == "skill.yml" && issue.Category == "missing_platforms" {
			found = true
		}
		if issue.File == "skill.yaml" {
			t.Fatalf("issue should report actual skill.yml file, got %+v", issue)
		}
	}
	if !found {
		t.Fatalf("missing_platforms was not reported for skill.yml: %+v", report.Issues)
	}
}
