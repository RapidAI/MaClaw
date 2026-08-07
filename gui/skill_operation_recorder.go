package main

import (
	"fmt"
	"log"
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
	mu         sync.Mutex
	active     atomic.Bool // fast-path check without lock; mirrors r.recording
	recording  bool
	entries    []RecordedOp
	startTime  time.Time
	workDir    string   // primary working directory during recording
	ownerID    string   // session/tab owner that started this recording (used for filtering)
	tabID      string   // frontend tabID that owns this recording (for event payloads)
	stepTitles []string // optional per-step titles suggested by the LLM (applied at Stop)
}

// NewSkillOperationRecorder creates a new recorder instance.
func NewSkillOperationRecorder() *SkillOperationRecorder {
	return &SkillOperationRecorder{}
}

// Start begins recording operations.
// ownerID identifies which session/tab owns this recording (e.g. "desktop-user"
// for the local tab, "desktop-user:{projectPath}" for project tabs).
// The capture point filters tool calls by ownerID to avoid cross-tab mixing.
func (r *SkillOperationRecorder) Start(workDir string, ownerID string) error {
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
	r.ownerID = ownerID
	return nil
}

// StartWithTab begins recording with both ownerID (for capture filtering) and
// tabID (for frontend event payloads). This is the preferred entry point.
func (r *SkillOperationRecorder) StartWithTab(workDir, ownerID, tabID string) error {
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
	r.ownerID = ownerID
	r.tabID = tabID
	r.stepTitles = nil
	return nil
}

// OwnerID returns the session/tab owner that started this recording.
// Returns empty string if not recording.
func (r *SkillOperationRecorder) OwnerID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ownerID
}

// TabID returns the frontend tab ID that owns this recording (mutex-protected).
func (r *SkillOperationRecorder) TabID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tabID
}

// SetSuggestedStepTitles stores optional per-step titles (e.g. produced by the
// LLM metadata pass) to be written into each generated step's name field at Stop.
func (r *SkillOperationRecorder) SetSuggestedStepTitles(titles []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stepTitles = titles
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
// Returns the generated skill directory path and any portability warnings
// detected after the auto-fix pass (non-fatal; empty when clean).
func (r *SkillOperationRecorder) Stop(skillName, description string) (string, []string, error) {
	r.mu.Lock()
	entries := r.entries
	workDir := r.workDir
	stepTitles := r.stepTitles
	r.recording = false
	r.active.Store(false)
	r.entries = nil
	r.stepTitles = nil
	r.mu.Unlock()

	if len(entries) == 0 {
		return "", nil, fmt.Errorf("no operations recorded")
	}

	if skillName == "" {
		skillName = r.suggestSkillName(entries)
	}

	if description == "" {
		description = r.suggestDescription(entries)
	}

	// Generate skill.yaml content + collect template files
	var templates []pendingTemplateFile
	yamlContent, err := r.generateSkillYAML(skillName, description, entries, workDir, stepTitles, &templates)
	if err != nil {
		return "", nil, fmt.Errorf("generate skill yaml: %w", err)
	}

	// Write to skill directory
	skillsDir, err := skill.PrimarySkillsDir()
	if err != nil {
		return "", nil, fmt.Errorf("resolve skills dir: %w", err)
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
		return "", nil, fmt.Errorf("create skill dir: %w", err)
	}

	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		return "", nil, fmt.Errorf("write skill.yaml: %w", err)
	}

	// Write template/patch files referenced by generated bash steps
	for _, tmpl := range templates {
		tmplPath := filepath.Join(skillDir, tmpl.Name)
		if err := os.WriteFile(tmplPath, []byte(tmpl.Content), 0o644); err != nil {
			return "", nil, fmt.Errorf("write template %s: %w", tmpl.Name, err)
		}
	}

	// Portability gate: auto-fix what can be fixed mechanically, then validate
	// and surface anything left (e.g. absolute paths embedded in file contents).
	// Failures here never block saving — the skill is already on disk.
	if changes, fixErr := skill.AutoFixPortability(skillDir); fixErr != nil {
		log.Printf("[skill-recorder] portability auto-fix failed (non-fatal): %v", fixErr)
	} else if len(changes) > 0 {
		log.Printf("[skill-recorder] portability auto-fix applied %d change(s) to %s", len(changes), skillDir)
	}

	var warnings []string
	if report, valErr := skill.ValidateSkillPortability(skillDir); valErr != nil {
		log.Printf("[skill-recorder] portability validation failed (non-fatal): %v", valErr)
	} else if report != nil {
		for _, issue := range report.Issues {
			if issue.Severity == skill.SeverityInfo {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", issue.File, issue.Message))
		}
	}

	return skillDir, warnings, nil
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

// scriptFileExts are extensions treated as "the script being run" when
// deriving a skill name from a bash command.
var scriptFileExts = map[string]bool{
	"py": true, "js": true, "ts": true, "mjs": true,
	"sh": true, "bat": true, "ps1": true, "rb": true, "pl": true,
}

func (r *SkillOperationRecorder) suggestSkillName(entries []RecordedOp) string {
	if len(entries) == 0 {
		return "recorded-skill"
	}

	// 1. Prefer the script a bash command actually runs: `python export_data.py`
	//    → "export-data". This is usually the heart of the workflow.
	for _, op := range entries {
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		for _, field := range strings.Fields(cmd) {
			field = strings.Trim(field, `"'`)
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(field), "."))
			if !scriptFileExts[ext] {
				continue
			}
			stem := strings.TrimSuffix(filepath.Base(field), filepath.Ext(field))
			if name := sanitizeSkillDirName(strings.ReplaceAll(stem, "_", "-")); name != "" && name != "skill" {
				return name
			}
		}
	}

	// 2. A written file reveals the artifact the skill produces:
	//    write_file report_template.xlsx → "report-template-xlsx".
	for _, op := range entries {
		if op.ToolName != "write_file" {
			continue
		}
		path, _ := op.Args["path"].(string)
		if path == "" {
			continue
		}
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		candidate := base
		if ext != "" {
			candidate = base + "-" + ext
		}
		if name := sanitizeSkillDirName(candidate); name != "" && name != "skill" {
			return name
		}
	}

	// 3. First non-generic executable: `ffmpeg -i ...` → "ffmpeg".
	for _, op := range entries {
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		fields := strings.Fields(cmd)
		if len(fields) == 0 {
			continue
		}
		base := strings.ToLower(filepath.Base(fields[0]))
		if base != "" && base != "." && !triggerGenericCmds[base] {
			return sanitizeSkillDirName(base)
		}
	}

	return "recorded-skill-" + time.Now().Format("0102-1504")
}

