package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func TestSubstituteSkillVariables_ReplacesSupportedPlaceholders(t *testing.T) {
	vars := map[string]string{"input": "hello world", "output": "report.pdf"}
	got := substituteSkillVariables("echo {{input}} && echo ${output}", vars)
	want := "echo " + quoteSkillInputForShell(vars["input"]) + " && echo " + quoteSkillInputForShell(vars["output"])
	if got != want {
		t.Fatalf("substituteSkillVariables() = %q, want %q", got, want)
	}
}

func TestSubstituteSkillVariables_ReplacesGenericArgs(t *testing.T) {
	vars := map[string]string{"format": "A4", "profile": "zh-CN"}
	got := substituteSkillVariables("echo {{format}} && echo ${profile}", vars)
	want := "echo " + quoteSkillInputForShell(vars["format"]) + " && echo " + quoteSkillInputForShell(vars["profile"])
	if got != want {
		t.Fatalf("substituteSkillVariables() = %q, want %q", got, want)
	}
}

func TestSubstituteSkillVariables_LeavesUnknownPlaceholderUntouched(t *testing.T) {
	got := substituteSkillVariables("echo {{missing}}", map[string]string{"input": "ignored"})
	if got != "echo {{missing}}" {
		t.Fatalf("substituteSkillVariables() = %q, want %q", got, "echo {{missing}}")
	}
}

func TestSubstituteSkillVariables_LeavesCommandUnchangedWithoutPlaceholder(t *testing.T) {
	const command = "echo fixed"
	if got := substituteSkillVariables(command, map[string]string{"input": "ignored", "output": "ignored"}); got != command {
		t.Fatalf("substituteSkillVariables() = %q, want %q", got, command)
	}
}

func TestNormalizeRunSkillVars_ArgsOverrideLegacy(t *testing.T) {
	got := normalizeRunSkillVars(map[string]interface{}{
		"args":   map[string]interface{}{"input": "new-in", "output": "new-out"},
		"input":  "old-in",
		"output": "old-out",
	})
	if got["input"] != "new-in" || got["output"] != "new-out" {
		t.Fatalf("normalizeRunSkillVars() = %#v, want args values to win", got)
	}
}

func TestNormalizeRunSkillVars_LegacyFillsMissingKeys(t *testing.T) {
	got := normalizeRunSkillVars(map[string]interface{}{
		"args":   map[string]interface{}{"input": "new-in"},
		"output": "old-out",
	})
	if got["input"] != "new-in" || got["output"] != "old-out" {
		t.Fatalf("normalizeRunSkillVars() = %#v, want mixed args+legacy values", got)
	}
}

func TestNormalizeRunSkillVars_CoercesNonStringArgs(t *testing.T) {
	got := normalizeRunSkillVars(map[string]interface{}{
		"args": map[string]interface{}{"count": 3, "enabled": true, "format": "pdf"},
	})
	// Non-string values are coerced via fmt.Sprintf (aligned with GUI behavior).
	if len(got) != 3 || got["format"] != "pdf" || got["count"] != "3" || got["enabled"] != "true" {
		t.Fatalf("normalizeRunSkillVars() = %#v, want all args coerced to strings", got)
	}
}

func TestQuoteSkillInputForShell_EscapesQuotes(t *testing.T) {
	input := "a'b"
	got := quoteSkillInputForShell(input)
	if runtime.GOOS == "windows" {
		if got != `"a'b"` {
			t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `"a'b"`)
		}
		return
	}
	if got != `'a'"'"'b'` {
		t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `'a'"'"'b'`)
	}
}

func TestRunSkillStep_MissingCommand(t *testing.T) {
	_, err := runSkillStep(corelib.NLSkillStep{Action: "bash", Params: map[string]interface{}{}}, "", nil)
	if err == nil || err.Error() != "missing command parameter" {
		t.Fatalf("runSkillStep() error = %v, want missing command parameter", err)
	}
}

