package skill

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func newHTTPResponse(status int, body []byte, headers map[string]string) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestParseSkillMarkdownCreatesCraftToolSkill(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{
		RepoFullName:   "octo/skills",
		RepoURL:        "https://github.com/octo/skills",
		FilePath:       "browser/skill.md",
		DefinitionType: githubDefinitionSkillMD,
		Description:    "Browser automation skill",
	}
	skill, err := gs.parseSkillMarkdown([]byte("# Browser Skill\n\nAutomate browser steps."), candidate)
	if err != nil {
		t.Fatalf("parseSkillMarkdown error: %v", err)
	}
	if skill.Name != "Browser Skill" {
		t.Fatalf("unexpected name: %q", skill.Name)
	}
	if skill.Source != "github" {
		t.Fatalf("unexpected source: %q", skill.Source)
	}
	if len(skill.Steps) != 1 || skill.Steps[0].Action != "craft_tool" {
		t.Fatalf("expected single craft_tool step, got %+v", skill.Steps)
	}
	if got := skill.Steps[0].Params["instructions"]; got != "# Browser Skill\n\nAutomate browser steps." {
		t.Fatalf("unexpected instructions: %#v", got)
	}
	if got := skill.Steps[0].Params["verification_mode"]; got != "artifact_optional" {
		t.Fatalf("verification_mode = %#v, want %q", got, "artifact_optional")
	}
	if got := skill.Steps[0].Params["register_policy"]; got != "manual" {
		t.Fatalf("register_policy = %#v, want %q", got, "manual")
	}
}

func TestParseCandidateDataUsesYAMLParser(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{
		RepoFullName:   "octo/skills",
		RepoURL:        "https://github.com/octo/skills",
		FilePath:       "browser/skill.yaml",
		DefinitionType: githubDefinitionYAML,
	}
	skill, err := gs.parseCandidateData([]byte("name: browser\ndescription: Browser automation\nsteps:\n  - action: craft_tool\n    params:\n      instructions: test\n"), candidate)
	if err != nil {
		t.Fatalf("parseCandidateData error: %v", err)
	}
	if skill.Name != "browser" {
		t.Fatalf("unexpected name: %q", skill.Name)
	}
	if len(skill.Steps) != 1 || skill.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skill.Steps)
	}
}

func TestGitHubYAMLSkillDefaultsToProducingArtifact(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills"}
	sf := &SkillYAMLFile{Name: "report", Steps: []SkillYAMLStep{{Action: "run", Params: map[string]interface{}{"command": "echo report"}}}}

	entry, err := gs.skillEntryFromDefinition(sf, candidate)
	if err != nil {
		t.Fatalf("skillEntryFromDefinition() error = %v", err)
	}
	if !entry.ProducesArtifact {
		t.Fatalf("ProducesArtifact = false, want true for structured YAML skills")
	}
}

