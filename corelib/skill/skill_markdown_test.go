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
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("---\nname: PPTX Generator\ndescription: Build decks\n---\n\n# PPTX Generator\n\nGenerate slides."), 0o644); err != nil {
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
}