func TestTUIAgentHandlerToolRunSkill_UsesInputPlaceholderAndUpdatesStats(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "demo-skill",
		Status:   "active",
		SkillDir: tempHome,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": shellPrintInputCommand("{{input}}")},
		}},
	}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := &TUIAgentHandler{}
	got := h.toolRunSkill(map[string]interface{}{"skill_name": "demo-skill", "input": "hello world"})
	if !strings.Contains(got, "hello world") {
		t.Fatalf("expected output to contain input, got %s", got)
	}
	if !strings.Contains(got, "✅ 技能 'demo-skill' 全部完成") {
		t.Fatalf("expected success summary, got %s", got)
	}

	reloaded, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after run error = %v", err)
	}
	if len(reloaded.NLSkills) != 1 {
		t.Fatalf("reloaded skill count = %d, want 1", len(reloaded.NLSkills))
	}
	if reloaded.NLSkills[0].UsageCount != 1 {
		t.Fatalf("UsageCount = %d, want 1", reloaded.NLSkills[0].UsageCount)
	}
	if reloaded.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("SuccessCount = %d, want 1", reloaded.NLSkills[0].SuccessCount)
	}
	if reloaded.NLSkills[0].LastError != "" {
		t.Fatalf("LastError = %q, want empty", reloaded.NLSkills[0].LastError)
	}
}

func TestTUIAgentHandlerToolRunSkill_UsesInputAndOutputPlaceholders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "demo-skill",
		Status:   "active",
		SkillDir: tempHome,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": shellPrintInputOutputCommand("{{input}}", "${output}")},
		}},
	}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := &TUIAgentHandler{}
	got := h.toolRunSkill(map[string]interface{}{"skill_name": "demo-skill", "input": "in.md", "output": "out.pdf"})
	if !strings.Contains(got, "in.md") || !strings.Contains(got, "out.pdf") {
		t.Fatalf("expected output to contain input and output, got %s", got)
	}
}

func TestTUIAgentHandlerToolRunSkill_UsesArgsMapPlaceholders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "demo-skill",
		Status:   "active",
		SkillDir: tempHome,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": shellPrintInputOutputCommand("{{source}}", "${format}")},
		}},
	}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := &TUIAgentHandler{}
	got := h.toolRunSkill(map[string]interface{}{
		"skill_name": "demo-skill",
		"args":       map[string]interface{}{"source": "report.md", "format": "pdf"},
	})
	if !strings.Contains(got, "report.md") || !strings.Contains(got, "pdf") {
		t.Fatalf("expected output to contain args map values, got %s", got)
	}
}

func TestTUIAgentHandlerToolRunSkill_ArgsOverrideLegacyInputOutput(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "demo-skill",
		Status:   "active",
		SkillDir: tempHome,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": shellPrintInputOutputCommand("{{input}}", "${output}")},
		}},
	}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := &TUIAgentHandler{}
	got := h.toolRunSkill(map[string]interface{}{
		"skill_name": "demo-skill",
		"input":      "legacy-in",
		"output":     "legacy-out",
		"args":       map[string]interface{}{"input": "args-in", "output": "args-out"},
	})
	if strings.Contains(got, "legacy-in") || strings.Contains(got, "legacy-out") {
		t.Fatalf("expected args values to override legacy ones, got %s", got)
	}
	if !strings.Contains(got, "args-in") || !strings.Contains(got, "args-out") {
		t.Fatalf("expected args values in output, got %s", got)
	}
}

func TestTUIAgentHandlerToolRunSkill_ContinuesOnErrorWithInput(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "demo-skill",
		Status:   "active",
		SkillDir: tempHome,
		Steps: []corelib.NLSkillStep{
			{
				Action:  "bash",
				OnError: "continue",
				Params:  map[string]interface{}{"command": shellFailCommand()},
			},
			{
				Action: "bash",
				Params: map[string]interface{}{"command": shellPrintInputCommand("${input}")},
			},
		},
	}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	h := &TUIAgentHandler{}
	got := h.toolRunSkill(map[string]interface{}{"skill_name": "demo-skill", "input": "follow-up"})
	if !strings.Contains(got, "⚠️ 步骤失败但 on_error=continue，继续下一步") {
		t.Fatalf("expected continue hint, got %s", got)
	}
	if !strings.Contains(got, "follow-up") {
		t.Fatalf("expected second step output to contain input, got %s", got)
	}
	if !strings.Contains(got, "❌ 技能 'demo-skill' 执行失败") {
		t.Fatalf("expected failure summary after continued error, got %s", got)
	}
}

func shellPrintInputCommand(placeholder string) string {
	if runtime.GOOS == "windows" {
		return "cmd /c echo " + placeholder
	}
	return "printf '%s\\n' " + placeholder
}

func shellPrintInputOutputCommand(inputPlaceholder, outputPlaceholder string) string {
	if runtime.GOOS == "windows" {
		return "cmd /c echo " + inputPlaceholder + " & cmd /c echo " + outputPlaceholder
	}
	return "printf '%s\\n' " + inputPlaceholder + " " + outputPlaceholder
}

func shellFailCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit /b 1"
	}
	return "echo boom >&2; exit 1"
}
