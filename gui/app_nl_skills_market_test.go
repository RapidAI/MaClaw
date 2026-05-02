package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func createSkillZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", zipPath, err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			t.Fatalf("Create(%q) in zip error = %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			_ = zw.Close()
			t.Fatalf("Write(%q) in zip error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close zip writer error = %v", err)
	}
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	skillBody := fmt.Sprintf(`{
		"id": "hub-demo",
		"name": "demo-skill",
		"description": "from hub",
		"version": "1.0.0",
		"trust_level": "trusted",
		"triggers": ["demo"],
		"steps": [{"action": "noop", "params": {}, "on_error": "stop"}],
		"files": {
			"skill.yaml": %q,
			"skill.md": %q
		}
	}`,
		fmt.Sprintf("%q", base64.StdEncoding.EncodeToString([]byte("name: demo-skill\ndescription: from files\n"))),
		fmt.Sprintf("%q", base64.StdEncoding.EncodeToString([]byte("# Demo Skill\n"))),
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/hub-demo/download" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(skillBody))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = server.URL
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillHubClient = NewSkillHubClient(app)

	if err := app.InstallHubSkill("hub-demo", server.URL); err != nil {
		t.Fatalf("InstallHubSkill() error = %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var hubCount, fileCount int
	for _, s := range skills {
		if s.Name != "demo-skill" {
			continue
		}
		switch s.Source {
		case "hub":
			hubCount++
		case "file":
			fileCount++
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub entry count = %d, want 1; skills = %#v", hubCount, skills)
	}
	if fileCount != 0 {
		t.Fatalf("file entry count = %d, want 0; skills = %#v", fileCount, skills)
	}
}

func TestInstallHubSkillWrapsFileBackedSkillAsCraftTool(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	skillMD := "# xh-md-to-pdf\n\nUse pandoc or available local PDF tooling to convert Markdown into PDF."
	skillBody := fmt.Sprintf(`{
		"id": "hub-file-only",
		"name": "xh-md-to-pdf",
		"description": "convert markdown to pdf",
		"version": "1.0.0",
		"trust_level": "trusted",
		"triggers": ["pdf", "markdown"],
		"steps": [],
		"files": {
			"skill.md": %q
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte(skillMD)),
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/hub-file-only/download" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(skillBody))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = server.URL
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillHubClient = NewSkillHubClient(app)

	if err := app.InstallHubSkill("hub-file-only", server.URL); err != nil {
		t.Fatalf("InstallHubSkill() error = %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "xh-md-to-pdf" && skills[i].Source == "hub" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected installed hub skill, skills = %#v", skills)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", found.Steps)
	}
	if got := found.Steps[0].Params["instructions"]; got != skillMD {
		t.Fatalf("unexpected instructions: %#v", got)
	}
	expectedDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "xh-md-to-pdf")
	if got := found.Steps[0].Params["working_dir"]; got != expectedDir {
		t.Fatalf("working_dir = %#v, want %q", got, expectedDir)
	}
	if got := found.Steps[0].Params["verification_mode"]; got != "artifact_required" {
		t.Fatalf("verification_mode = %#v, want %q", got, "artifact_required")
	}
	if got := found.Steps[0].Params["register_policy"]; got != "manual" {
		t.Fatalf("register_policy = %#v, want %q", got, "manual")
	}
}

func TestSearchMixedSkillsIncludesGitHubSkillMDResult(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/skillmarket/search"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[]}`))
		case r.URL.Path == "/api/v1/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[]}`))
		case r.URL.Path == "/search/code":
			q := r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(q, "filename:skill.md"):
				_, _ = w.Write([]byte(`{"items":[{"path":"browser/skill.md","repository":{"full_name":"octo/skills","html_url":"https://github.com/octo/skills","description":"Browser skill","stargazers_count":42,"default_branch":"main"}}]}`))
			case strings.Contains(q, "filename:skill.yaml"), strings.Contains(q, "filename:skill.yaml"):
				_, _ = w.Write([]byte(`{"items":[]}`))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = server.URL
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	originalTransport := http.DefaultTransport
	http.DefaultTransport = rewriteGitHubTransport(t, server, originalTransport)
	defer func() { http.DefaultTransport = originalTransport }()

	results, err := app.SearchMixedSkills("browser")
	if err != nil {
		t.Fatalf("SearchMixedSkills() error = %v", err)
	}
	var got *MixedSkillSearchResult
	for i := range results {
		if results[i].Source == "github" {
			got = &results[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected github result in mixed search results: %#v", results)
	}
	if got.Source != "github" {
		t.Fatalf("Source = %q, want github", got.Source)
	}
	if got.FilePath != "browser/skill.md" {
		t.Fatalf("FilePath = %q, want browser/skill.md", got.FilePath)
	}
	if !strings.Contains(got.InstallRef, `"definition_type":"skill_md"`) {
		t.Fatalf("InstallRef missing skill_md definition: %s", got.InstallRef)
	}
}

func TestInstallMixedSkillRegistersGitHubSkillMD(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/octo/skills/main/browser/skill.md" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Browser Skill\n\nAutomate browser tasks."))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	originalTransport := http.DefaultTransport
	http.DefaultTransport = rewriteGitHubTransport(t, server, originalTransport)
	defer func() { http.DefaultTransport = originalTransport }()

	installRef, err := json.Marshal(map[string]any{
		"repo_full_name":  "octo/skills",
		"repo_url":        "https://github.com/octo/skills",
		"description":     "Browser skill",
		"file_path":       "browser/skill.md",
		"raw_url":         "https://raw.githubusercontent.com/octo/skills/main/browser/skill.md",
		"branch":          "main",
		"definition_type": "skill_md",
	})
	if err != nil {
		t.Fatalf("Marshal(installRef) error = %v", err)
	}

	if err := app.InstallMixedSkill("github", "octo/skills", string(installRef)); err != nil {
		t.Fatalf("InstallMixedSkill() error = %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Source == "github" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected github skill to be registered, skills = %#v", skills)
	}
	if found.Name != "Browser Skill" {
		t.Fatalf("Name = %q, want Browser Skill", found.Name)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", found.Steps)
	}
	if got := found.Steps[0].Params["instructions"]; got != "# Browser Skill\n\nAutomate browser tasks." {
		t.Fatalf("unexpected instructions: %#v", got)
	}
	if got := found.Steps[0].Params["verification_mode"]; got != "artifact_required" {
		t.Fatalf("verification_mode = %#v, want %q", got, "artifact_required")
	}
	if got := found.Steps[0].Params["register_policy"]; got != "manual" {
		t.Fatalf("register_policy = %#v, want %q", got, "manual")
	}
	defs := app.skillExecutor.List()
	var def *NLSkillDefinition
	for i := range defs {
		if defs[i].Name == "Browser Skill" {
			def = &defs[i]
			break
		}
	}
	if def == nil {
		t.Fatalf("expected Browser Skill definition, defs = %#v", defs)
	}
	if def.ExecutionClass != "agent_markdown_skill" {
		t.Fatalf("ExecutionClass = %q, want agent_markdown_skill", def.ExecutionClass)
	}
}

func rewriteGitHubTransport(t *testing.T, server *httptest.Server, fallback http.RoundTripper) http.RoundTripper {
	t.Helper()
	base := server.URL
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" || req.URL.Host == "raw.githubusercontent.com" {
			target := base + req.URL.RequestURI()
			proxyReq, err := http.NewRequest(req.Method, target, nil)
			if err != nil {
				return nil, err
			}
			proxyReq.Header = req.Header.Clone()
			return fallback.RoundTrip(proxyReq)
		}
		return fallback.RoundTrip(req)
	})
}

