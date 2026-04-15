package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Regex patterns for path and platform detection
// ---------------------------------------------------------------------------

// unixAbsPathRe matches Unix absolute paths containing common top-level dirs.
var unixAbsPathRe = regexp.MustCompile(`/(home|usr|opt|tmp|var|etc)/`)

// macosUsersPathRe matches macOS /Users/ paths.
var macosUsersPathRe = regexp.MustCompile(`/Users/`)

// windowsDrivePathRe matches Windows drive-letter paths like C:\ or D:\.
var windowsDrivePathRe = regexp.MustCompile(`[A-Za-z]:\\`)

// systemBinaryPrefixes are path prefixes for well-known system binaries
// that should NOT be flagged as hardcoded paths.
var systemBinaryPrefixes = []string{
	"/usr/bin/",
	"/usr/local/bin/",
	"/usr/sbin/",
	"/sbin/",
	"/bin/",
}

// backslashPathRe matches backslash-separated path patterns in commands,
// e.g. scripts\run.py, subdir\file.txt. It looks for word chars around \.
var backslashPathRe = regexp.MustCompile(`\w\\[A-Za-z_]`)

// shellEscapes are common shell escape sequences that should not be treated
// as path separators.
var shellEscapes = []string{`\n`, `\t`, `\"`, `\\`, `\$`, `\'`}

// windowsEnvVarRe matches Windows-style %VAR% environment variable references.
var windowsEnvVarRe = regexp.MustCompile(`%[A-Za-z_][A-Za-z0-9_]*%`)

// shebangRe matches shebang lines.
var shebangRe = regexp.MustCompile(`^#!\s*/`)

// posixOnlyCommands are commands that only work on POSIX systems.
var posixOnlyCommands = []string{"chmod", "chown", "ln -s", "grep -P"}

// windowsSpecificPatterns are syntax patterns specific to Windows/PowerShell.
var windowsSpecificPatterns = []string{"$env:", "[Console]::", ".ps1", "cmd.exe"}

// knownRuntimeCommands are common runtime commands that should be declared
// as dependencies.
var knownRuntimeCommands = []string{
	"python", "python3", "node", "npm", "npx",
	"pip", "pip3", "java", "go", "cargo", "ruby", "perl",
}

// commandSource pairs a bash command string with the file it came from.
type commandSource struct {
	command string
	file    string
}

// ---------------------------------------------------------------------------
// ValidateSkillPortability — main entry point
// ---------------------------------------------------------------------------

// ValidateSkillPortability scans a skill directory for portability issues.
// It reads skill.yaml and optionally SKILL.md, checking bash step commands
// for hardcoded paths, platform-specific syntax, missing metadata, and
// undeclared dependencies. Returns a PortabilityReport.
func ValidateSkillPortability(skillDir string) (*PortabilityReport, error) {
	// Check directory exists.
	info, err := os.Stat(skillDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("skill directory does not exist: %w", os.ErrNotExist)
		}
		return nil, fmt.Errorf("cannot access skill directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", skillDir)
	}

	// Try to parse skill.yaml.
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	var sf *SkillYAMLFile
	var yamlExists bool

	yamlData, err := os.ReadFile(yamlPath)
	if err == nil {
		yamlExists = true
		parsed, parseErr := ParseSkillYAMLFile(yamlData)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse skill.yaml: %w", parseErr)
		}
		sf = parsed
	}

	// Check for SKILL.md existence.
	mdPath, mdErr := findSkillMD(skillDir)
	mdExists := mdErr == nil

	// If neither skill.yaml nor SKILL.md exists, return error.
	if !yamlExists && !mdExists {
		return nil, fmt.Errorf("skill directory contains neither skill.yaml nor SKILL.md: %s", skillDir)
	}

	// If no skill.yaml, create a minimal struct for metadata checks.
	if sf == nil {
		sf = &SkillYAMLFile{}
	}

	var issues []PortabilityIssue

	// Collect bash commands from skill.yaml steps.
	var yamlCommands []commandSource
	for _, step := range sf.Steps {
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}
		yamlCommands = append(yamlCommands, commandSource{command: cmd, file: "skill.yaml"})
	}

	// --- Run checkers on skill.yaml commands ---

	// checkMissingBaseDir runs FIRST and returns matched paths to exclude.
	baseDirMatched := checkMissingBaseDir(&issues, yamlCommands, skillDir)

	// checkHardcodedPaths excludes paths already reported by checkMissingBaseDir.
	checkHardcodedPaths(&issues, yamlCommands, baseDirMatched)

	// Metadata checks (only meaningful if skill.yaml exists).
	if yamlExists {
		checkMetadata(&issues, sf)
	}

	// Path separator checks.
	checkPathSeparators(&issues, yamlCommands)

	// Platform compatibility checks.
	checkPlatformCompat(&issues, yamlCommands, sf)

	// Shell mismatch checks.
	checkShellMismatch(&issues, sf)

	// Dependency checks.
	checkDependencies(&issues, yamlCommands, sf)

	// --- SKILL.md validation (task 2.9) ---
	if mdExists {
		validateSkillMD(&issues, mdPath, skillDir, sf)
	}

	skillName := sf.Name
	if skillName == "" {
		skillName = filepath.Base(skillDir)
	}

	return NewPortabilityReport(skillName, skillDir, issues), nil
}

