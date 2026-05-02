package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMarkdownSkillDir_CreatesCraftToolWhenNoScripts(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: PPTX Generator\ndescription: Build decks\ncompatibility: claude\n---\n\n# PPTX Generator\n\nGenerate presentations."
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if entry.Name != "PPTX Generator" {
		t.Fatalf("Name = %q, want %q", entry.Name, "PPTX Generator")
	}
	if entry.Description != "Build decks" {
		t.Fatalf("Description = %q, want %q", entry.Description, "Build decks")
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", entry.Steps)
	}
	if got := entry.Steps[0].Params["instructions"]; got != strings.TrimSpace(content) {
		t.Fatalf("unexpected instructions: %#v", got)
	}
	if got := entry.Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
	if got := entry.Steps[0].Params["verification_mode"]; got != "artifact_required" {
		t.Fatalf("verification_mode = %#v, want %q", got, "artifact_required")
	}
	if got := entry.Steps[0].Params["register_policy"]; got != "manual" {
		t.Fatalf("register_policy = %#v, want %q", got, "manual")
	}
	if entry.SourceProject != "claude" {
		t.Fatalf("SourceProject = %q, want %q", entry.SourceProject, "claude")
	}
}

func TestImportMarkdownSkillDir_AcceptsUppercaseSkillMarkdown(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "rapidocr")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: RapidOCR\ndescription: OCR\n---\n\n# RapidOCR\n\nUse OCR."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if entry.Name != "RapidOCR" {
		t.Fatalf("Name = %q, want %q", entry.Name, "RapidOCR")
	}
}

func TestImportMarkdownSkillDir_UsesScriptsAsBashSteps(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "browser-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Browser Skill\n\nAutomate browser."), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}
	shPath := filepath.Join(scriptsDir, "01_setup.sh")
	if err := os.WriteFile(shPath, []byte("echo setup"), 0o755); err != nil {
		t.Fatalf("WriteFile(script1) error = %v", err)
	}
	jsPath := filepath.Join(scriptsDir, "02_run.mjs")
	if err := os.WriteFile(jsPath, []byte("console.log('run')"), 0o755); err != nil {
		t.Fatalf("WriteFile(script2) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "agent_skill", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2", len(entry.Steps))
	}
	if got := entry.Steps[0].Params["command"]; got != "bash \""+strings.ReplaceAll(filepath.ToSlash(shPath), "\"", `\\"`)+"\"" {
		t.Fatalf("step 0 command = %#v", got)
	}
	wantNodeCommand := "node \"" + strings.ReplaceAll(filepath.ToSlash(jsPath), "\"", `\\"`) + "\""
	if got := entry.Steps[1].Params["command"]; got != wantNodeCommand {
		t.Fatalf("step 1 command = %#v, want %q", got, wantNodeCommand)
	}
	if strings.Contains(entry.Steps[1].Params["command"].(string), "/path/in.md") || strings.Contains(entry.Steps[1].Params["command"].(string), "/绝对路径/输入.md") {
		t.Fatalf("step 1 command still contains example placeholders: %#v", entry.Steps[1].Params["command"])
	}
	for i, step := range entry.Steps {
		if step.Action != "bash" {
			t.Fatalf("step %d action = %q, want bash", i, step.Action)
		}
		if got := step.Params["working_dir"]; got != skillDir {
			t.Fatalf("step %d working_dir = %#v, want %q", i, got, skillDir)
		}
	}
}

func TestExtractBashBlocksFromMarkdown_SkipsChinesePlaceholders(t *testing.T) {
	content := `# Test Skill

First block is a usage example with Chinese path:

` + "```bash" + `
node "{baseDir}/scripts/convert.mjs" "/绝对路径/输入.md" "/绝对路径/输出.pdf"
` + "```\n" + `

Second block is actual executable:

` + "```bash" + `
node /home/user/scripts/convert.mjs input.md output.pdf
` + "```\n"

	blocks := extractBashBlocksFromMarkdown(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 executable block, got %d", len(blocks))
	}
	if strings.Contains(blocks[0], "绝对路径") {
		t.Fatalf("block still contains Chinese placeholder: %s", blocks[0])
	}
}

func TestExtractBashBlocksFromMarkdown_SkipsUnresolvedPlaceholders(t *testing.T) {
	// {baseDir} unresolved is a sign of an example, not executable
	content := "```bash\nnode \"{baseDir}/scripts/script.mjs\"\n```\n"
	blocks := extractBashBlocksFromMarkdown(content)
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks (unresolved {baseDir}), got %d", len(blocks))
	}
}

