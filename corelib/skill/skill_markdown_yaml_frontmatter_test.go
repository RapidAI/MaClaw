package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// --- ParseMarkdownFrontmatterYAML ---

func TestParseMarkdownFrontmatterYAML_ListValues(t *testing.T) {
	content := "---\nname: weather\ntriggers:\n  - 天气\n  - weather\n  - 查天气\nplatforms:\n  - windows\n  - linux\n---\n\n# Weather"
	fm, body := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	triggers := yamlStringList(fm["triggers"])
	if len(triggers) != 3 || triggers[0] != "天气" || triggers[1] != "weather" || triggers[2] != "查天气" {
		t.Fatalf("triggers = %v, want [天气 weather 查天气]", triggers)
	}
	platforms := yamlStringList(fm["platforms"])
	if len(platforms) != 2 || platforms[0] != "windows" || platforms[1] != "linux" {
		t.Fatalf("platforms = %v, want [windows linux]", platforms)
	}
	if body != "# Weather" {
		t.Fatalf("body = %q, want %q", body, "# Weather")
	}
}

func TestParseMarkdownFrontmatterYAML_BoolValues(t *testing.T) {
	content := "---\nname: gui-skill\nrequires_gui: true\nproduces_artifact: false\n---\n\n# GUI"
	fm, _ := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	gui := yamlBool(fm["requires_gui"])
	if gui == nil || !*gui {
		t.Fatalf("requires_gui = %v, want true", gui)
	}
	artifact := yamlBool(fm["produces_artifact"])
	if artifact == nil || *artifact {
		t.Fatalf("produces_artifact = %v, want false", artifact)
	}
}

func TestParseMarkdownFrontmatterYAML_NestedRequires(t *testing.T) {
	content := "---\nname: py-skill\nrequires:\n  python:\n    - requests\n    - beautifulsoup4\n  node:\n    - puppeteer\n---\n\n# Py"
	fm, _ := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	reqMap, ok := fm["requires"].(map[string]interface{})
	if !ok {
		t.Fatalf("requires is not a map: %T", fm["requires"])
	}
	python := yamlStringList(reqMap["python"])
	if len(python) != 2 || python[0] != "requests" || python[1] != "beautifulsoup4" {
		t.Fatalf("requires.python = %v, want [requests beautifulsoup4]", python)
	}
	node := yamlStringList(reqMap["node"])
	if len(node) != 1 || node[0] != "puppeteer" {
		t.Fatalf("requires.node = %v, want [puppeteer]", node)
	}
}

func TestParseMarkdownFrontmatterYAML_InlineListSyntax(t *testing.T) {
	content := "---\nname: test\ntriggers: [天气, weather]\nplatforms: [windows, macos]\n---\n\n# Test"
	fm, _ := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	triggers := yamlStringList(fm["triggers"])
	if len(triggers) != 2 || triggers[0] != "天气" || triggers[1] != "weather" {
		t.Fatalf("triggers = %v, want [天气 weather]", triggers)
	}
}

