package skill

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// inferScriptFileRequirements scans Python script files referenced by bash
// steps and extracts import-based pip requirements. This bridges the gap
// between declared dependencies (requires.python) and actual runtime imports.
//
// Design:
//   - Only scans .py files referenced in bash step commands
//   - Uses extractPythonRequires (same logic as craft_tool script analysis)
//   - Deduplicates against already-declared RequiresPython packages
//   - Returns requirements with Source="inferred_script" so they can be
//     distinguished from explicit declarations and promoted/demoted as needed
//   - Tolerates missing/unreadable files gracefully (log + skip)
//   - Caps total scan at 10 files to avoid I/O storms on large skill bundles
func inferScriptFileRequirements(skill *corelib.NLSkillEntry, skillDir string, cc *CheckContext) []Requirement {
	if skill == nil || skillDir == "" {
		return nil
	}
	// If the skill directory doesn't exist on disk, can't scan files.
	if _, err := os.Stat(skillDir); err != nil {
		return nil
	}

	// Build set of already-declared pip packages (avoid duplicates).
	declared := make(map[string]bool, len(skill.RequiresPython))
	for _, pkg := range skill.RequiresPython {
		name, _ := splitPkgVersion(pkg)
		declared[strings.ToLower(name)] = true
	}

	// Collect Python script paths from bash step commands.
	scriptPaths := extractPythonScriptPaths(skill, skillDir)
	if len(scriptPaths) == 0 {
		return nil
	}

	// Cap scan to avoid I/O storms.
	const maxScriptScan = 10
	if len(scriptPaths) > maxScriptScan {
		scriptPaths = scriptPaths[:maxScriptScan]
	}

	// Build Context for inferred requirements — ensures PipFixer installs into
	// the same Python environment as the pre-execution explicit requirements.
	var reqContext map[string]string
	if cc != nil && cc.PythonPath != "" {
		reqContext = map[string]string{"python_path": cc.PythonPath}
	}

	// Scan each script file for imports.
	seen := make(map[string]bool)
	var reqs []Requirement
	for _, path := range scriptPaths {
		var content []byte
		if strings.HasPrefix(path, inlinePythonPrefix) {
			// Virtual inline script — the command string IS the content.
			content = []byte(strings.TrimPrefix(path, inlinePythonPrefix))
		} else {
			var err error
			content, err = os.ReadFile(path)
			if err != nil {
				log.Printf("[requirement-infer] cannot read script %s: %v", path, err)
				continue
			}
			// Skip files that are too large (likely not a simple script).
			if len(content) > 512*1024 { // 512KB
				continue
			}
		}
		pkgs := extractPythonRequiresAggressive(string(content))
		for _, pkg := range pkgs {
			lower := strings.ToLower(pkg)
			if declared[lower] || seen[lower] {
				continue
			}
			// Skip modules that exist as local files in the skill directory.
			// This prevents false positives where "import utils" refers to a
			// skill-local utils.py, not a PyPI package named "utils".
			if isLocalPythonModule(pkg, skillDir) {
				continue
			}
			seen[lower] = true
			reqs = append(reqs, Requirement{
				Type:    "pip",
				Name:    pkg,
				Source:  "inferred_script",
				Context: reqContext,
			})
		}
	}

	if len(reqs) > 0 {
		names := make([]string, len(reqs))
		for i, r := range reqs {
			names[i] = r.Name
		}
		log.Printf("[requirement-infer] skill=%q inferred %d pip packages from script files: %v", skill.Name, len(reqs), names)
	}
	return reqs
}

