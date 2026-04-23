package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ---------------------------------------------------------------------------
// Tests for the unified skill doc injection mechanism
// (appendKnowledgeSkillSection extended to cover executable skills with SKILL.md)
// ---------------------------------------------------------------------------

// TestSkillDocInjection_KnowledgeSkillStillWorks verifies that the original
// knowledge skill injection path is preserved after the mechanism extension.
func TestSkillDocInjection_KnowledgeSkillStillWorks(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	// Register a knowledge skill with inline content.
	_ = exec.Register(corelib.NLSkillEntry{
		Name:        "git-workflow",
		Description: "Git workflow guide",
		Triggers:    []string{"git", "commit", "branch"},
		Type:        "knowledge",
		Content:     "Always use feature branches. Never push to main directly.",
		Status:      "active",
	})

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "how do I use git branches?")
	result := b.String()

	if !strings.Contains(result, "git-workflow") {
		t.Fatalf("expected knowledge skill to be injected, got: %s", result)
	}
	if !strings.Contains(result, "feature branches") {
		t.Fatalf("expected knowledge skill content, got: %s", result)
	}
}

// TestSkillDocInjection_ExecutableSkillWithDoc verifies that executable skills
// with SKILL.md get their documentation injected when triggers match.
func TestSkillDocInjection_ExecutableSkillWithDoc(t *testing.T) {
	// Isolate from real skill directories.
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	// Create a temp skill directory with SKILL.md
	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "drawio-skill")
	os.MkdirAll(skillDir, 0755)
	skillMD := `# drawio-skill

## 工作流程
1. 根据用户需求生成 drawio XML 格式的图表内容
2. 将 XML 内容保存为 .drawio 文件
3. 调用 run.js 将 .drawio 文件转换为 PNG

## 前置条件
- 需要先用 write_file 生成 .drawio XML 文件
- run.js 需要 .drawio 文件路径作为参数
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}
	// Also write a minimal skill.yaml so the scanner picks it up.
	skillYAML := `name: drawio-skill
description: "根据用户需求生成 drawio 流程图"
triggers: ["drawio", "流程图", "架构图"]
steps:
  - action: bash
    params:
      command: "node run.js"
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(skillYAML), 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "用 drawio skill 画一个北京5环图")
	result := b.String()

	if !strings.Contains(result, "drawio-skill") {
		t.Fatalf("expected executable skill doc to be injected, got: %s", result)
	}
	if !strings.Contains(result, "write_file") {
		t.Fatalf("expected SKILL.md content with prerequisites, got: %s", result)
	}
	if !strings.Contains(result, "run.js") {
		t.Fatalf("expected SKILL.md content with workflow, got: %s", result)
	}
}

// TestSkillDocInjection_NoMatchNoInjection verifies that skills whose triggers
// don't match the user message are not injected.
func TestSkillDocInjection_NoMatchNoInjection(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "drawio-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# drawio docs"), 0644)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: drawio-skill\ndescription: drawio\ntriggers: [drawio, 流程图]\nsteps:\n  - action: bash\n    params:\n      command: echo test\n"), 0644)

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "查询杭州天气")
	result := b.String()

	if strings.Contains(result, "drawio") {
		t.Fatalf("expected no injection for unrelated message, got: %s", result)
	}
}

// TestSkillDocInjection_NameMatchFallback verifies that a skill is matched
// when the user mentions the skill name even if no trigger matches.
func TestSkillDocInjection_NameMatchFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "mermaid-chart")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# mermaid-chart usage guide"), 0644)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: mermaid-chart\ndescription: Generate mermaid charts\ntriggers: [mermaid]\nsteps:\n  - action: bash\n    params:\n      command: echo test\n"), 0644)

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "使用 mermaid-chart 画一个流程图")
	result := b.String()

	if !strings.Contains(result, "mermaid-chart") {
		t.Fatalf("expected skill matched by name, got: %s", result)
	}
}

// TestSkillDocInjection_NoDocNoInjection verifies that executable skills
// without SKILL.md are not injected even when triggers match.
func TestSkillDocInjection_NoDocNoInjection(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "weather-query")
	os.MkdirAll(skillDir, 0755)
	// Only skill.yaml, no SKILL.md
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: weather-query\ndescription: 查询天气\ntriggers: [天气, weather]\nsteps:\n  - action: bash\n    params:\n      command: echo test\n"), 0644)

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "查询天气")
	result := b.String()

	if strings.Contains(result, "weather-query") {
		t.Fatalf("expected no injection for skill without documentation, got: %s", result)
	}
}

// TestSkillDocInjection_DisabledSkillNotInjected verifies that disabled skills
// are not injected regardless of trigger match.
func TestSkillDocInjection_DisabledSkillNotInjected(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "disabled-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# disabled skill docs"), 0644)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: disabled-skill\ndescription: disabled\ntriggers: [disabled]\nstatus: disabled\nsteps:\n  - action: bash\n    params:\n      command: echo test\n"), 0644)

	app := &App{}
	h := &IMMessageHandler{app: app}
	exec := &SkillExecutor{app: app}
	app.skillExecutor = exec

	var b strings.Builder
	h.appendKnowledgeSkillSection(&b, "use disabled skill")
	result := b.String()

	if strings.Contains(result, "disabled-skill") {
		t.Fatalf("expected no injection for disabled skill, got: %s", result)
	}
}
