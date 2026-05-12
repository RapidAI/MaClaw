package skill

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ---------------------------------------------------------------------------
// Regex patterns for path and platform detection
// ---------------------------------------------------------------------------

// unixAbsPathRe matches Unix absolute paths containing common top-level dirs.
var unixAbsPathRe = regexp.MustCompile(`/(home|usr|opt|tmp|var|etc)/`)

// macosUsersPathRe matches macOS /Users/ paths.
var macosUsersPathRe = regexp.MustCompile(`/Users/`)

// windowsDrivePathRe matches Windows drive-letter paths like C:\, D:\, or JSON-friendly C:/.
var windowsDrivePathRe = regexp.MustCompile(`[A-Za-z]:[\\/]`)

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

// knownRuntimeSet is a set version of knownRuntimeCommands for O(1) lookup.
var knownRuntimeSet = func() map[string]bool {
	m := make(map[string]bool, len(knownRuntimeCommands))
	for _, cmd := range knownRuntimeCommands {
		m[cmd] = true
	}
	return m
}()

var portabilityScriptExts = map[string]bool{
	".bat": true,
	".cjs": true,
	".cmd": true,
	".js":  true,
	".mjs": true,
	".pl":  true,
	".ps1": true,
	".py":  true,
	".rb":  true,
	".sh":  true,
}

// commandSource pairs a bash command string with the file it came from.
type commandSource struct {
	command string
	file    string
}

// ---------------------------------------------------------------------------
// ValidateSkillPortability 閳?main entry point
// ---------------------------------------------------------------------------

// ValidateSkillPortability scans a skill directory for portability issues.
// It reads skill.yaml/skill.yml and optional skill docs, checking bash step commands
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

	// Try to parse a structured skill definition.
	defPath, defFormat, defErr := skillDefinitionPath(skillDir)
	var sf *SkillYAMLFile
	var defExists bool
	defFile := "skill definition"

	if defErr == nil {
		defExists = true
		defFile = filepath.Base(defPath)
		defData, err := os.ReadFile(defPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", defFile, err)
		}
		parsed, parseErr := ParseSkillDefinitionFile(defData, defFormat)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", defFile, parseErr)
		}
		sf = parsed
	}

	// Check for SKILL.md existence.
	mdPath, mdErr := findSkillMD(skillDir)
	mdExists := mdErr == nil

	// If neither structured definition nor skill docs exist, return error.
	if !defExists && !mdExists {
		return nil, fmt.Errorf("skill directory contains no skill definition or documentation: %s", skillDir)
	}

	// If no structured definition exists, create a minimal struct for metadata checks.
	if sf == nil {
		sf = &SkillYAMLFile{}
	}

	var issues []PortabilityIssue

	// Collect executable commands from skill.yaml steps using the same
	// compatibility aliases as the runner. Downloaded skills often use action
	// names such as run/shell/python rather than native bash.
	yamlCommands := commandSourcesFromYAMLSteps(sf.Steps, defFile)

	// --- Run checkers on structured-definition commands ---

	// checkMissingBaseDir runs FIRST and returns matched paths to exclude.
	baseDirMatched := checkMissingBaseDir(&issues, yamlCommands, skillDir)

	// checkHardcodedPaths excludes paths already reported by checkMissingBaseDir.
	checkHardcodedPaths(&issues, yamlCommands, baseDirMatched)

	// Metadata checks (only meaningful if a structured definition exists).
	if defExists {
		checkMetadata(&issues, sf, defFile)
	}

	// Path separator checks.
	checkPathSeparators(&issues, yamlCommands)

	// Platform compatibility checks.
	checkPlatformCompat(&issues, yamlCommands, sf)

	// Shell mismatch checks.
	checkShellMismatch(&issues, sf, defFile)

	// Dependency checks.
	checkDependencies(&issues, yamlCommands, sf)

	// --- SKILL.md validation (task 2.9) ---
	if mdExists {
		validateSkillMD(&issues, mdPath, skillDir, sf)
	}
	checkScriptTextEncoding(&issues, skillDir)

	skillName := sf.Name
	if skillName == "" {
		skillName = filepath.Base(skillDir)
	}

	return NewPortabilityReport(skillName, skillDir, issues), nil
}

func commandSourcesFromYAMLSteps(steps []SkillYAMLStep, file string) []commandSource {
	commands := make([]commandSource, 0, len(steps))
	for _, step := range steps {
		if cmd, _, ok := portabilityStepCommand(step); ok {
			commands = append(commands, commandSource{command: cmd, file: file})
			continue
		}
		normalized := NormalizeStepForRunner(corelib.NLSkillStep{
			Action: step.Action,
			Params: copyStepParams(step.Params),
		}, "")
		if normalized.Action != "bash" && normalized.Action != "poll" {
			continue
		}
		cmd, _ := normalized.Params["command"].(string)
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		commands = append(commands, commandSource{command: cmd, file: file})
	}
	return commands
}

