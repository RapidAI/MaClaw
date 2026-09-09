package guiapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExportAgentSkillAllowsCriticalRiskByDefault(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "agent-export")
	err := ExportAgentSkill(corelib.NLSkillEntry{
		Name:        "dangerous-export",
		Description: "dangerous",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "rm -rf $HOME/.ssh"},
		}},
	}, outDir)
	if err != nil {
		t.Fatalf("ExportAgentSkill() error = %v, want allow", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "SKILL.md")); statErr != nil {
		t.Fatalf("ExportAgentSkill() did not write SKILL.md: %v", statErr)
	}
}
func TestExportAgentSkillAllowsHighRiskByDefault(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "agent-export-high")
	err := ExportAgentSkill(corelib.NLSkillEntry{
		Name:        "high-risk-export",
		Description: "high risk",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "chmod 777 /tmp/maclaw-test"},
		}},
	}, outDir)
	if err != nil {
		t.Fatalf("ExportAgentSkill() error = %v, want allow", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "SKILL.md")); statErr != nil {
		t.Fatalf("ExportAgentSkill() did not write SKILL.md: %v", statErr)
	}
}
func TestImportAgentSkill_UsesCraftToolWhenNoScripts(t *testing.T) {
	skillDir := t.TempDir()
	content := "---\nname: PPTX Generator\ndescription: Build decks\ncompatibility: claude\n---\n\n# PPTX Generator\n\nGenerate presentations."
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}

	entry, err := ImportAgentSkill(skillDir)
	if err != nil {
		t.Fatalf("ImportAgentSkill() error = %v", err)
	}
	if entry.Source != "agent_skill" {
		t.Fatalf("Source = %q, want %q", entry.Source, "agent_skill")
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", entry.Steps)
	}
	if got := entry.Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
	if entry.SourceProject != "claude" {
		t.Fatalf("SourceProject = %q, want %q", entry.SourceProject, "claude")
	}
}

func TestImportAgentSkillDirUsesDurableCommitAndRollsBackIndexFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	skillDir := filepath.Join(home, "agent-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(skillDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: Agent Import\ndescription: Imported agent skill\n---\n\n# Agent Import\n\nReusable instructions."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return errors.New("injected index provider failure")
		}
		return nil
	}

	if _, err := app.ImportAgentSkillDir(skillDir); err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("ImportAgentSkillDir() error = %v, want commit failure", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 0 {
		t.Fatalf("failed agent import remained registered: %#v", cfg.NLSkills)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want forward failure and rollback refresh", refreshCalls)
	}
}

func TestImportAgentSkill_UsesScriptsAsBashSteps(t *testing.T) {
	skillDir := t.TempDir()
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Browser Skill\n\nAutomate browser."), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}
	shPath := filepath.Join(scriptsDir, "step_01.sh")
	if err := os.WriteFile(shPath, []byte("echo one"), 0o755); err != nil {
		t.Fatalf("WriteFile(step_01.sh) error = %v", err)
	}
	jsPath := filepath.Join(scriptsDir, "step_02.mjs")
	if err := os.WriteFile(jsPath, []byte("console.log('two')"), 0o755); err != nil {
		t.Fatalf("WriteFile(step_02.mjs) error = %v", err)
	}

	entry, err := ImportAgentSkill(skillDir)
	if err != nil {
		t.Fatalf("ImportAgentSkill() error = %v", err)
	}
	if len(entry.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2", len(entry.Steps))
	}
	if got := entry.Steps[0].Params["command"]; got != "bash \""+strings.ReplaceAll(shPath, "\"", `\\"`)+"\"" {
		t.Fatalf("step 0 command = %#v", got)
	}
	wantNodeCommand := "node \"" + strings.ReplaceAll(jsPath, "\"", `\\"`) + "\""
	if got := entry.Steps[1].Params["command"]; got != wantNodeCommand {
		t.Fatalf("step 1 command = %#v, want %q", got, wantNodeCommand)
	}
	if strings.Contains(entry.Steps[1].Params["command"].(string), "/path/in.md") || strings.Contains(entry.Steps[1].Params["command"].(string), "/缂佹繂顕捄顖氱窞/鏉堟挸鍙?md") {
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
