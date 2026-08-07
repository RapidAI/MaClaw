package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// --- portabilizePath ---

func TestPortabilizePath_WorkspaceRelative(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(workDir, "out", "report.txt")

	got := portabilizePath(target, workDir)
	want := workspacePlaceholder + "/out/report.txt"
	if got != want {
		t.Fatalf("portabilizePath(%q) = %q, want %q", target, got, want)
	}
}

func TestPortabilizePath_ForeignAbsolutePath(t *testing.T) {
	workDir := t.TempDir()

	// A path on another drive / another machine layout must become a
	// {{placeholder}} — never leak into the skill verbatim.
	got := portabilizePath(`D:\external\config.ini`, workDir)
	if strings.Contains(got, `D:\`) || strings.Contains(got, "D:") {
		t.Fatalf("foreign path leaked: %q", got)
	}
	if got != "{{config_ini}}" {
		t.Fatalf("portabilizePath(foreign) = %q, want {{config_ini}}", got)
	}

	// Unix user-home path of another user.
	got = portabilizePath("/Users/other/toolkit", workDir)
	if strings.Contains(got, "/Users/") {
		t.Fatalf("foreign unix path leaked: %q", got)
	}
	if got != "{{toolkit_dir}}" {
		t.Fatalf("portabilizePath(foreign unix) = %q, want {{toolkit_dir}}", got)
	}
}

func TestPortabilizePath_RelativeUnchanged(t *testing.T) {
	got := portabilizePath("src/main.go", t.TempDir())
	if got != "src/main.go" {
		t.Fatalf("relative path changed: %q", got)
	}
}

func TestPortabilizePath_WorkspaceCaseInsensitiveWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive matching is Windows-specific")
	}
	workDir := `C:\Foo\Proj`
	got := portabilizePath(`c:\foo\proj\out\report.txt`, workDir)
	if got != workspacePlaceholder+"/out/report.txt" {
		t.Fatalf("got %q, want %s/out/report.txt", got, workspacePlaceholder)
	}
}

func TestPortabilizeCommand_WorkspaceCaseInsensitiveWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive matching is Windows-specific")
	}
	got := portabilizeCommand(`python c:\FOO\proj\run.py`, `C:\Foo\Proj`)
	if strings.Contains(strings.ToLower(got), `c:\foo`) {
		t.Fatalf("workDir leaked: %q", got)
	}
	if !strings.Contains(got, workspacePlaceholder+`\run.py`) && !strings.Contains(got, workspacePlaceholder+"/run.py") {
		t.Fatalf("expected workspace placeholder: %q", got)
	}
}

// --- portabilizeCommand ---

func TestPortabilizeCommand_WorkspaceAndForeignPaths(t *testing.T) {
	workDir := t.TempDir()
	cmd := fmt.Sprintf(`python %s %s --tool D:\tools\helper.exe`, filepath.Join(workDir, "run.py"), filepath.Join(workDir, "data"))

	got := portabilizeCommand(cmd, workDir)
	if strings.Contains(got, workDir) {
		t.Fatalf("workDir leaked into command: %q", got)
	}
	if !strings.Contains(got, workspacePlaceholder) {
		t.Fatalf("expected %s in command: %q", workspacePlaceholder, got)
	}
	if strings.Contains(got, `D:\tools`) {
		t.Fatalf("foreign drive path leaked into command: %q", got)
	}
	if !strings.Contains(got, "{{helper_exe}}") {
		t.Fatalf("expected {{helper_exe}} placeholder in command: %q", got)
	}
}

func TestPortabilizeCommand_PreservesURLsAndFlags(t *testing.T) {
	workDir := t.TempDir()
	cmds := []string{
		"curl https://example.com/api/v1 -o out.json",
		"curl https://example.com/home/user Guide.pdf",   // URL path must not be parameterized
		"curl https://example.com:8080/home/x -o y.json", // host:port + /home path
	}
	for _, cmd := range cmds {
		if got := portabilizeCommand(cmd, workDir); got != cmd {
			t.Errorf("command mangled: %q → %q", cmd, got)
		}
	}
}

func TestPortabilizeCommand_PrefixBoundary(t *testing.T) {
	// workDir ".../proj" must not rewrite the sibling directory ".../proj2".
	workDir := filepath.Join(t.TempDir(), "proj")
	sibling := workDir + "2"
	cmd := fmt.Sprintf("python %s && ls %s", filepath.Join(workDir, "run.py"), sibling)

	got := portabilizeCommand(cmd, workDir)
	if !strings.Contains(got, workspacePlaceholder) {
		t.Fatalf("workDir ref not replaced: %q", got)
	}
	if strings.Contains(got, filepath.ToSlash(workDir)) || strings.Contains(got, workDir) {
		t.Fatalf("workDir leaked: %q", got)
	}
	if strings.Contains(got, workspacePlaceholder+"2") {
		t.Fatalf("sibling dir %q partially rewritten: %q", sibling, got)
	}
}

// --- placeholderForPath ---

func TestPlaceholderForPath(t *testing.T) {
	cases := map[string]string{
		`D:\data\input.csv`:    "{{input_csv}}",
		"/Users/x/tools":       "{{tools_dir}}",
		`/home/y/output.json`:  "{{output_json}}",
		`E:\reports\sale.xlsx`: "{{sale_xlsx}}",
	}
	for in, want := range cases {
		if got := placeholderForPath(in); got != want {
			t.Errorf("placeholderForPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- convertOpToStep ---

func TestConvertOpToStep_WriteFileTargetNotBaseDir(t *testing.T) {
	workDir := t.TempDir()
	r := NewSkillOperationRecorder()
	var templates []pendingTemplateFile

	target := filepath.Join(workDir, "out", "report.txt")
	op := RecordedOp{
		Timestamp: time.Now(),
		ToolName:  "write_file",
		Args: map[string]interface{}{
			"path":    target,
			"content": fmt.Sprintf("log written to %s\n", filepath.Join(workDir, "out")),
		},
		Success: true,
	}

	step := r.convertOpToStep(op, workDir, &templates)
	if step == nil {
		t.Fatal("step is nil")
	}
	params, _ := step["params"].(map[string]interface{})
	cmd, _ := params["command"].(string)

	// The destination must be {{workspace}}-relative, not {baseDir} (which the
	// runner resolves to the skill install dir, not the user's workspace).
	if strings.Contains(cmd, "'{baseDir}/out") {
		t.Fatalf("write target still uses {baseDir}: %q", cmd)
	}
	if !strings.Contains(cmd, workspacePlaceholder+"/out/report.txt") {
		t.Fatalf("expected %s-relative target in command: %q", workspacePlaceholder, cmd)
	}

	// Template source still uses {baseDir} (skill-internal file — correct).
	if !strings.Contains(cmd, "{baseDir}/template_") {
		t.Fatalf("template source should use {baseDir}: %q", cmd)
	}

	// File content must be scrubbed of the recording machine's workDir.
	if len(templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(templates))
	}
	if strings.Contains(templates[0].Content, workDir) {
		t.Fatalf("workDir leaked into template content: %q", templates[0].Content)
	}
	if !strings.Contains(templates[0].Content, workspacePlaceholder) {
		t.Fatalf("expected %s in template content: %q", workspacePlaceholder, templates[0].Content)
	}
}

func TestConvertOpToStep_EditFileTargetViaArgv(t *testing.T) {
	workDir := t.TempDir()
	r := NewSkillOperationRecorder()
	var templates []pendingTemplateFile

	target := filepath.Join(workDir, "config.yaml")
	op := RecordedOp{
		Timestamp: time.Now(),
		ToolName:  "edit_file",
		Args: map[string]interface{}{
			"path":       target,
			"old_string": "a: 1",
			"new_string": "a: 2",
		},
		Success: true,
	}

	step := r.convertOpToStep(op, workDir, &templates)
	if step == nil {
		t.Fatal("step is nil")
	}
	params, _ := step["params"].(map[string]interface{})
	cmd, _ := params["command"].(string)

	// Target path goes through argv so {{workspace}} is substituted by the runner.
	if !strings.Contains(cmd, `"`+workspacePlaceholder+"/config.yaml\"") {
		t.Fatalf("target not passed as argv with placeholder: %q", cmd)
	}
	// The script must not hardcode the machine path.
	for _, tmpl := range templates {
		if strings.Contains(tmpl.Content, workDir) {
			t.Fatalf("workDir leaked into %s: %q", tmpl.Name, tmpl.Content)
		}
	}
}