// ---------------------------------------------------------------------------
// findSkillMD locates SKILL.md or skill.md in the skill directory.
// ---------------------------------------------------------------------------

func findSkillMD(skillDir string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(skillDir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no SKILL.md found")
}

// ---------------------------------------------------------------------------
// checkMissingBaseDir — detect absolute paths pointing inside the skill dir
// ---------------------------------------------------------------------------

func checkMissingBaseDir(issues *[]PortabilityIssue, commands []commandSource, skillDir string) map[string]bool {
	matched := make(map[string]bool)
	if skillDir == "" {
		return matched
	}

	// Normalize skill dir to forward slashes for comparison.
	normalizedDir := filepath.ToSlash(filepath.Clean(skillDir))
	// Ensure it doesn't end with a slash for prefix matching.
	normalizedDir = strings.TrimRight(normalizedDir, "/")

	for _, cs := range commands {
		paths := extractAbsolutePaths(cs.command)
		for _, p := range paths {
			normalizedPath := filepath.ToSlash(p)
			if strings.HasPrefix(normalizedPath, normalizedDir+"/") || normalizedPath == normalizedDir {
				// This path points inside the skill directory.
				relPath := strings.TrimPrefix(normalizedPath, normalizedDir+"/")
				if relPath == "" {
					relPath = "."
				}
				matched[p] = true
				*issues = append(*issues, PortabilityIssue{
					Severity:   SeverityError,
					Category:   "missing_basedir",
					Message:    fmt.Sprintf("Command contains absolute path %q pointing inside the skill directory", p),
					File:       cs.file,
					Suggestion: fmt.Sprintf("Use {baseDir}/%s instead", relPath),
				})
			}
		}
	}
	return matched
}

// ---------------------------------------------------------------------------
// checkHardcodedPaths — detect absolute paths in bash commands
// ---------------------------------------------------------------------------

func checkHardcodedPaths(issues *[]PortabilityIssue, commands []commandSource, excludePaths map[string]bool) {
	for _, cs := range commands {
		paths := extractAbsolutePaths(cs.command)
		for _, p := range paths {
			// Skip paths already reported by checkMissingBaseDir.
			if excludePaths[p] {
				continue
			}
			// Skip system binary paths.
			if isSystemBinaryPath(p) {
				continue
			}
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityError,
				Category:   "hardcoded_path",
				Message:    fmt.Sprintf("Command contains absolute path %q", p),
				File:       cs.file,
				Suggestion: "Replace with a relative path, environment variable, or {baseDir} placeholder",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// checkMetadata — check for missing/incomplete metadata
// ---------------------------------------------------------------------------

func checkMetadata(issues *[]PortabilityIssue, sf *SkillYAMLFile) {
	if len(sf.Platforms) == 0 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "missing_platforms",
			Message:    "No platforms declared in skill.yaml",
			File:       "skill.yaml",
			Suggestion: `Add platforms: ["universal"] or specify target platforms`,
		})
	}

	if len(sf.Description) < 10 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "incomplete_metadata",
			Message:    "Description is missing or too short (less than 10 characters)",
			File:       "skill.yaml",
			Suggestion: "Add a descriptive description of at least 10 characters",
		})
	}

	if len(sf.Triggers) == 0 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "incomplete_metadata",
			Message:    "No triggers declared in skill.yaml",
			File:       "skill.yaml",
			Suggestion: "Add at least one trigger keyword",
		})
	}
}

