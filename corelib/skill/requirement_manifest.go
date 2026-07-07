package skill

// requirement_manifest.go discovers and parses standard dependency manifest files
// (requirements.txt, pyproject.toml, package.json) in the skill directory.
//
// Design:
//   - Supplements explicit requires.python/requires.node declarations
//   - Deduplicates against already-declared packages
//   - Returns requirements with Source="manifest_file" for distinguishability
//   - Caps at 50 packages per manifest to avoid I/O storms on monorepo-sized projects
//   - Tolerates missing/malformed files gracefully (log + skip)
//
// This addresses the primary gap for GitHub-sourced skills: they typically have
// requirements.txt or package.json but do NOT declare requires.python/requires.node
// in skill.yaml, causing all dependencies to be missed by Layer 1 pre-checks.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	manifestSourceTag    = "manifest_file"
	maxManifestPackages  = 50
	maxManifestFileBytes = 256 * 1024 // 256KB — larger files are likely not simple dependency lists
)

// inferManifestFileRequirements scans the skill directory for standard dependency
// manifest files and extracts pip/npm requirements from them.
//
// Priority order: requirements.txt > pyproject.toml (for pip), package.json (for npm).
// Results are deduplicated against already-declared packages in the skill entry.
func inferManifestFileRequirements(skill *corelib.NLSkillEntry, skillDir string, cc *CheckContext) []Requirement {
	if skill == nil || skillDir == "" {
		return nil
	}
	if _, err := os.Stat(skillDir); err != nil {
		return nil
	}

	// Build set of already-declared packages to avoid duplicates.
	declaredPip := make(map[string]bool, len(skill.RequiresPython))
	for _, pkg := range skill.RequiresPython {
		name, _ := splitPkgVersion(pkg)
		declaredPip[strings.ToLower(name)] = true
	}
	declaredNpm := make(map[string]bool, len(skill.RequiresNode))
	for _, pkg := range skill.RequiresNode {
		name, _ := splitPkgVersion(pkg)
		declaredNpm[strings.ToLower(name)] = true
	}

	// Build Context for requirements — ensures PipFixer/NpmFixer install into
	// the correct environment.
	var pipContext map[string]string
	if cc != nil && cc.PythonPath != "" {
		pipContext = map[string]string{"python_path": cc.PythonPath}
	}
	npmContext := map[string]string{}
	if skillDir != "" {
		npmContext["skill_dir"] = skillDir
	}

	var reqs []Requirement

	// --- Python: requirements.txt ---
	pipReqs := parseRequirementsTxt(filepath.Join(skillDir, "requirements.txt"), skillDir)
	for _, pkg := range pipReqs {
		name, version := splitPkgVersion(pkg)
		if declaredPip[strings.ToLower(name)] {
			continue
		}
		declaredPip[strings.ToLower(name)] = true
		req := Requirement{Type: "pip", Name: name, Version: version, Source: manifestSourceTag}
		if pipContext != nil {
			req.Context = cloneStringMap(pipContext)
		}
		reqs = append(reqs, req)
		if len(reqs) >= maxManifestPackages {
			break
		}
	}

	// --- Python: pyproject.toml [project.dependencies] ---
	if len(reqs) < maxManifestPackages {
		pyprojectReqs := parsePyprojectDependencies(filepath.Join(skillDir, "pyproject.toml"))
		for _, pkg := range pyprojectReqs {
			name, version := splitPkgVersion(pkg)
			if declaredPip[strings.ToLower(name)] {
				continue
			}
			declaredPip[strings.ToLower(name)] = true
			req := Requirement{Type: "pip", Name: name, Version: version, Source: manifestSourceTag}
			if pipContext != nil {
				req.Context = cloneStringMap(pipContext)
			}
			reqs = append(reqs, req)
			if len(reqs) >= maxManifestPackages {
				break
			}
		}
	}

	// --- Node.js: package.json ---
	npmReqs := parsePackageJsonDeps(filepath.Join(skillDir, "package.json"))
	npmCount := 0
	for _, pkg := range npmReqs {
		name, version := splitPkgVersion(pkg)
		if declaredNpm[strings.ToLower(name)] {
			continue
		}
		declaredNpm[strings.ToLower(name)] = true
		req := Requirement{Type: "npm", Name: name, Version: version, Source: manifestSourceTag}
		if len(npmContext) > 0 {
			req.Context = cloneStringMap(npmContext)
		}
		reqs = append(reqs, req)
		npmCount++
		if npmCount >= maxManifestPackages {
			break
		}
	}

	if len(reqs) > 0 {
		pipCount := 0
		for _, r := range reqs {
			if r.Type == "pip" {
				pipCount++
			}
		}
		log.Printf("[requirement-manifest] skill=%q found %d pip + %d npm packages from manifest files",
			skill.Name, pipCount, npmCount)
	}
	return reqs
}