func TestExtractBashBlocksFromMarkdown_AcceptsCleanBlocks(t *testing.T) {
	content := "```bash\necho hello world\n```"
	blocks := extractBashBlocksFromMarkdown(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "echo hello world") {
		t.Fatalf("unexpected block content: %s", blocks[0])
	}
}

func TestScanSkillDir_RecognizesMarkdownSkillDirectory(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# PPTX Generator\n\nGenerate slides."), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if skills[0].Name != "PPTX Generator" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "PPTX Generator")
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
}

func TestScanSkillDir_RecognizesUppercaseMarkdownSkillDirectory(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "rapidocr")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# RapidOCR\n\nUse OCR."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if skills[0].Name != "RapidOCR" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "RapidOCR")
	}
}

func TestScanSkillDir_PrefersYAMLOverMarkdown(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: yaml-skill\ndescription: yaml\nsteps:\n  - action: bash\n    params:\n      command: echo yaml\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Markdown Skill\n\nmarkdown"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if skills[0].Name != "yaml-skill" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "yaml-skill")
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "bash" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
}

func TestScanSkillDir_FallsBackToMarkdownWhenYAMLHasNoSteps(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: pptx-generator\ndescription: yaml metadata only\nversion: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("---\nname: PPTX Generator\ndescription: Build decks\nmode: api_workflow\nproduces_artifact: false\nrequires_gui: true\nparams:\n  - name: input\n    required: true\n---\n\n# PPTX Generator\n\nGenerate slides."), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
	if got := skills[0].Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
	if skills[0].Mode != "api_workflow" || skills[0].ProducesArtifact {
		t.Fatalf("markdown frontmatter was overwritten: mode=%q produces=%v", skills[0].Mode, skills[0].ProducesArtifact)
	}
	if !skills[0].RequiresGUI {
		t.Fatalf("RequiresGUI = false, want markdown frontmatter true")
	}
	if len(skills[0].Params) != 1 || skills[0].Params[0].Name != "input" || !skills[0].Params[0].Required {
		t.Fatalf("Params = %+v, want markdown frontmatter param", skills[0].Params)
	}
}

// --- Bug #1: quoteScriptPath should not double-quote already-quoted paths ---

func TestQuoteScriptPath_NoDoubleQuote(t *testing.T) {
	// Already quoted path should be returned as-is.
	input := `"C:/Users/test/.maclaw/data/skills/test-skill/scripts/diag.mjs"`
	got := quoteScriptPath(input)
	if got != input {
		t.Fatalf("quoteScriptPath(%q) = %q, want unchanged", input, got)
	}
}

func TestQuoteScriptPath_AddsQuotesWhenNeeded(t *testing.T) {
	input := `C:\Users\test\.maclaw\data\skills\test-skill\scripts\diag.mjs`
	got := quoteScriptPath(input)
	// Should convert backslashes and add quotes.
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("quoteScriptPath(%q) = %q, expected quoted", input, got)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("quoteScriptPath(%q) = %q, should not contain backslashes", input, got)
	}
}

func TestQuoteScriptPath_SimplePath(t *testing.T) {
	input := "scripts/diag.mjs"
	got := quoteScriptPath(input)
	if got != `"scripts/diag.mjs"` {
		t.Fatalf("quoteScriptPath(%q) = %q, want %q", input, got, `"scripts/diag.mjs"`)
	}
}

// --- Bug #1: commandFromSkillMarkdown should produce correct paths with {baseDir} ---

func TestCommandFromSkillMarkdown_BaseDirSubstitution(t *testing.T) {
	skillDir := filepath.Join("C:", "Users", "test", ".maclaw", "data", "skills", "test-runner")
	scriptPath := filepath.Join(skillDir, "scripts", "diag.mjs")
	markdown := "# Test\n\n```bash\nnode \"{baseDir}/scripts/diag.mjs\"\n```\n"

	command, ok := commandFromSkillMarkdown(scriptPath, markdown, skillDir)
	if !ok {
		t.Fatalf("commandFromSkillMarkdown() returned false")
	}
	slashDir := filepath.ToSlash(skillDir)
	expected := `node "` + slashDir + `/scripts/diag.mjs"`
	if command != expected {
		t.Fatalf("command = %q, want %q", command, expected)
	}
	// The command should NOT contain {baseDir} anymore.
	if strings.Contains(command, "{baseDir}") {
		t.Fatalf("command still contains {baseDir}: %q", command)
	}
	// The command should NOT have double skillDir.
	count := strings.Count(command, slashDir)
	if count != 1 {
		t.Fatalf("command contains skillDir %d times (want 1): %q", count, command)
	}
}