func TestConvertOpToStep_RelativeTargetAnchoredToWorkspace(t *testing.T) {
	r := NewSkillOperationRecorder()
	var templates []pendingTemplateFile

	op := RecordedOp{
		Timestamp: time.Now(),
		ToolName:  "write_file",
		Args: map[string]interface{}{
			"path":    "out/report.txt", // relative at record time = workspace-relative
			"content": "data\n",
		},
		Success: true,
	}

	step := r.convertOpToStep(op, t.TempDir(), &templates)
	if step == nil {
		t.Fatal("step is nil")
	}
	params, _ := step["params"].(map[string]interface{})
	cmd, _ := params["command"].(string)
	if !strings.Contains(cmd, workspacePlaceholder+"/out/report.txt") {
		t.Fatalf("relative target not anchored to %s: %q", workspacePlaceholder, cmd)
	}
}

// --- heuristic suggestions ---

func TestSuggestSkillName_PrefersScriptName(t *testing.T) {
	r := NewSkillOperationRecorder()
	entries := []RecordedOp{
		{ToolName: "bash", Args: map[string]interface{}{"command": "python scripts/export_data.py --all"}, Success: true},
	}
	if got := r.suggestSkillName(entries); got != "export-data" {
		t.Fatalf("suggestSkillName = %q, want export-data", got)
	}
}