func (r *SkillOperationRecorder) suggestDescription(entries []RecordedOp) string {
	// Describe what the workflow actually does, one short phrase per action,
	// instead of a bare operation count. Commands are scrubbed of
	// machine-specific paths first — this text is persisted into skill.yaml.
	var actions []string
	for _, op := range entries {
		if !op.Success {
			continue
		}
		switch op.ToolName {
		case "bash":
			cmd, _ := op.Args["command"].(string)
			cmd = strings.Join(strings.Fields(cmd), " ") // collapse whitespace
			if cmd == "" {
				continue
			}
			cmd = portabilizeCommand(cmd, r.workDir)
			actions = append(actions, fmt.Sprintf("运行 `%s`", truncateString(cmd, 50)))
		case "write_file":
			path, _ := op.Args["path"].(string)
			if path != "" {
				actions = append(actions, fmt.Sprintf("写入 `%s`", filepath.Base(path)))
			}
		case "edit_file":
			path, _ := op.Args["path"].(string)
			if path != "" {
				actions = append(actions, fmt.Sprintf("修改 `%s`", filepath.Base(path)))
			}
		}
		if len(actions) >= 4 {
			break
		}
	}

	if len(actions) == 0 {
		return fmt.Sprintf("录制的工作流（%d 个操作）", len(entries))
	}
	desc := "录制的工作流：" + strings.Join(actions, "；")
	if remaining := len(entries) - len(actions); remaining > 0 {
		desc += fmt.Sprintf("；等共 %d 步", len(entries))
	}
	return desc
}

