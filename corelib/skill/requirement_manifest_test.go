package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestParseRequirementsTxt_BasicPackages(t *testing.T) {
	dir := t.TempDir()
	content := `
# This is a comment
requests>=2.28
flask==2.3.0
numpy
pandas>=1.5,<2.0

# Another comment
scipy
`
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	expected := []string{"requests>=2.28", "flask==2.3.0", "numpy", "pandas>=1.5,<2.0", "scipy"}
	if len(pkgs) != len(expected) {
		t.Fatalf("got %d packages, want %d: %v", len(pkgs), len(expected), pkgs)
	}
	for i, pkg := range pkgs {
		if pkg != expected[i] {
			t.Errorf("pkg[%d] = %q, want %q", i, pkg, expected[i])
		}
	}
}

func TestParseRequirementsTxt_SkipsOptions(t *testing.T) {
	dir := t.TempDir()
	content := `
--index-url https://pypi.org/simple
-i https://mirrors.aliyun.com/pypi/simple/
-e ./local-package
-f https://download.pytorch.org/whl/torch_stable.html
requests
./relative-path
/absolute/path
git+https://github.com/user/repo.git
numpy
`
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	expected := []string{"requests", "numpy"}
	if len(pkgs) != len(expected) {
		t.Fatalf("got %d packages, want %d: %v", len(pkgs), len(expected), pkgs)
	}
}

func TestParseRequirementsTxt_Extras(t *testing.T) {
	dir := t.TempDir()
	content := `requests[security]>=2.28
uvicorn[standard]`
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "requests>=2.28" {
		t.Errorf("pkg[0] = %q, want %q", pkgs[0], "requests>=2.28")
	}
	if pkgs[1] != "uvicorn" {
		t.Errorf("pkg[1] = %q, want %q", pkgs[1], "uvicorn")
	}
}

func TestParseRequirementsTxt_EnvMarkers(t *testing.T) {
	dir := t.TempDir()
	content := `pywin32>=300; sys_platform == "win32"
numpy>=1.21; python_version>="3.8"`
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "pywin32>=300" {
		t.Errorf("pkg[0] = %q, want %q", pkgs[0], "pywin32>=300")
	}
	if pkgs[1] != "numpy>=1.21" {
		t.Errorf("pkg[1] = %q, want %q", pkgs[1], "numpy>=1.21")
	}
}

func TestParseRequirementsTxt_RecursiveIncludes(t *testing.T) {
	dir := t.TempDir()
	baseContent := `flask
-r sub/more.txt
numpy`
	subDir := filepath.Join(dir, "sub")
	os.MkdirAll(subDir, 0755)
	subContent := `scipy
pandas`
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(baseContent), 0644)
	os.WriteFile(filepath.Join(subDir, "more.txt"), []byte(subContent), 0644)

	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	expected := []string{"flask", "scipy", "pandas", "numpy"}
	if len(pkgs) != len(expected) {
		t.Fatalf("got %d packages, want %d: %v", len(pkgs), len(expected), pkgs)
	}
	for i, pkg := range pkgs {
		if pkg != expected[i] {
			t.Errorf("pkg[%d] = %q, want %q", i, pkg, expected[i])
		}
	}
}

func TestParsePyprojectDependencies_MultiLine(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "my-skill"
version = "1.0.0"
dependencies = [
    "requests>=2.28",
    "flask==2.3.0",
    "numpy",
]

[build-system]
requires = ["setuptools"]`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644)

	pkgs := parsePyprojectDependencies(filepath.Join(dir, "pyproject.toml"))
	expected := []string{"requests>=2.28", "flask==2.3.0", "numpy"}
	if len(pkgs) != len(expected) {
		t.Fatalf("got %d packages, want %d: %v", len(pkgs), len(expected), pkgs)
	}
	for i, pkg := range pkgs {
		if pkg != expected[i] {
			t.Errorf("pkg[%d] = %q, want %q", i, pkg, expected[i])
		}
	}
}

func TestParsePyprojectDependencies_SingleLine(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "tiny"
dependencies = ["requests", "flask>=2.0"]`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644)

	pkgs := parsePyprojectDependencies(filepath.Join(dir, "pyproject.toml"))
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(pkgs), pkgs)
	}
}