func TestParseMarkdownFrontmatterYAML_BackwardCompatSimple(t *testing.T) {
	// Simple key: value frontmatter should still work.
	content := "---\nname: simple-skill\ndescription: A simple skill\nshell: bash\n---\n\n# Simple"
	fm, body := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	if yamlString(fm["name"]) != "simple-skill" {
		t.Fatalf("name = %v, want simple-skill", fm["name"])
	}
	if yamlString(fm["description"]) != "A simple skill" {
		t.Fatalf("description = %v", fm["description"])
	}
	if yamlString(fm["shell"]) != "bash" {
		t.Fatalf("shell = %v", fm["shell"])
	}
	if body != "# Simple" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseMarkdownFrontmatterYAML_NoFrontmatter(t *testing.T) {
	content := "# No Frontmatter\n\nJust markdown."
	fm, body := ParseMarkdownFrontmatterYAML(content)
	if fm != nil {
		t.Fatalf("expected nil fm, got %v", fm)
	}
	if body != content {
		t.Fatalf("body = %q, want original content", body)
	}
}

// --- parseSkillMarkdownDocument extended fields ---

func TestParseSkillMarkdownDocument_YAMLTriggers(t *testing.T) {
	content := "---\nname: weather\ndescription: Query weather\ntriggers:\n  - 天气\n  - weather\nplatforms:\n  - windows\n  - linux\nrequires_gui: false\nmode: api_workflow\nproduces_artifact: false\nrequires:\n  python:\n    - requests\n---\n\n# Weather\n\nQuery weather."
	parsed, err := parseSkillMarkdownDocument(content, "", "")
	if err != nil {
		t.Fatalf("parseSkillMarkdownDocument() error = %v", err)
	}
	if len(parsed.triggers) != 2 || parsed.triggers[0] != "天气" {
		t.Fatalf("triggers = %v, want [天气 weather]", parsed.triggers)
	}
	if len(parsed.platforms) != 2 || parsed.platforms[0] != "windows" {
		t.Fatalf("platforms = %v, want [windows linux]", parsed.platforms)
	}
	if parsed.requiresGUI == nil || *parsed.requiresGUI {
		t.Fatalf("requiresGUI = %v, want false", parsed.requiresGUI)
	}
	if parsed.mode != "api_workflow" {
		t.Fatalf("mode = %q, want api_workflow", parsed.mode)
	}
	if parsed.producesArtifact == nil || *parsed.producesArtifact {
		t.Fatalf("producesArtifact = %v, want false", parsed.producesArtifact)
	}
	if len(parsed.requiresPython) != 1 || parsed.requiresPython[0] != "requests" {
		t.Fatalf("requiresPython = %v, want [requests]", parsed.requiresPython)
	}
}

func TestParseSkillMarkdownDocument_YAMLRequiredArgsAsList(t *testing.T) {
	// required_args as YAML list instead of CSV string
	content := "---\nname: test\nrequired_args:\n  - input\n  - output\nrequires_env:\n  - API_KEY\n  - SECRET\n---\n\n# Test"
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
}

// --- ImportMarkdownSkillDir with YAML frontmatter ---

func TestImportMarkdownSkillDir_YAMLFrontmatterTriggersAndPlatforms(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "weather-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: weather-query\ndescription: Query weather\ntriggers:\n  - 天气\n  - weather\n  - 查天气\nplatforms:\n  - windows\n  - macos\nrequires_env: WEATHER_API_KEY\nshell: bash\nrequires:\n  python:\n    - requests\n---\n\n# Weather Query\n\n```bash\necho query weather\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if entry.Name != "weather-query" {
		t.Fatalf("Name = %q, want weather-query", entry.Name)
	}
	// Triggers from YAML frontmatter
	if len(entry.Triggers) != 3 || entry.Triggers[0] != "天气" {
		t.Fatalf("Triggers = %v, want [天气 weather 查天气]", entry.Triggers)
	}
	// Platforms from YAML frontmatter
	if len(entry.Platforms) != 2 || entry.Platforms[0] != "windows" {
		t.Fatalf("Platforms = %v, want [windows macos]", entry.Platforms)
	}
	// RequiredEnv from simple frontmatter (backward compat)
	if len(entry.RequiredEnv) != 1 || entry.RequiredEnv[0] != "WEATHER_API_KEY" {
		t.Fatalf("RequiredEnv = %v, want [WEATHER_API_KEY]", entry.RequiredEnv)
	}
	// RequiresPython from nested requires
	if len(entry.RequiresPython) != 1 || entry.RequiresPython[0] != "requests" {
		t.Fatalf("RequiresPython = %v, want [requests]", entry.RequiresPython)
	}
	// Steps should be parsed from bash block
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "bash" {
		t.Fatalf("Steps = %+v, want 1 bash step", entry.Steps)
	}
}

func TestImportMarkdownSkillDir_OptsOverrideFrontmatterTriggers(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "override-test")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: test\ntriggers:\n  - from-frontmatter\nplatforms:\n  - linux\n---\n\n# Test\n\n```bash\necho test\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// MarkdownSkillOptions triggers/platforms should take precedence
	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{
		Source:    "file",
		SkillDir:  skillDir,
		Triggers:  []string{"from-opts"},
		Platforms: []string{"windows"},
	})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if len(entry.Triggers) != 1 || entry.Triggers[0] != "from-opts" {
		t.Fatalf("Triggers = %v, want [from-opts] (opts should override frontmatter)", entry.Triggers)
	}
	if len(entry.Platforms) != 1 || entry.Platforms[0] != "windows" {
		t.Fatalf("Platforms = %v, want [windows] (opts should override frontmatter)", entry.Platforms)
	}
}

func TestImportMarkdownSkillDir_YAMLFrontmatterMode(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "api-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: api-skill\nmode: api_workflow\nproduces_artifact: false\nrequires_gui: true\n---\n\n# API Skill\n\n```bash\necho api\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if entry.Mode != "api_workflow" {
		t.Fatalf("Mode = %q, want api_workflow", entry.Mode)
	}
	if entry.ProducesArtifact {
		t.Fatal("ProducesArtifact = true, want false")
	}
	if !entry.RequiresGUI {
		t.Fatal("RequiresGUI = false, want true")
	}
}

// --- buildCraftToolFallback with YAML frontmatter ---