// parseRequirementsTxt parses a pip requirements.txt file.
// Supports:
//   - Package specs: "requests>=2.28", "flask==2.3.0", "numpy"
//   - Comments: lines starting with #
//   - Blank lines
//   - -r references (resolved relative to baseDir, max 1 level deep)
//   - Skips options lines (-i, --index-url, -f, --find-links, etc.)
//   - Skips editable installs (-e)
//   - Skips local path references (./foo, /path/to/foo)
func parseRequirementsTxt(path, baseDir string) []string {
	data, err := readManifestFile(path)
	if err != nil {
		return nil
	}
	return parseRequirementsLines(string(data), baseDir, 0)
}

func parseRequirementsLines(content, baseDir string, depth int) []string {
	if depth > 1 {
		return nil // prevent infinite recursion
	}
	var pkgs []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Handle inline comments
		if idx := strings.Index(line, " #"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		// Skip options
		if strings.HasPrefix(line, "-") {
			// Handle -r (recursive include)
			if strings.HasPrefix(line, "-r ") || strings.HasPrefix(line, "-r\t") {
				ref := strings.TrimSpace(line[3:])
				if ref != "" && baseDir != "" {
					refPath := filepath.Join(baseDir, ref)
					subData, err := readManifestFile(refPath)
					if err == nil {
						subPkgs := parseRequirementsLines(string(subData), filepath.Dir(refPath), depth+1)
						pkgs = append(pkgs, subPkgs...)
					}
				}
			}
			// Skip all other options (-i, -e, --index-url, etc.)
			continue
		}
		// Skip local path references
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "/") || strings.HasPrefix(line, "\\") {
			continue
		}
		// Skip URLs (git+https://, https://)
		if strings.Contains(line, "://") {
			continue
		}
		// Skip environment markers after semicolons for extraction but keep the package
		if idx := strings.Index(line, ";"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		// Skip extras bracket notation for name extraction: "package[extra1,extra2]>=1.0"
		pkg := normalizeRequirementLine(line)
		if pkg != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// normalizeRequirementLine extracts the package name + version spec from a
// requirements.txt line. Handles extras notation: "requests[security]>=2.28" → "requests>=2.28"
func normalizeRequirementLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	// Remove extras: "pkg[extra1,extra2]>=1.0" → "pkg>=1.0"
	if bracketIdx := strings.Index(line, "["); bracketIdx > 0 {
		closeBracket := strings.Index(line, "]")
		if closeBracket > bracketIdx {
			line = line[:bracketIdx] + line[closeBracket+1:]
		}
	}
	// Validate: first char should be a letter or digit (package names start with alphanumeric)
	if len(line) == 0 {
		return ""
	}
	first := line[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')) {
		return ""
	}
	return line
}

// parsePyprojectDependencies parses [project.dependencies] from pyproject.toml.
// Uses a simple line-based parser — avoids importing a full TOML library.
// Only extracts the dependencies array that appears under a [project] section.
func parsePyprojectDependencies(path string) []string {
	data, err := readManifestFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	// Two-pass: first find [project] section, then find dependencies within it.
	var inProjectSection bool
	var inDeps bool
	var pkgs []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track TOML sections
		if strings.HasPrefix(trimmed, "[") {
			// New section header — are we entering [project] or leaving it?
			sectionName := strings.TrimSpace(strings.Trim(trimmed, "[]"))
			if sectionName == "project" {
				inProjectSection = true
			} else {
				// Any other section ends [project]
				if inProjectSection && !inDeps {
					// We were in [project] but didn't find dependencies — done
					return nil
				}
				if inDeps {
					// We were reading dependencies when a new section started — done
					break
				}
				inProjectSection = false
			}
			continue
		}

		if !inProjectSection && !inDeps {
			continue
		}

		if !inDeps {
			// Inside [project], look for `dependencies = [`
			if strings.HasPrefix(trimmed, "dependencies") && strings.Contains(trimmed, "=") {
				if strings.Contains(trimmed, "[") {
					inDeps = true
					if strings.Contains(trimmed, "]") {
						// Single-line: dependencies = ["requests>=2.28", "flask"]
						arrayPart := trimmed[strings.Index(trimmed, "["):]
						pkgs = append(pkgs, parsePyprojectArray(arrayPart)...)
						return pkgs
					}
				}
			}
			continue
		}

		// Inside multi-line array
		if strings.Contains(trimmed, "]") {
			if before := strings.TrimSpace(strings.Split(trimmed, "]")[0]); before != "" {
				pkgs = append(pkgs, parsePyprojectArrayItem(before)...)
			}
			break
		}
		pkgs = append(pkgs, parsePyprojectArrayItem(trimmed)...)
	}
	return pkgs
}