// extractPythonScriptPaths extracts absolute paths of .py files referenced
// in bash step commands. Resolves {baseDir} and relative paths against skillDir.
// Also detects inline Python code (python -c "...") and returns a sentinel
// empty path with the inline code stored separately.
func extractPythonScriptPaths(skill *corelib.NLSkillEntry, skillDir string) []string {
	seen := make(map[string]bool)
	var paths []string

	for _, step := range skill.Steps {
		step = NormalizeStepForRunnerCopy(step, skillDir)
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}

		// Case 1: Inline python -c "..." or python3 << 'EOF' ... EOF
		// Scan the command string itself for import statements.
		if looksLikePythonInlineCommand(cmd) {
			// Use the command string as a virtual "script file".
			// The caller reads the content — for inline scripts we store the
			// command itself as the path with an inlinePythonPrefix marker.
			key := inlinePythonPrefix + cmd
			if !seen[key] {
				seen[key] = true
				paths = append(paths, key)
			}
		}

		// Case 2: External .py file references.
		refs, _ := commandFileReferencesForPrecheck(skill.Name, 0, cmd, skillDir, "")
		for _, ref := range refs {
			path := resolveScriptPath(ref.Path, ref.BaseDir, skillDir)
			if path == "" {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(path), ".py") {
				continue
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// inlinePythonPrefix is a sentinel prefix for inline Python code stored as
// "virtual script paths" in the paths list.
const inlinePythonPrefix = "\x00inline:"

// looksLikePythonInlineCommand detects commands that contain inline Python code.
// Matches patterns like: python -c "...", python3 -c '...', python << 'EOF'
func looksLikePythonInlineCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	// python -c / python3 -c
	if strings.Contains(lower, "python") && strings.Contains(lower, " -c ") {
		return true
	}
	// heredoc patterns with python
	if strings.Contains(lower, "python") && (strings.Contains(cmd, "<<") || strings.Contains(cmd, "<<-")) {
		return true
	}
	// Multi-line command that starts with python and has import statements
	if strings.Contains(lower, "python") && strings.Contains(lower, "\nimport ") {
		return true
	}
	if strings.Contains(lower, "python") && strings.Contains(lower, "\nfrom ") {
		return true
	}
	return false
}

// isLocalPythonModule returns true if the module name corresponds to a local
// Python file or package in the skill directory. This prevents false positives
// where "import utils" refers to a skill-local utils.py rather than a PyPI
// package named "utils".
func isLocalPythonModule(moduleName, skillDir string) bool {
	if skillDir == "" || moduleName == "" {
		return false
	}
	// Check for skillDir/moduleName.py
	if _, err := os.Stat(filepath.Join(skillDir, moduleName+".py")); err == nil {
		return true
	}
	// Check for skillDir/moduleName/ (package directory)
	info, err := os.Stat(filepath.Join(skillDir, moduleName))
	if err == nil && info.IsDir() {
		return true
	}
	// Check for skillDir/scripts/moduleName.py (common sub-directory pattern)
	if _, err := os.Stat(filepath.Join(skillDir, "scripts", moduleName+".py")); err == nil {
		return true
	}
	return false
}

// resolveScriptPath resolves a script reference to an absolute path.
func resolveScriptPath(ref, baseDir, skillDir string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	// Expand {baseDir} placeholder.
	ref = strings.ReplaceAll(ref, "{baseDir}", skillDir)
	ref = strings.ReplaceAll(ref, "${baseDir}", skillDir)

	// If already absolute, use as-is.
	if filepath.IsAbs(ref) {
		return filepath.Clean(ref)
	}
	// Resolve relative to baseDir (which may differ from skillDir for cd-ed commands).
	if baseDir != "" {
		candidate := filepath.Join(baseDir, ref)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate)
		}
	}
	// Resolve relative to skillDir.
	candidate := filepath.Join(skillDir, ref)
	if _, err := os.Stat(candidate); err == nil {
		return filepath.Clean(candidate)
	}
	return ""
}

// extractPythonRequiresAggressive is like extractPythonRequires but also
// includes packages that are not in pythonImportToPackage or pythonCommonThirdParty.
// For known mappings (cv2→opencv-python, etc.) it uses the pip name.
// For unknown packages, it uses the import name directly as the pip name
// (works for the majority of PyPI packages where import name == pip name).
//
// This is more aggressive than extractPythonRequires (which is conservative to
// avoid treating local helper files as pip packages). The aggressiveness is
// acceptable here because:
//  1. Failed pip installs are harmless (PipFixer catches the error gracefully)
//  2. A false positive (try to install local module name) fails fast with
//     "ERROR: No matching distribution found" and is skipped
//  3. A false negative (skip a real dependency) causes a runtime failure that
//     is much more expensive for the user
func extractPythonRequiresAggressive(script string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(raw string) {
		top := pythonTopLevelImportName(raw)
		if top == "" {
			return
		}
		// Skip stdlib modules.
		if pythonStdlibModules[top] {
			return
		}
		// Skip relative imports (leading dot already handled by pythonTopLevelImportName).
		if strings.HasPrefix(top, ".") {
			return
		}
		// Skip names that look like local modules (single underscore prefix is
		// a convention for package-internal modules; double underscore is dunder).
		if strings.HasPrefix(top, "_") {
			return
		}

		// Use known mapping if available.
		var pkg string
		if mapped, ok := pythonImportToPackage[top]; ok {
			pkg = mapped
		} else if pythonCommonThirdParty[top] {
			pkg = top
		} else {
			// Unknown → use import name as pip name (works for majority of PyPI).
			pkg = top
		}
		if pkg == "" || seen[strings.ToLower(pkg)] {
			return
		}
		seen[strings.ToLower(pkg)] = true
		result = append(result, pkg)
	}
	for _, line := range strings.Split(script, "\n") {
		if m := pythonImportLineRe.FindStringSubmatch(line); len(m) > 1 {
			for _, raw := range splitCSV(stripPythonLineComment(m[1])) {
				add(raw)
			}
			continue
		}
		if m := pythonFromImportRe.FindStringSubmatch(line); len(m) > 1 {
			add(m[1])
		}
	}
	return result
}
