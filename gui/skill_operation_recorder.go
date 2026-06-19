package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// RecordedOp represents a single tool call recorded during skill recording.
type RecordedOp struct {
	Timestamp  time.Time              `json:"timestamp"`
	ToolName   string                 `json:"tool_name"`
	Args       map[string]interface{} `json:"args"`
	ResultHint string                 `json:"result_hint"` // truncated result for display
	Success    bool                   `json:"success"`
	WorkDir    string                 `json:"work_dir,omitempty"`
}

// pendingTemplateFile represents a file that needs to be written to the skill directory
// alongside skill.yaml. Used for write_file content and edit_file patches.
type pendingTemplateFile struct {
	Name    string
	Content string
}

// SkillOperationRecorder records tool operations during an agent loop session
// and generates a portable skill.yaml from the recorded sequence.
type SkillOperationRecorder struct {
	mu        sync.Mutex
	active    atomic.Bool // fast-path check without lock; mirrors r.recording
	recording bool
	entries   []RecordedOp
	startTime time.Time
	workDir   string // primary working directory during recording
}

// NewSkillOperationRecorder creates a new recorder instance.
func NewSkillOperationRecorder() *SkillOperationRecorder {
	return &SkillOperationRecorder{}
}

// Start begins recording operations.
func (r *SkillOperationRecorder) Start(workDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("already recording")
	}

	r.recording = true
	r.active.Store(true)
	r.startTime = time.Now()
	r.entries = nil
	r.workDir = workDir
	return nil
}

// IsRecording returns whether the recorder is currently active.
// Uses atomic fast-path for hot-path callers (agent loop).
func (r *SkillOperationRecorder) IsRecording() bool {
	return r.active.Load()
}

// Record adds a tool call to the recording buffer.
// Only called when IsRecording() is true.
func (r *SkillOperationRecorder) Record(toolName string, args map[string]interface{}, resultHint string, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return
	}

	// Deep-copy args to avoid mutation
	argsCopy := make(map[string]interface{}, len(args))
	for k, v := range args {
		argsCopy[k] = v
	}

	r.entries = append(r.entries, RecordedOp{
		Timestamp:  time.Now(),
		ToolName:   toolName,
		Args:       argsCopy,
		ResultHint: truncateString(resultHint, 200),
		Success:    success,
		WorkDir:    r.workDir,
	})
}

// EntryCount returns the number of recorded operations.
func (r *SkillOperationRecorder) EntryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Stop ends recording and generates a skill from the recorded operations.
// Returns the generated skill directory path.
func (r *SkillOperationRecorder) Stop(skillName, description string) (string, error) {
	r.mu.Lock()
	entries := r.entries
	workDir := r.workDir
	r.recording = false
	r.active.Store(false)
	r.entries = nil
	r.mu.Unlock()

	if len(entries) == 0 {
		return "", fmt.Errorf("no operations recorded")
	}

	if skillName == "" {
		skillName = r.suggestSkillName(entries)
	}

	if description == "" {
		description = r.suggestDescription(entries)
	}

	// Generate skill.yaml content + collect template files
	var templates []pendingTemplateFile
	yamlContent, err := r.generateSkillYAML(skillName, description, entries, workDir, &templates)
	if err != nil {
		return "", fmt.Errorf("generate skill yaml: %w", err)
	}

	// Write to skill directory
	skillsDir, err := skill.PrimarySkillsDir()
	if err != nil {
		return "", fmt.Errorf("resolve skills dir: %w", err)
	}

	dirName := sanitizeSkillDirName(skillName)
	skillDir := filepath.Join(skillsDir, dirName)

	// Avoid overwriting existing skill directories — append numeric suffix if collision
	if _, err := os.Stat(skillDir); err == nil {
		for suffix := 2; suffix <= 99; suffix++ {
			candidate := filepath.Join(skillsDir, fmt.Sprintf("%s-%d", dirName, suffix))
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				skillDir = candidate
				break
			}
		}
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create skill dir: %w", err)
	}

	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		return "", fmt.Errorf("write skill.yaml: %w", err)
	}

	// Write template/patch files referenced by generated bash steps
	for _, tmpl := range templates {
		tmplPath := filepath.Join(skillDir, tmpl.Name)
		if err := os.WriteFile(tmplPath, []byte(tmpl.Content), 0o644); err != nil {
			return "", fmt.Errorf("write template %s: %w", tmpl.Name, err)
		}
	}

	return skillDir, nil
}

