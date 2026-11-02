package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferCodingFrameworksFromManifests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module example\n\nrequire (\n\tgithub.com/gin-gonic/gin v1.9.0\n\tgorm.io/gorm v1.25.0\n)\n")
	write("package.json", `{"dependencies":{"react":"^18","next":"^14"},"devDependencies":{"vitest":"^1"}}`)
	write("requirements.txt", "Django==5.0\nfastapi==0.110\n")
	write("Cargo.toml", "[dependencies]\naxum = \"0.7\"\n")
	write("pom.xml", "<dependency><artifactId>spring-boot-starter-web</artifactId></dependency>")

	got := inferCodingFrameworks(dir, nil)
	for _, want := range []string{"gin", "gorm", "react", "next", "vitest", "django", "fastapi", "axum", "spring-boot"} {
		if !codingFrameworkHintsContain(got, want) {
			t.Fatalf("frameworks = %v, want %q", got, want)
		}
	}

	if got := inferCodingFrameworks(t.TempDir(), nil); len(got) != 0 {
		t.Fatalf("empty project frameworks = %v, want none", got)
	}
}

func TestInferCodingProjectLanguageFromManifest(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"go", map[string]string{"go.mod": "module example\n"}, "go"},
		{"rust", map[string]string{"Cargo.toml": "[package]\nname = \"example\"\n"}, "rust"},
		{"python requirements", map[string]string{"requirements.txt": "django==5\n"}, "python"},
		{"python pyproject", map[string]string{"pyproject.toml": "[project]\nname = \"example\"\n"}, "python"},
		{"typescript", map[string]string{"package.json": `{"devDependencies":{"typescript":"^5"}}`, "tsconfig.json": "{}"}, "typescript"},
		{"typescript dependency", map[string]string{"package.json": `{"devDependencies":{"typescript":"^5"}}`}, "typescript"},
		{"javascript", map[string]string{"package.json": `{"dependencies":{"react":"^18"}}`}, "javascript"},
		{"next without tsconfig stays javascript", map[string]string{"package.json": `{"dependencies":{"next":"^14"}}`}, "javascript"},
		{"java maven", map[string]string{"pom.xml": "<project/>"}, "java"},
		{"ruby", map[string]string{"Gemfile": "gem 'rails'\n"}, "ruby"},
		{"php", map[string]string{"composer.json": "{}"}, "php"},
		{"mixed repo", map[string]string{"go.mod": "module example\n", "package.json": `{"dependencies":{"react":"^18"}}`}, ""},
		{"empty project", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			got := inferCodingProjectLanguage(dir)
			if got != tc.want {
				t.Fatalf("language = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInferCodingProjectLanguageCachesByPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if got := inferCodingProjectLanguage(dir); got != "go" {
		t.Fatalf("initial language = %q, want go", got)
	}
	// Within the short cache window the manifest change is intentionally not
	// re-read on every recall; the next task after expiry sees it.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if got := inferCodingProjectLanguage(dir); got != "go" {
		t.Fatalf("cached language = %q, want go", got)
	}
}

func codingFrameworkHintsContain(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
