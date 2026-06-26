package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// CollectMissingPackageFileReferences reports local package file references
// that would not survive upload to another machine. It extends the runner step
// precheck with upload/package metadata fields such as params defaults,
// required credential files, pipeline params, and explicit path-like step
// parameters.
func CollectMissingPackageFileReferences(entry *corelib.NLSkillEntry) []string {
	if entry == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var missing []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		missing = append(missing, ref)
	}

	for _, ref := range CollectMissingStepFileReferences(entry) {
		add(ref)
	}
	collectMissingPackageParamDefaults(entry.SkillDir, entry.Params, add)
	collectMissingPackageCredentialRefs(entry.SkillDir, entry.RequiredCredentialFiles, add)
	for _, step := range entry.Steps {
		collectMissingPackageStepParams(entry.SkillDir, step, add, map[*corelib.NLSkillStep]struct{}{})
	}
	for _, step := range entry.Pipeline {
		for _, ref := range packageLocalPathRefsFromStringMap(step.Params) {
			if missingRef, missing := missingPackageLocalRef(entry.SkillDir, ref, ""); missing {
				add(missingRef)
			}
		}
	}
	return missing
}

func collectMissingPackageParamDefaults(skillDir string, params []corelib.NLSkillParam, add func(string)) {
	for _, param := range params {
		defaultValue := strings.TrimSpace(param.Default)
		if defaultValue == "" {
			continue
		}
		if packageOutputPathKey(param.Name) {
			continue
		}
		var refs []string
		if packagePathKey(param.Name) {
			refs = append(refs, packageLocalPathRefsFromPathValue(defaultValue)...)
		} else {
			refs = append(refs, packageLocalPathRefFromString(defaultValue)...)
		}
		for _, ref := range refs {
			if missingRef, missing := missingPackageLocalRef(skillDir, ref, ""); missing {
				add(missingRef)
			}
		}
	}
}

func collectMissingPackageCredentialRefs(skillDir string, refs []string, add func(string)) {
	for _, ref := range refs {
		if packageRefShouldSkipRawString(ref) {
			continue
		}
		if missingRef, missing := missingPackageLocalRef(skillDir, ref, ""); missing {
			add(missingRef)
		}
	}
}

func collectMissingPackageStepParams(skillDir string, step corelib.NLSkillStep, add func(string), seenFallbacks map[*corelib.NLSkillStep]struct{}) {
	workingDir, invalidWorkingDir := packageStepWorkingDir(skillDir, step)
	if invalidWorkingDir != "" {
		add(invalidWorkingDir)
	}
	if workingDir != "" {
		if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(workingDir))); err != nil {
			add(workingDir)
		}
	}
	for _, ref := range packageLocalPathRefsFromParams(step.Params) {
		if missingRef, missing := missingPackageLocalRef(skillDir, ref, workingDir); missing {
			add(missingRef)
		}
	}
	if step.FallbackStep != nil {
		if _, seen := seenFallbacks[step.FallbackStep]; !seen {
			seenFallbacks[step.FallbackStep] = struct{}{}
			collectMissingPackageStepParams(skillDir, *step.FallbackStep, add, seenFallbacks)
		}
	}
}

func missingPackageLocalRef(skillDir, ref, workingDir string) (string, bool) {
	cleanRef := normalizePackageRelativePath(ref)
	if cleanRef == "" {
		return "", false
	}
	if packageRefIsSkillDir(skillDir, ref) {
		return "", false
	}
	if missingRef, invalid := invalidPackageLocalRef(ref); invalid {
		return missingRef, true
	}
	candidates := packageLocalRefCandidates(cleanRef, workingDir)
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(candidate))); err == nil {
			return "", false
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[len(candidates)-1], true
}

func packageRefIsSkillDir(skillDir, ref string) bool {
	skillDir = strings.TrimSpace(skillDir)
	ref = trimPackageRefDecorations(strings.TrimSpace(ref))
	if skillDir == "" || ref == "" || !packagePathIsAbs(ref) {
		return false
	}
	skillAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return false
	}
	refAbs, err := filepath.Abs(ref)
	if err != nil {
		refAbs = ref
	}
	skillClean := filepath.Clean(skillAbs)
	refClean := filepath.Clean(refAbs)
	return skillClean == refClean || strings.EqualFold(skillClean, refClean)
}