func TestDefinitionTypeForPath(t *testing.T) {
	cases := map[string]string{
		"foo/skill.md":   githubDefinitionSkillMD,
		"foo/skill.yaml": githubDefinitionYAML,
		"foo/skill.yml":  githubDefinitionYAML,
		"foo/skill.json": "",
		"foo/README.md":  "",
	}
	for input, want := range cases {
		if got := definitionTypeForPath(input, ""); got != want {
			t.Fatalf("definitionTypeForPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSearchGitHubReturnsCombinedCandidates(t *testing.T) {
	gs := NewGitHubSearcher("test-token") // token enables Code Search fallback
	calls := map[string]int{}
	responses := map[string][]byte{
		"skill.md": mustJSON(t, ghCodeSearchResponse{Items: []ghCodeSearchItem{{
			Path:       "browser/skill.md",
			Repository: ghSearchRepo{FullName: "octo/skills", HTMLURL: "https://github.com/octo/skills", Description: "Browser skill", Stars: 12, DefaultBranch: "main"},
		}}}),
		"skill.yaml": mustJSON(t, ghCodeSearchResponse{Items: []ghCodeSearchItem{{
			Path:       "browser/skill.yaml",
			Repository: ghSearchRepo{FullName: "octo/skills", HTMLURL: "https://github.com/octo/skills", Description: "Browser skill", Stars: 12, DefaultBranch: "main"},
		}}}),
		"skill.yml": mustJSON(t, ghCodeSearchResponse{Items: []ghCodeSearchItem{{
			Path:       "browser/skill.yml",
			Repository: ghSearchRepo{FullName: "octo/yml-skills", HTMLURL: "https://github.com/octo/yml-skills", Description: "YML browser skill", Stars: 9, DefaultBranch: "main"},
		}}}),
	}
	repoResponse := mustJSON(t, ghRepoSearchResponse{Items: []ghSearchRepo{{
		FullName: "octo/repo-skill", HTMLURL: "https://github.com/octo/repo-skill",
		Description: "Repo skill", Stars: 5, DefaultBranch: "main",
	}}})
	gs.client = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query().Get("q")
		// Repository Search API
		if strings.Contains(req.URL.Path, "/search/repositories") {
			calls["repo"]++
			return newHTTPResponse(200, repoResponse, nil), nil
		}
		// Code Search API
		for filename, body := range responses {
			if strings.Contains(q, "filename:"+filename) {
				calls[filename]++
				return newHTTPResponse(200, body, nil), nil
			}
		}
		return newHTTPResponse(500, []byte("unexpected query"), nil), nil
	})
	results, err := gs.SearchGitHub("browser")
	if err != nil {
		t.Fatalf("SearchGitHub error: %v", err)
	}
	// Should have: 1 from repo search + 2 from code search (octo/skills has 2 files but same repo = 1 deduped)
	// octo/repo-skill (repo search) + octo/skills (code search, deduped to 1)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if calls["repo"] != 1 {
		t.Fatalf("expected 1 repo search call, got %d", calls["repo"])
	}
	if calls["skill.md"] != 1 || calls["skill.yaml"] != 1 || calls["skill.yml"] != 1 {
		t.Fatalf("unexpected code search calls: %+v", calls)
	}
	if calls["skill.json"] != 0 {
		t.Fatalf("skill.json should not be searched as a skill definition: %+v", calls)
	}
}

func TestImportFromRepoURLLoadsSkillMarkdownFromTree(t *testing.T) {
	gs := NewGitHubSearcher("")
	gs.client = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.github.com":
			if req.URL.Path != "/repos/octo/skills/git/trees/main" {
				return newHTTPResponse(404, []byte("not found"), nil), nil
			}
			body := mustJSON(t, ghTreeResponse{Tree: []ghTreeEntry{{Path: "browser/skill.md", Type: "blob"}}})
			return newHTTPResponse(200, body, nil), nil
		case "raw.githubusercontent.com":
			if req.URL.Path != "/octo/skills/main/browser/skill.md" {
				return newHTTPResponse(404, []byte("not found"), nil), nil
			}
			return newHTTPResponse(200, []byte("# Browser Skill\n\nAutomate browser tasks."), map[string]string{"Content-Type": "text/markdown"}), nil
		default:
			return newHTTPResponse(500, []byte("unexpected host"), nil), nil
		}
	})

	skills, err := gs.ImportFromRepoURL("https://github.com/octo/skills")
	if err != nil {
		t.Fatalf("ImportFromRepoURL error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "Browser Skill" {
		t.Fatalf("unexpected skill name: %q", skills[0].Name)
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
}

func TestImportFromRepoURLRespectsSubPathForSkillMarkdown(t *testing.T) {
	gs := NewGitHubSearcher("")
	gs.client = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.github.com":
			body := mustJSON(t, ghTreeResponse{Tree: []ghTreeEntry{
				{Path: "browser/skill.md", Type: "blob"},
				{Path: "other/skill.md", Type: "blob"},
			}})
			return newHTTPResponse(200, body, nil), nil
		case "raw.githubusercontent.com":
			if req.URL.Path != "/octo/skills/main/browser/skill.md" {
				return newHTTPResponse(500, []byte("unexpected raw path"), nil), nil
			}
			return newHTTPResponse(200, []byte("# Browser Skill\n\nScoped skill."), map[string]string{"Content-Type": "text/markdown"}), nil
		default:
			return newHTTPResponse(500, []byte("unexpected host"), nil), nil
		}
	})

	skills, err := gs.ImportFromRepoURL("https://github.com/octo/skills/tree/main/browser")
	if err != nil {
		t.Fatalf("ImportFromRepoURL error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 scoped skill, got %d", len(skills))
	}
	if skills[0].Name != "Browser Skill" {
		t.Fatalf("unexpected skill name: %q", skills[0].Name)
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}
	return data
}