func portabilityStepCommand(step SkillYAMLStep) (string, string, bool) {
	if !isPortableCommandStep(step.Action) || step.Params == nil {
		return "", "", false
	}
	for _, key := range []string{"command", "cmd", "run", "script", "shell_command"} {
		raw, ok := step.Params[key]
		if !ok || raw == nil {
			continue
		}
		if command, ok := portabilityCommandValueString(raw); ok && strings.TrimSpace(command) != "" {
			return command, key, true
		}
	}
	return "", "", false
}

func portabilityCommandValueString(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	case []string:
		return strings.Join(nonEmptyStringItems(v), " "), true
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " "), true
	case map[string]interface{}:
		return portabilityCommandMapString(v)
	case map[string]string:
		converted := make(map[string]interface{}, len(v))
		for key, value := range v {
			converted[key] = value
		}
		return portabilityCommandMapString(converted)
	default:
		return "", false
	}
}

func portabilityCommandMapString(m map[string]interface{}) (string, bool) {
	program := firstStringParam(m, "program", "cmd", "command", "executable", "binary")
	if program == "" {
		return "", false
	}
	parts := []string{program}
	if args, ok := portabilityCommandValueString(firstNonNil(m["args"], m["argv"], m["arguments"])); ok && args != "" {
		parts = append(parts, args)
	}
	return strings.Join(parts, " "), true
}

func nonEmptyStringItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func copyStepParams(params map[string]interface{}) map[string]interface{} {
	if len(params) == 0 {
		return map[string]interface{}{}
	}
	copy := make(map[string]interface{}, len(params))
	for k, v := range params {
		copy[k] = v
	}
	return copy
}

// ---------------------------------------------------------------------------
// findSkillMD locates SKILL.md or skill.md in the skill directory.
// ---------------------------------------------------------------------------

func findSkillMD(skillDir string) (string, error) {
	mdPath, err := findSkillMarkdownDocPath(skillDir)
	if err != nil {
		return "", fmt.Errorf("no skill documentation found")
	}
	return mdPath, nil
}

// ---------------------------------------------------------------------------
// checkMissingBaseDir 閳?detect absolute paths pointing inside the skill dir
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
// checkHardcodedPaths 閳?detect absolute paths in bash commands
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
// checkMetadata 閳?check for missing/incomplete metadata
// ---------------------------------------------------------------------------

func checkMetadata(issues *[]PortabilityIssue, sf *SkillYAMLFile, file string) {
	if len(sf.Platforms) == 0 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "missing_platforms",
			Message:    fmt.Sprintf("No platforms declared in %s", file),
			File:       file,
			Suggestion: `Add platforms: ["universal"] or specify target platforms`,
		})
	}

	if len(sf.Description) < 10 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "incomplete_metadata",
			Message:    "Description is missing or too short (less than 10 characters)",
			File:       file,
			Suggestion: "Add a descriptive description of at least 10 characters",
		})
	}

	if len(sf.Triggers) == 0 {
		*issues = append(*issues, PortabilityIssue{
			Severity:   SeverityWarning,
			Category:   "incomplete_metadata",
			Message:    fmt.Sprintf("No triggers declared in %s", file),
			File:       file,
			Suggestion: "Add at least one trigger keyword",
		})
	}
}

// ---------------------------------------------------------------------------
// checkPathSeparators 閳?detect backslash path separators in bash commands
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
// checkPlatformCompat 閳?detect platform-specific constructs
// ---------------------------------------------------------------------------

func checkPlatformCompat(issues *[]PortabilityIssue, commands []commandSource, sf *SkillYAMLFile) {
	platforms := sf.Platforms

	for _, cs := range commands {
		// python3 without fallback 閳?info (only relevant when targeting non-Windows)
		if strings.Contains(cs.command, "python3") && platformIncludesWindows(platforms) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityInfo,
				Category:   "platform_compat",
				Message:    "Command uses python3, which may not be available on Windows",
				File:       cs.file,
				Suggestion: "Consider using a python3/python conditional or document the requirement",
			})
		}

		// %VAR% Windows env vars 閳?warning
		if windowsEnvVarRe.MatchString(cs.command) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "platform_compat",
				Message:    "Command uses Windows-style environment variable syntax (%VAR%)",
				File:       cs.file,
				Suggestion: "Use $VAR or ${VAR} for cross-platform compatibility",
			})
		}

		// Shebangs 閳?info
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
// checkShellMismatch 閳?detect preferred_shell conflicts with platforms
// ---------------------------------------------------------------------------

