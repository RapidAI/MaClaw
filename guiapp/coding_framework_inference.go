package guiapp

// coding_framework_inference.go infers a small set of framework/library hints
// from local project manifests. The hints only affect recall ranking (a
// language-scoped experience tied to the same framework ranks higher); they
// never filter results or change scope.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	codingFrameworkHintTTL       = 2 * time.Minute
	codingFrameworkManifestLimit = 1 << 20 // 1 MiB
	maxCodingFrameworkHints      = 12
)

type codingFrameworkHintEntry struct {
	expiresAt  time.Time
	frameworks []string
}

var codingFrameworkHintCache sync.Map

type codingLanguageHintEntry struct {
	expiresAt time.Time
	language  string
}

var codingLanguageHintCache sync.Map

// inferCodingFrameworks reads well-known manifests under projectPath. It is
// deliberately conservative: it only recognizes explicit dependency names, so
// an unknown or huge manifest degrades to "no hints" rather than a wrong boost.
func inferCodingFrameworks(projectPath string, files []string) []string {
	projectPath = strings.TrimSpace(projectPath)
	now := time.Now()
	if projectPath != "" {
		if cached, ok := codingFrameworkHintCache.Load(projectPath); ok {
			if entry, ok := cached.(codingFrameworkHintEntry); ok && now.Before(entry.expiresAt) {
				return append([]string(nil), entry.frameworks...)
			}
			codingFrameworkHintCache.Delete(projectPath)
		}
	}

	seen := make(map[string]struct{})
	add := func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			seen[name] = struct{}{}
		}
	}

	if projectPath != "" {
		readGoModFrameworks(filepath.Join(projectPath, "go.mod"), add)
		readNodeManifestFrameworks(filepath.Join(projectPath, "package.json"), add)
		readPythonFrameworks(filepath.Join(projectPath, "requirements.txt"), add)
		readPythonFrameworks(filepath.Join(projectPath, "pyproject.toml"), add)
		readCargoFrameworks(filepath.Join(projectPath, "Cargo.toml"), add)
		readJavaFrameworks(filepath.Join(projectPath, "pom.xml"), add)
		readJavaFrameworks(filepath.Join(projectPath, "build.gradle"), add)
		readJavaFrameworks(filepath.Join(projectPath, "build.gradle.kts"), add)
	}
	// Extension-only hints are safe when the file type itself is unambiguous.
	for _, file := range files {
		switch strings.ToLower(filepath.Ext(strings.TrimSpace(file))) {
		case ".vue":
			add("vue")
		case ".svelte":
			add("svelte")
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > maxCodingFrameworkHints {
		out = out[:maxCodingFrameworkHints]
	}
	if projectPath != "" {
		codingFrameworkHintCache.Store(projectPath, codingFrameworkHintEntry{
			expiresAt:  now.Add(codingFrameworkHintTTL),
			frameworks: append([]string(nil), out...),
		})
	}
	return out
}

func readCodingFrameworkManifest(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > codingFrameworkManifestLimit {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

func readGoModFrameworks(path string, add func(string)) {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		add(frameworkFromGoModule(line))
	}
}

func frameworkFromGoModule(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "gin-gonic/gin"):
		return "gin"
	case strings.Contains(lower, "labstack/echo"):
		return "echo"
	case strings.Contains(lower, "gofiber/fiber"):
		return "fiber"
	case strings.Contains(lower, "gorm.io/gorm"):
		return "gorm"
	case strings.Contains(lower, "google.golang.org/grpc"):
		return "grpc"
	case strings.Contains(lower, "go-redis"):
		return "redis"
	case strings.Contains(lower, "gorilla/mux"):
		return "gorilla"
	case strings.Contains(lower, "go-chi/chi"):
		return "chi"
	default:
		return ""
	}
}

func readNodeManifestFrameworks(path string, add func(string)) {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return
	}
	for name := range pkg.Dependencies {
		add(frameworkFromNodeDependency(name))
	}
	for name := range pkg.DevDependencies {
		add(frameworkFromNodeDependency(name))
	}
}

