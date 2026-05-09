package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestReportRegressionInstalledSkillShapes(t *testing.T) {
	root := t.TempDir()

	weather := loadReportRegressionSkill(t, root, "weather-query", map[string]string{
		"weather.py": "# \u9225? \u951b\nprint('weather')\n",
		"SKILL.md": `---
name: weather-query
required_args: [city]
produces_artifact: false
---

# Weather

## Usage

### Current weather

` + "```bash" + `
python "{baseDir}/weather.py" realtime --city "{{city}}"
` + "```" + `

### Hourly forecast

` + "```bash" + `
python "{baseDir}/weather.py" hourly --city "{{city}}"
` + "```" + `

### Weekly forecast

` + "```bash" + `
python "{baseDir}/weather.py" weekly --city "{{city}}"
` + "```" + `
`,
	})
	if len(weather.Steps) != 1 {
		t.Fatalf("weather steps = %#v, want one default usage alternative", weather.Steps)
	}
	weatherCmd, _ := weather.Steps[0].Params["command"].(string)
	if !strings.Contains(weatherCmd, "realtime") || strings.Contains(weatherCmd, "hourly") || strings.Contains(weatherCmd, "weekly") {
		t.Fatalf("weather command = %q, want realtime default only", weatherCmd)
	}
	weatherDiagnostics, err := CheckStepFileReferencesWithDiagnostics(weather)
	if err != nil {
		t.Fatalf("CheckStepFileReferencesWithDiagnostics(weather) error = %v", err)
	}
	if len(weatherDiagnostics) != 1 || !strings.Contains(weatherDiagnostics[0].Message, "mojibake") {
		t.Fatalf("weather diagnostics = %#v, want non-blocking mojibake warning", weatherDiagnostics)
	}
	weather.Params = CompleteParamsForRunner(weather.Params, weather.Steps, weather.RequiredArgs)
	weatherVars := NormalizeRunVars(map[string]interface{}{"input": "Chengdu"})
	ApplyRunInputInference(weather, weatherVars, map[string]interface{}{"input": "Chengdu"})
	if missing := MissingRunRequiredArgs(weather.RequiredArgs, weather.Params, weatherVars); len(missing) != 0 {
		t.Fatalf("weather missing required args = %#v, vars=%#v", missing, weatherVars)
	}
	resolvedWeather, err := ResolveStep(weather.Steps[0], weatherVars, weather.SkillDir, weather.Params, nil)
	if err != nil {
		t.Fatalf("ResolveStep(weather) error = %v", err)
	}
	weatherResolvedCmd, _ := resolvedWeather.Step.Params["command"].(string)
	if strings.Contains(weatherResolvedCmd, "{{city}}") || !strings.Contains(weatherResolvedCmd, "Chengdu") {
		t.Fatalf("resolved weather command = %q, want city substituted", weatherResolvedCmd)
	}

	drawio := loadReportRegressionSkill(t, root, "drawio-skill", map[string]string{
		"generate.js": "console.log(process.argv.slice(2).join(' '))\n",
		"skill.yaml": `name: drawio-skill
description: Generate draw.io diagrams from natural language.
command: node {baseDir}/generate.js {content}
platforms: [universal]
steps: []
`,
	})
	assertSingleExecutableCommand(t, drawio, "drawio-skill", "generate.js")
	drawio.Params = CompleteParamsForRunner(drawio.Params, drawio.Steps, drawio.RequiredArgs)
	drawioVars := NormalizeRunVars(map[string]interface{}{"input": "flowchart LR; A-->B"})
	ApplyRunInputInference(drawio, drawioVars, map[string]interface{}{"input": "flowchart LR; A-->B"})
	if implicit := DetectImplicitRunRequiredArgs(drawio.Steps, drawioVars, drawio.RequiredArgs, drawio.Params); len(implicit) != 0 {
		t.Fatalf("drawio implicit required args = %#v, vars=%#v", implicit, drawioVars)
	}
	resolvedDrawio, err := ResolveStep(drawio.Steps[0], drawioVars, drawio.SkillDir, drawio.Params, nil)
	if err != nil {
		t.Fatalf("ResolveStep(drawio) error = %v", err)
	}
	drawioCmd, _ := resolvedDrawio.Step.Params["command"].(string)
	if strings.Contains(drawioCmd, "{baseDir}") || strings.Contains(drawioCmd, "{content}") || !strings.Contains(drawioCmd, "flowchart") {
		t.Fatalf("resolved drawio command = %q, want baseDir/content resolved", drawioCmd)
	}

	mdToPDF := loadReportRegressionSkill(t, root, "xh-md-to-pdf", map[string]string{
		"scripts/xh-md-to-pdf.mjs": "console.log('pdf')\n",
		"skill.yaml": `name: xh-md-to-pdf
description: Convert Markdown to PDF.
platforms: [universal]
steps: []
`,
		"SKILL.md": `---
name: xh-md-to-pdf
---

# Markdown to PDF

## Recommended execution methods

### Local skill command

` + "```bash" + `
node "{baseDir}/scripts/xh-md-to-pdf.mjs" "/path/in.md" "/path/out.pdf"
` + "```" + `

Optional custom CSS:

` + "```bash" + `
node "{baseDir}/scripts/xh-md-to-pdf.mjs" "/path/in.md" "/path/out.pdf" --css "/path/custom.css"
` + "```" + `
`,
	})
	assertSingleExecutableCommand(t, mdToPDF, "xh-md-to-pdf", "xh-md-to-pdf.mjs")
	mdCmd, _ := mdToPDF.Steps[0].Params["command"].(string)
	if strings.Contains(mdCmd, "--css") || !strings.Contains(mdCmd, "{{input}}") || !strings.Contains(mdCmd, "{{output}}") {
		t.Fatalf("md-to-pdf command = %q, want default local command with input/output placeholders", mdCmd)
	}

	simpleTrans := loadReportRegressionSkill(t, root, "simple_trans", map[string]string{
		"translate.py": "import os\nprint(os.environ['OPENAI_API_KEY'])\n",
		"SKILL.md": `---
name: simple_trans
requires_env: OPENAI_API_KEY
required_args: text
produces_artifact: false
---

# Simple Trans

## Usage

` + "```bash" + `
python "{baseDir}/translate.py" --text "{{text}}"
` + "```" + `

` + "```bash" + `
python "{baseDir}/translate.py" --text "{{text}}" --target_lang "{{target_lang}}"
` + "```" + `
`,
	})
	simpleTrans.Params = CompleteParamsForRunner(simpleTrans.Params, simpleTrans.Steps, simpleTrans.RequiredArgs)
	if len(simpleTrans.Steps) != 1 {
		t.Fatalf("simple_trans steps = %#v, want one default usage alternative", simpleTrans.Steps)
	}
	simpleCmd, _ := simpleTrans.Steps[0].Params["command"].(string)
	if strings.Contains(simpleCmd, "target_lang") {
		t.Fatalf("simple_trans command = %q, want simplest required-args alternative", simpleCmd)
	}
	simpleVars := NormalizeRunVars(map[string]interface{}{"input": "hello"})
	ApplyRunInputInference(simpleTrans, simpleVars, map[string]interface{}{"input": "hello"})
	if missing := MissingRunRequiredArgs(simpleTrans.RequiredArgs, simpleTrans.Params, simpleVars); len(missing) != 0 {
		t.Fatalf("simple_trans missing required args = %#v, vars=%#v", missing, simpleVars)
	}
	if implicit := DetectImplicitRunRequiredArgs(simpleTrans.Steps, simpleVars, simpleTrans.RequiredArgs, simpleTrans.Params); len(implicit) != 0 {
		t.Fatalf("simple_trans implicit required args = %#v, vars=%#v", implicit, simpleVars)
	}
	guiCtx := BuildRunCheckContextForRunner(simpleTrans, nil, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(simpleTrans, nil, RunnerBackendTUI)
	if !guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("GUI context should provide OPENAI_API_KEY: %#v", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("TUI context should not provide OPENAI_API_KEY implicitly: %#v", tuiCtx.ProvidedEnvVars)
	}

	babeldoc := loadReportRegressionSkill(t, root, "babeldoc-pdf-translate", map[string]string{
		"translate.js": "console.log(process.env.API_KEY)\n",
		"skill.yaml": `name: babeldoc-pdf-translate
description: Translate PDF academic papers.
command: node {baseDir}/translate.js {content}
platforms: [universal]
env:
  API_KEY: sk-test
  BASE_URL: https://example.test/v1
required_env:
  - API_KEY
  - BASE_URL
steps:
  - action: run
    params:
      command: node {baseDir}/translate.js {content}
      env:
        API_KEY: sk-test
        BASE_URL: https://example.test/v1
      required_env:
        - API_KEY
        - BASE_URL
`,
	})
	assertSingleExecutableCommand(t, babeldoc, "babeldoc-pdf-translate", "translate.js")
	babelCtx := BuildRunCheckContextForRunner(babeldoc, nil, RunnerBackendTUI)
	if !babelCtx.ProvidedEnvVars["API_KEY"] || !babelCtx.ProvidedEnvVars["BASE_URL"] {
		t.Fatalf("babeldoc env map should satisfy required env, got %#v", babelCtx.ProvidedEnvVars)
	}

	xparse := &corelib.NLSkillEntry{
		Name: "xparse-parse",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "xparse-cli parse report.pdf"},
		}},
	}
	reqs := ExtractRequirements(xparse)
	if !hasRequirement(reqs, "command", "xparse-cli") {
		t.Fatalf("xparse requirements = %#v, want explicit command requirement for xparse-cli", reqs)
	}
}

func loadReportRegressionSkill(t *testing.T, root, name string, files map[string]string) *corelib.NLSkillEntry {
	t.Helper()
	skillDir := filepath.Join(root, name)
	for rel, content := range files {
		path := filepath.Join(skillDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}
	entry, _, err := loadSkillFromDir(skillDir, name)
	if err != nil {
		t.Fatalf("loadSkillFromDir(%s) error = %v", name, err)
	}
	return entry
}

func assertSingleExecutableCommand(t *testing.T, entry *corelib.NLSkillEntry, name, commandPart string) {
	t.Helper()
	if entry.Name != name {
		t.Fatalf("entry name = %q, want %q", entry.Name, name)
	}
	if len(entry.Steps) != 1 || NormalizeStepActionName(entry.Steps[0].Action) != "bash" {
		t.Fatalf("%s steps = %#v, want one bash step", name, entry.Steps)
	}
	cmd, _ := entry.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd, commandPart) {
		t.Fatalf("%s command = %q, want %q", name, cmd, commandPart)
	}
}

func hasRequirement(reqs []Requirement, typ, name string) bool {
	for _, req := range reqs {
		if req.Type == typ && req.Name == name {
			return true
		}
	}
	return false
}