func TestBuildCraftToolFallback_YAMLFrontmatter(t *testing.T) {
	content := "---\nname: craft-test\ndescription: A craft skill\ntriggers:\n  - craft\n  - test\nplatforms:\n  - windows\nrequires_env: MY_KEY\nrequires:\n  python:\n    - numpy\n---\n\n# Craft Test\n\nDo something."
	entry := buildCraftToolFallback("/tmp/craft-test", "craft-test", []byte(content))
	if entry == nil {
		t.Fatal("buildCraftToolFallback returned nil")
	}
	if len(entry.Triggers) != 2 || entry.Triggers[0] != "craft" {
		t.Fatalf("Triggers = %v, want [craft test]", entry.Triggers)
	}
	if len(entry.Platforms) != 1 || entry.Platforms[0] != "windows" {
		t.Fatalf("Platforms = %v, want [windows]", entry.Platforms)
	}
	if len(entry.RequiredEnv) != 1 || entry.RequiredEnv[0] != "MY_KEY" {
		t.Fatalf("RequiredEnv = %v, want [MY_KEY]", entry.RequiredEnv)
	}
	if len(entry.RequiresPython) != 1 || entry.RequiresPython[0] != "numpy" {
		t.Fatalf("RequiresPython = %v, want [numpy]", entry.RequiresPython)
	}
}

func TestBuildCraftToolFallback_NoTriggersDefaultsToName(t *testing.T) {
	content := "---\nname: simple\n---\n\n# Simple\n\nDo stuff."
	entry := buildCraftToolFallback("/tmp/simple", "simple", []byte(content))
	if entry == nil {
		t.Fatal("buildCraftToolFallback returned nil")
	}
	if len(entry.Triggers) != 1 || entry.Triggers[0] != "simple" {
		t.Fatalf("Triggers = %v, want [simple]", entry.Triggers)
	}
}

// --- Key alias normalization (mechanism test) ---

func TestParseMarkdownFrontmatterYAML_NormalizesRequiresEnvAlias(t *testing.T) {
	// SKILL.md uses historical "requires_env" key.
	// After parsing, it should appear as canonical "required_env".
	content := "---\nname: test\nrequires_env:\n  - API_KEY\n  - SECRET\n---\n\n# Test"
	fm, _ := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	// Canonical key should exist.
	if fm["required_env"] == nil {
		t.Fatal("expected canonical key 'required_env' to exist after alias normalization")
	}
	// Alias key should NOT exist — it was normalized away.
	if fm["requires_env"] != nil {
		t.Fatal("alias key 'requires_env' should not exist after normalization")
	}
	// Values should be correct.
	envList := yamlStringList(fm["required_env"])
	if len(envList) != 2 || envList[0] != "API_KEY" || envList[1] != "SECRET" {
		t.Fatalf("required_env = %v, want [API_KEY SECRET]", envList)
	}
}

func TestParseMarkdownFrontmatter_NormalizesRequiresEnvAlias(t *testing.T) {
	// Simple parser should also normalize the alias.
	content := "---\nname: test\nrequires_env: MY_TOKEN\n---\n\n# Test"
	fm, _ := ParseMarkdownFrontmatter(content)
	if fm["required_env"] != "MY_TOKEN" {
		t.Fatalf("expected canonical key 'required_env' = MY_TOKEN, got %q", fm["required_env"])
	}
	if _, exists := fm["requires_env"]; exists {
		t.Fatal("alias key 'requires_env' should not exist after normalization")
	}
}

func TestParseMarkdownFrontmatterYAML_CanonicalKeyNotOverriddenByAlias(t *testing.T) {
	// If both canonical and alias keys exist, canonical wins.
	content := "---\nname: test\nrequired_env:\n  - CANONICAL\nrequires_env:\n  - ALIAS\n---\n\n# Test"
	fm, _ := ParseMarkdownFrontmatterYAML(content)
	if fm == nil {
		t.Fatal("ParseMarkdownFrontmatterYAML returned nil")
	}
	envList := yamlStringList(fm["required_env"])
	if len(envList) != 1 || envList[0] != "CANONICAL" {
		t.Fatalf("required_env = %v, want [CANONICAL] (canonical should win over alias)", envList)
	}
}