func TestSkillExecutorRegisterAllowsHubSkillWhenPrimaryExtractedFilesExist(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)

	primaryRoot := filepath.Join(tempHome, ".maclaw", "data", "skills")
	skillDir := filepath.Join(primaryRoot, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo-skill\ndescription: from files\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	entry := corelib.NLSkillEntry{
		Name:        "demo-skill",
		Description: "from hub",
		Source:      "hub",
		HubSkillID:  "hub-demo",
	}
	if err := executor.Register(entry); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	skills := executor.loadSkills()
	var hubCount, fileCount int
	for _, s := range skills {
		if s.Name != "demo-skill" {
			continue
		}
		switch s.Source {
		case "hub":
			hubCount++
		case "file":
			fileCount++
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub entry count = %d, want 1; skills = %#v", hubCount, skills)
	}
	if fileCount != 0 {
		t.Fatalf("file entry count = %d, want 0; skills = %#v", fileCount, skills)
	}
}

func TestSkillExecutorLoadSkillsHydratesEmptyHubSkillFromFileSkill(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "xh-md-to-pdf",
		Description: "stale hub copy",
		Source:      "hub",
		HubSkillID:  "hub-demo",
		Status:      "active",
		Steps:       []corelib.NLSkillStep{},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "xh-md-to-pdf")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: xh-md-to-pdf\ndescription: from files\nsteps:\n  - action: craft_tool\n    params:\n      task: convert markdown to pdf\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	executor := NewSkillExecutor(app, nil, nil)
	skills := executor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "xh-md-to-pdf" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected hydrated skill, skills = %#v", skills)
	}
	if found.Source != "hub" {
		t.Fatalf("Source = %q, want hub", found.Source)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected hydrated steps: %+v", found.Steps)
	}
	if filepath.Clean(found.SkillDir) != filepath.Clean(skillDir) {
		t.Fatalf("SkillDir = %q, want %q", found.SkillDir, skillDir)
	}
}