// consolidateRecordedOps reduces a raw recording into a cleaner operation list by:
//  1. Removing diagnostic/read-only bash commands (ls, dir, cat, type, python --version, pip list)
//  2. Collapsing multiple write_file ops to the same path into one (keeping the last version)
//  3. Collapsing multiple edit_file ops to the same file by applying them sequentially into a
//     single write_file of the final content (if the original file was also written by write_file)
//  4. Deduplicating pip/npm install commands (keep only the last successful install)
func consolidateRecordedOps(entries []RecordedOp) []RecordedOp {
	// --- Pass 1: Filter out diagnostic/read-only commands ---
	var filtered []RecordedOp
	for _, op := range entries {
		if !op.Success {
			continue
		}
		if op.ToolName == "bash" {
			cmd, _ := op.Args["command"].(string)
			if isDiagnosticCommand(cmd) {
				continue
			}
		}
		filtered = append(filtered, op)
	}

	// --- Pass 2: Collapse write_file — keep only last write per path ---
	// Track last write index per normalized path
	lastWriteIdx := make(map[string]int) // normalized path → last index in filtered
	for i, op := range filtered {
		if op.ToolName == "write_file" {
			path, _ := op.Args["path"].(string)
			mode, _ := op.Args["mode"].(string)
			if path != "" && mode != "append" {
				lastWriteIdx[normalizePath(path)] = i
			}
		}
	}
	// Mark superseded writes for removal
	superseded := make(map[int]bool)
	for i, op := range filtered {
		if op.ToolName == "write_file" {
			path, _ := op.Args["path"].(string)
			mode, _ := op.Args["mode"].(string)
			if path != "" && mode != "append" {
				np := normalizePath(path)
				if lastIdx, ok := lastWriteIdx[np]; ok && i < lastIdx {
					superseded[i] = true
				}
			}
		}
	}

	// --- Pass 3: Collapse edit_file — merge consecutive edits to same file ---
	// If a file is first written by write_file then edited multiple times,
	// apply all edits to the write_file content and keep only the final write_file.
	// Track the "running content" for files created by write_file within this recording.
	fileContents := make(map[string]int) // normalized path → index in filtered (of write_file)
	for i, op := range filtered {
		if superseded[i] {
			continue
		}
		if op.ToolName == "write_file" {
			path, _ := op.Args["path"].(string)
			mode, _ := op.Args["mode"].(string)
			if path != "" && mode != "append" {
				fileContents[normalizePath(path)] = i
			}
		}
	}

	// Apply edits into the tracked write_file content
	editsAbsorbed := make(map[int]bool) // indices of edit_file ops absorbed into a write_file
	for i, op := range filtered {
		if superseded[i] {
			continue
		}
		if op.ToolName != "edit_file" {
			continue
		}
		path, _ := op.Args["path"].(string)
		if path == "" {
			continue
		}
		np := normalizePath(path)
		writeIdx, hasWrite := fileContents[np]
		if !hasWrite {
			continue // file was not created in this recording, can't merge
		}
		oldStr, _ := op.Args["old_string"].(string)
		newStr, _ := op.Args["new_string"].(string)
		if oldStr == "" {
			continue
		}
		// Apply the edit to the write_file's content
		writeOp := &filtered[writeIdx]
		content, _ := writeOp.Args["content"].(string)
		if strings.Contains(content, oldStr) {
			writeOp.Args["content"] = strings.Replace(content, oldStr, newStr, 1)
			editsAbsorbed[i] = true
		}
	}

	// --- Pass 4: Deduplicate pip/npm install commands ---
	// Keep only the last actual install command per installer (pip/npm).
	lastPipIdx := -1
	lastNpmIdx := -1
	for i, op := range filtered {
		if superseded[i] || editsAbsorbed[i] {
			continue
		}
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if isPipInstallCommand(cmd) {
			if lastPipIdx >= 0 {
				superseded[lastPipIdx] = true
			}
			lastPipIdx = i
		} else if isNpmInstallCommand(cmd) {
			if lastNpmIdx >= 0 {
				superseded[lastNpmIdx] = true
			}
			lastNpmIdx = i
		}
	}

	// --- Final pass: build result excluding superseded/absorbed ops ---
	var result []RecordedOp
	for i, op := range filtered {
		if superseded[i] || editsAbsorbed[i] {
			continue
		}
		result = append(result, op)
	}
	return result
}