func TestSuggestSkillName_WriteFileFallback(t *testing.T) {
	r := NewSkillOperationRecorder()
	entries := []RecordedOp{
		{ToolName: "write_file", Args: map[string]interface{}{"path": "reports/summary.xlsx", "content": "x"}, Success: true},
	}
	if got := r.suggestSkillName(entries); got != "summary-xlsx" {
		t.Fatalf("suggestSkillName = %q, want summary-xlsx", got)
	}
}

func TestSuggestDescription_ListActions(t *testing.T) {
	r := NewSkillOperationRecorder()
	entries := []RecordedOp{
		{ToolName: "bash", Args: map[string]interface{}{"command": "python export.py"}, Success: true},
		{ToolName: "write_file", Args: map[string]interface{}{"path": "report.xlsx", "content": "x"}, Success: true},
	}
	got := r.suggestDescription(entries)
	if !strings.Contains(got, "python export.py") || !strings.Contains(got, "report.xlsx") {
		t.Fatalf("description lacks concrete actions: %q", got)
	}
	if strings.Contains(got, "Auto-learned") {
		t.Fatalf("description still uses old template: %q", got)
	}
}

func TestSuggestDescription_ScrubsMachinePaths(t *testing.T) {
	// The description is persisted into skill.yaml — it must not leak the
	// recording machine's paths even when the LLM is unavailable.
	workDir := t.TempDir()
	r := NewSkillOperationRecorder()
	r.workDir = workDir
	entries := []RecordedOp{
		{ToolName: "bash", Args: map[string]interface{}{"command": "cd " + workDir}, Success: true},
	}
	got := r.suggestDescription(entries)
	if strings.Contains(got, workDir) {
		t.Fatalf("workDir leaked into description: %q", got)
	}
	if !strings.Contains(got, workspacePlaceholder) {
		t.Fatalf("expected %s in description: %q", workspacePlaceholder, got)
	}
}

// --- LLM metadata parsing ---