func frameworkFromNodeDependency(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "react", "react-dom":
		return "react"
	case "react-native":
		return "react-native"
	case "next":
		return "next"
	case "vue":
		return "vue"
	case "nuxt":
		return "nuxt"
	case "@angular/core":
		return "angular"
	case "svelte":
		return "svelte"
	case "@nestjs/core":
		return "nestjs"
	case "express":
		return "express"
	case "koa":
		return "koa"
	case "fastify":
		return "fastify"
	case "vite":
		return "vite"
	case "vitest":
		return "vitest"
	case "jest":
		return "jest"
	default:
		return ""
	}
}

func readPythonFrameworks(path string, add func(string)) {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return
	}
	lower := strings.ToLower(string(data))
	for _, token := range []string{
		"django", "flask", "fastapi", "pytest", "sqlalchemy", "pydantic",
		"celery", "pandas", "numpy", "torch", "tensorflow",
	} {
		if strings.Contains(lower, token) {
			add(token)
		}
	}
}

func readCargoFrameworks(path string, add func(string)) {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return
	}
	lower := strings.ToLower(string(data))
	for _, token := range []string{
		"axum", "actix-web", "rocket", "tokio", "serde", "sqlx", "diesel", "tonic",
	} {
		if strings.Contains(lower, token) {
			add(token)
		}
	}
}

func readJavaFrameworks(path string, add func(string)) {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return
	}
	lower := strings.ToLower(string(data))
	for _, token := range []string{
		"spring-boot", "spring-web", "hibernate", "junit", "mybatis", "quarkus", "micronaut",
	} {
		if strings.Contains(lower, token) {
			add(token)
		}
	}
}

// inferCodingProjectLanguage returns the primary project language from its
// build manifest. It is a fallback for high-level tasks that name no source
// files; extension-based task inference remains authoritative because it also
// handles multi-language repositories. A project with manifests from more than
// one language family returns empty: filtering recall by a guessed language
// would hide the other half of a monorepo.
func inferCodingProjectLanguage(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	now := time.Now()
	if cached, ok := codingLanguageHintCache.Load(projectPath); ok {
		if entry, ok := cached.(codingLanguageHintEntry); ok && now.Before(entry.expiresAt) {
			return entry.language
		}
		codingLanguageHintCache.Delete(projectPath)
	}
	hasFile := func(name string) bool {
		info, err := os.Stat(filepath.Join(projectPath, name))
		return err == nil && !info.IsDir()
	}
	languages := make([]string, 0, 2)
	if hasFile("go.mod") {
		languages = append(languages, "go")
	}
	if hasFile("Cargo.toml") {
		languages = append(languages, "rust")
	}
	if hasFile("pyproject.toml") || hasFile("requirements.txt") || hasFile("setup.py") {
		languages = append(languages, "python")
	}
	if hasFile("package.json") {
		// Next/Angular/Vue projects are frequently JavaScript too; only a
		// tsconfig or an explicit TypeScript dependency makes the project
		// TypeScript. Guessing from the framework would hide JavaScript guidance
		// in a legitimate JS project.
		typescript := hasFile("tsconfig.json") ||
			nodeManifestHasDependency(filepath.Join(projectPath, "package.json"), "typescript")
		if typescript {
			languages = append(languages, "typescript")
		} else {
			languages = append(languages, "javascript")
		}
	}
	if hasFile("pom.xml") || hasFile("build.gradle") || hasFile("build.gradle.kts") {
		languages = append(languages, "java")
	}
	if hasFile("Gemfile") {
		languages = append(languages, "ruby")
	}
	if hasFile("composer.json") {
		languages = append(languages, "php")
	}
	language := ""
	if len(languages) == 1 {
		language = languages[0]
	}
	codingLanguageHintCache.Store(projectPath, codingLanguageHintEntry{
		expiresAt: now.Add(codingFrameworkHintTTL),
		language:  language,
	})
	return language
}

func nodeManifestHasDependency(path, dependency string) bool {
	data, ok := readCodingFrameworkManifest(path)
	if !ok {
		return false
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	if _, ok := pkg.Dependencies[dependency]; ok {
		return true
	}
	_, ok = pkg.DevDependencies[dependency]
	return ok
}