// isDiagnosticCommand returns true for commands that are read-only exploration/diagnostics
// and should not be part of a reproducible skill.
func isDiagnosticCommand(cmd string) bool {
	if cmd == "" {
		return true
	}
	cmd = strings.TrimSpace(cmd)

	// Multi-line commands are likely intentional scripts — never diagnostic
	if strings.Contains(cmd, "\n") {
		return false
	}

	// Commands with output redirection (>, >>) are side-effectful — not diagnostic
	if strings.Contains(cmd, " > ") || strings.Contains(cmd, " >> ") {
		return false
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return true
	}
	first := strings.ToLower(filepath.Base(fields[0]))

	// Read-only diagnostic commands (no side effects in any usage pattern)
	if readOnlyDiagnosticCmds[first] {
		return true
	}

	// Version/list checks: python --version, pip list, node --version, npm list
	if len(fields) >= 2 {
		switch first {
		case "python", "python3", "node", "pip", "pip3", "npm":
			arg := strings.ToLower(fields[1])
			if arg == "--version" || arg == "-v" || arg == "-V" || arg == "list" || arg == "--list" {
				return true
			}
		}
	}

	// python -c "import X; print(...)" — one-line import/version checks
	if (first == "python" || first == "python3") && len(fields) >= 3 {
		if fields[1] == "-c" {
			inline := strings.Join(fields[2:], " ")
			inline = strings.Trim(inline, `"'`)
			// Only diagnostic if it doesn't write/modify anything
			if !strings.Contains(inline, "open(") &&
				!strings.Contains(inline, "write") &&
				!strings.Contains(inline, "shutil") &&
				!strings.Contains(inline, "os.remove") &&
				!strings.Contains(inline, "os.rename") &&
				!strings.Contains(inline, "pathlib") &&
				!strings.Contains(inline, "subprocess") &&
				len(inline) < 200 {
				return true
			}
		}
	}

	// pip/npm install check commands (not actual installs)
	if strings.Contains(cmd, "pip list") || strings.Contains(cmd, "pip3 list") {
		return true
	}

	return false
}

// isPipInstallCommand checks if a command is an actual pip install invocation.
// Matches: "pip install ...", "pip3 install ...", "python -m pip install ..."
func isPipInstallCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		fl := strings.ToLower(f)
		if (fl == "pip" || fl == "pip3") && i+1 < len(fields) && strings.ToLower(fields[i+1]) == "install" {
			return true
		}
	}
	return false
}

// isNpmInstallCommand checks if a command is an actual npm install invocation.
func isNpmInstallCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	for i, f := range fields {
		if strings.ToLower(f) == "npm" && i+1 < len(fields) {
			next := strings.ToLower(fields[i+1])
			if next == "install" || next == "i" {
				return true
			}
		}
	}
	return false
}