func TestParsePyprojectDependencies_IgnoresBuildSystem(t *testing.T) {
	dir := t.TempDir()
	// This pyproject.toml has dependencies under [build-system] but NOT [project].
	// The parser should NOT extract these.
	content := `[build-system]
requires = ["setuptools>=61.0", "wheel"]
build-backend = "setuptools.backends"

[tool.poetry.dependencies]
python = "^3.9"
requests = "^2.28"
`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644)

	pkgs := parsePyprojectDependencies(filepath.Join(dir, "pyproject.toml"))
	if len(pkgs) != 0 {
		t.Fatalf("got %d packages from non-[project] section, want 0: %v", len(pkgs), pkgs)
	}
}

func TestParsePyprojectDependencies_ProjectSectionOnly(t *testing.T) {
	dir := t.TempDir()
	content := `[build-system]
requires = ["setuptools"]

[project]
name = "my-tool"
dependencies = [
    "click>=8.0",
    "rich",
]

[tool.pytest.ini_options]
testpaths = ["tests"]
`
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644)

	pkgs := parsePyprojectDependencies(filepath.Join(dir, "pyproject.toml"))
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "click>=8.0" {
		t.Errorf("pkg[0] = %q, want %q", pkgs[0], "click>=8.0")
	}
	if pkgs[1] != "rich" {
		t.Errorf("pkg[1] = %q, want %q", pkgs[1], "rich")
	}
}

func TestParsePackageJsonDeps(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "name": "my-skill",
  "dependencies": {
    "puppeteer": "^21.0.0",
    "express": "4.18.2"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(content), 0644)

	pkgs := parsePackageJsonDeps(filepath.Join(dir, "package.json"))
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want 3: %v", len(pkgs), pkgs)
	}
	// Should return bare package names (no version specs) — version constraints
	// are in package.json and respected by `npm install` directly.
	found := map[string]bool{}
	for _, p := range pkgs {
		found[p] = true
	}
	if !found["puppeteer"] {
		t.Errorf("missing puppeteer in %v", pkgs)
	}
	if !found["express"] {
		t.Errorf("missing express in %v", pkgs)
	}
	if !found["typescript"] {
		t.Errorf("missing typescript in %v", pkgs)
	}
}

func TestInferManifestFileRequirements_DeduplicatesAgainstExplicit(t *testing.T) {
	dir := t.TempDir()
	// requirements.txt has "requests" and "flask"
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests\nflask\nnumpy"), 0644)

	skill := &corelib.NLSkillEntry{
		Name:           "test-skill",
		SkillDir:       dir,
		RequiresPython: []string{"requests>=2.28"}, // already declared
	}

	reqs := inferManifestFileRequirements(skill, dir, nil)
	// "requests" should be deduplicated, only "flask" and "numpy" remain
	if len(reqs) != 2 {
		t.Fatalf("got %d requirements, want 2 (flask, numpy): %v", len(reqs), reqs)
	}
	names := make([]string, len(reqs))
	for i, r := range reqs {
		names[i] = r.Name
	}
	if names[0] != "flask" || names[1] != "numpy" {
		t.Errorf("got names %v, want [flask numpy]", names)
	}
	for _, r := range reqs {
		if r.Source != manifestSourceTag {
			t.Errorf("requirement %s has source %q, want %q", r.Name, r.Source, manifestSourceTag)
		}
	}
}

func TestInferManifestFileRequirements_BothPythonAndNode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\nuvicorn"), 0644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"puppeteer":"^21.0"}}`), 0644)

	skill := &corelib.NLSkillEntry{
		Name:     "full-stack",
		SkillDir: dir,
	}

	reqs := inferManifestFileRequirements(skill, dir, nil)
	pipCount := 0
	npmCount := 0
	for _, r := range reqs {
		switch r.Type {
		case "pip":
			pipCount++
		case "npm":
			npmCount++
		}
	}
	if pipCount != 2 {
		t.Errorf("got %d pip reqs, want 2", pipCount)
	}
	if npmCount != 1 {
		t.Errorf("got %d npm reqs, want 1", npmCount)
	}
}

func TestInferManifestFileRequirements_NoManifestFiles(t *testing.T) {
	dir := t.TempDir()
	skill := &corelib.NLSkillEntry{
		Name:     "no-manifest",
		SkillDir: dir,
	}
	reqs := inferManifestFileRequirements(skill, dir, nil)
	if len(reqs) != 0 {
		t.Fatalf("got %d requirements from empty dir, want 0", len(reqs))
	}
}