func TestSkillExecutorLoadSkillsPrefersPrimaryFileSkillOverHubSnapshot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "xh-md-to-pdf",
		Description: "hub snapshot",
		Source:      "hub",
		HubSkillID:  "hub-demo",
		Status:      "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo stale"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "xh-md-to-pdf")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# xh-md-to-pdf\n\nnode \"{baseDir}/scripts/xh-md-to-pdf.mjs\" \"/tmp/in.md\" \"/tmp/out.pdf\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "xh-md-to-pdf.mjs"), []byte("console.log('ok')"), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	executor := NewSkillExecutor(app, nil, nil)
	skills := executor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "xh-md-to-pdf" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected hydrated skill, skills = %#v", skills)
	}
	if len(found.Steps) != 1 {
		t.Fatalf("steps len = %d, want 1", len(found.Steps))
	}
	if got := found.Steps[0].Params["command"]; got == "echo stale" {
		t.Fatalf("expected primary file skill to override stale hub snapshot")
	}
}

func TestImportNLSkillZipPathImportsStandardOpenClawYamlPackage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "demo-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"demo-skill/skill.yaml": "name: demo-skill\ndescription: imported from zip\nsteps:\n  - action: bash\n    params:\n      command: echo imported\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "demo-skill" {
		t.Fatalf("name = %q, want demo-skill", name)
	}

	skillDir := filepath.Join(tempHome, ".maclaw", "data", "skills", "demo-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "skill.yaml")); err != nil {
		t.Fatalf("expected imported skill.yaml on disk: %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "demo-skill" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected imported skill, skills = %#v", skills)
	}
	if found.Source != "file" {
		t.Fatalf("Source = %q, want file", found.Source)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "bash" {
		t.Fatalf("unexpected steps: %+v", found.Steps)
	}
}

func TestImportNLSkillZipPathPreservesStructuredMetadataWithMarkdownSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "md-meta.zip")
	createSkillZip(t, zipPath, map[string]string{
		"md-meta/skill.yaml": "name: md-meta\ndescription: metadata plus markdown steps\nmode: api_workflow\nproduces_artifact: false\nrequired_env:\n  - API_KEY\nrequires:\n  python:\n    - requests\nparams:\n  - name: input\n    required: true\n",
		"md-meta/skill.md":   "# md-meta\n\necho from markdown\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "md-meta" {
		t.Fatalf("name = %q, want md-meta", name)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "md-meta" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected imported skill, skills = %#v", skills)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action == "" {
		t.Fatalf("unexpected markdown-derived steps: %+v", found.Steps)
	}
	if found.Mode != "api_workflow" || found.ProducesArtifact {
		t.Fatalf("structured metadata not preserved: mode=%q produces=%v", found.Mode, found.ProducesArtifact)
	}
	if len(found.RequiredEnv) != 1 || found.RequiredEnv[0] != "API_KEY" || len(found.RequiresPython) != 1 || found.RequiresPython[0] != "requests" {
		t.Fatalf("runtime metadata not preserved: env=%+v python=%+v", found.RequiredEnv, found.RequiresPython)
	}
	if len(found.Params) != 1 || found.Params[0].Name != "input" || !found.Params[0].Required {
		t.Fatalf("params metadata not preserved: %+v", found.Params)
	}
}