// parsePyprojectArray parses a single-line TOML array like: ["requests>=2.28", "flask"]
func parsePyprojectArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSuffix(s, ",")
	var pkgs []string
	for _, item := range strings.Split(s, ",") {
		if pkg := cleanPyprojectItem(item); pkg != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// parsePyprojectArrayItem parses a single line inside a multi-line TOML array.
func parsePyprojectArrayItem(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(line, ",")
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	if pkg := cleanPyprojectItem(line); pkg != "" {
		return []string{pkg}
	}
	return nil
}

// cleanPyprojectItem strips quotes and whitespace from a TOML array item.
func cleanPyprojectItem(item string) string {
	item = strings.TrimSpace(item)
	item = strings.Trim(item, `"'`)
	item = strings.TrimSpace(item)
	if item == "" {
		return ""
	}
	// Remove environment markers: "numpy>=1.21; python_version>='3.8'" → "numpy>=1.21"
	if idx := strings.Index(item, ";"); idx > 0 {
		item = strings.TrimSpace(item[:idx])
	}
	// Remove extras
	if bracketIdx := strings.Index(item, "["); bracketIdx > 0 {
		closeBracket := strings.Index(item, "]")
		if closeBracket > bracketIdx {
			item = item[:bracketIdx] + item[closeBracket+1:]
		}
	}
	return normalizeRequirementLine(item)
}

// parsePackageJsonDeps parses dependencies from package.json.
// Extracts both "dependencies" and optionally "devDependencies" (since skill
// scripts often import dev tools at runtime).
//
// Returns package names WITHOUT version specifiers. npm's NpmChecker needs
// bare names for `npm list {name}`, and NpmFixer's batch `npm install` uses
// package.json directly (not individual specs). Version constraints are
// already encoded in package.json and respected by `npm install`.
func parsePackageJsonDeps(path string) []string {
	data, err := readManifestFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		log.Printf("[requirement-manifest] cannot parse package.json: %v", err)
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	// Production dependencies first
	for name := range pkg.Dependencies {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		result = append(result, name)
	}
	// Dev dependencies (many skill scripts use dev tools like typescript, tsx, etc.)
	for name := range pkg.DevDependencies {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		result = append(result, name)
	}
	return result
}

// --- Batch Install Support ---

// HasRequirementsTxt returns true if the skill directory contains a requirements.txt file.
// Used by the batch install optimization in FixAllWithProgress.
func HasRequirementsTxt(skillDir string) bool {
	if skillDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(skillDir, "requirements.txt"))
	return err == nil
}

// HasPackageJson returns true if the skill directory contains a package.json file.
func HasPackageJson(skillDir string) bool {
	if skillDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(skillDir, "package.json"))
	return err == nil
}

// NpmNodeModulesStale returns true if node_modules needs (re)installation:
//   - node_modules doesn't exist, OR
//   - package.json is newer than node_modules/.package-lock.json
func NpmNodeModulesStale(skillDir string) bool {
	if skillDir == "" {
		return false
	}
	packageJsonPath := filepath.Join(skillDir, "package.json")
	nodeModulesLock := filepath.Join(skillDir, "node_modules", ".package-lock.json")

	pjInfo, err := os.Stat(packageJsonPath)
	if err != nil {
		return false // no package.json → not stale (nothing to install)
	}
	lockInfo, err := os.Stat(nodeModulesLock)
	if err != nil {
		return true // node_modules doesn't exist or no lock → stale
	}
	return pjInfo.ModTime().After(lockInfo.ModTime())
}

// --- Helpers ---

func readManifestFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxManifestFileBytes {
		log.Printf("[requirement-manifest] skipping oversized manifest %s (%d bytes)", path, info.Size())
		return nil, os.ErrInvalid
	}
	return os.ReadFile(path)
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
