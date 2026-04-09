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
	if got := skill.Steps[0].Params["verification_mode"]; got != "artifact_required" {
		t.Fatalf("verification_mode = %#v, want %q", got, "artifact_required")
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

func TestDefinitionTypeForPath(t *testing.T) {
	cases := map[string]string{
		"foo/skill.md":   githubDefinitionSkillMD,
		"foo/skill.yaml": githubDefinitionYAML,
		"foo/README.md":  "",
	}
	for input, want := range cases {
		if got := definitionTypeForPath(input, ""); got != want {
			t.Fatalf("definitionTypeForPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSearchGitHubReturnsCombinedCandidates(t *testing.T) {
	gs := NewGitHubSearcher("")
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
	}
	gs.client = fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query().Get("q")
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
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if calls["skill.md"] != 1 || calls["skill.yaml"] != 1 {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if results[0].DefinitionType != githubDefinitionSkillMD {
		t.Fatalf("expected first result to be skill_md, got %q", results[0].DefinitionType)
	}
	if results[1].DefinitionType != githubDefinitionYAML {
		t.Fatalf("expected second result to be yaml, got %q", results[1].DefinitionType)
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