func TestExtractSkillMetadata_UsesCanonicalKeys(t *testing.T) {
	// extractSkillMetadata only needs to check canonical key names.
	// Alias normalization already happened in ParseMarkdownFrontmatterYAML.
	yamlFM := map[string]interface{}{
		"required_env":      []interface{}{"API_KEY"},
		"required_args":     []interface{}{"input", "output"},
		"shell":             "bash",
		"mode":              "api_workflow",
		"triggers":          []interface{}{"天气", "weather"},
		"platforms":         []interface{}{"windows"},
		"requires_gui":      true,
		"produces_artifact": false,
		"requires": map[string]interface{}{
			"python": []interface{}{"requests"},
			"node":   []interface{}{"puppeteer"},
		},
	}
	meta := extractSkillMetadata(yamlFM)
	if len(meta.requiredEnv) != 1 || meta.requiredEnv[0] != "API_KEY" {
		t.Fatalf("requiredEnv = %v, want [API_KEY]", meta.requiredEnv)
	}
	if len(meta.requiredArgs) != 2 {
		t.Fatalf("requiredArgs = %v, want [input output]", meta.requiredArgs)
	}
	if meta.preferredShell != "bash" {
		t.Fatalf("preferredShell = %q", meta.preferredShell)
	}
	if meta.mode != "api_workflow" {
		t.Fatalf("mode = %q", meta.mode)
	}
	if len(meta.triggers) != 2 {
		t.Fatalf("triggers = %v", meta.triggers)
	}
	if len(meta.platforms) != 1 {
		t.Fatalf("platforms = %v", meta.platforms)
	}
	if meta.requiresGUI == nil || !*meta.requiresGUI {
		t.Fatalf("requiresGUI = %v", meta.requiresGUI)
	}
	if meta.producesArtifact == nil || *meta.producesArtifact {
		t.Fatalf("producesArtifact = %v", meta.producesArtifact)
	}
	if len(meta.requiresPython) != 1 || meta.requiresPython[0] != "requests" {
		t.Fatalf("requiresPython = %v", meta.requiresPython)
	}
	if len(meta.requiresNode) != 1 || meta.requiresNode[0] != "puppeteer" {
		t.Fatalf("requiresNode = %v", meta.requiresNode)
	}
}

func TestExtractSkillMetadata_NilMap(t *testing.T) {
	meta := extractSkillMetadata(nil)
	if len(meta.requiredEnv) != 0 || len(meta.triggers) != 0 || meta.mode != "" {
		t.Fatalf("expected zero-value metadata from nil map, got %+v", meta)
	}
}

func TestParseMarkdownSkill_YAMLFrontmatterSkillLevelCompatibility(t *testing.T) {
	content := `---
name: fm-compat
requires_gui: "true"
global_timeout: "180"
pip: requests
npm: playwright
params:
  input:
    desc: Input file
    required: yes
operations:
  generate:
    steps: create, verify
pipeline:
  - extract
---

# FM Compat

Use this skill.
`

	entry, err := ParseMarkdownSkill(content, MarkdownSkillOptions{})
	if err != nil {
		t.Fatalf("ParseMarkdownSkill error: %v", err)
	}
	if entry.Name != "fm-compat" || !entry.RequiresGUI || entry.GlobalTimeout != 180 {
		t.Fatalf("frontmatter scalar metadata not normalized: name=%q gui=%v timeout=%d", entry.Name, entry.RequiresGUI, entry.GlobalTimeout)
	}
	if len(entry.RequiresPython) != 1 || entry.RequiresPython[0] != "requests" || len(entry.RequiresNode) != 1 || entry.RequiresNode[0] != "playwright" {
		t.Fatalf("frontmatter requires aliases not preserved: python=%#v node=%#v", entry.RequiresPython, entry.RequiresNode)
	}
	if len(entry.Params) != 1 || entry.Params[0].Name != "input" || !entry.Params[0].Required || entry.Params[0].Description != "Input file" {
		t.Fatalf("frontmatter params not normalized: %#v", entry.Params)
	}
	if entry.Mode != "api_workflow" || len(entry.Operations) != 1 || entry.Operations[0].Name != "generate" || len(entry.Operations[0].Labels) != 2 {
		t.Fatalf("frontmatter operations not normalized: mode=%q ops=%#v", entry.Mode, entry.Operations)
	}
	if len(entry.Pipeline) != 1 || entry.Pipeline[0].Skill != "extract" {
		t.Fatalf("frontmatter pipeline not preserved: %#v", entry.Pipeline)
	}
}

func TestImportMarkdownSkillDir_YAMLFrontmatterSkillLevelCompatibility(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "fm-dir")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	content := "---\nname: fm-dir\nstateful: \"true\"\nrequires_tools: shell, browser\npipeline:\n  - load:\n      target: warehouse\n---\n\n# FM Dir\n\n```bash\necho ok\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir error: %v", err)
	}
	if !entry.Stateful || len(entry.RequiresTools) != 2 || entry.RequiresTools[0] != "shell" {
		t.Fatalf("frontmatter stateful/requires_tools not normalized: stateful=%v tools=%#v", entry.Stateful, entry.RequiresTools)
	}
	if entry.Mode != "pipeline" || len(entry.Pipeline) != 1 || entry.Pipeline[0].Skill != "load" || entry.Pipeline[0].Params["target"] != "warehouse" {
		t.Fatalf("frontmatter pipeline not normalized: mode=%q pipeline=%#v", entry.Mode, entry.Pipeline)
	}
}