func TestParseCandidateDataRejectsJSONSkillDefinition(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.json"}
	if _, err := gs.parseCandidateData([]byte(`{"name":"json-gh"}`), candidate); err == nil {
		t.Fatal("parseCandidateData should reject retired skill.json definitions")
	}
}

func TestParseSkillYAML_GitHubTopLevelStepParamsCompatibility(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{
		RepoFullName:   "octo/skills",
		RepoURL:        "https://github.com/octo/skills",
		FilePath:       "browser/skill.yaml",
		DefinitionType: githubDefinitionYAML,
	}
	data := []byte(`name: browser
requires_env:
  - API_TOKEN
shell: powershell
steps:
  - action: run
    command: [node, scripts/run.mjs, hello world]
    cwd: scripts
`)

	skill, err := gs.parseSkillYAML(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillYAML error: %v", err)
	}
	if len(skill.RequiredEnv) != 1 || skill.RequiredEnv[0] != "API_TOKEN" {
		t.Fatalf("required env was not preserved: %#v", skill.RequiredEnv)
	}
	if skill.PreferredShell != "powershell" {
		t.Fatalf("preferred shell = %q, want powershell", skill.PreferredShell)
	}
	if len(skill.Steps) != 1 || skill.Steps[0].Action != "bash" {
		t.Fatalf("unexpected normalized steps: %+v", skill.Steps)
	}
	cmd, _ := skill.Steps[0].Params["command"].(string)
	if !strings.Contains(cmd, "node") || !strings.Contains(cmd, "\"hello world\"") || skill.Steps[0].Params["working_dir"] != "scripts" {
		t.Fatalf("top-level command/cwd were not normalized: command=%q params=%#v", cmd, skill.Steps[0].Params)
	}
}

func TestParseSkillYAML_GitHubStringStepsAndMetadataCompatibility(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.yaml", DefinitionType: githubDefinitionYAML}
	data := []byte(`name: browser
steps:
  - echo hello
  - name: capture-step
    command: echo token
    capture:
      token: token
    with:
      format: json
`)

	skill, err := gs.parseSkillYAML(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillYAML error: %v", err)
	}
	if len(skill.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(skill.Steps))
	}
	if skill.Steps[0].Action != "bash" || skill.Steps[0].Params["command"] != "echo hello" {
		t.Fatalf("string step was not normalized: %+v", skill.Steps[0])
	}
	if skill.Steps[1].Name != "capture-step" || skill.Steps[1].Capture["token"] != "token" || skill.Steps[1].Params["format"] != "json" {
		t.Fatalf("step metadata/with params were not preserved: %+v", skill.Steps[1])
	}
}

func TestParseSkillYAML_GitHubControlFlowAliases(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.yaml", DefinitionType: githubDefinitionYAML}
	data := []byte(`name: browser
steps:
  - command: echo maybe
    only_if: "{{mode}} == run"
    continue_on_fail: true
  - command: echo repair
    on_failure: true
`)

	skill, err := gs.parseSkillYAML(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillYAML error: %v", err)
	}
	if len(skill.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(skill.Steps))
	}
	if skill.Steps[0].When != "{{mode}} == run" || skill.Steps[0].OnError != "continue" {
		t.Fatalf("GitHub control-flow aliases were not normalized: %+v", skill.Steps[0])
	}
	if skill.Steps[1].Condition != "on_failure" {
		t.Fatalf("GitHub on_failure alias was not normalized: %+v", skill.Steps[1])
	}
}