func TestImportNLSkillZipPathPreservesMarkdownFrontmatterWhenStructuredMetadataIsSparse(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "md-frontmatter.zip")
	createSkillZip(t, zipPath, map[string]string{
		"md-frontmatter/skill.yaml": "name: md-frontmatter\ndescription: sparse metadata\n",
		"md-frontmatter/skill.md":   "---\nmode: api_workflow\nproduces_artifact: false\nrequires_gui: true\nparams:\n  - name: input\n    required: true\n---\n\n# md-frontmatter\n\necho from markdown\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "md-frontmatter" {
		t.Fatalf("name = %q, want md-frontmatter", name)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "md-frontmatter" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected imported skill, skills = %#v", skills)
	}
	if found.Mode != "api_workflow" || found.ProducesArtifact {
		t.Fatalf("markdown frontmatter was overwritten: mode=%q produces=%v", found.Mode, found.ProducesArtifact)
	}
	if !found.RequiresGUI {
		t.Fatalf("RequiresGUI = false, want markdown frontmatter true")
	}
	if len(found.Params) != 1 || found.Params[0].Name != "input" || !found.Params[0].Required {
		t.Fatalf("Params = %+v, want markdown frontmatter param", found.Params)
	}
}

func TestImportNLSkillZipPathImportsMultipleStandardSkillDirs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "multi-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"demo-one/skill.yaml": "name: demo-one\ndescription: first\nsteps:\n  - action: bash\n    params:\n      command: echo one\n",
		"demo-two/skill.md":   "# demo-two\n\necho two\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "demo-one" {
		t.Fatalf("name = %q, want demo-one", name)
	}

	for _, dir := range []string{"demo-one", "demo-two"} {
		if _, err := os.Stat(filepath.Join(tempHome, ".maclaw", "data", "skills", dir)); err != nil {
			t.Fatalf("expected imported dir %q: %v", dir, err)
		}
	}

	skills := app.skillExecutor.loadSkills()
	seen := map[string]bool{}
	for _, s := range skills {
		if s.Name == "demo-one" || s.Name == "demo-two" {
			seen[s.Name] = true
		}
	}
	if !seen["demo-one"] || !seen["demo-two"] {
		t.Fatalf("expected both imported skills, got %#v", skills)
	}
}

func TestImportNLSkillZipPathRejectsInvalidZipWithoutKnownSkillFormat(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "invalid-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"README.txt": "not a skill",
	})

	_, err := app.importNLSkillZipPath(zipPath)
	if err == nil {
		t.Fatalf("expected importNLSkillZipPath() error")
	}
	if !strings.Contains(err.Error(), "skill.md") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "skill.md") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSkillZipAcceptsUppercaseSkillMarkdownPackage(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "uppercase-md.zip")
	createSkillZip(t, zipPath, map[string]string{
		"upper-md/SKILL.md": "# upper-md\n\n```bash\necho uppercase\n```\n",
	})

	app := &App{}
	if err := app.validateSkillZip(zipPath); err != nil {
		t.Fatalf("validateSkillZip() error = %v", err)
	}
}