// --- Bug #2: absolute path commands without {baseDir} should be bash steps ---

func TestImportMarkdownSkillDir_AbsolutePathBashBlock(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "test-nq")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Skill with an absolute path command (no {baseDir}), no scripts/ directory.
	// This should be recognized as a bash step, not fall through to craft_tool.
	content := "# test-nq\n\nDiagnostic skill.\n\n```bash\nnode /home/user/skills/test-nq/full-diag.mjs\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 1 {
		t.Fatalf("Steps len = %d, want 1; steps = %+v", len(entry.Steps), entry.Steps)
	}
	if entry.Steps[0].Action != "bash" {
		t.Fatalf("step action = %q, want %q (was incorrectly assigned to craft_tool)", entry.Steps[0].Action, "bash")
	}
	cmd, _ := entry.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd, "full-diag.mjs") {
		t.Fatalf("step command = %q, expected to contain full-diag.mjs", cmd)
	}
}

func TestImportMarkdownSkillDir_AbsolutePathWindowsStyle(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "test-nq-win")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Windows-style absolute path without {baseDir}.
	content := "# test-nq-win\n\n```bash\nnode C:/Users/test/skills/test-nq/full-diag.mjs\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 1 {
		t.Fatalf("Steps len = %d, want 1", len(entry.Steps))
	}
	if entry.Steps[0].Action != "bash" {
		t.Fatalf("step action = %q, want bash", entry.Steps[0].Action)
	}
}

// --- P0: Multi-step parsing — multiple bash blocks should all become steps ---

func TestImportMarkdownSkillDir_MultipleBashBlocksAllBecomeSteps(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "multi-step")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "# Multi Step Test\n\n" +
		"## 步骤1: 准备\n\n" +
		"```bash\necho step1\n```\n\n" +
		"## 步骤2: 执行\n\n" +
		"```bash\necho step2\n```\n\n" +
		"## 步骤3: 清理\n\n" +
		"```bash\necho step3\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 3 {
		t.Fatalf("Steps len = %d, want 3; steps = %+v", len(entry.Steps), entry.Steps)
	}
	for i, step := range entry.Steps {
		if step.Action != "bash" {
			t.Fatalf("step %d action = %q, want bash", i, step.Action)
		}
		cmd, _ := step.Params["command"].(string)
		expected := "echo step" + string(rune('1'+i))
		if !strings.Contains(cmd, expected) {
			t.Fatalf("step %d command = %q, expected to contain %q", i, cmd, expected)
		}
	}
}

// --- P0: Mixed steps — scripts + direct bash blocks should all be included ---

func TestImportMarkdownSkillDir_MixedScriptsAndDirectBlocks(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "mixed-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Create one script file
	scriptPath := filepath.Join(scriptsDir, "setup.mjs")
	if err := os.WriteFile(scriptPath, []byte("console.log('setup')"), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	// Markdown references the script AND has a direct bash block
	content := "# Mixed Skill\n\n" +
		"```bash\nnode \"{baseDir}/scripts/setup.mjs\"\n```\n\n" +
		"```bash\necho direct-command\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	// Should have 2 steps: one from scripts/ and one direct bash block
	if len(entry.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2; steps = %+v", len(entry.Steps), entry.Steps)
	}
	// First step should reference setup.mjs
	cmd0, _ := entry.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd0, "setup.mjs") {
		t.Fatalf("step 0 command = %q, expected to contain setup.mjs", cmd0)
	}
	// Second step should be the direct echo command
	cmd1, _ := entry.Steps[1].Params["command"].(string)
	if !strings.Contains(cmd1, "echo direct-command") {
		t.Fatalf("step 1 command = %q, expected to contain 'echo direct-command'", cmd1)
	}
}

// --- Frontmatter: required_args and required_env parsing ---