func TestParseSkillYAML_GitHubSkillLevelCompatibility(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.yaml", DefinitionType: githubDefinitionYAML}
	data := []byte(`name: github-api
requires_gui: "true"
global_timeout: "300"
pip: requests
npm: playwright
params:
  input:
    desc: Input file
    required: yes
operations:
  generate:
    steps: [create, verify]
pipeline:
  - extract
steps:
  - label: create
    command: echo create
    poll:
      pattern: READY
      retries: "2"
  - label: verify
    command: echo verify
`)

	skill, err := gs.parseSkillYAML(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillYAML error: %v", err)
	}
	if skill.Mode != "api_workflow" || !skill.RequiresGUI || skill.GlobalTimeout != 300 {
		t.Fatalf("GitHub mode/scalars were not normalized: mode=%q gui=%v timeout=%d", skill.Mode, skill.RequiresGUI, skill.GlobalTimeout)
	}
	if len(skill.RequiresPython) != 1 || skill.RequiresPython[0] != "requests" || len(skill.RequiresNode) != 1 || skill.RequiresNode[0] != "playwright" {
		t.Fatalf("GitHub requires aliases were not preserved: python=%#v node=%#v", skill.RequiresPython, skill.RequiresNode)
	}
	if len(skill.Params) != 1 || skill.Params[0].Name != "input" || !skill.Params[0].Required || skill.Params[0].Description != "Input file" {
		t.Fatalf("GitHub params were not normalized: %#v", skill.Params)
	}
	if len(skill.Operations) != 1 || skill.Operations[0].Name != "generate" || len(skill.Operations[0].Labels) != 2 || skill.Operations[0].Labels[0] != "create" {
		t.Fatalf("GitHub operations were not normalized: %#v", skill.Operations)
	}
	if len(skill.Pipeline) != 1 || skill.Pipeline[0].Skill != "extract" {
		t.Fatalf("GitHub pipeline was not preserved: %#v", skill.Pipeline)
	}
	if len(skill.Steps) != 2 || skill.Steps[0].Poll == nil || skill.Steps[0].Poll.UntilMatch != "READY" || skill.Steps[0].Poll.MaxAttempts != 2 {
		t.Fatalf("GitHub step poll was not normalized: %+v", skill.Steps)
	}
}

func TestParseSkillYAML_GitHubPipelineModeWhenNoOperations(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.yaml", DefinitionType: githubDefinitionYAML}
	data := []byte(`name: github-pipeline
pipeline:
  - extract
  - load:
      target: warehouse
`)

	skill, err := gs.parseSkillYAML(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillYAML error: %v", err)
	}
	if skill.Mode != "pipeline" || len(skill.Pipeline) != 2 || skill.Pipeline[1].Skill != "load" || skill.Pipeline[1].Params["target"] != "warehouse" {
		t.Fatalf("GitHub pipeline mode/steps were not normalized: mode=%q pipeline=%#v", skill.Mode, skill.Pipeline)
	}
}

func TestParseSkillMarkdown_GitHubFrontmatterMetadataCompatibility(t *testing.T) {
	gs := NewGitHubSearcher("")
	candidate := GitHubSkillCandidate{RepoFullName: "octo/skills", RepoURL: "https://github.com/octo/skills", FilePath: "skill.md", DefinitionType: githubDefinitionSkillMD}
	data := []byte(`---
name: github-md
requires_gui: "true"
global_timeout: "120"
pip: requests
params:
  input:
    desc: Input file
    required: yes
operations:
  generate: create
pipeline:
  - extract
---

# GitHub MD

Use this skill.
`)

	skill, err := gs.parseSkillMarkdown(data, candidate)
	if err != nil {
		t.Fatalf("parseSkillMarkdown error: %v", err)
	}
	if skill.Name != "github-md" || !skill.RequiresGUI || skill.GlobalTimeout != 120 {
		t.Fatalf("GitHub markdown scalars were not preserved: name=%q gui=%v timeout=%d", skill.Name, skill.RequiresGUI, skill.GlobalTimeout)
	}
	if len(skill.RequiresPython) != 1 || skill.RequiresPython[0] != "requests" {
		t.Fatalf("GitHub markdown requires not preserved: %#v", skill.RequiresPython)
	}
	if len(skill.Params) != 1 || skill.Params[0].Name != "input" || !skill.Params[0].Required {
		t.Fatalf("GitHub markdown params not preserved: %#v", skill.Params)
	}
	if skill.Mode != "api_workflow" || len(skill.Operations) != 1 || skill.Operations[0].Name != "generate" || len(skill.Operations[0].Labels) != 1 || skill.Operations[0].Labels[0] != "create" {
		t.Fatalf("GitHub markdown operations not preserved: mode=%q ops=%#v", skill.Mode, skill.Operations)
	}
	if len(skill.Pipeline) != 1 || skill.Pipeline[0].Skill != "extract" {
		t.Fatalf("GitHub markdown pipeline not preserved: %#v", skill.Pipeline)
	}
	if len(skill.Steps) != 1 || skill.Steps[0].Action != "craft_tool" {
		t.Fatalf("GitHub markdown should still use craft_tool body: %+v", skill.Steps)
	}
}