// normalizePath normalizes a file path for comparison (lowercase on Windows, forward slashes).
func normalizePath(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func (r *SkillOperationRecorder) generateSkillYAML(name, description string, entries []RecordedOp, workDir string, stepTitles []string, templates *[]pendingTemplateFile) (string, error) {
	// ===== Step Consolidation Layer =====
	// Before converting ops to steps, consolidate the raw recording:
	// 1. Remove diagnostic/exploratory commands that don't produce side effects
	// 2. Merge multiple edits to the same file into a single write of the final version
	// 3. Remove superseded write_file calls (keep only the last write to each path)
	// 4. Collapse duplicate pip/npm install commands
	entries = consolidateRecordedOps(entries)

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

	// Attach human-readable step titles when available (LLM-suggested). The
	// SkillYAMLStep.Name field is optional metadata used for display/logging.
	for i := range steps {
		if i < len(stepTitles) {
			if title := strings.TrimSpace(stepTitles[i]); title != "" {
				steps[i]["name"] = title
			}
		}
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
		path = workspaceRelIfRelative(portabilizePath(path, workDir))
		// Scrub machine-specific absolute paths from the file content as well —
		// recorded content must not leak the recording machine's layout.
		content = portabilizeCommand(content, workDir)
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
		path = workspaceRelIfRelative(portabilizePath(path, workDir))
		oldStr, _ := op.Args["old_string"].(string)
		newStr, _ := op.Args["new_string"].(string)
		if oldStr == "" {
			return nil
		}
		// Same scrubbing for the patch payloads.
		oldStr = portabilizeCommand(oldStr, workDir)
		newStr = portabilizeCommand(newStr, workDir)

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
		// The target path is passed as argv[1] so {{placeholder}} args in it are
		// substituted by the skill runner (substitution only happens in the step
		// command string, not inside script files).
		scriptName := patchBaseName + "_apply.py"
		scriptContent := fmt.Sprintf(`import pathlib
import os
import sys
_dir = pathlib.Path(os.path.dirname(os.path.abspath(__file__)))
_base = str(_dir)
target_path = sys.argv[1].replace('{baseDir}', _base).replace('${baseDir}', _base)
target = pathlib.Path(target_path)
old = (_dir / '%s').read_text(encoding='utf-8')
new = (_dir / '%s').read_text(encoding='utf-8')
content = target.read_text(encoding='utf-8')
target.write_text(content.replace(old, new), encoding='utf-8')
`, oldFileName, newFileName)
		*templates = append(*templates, pendingTemplateFile{
			Name:    scriptName,
			Content: scriptContent,
		})

		safeTarget := strings.ReplaceAll(path, `"`, `\"`)
		cmd := fmt.Sprintf(`python {baseDir}/%s "%s"`, scriptName, safeTarget)
		return map[string]interface{}{
			"action": "bash",
			"params": map[string]interface{}{"command": cmd},
		}

	default:
		// For other tools, wrap as a comment/note step
		return nil
	}
}

// workspacePlaceholder is the {{arg}} name used for the recording-time working
// directory. It becomes a required_arg in the generated skill.yaml, so at replay
// time the caller supplies the target workspace explicitly — the skill never
// depends on the machine it was recorded on.
const workspacePlaceholder = "{{workspace}}"

// portabilizeCommand replaces machine-specific absolute paths in a command with
// portable macros/placeholders:
//   - recording workDir → {{workspace}} (resolved from skill args at replay)
//   - user home dir     → $HOME
//   - any other absolute path (Windows drive paths, /Users/..., /home/...) →
//     a {{placeholder}} derived from the path's base name
func portabilizeCommand(cmd string, workDir string) string {
	if workDir != "" {
		cmd = replacePathPrefix(cmd, workDir, workspacePlaceholder)
	}

	// Replace home directory references
	if home, err := os.UserHomeDir(); err == nil {
		cmd = replacePathPrefix(cmd, home, "$HOME")
	}

	// Parameterize any remaining machine-specific absolute paths.
	cmd = replaceForeignAbsPaths(cmd)
	return cmd
}

// replacePathPrefix replaces occurrences of an absolute path prefix in text
// with replacement, guarding against partial-segment matches (e.g. prefix
// "/a/proj" must not rewrite "/a/proj2"). Matching is case-insensitive on
// Windows, where the filesystem ignores path case.
func replacePathPrefix(text, prefix, replacement string) string {
	if prefix == "" {
		return text
	}
	fold := runtime.GOOS == "windows"
	for _, variant := range pathVariants(prefix) {
		// Separator-terminated form first: keeps the remaining suffix intact.
		if fold {
			text = replaceAllFold(text, variant+"/", replacement+"/")
			text = replaceAllFold(text, variant+`\`, replacement+`\`)
		} else {
			text = strings.ReplaceAll(text, variant+"/", replacement+"/")
			text = strings.ReplaceAll(text, variant+`\`, replacement+`\`)
		}
		// Bare prefix only at a segment boundary (end of string or a character
		// that cannot continue a longer path segment).
		pattern := regexp.QuoteMeta(variant) + `(?:$|[^A-Za-z0-9._/\\-])`
		if fold {
			pattern = "(?i)" + pattern
		}
		re := regexp.MustCompile(pattern)
		text = re.ReplaceAllStringFunc(text, func(m string) string {
			return replacement + m[len(variant):]
		})
	}
	return text
}

// replaceAllFold is a case-insensitive strings.ReplaceAll. Falls back to
// case-sensitive replacement when case folding changes byte lengths
// (non-ASCII), where index arithmetic would misalign.
func replaceAllFold(text, old, new string) string {
	if old == "" {
		return text
	}
	lowerOld := strings.ToLower(old)
	if len(lowerOld) != len(old) {
		return strings.ReplaceAll(text, old, new)
	}
	lower := strings.ToLower(text)
	if len(lower) != len(text) {
		return strings.ReplaceAll(text, old, new)
	}
	var b strings.Builder
	b.Grow(len(text))
	for {
		i := strings.Index(lower, lowerOld)
		if i < 0 {
			break
		}
		b.WriteString(text[:i])
		b.WriteString(new)
		text = text[i+len(old):]
		lower = lower[i+len(old):]
	}
	b.WriteString(text)
	return b.String()
}

// pathVariants returns the slash/backslash spellings of a path, deduped.
func pathVariants(p string) []string {
	seen := make(map[string]bool, 3)
	var out []string
	for _, v := range []string{p, filepath.ToSlash(p), strings.ReplaceAll(p, "/", `\`)} {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// portabilizePath makes a file path portable by replacing absolute prefixes:
//   - under recording workDir → {{workspace}}/relative
//   - under user home         → $HOME/relative
//   - other absolute paths    → {{placeholder}} derived from the base name
//   - relative paths          → returned unchanged
func portabilizePath(path string, workDir string) string {
	fold := runtime.GOOS == "windows" // Windows filesystems ignore path case
	if workDir != "" {
		// filepath.Rel fails across drive letters on Windows — check same prefix first
		normPath := filepath.ToSlash(path)
		normWorkDir := filepath.ToSlash(workDir)
		if !strings.HasSuffix(normWorkDir, "/") {
			normWorkDir += "/"
		}

		if hasPathPrefix(normPath, normWorkDir, fold) {
			suffix := normPath[len(normWorkDir):]
			if suffix == "" {
				return workspacePlaceholder
			}
			return workspacePlaceholder + "/" + suffix
		}

		// Try filepath.Rel for same-drive relative paths
		rel, err := filepath.Rel(workDir, path)
		if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			if rel == "." {
				return workspacePlaceholder
			}
			return workspacePlaceholder + "/" + filepath.ToSlash(rel)
		}
	}

	// Replace home directory
	if home, err := os.UserHomeDir(); err == nil {
		normPath := filepath.ToSlash(path)
		normHome := filepath.ToSlash(home)
		if !strings.HasSuffix(normHome, "/") {
			normHome += "/"
		}
		if hasPathPrefix(normPath, normHome, fold) {
			suffix := normPath[len(normHome):]
			return "$HOME/" + suffix
		}
	}

	// Foreign absolute path (another drive, another user, another machine's
	// layout): never bake it into the skill — turn it into a required arg.
	if isMachineAbsPath(path) {
		return placeholderForPath(path)
	}

	return path
}

// hasPathPrefix reports whether path starts with prefix (both already
// slash-normalized), optionally case-insensitively. Byte-length equality is
// guaranteed for ASCII case folding; non-ASCII paths fall back to
// case-sensitive matching to keep index arithmetic aligned.
func hasPathPrefix(path, prefix string, fold bool) bool {
	if fold {
		lp, lx := strings.ToLower(path), strings.ToLower(prefix)
		if len(lp) == len(path) && len(lx) == len(prefix) {
			return strings.HasPrefix(lp, lx)
		}
	}
	return strings.HasPrefix(path, prefix)
}

// workspaceRelIfRelative anchors a still-relative target path to {{workspace}}.
// At record time relative paths resolve against the working directory; at
// replay time bash steps run with the skill directory as cwd, so an
// unqualified relative path would land in the wrong place.
// Paths already parameterized ({{...}}, $HOME) or absolute are returned as-is.
func workspaceRelIfRelative(p string) string {
	if p == "" || strings.HasPrefix(p, "{{") || strings.HasPrefix(p, "$HOME") {
		return p
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || winDriveAbsRe.MatchString(p) {
		return p
	}
	return workspacePlaceholder + "/" + filepath.ToSlash(p)
}

// isMachineAbsPath reports whether p looks like a machine-specific absolute
// path (Windows drive path or a unix user-home path). System paths such as
// /usr/bin are intentionally NOT treated as machine-specific.
func isMachineAbsPath(p string) bool {
	if winDriveAbsRe.MatchString(p) {
		return true
	}
	return unixUserHomeRe.MatchString(filepath.ToSlash(p))
}

// placeholderForPath derives a {{arg}} placeholder from a path's base name,
// e.g. `D:\data\input.csv` → {{input_csv}}, `/Users/x/tools` → {{tools_dir}}.
func placeholderForPath(p string) string {
	norm := strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	base := filepath.Base(norm)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	name := sanitizeSkillDirName(stem)
	name = strings.ReplaceAll(name, "-", "_")
	if name == "" || name == "skill" {
		name = "path"
	}
	if ext == "" {
		// No extension: most likely a directory.
		if !strings.HasSuffix(name, "_dir") {
			name += "_dir"
		}
	} else {
		name += "_" + strings.ToLower(strings.TrimPrefix(ext, "."))
	}
	return "{{" + name + "}}"
}

// replaceForeignAbsPaths scans text for machine-specific absolute paths that
// survived the workDir/home replacements and parameterizes each of them.
func replaceForeignAbsPaths(text string) string {
	text = replaceMatchesWithPlaceholders(winDriveAbsRe, text, 1)
	text = replaceMatchesWithPlaceholders(unixUserHomeRe, text, 1)
	return text
}

// replaceMatchesWithPlaceholders replaces the given capture group (0 = whole
// match) of every regex hit with a {{placeholder}} derived from the matched path.
func replaceMatchesWithPlaceholders(re *regexp.Regexp, text string, group int) string {
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, loc := range locs {
		s, e := loc[0], loc[1]
		if group > 0 {
			s, e = loc[2*group], loc[2*group+1]
		}
		if s < 0 || s < last {
			continue
		}
		b.WriteString(text[last:s])
		b.WriteString(placeholderForPath(text[s:e]))
		last = e
	}
	b.WriteString(text[last:])
	return b.String()
}

// Pre-compiled regexps for hot paths.
var (
	skillDirNameInvalidRe = regexp.MustCompile(`[^a-z0-9\-_]`)
	skillDirNameMultiDash = regexp.MustCompile(`-+`)
	// pipInstallRe captures the package list portion of a pip install command,
	// stopping at the first shell operator (|, >, ;, &) to avoid capturing
	// shell syntax as package names.
	pipInstallRe = regexp.MustCompile(`pip[3]?\s+install\s+((?:[^|>&;])+)`)
	npmInstallRe = regexp.MustCompile(`npm\s+install\s+((?:[^|>&;])+)`)
	pkgFlagRe    = regexp.MustCompile(`-\S+`)
	// validPkgNameRe matches legitimate pip/npm package names:
	// alphanumeric, hyphens, underscores, dots, brackets (extras), version specifiers.
	validPkgNameRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9\-_.]*(\[[\w,]+\])?(([><=!~]+).+)?$`)
	windowsPathRe    = regexp.MustCompile(`[A-Za-z]:\\[\w]`)
	placeholderArgRe = regexp.MustCompile(`\{\{(\w+)\}\}`)
	// winDriveAbsRe matches a Windows drive-letter absolute path (capture group 1),
	// guarded so the drive letter is not preceded by an alphanumeric char
	// (avoids matching the "s://" inside "https://...").
	winDriveAbsRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z]:[\\/][^\s"'|;&<>]*)`)
	// unixUserHomeRe matches unix user-home absolute paths (/Users/..., /home/...)
	// in capture group 1, guarded so it does not fire inside URLs or other
	// tokens (e.g. "https://example.com/home/x" must not match).
	// System paths like /usr/bin are intentionally not matched.
	unixUserHomeRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9.:~/\\-])(/(?:Users|home)/[^\s"'|;&<>]+)`)
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

// readOnlyDiagnosticCmds are commands that NEVER produce side effects regardless of arguments.
// Commands like "find", "echo" are intentionally excluded — they CAN have side effects
// (find -exec rm, echo > file).
var readOnlyDiagnosticCmds = map[string]bool{
	"ls": true, "dir": true, "cat": true, "type": true,
	"pwd": true, "whoami": true, "hostname": true,
	"which": true, "where": true, "locate": true,
	"head": true, "tail": true, "wc": true, "file": true,
	"uname": true, "date": true, "uptime": true, "df": true,
	"env": true, "printenv": true,
}

// triggerGenericCmds are commands too generic to be meaningful skill triggers.
var triggerGenericCmds = map[string]bool{
	"python": true, "python3": true, "node": true,
	"pip": true, "pip3": true, "npm": true,
	"cd": true, "cat": true, "cp": true, "echo": true,
	"dir": true, "del": true, "type": true, "copy": true,
	"mv": true, "rm": true, "ls": true, "mkdir": true,
}

// triggerValidExtensions are file extensions meaningful enough to be skill triggers.
var triggerValidExtensions = map[string]bool{
	"pdf": true, "md": true, "txt": true, "html": true, "csv": true, "json": true,
	"py": true, "js": true, "ts": true, "go": true, "rs": true, "java": true,
	"png": true, "jpg": true, "svg": true, "mp3": true, "mp4": true,
	"docx": true, "xlsx": true, "pptx": true, "yaml": true, "xml": true,
}

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
// Uses positive detection: looks for platform-specific indicators in commands.
// If both Windows and Unix indicators are found (mixed commands), returns all platforms.
func detectPlatforms(entries []RecordedOp) []string {
	hasWindowsIndicator := false
	hasUnixIndicator := false

	for _, op := range entries {
		if op.ToolName != "bash" {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if cmd == "" {
			continue
		}
		if hasWindowsSyntax(cmd) {
			hasWindowsIndicator = true
		}
		if hasUnixOnlySyntax(cmd) {
			hasUnixIndicator = true
		}
	}

	switch {
	case hasWindowsIndicator && !hasUnixIndicator:
		return []string{"windows"}
	case hasUnixIndicator && !hasWindowsIndicator:
		return []string{"linux", "macos"}
	default:
		// Both or neither — assume universal
		return []string{"windows", "linux", "macos"}
	}
}

// hasWindowsSyntax detects Windows-specific command patterns.
func hasWindowsSyntax(cmd string) bool {
	windowsIndicators := []string{
		`C:\`, `D:\`, `E:\`,
		`%APPDATA%`, `%USERPROFILE%`, `%WINDIR%`,
		`$env:`, // PowerShell
	}
	for _, ind := range windowsIndicators {
		if strings.Contains(cmd, ind) {
			return true
		}
	}
	// Windows-only commands (case-insensitive check on first token)
	windowsCmds := []string{"dir", "del", "copy", "move", "cls", "findstr", "type", "rmdir", "icacls", "chcp"}
	fields := strings.Fields(cmd)
	if len(fields) > 0 {
		firstToken := strings.ToLower(fields[0])
		for _, wc := range windowsCmds {
			if firstToken == wc {
				return true
			}
		}
	}
	// Backslash path separators (heuristic: at least one drive:\word pattern)
	if windowsPathRe.MatchString(cmd) {
		return true
	}
	return false
}

// hasUnixOnlySyntax detects Unix-specific syntax that does NOT work on Windows.
// Note: pipes (|), && , || , and 2>&1 work on both Windows cmd/PowerShell and Unix,
// so they are NOT unix-only indicators.
func hasUnixOnlySyntax(cmd string) bool {
	unixOnlyIndicators := []string{
		"#!/bin/bash", "#!/bin/sh", "#!/usr/bin/env",
		"export ", "source ",
		"chmod ", "chown ", "sudo ",
		"/usr/", "/etc/", "/var/", "/home/", "/opt/",
		"~/.config/", "~/.local/",
	}
	for _, ind := range unixOnlyIndicators {
		if strings.Contains(cmd, ind) {
			return true
		}
	}
	// $(...) command substitution — NOT ${...} which is also PowerShell
	if strings.Contains(cmd, "$(") && !strings.Contains(cmd, "$env:") {
		return true
	}
	return false
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
		// Validate: must look like a package name (starts with letter, contains only
		// alphanumeric/hyphens/dots/underscores, optional extras/version specifier).
		// This filters shell operators (2>&1, |, &&), shell commands (findstr, tail),
		// and quoted strings that leak through.
		if !validPkgNameRe.MatchString(p) {
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

// cloneRecordedOps deep-copies the Args maps so consolidation (which rewrites
// write_file contents in place) cannot mutate the recorder's pending entries.
func cloneRecordedOps(entries []RecordedOp) []RecordedOp {
	out := make([]RecordedOp, len(entries))
	for i, op := range entries {
		args := make(map[string]interface{}, len(op.Args))
		for k, v := range op.Args {
			args[k] = v
		}
		op.Args = args
		out[i] = op
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

	// Extract meaningful file extensions from commands (only from path-like tokens)
	for _, op := range entries {
		if op.ToolName != "bash" || !op.Success {
			continue
		}
		cmd, _ := op.Args["command"].(string)
		if cmd == "" {
			continue
		}
		parts := strings.Fields(cmd)
		// Extract first meaningful command name (skip generic commands)
		if len(parts) > 0 {
			base := strings.ToLower(filepath.Base(parts[0]))
			if !triggerGenericCmds[base] && len(base) >= 3 && isAlphanumeric(base) {
				triggers[base] = true
			}
		}
		// Only extract file extensions from tokens that look like actual file paths
		// (contain path separator or start with a letter followed by dot+extension).
		for _, p := range parts[1:] {
			// Strip surrounding quotes
			p = strings.Trim(p, `"'`)
			// Must look like a file path: contains / or \ or ends with .ext
			if !strings.Contains(p, "/") && !strings.Contains(p, "\\") && !strings.Contains(p, ".") {
				continue
			}
			ext := strings.TrimPrefix(filepath.Ext(p), ".")
			ext = strings.ToLower(ext)
			if ext != "" && triggerValidExtensions[ext] {
				triggers[ext] = true
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

// isAlphanumeric checks if a string contains only letters, digits, hyphens, and underscores.
func isAlphanumeric(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