func TestParseSkillMarkdownDocument_ExtendedFrontmatter(t *testing.T) {
	content := "---\nname: test-skill\nrequired_args: input, output\nrequires_env: API_KEY, SECRET\nshell: bash\n---\n\n# Test\n\nA test skill."
	parsed, err := parseSkillMarkdownDocument(content, "", "")
	if err != nil {
		t.Fatalf("parseSkillMarkdownDocument() error = %v", err)
	}
	if len(parsed.requiredArgs) != 2 || parsed.requiredArgs[0] != "input" || parsed.requiredArgs[1] != "output" {
		t.Fatalf("requiredArgs = %v, want [input output]", parsed.requiredArgs)
	}
	if len(parsed.requiredEnv) != 2 || parsed.requiredEnv[0] != "API_KEY" || parsed.requiredEnv[1] != "SECRET" {
		t.Fatalf("requiredEnv = %v, want [API_KEY SECRET]", parsed.requiredEnv)
	}
	if parsed.preferredShell != "bash" {
		t.Fatalf("preferredShell = %q, want %q", parsed.preferredShell, "bash")
	}
}

func TestImportMarkdownSkillDir_PropagatesExtendedFrontmatter(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "args-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: args-skill\nrequired_args: input, output\nrequires_env: MY_TOKEN\nshell: bash\n---\n\n# Args Skill\n\n```bash\necho hello\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.RequiredArgs) != 2 {
		t.Fatalf("RequiredArgs = %v, want 2 items", entry.RequiredArgs)
	}
	if len(entry.RequiredEnv) != 1 || entry.RequiredEnv[0] != "MY_TOKEN" {
		t.Fatalf("RequiredEnv = %v, want [MY_TOKEN]", entry.RequiredEnv)
	}
	if entry.PreferredShell != "bash" {
		t.Fatalf("PreferredShell = %q, want %q", entry.PreferredShell, "bash")
	}
}

// --- splitCSV helper ---

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"  single  ", []string{"single"}},
		{"", nil},
		{" , , ", nil},
	}
	for _, tt := range tests {
		got := splitCSV(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// --- P0-3.1: Step ordering — blocks should execute in markdown order ---

func TestImportMarkdownSkillDir_StepOrderMatchesMarkdown(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "order-test")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Create a script file
	if err := os.WriteFile(filepath.Join(scriptsDir, "step2.mjs"), []byte("console.log('step2')"), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	// Markdown: step1 (direct) → step2 (script ref) → step3 (direct)
	content := "# Order Test\n\n" +
		"## Step 1\n\n" +
		"```bash\necho step1\n```\n\n" +
		"## Step 2\n\n" +
		"```bash\nnode \"{baseDir}/scripts/step2.mjs\"\n```\n\n" +
		"## Step 3\n\n" +
		"```bash\necho step3\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 3 {
		t.Fatalf("Steps len = %d, want 3; steps = %+v", len(entry.Steps), entry.Steps)
	}
	// Step 1 should be "echo step1"
	cmd0, _ := entry.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd0, "echo step1") {
		t.Errorf("step 0 command = %q, expected 'echo step1'", cmd0)
	}
	// Step 2 should reference step2.mjs
	cmd1, _ := entry.Steps[1].Params["command"].(string)
	if !strings.Contains(cmd1, "step2.mjs") {
		t.Errorf("step 1 command = %q, expected to contain 'step2.mjs'", cmd1)
	}
	// Step 3 should be "echo step3"
	cmd2, _ := entry.Steps[2].Params["command"].(string)
	if !strings.Contains(cmd2, "echo step3") {
		t.Errorf("step 2 command = %q, expected 'echo step3'", cmd2)
	}
}

// --- P0-3.2: Chinese path in {baseDir} should not be skipped ---

func TestImportMarkdownSkillDir_ChineseSubdirNotSkipped(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "chinese-path")
	chineseDir := filepath.Join(skillDir, "脚本目录")
	if err := os.MkdirAll(chineseDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(chineseDir, "test.mjs"), []byte("console.log('ok')"), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	content := "# Chinese Path Test\n\n" +
		"```bash\nnode \"{baseDir}/脚本目录/test.mjs\"\n```\n\n" +
		"```bash\necho done\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2; steps = %+v", len(entry.Steps), entry.Steps)
	}
	// First step should contain the Chinese path (resolved)
	cmd0, _ := entry.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd0, "test.mjs") {
		t.Errorf("step 0 command = %q, expected to contain 'test.mjs'", cmd0)
	}
	// Should NOT contain {baseDir} anymore
	if strings.Contains(cmd0, "{baseDir}") {
		t.Errorf("step 0 command still contains {baseDir}: %q", cmd0)
	}
	// Second step should be "echo done"
	cmd1, _ := entry.Steps[1].Params["command"].(string)
	if !strings.Contains(cmd1, "echo done") {
		t.Errorf("step 1 command = %q, expected 'echo done'", cmd1)
	}
}

