package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PackageViewFromRuntimeEntry returns a copy of a runtime skill entry suitable
// for package/market validation. Runtime entries may contain local absolute
// paths after runner normalization; package views express paths relative to the
// skill package using {baseDir}.
func PackageViewFromRuntimeEntry(entry *corelib.NLSkillEntry, skillDir string) *corelib.NLSkillEntry {
	if entry == nil {
		return nil
	}
	cp := *entry
	cp.RequiredCredentialFiles = packageViewStringSliceFromRuntime(entry.RequiredCredentialFiles, skillDir)
	cp.Params = packageViewParamSchemaFromRuntime(entry.Params, skillDir)
	cp.Steps = make([]corelib.NLSkillStep, 0, len(entry.Steps))
	for _, step := range entry.Steps {
		cp.Steps = append(cp.Steps, packageViewStepFromRuntime(step, skillDir))
	}
	cp.Pipeline = make([]corelib.SkillPipelineStep, 0, len(entry.Pipeline))
	for _, step := range entry.Pipeline {
		stepCopy := step
		if step.Params != nil {
			stepCopy.Params = PackageViewStringMapFromRuntimeParams(step.Params, skillDir)
		}
		cp.Pipeline = append(cp.Pipeline, stepCopy)
	}
	cp.SkillDir = skillDir
	return &cp
}

func packageViewStringSliceFromRuntime(values []string, skillDir string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, ReplaceRuntimeSkillDirRefs(value, skillDir))
	}
	return out
}

func packageViewParamSchemaFromRuntime(params []corelib.NLSkillParam, skillDir string) []corelib.NLSkillParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]corelib.NLSkillParam, 0, len(params))
	for _, param := range params {
		paramCopy := param
		paramCopy.Aliases = append([]string(nil), param.Aliases...)
		paramCopy.Default = ReplaceRuntimeSkillDirRefs(param.Default, skillDir)
		out = append(out, paramCopy)
	}
	return out
}

func packageViewStepFromRuntime(step corelib.NLSkillStep, skillDir string) corelib.NLSkillStep {
	stepCopy := step
	if step.Params != nil {
		stepCopy.Params = PackageViewParamsFromRuntimeParams(step.Params, skillDir)
	}
	if step.FallbackStep != nil {
		fallback := packageViewStepFromRuntime(*step.FallbackStep, skillDir)
		stepCopy.FallbackStep = &fallback
	}
	return stepCopy
}

// PackageViewParamsFromRuntimeParams converts command/working-directory params
// from runner-local paths back to package-local references.
func PackageViewParamsFromRuntimeParams(params map[string]interface{}, skillDir string) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for key, value := range params {
		out[key] = packageViewParamValueFromRuntime(key, value, skillDir)
	}
	return out
}

// PackageViewStringMapFromRuntimeParams converts string-only param maps used by
// pipeline declarations without widening non-path text fields.
func PackageViewStringMapFromRuntimeParams(params map[string]string, skillDir string) map[string]string {
	out := make(map[string]string, len(params))
	for key, value := range params {
		out[key] = PackageViewStringFromRuntime(key, value, skillDir)
	}
	return out
}

func packageViewParamValueFromRuntime(key string, value interface{}, skillDir string) interface{} {
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	switch v := value.(type) {
	case string:
		return PackageViewStringFromRuntime(key, v, skillDir)
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, PackageViewStringFromRuntime(key, item, skillDir))
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, packageViewParamValueFromRuntime(key, item, skillDir))
		}
		return out
	case map[string]interface{}:
		if packageViewShouldRewriteStringKey(normalizedKey) {
			return packageViewExecutionMapFromRuntimeParams(v, skillDir)
		}
		return PackageViewParamsFromRuntimeParams(v, skillDir)
	case map[interface{}]interface{}:
		converted := packageViewStringKeyMap(v)
		if packageViewShouldRewriteStringKey(normalizedKey) {
			return packageViewExecutionMapFromRuntimeParams(converted, skillDir)
		}
		return PackageViewParamsFromRuntimeParams(converted, skillDir)
	case map[string]string:
		if packageViewShouldRewriteStringKey(normalizedKey) {
			return packageViewExecutionStringMapFromRuntimeParams(v, skillDir)
		}
		return PackageViewStringMapFromRuntimeParams(v, skillDir)
	default:
		return value
	}
}

func packageViewExecutionMapFromRuntimeParams(params map[string]interface{}, skillDir string) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for key, value := range params {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if packageViewIsExecutionCommandKey(normalizedKey) || packageViewIsExecutionArgKey(normalizedKey) {
			out[key] = packageViewExecutionArgValueFromRuntime(value, skillDir)
			continue
		}
		out[key] = packageViewParamValueFromRuntime(key, value, skillDir)
	}
	return out
}

func packageViewExecutionStringMapFromRuntimeParams(params map[string]string, skillDir string) map[string]string {
	out := make(map[string]string, len(params))
	for key, value := range params {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if packageViewIsExecutionCommandKey(normalizedKey) || packageViewIsExecutionArgKey(normalizedKey) {
			out[key] = ReplaceRuntimeSkillDirRefs(value, skillDir)
			continue
		}
		out[key] = PackageViewStringFromRuntime(key, value, skillDir)
	}
	return out
}