func invalidPackageLocalRef(ref string) (string, bool) {
	cleanRef := normalizePackageRelativePath(ref)
	if cleanRef == "" {
		return "", false
	}
	if packagePathIsAbs(ref) || packagePathIsAbs(cleanRef) || strings.HasPrefix(cleanRef, "../") {
		return cleanRef, true
	}
	return "", false
}

func packageStepWorkingDir(skillDir string, step corelib.NLSkillStep) (string, string) {
	rawWD := firstPackageString(step.Params, "working_dir", "cwd", "workdir", "dir")
	if packageRefIsSkillDir(skillDir, rawWD) {
		return "", ""
	}
	wd := normalizePackageRelativePath(rawWD)
	if wd == "" {
		return "", ""
	}
	if strings.HasPrefix(wd, "../") || packagePathIsAbs(wd) {
		return "", wd
	}
	return wd, ""
}

func packageLocalRefCandidates(ref, workingDir string) []string {
	ref = normalizePackageRelativePath(ref)
	if ref == "" || packagePathIsAbs(ref) || strings.HasPrefix(ref, "../") {
		return nil
	}
	candidates := []string{ref}
	if workingDir != "" && ref != workingDir && !strings.HasPrefix(ref, workingDir+"/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Join(filepath.FromSlash(workingDir), filepath.FromSlash(ref))))
	}
	return candidates
}

func packageLocalPathRefsFromParams(params map[string]interface{}) []string {
	if len(params) == 0 {
		return nil
	}
	var refs []string
	for key, raw := range params {
		if packageOutputPathKey(key) || !packagePathKey(key) {
			continue
		}
		refs = append(refs, packageLocalPathRefsFromPathValue(raw)...)
	}
	return refs
}

func packageLocalPathRefsFromStringMap(params map[string]string) []string {
	if len(params) == 0 {
		return nil
	}
	var refs []string
	for key, value := range params {
		if packageOutputPathKey(key) || !packagePathKey(key) {
			continue
		}
		refs = append(refs, packageLocalPathRefsFromPathValue(value)...)
	}
	return refs
}

func packagePathKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "file", "path", "filename", "filepath", "dir", "directory", "folder":
		return true
	default:
		return strings.HasSuffix(key, "_path") ||
			strings.HasSuffix(key, "_file") ||
			strings.HasSuffix(key, "_files") ||
			strings.HasSuffix(key, "_script") ||
			strings.HasSuffix(key, "_dir") ||
			strings.HasSuffix(key, "_dirs") ||
			strings.HasSuffix(key, "_directory") ||
			strings.HasSuffix(key, "_directories") ||
			strings.HasSuffix(key, "_folder") ||
			strings.HasSuffix(key, "_folders")
	}
}

func packageOutputPathKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "output", "output_file", "output_path", "outfile", "out_file", "out_path",
		"output_dir", "output_directory", "output_folder",
		"outfile_dir", "out_dir", "out_directory", "out_folder",
		"dest", "destination", "target", "target_file", "target_dir", "target_directory", "target_folder",
		"result", "result_file", "result_dir", "result_directory", "result_folder":
		return true
	default:
		return strings.HasPrefix(key, "output_") ||
			strings.HasPrefix(key, "out_") ||
			strings.HasPrefix(key, "result_") ||
			strings.HasSuffix(key, "_output") ||
			strings.HasSuffix(key, "_outfile") ||
			strings.HasSuffix(key, "_output_dir") ||
			strings.HasSuffix(key, "_output_directory") ||
			strings.HasSuffix(key, "_output_folder")
	}
}

func packageLocalPathRefsFromValue(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		return packageLocalPathRefFromString(v)
	case []string:
		var refs []string
		for _, item := range v {
			refs = append(refs, packageLocalPathRefFromString(item)...)
		}
		return refs
	case []interface{}:
		var refs []string
		for _, item := range v {
			refs = append(refs, packageLocalPathRefsFromValue(item)...)
		}
		return refs
	case map[string]interface{}:
		return packageLocalPathRefsFromParams(v)
	case map[interface{}]interface{}:
		return packageLocalPathRefsFromParams(packageInterfaceKeyMapToStringMap(v))
	case map[string]string:
		return packageLocalPathRefsFromStringMap(v)
	default:
		return nil
	}
}