func TestExtractCaptureDirectives_BasicCapture(t *testing.T) {
	content := `# Test Skill

<!-- extract: SESSION_ID=sessionId[":]\s*([a-f0-9-]+) -->
` + "```bash\npython3 create_session.py\n```" + `

` + "```bash\npython3 query_session.py {{SESSION_ID}}\n```" + `
`
	directives := extractCaptureDirectives(content)
	if len(directives) != 2 {
		t.Fatalf("extractCaptureDirectives() returned %d entries, want 2", len(directives))
	}
	if directives[0] == nil {
		t.Fatal("directives[0] is nil, expected capture map")
	}
	if directives[0]["SESSION_ID"] == "" {
		t.Error("directives[0] missing SESSION_ID key")
	}
	if directives[1] != nil {
		t.Errorf("directives[1] = %v, want nil (no capture for second block)", directives[1])
	}
}

func TestExtractCaptureDirectives_NoCaptures(t *testing.T) {
	content := "```bash\necho hello\n```\n\n```bash\necho world\n```\n"
	directives := extractCaptureDirectives(content)
	if len(directives) != 2 {
		t.Fatalf("extractCaptureDirectives() returned %d entries, want 2", len(directives))
	}
	if directives[0] != nil || directives[1] != nil {
		t.Errorf("expected nil directives, got %v", directives)
	}
}

func TestExtractCaptureDirectives_SkipsNorunBlocks(t *testing.T) {
	content := `<!-- extract: VAR=pattern -->
` + "```bash.norun\necho example\n```" + `

` + "```bash\necho real\n```" + `
`
	directives := extractCaptureDirectives(content)
	// The .norun block should be skipped, and the extract comment should be
	// cleared (not attached to the next non-norun block).
	if len(directives) != 1 {
		t.Fatalf("extractCaptureDirectives() returned %d entries, want 1", len(directives))
	}
	if directives[0] != nil {
		t.Errorf("directives[0] = %v, want nil (capture was before .norun block)", directives[0])
	}
}

func TestExtractCaptureDirectives_SkipsEmptyBlocks(t *testing.T) {
	content := "```bash\n\n```\n\n```bash\necho hello\n```\n"
	directives := extractCaptureDirectives(content)
	// Empty block should be skipped (matching extractAllBashBlocksFromMarkdown behavior)
	if len(directives) != 1 {
		t.Fatalf("extractCaptureDirectives() returned %d entries, want 1", len(directives))
	}
}

func TestExtractCaptureDirectives_MultipleCaptures(t *testing.T) {
	content := `<!-- extract: ID=id:\s*(\w+) -->
<!-- extract: TOKEN=token:\s*(\w+) -->
` + "```bash\npython3 auth.py\n```" + `
`
	directives := extractCaptureDirectives(content)
	if len(directives) != 1 {
		t.Fatalf("extractCaptureDirectives() returned %d entries, want 1", len(directives))
	}
	if directives[0] == nil {
		t.Fatal("directives[0] is nil")
	}
	if directives[0]["ID"] == "" || directives[0]["TOKEN"] == "" {
		t.Errorf("expected both ID and TOKEN captures, got %v", directives[0])
	}
}

func TestStripBashCommentLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips comment lines",
			input: "# Text extraction\npython -m markitdown presentation.pptx",
			want:  "python -m markitdown presentation.pptx",
		},
		{
			name:  "preserves non-comment lines",
			input: "echo hello\necho world",
			want:  "echo hello\necho world",
		},
		{
			name:  "strips shebang",
			input: "#!/bin/bash\necho hello",
			want:  "echo hello",
		},
		{
			name:  "handles empty input",
			input: "",
			want:  "",
		},
		{
			name:  "strips all comment lines",
			input: "# comment 1\n# comment 2",
			want:  "",
		},
		{
			name:  "preserves indented non-comment lines",
			input: "# header\n  python script.py\n# footer",
			want:  "  python script.py",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripBashCommentLines(tt.input)
			if got != tt.want {
				t.Errorf("StripBashCommentLines(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