func packageViewExecutionArgValueFromRuntime(value interface{}, skillDir string) interface{} {
	switch v := value.(type) {
	case string:
		return ReplaceRuntimeSkillDirRefs(v, skillDir)
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, ReplaceRuntimeSkillDirRefs(item, skillDir))
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, packageViewExecutionArgValueFromRuntime(item, skillDir))
		}
		return out
	case map[string]interface{}:
		return packageViewExecutionMapFromRuntimeParams(v, skillDir)
	case map[interface{}]interface{}:
		return packageViewExecutionMapFromRuntimeParams(packageViewStringKeyMap(v), skillDir)
	case map[string]string:
		return packageViewExecutionStringMapFromRuntimeParams(v, skillDir)
	default:
		return value
	}
}

func packageViewStringKeyMap(params map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for key, value := range params {
		name := strings.TrimSpace(fmt.Sprintf("%v", key))
		if name == "" || name == "<nil>" {
			continue
		}
		out[name] = value
	}
	return out
}

func packageViewIsExecutionArgKey(key string) bool {
	switch key {
	case "arg", "args", "argv", "argument", "arguments":
		return true
	default:
		return false
	}
}

func packageViewIsExecutionCommandKey(key string) bool {
	switch key {
	case "program", "cmd", "command", "executable", "binary":
		return true
	default:
		return false
	}
}

// PackageViewStringFromRuntime converts a single runtime parameter string back
// to package semantics when it references files under skillDir.
func PackageViewStringFromRuntime(key, value, skillDir string) string {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" || strings.TrimSpace(skillDir) == "" {
		return value
	}
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	if packageViewIsWorkingDirKey(normalizedKey) {
		if rel, ok := PackageRelativePathFromRuntimePath(skillDir, trimmedValue); ok {
			if rel == "." {
				return ""
			}
			return rel
		}
		return value
	}
	if packageViewShouldRewriteStringKey(normalizedKey) {
		return ReplaceRuntimeSkillDirRefs(value, skillDir)
	}
	return value
}

func packageViewIsWorkingDirKey(key string) bool {
	switch key {
	case "working_dir", "cwd", "workdir", "dir":
		return true
	default:
		return false
	}
}

func packageViewShouldRewriteStringKey(key string) bool {
	if packageViewIsWorkingDirKey(key) {
		return true
	}
	switch key {
	case "command", "cmd", "run", "script", "shell_command", "file", "path", "filename", "filepath":
		return true
	default:
		return strings.HasSuffix(key, "_path") ||
			strings.HasSuffix(key, "_file") ||
			strings.HasSuffix(key, "_files") ||
			strings.HasSuffix(key, "_script") ||
			strings.HasSuffix(key, "_command")
	}
}

// ReplaceRuntimeSkillDirRefs rewrites references to the concrete skill
// directory as {baseDir} package references.
func ReplaceRuntimeSkillDirRefs(value, skillDir string) string {
	slashDir := filepath.ToSlash(strings.TrimRight(filepath.Clean(skillDir), `/\`))
	if slashDir == "" || slashDir == "." {
		return value
	}
	slashValue := filepath.ToSlash(value)
	return replaceRuntimeSkillDirRefsFold(slashValue, slashDir)
}

func replaceRuntimeSkillDirRefsFold(value, slashDir string) string {
	lowerValue := strings.ToLower(value)
	lowerDir := strings.ToLower(slashDir)
	if lowerDir == "" {
		return value
	}
	var out strings.Builder
	start := 0
	for {
		idx := strings.Index(lowerValue[start:], lowerDir)
		if idx < 0 {
			out.WriteString(value[start:])
			break
		}
		idx += start
		if !isRuntimeSkillDirRefBoundary(value, idx, idx+len(slashDir)) {
			out.WriteString(value[start : idx+len(slashDir)])
			start = idx + len(slashDir)
			continue
		}
		out.WriteString(value[start:idx])
		out.WriteString("{baseDir}")
		start = idx + len(slashDir)
	}
	return out.String()
}

func isRuntimeSkillDirRefBoundary(value string, start, end int) bool {
	if start < 0 || end > len(value) || start > end {
		return false
	}
	if start > 0 {
		prev := value[start-1]
		if isPackagePathChar(prev) {
			return false
		}
	}
	if end < len(value) {
		next := value[end]
		if next != '/' && isPackagePathChar(next) {
			return false
		}
	}
	return true
}

func isPackagePathChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-' || b == '.' || b == ':' || b == '~'
}

// PackageRelativePathFromRuntimePath returns the package-relative path for a
// runtime path when that path is inside skillDir.
func PackageRelativePathFromRuntimePath(skillDir, value string) (string, bool) {
	skillAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return "", false
	}
	valueAbs, err := filepath.Abs(value)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(filepath.Clean(skillAbs), filepath.Clean(valueAbs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	return filepath.ToSlash(rel), true
}