func TestParseRecordingMetadata_Valid(t *testing.T) {
	raw := `{"name":"export_excel_report","description":"导出 Excel 报表","steps":["安装依赖","运行导出脚本"]}`
	meta, ok := parseRecordingMetadata(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if meta.Name != "export-excel-report" {
		t.Fatalf("name = %q, want export-excel-report (kebab)", meta.Name)
	}
	if meta.Description != "导出 Excel 报表" {
		t.Fatalf("description = %q", meta.Description)
	}
	if len(meta.Steps) != 2 || meta.Steps[0] != "安装依赖" {
		t.Fatalf("steps = %#v", meta.Steps)
	}
}

func TestParseRecordingMetadata_DirtyOutput(t *testing.T) {
	cases := []string{
		"",                              // empty
		"export_report",                 // no JSON at all
		`{"name":"","description":"x"}`, // empty name
		`{"name":"导出报表","description":"x"}`, // non-ASCII name
	}
	for _, raw := range cases {
		if _, ok := parseRecordingMetadata(raw); ok {
			t.Errorf("parseRecordingMetadata(%q) unexpectedly ok", raw)
		}
	}
}

func TestParseRecordingMetadata_LenientOptionalFields(t *testing.T) {
	// A valid name alone is enough — description/steps are optional
	// enrichments; the caller keeps heuristic values for them.
	meta, ok := parseRecordingMetadata(`{"name":"ok-name","description":"","steps":[]}`)
	if !ok {
		t.Fatal("name-only metadata should be accepted")
	}
	if meta.Name != "ok-name" || meta.Description != "" || len(meta.Steps) != 0 {
		t.Fatalf("meta = %+v", meta)
	}
}

// stubRecordingLLM implements skillNamingLLM for tests.
type stubRecordingLLM struct {
	configured bool
	resp       string
	err        error
}

func (s *stubRecordingLLM) ChatCall(messages []map[string]string) (string, error) {
	return s.resp, s.err
}
func (s *stubRecordingLLM) IsConfigured() bool { return s.configured }

func TestSuggestRecordingMetadataWithLLM(t *testing.T) {
	entries := []RecordedOp{
		{ToolName: "bash", Args: map[string]interface{}{"command": "python export.py"}, Success: true},
	}

	// Unconfigured → fallback.
	if _, _, _, ok := SuggestRecordingMetadataWithLLM(&stubRecordingLLM{configured: false}, entries, "", nil); ok {
		t.Fatal("unconfigured LLM should return ok=false")
	}

	// Valid response.
	name, desc, titles, ok := SuggestRecordingMetadataWithLLM(&stubRecordingLLM{
		configured: true,
		resp:       `{"name":"export-report","description":"导出报表","steps":["运行脚本"]}`,
	}, entries, "", nil)
	if !ok || name != "export-report" || desc != "导出报表" || len(titles) != 1 {
		t.Fatalf("got name=%q desc=%q titles=%v ok=%v", name, desc, titles, ok)
	}

	// Garbage response → fallback.
	if _, _, _, ok := SuggestRecordingMetadataWithLLM(&stubRecordingLLM{
		configured: true,
		resp:       "I cannot help with that.",
	}, entries, "", nil); ok {
		t.Fatal("garbage LLM output should return ok=false")
	}
}

// --- Stop end-to-end portability ---

func TestConsolidateRecordedOps_DoesNotMutateSharedArgs(t *testing.T) {
	// bindings pass a shallow-copied slice to consolidation for the LLM pass;
	// without cloning, the in-place edit-merge would corrupt the recorder's
	// pending entries that Stop() later consolidates again.
	writeArgs := map[string]interface{}{"path": "a.txt", "content": "hello world"}
	editArgs := map[string]interface{}{"path": "a.txt", "old_string": "world", "new_string": "there"}
	entries := []RecordedOp{
		{ToolName: "write_file", Args: writeArgs, Success: true},
		{ToolName: "edit_file", Args: editArgs, Success: true},
	}

	consolidateRecordedOps(cloneRecordedOps(entries))

	if writeArgs["content"] != "hello world" {
		t.Fatalf("shared write_file args mutated: %v", writeArgs["content"])
	}
}

func TestStop_GeneratesPortableSkill(t *testing.T) {
	// Redirect the skills dir into a temp location.
	base := t.TempDir()
	old := maclawpath.BaseDir()
	maclawpath.SetBaseDir(base)
	t.Cleanup(func() { maclawpath.SetBaseDir(old) })

	workDir := filepath.Join(t.TempDir(), "proj")
	foreignTarget := `D:\external\config.ini`

	r := NewSkillOperationRecorder()
	if err := r.StartWithTab(workDir, "owner", "tab"); err != nil {
		t.Fatal(err)
	}
	r.Record("bash", map[string]interface{}{
		"command": fmt.Sprintf("python %s %s", filepath.Join(workDir, "scripts", "run.py"), filepath.Join(workDir, "data")),
	}, "ok", true)
	r.Record("write_file", map[string]interface{}{
		"path":    filepath.Join(workDir, "out", "report.txt"),
		"content": "see " + filepath.Join(workDir, "data") + "\n",
	}, "ok", true)
	r.Record("write_file", map[string]interface{}{
		"path":    foreignTarget,
		"content": "key=value\n",
	}, "ok", true)

	skillDir, warnings, err := r.Stop("", "")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	yamlBytes, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatalf("read skill.yaml: %v", err)
	}
	yamlText := string(yamlBytes)

	// No machine-specific paths anywhere in the skill definition.
	if strings.Contains(yamlText, workDir) {
		t.Fatalf("workDir leaked into skill.yaml:\n%s", yamlText)
	}
	if strings.Contains(yamlText, `D:\external`) || strings.Contains(yamlText, "D:/external") {
		t.Fatalf("foreign path leaked into skill.yaml:\n%s", yamlText)
	}
	// Placeholders present and declared as required args.
	for _, want := range []string{workspacePlaceholder, "{{config_ini}}", "required_args", "workspace", "config_ini"} {
		if !strings.Contains(yamlText, want) {
			t.Fatalf("skill.yaml missing %q:\n%s", want, yamlText)
		}
	}
	// Write targets must not resolve into the skill install dir.
	if strings.Contains(yamlText, "'{baseDir}/out/report.txt'") {
		t.Fatalf("write target still uses {baseDir}:\n%s", yamlText)
	}

	// The on-disk skill should pass portability validation without
	// hardcoded-path findings (auto-fix already ran inside Stop).
	report, err := skill.ValidateSkillPortability(skillDir)
	if err != nil {
		t.Fatalf("ValidateSkillPortability: %v", err)
	}
	for _, issue := range report.Issues {
		if issue.Category == "hardcoded_path" && issue.Severity != skill.SeverityInfo {
			t.Errorf("hardcoded path issue remains: %+v", issue)
		}
	}
	_ = warnings // warnings are advisory; the assertions above are authoritative
}
