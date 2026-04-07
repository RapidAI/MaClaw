package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

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
			"SKILL.md": %q
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
			case strings.Contains(q, "filename:SKILL.md"):
				_, _ = w.Write([]byte(`{"items":[{"path":"browser/SKILL.md","repository":{"full_name":"octo/skills","html_url":"https://github.com/octo/skills","description":"Browser skill","stargazers_count":42,"default_branch":"main"}}]}`))
			case strings.Contains(q, "filename:skill.yaml"), strings.Contains(q, "filename:skill.yml"):
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
	if got.FilePath != "browser/SKILL.md" {
		t.Fatalf("FilePath = %q, want browser/SKILL.md", got.FilePath)
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
		if r.URL.Path != "/octo/skills/main/browser/SKILL.md" {
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
		"file_path":       "browser/SKILL.md",
		"raw_url":         "https://raw.githubusercontent.com/octo/skills/main/browser/SKILL.md",
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
	var found *NLSkillEntry
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

	entry := NLSkillEntry{
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