func checkShellMismatch(issues *[]PortabilityIssue, sf *SkillYAMLFile, file string) {
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
					File:       file,
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
				File:       file,
				Suggestion: "Consider using cmd or powershell for Windows-only skills",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// checkDependencies 閳?detect undeclared runtime dependencies
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
	if sf.Requires != nil {
		for _, bin := range sf.Requires.Bins {
			declaredText += " " + strings.ToLower(bin)
		}
	}

	for _, cs := range commands {
		// Check for pip install / npm install 閳?info "runtime_install"
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

		// Track which runtime commands we've already reported for this command source
		// to avoid duplicate issues when the same command appears on multiple lines.
		reportedCmds := make(map[string]bool)

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
			if !knownRuntimeSet[firstWordLower] {
				continue
			}

			// Skip if already reported for this command source.
			if reportedCmds[firstWordLower] {
				continue
			}

			// Check if it's declared in required_env, description, triggers, or binary requirements.
			if strings.Contains(declaredText, firstWordLower) {
				continue
			}

			reportedCmds[firstWordLower] = true
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "undeclared_dependency",
				Message:    fmt.Sprintf("Command uses %q which is not declared in required_env, description, or requires.bins", firstWord),
				File:       cs.file,
				Suggestion: fmt.Sprintf("Add %q to requires.bins or mention it in the skill description", firstWord),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// validateSkillMD 閳?extract bash blocks from SKILL.md and run checks
// ---------------------------------------------------------------------------

func validateSkillMD(issues *[]PortabilityIssue, mdPath, skillDir string, sf *SkillYAMLFile) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		log.Printf("[portability-validator] warning: cannot read %s: %v", filepath.Base(mdPath), err)
		return
	}

	blocks := extractAllBashBlocksFromMarkdown(string(data))
	if len(blocks) == 0 {
		return
	}

	// Build command sources from SKILL.md bash blocks.
	var mdCommands []commandSource
	for _, block := range blocks {
		mdCommands = append(mdCommands, commandSource{command: block, file: filepath.Base(mdPath)})
	}

	// Run path and platform checks on SKILL.md commands.
	baseDirMatched := checkMissingBaseDir(issues, mdCommands, skillDir)
	checkHardcodedPaths(issues, mdCommands, baseDirMatched)
	checkPathSeparators(issues, mdCommands)
	checkPlatformCompat(issues, mdCommands, sf)
	checkDependencies(issues, mdCommands, sf)
}

func checkScriptTextEncoding(issues *[]PortabilityIssue, skillDir string) {
	if strings.TrimSpace(skillDir) == "" {
		return
	}
	_ = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipEncodingScanDir(d.Name()) && path != skillDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !portabilityScriptExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 || len(data) > 2*1024*1024 {
			return nil
		}
		if bytes.Contains(data, []byte{0}) && utf8.Valid(data) {
			return nil
		}
		rel, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			rel = filepath.Base(path)
		}
		rel = filepath.ToSlash(rel)
		if !utf8.Valid(data) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "encoding",
				Message:    fmt.Sprintf("Script %s is not valid UTF-8", rel),
				File:       rel,
				Suggestion: "Convert the script to UTF-8 so comments and diagnostics survive install and runner output.",
			})
			return nil
		}
		if containsEncodingDamageMarker(string(data)) {
			*issues = append(*issues, PortabilityIssue{
				Severity:   SeverityWarning,
				Category:   "encoding",
				Message:    fmt.Sprintf("Script %s contains text that looks like encoding damage", rel),
				File:       rel,
				Suggestion: "Review comments and user-facing strings; save the file as clean UTF-8 if the text is garbled.",
			})
		}
		return nil
	})
}

func shouldSkipEncodingScanDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", ".hg", ".svn", ".venv", "venv", "node_modules", "__pycache__", "dist", "build":
		return true
	default:
		return false
	}
}

func containsEncodingDamageMarker(text string) bool {
	if strings.ContainsRune(text, utf8.RuneError) {
		return true
	}
	for _, marker := range []string{"锟斤拷", "ï¿½", string(utf8.RuneError)} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
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
	fields := splitCommandFields(line)
	commandIndex := firstShellCommandFieldIndex(fields)
	if commandIndex < 0 {
		return ""
	}
	return fields[commandIndex]
}

// splitCommandSegments splits a command line by shell command separators while
// preserving separators inside quotes.
func splitCommandSegments(line string) []string {
	var segments []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if strings.TrimSpace(current.String()) != "" {
			segments = append(segments, current.String())
		}
		current.Reset()
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			current.WriteByte(c)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteByte(c)
			if rune(c) == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = rune(c)
			current.WriteByte(c)
		case ';', '|', '&':
			if c == '&' && i+1 < len(line) && line[i+1] != '&' {
				current.WriteByte(c)
				continue
			}
			flush()
			if (c == '|' || c == '&') && i+1 < len(line) && line[i+1] == c {
				i++
			}
		default:
			current.WriteByte(c)
		}
	}
	flush()
	return segments
}

// isKnownRuntimeCommand checks if a command name is in the known runtime list.
func isKnownRuntimeCommand(cmd string) bool {
	return knownRuntimeSet[cmd]
}