func packageLocalPathRefsFromPathValue(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		return packagePathRefFromString(v)
	case []string:
		var refs []string
		for _, item := range v {
			refs = append(refs, packagePathRefFromString(item)...)
		}
		return refs
	case []interface{}:
		var refs []string
		for _, item := range v {
			refs = append(refs, packageLocalPathRefsFromPathValue(item)...)
		}
		return refs
	case map[string]interface{}:
		return packageLocalPathRefsFromParams(v)
	case map[interface{}]interface{}:
		return packageLocalPathRefsFromParams(packageInterfaceKeyMapToStringMap(v))
	case map[string]string:
		return packageLocalPathRefsFromStringMap(v)
	default:
		return nil
	}
}

func packageLocalPathRefFromString(value string) []string {
	if packageRefShouldSkipRawString(value) {
		return nil
	}
	ref := normalizePackageRelativePath(value)
	if ref == "" {
		return nil
	}
	if packagePathIsAbs(ref) || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "./") || strings.Contains(ref, "/") || looksLikePackageScriptFile(ref) {
		return []string{ref}
	}
	return nil
}

func packagePathRefFromString(value string) []string {
	if packageRefShouldSkipRawString(value) {
		return nil
	}
	ref := normalizePackageRelativePath(value)
	if ref == "" || packageRefShouldSkipNormalizedPath(ref) {
		return nil
	}
	return []string{ref}
}

func packageRefShouldSkipRawString(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if _, ok := stripPackageBaseDirRef(trimPackageRefDecorations(value)); ok {
		return false
	}
	return packageRefShouldSkipNormalizedPath(value)
}

func packageRefShouldSkipNormalizedPath(value string) bool {
	return containsUnresolvedRunPlaceholder(value) || isRemoteURL(value) || strings.HasPrefix(value, "$") || strings.HasPrefix(value, "%")
}

func normalizePackageRelativePath(value string) string {
	value = strings.TrimSpace(value)
	value = trimPackageRefDecorations(value)
	value = strings.TrimPrefix(value, "./")
	if stripped, ok := stripPackageBaseDirRef(value); ok {
		value = stripped
	}
	if value == "" || value == "." {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func stripPackageBaseDirRef(value string) (string, bool) {
	for _, marker := range []string{"{baseDir}", "$BASE_DIR", "${BASE_DIR}"} {
		if value == marker {
			return "", true
		}
		if strings.HasPrefix(value, marker+"/") {
			return strings.TrimPrefix(value, marker+"/"), true
		}
		if strings.HasPrefix(value, marker+"\\") {
			return strings.TrimPrefix(value, marker+"\\"), true
		}
	}
	return value, false
}

func packagePathIsAbs(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	slash := filepath.ToSlash(value)
	if strings.HasPrefix(slash, "/") {
		return true
	}
	if len(slash) >= 2 && slash[1] == ':' {
		drive := slash[0]
		return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
	}
	return false
}

func firstPackageString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s := packageStringParam(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func packageStringParam(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]interface{}:
		if len(v) == 1 {
			if raw, ok := v["baseDir"]; ok && raw == nil {
				return "{baseDir}"
			}
		}
	case map[interface{}]interface{}:
		if len(v) == 1 {
			for key, raw := range v {
				if fmt.Sprintf("%v", key) == "baseDir" && raw == nil {
					return "{baseDir}"
				}
			}
		}
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", value))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func packageInterfaceKeyMapToStringMap(m map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for key, value := range m {
		name := strings.TrimSpace(fmt.Sprintf("%v", key))
		if name == "" || name == "<nil>" {
			continue
		}
		out[name] = value
	}
	return out
}

func trimPackageRefDecorations(value string) string {
	return strings.Trim(value, "\"'`;,()[]")
}

func looksLikePackageScriptFile(path string) bool {
	return runnerFileReferenceExts[strings.ToLower(filepath.Ext(path))]
}