// Cancel discards the current recording without generating a skill.
func (r *SkillOperationRecorder) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording = false
	r.active.Store(false)
	r.entries = nil
}

// Pause stops accepting new entries but keeps recorded data for later Stop/Cancel.
func (r *SkillOperationRecorder) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recording = false
	r.active.Store(false)
}

// SuggestSkillName generates a suggested skill name from recorded operations.
func (r *SkillOperationRecorder) SuggestSkillName() string {
	r.mu.Lock()
	entries := make([]RecordedOp, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()
	return r.suggestSkillName(entries)
}

// SuggestDescription generates a suggested description from recorded operations.
func (r *SkillOperationRecorder) SuggestDescription() string {
	r.mu.Lock()
	entries := make([]RecordedOp, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()
	return r.suggestDescription(entries)
}

// OperationSummary returns a brief summary of recorded operations for display.
func (r *SkillOperationRecorder) OperationSummary() []string {
	r.mu.Lock()
	entries := make([]RecordedOp, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()

	var lines []string
	for i, op := range entries {
		if i >= 10 {
			lines = append(lines, fmt.Sprintf("... and %d more", len(entries)-10))
			break
		}
		line := formatOpSummaryLine(op)
		lines = append(lines, line)
	}
	return lines
}

// --- Internal helpers ---

func (r *SkillOperationRecorder) suggestSkillName(entries []RecordedOp) string {
	if len(entries) == 0 {
		return "my-skill"
	}

	// Use the first meaningful tool action as the name base
	for _, op := range entries {
		switch op.ToolName {
		case "bash":
			if cmd, ok := op.Args["command"].(string); ok {
				parts := strings.Fields(cmd)
				if len(parts) > 0 {
					base := filepath.Base(parts[0])
					if base != "" && base != "." {
						return "auto-" + sanitizeSkillDirName(base)
					}
				}
			}
		case "write_file":
			if path, ok := op.Args["path"].(string); ok {
				ext := strings.TrimPrefix(filepath.Ext(path), ".")
				base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				if ext != "" {
					return "auto-" + sanitizeSkillDirName(base+"-"+ext)
				}
				return "auto-" + sanitizeSkillDirName(base)
			}
		}
	}

	return "auto-skill-" + time.Now().Format("0102-1504")
}

func (r *SkillOperationRecorder) suggestDescription(entries []RecordedOp) string {
	var toolNames []string
	seen := make(map[string]bool)
	for _, op := range entries {
		if !seen[op.ToolName] {
			toolNames = append(toolNames, op.ToolName)
			seen[op.ToolName] = true
		}
	}
	return fmt.Sprintf("Auto-learned skill from %d operations (tools: %s)",
		len(entries), strings.Join(toolNames, ", "))
}

func (r *SkillOperationRecorder) generateSkillYAML(name, description string, entries []RecordedOp, workDir string, templates *[]pendingTemplateFile) (string, error) {
	steps := make([]map[string]interface{}, 0, len(entries))

	for _, op := range entries {
		if !op.Success {
			continue // skip failed operations
		}

		step := r.convertOpToStep(op, workDir, templates)
		if step != nil {
			steps = append(steps, step)
		}
	}

	if len(steps) == 0 {
		return "", fmt.Errorf("no successful operations to convert")
	}

	// Detect dependencies
	pythonDeps, nodeDeps := detectDependencies(entries)

	// Build skill YAML structure
	skillYAML := map[string]interface{}{
		"name":        name,
		"description": description,
		"status":      "active",
		"source":      "learned",
		"triggers":    generateTriggers(name, description, entries),
		"platforms":   detectPlatforms(entries),
		"steps":       steps,
	}

	if len(pythonDeps) > 0 || len(nodeDeps) > 0 {
		requires := map[string]interface{}{}
		if len(pythonDeps) > 0 {
			requires["python"] = pythonDeps
		}
		if len(nodeDeps) > 0 {
			requires["node"] = nodeDeps
		}
		skillYAML["requires"] = requires
	}

	// Detect required args
	reqArgs := detectRequiredArgs(steps)
	if len(reqArgs) > 0 {
		skillYAML["required_args"] = reqArgs
	}

	data, err := yaml.Marshal(skillYAML)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (r *SkillOperationRecorder) convertOpToStep(op RecordedOp, workDir string, templates *[]pendingTemplateFile) map[string]interface{} {
	// templateSeqNum ensures unique filenames even for same-millisecond operations
	templateSeqNum := len(*templates)

	switch op.ToolName {
	case "bash":
		cmd, _ := op.Args["command"].(string)
		if cmd == "" {
			return nil
		}
		cmd = portabilizeCommand(cmd, workDir)
		params := map[string]interface{}{"command": cmd}
		if bg, ok := op.Args["background"].(bool); ok && bg {
			params["background"] = true
		}
		return map[string]interface{}{
			"action": "bash",
			"params": params,
		}

	case "write_file":
		path, _ := op.Args["path"].(string)
		content, _ := op.Args["content"].(string)
		if path == "" {
			return nil
		}
		path = portabilizePath(path, workDir)
		mode, _ := op.Args["mode"].(string)

		// SkillRunner only supports "bash" action.
		// File content is written to a template file in the skill directory.
		// The bash step uses Python to copy/append (cross-platform: works on Windows cmd.exe too).
		templateName := fmt.Sprintf("template_%d_%d%s", op.Timestamp.UnixMilli(), templateSeqNum, filepath.Ext(path))
		*templates = append(*templates, pendingTemplateFile{
			Name:    templateName,
			Content: content,
		})

		if mode == "append" {
			safePath := strings.ReplaceAll(path, "'", "\\'")
			cmd := fmt.Sprintf("python -c \"import pathlib; src=pathlib.Path('{baseDir}/%s'); dst=pathlib.Path('%s'); f=open(str(dst),'a',encoding='utf-8'); f.write(src.read_text(encoding='utf-8')); f.close()\"",
				templateName, safePath)
			return map[string]interface{}{
				"action": "bash",
				"params": map[string]interface{}{"command": cmd},
			}
		}
		safePath := strings.ReplaceAll(path, "'", "\\'")
		cmd := fmt.Sprintf("python -c \"import shutil; shutil.copy2('{baseDir}/%s','%s')\"",
			templateName, safePath)
		return map[string]interface{}{
			"action": "bash",
			"params": map[string]interface{}{"command": cmd},
		}

	case "edit_file":
		path, _ := op.Args["path"].(string)
		if path == "" {
			return nil
		}
		path = portabilizePath(path, workDir)
		oldStr, _ := op.Args["old_string"].(string)
		newStr, _ := op.Args["new_string"].(string)
		if oldStr == "" {
			return nil
		}

		// Write old/new strings to patch files to avoid shell quoting issues.
		patchBaseName := fmt.Sprintf("patch_%d_%d", op.Timestamp.UnixMilli(), templateSeqNum)
		oldFileName := patchBaseName + "_old.txt"
		newFileName := patchBaseName + "_new.txt"
		*templates = append(*templates, pendingTemplateFile{
			Name:    oldFileName,
			Content: oldStr,
		})
		*templates = append(*templates, pendingTemplateFile{
			Name:    newFileName,
			Content: newStr,
		})

		// Generate a standalone Python script for the patch operation.
		// Uses __file__ directory to locate sibling patch files (portable).
		// Also resolves {baseDir} in the target path at runtime.
		scriptName := patchBaseName + "_apply.py"
		scriptContent := fmt.Sprintf(`import pathlib
import os
_dir = pathlib.Path(os.path.dirname(os.path.abspath(__file__)))
_base = str(_dir)
target_path = r'%s'.replace('{baseDir}', _base).replace('${baseDir}', _base)
target = pathlib.Path(target_path)
old = (_dir / '%s').read_text(encoding='utf-8')
new = (_dir / '%s').read_text(encoding='utf-8')
content = target.read_text(encoding='utf-8')
target.write_text(content.replace(old, new), encoding='utf-8')
`, path, oldFileName, newFileName)
		*templates = append(*templates, pendingTemplateFile{
			Name:    scriptName,
			Content: scriptContent,
		})

		cmd := fmt.Sprintf("python {baseDir}/%s", scriptName)
		return map[string]interface{}{
			"action": "bash",
			"params": map[string]interface{}{"command": cmd},
		}

	default:
		// For other tools, wrap as a comment/note step
		return nil
	}
}

// portabilizeCommand replaces absolute paths in a command with {baseDir} macros
// and parameterizes obvious input/output file references.
func portabilizeCommand(cmd string, workDir string) string {
	if workDir != "" {
		// Normalize path separators for comparison
		normWorkDir := filepath.ToSlash(workDir)
		normCmd := cmd

		// Replace workDir references with {baseDir}
		normCmd = strings.ReplaceAll(normCmd, normWorkDir, "{baseDir}")
		normCmd = strings.ReplaceAll(normCmd, workDir, "{baseDir}")

		// Also handle backslash variant on Windows
		if runtime.GOOS == "windows" {
			backslashDir := strings.ReplaceAll(workDir, "/", "\\")
			normCmd = strings.ReplaceAll(normCmd, backslashDir, "{baseDir}")
		}

		cmd = normCmd
	}

	// Replace home directory references
	if home, err := os.UserHomeDir(); err == nil {
		cmd = strings.ReplaceAll(cmd, filepath.ToSlash(home), "$HOME")
		cmd = strings.ReplaceAll(cmd, home, "$HOME")
	}

	return cmd
}

// portabilizePath makes a file path portable by replacing absolute prefixes.
func portabilizePath(path string, workDir string) string {
	if workDir != "" {
		// filepath.Rel fails across drive letters on Windows — check same prefix first
		normPath := filepath.ToSlash(path)
		normWorkDir := filepath.ToSlash(workDir)
		if !strings.HasSuffix(normWorkDir, "/") {
			normWorkDir += "/"
		}

		if strings.HasPrefix(normPath, normWorkDir) {
			suffix := strings.TrimPrefix(normPath, normWorkDir)
			if suffix == "" {
				return "{baseDir}"
			}
			return "{baseDir}/" + suffix
		}

		// Try filepath.Rel for same-drive relative paths
		rel, err := filepath.Rel(workDir, path)
		if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			if rel == "." {
				return "{baseDir}"
			}
			return "{baseDir}/" + filepath.ToSlash(rel)
		}
	}

	// Replace home directory
	if home, err := os.UserHomeDir(); err == nil {
		normPath := filepath.ToSlash(path)
		normHome := filepath.ToSlash(home)
		if !strings.HasSuffix(normHome, "/") {
			normHome += "/"
		}
		if strings.HasPrefix(normPath, normHome) {
			suffix := strings.TrimPrefix(normPath, normHome)
			return "$HOME/" + suffix
		}
	}

	return path
}

// Pre-compiled regexps for hot paths.
var (
	skillDirNameInvalidRe = regexp.MustCompile(`[^a-z0-9\-_]`)
	skillDirNameMultiDash = regexp.MustCompile(`-+`)
	pipInstallRe         = regexp.MustCompile(`pip[3]?\s+install\s+(.+)`)
	npmInstallRe         = regexp.MustCompile(`npm\s+install\s+(.+)`)
	pkgFlagRe            = regexp.MustCompile(`-\S+`)
	placeholderArgRe     = regexp.MustCompile(`\{\{(\w+)\}\}`)
	// Credential detection patterns for security warnings
	credentialPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|access[_-]?token|auth[_-]?token|bearer)\s*[=:]\s*\S+`),
		regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*\S+`),
		regexp.MustCompile(`(?i)Authorization:\s*(Bearer|Basic)\s+\S+`),
		regexp.MustCompile(`(?i)export\s+(API_KEY|SECRET_KEY|TOKEN|PASSWORD|AWS_SECRET)\s*=`),
		regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),  // OpenAI-style keys
		regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`), // GitHub PAT
	}
)

// detectDependencies scans bash commands for pip/npm install patterns.
func detectDependencies(entries []RecordedOp) (pythonDeps []string, nodeDeps []string) {
	for _, op := range entries {
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if cmd == "" {
			continue
		}

		if m := pipInstallRe.FindStringSubmatch(cmd); len(m) > 1 {
			pkgs := parsePkgList(m[1])
			pythonDeps = append(pythonDeps, pkgs...)
		}
		if m := npmInstallRe.FindStringSubmatch(cmd); len(m) > 1 {
			pkgs := parsePkgList(m[1])
			nodeDeps = append(nodeDeps, pkgs...)
		}
	}

	return dedup(pythonDeps), dedup(nodeDeps)
}

// detectPlatforms infers platform compatibility from recorded operations.
func detectPlatforms(entries []RecordedOp) []string {
	hasBashOnly := false
	for _, op := range entries {
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if needsBashSyntax(cmd) {
			hasBashOnly = true
			break
		}
	}

	if hasBashOnly {
		return []string{"linux", "macos"}
	}
	// Default: universal
	return []string{"windows", "linux", "macos"}
}

// detectRequiredArgs scans step params for {{placeholder}} patterns.
func detectRequiredArgs(steps []map[string]interface{}) []string {
	seen := make(map[string]bool)
	var args []string

	for _, step := range steps {
		params, _ := step["params"].(map[string]interface{})
		for _, v := range params {
			s, ok := v.(string)
			if !ok {
				continue
			}
			for _, m := range placeholderArgRe.FindAllStringSubmatch(s, -1) {
				if len(m) > 1 && !seen[m[1]] {
					seen[m[1]] = true
					args = append(args, m[1])
				}
			}
		}
	}
	return args
}

// needsBashSyntax checks if a command uses bash-specific syntax.
func needsBashSyntax(cmd string) bool {
	bashIndicators := []string{
		"#!/bin/bash", "#!/bin/sh",
		"export ", "source ",
		"$(", "${",
		" | ", " && ", " || ",
		">>", "2>&1",
	}
	for _, ind := range bashIndicators {
		if strings.Contains(cmd, ind) {
			return true
		}
	}
	return false
}

func formatOpSummaryLine(op RecordedOp) string {
	switch op.ToolName {
	case "bash":
		cmd, _ := op.Args["command"].(string)
		cmd = truncateString(cmd, 60)
		return fmt.Sprintf("bash: %s", cmd)
	case "write_file":
		path, _ := op.Args["path"].(string)
		return fmt.Sprintf("write_file: %s", filepath.Base(path))
	case "edit_file":
		path, _ := op.Args["path"].(string)
		return fmt.Sprintf("edit_file: %s", filepath.Base(path))
	default:
		return op.ToolName
	}
}

func sanitizeSkillDirName(name string) string {
	name = strings.ToLower(name)
	name = skillDirNameInvalidRe.ReplaceAllString(name, "-")
	name = skillDirNameMultiDash.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "skill"
	}
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}

func parsePkgList(raw string) []string {
	// Remove flags like -U, --upgrade, -q, etc.
	raw = pkgFlagRe.ReplaceAllString(raw, "")
	parts := strings.Fields(raw)
	var pkgs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "-") {
			continue
		}
		// Skip file paths (requirements files, local packages)
		if strings.Contains(p, "/") || strings.Contains(p, "\\") || strings.HasSuffix(p, ".txt") || strings.HasSuffix(p, ".cfg") {
			continue
		}
		// Skip URLs
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "git+") {
			continue
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func dedup(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}


// isRecordableToolForSkill determines which tools should be captured during
// skill recording. Only tools that can be converted to skill steps are recorded.
func isRecordableToolForSkill(toolName string) bool {
	switch toolName {
	case "bash", "write_file", "edit_file":
		return true
	default:
		return false
	}
}

// detectCredentialWarnings scans recorded operations for potential credential leakage.
// Returns human-readable warnings (empty if no issues found).
func detectCredentialWarnings(entries []RecordedOp) []string {
	var warnings []string
	seen := make(map[string]bool)

	for i, op := range entries {
		var textToScan string
		switch op.ToolName {
		case "bash":
			textToScan, _ = op.Args["command"].(string)
		case "write_file":
			textToScan, _ = op.Args["content"].(string)
		case "edit_file":
			old, _ := op.Args["old_string"].(string)
			new, _ := op.Args["new_string"].(string)
			textToScan = old + "\n" + new
		}
		if textToScan == "" {
			continue
		}

		for _, re := range credentialPatterns {
			if m := re.FindString(textToScan); m != "" {
				// Truncate the matched credential for display (don't show the full secret)
				display := m
				if len(display) > 30 {
					display = display[:15] + "***" + display[len(display)-5:]
				}
				key := fmt.Sprintf("%d:%s", i, re.String()[:20])
				if !seen[key] {
					seen[key] = true
					warnings = append(warnings, fmt.Sprintf("Step %d (%s): possible credential detected — %s", i+1, op.ToolName, display))
				}
			}
		}
	}
	return warnings
}

// generateTriggers produces trigger phrases from the skill name, description,
// and recorded operations. These allow the LLM to discover and match the skill
// when the user describes a similar task.
func generateTriggers(name, description string, entries []RecordedOp) []string {
	triggers := make(map[string]bool)

	// Use skill name words as triggers (split on - and _)
	nameWords := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for _, w := range nameWords {
		w = strings.ToLower(strings.TrimSpace(w))
		if len(w) >= 3 && w != "auto" && w != "skill" {
			triggers[w] = true
		}
	}

	// Extract key commands from bash steps
	for _, op := range entries {
		if op.ToolName != "bash" || !op.Success {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if cmd == "" {
			continue
		}
		parts := strings.Fields(cmd)
		if len(parts) > 0 {
			base := strings.ToLower(filepath.Base(parts[0]))
			// Skip generic commands
			if base != "python" && base != "python3" && base != "node" && base != "pip" && base != "pip3" && base != "npm" && base != "cd" && base != "cat" && base != "cp" && base != "echo" && len(base) >= 3 {
				triggers[base] = true
			}
		}
		// Look for meaningful arguments (file extensions, tool names)
		for _, p := range parts[1:] {
			ext := filepath.Ext(p)
			if ext != "" && len(ext) <= 5 {
				triggers[strings.TrimPrefix(ext, ".")] = true
			}
		}
	}

	// Limit to 8 triggers
	var result []string
	for t := range triggers {
		result = append(result, t)
		if len(result) >= 8 {
			break
		}
	}

	// Always include the skill name as a trigger
	if len(result) == 0 {
		result = append(result, name)
	}

	return result
}