// ---------------------------------------------------------------------------
// checkPathSeparators — detect backslash path separators in bash commands
// ---------------------------------------------------------------------------

func checkPathSeparators(issues *[]PortabilityIssue, commands []commandSource) {
	for _, cs := range commands {
		if containsBackslashPath(cs.command) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "path_separator",
				Message:    "Command contains backslash path separators",
				File:       cs.file,
				Suggestion: "Use forward slashes (/) for cross-platform compatibility",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// checkPlatformCompat — detect platform-specific constructs
// ---------------------------------------------------------------------------

func checkPlatformCompat(issues *[]PortabilityIssue, commands []commandSource, sf *SkillYAMLFile) {
	platforms := sf.Platforms

	for _, cs := range commands {
		// python3 without fallback → info
		if strings.Contains(cs.command, "python3") {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityInfo,
				Category:   "platform_compat",
				Message:    "Command uses python3, which may not be available on Windows",
				File:       cs.file,
				Suggestion: "Consider using a python3/python conditional or document the requirement",
			})
		}

		// %VAR% Windows env vars → warning
		if windowsEnvVarRe.MatchString(cs.command) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "platform_compat",
				Message:    "Command uses Windows-style environment variable syntax (%VAR%)",
				File:       cs.file,
				Suggestion: "Use $VAR or ${VAR} for cross-platform compatibility",
			})
		}

		// Shebangs → info
		for _, line := range strings.Split(cs.command, "\n") {
			line = strings.TrimSpace(line)
			if shebangRe.MatchString(line) {
				*issues = append(*issues, PortabilityIssue{
					Severity:   SeverityInfo,
					Category:   "platform_compat",
					Message:    fmt.Sprintf("Command contains shebang %q", line),
					File:       cs.file,
					Suggestion: "Shebangs are ignored on Windows; ensure Git Bash or WSL is available",
				})
				break // Only report once per command
			}
		}

		// POSIX-only commands when targeting Windows/universal/empty platforms
		if platformIncludesWindows(platforms) {
			for _, posixCmd := range posixOnlyCommands {
				if containsCommand(cs.command, posixCmd) {
					*issues = append(*issues, PortabilityIssue{
						Severity:   SeverityWarning,
						Category:   "platform_compat",
						Message:    fmt.Sprintf("Command uses POSIX-only %q but skill targets Windows-compatible platforms", posixCmd),
						File:       cs.file,
						Suggestion: "Add a cross-platform alternative or restrict platforms to linux/macos",
					})
				}
			}
		}

		// Windows-specific syntax when targeting Linux/macOS/universal/empty platforms
		if platformIncludesUnix(platforms) {
			for _, winPattern := range windowsSpecificPatterns {
				if strings.Contains(cs.command, winPattern) {
					*issues = append(*issues, PortabilityIssue{
						Severity:   SeverityWarning,
						Category:   "platform_compat",
						Message:    fmt.Sprintf("Command uses Windows-specific syntax %q but skill targets Unix-compatible platforms", winPattern),
						File:       cs.file,
						Suggestion: "Add a cross-platform alternative or restrict platforms to windows",
					})
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// checkShellMismatch — detect preferred_shell conflicts with platforms
// ---------------------------------------------------------------------------

func checkShellMismatch(issues *[]PortabilityIssue, sf *SkillYAMLFile) {
	shell := strings.ToLower(strings.TrimSpace(sf.PreferredShell))
	if shell == "" {
		return
	}

	platforms := sf.Platforms

	// "cmd" or "powershell" with linux/macos platforms
	if shell == "cmd" || shell == "powershell" || shell == "pwsh" {
		for _, p := range platforms {
			p = strings.ToLower(p)
			if p == "linux" || p == "macos" {
				*issues = append(*issues, PortabilityIssue{
					Severity:   SeverityWarning,
					Category:   "shell_mismatch",
					Message:    fmt.Sprintf("Preferred shell %q is not available on platform %q", shell, p),
					File:       "skill.yaml",
					Suggestion: "Use bash for cross-platform compatibility or adjust platforms",
				})
			}
		}
	}

	// "bash" with windows-only platforms (less common but worth checking)
	if shell == "bash" {
		windowsOnly := true
		for _, p := range platforms {
			p = strings.ToLower(p)
			if p != "windows" {
				windowsOnly = false
				break
			}
		}
		if len(platforms) > 0 && windowsOnly {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "shell_mismatch",
				Message:    fmt.Sprintf("Preferred shell %q may not be available on Windows without Git Bash or WSL", shell),
				File:       "skill.yaml",
				Suggestion: "Consider using cmd or powershell for Windows-only skills",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// checkDependencies — detect undeclared runtime dependencies
// ---------------------------------------------------------------------------

func checkDependencies(issues *[]PortabilityIssue, commands []commandSource, sf *SkillYAMLFile) {
	// Build a combined text of required_env, description, and triggers for
	// case-insensitive substring matching.
	declaredText := strings.ToLower(sf.Description)
	for _, env := range sf.RequiredEnv {
		declaredText += " " + strings.ToLower(env)
	}
	for _, trigger := range sf.Triggers {
		declaredText += " " + strings.ToLower(trigger)
	}

	for _, cs := range commands {
		// Check for pip install / npm install → info "runtime_install"
		cmdLower := strings.ToLower(cs.command)
		if strings.Contains(cmdLower, "pip install") || strings.Contains(cmdLower, "npm install") {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityInfo,
				Category:   "runtime_install",
				Message:    "Command installs packages at runtime",
				File:       cs.file,
				Suggestion: "Marketplace skills should bundle dependencies or document installation requirements",
			})
		}

		// Extract first word of each line as the command name.
		for _, line := range strings.Split(cs.command, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			firstWord := extractFirstWord(line)
			if firstWord == "" {
				continue
			}
			firstWordLower := strings.ToLower(firstWord)

			// Check if it's a known runtime command.
			if !isKnownRuntimeCommand(firstWordLower) {
				continue
			}

			// Check if it's declared in required_env, description, or triggers.
			if strings.Contains(declaredText, firstWordLower) {
				continue
			}

			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "undeclared_dependency",
				Message:    fmt.Sprintf("Command uses %q which is not declared in required_env or description", firstWord),
				File:       cs.file,
				Suggestion: fmt.Sprintf("Add %q to required_env or mention it in the skill description", firstWord),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// validateSkillMD — extract bash blocks from SKILL.md and run checks
// ---------------------------------------------------------------------------

func validateSkillMD(issues *[]PortabilityIssue, mdPath, skillDir string, sf *SkillYAMLFile) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		log.Printf("[portability-validator] warning: cannot read SKILL.md: %v", err)
		return
	}

	blocks := extractAllBashBlocksFromMarkdown(string(data))
	if len(blocks) == 0 {
		return
	}

	// Build command sources from SKILL.md bash blocks.
	var mdCommands []commandSource
	for _, block := range blocks {
		mdCommands = append(mdCommands, commandSource{command: block, file: "SKILL.md"})
	}

	// Run path and platform checks on SKILL.md commands.
	baseDirMatched := checkMissingBaseDir(issues, mdCommands, skillDir)
	checkHardcodedPaths(issues, mdCommands, baseDirMatched)
	checkPathSeparators(issues, mdCommands)
	checkPlatformCompat(issues, mdCommands, sf)
	checkDependencies(issues, mdCommands, sf)
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// extractAbsolutePaths finds all absolute path strings in a command.
func extractAbsolutePaths(command string) []string {
	var paths []string
	seen := make(map[string]bool)

	// Find Unix absolute paths.
	for _, re := range []*regexp.Regexp{unixAbsPathRe, macosUsersPathRe} {
		for _, loc := range re.FindAllStringIndex(command, -1) {
			p := extractPathAtIndex(command, loc[0])
			if p != "" && !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}

	// Find Windows drive paths.
	for _, loc := range windowsDrivePathRe.FindAllStringIndex(command, -1) {
		p := extractWindowsPathAtIndex(command, loc[0])
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	return paths
}

// extractPathAtIndex extracts a Unix path starting from the last '/' before idx.
func extractPathAtIndex(s string, idx int) string {
	// Walk backwards to find the start of the path (first '/').
	start := idx
	for start > 0 && s[start-1] != ' ' && s[start-1] != '"' && s[start-1] != '\'' &&
		s[start-1] != '=' && s[start-1] != '(' && s[start-1] != '\n' && s[start-1] != '\t' {
		if s[start-1] == '/' {
			start--
			continue
		}
		start--
	}
	// Ensure we start at a '/'.
	for start < len(s) && s[start] != '/' {
		start++
	}
	if start >= len(s) {
		return ""
	}

	// Walk forward to find the end of the path.
	end := start
	for end < len(s) {
		c := s[end]
		if c == ' ' || c == '"' || c == '\'' || c == '\n' || c == '\t' ||
			c == ')' || c == ';' || c == '|' || c == '&' || c == '>' || c == '<' {
			break
		}
		end++
	}

	path := s[start:end]
	// Must start with / and have at least one segment.
	if !strings.HasPrefix(path, "/") || len(path) < 2 {
		return ""
	}
	return path
}

// extractWindowsPathAtIndex extracts a Windows path starting at the drive letter.
func extractWindowsPathAtIndex(s string, idx int) string {
	start := idx
	end := start
	for end < len(s) {
		c := s[end]
		if c == ' ' || c == '"' || c == '\'' || c == '\n' || c == '\t' ||
			c == ')' || c == ';' || c == '|' || c == '&' || c == '>' || c == '<' {
			break
		}
		end++
	}
	path := s[start:end]
	if len(path) < 3 {
		return ""
	}
	return path
}

// isSystemBinaryPath checks if a path is a well-known system binary path.
func isSystemBinaryPath(path string) bool {
	normalized := filepath.ToSlash(path)
	for _, prefix := range systemBinaryPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

// containsBackslashPath checks if a command contains backslash path separators
// that are not shell escape sequences.
func containsBackslashPath(command string) bool {
	if !strings.Contains(command, `\`) {
		return false
	}

	// Check each line for backslash patterns.
	for _, line := range strings.Split(command, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for word\word patterns that aren't escape sequences.
		locs := backslashPathRe.FindAllStringIndex(line, -1)
		for _, loc := range locs {
			// Extract the backslash and following char.
			bsIdx := loc[0]
			// Find the actual backslash position within the match.
			for i := loc[0]; i < loc[1]; i++ {
				if line[i] == '\\' {
					bsIdx = i
					break
				}
			}
			if bsIdx+1 >= len(line) {
				continue
			}

			// Check if this is a shell escape sequence.
			twoChar := line[bsIdx : bsIdx+2]
			isEscape := false
			for _, esc := range shellEscapes {
				if twoChar == esc {
					isEscape = true
					break
				}
			}
			if !isEscape {
				return true
			}
		}
	}
	return false
}

// platformIncludesWindows returns true if the platform list includes "windows",
// "universal", or is empty (meaning all platforms).
func platformIncludesWindows(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		p = strings.ToLower(p)
		if p == "windows" || p == "universal" {
			return true
		}
	}
	return false
}

// platformIncludesUnix returns true if the platform list includes "linux",
// "macos", "universal", or is empty.
func platformIncludesUnix(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		p = strings.ToLower(p)
		if p == "linux" || p == "macos" || p == "universal" {
			return true
		}
	}
	return false
}

// containsCommand checks if a command string contains a specific command,
// handling multi-word commands like "ln -s" and "grep -P".
func containsCommand(command, target string) bool {
	for _, line := range strings.Split(command, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// For multi-word targets like "ln -s", check substring.
		if strings.Contains(target, " ") {
			if strings.Contains(line, target) {
				return true
			}
			continue
		}
		// For single-word targets, check if it appears as a command word.
		firstWord := extractFirstWord(line)
		if strings.EqualFold(firstWord, target) {
			return true
		}
		// Also check if it appears after a pipe or semicolon.
		for _, segment := range splitCommandSegments(line) {
			fw := extractFirstWord(strings.TrimSpace(segment))
			if strings.EqualFold(fw, target) {
				return true
			}
		}
	}
	return false
}

// extractFirstWord returns the first whitespace-delimited word from a line.
func extractFirstWord(line string) string {
	line = strings.TrimSpace(line)
	// Skip environment variable assignments like VAR=value command.
	for strings.Contains(line, "=") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			break
		}
		first := parts[0]
		if strings.Contains(first, "=") && !strings.HasPrefix(first, "-") {
			line = strings.TrimSpace(parts[1])
			continue
		}
		break
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// splitCommandSegments splits a command line by pipe and semicolon operators.
func splitCommandSegments(line string) []string {
	var segments []string
	var current strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '|' || c == ';' {
			segments = append(segments, current.String())
			current.Reset()
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// isKnownRuntimeCommand checks if a command name is in the known runtime list.
func isKnownRuntimeCommand(cmd string) bool {
	for _, known := range knownRuntimeCommands {
		if cmd == known {
			return true
		}
	}
	return false
}