func TestValidateSkillZipRejectsLegacyUppercaseSkillMarkdownPackage(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "legacy-md.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy/SKILL.md":   "# legacy\n",
		"legacy/_meta.json": `{"description":"legacy"}`,
	})

	app := &App{}
	err := app.validateSkillZip(zipPath)
	if err == nil {
		t.Fatalf("expected validateSkillZip() error")
	}
	if !strings.Contains(err.Error(), "SKILL.md/_meta.json") {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestImportNLSkillZipPathRejectsLegacySkillPackage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "legacy-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"SKILL.md":   "# Legacy Skill\n\nUse it.\n",
		"_meta.json": `{"description":"legacy"}`,
	})

	_, err := app.importNLSkillZipPath(zipPath)
	if err == nil {
		t.Fatalf("expected importNLSkillZipPath() error")
	}
	if !strings.Contains(err.Error(), "SKILL.md/_meta.json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportNLSkillZipPathImportsUppercaseSkillMarkdownPackage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "uppercase-md.zip")
	createSkillZip(t, zipPath, map[string]string{
		"upper-md/SKILL.md": "# upper-md\n\n```bash\necho uppercase\n```\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "upper-md" {
		t.Fatalf("name = %q, want upper-md", name)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "upper-md" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected imported SKILL.md skill, skills = %#v", skills)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "bash" {
		t.Fatalf("unexpected steps: %+v", found.Steps)
	}

	tools := app.skillExecutor.AsRegisteredTools()
	var body string
	for _, rt := range tools {
		if rt.Name == "upper-md" {
			body = rt.Body
			break
		}
	}
	if !strings.Contains(body, "echo uppercase") {
		t.Fatalf("registered tool body did not include SKILL.md content: %q", body)
	}
}

func TestImportNLSkillZipPathImportsSkillMarkdownPackage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "xh-md-to-pdf.zip")
	createSkillZip(t, zipPath, map[string]string{
		"xh-md-to-pdf/skill.md":                 "# xh-md-to-pdf\n\nnode \"{baseDir}/scripts/xh-md-to-pdf.mjs\" \"/path/in.md\" \"/path/out.pdf\"\n",
		"xh-md-to-pdf/scripts/xh-md-to-pdf.mjs": "console.log('ok')\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "xh-md-to-pdf" {
		t.Fatalf("name = %q, want xh-md-to-pdf", name)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "xh-md-to-pdf" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected imported markdown skill, skills = %#v", skills)
	}
	if found.Source != "file" {
		t.Fatalf("Source = %q, want file", found.Source)
	}
	if len(found.Steps) != 1 || found.Steps[0].Action != "bash" {
		t.Fatalf("unexpected steps: %+v", found.Steps)
	}
	if got := found.Steps[0].Params["working_dir"]; got != filepath.Join(tempHome, ".maclaw", "data", "skills", "xh-md-to-pdf") {
		t.Fatalf("working_dir = %#v", got)
	}
}

func TestSkillExecutorDeleteRemovesExternalSkillDirs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	externalRoot := filepath.Join(tempHome, "external-skills")
	cfg.ExternalSkillDirs = []string{externalRoot}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	executor := NewSkillExecutor(app, nil, nil)

	skillDir := filepath.Join(externalRoot, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo-skill\ndescription: external\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	if err := executor.Delete("demo-skill"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skillDir still exists after Delete(): err = %v", err)
	}
}

func TestInstallMixedSkillSkillMarketFailsOver(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/failover-skill/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "failover-skill",
				"name":        "Failover Skill",
				"description": "downloaded via backup hubcenter",
				"version":     "1.0.0",
				"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "hello"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.InstallMixedSkill("skillmarket", "failover-skill", ""); err != nil {
		t.Fatalf("InstallMixedSkill() error = %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "Failover Skill" {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected failover skill to be registered, skills = %#v", skills)
	}
	if found.HubSkillID != "failover-skill" {
		t.Fatalf("HubSkillID = %q, want failover-skill", found.HubSkillID)
	}
}

func TestImportNLSkillZipPathRejectsJSONSkillPackage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath := filepath.Join(t.TempDir(), "json-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"json-skill/skill.json": `{"name":"json-skill","steps":[{"run":"echo imported"}]}`,
	})

	if _, err := app.importNLSkillZipPath(zipPath); err == nil {
		t.Fatal("importNLSkillZipPath should reject retired skill.json packages")
	}
}
