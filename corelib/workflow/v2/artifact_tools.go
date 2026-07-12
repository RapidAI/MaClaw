package v2

import (
	"fmt"
	"strings"
)

// ArtifactPhaseAllowedTools lists tools that may appear during artifact-scoped
// phases (PPT/PDF/office deliverables). Project mutation tools stay blocked.
var ArtifactPhaseAllowedTools = map[string]bool{
	"bash":                     true,
	"craft_tool":               true,
	"generate_pdf":             true,
	"list_directory":           true,
	"manage_skill":             true,
	"office":                   true,
	"read_file":                true,
	"search_and_install_skill": true,
	"send_file":                true,
	"web_fetch":                true,
	"web_search":               true,
	"write_file":               true,
}

// IsArtifactPhase returns true when the phase generates deliverable artifacts
// rather than mutating project source.
func IsArtifactPhase(phase *Phase) bool {
	if phase == nil {
		return false
	}
	return phase.Kind == PhaseKindArtifactGeneration || phase.MutationScope == MutationScopeArtifact
}

// IsToolAllowedInArtifactPhase reports whether the tool name itself is allowed
// during an artifact-scoped phase.
func IsToolAllowedInArtifactPhase(name string) bool {
	return ArtifactPhaseAllowedTools[strings.TrimSpace(name)]
}

// ValidateArtifactPhaseToolCall rejects project-mutation tools and paths that
// target source directories during artifact generation phases.
func ValidateArtifactPhaseToolCall(name string, args map[string]interface{}) error {
	name = strings.TrimSpace(name)
	if !IsToolAllowedInArtifactPhase(name) {
		return fmt.Errorf("artifact workflow phase cannot run project mutation tools")
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	path := ""
	switch name {
	case "write_file":
		path = stringArg(args, "path")
	case "office":
		path = firstNonEmptyArg(args, "file_path", "path", "output")
	case "web_fetch":
		path = firstNonEmptyArg(args, "save_path", "output")
	case "bash":
		if cmd := stringArg(args, "command"); cmd != "" {
			for _, token := range artifactPathTokens(cmd) {
				if dir := matchedProjectMutationDir(token); dir != "" {
					return fmt.Errorf("artifact workflow phase cannot run bash commands that target source/project paths. Use a non-source output directory instead")
				}
			}
		}
	case "craft_tool":
		if text := firstNonEmptyArg(args, "task", "instructions", "description", "user_prompt"); text != "" {
			if containsProjectMutationReference(text) {
				return fmt.Errorf("artifact workflow phase cannot craft tools that mutate source/project paths")
			}
		}
	}
	if path != "" {
		if dir := matchedProjectMutationDir(path); dir != "" {
			return fmt.Errorf("artifact workflow phase cannot write into source/project paths (matched: %s/). Use a temp or output directory instead", dir)
		}
		if IsProjectControlPath(path) {
			return fmt.Errorf("artifact workflow phase cannot write project control files (matched: %s). Use a temp or output directory instead", pathBaseName(path))
		}
	}
	return nil
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func firstNonEmptyArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v := stringArg(args, key); v != "" {
			return v
		}
	}
	return ""
}

func matchedProjectMutationDir(path string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	normalized = strings.TrimPrefix(normalized, "./")
	if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/' {
		normalized = normalized[3:]
	}
	for _, dir := range []string{"src", "cmd", "internal", "pkg", "frontend", "backend"} {
		if strings.HasPrefix(normalized, dir+"/") {
			return dir
		}
		if strings.Contains(normalized, "/"+dir+"/") {
			return dir
		}
	}
	for _, dir := range []string{"app", "web"} {
		if strings.HasPrefix(normalized, dir+"/") {
			return dir
		}
	}
	return ""
}

func pathBaseName(path string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if i := strings.LastIndex(normalized, "/"); i >= 0 {
		return normalized[i+1:]
	}
	return normalized
}

// IsProjectControlPath reports whether path is a project build/config control
// file that artifact phases must not rewrite (even at repo root).
func IsProjectControlPath(path string) bool {
	base := strings.ToLower(pathBaseName(path))
	switch base {
	case "cmakelists.txt", "makefile", "gnumakefile", "dockerfile",
		"go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml",
		"yarn.lock", "cargo.toml", "cargo.lock", "pom.xml", "build.gradle",
		"build.gradle.kts", "settings.gradle", "settings.gradle.kts",
		".gitignore", ".gitattributes", "pyproject.toml", "requirements.txt",
		"setup.py", "setup.cfg", "tsconfig.json":
		return true
	default:
		return false
	}
}

func containsProjectMutationReference(text string) bool {
	if !textHasProjectMutationIntent(text) {
		return false
	}
	for _, token := range artifactPathTokens(text) {
		if matchedProjectMutationDir(token) != "" {
			return true
		}
	}
	return false
}

func textHasProjectMutationIntent(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	for _, marker := range []string{
		"write", "modify", "update", "edit", "patch", "refactor", "overwrite", "save", "create",
		"写", "写入", "修改", "更新", "编辑", "补丁", "重构", "覆盖", "保存", "创建", "新建",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func artifactPathTokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(text, "\\", "/")), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/')
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.Trim(strings.TrimSpace(field), `"'“”‘’()[]{}<>，。！？；：、,;:`)
		token = strings.TrimPrefix(token, "./")
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}