func TestInferManifestFileRequirements_NpmContextCarriesSkillDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"express":"4.18"}}`), 0644)

	skill := &corelib.NLSkillEntry{
		Name:     "node-skill",
		SkillDir: dir,
	}

	reqs := inferManifestFileRequirements(skill, dir, nil)
	if len(reqs) != 1 {
		t.Fatalf("got %d requirements, want 1", len(reqs))
	}
	if reqs[0].Context == nil || reqs[0].Context["skill_dir"] != dir {
		t.Errorf("npm requirement missing skill_dir context: %v", reqs[0].Context)
	}
}

func TestHasRequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	if HasRequirementsTxt(dir) {
		t.Error("empty dir should not have requirements.txt")
	}
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests\n"), 0644)
	if !HasRequirementsTxt(dir) {
		t.Error("dir with requirements.txt should return true")
	}
}

func TestNpmNodeModulesStale(t *testing.T) {
	dir := t.TempDir()
	// No package.json → not stale
	if NpmNodeModulesStale(dir) {
		t.Error("no package.json should not be stale")
	}
	// Create package.json, no node_modules → stale
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)
	if !NpmNodeModulesStale(dir) {
		t.Error("package.json without node_modules should be stale")
	}
	// Create node_modules/.package-lock.json newer than package.json → not stale
	nmDir := filepath.Join(dir, "node_modules")
	os.MkdirAll(nmDir, 0755)
	os.WriteFile(filepath.Join(nmDir, ".package-lock.json"), []byte(`{}`), 0644)
	if NpmNodeModulesStale(dir) {
		t.Error("fresh node_modules should not be stale")
	}
}

func TestParseRequirementsTxt_InlineComments(t *testing.T) {
	dir := t.TempDir()
	content := `requests>=2.28 # HTTP library
flask # web framework`
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(content), 0644)
	pkgs := parseRequirementsTxt(filepath.Join(dir, "requirements.txt"), dir)
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(pkgs), pkgs)
	}
	if pkgs[0] != "requests>=2.28" {
		t.Errorf("pkg[0] = %q, want %q", pkgs[0], "requests>=2.28")
	}
	if pkgs[1] != "flask" {
		t.Errorf("pkg[1] = %q, want %q", pkgs[1], "flask")
	}
}

func TestExtractRequirements_IntegrationWithManifest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\nuvicorn\n"), 0644)

	skill := &corelib.NLSkillEntry{
		Name:           "integration-test",
		SkillDir:       dir,
		RequiresPython: []string{"requests"}, // explicit, should not duplicate
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python app.py"}},
		},
	}

	reqs := ExtractRequirements(skill)
	// Should contain: requests (explicit) + flask, uvicorn (manifest) + python (inferred command)
	var pipNames []string
	for _, r := range reqs {
		if r.Type == "pip" {
			pipNames = append(pipNames, r.Name)
		}
	}
	if !sliceContains(pipNames, "requests") {
		t.Error("missing explicit pip requirement 'requests'")
	}
	if !sliceContains(pipNames, "flask") {
		t.Error("missing manifest pip requirement 'flask'")
	}
	if !sliceContains(pipNames, "uvicorn") {
		t.Error("missing manifest pip requirement 'uvicorn'")
	}
	// Ensure no duplicates
	seen := map[string]int{}
	for _, n := range pipNames {
		seen[strings.ToLower(n)]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("duplicate pip requirement %q appeared %d times", name, count)
		}
	}
}

func TestExtractRequirements_DeduplicatesManifestVsScriptInference(t *testing.T) {
	dir := t.TempDir()
	// requirements.txt declares "flask"
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\nrequests\n"), 0644)
	// Python script also imports "flask" and "requests"
	os.WriteFile(filepath.Join(dir, "app.py"), []byte("import flask\nimport requests\nimport numpy\n"), 0644)

	skill := &corelib.NLSkillEntry{
		Name:     "dedup-test",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python " + dir + "/app.py"}},
		},
	}

	reqs := ExtractRequirements(skill)
	// Count pip requirements — flask and requests should appear only once each
	pipCounts := map[string]int{}
	for _, r := range reqs {
		if r.Type == "pip" {
			pipCounts[strings.ToLower(r.Name)]++
		}
	}
	for name, count := range pipCounts {
		if count > 1 {
			t.Errorf("pip package %q appears %d times, want 1 (dedup failed)", name, count)
		}
	}
	// numpy should appear (from script inference, not in requirements.txt)
	if pipCounts["numpy"] != 1 {
		t.Errorf("numpy count = %d, want 1 (script-inferred)", pipCounts["numpy"])
	}
}

func sliceContains(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}
