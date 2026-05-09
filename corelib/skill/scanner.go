package skill

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// MigrateSkillsDir is a no-op kept for backward compatibility.
// The old ~/.maclaw/skills path is no longer supported.
// Skills live exclusively in ~/.maclaw/data/skills.
func MigrateSkillsDir() {}

// SkillScanRoots returns all directories that should be scanned for
// file-based skills, in priority order (first wins on name conflict):
//  1. ~/.maclaw/data/skills  (canonical location)
//  2. ~/.agents/skills
func SkillScanRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".maclaw", "data", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
}

// PrimarySkillsDir returns the canonical skills directory (~/.maclaw/data/skills).
// Callers that need to write new skills should use this path.
func PrimarySkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".maclaw", "data", "skills"), nil
}

// IsUnderSkillsRoot reports whether the given path is a subdirectory of any
// known skill scan root (e.g. ~/.maclaw/data/skills/<name>). This is used to
// detect skill directories so that workspace preparation can skip git worktree
// creation and use the directory directly.
func IsUnderSkillsRoot(path string) bool {
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	// Normalize via EvalSymlinks to handle 8.3 short paths on Windows.
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	for _, root := range SkillScanRoots() {
		rootAbs := root
		if abs, err := filepath.Abs(root); err == nil {
			rootAbs = abs
		}
		if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
			rootAbs = resolved
		}
		// On Windows, paths are case-insensitive but filepath.Rel does
		// pure string comparison. Normalize case before comparing.
		c, r := cleaned, rootAbs
		if runtime.GOOS == "windows" {
			c = strings.ToLower(c)
			r = strings.ToLower(r)
		}
		// Check if cleaned is rootAbs itself or a child of rootAbs.
		rel, err := filepath.Rel(r, c)
		if err != nil {
			continue
		}
		// rel must not start with ".."; that would mean cleaned is outside rootAbs.
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// SkillScanRootsWithExternal returns SkillScanRoots() plus the given
// external directories appended at the end (lower priority).
// Duplicates of built-in roots are silently skipped.
func SkillScanRootsWithExternal(externalDirs []string) []string {
	roots := SkillScanRoots()
	seen := make(map[string]bool, len(roots))
	for _, r := range roots {
		seen[filepath.Clean(r)] = true
	}
	for _, d := range externalDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		cleaned := filepath.Clean(d)
		if !seen[cleaned] {
			roots = append(roots, cleaned)
			seen[cleaned] = true
		}
	}
	return roots
}

// ScanAllSkillDirsWithExternal scans built-in roots plus external directories.
func ScanAllSkillDirsWithExternal(externalDirs []string) []corelib.NLSkillEntry {
	roots := SkillScanRootsWithExternal(externalDirs)
	seen := make(map[string]bool)
	var result []corelib.NLSkillEntry
	for _, root := range roots {
		skills := ScanSkillDir(root)
		for _, s := range skills {
			if !seen[s.Name] {
				result = append(result, s)
				seen[s.Name] = true
			}
		}
	}
	return result
}

// ValidateExternalSkillDir checks whether a directory is a valid skill
// directory (contains at least one subdirectory with skill.yaml, skill.yml,
// skill.md, SKILL.md, or README.md).
// Returns the count of valid skill subdirectories and an error if the
// directory is not usable.
func ValidateExternalSkillDir(dir string) (int, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return 0, fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("path is not a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("cannot read directory: %w", err)
	}
	count := 0
	for _, entry := range entries {
		subInfo, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || !subInfo.IsDir() {
			continue
		}
		if externalSkillDirHasDefinition(filepath.Join(dir, entry.Name())) {
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no valid skill subdirectories found (need skill.yaml/skill.yml or markdown docs such as skill.md/SKILL.md/README.md)")
	}
	return count, nil
}

func externalSkillDirHasDefinition(skillDir string) bool {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		if _, err := os.Stat(filepath.Join(skillDir, name)); err == nil {
			return true
		}
	}
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && isSkillMarkdownDocName(entry.Name()) {
			return true
		}
	}
	return false
}

// ScanAllSkillDirs scans all known skill directories and returns
// deduplicated NLSkillEntry list. Earlier roots have higher priority.
func ScanAllSkillDirs() []corelib.NLSkillEntry {
	roots := SkillScanRoots()
	seen := make(map[string]bool)
	var result []corelib.NLSkillEntry
	for _, root := range roots {
		skills := ScanSkillDir(root)
		for _, s := range skills {
			if !seen[s.Name] {
				result = append(result, s)
				seen[s.Name] = true
			}
		}
	}
	return result
}

// SkillYAMLFile is the on-disk YAML format for a skill definition.
type SkillYAMLFile struct {
	Name             string               `yaml:"name"`
	Description      string               `yaml:"description"`
	Triggers         []string             `yaml:"triggers"`
	Steps            []SkillYAMLStep      `yaml:"steps"`
	Status           string               `yaml:"status"`
	Platforms        []string             `yaml:"platforms"`
	RequiresGUI      bool                 `yaml:"requires_gui"`
	ProducesArtifact *bool                `yaml:"produces_artifact,omitempty"` // false = diagnostic/instruction skill, no file output expected
	Mode             string               `yaml:"mode,omitempty"`              // "sequential" (default) | "interactive" | "api_workflow"
	ExecMode         string               `yaml:"exec_mode,omitempty"`         // "all" (default) | "first" | "named"
	GlobalTimeout    int                  `yaml:"global_timeout,omitempty"`    // per-skill global timeout in seconds
	RequiredArgs     []string             `yaml:"required_args,omitempty"`     // required template variables
	RequiredEnv      []string             `yaml:"required_env,omitempty"`      // required environment variables
	PreferredShell   string               `yaml:"shell,omitempty"`             // "bash" or "cmd"; empty = auto-detect
	Operations       []SkillYAMLOperation `yaml:"operations,omitempty"`        // named operations for api_workflow mode
	Params           []SkillYAMLParam     `yaml:"params,omitempty"`            // parameter schema (aliases, CLI flags, defaults)
	Type             string               `yaml:"type,omitempty"`              // "executable" (default) | "knowledge"
	Content          string               `yaml:"content,omitempty"`           // Markdown content for knowledge-type skills
	// Tool availability conditions
	RequiresTools       []string `yaml:"requires_tools,omitempty"`
	FallbackForTools    []string `yaml:"fallback_for_tools,omitempty"`
	RequiresToolsets    []string `yaml:"requires_toolsets,omitempty"`
	FallbackForToolsets []string `yaml:"fallback_for_toolsets,omitempty"`
	// Credential file mounting
	RequiredCredentialFiles []string `yaml:"required_credential_files,omitempty"`
	// Dependency auto-install: pip/npm packages to install before execution
	Requires *SkillYAMLRequires `yaml:"requires,omitempty"`
	// Stateful enables cross-invocation state persistence (Pattern 4: Baton Relay)
	Stateful bool `yaml:"stateful,omitempty"`
	// Pipeline declares multi-skill orchestration (Pattern 5: Multi-Phase + Checkpoint)
	Pipeline []SkillYAMLPipelineStep `yaml:"pipeline,omitempty"`
	Extra    map[string]any          `yaml:"-"`
}

// SkillYAMLOperation describes a named operation in a skill definition.
type SkillYAMLOperation struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Params      []string `yaml:"params,omitempty"`
	Labels      []string `yaml:"labels,omitempty"`
}

// SkillYAMLParam declares a single parameter in the skill's parameter schema.
// This is the YAML on-disk format; it is converted to corelib.NLSkillParam
// during skill loading.
type SkillYAMLParam struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty"`
	CLIFlag     string   `yaml:"cli_flag,omitempty"`
	Default     string   `yaml:"default,omitempty"`
	Required    bool     `yaml:"required,omitempty"`
}

// SkillYAMLRequires declares runtime dependencies that should be auto-installed
// before skill execution. Supports pip (Python) and npm (Node.js) packages.
type SkillYAMLRequires struct {
	Python []string `yaml:"python,omitempty"` // pip packages, e.g. ["markitdown>=0.1.0", "python-pptx"]
	Node   []string `yaml:"node,omitempty"`   // npm packages, e.g. ["puppeteer"]
	Bins   []string `yaml:"bins,omitempty"`   // command-line binaries, e.g. ["python", "node"]
}

// SkillYAMLStepPoll configures repeated execution of a step until a condition is met.
type SkillYAMLStepPoll struct {
	Interval    int    `yaml:"interval"`
	MaxAttempts int    `yaml:"max_attempts"`
	UntilMatch  string `yaml:"until_match,omitempty"`
	UntilStatus string `yaml:"until_status,omitempty"`
}

// SkillYAMLStep is a single step in a YAML skill definition.
type SkillYAMLStep struct {
	Action         string                 `yaml:"action"`
	Params         map[string]interface{} `yaml:"params"`
	OnError        string                 `yaml:"on_error"`
	Name           string                 `yaml:"name,omitempty"`
	Condition      string                 `yaml:"condition,omitempty"`
	When           string                 `yaml:"when,omitempty"`    // conditional expression for dynamic routing
	Label          string                 `yaml:"label,omitempty"`   // step selector label for api_workflow mode
	Capture        map[string]string      `yaml:"capture,omitempty"` // output capture: varName to regex pattern
	TimeoutSeconds int                    `yaml:"timeout,omitempty"` // per-step timeout in seconds (0 = use default)
	ContinueOnErr  bool                   `yaml:"continue_on_error"`
	Poll           *SkillYAMLStepPoll     `yaml:"poll,omitempty"` // poll config for async steps
	Loop           *SkillYAMLStepLoop     `yaml:"loop,omitempty"` // iterative loop config (Pattern 3)
}

// SkillYAMLStepLoop configures iterative execution with verification gate.
type SkillYAMLStepLoop struct {
	MaxIterations int    `yaml:"max_iterations"`
	UntilStep     string `yaml:"until_step,omitempty"`
	UntilMatch    string `yaml:"until_match,omitempty"`
	OnFailStep    string `yaml:"on_fail_step,omitempty"`
}

// SkillYAMLPipelineStep declares one step in a skill pipeline.
type SkillYAMLPipelineStep struct {
	Skill              string            `yaml:"skill"`
	Params             map[string]string `yaml:"params,omitempty"`
	Checkpoint         bool              `yaml:"checkpoint,omitempty"`
	CheckpointMessage  string            `yaml:"checkpoint_message,omitempty"`
	ContinueOnFail     bool              `yaml:"continue_on_fail,omitempty"`
	TimeImpactOnReject string            `yaml:"time_impact_on_reject,omitempty"`
}

func ParseSkillYAMLFile(data []byte) (*SkillYAMLFile, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	return parseSkillDefinitionRaw(raw, "YAML")
}

// ParseSkillDefinitionFile parses supported structured skill definitions.
func ParseSkillDefinitionFile(data []byte, format string) (*SkillYAMLFile, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "yaml", "yml", "":
		return ParseSkillYAMLFile(data)
	default:
		return nil, fmt.Errorf("unsupported skill definition format: %s", format)
	}
}

func parseSkillDefinitionRaw(raw map[string]any, label string) (*SkillYAMLFile, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s parse error: empty document", label)
	}
	normalizedRaw := normalizeSkillYAMLRaw(raw)
	normalizedData, err := yaml.Marshal(normalizedRaw)
	if err != nil {
		return nil, fmt.Errorf("%s normalize error: %w", label, err)
	}
	var sf SkillYAMLFile
	if err := yaml.Unmarshal(normalizedData, &sf); err != nil {
		return nil, fmt.Errorf("%s parse error: %w", label, err)
	}
	applySkillYAMLCompatibility(&sf, normalizedRaw)
	knownKeys := map[string]bool{
		"name": true, "description": true, "triggers": true, "steps": true,
		"status": true, "platforms": true, "requires_gui": true,
		"produces_artifact": true, "required_args": true, "required_env": true,
		"shell": true, "mode": true, "operations": true, "params": true,
		"type": true, "content": true,
		"requires_tools": true, "fallback_for_tools": true,
		"requires_toolsets": true, "fallback_for_toolsets": true,
		"required_credential_files": true,
		"requires":                  true,
		"exec_mode":                 true, "global_timeout": true,
		"stateful": true, "pipeline": true,
	}
	extra := make(map[string]any)
	for k, v := range raw {
		if !knownKeys[k] {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		sf.Extra = extra
	}
	return &sf, nil
}

func normalizeSkillYAMLRaw(raw map[string]any) map[string]any {
	normalized := make(map[string]any, len(raw)+4)
	for key, value := range raw {
		normalized[key] = value
	}
	copyRawAlias(normalized, "required_env", "requires_env", "required_environment", "env")
	copyRawAlias(normalized, "required_args", "requires_args", "inputs", "input")
	copyRawAlias(normalized, "shell", "preferred_shell")
	for _, key := range []string{"required_env", "required_args", "triggers", "platforms", "requires_tools", "fallback_for_tools", "requires_toolsets", "fallback_for_toolsets", "required_credential_files"} {
		if values := yamlStringList(normalized[key]); len(values) > 0 {
			normalized[key] = values
		}
	}
	if _, ok := normalized["params"]; !ok {
		copyRawAlias(normalized, "params", "parameters", "args_schema", "input_schema")
	}
	if params, ok := normalizeYAMLParamSchema(normalized["params"]); ok {
		normalized["params"] = params
	}
	if isEmptyYAMLValue(normalized["steps"]) {
		if step, ok := synthesizeTopLevelYAMLStep(normalized); ok {
			normalized["steps"] = []interface{}{step}
		}
	}
	if steps, ok := normalizeYAMLSteps(normalized["steps"]); ok {
		normalized["steps"] = steps
	}
	if operations, ok := normalizeYAMLOperations(normalized["operations"]); ok {
		normalized["operations"] = operations
		if isEmptyYAMLValue(normalized["mode"]) {
			normalized["mode"] = "api_workflow"
		}
	}
	if requires, ok := normalizeYAMLRequires(normalized); ok {
		normalized["requires"] = requires
	}
	if pipeline, ok := normalizeYAMLPipeline(normalized["pipeline"]); ok {
		normalized["pipeline"] = pipeline
		if isEmptyYAMLValue(normalized["mode"]) {
			normalized["mode"] = "pipeline"
		}
	}
	normalizeYAMLScalars(normalized)
	return normalized
}

func synthesizeTopLevelYAMLStep(raw map[string]any) (map[string]interface{}, bool) {
	step := map[string]interface{}{}
	for _, key := range []string{"command", "cmd", "run", "script", "shell_command"} {
		if value, ok := raw[key]; ok && !isEmptyYAMLValue(value) {
			step[key] = value
			step["action"] = "run"
			break
		}
	}
	if len(step) == 0 {
		for _, key := range []string{"instructions", "instruction", "prompt", "task"} {
			if value, ok := raw[key]; ok && !isEmptyYAMLValue(value) {
				step[key] = value
				step["action"] = "craft_tool"
				break
			}
		}
	}
	if len(step) == 0 {
		return nil, false
	}
	for _, key := range []string{"working_dir", "cwd", "shell", "preferred_shell", "timeout", "timeout_seconds", "env", "extra_env", "required_env", "requires_env"} {
		if value, ok := raw[key]; ok && !isEmptyYAMLValue(value) {
			step[key] = value
		}
	}
	return step, true
}

func normalizeYAMLOperations(raw interface{}) ([]interface{}, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		operations := make([]interface{}, 0, len(v))
		for name, value := range v {
			op := map[string]interface{}{"name": strings.TrimSpace(name)}
			mergeYAMLOperationValue(op, value)
			operations = append(operations, op)
		}
		return operations, true
	case []interface{}:
		operations := make([]interface{}, 0, len(v))
		for _, item := range v {
			switch op := item.(type) {
			case string:
				name := strings.TrimSpace(op)
				if name != "" {
					operations = append(operations, map[string]interface{}{"name": name, "labels": []string{name}})
				}
			case map[string]interface{}:
				normalized := copyYAMLMap(op)
				copyStepAlias(normalized, "name", "operation", "id")
				copyStepAlias(normalized, "description", "desc")
				copyStepAlias(normalized, "labels", "label", "steps", "step")
				if labels := yamlStringList(normalized["labels"]); len(labels) > 0 {
					normalized["labels"] = labels
				}
				if params := yamlStringList(normalized["params"]); len(params) > 0 {
					normalized["params"] = params
				}
				operations = append(operations, normalized)
			default:
				continue
			}
		}
		return operations, true
	default:
		return nil, false
	}
}

func mergeYAMLOperationValue(op map[string]interface{}, value interface{}) {
	switch v := value.(type) {
	case string:
		if labels := splitCSV(v); len(labels) > 0 {
			op["labels"] = labels
		}
	case []interface{}:
		if labels := yamlStringList(v); len(labels) > 0 {
			op["labels"] = labels
		}
	case map[string]interface{}:
		for key, item := range v {
			op[key] = item
		}
		copyStepAlias(op, "description", "desc")
		copyStepAlias(op, "labels", "label", "steps", "step")
		if labels := yamlStringList(op["labels"]); len(labels) > 0 {
			op["labels"] = labels
		}
		if params := yamlStringList(op["params"]); len(params) > 0 {
			op["params"] = params
		}
	}
}

func normalizeYAMLRequires(raw map[string]any) (map[string]interface{}, bool) {
	req := map[string]interface{}{}
	if existing, ok := raw["requires"].(map[string]interface{}); ok {
		for key, value := range existing {
			req[key] = value
		}
	}
	copyRequiresAlias(req, "python", "pip", "python_packages", "pypi")
	copyRequiresAlias(req, "node", "npm", "node_packages", "javascript")
	copyRequiresAlias(req, "bins", "bin", "commands", "executables")
	if req["bins"] == nil {
		if metadataBins := openclawMetadataRequiredBins(raw); len(metadataBins) > 0 {
			req["bins"] = metadataBins
		}
	}
	if req["python"] == nil {
		for _, key := range []string{"python", "pip", "python_packages", "pypi"} {
			if values := yamlStringList(raw[key]); len(values) > 0 {
				req["python"] = values
				break
			}
		}
	}
	if req["node"] == nil {
		for _, key := range []string{"node", "npm", "node_packages", "javascript"} {
			if values := yamlStringList(raw[key]); len(values) > 0 {
				req["node"] = values
				break
			}
		}
	}
	for _, key := range []string{"python", "node", "bins"} {
		if values := yamlStringList(req[key]); len(values) > 0 {
			req[key] = values
		}
	}
	return req, len(req) > 0
}

func openclawMetadataRequiredBins(raw map[string]any) []string {
	metadata, ok := yamlMap(raw["metadata"])
	if !ok {
		return nil
	}
	openclaw, ok := yamlMap(metadata["openclaw"])
	if !ok {
		return nil
	}
	requires, ok := yamlMap(openclaw["requires"])
	if !ok {
		return nil
	}
	return yamlStringList(requires["bins"])
}

func yamlMap(raw interface{}) (map[string]interface{}, bool) {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v, true
	default:
		return nil, false
	}
}

func copyRequiresAlias(req map[string]interface{}, canonical string, aliases ...string) {
	if req[canonical] != nil {
		return
	}
	for _, alias := range aliases {
		if value, ok := req[alias]; ok && value != nil {
			req[canonical] = value
			return
		}
	}
}

func normalizeYAMLPipeline(raw interface{}) ([]interface{}, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case string:
		steps := []interface{}{}
		for _, skill := range splitCSV(v) {
			steps = append(steps, map[string]interface{}{"skill": skill})
		}
		return steps, len(steps) > 0
	case []interface{}:
		steps := make([]interface{}, 0, len(v))
		for _, item := range v {
			switch step := item.(type) {
			case string:
				if skill := strings.TrimSpace(step); skill != "" {
					steps = append(steps, map[string]interface{}{"skill": skill})
				}
			case map[string]interface{}:
				steps = append(steps, normalizeYAMLPipelineStep(step))
			}
		}
		return steps, true
	case map[string]interface{}:
		steps := make([]interface{}, 0, len(v))
		for skill, value := range v {
			step := map[string]interface{}{"skill": strings.TrimSpace(skill)}
			if params, ok := normalizeStringMap(value); ok {
				step["params"] = params
			}
			steps = append(steps, step)
		}
		return steps, true
	default:
		return nil, false
	}
}

func normalizeYAMLPipelineStep(raw map[string]interface{}) map[string]interface{} {
	if raw["skill"] == nil && raw["name"] == nil && raw["use"] == nil && raw["uses"] == nil && len(raw) == 1 {
		for skill, value := range raw {
			step := map[string]interface{}{"skill": strings.TrimSpace(skill)}
			if params, ok := normalizeStringMap(value); ok {
				step["params"] = params
			}
			return step
		}
	}
	step := copyYAMLMap(raw)
	copyStepAlias(step, "skill", "name", "use", "uses")
	copyStepAlias(step, "checkpoint_message", "message", "confirm", "prompt")
	if value, ok := yamlBoolValue(step["checkpoint"]); ok {
		step["checkpoint"] = value
	}
	if value, ok := yamlBoolValue(firstNonNil(step["continue_on_fail"], step["continue"])); ok {
		step["continue_on_fail"] = value
	}
	if params, ok := normalizeStringMap(step["params"]); ok {
		step["params"] = params
	}
	return step
}

func normalizeStringMap(raw interface{}) (map[string]string, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case map[string]string:
		return v, true
	case map[string]interface{}:
		result := make(map[string]string, len(v))
		for key, value := range v {
			if k := strings.TrimSpace(key); k != "" {
				result[k] = strings.TrimSpace(fmt.Sprintf("%v", value))
			}
		}
		return result, true
	case string:
		result := map[string]string{}
		for _, item := range splitCSV(v) {
			key, value, ok := strings.Cut(item, "=")
			if ok && strings.TrimSpace(key) != "" {
				result[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		return result, len(result) > 0
	default:
		return nil, false
	}
}

func isEmptyYAMLValue(value interface{}) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case []interface{}:
		return len(v) == 0
	case []string:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	case map[string]string:
		return len(v) == 0
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	return text == "" || text == "<nil>"
}

func normalizeYAMLScalars(raw map[string]any) {
	for _, key := range []string{"global_timeout"} {
		if value, ok := yamlIntValue(raw[key]); ok {
			raw[key] = value
		}
	}
	for _, key := range []string{"requires_gui", "stateful", "produces_artifact"} {
		if value, ok := yamlBoolValue(raw[key]); ok {
			raw[key] = value
		}
	}
}

func normalizeYAMLSteps(raw interface{}) ([]interface{}, bool) {
	list, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	normalized := make([]interface{}, 0, len(list))
	for _, item := range list {
		switch step := item.(type) {
		case string:
			command := strings.TrimSpace(step)
			if command != "" {
				normalized = append(normalized, map[string]interface{}{"action": "run", "command": command})
			}
		case map[string]interface{}:
			normalized = append(normalized, normalizeYAMLStepMap(step))
		default:
			normalized = append(normalized, item)
		}
	}
	return normalized, true
}

func normalizeYAMLStepMap(step map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{}, len(step)+2)
	for key, value := range step {
		normalized[key] = value
	}
	copyStepAlias(normalized, "when", "if", "only_if", "run_if")
	copyStepAlias(normalized, "condition", "on")
	copyStepAlias(normalized, "on_error", "error", "error_policy")
	if value, ok := normalized["continue_on_fail"]; ok && normalized["continue_on_error"] == nil {
		normalized["continue_on_error"] = value
	}
	if value, ok := normalized["continue"]; ok && normalized["continue_on_error"] == nil {
		normalized["continue_on_error"] = value
	}
	if value, ok := normalized["ignore_errors"]; ok && normalized["continue_on_error"] == nil {
		normalized["continue_on_error"] = value
	}
	if value, ok := normalized["on_failure"]; ok {
		if enabled, ok := yamlBoolValue(value); ok && enabled && normalized["condition"] == nil {
			normalized["condition"] = "on_failure"
		}
	}
	if value, ok := normalized["on_success"]; ok {
		if enabled, ok := yamlBoolValue(value); ok && enabled && normalized["condition"] == nil {
			normalized["condition"] = "on_success"
		}
	}
	if value, ok := normalized["continue_on_error"]; ok {
		if enabled, ok := yamlBoolValue(value); ok && enabled && normalized["on_error"] == nil {
			normalized["on_error"] = "continue"
		}
	}
	if capture, ok := normalizeYAMLCapture(normalized["capture"]); ok {
		normalized["capture"] = capture
	}
	if poll, ok := normalizeYAMLPoll(normalized["poll"]); ok {
		normalized["poll"] = poll
	}
	if loop, ok := normalizeYAMLLoop(normalized["loop"]); ok {
		normalized["loop"] = loop
	}
	params := map[string]interface{}{}
	if nested, ok := normalized["params"].(map[string]interface{}); ok {
		for key, value := range nested {
			params[key] = value
		}
	}
	if with, ok := normalized["with"].(map[string]interface{}); ok {
		for key, value := range with {
			if _, exists := params[key]; !exists {
				params[key] = value
			}
		}
	}
	if len(params) > 0 {
		normalized["params"] = params
	}
	if strings.TrimSpace(fmt.Sprintf("%v", normalized["action"])) == "" {
		switch {
		case hasAnyYAMLStepKey(normalized, "command", "cmd", "run", "script", "shell_command"):
			normalized["action"] = "run"
		case hasAnyYAMLStepKey(normalized, "instructions", "instruction", "prompt", "task", "text"):
			normalized["action"] = "craft_tool"
		case hasAnyYAMLStepKey(normalized, "uses", "server", "server_id", "mcp_server", "tool", "tool_name"):
			normalized["action"] = "call_mcp_tool"
		}
	}
	return normalized
}

func normalizeYAMLCapture(raw interface{}) (map[string]string, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case map[string]string:
		return v, true
	case map[string]interface{}:
		result := make(map[string]string, len(v))
		for key, value := range v {
			if name := strings.TrimSpace(key); name != "" {
				result[name] = strings.TrimSpace(fmt.Sprintf("%v", value))
			}
		}
		return result, true
	case []interface{}:
		result := map[string]string{}
		for _, item := range v {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name := firstYAMLMapString(entry, "name", "var", "variable", "key")
			pattern := firstYAMLMapString(entry, "pattern", "regex", "match", "from")
			if name != "" && pattern != "" {
				result[name] = pattern
			}
		}
		return result, true
	default:
		return nil, false
	}
}

func normalizeYAMLPoll(raw interface{}) (map[string]interface{}, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case bool:
		if v {
			return map[string]interface{}{}, true
		}
		return nil, false
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return map[string]interface{}{"until_match": strings.TrimSpace(v)}, true
	case map[string]interface{}:
		poll := copyYAMLMap(v)
		copyStepAlias(poll, "until_match", "match", "pattern", "success_pattern")
		copyStepAlias(poll, "until_status", "status", "success_status")
		copyStepAlias(poll, "max_attempts", "attempts", "retries", "max_retries")
		copyStepAlias(poll, "interval", "interval_seconds", "every", "delay")
		for _, key := range []string{"interval", "max_attempts"} {
			if value, ok := yamlIntValue(poll[key]); ok {
				poll[key] = value
			}
		}
		return poll, true
	default:
		return nil, false
	}
}

func normalizeYAMLLoop(raw interface{}) (map[string]interface{}, bool) {
	v, ok := raw.(map[string]interface{})
	if !ok {
		return nil, false
	}
	loop := copyYAMLMap(v)
	copyStepAlias(loop, "max_iterations", "iterations", "max_attempts", "attempts")
	copyStepAlias(loop, "until_match", "match", "pattern", "success_pattern")
	copyStepAlias(loop, "until_step", "verify_step", "check_step")
	copyStepAlias(loop, "on_fail_step", "repair_step", "fix_step")
	if value, ok := yamlIntValue(loop["max_iterations"]); ok {
		loop["max_iterations"] = value
	}
	return loop, true
}

func copyYAMLMap(raw map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		result[key] = value
	}
	return result
}

func firstYAMLMapString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprintf("%v", m[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func yamlIntValue(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &parsed); err == nil {
			return int(parsed), true
		}
	}
	return 0, false
}

func copyStepAlias(step map[string]interface{}, canonical string, aliases ...string) {
	if step == nil || step[canonical] != nil {
		return
	}
	for _, alias := range aliases {
		if value, ok := step[alias]; ok && value != nil {
			step[canonical] = value
			return
		}
	}
}

func hasAnyYAMLStepKey(step map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := step[key]; ok && value != nil {
			if strings.TrimSpace(fmt.Sprintf("%v", value)) != "" {
				return true
			}
		}
	}
	return false
}

func copyRawAlias(raw map[string]any, canonical string, aliases ...string) {
	if raw == nil || raw[canonical] != nil {
		return
	}
	for _, alias := range aliases {
		if value, ok := raw[alias]; ok && value != nil {
			raw[canonical] = value
			return
		}
	}
}

func normalizeYAMLParamSchema(raw interface{}) (interface{}, bool) {
	switch v := raw.(type) {
	case nil:
		return nil, false
	case []interface{}:
		return normalizeYAMLParamList(v), true
	case []SkillYAMLParam:
		return raw, true
	case map[string]interface{}:
		if props, ok := v["properties"].(map[string]interface{}); ok {
			required := stringSetFromYAMLList(v["required"])
			return yamlParamsFromMap(props, required), true
		}
		return yamlParamsFromMap(v, nil), true
	default:
		return raw, true
	}
}

func yamlParamsFromMap(raw map[string]interface{}, required map[string]bool) []map[string]interface{} {
	params := make([]map[string]interface{}, 0, len(raw))
	for name, value := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		param := map[string]interface{}{"name": name}
		if required != nil && required[name] {
			param["required"] = true
		}
		if spec, ok := value.(map[string]interface{}); ok {
			for key, specValue := range spec {
				switch key {
				case "desc":
					if param["description"] == nil {
						param["description"] = specValue
					}
				case "flag":
					if param["cli_flag"] == nil {
						param["cli_flag"] = specValue
					}
				case "alias":
					if param["aliases"] == nil {
						param["aliases"] = specValue
					}
				default:
					param[key] = specValue
				}
			}
		} else if description := strings.TrimSpace(fmt.Sprintf("%v", value)); description != "" {
			param["description"] = description
		}
		params = append(params, normalizeYAMLParamMap(param))
	}
	return params
}

func normalizeYAMLParamList(raw []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(raw))
	for _, item := range raw {
		param, ok := item.(map[string]interface{})
		if !ok {
			name := strings.TrimSpace(fmt.Sprintf("%v", item))
			if name != "" {
				normalized = append(normalized, map[string]interface{}{"name": name})
			}
			continue
		}
		normalized = append(normalized, normalizeYAMLParamMap(param))
	}
	return normalized
}

func normalizeYAMLParamMap(raw map[string]interface{}) map[string]interface{} {
	param := make(map[string]interface{}, len(raw)+2)
	for key, value := range raw {
		switch key {
		case "desc":
			if param["description"] == nil {
				param["description"] = value
			}
		case "flag":
			if param["cli_flag"] == nil {
				param["cli_flag"] = value
			}
		case "alias":
			if param["aliases"] == nil {
				param["aliases"] = value
			}
		default:
			param[key] = value
		}
	}
	for _, key := range []string{"name", "description", "cli_flag", "default"} {
		if value, ok := param[key]; ok && value != nil {
			param[key] = strings.TrimSpace(fmt.Sprintf("%v", value))
		}
	}
	if aliases := yamlStringList(param["aliases"]); len(aliases) > 0 {
		param["aliases"] = aliases
	}
	if required, ok := yamlBoolValue(param["required"]); ok {
		param["required"] = required
	}
	return param
}

func yamlBoolValue(raw interface{}) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "y", "1", "required":
			return true, true
		case "false", "no", "n", "0", "optional":
			return false, true
		}
	case int:
		return v != 0, true
	case int64:
		return v != 0, true
	case float64:
		return v != 0, true
	}
	return false, false
}

func stringSetFromYAMLList(raw interface{}) map[string]bool {
	values := yamlStringList(raw)
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = true
		}
	}
	return set
}

func applySkillYAMLCompatibility(sf *SkillYAMLFile, raw map[string]any) {
	if sf == nil || raw == nil {
		return
	}
	if len(sf.RequiredEnv) == 0 {
		sf.RequiredEnv = firstYAMLStringList(raw, "required_env", "requires_env", "required_environment", "env")
	}
	if len(sf.RequiredArgs) == 0 {
		sf.RequiredArgs = firstYAMLStringList(raw, "required_args", "requires_args", "inputs", "input")
	}
	if strings.TrimSpace(sf.PreferredShell) == "" {
		sf.PreferredShell = firstYAMLString(raw, "preferred_shell", "shell")
	}
	mergeTopLevelStepParams(sf, raw)
}

func firstYAMLString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := yamlString(raw[key]); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstYAMLStringList(raw map[string]any, keys ...string) []string {
	for _, key := range keys {
		if values := yamlStringList(raw[key]); len(values) > 0 {
			return values
		}
	}
	return nil
}

func mergeTopLevelStepParams(sf *SkillYAMLFile, raw map[string]any) {
	rawSteps, ok := raw["steps"].([]interface{})
	if !ok {
		return
	}
	for i := 0; i < len(sf.Steps) && i < len(rawSteps); i++ {
		stepMap, ok := rawSteps[i].(map[string]interface{})
		if !ok {
			continue
		}
		if sf.Steps[i].Params == nil {
			sf.Steps[i].Params = map[string]interface{}{}
		}
		for key, value := range stepMap {
			if isSkillYAMLStepField(key) {
				continue
			}
			if _, exists := sf.Steps[i].Params[key]; !exists {
				sf.Steps[i].Params[key] = value
			}
		}
	}
}

func isSkillYAMLStepField(key string) bool {
	switch key {
	case "action", "params", "with", "on_error", "error", "error_policy", "name", "condition", "on", "when", "if", "only_if", "run_if", "on_failure", "on_success", "label", "capture", "timeout", "continue", "ignore_errors", "continue_on_error", "continue_on_fail", "poll", "loop":
		return true
	default:
		return false
	}
}

func FormatSkillYAMLFile(sf *SkillYAMLFile) ([]byte, error) {
	data, err := yaml.Marshal(sf)
	if err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	if len(sf.Extra) == 0 {
		return data, nil
	}
	var known map[string]any
	if err := yaml.Unmarshal(data, &known); err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	if known == nil {
		known = make(map[string]any)
	}
	for k, v := range sf.Extra {
		known[k] = v
	}
	merged, err := yaml.Marshal(known)
	if err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	return merged, nil
}

func FormatSkillDefinitionFile(sf *SkillYAMLFile, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "yaml", "yml", "":
		return FormatSkillYAMLFile(sf)
	default:
		return nil, fmt.Errorf("unsupported skill definition format: %s", format)
	}
}

func skillDefinitionPath(skillDir string) (string, string, error) {
	for _, candidate := range []struct {
		name   string
		format string
	}{
		{name: "skill.yaml", format: "yaml"},
		{name: "skill.yml", format: "yaml"},
	} {
		path := filepath.Join(skillDir, candidate.name)
		if _, err := os.Stat(path); err == nil {
			return path, candidate.format, nil
		}
	}
	return "", "", os.ErrNotExist
}

func loadSkillFromDir(skillDir, fallbackName string) (*corelib.NLSkillEntry, string, error) {
	defPath, defFormat, defErr := skillDefinitionPath(skillDir)
	if defErr == nil {
		data, err := os.ReadFile(defPath)
		if err != nil {
			return nil, "", err
		}
		parsedYAML, err := ParseSkillDefinitionFile(data, defFormat)
		if err != nil {
			if parsed, fallbackPath, fallbackErr := loadMarkdownSkillFromDir(skillDir, fallbackName); fallbackErr == nil {
				log.Printf("[skill-scanner] %s: %s parse failed (%v), using markdown fallback %s", fallbackName, filepath.Base(defPath), err, filepath.Base(fallbackPath))
				return parsed, fallbackPath, nil
			}
			return nil, "", err
		}
		sf := *parsedYAML
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = fallbackName
		}
		// Preserve the directory name and skill.yaml name for alias lookup
		// when SKILL.md may override the display name.
		yamlName := name
		status := sf.Status
		if status == "" {
			status = "active"
		}
		steps := make([]corelib.NLSkillStep, 0, len(sf.Steps))
		for _, s := range sf.Steps {
			params := s.Params
			if params == nil {
				params = make(map[string]interface{})
			}
			if s.TimeoutSeconds > 0 {
				params["timeout"] = float64(s.TimeoutSeconds)
			}
			onError := s.OnError
			if onError == "" {
				if s.ContinueOnErr {
					onError = "continue"
				} else {
					onError = "stop"
				}
			}
			step := corelib.NLSkillStep{
				Action:    s.Action,
				Params:    params,
				OnError:   onError,
				Name:      s.Name,
				Condition: s.Condition,
				When:      s.When,
				Label:     s.Label,
				Capture:   s.Capture,
			}
			if s.Poll != nil {
				step.Poll = &corelib.StepPollConfig{
					Interval:    s.Poll.Interval,
					MaxAttempts: s.Poll.MaxAttempts,
					UntilMatch:  s.Poll.UntilMatch,
					UntilStatus: s.Poll.UntilStatus,
				}
			}
			if s.Loop != nil {
				step.Loop = &corelib.StepLoopConfig{
					MaxIterations: s.Loop.MaxIterations,
					UntilStep:     s.Loop.UntilStep,
					UntilMatch:    s.Loop.UntilMatch,
					OnFailStep:    s.Loop.OnFailStep,
				}
			}
			steps = append(steps, step)
		}

		// Handle YAML-declared knowledge skills before any markdown command
		// extraction. Documentation skills often contain many bash examples;
		// type: knowledge is an explicit signal that those blocks are reference
		// material, not a sequential executable workflow.
		if isKnowledgeSkillType(sf.Type) {
			content := strings.TrimSpace(sf.Content)
			contentPath := defPath
			if content == "" {
				if mdPath, mdErr := skillMarkdownPath(skillDir); mdErr == nil {
					if data, readErr := os.ReadFile(mdPath); readErr == nil {
						content = strings.TrimSpace(string(data))
						contentPath = mdPath
					} else {
						return nil, "", readErr
					}
				}
			}
			if content != "" {
				return buildYAMLKnowledgeSkillEntry(skillDir, name, fallbackName, status, content, contentPath, sf), contentPath, nil
			}
			if knowledgeEntry, knowledgePath, knowledgeErr := loadKnowledgeSkill(skillDir, name, fallbackName, sf.Description, sf.Triggers, status, sf.Platforms, sf.RequiresGUI, defPath); knowledgeErr == nil {
				return knowledgeEntry, knowledgePath, nil
			}
		}

		if len(steps) == 0 {
			var requiresGUI *bool
			if sf.RequiresGUI {
				requiresGUI = &sf.RequiresGUI
			}
			var producesArtifact *bool
			if sf.ProducesArtifact != nil {
				producesArtifact = sf.ProducesArtifact
			}
			parsed, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{
				NameFallback:        name,
				DescriptionFallback: sf.Description,
				Triggers:            sf.Triggers,
				Source:              "file",
				SkillDir:            skillDir,
				Platforms:           sf.Platforms,
				RequiresGUI:         requiresGUI,
				ProducesArtifact:    producesArtifact,
			})
			if err == nil {
				// If SKILL.md overrode the name (e.g. H1 heading "Python 测试"
				// vs skill.yaml name "_test-python"), preserve the yaml/dir name
				// as DirName so run_skill can find it by either name.
				if parsed.Name != yamlName {
					parsed.DirName = yamlName
				}
				if parsed.DirName == "" && fallbackName != parsed.Name {
					parsed.DirName = fallbackName
				}
				// YAML fields take precedence over markdown frontmatter.
				if len(sf.RequiredArgs) > 0 {
					parsed.RequiredArgs = sf.RequiredArgs
				}
				if len(sf.RequiredEnv) > 0 {
					parsed.RequiredEnv = sf.RequiredEnv
				}
				if sf.PreferredShell != "" {
					parsed.PreferredShell = sf.PreferredShell
				}
				if len(sf.RequiresTools) > 0 {
					parsed.RequiresTools = sf.RequiresTools
				}
				if len(sf.FallbackForTools) > 0 {
					parsed.FallbackForTools = sf.FallbackForTools
				}
				if len(sf.RequiresToolsets) > 0 {
					parsed.RequiresToolsets = sf.RequiresToolsets
				}
				if len(sf.FallbackForToolsets) > 0 {
					parsed.FallbackForToolsets = sf.FallbackForToolsets
				}
				if len(sf.RequiredCredentialFiles) > 0 {
					parsed.RequiredCredentialFiles = sf.RequiredCredentialFiles
				}
				if reqs := requiresPythonFromYAML(sf.Requires); len(reqs) > 0 {
					parsed.RequiresPython = reqs
				}
				if reqs := requiresNodeFromYAML(sf.Requires); len(reqs) > 0 {
					parsed.RequiresNode = reqs
				}
				if reqs := requiresBinsFromYAML(sf.Requires); len(reqs) > 0 {
					parsed.RequiresBins = reqs
				}
				if sf.Mode != "" {
					parsed.Mode = sf.Mode
				}
				if sf.ExecMode != "" {
					parsed.ExecMode = sf.ExecMode
				}
				if sf.GlobalTimeout > 0 {
					parsed.GlobalTimeout = sf.GlobalTimeout
				}
				if len(sf.Operations) > 0 {
					parsed.Operations = convertSkillYAMLOperations(sf.Operations)
				}
				if len(sf.Params) > 0 {
					parsed.Params = convertSkillYAMLParams(sf.Params)
				}
				if sf.Stateful {
					parsed.Stateful = true
				}
				if len(sf.Pipeline) > 0 {
					parsed.Pipeline = convertPipelineSteps(sf.Pipeline)
				}
				NormalizeSkillForRunner(parsed)
				if mdPath, mdErr := skillMarkdownPath(skillDir); mdErr == nil {
					return parsed, mdPath, nil
				}
				return parsed, defPath, nil
			}

			// No SKILL.md found; check for KNOWLEDGE.md to create a knowledge skill.
			if knowledgeEntry, knowledgePath, knowledgeErr := loadKnowledgeSkill(skillDir, name, fallbackName, sf.Description, sf.Triggers, status, sf.Platforms, sf.RequiresGUI, defPath); knowledgeErr == nil {
				return knowledgeEntry, knowledgePath, nil
			}
		}
		// Convert YAML operations to runtime operations.
		var operations []corelib.NLSkillOperation
		for _, op := range sf.Operations {
			operations = append(operations, corelib.NLSkillOperation{
				Name:        op.Name,
				Description: op.Description,
				Params:      op.Params,
				Labels:      op.Labels,
			})
		}
		// Convert YAML params to runtime params.
		var skillParams []corelib.NLSkillParam
		for _, p := range sf.Params {
			skillParams = append(skillParams, corelib.NLSkillParam{
				Name:        p.Name,
				Description: p.Description,
				Aliases:     p.Aliases,
				CLIFlag:     p.CLIFlag,
				Default:     p.Default,
				Required:    p.Required,
			})
		}
		producesArtifact := true
		if sf.ProducesArtifact != nil && !*sf.ProducesArtifact {
			producesArtifact = false
		}
		entry := &corelib.NLSkillEntry{
			Name:                    name,
			DirName:                 fallbackName, // directory name for alias lookup
			Description:             sf.Description,
			Triggers:                sf.Triggers,
			Steps:                   steps,
			Status:                  status,
			Source:                  "file",
			Platforms:               sf.Platforms,
			RequiresGUI:             sf.RequiresGUI,
			Mode:                    sf.Mode,
			ExecMode:                sf.ExecMode,
			GlobalTimeout:           sf.GlobalTimeout,
			SkillDir:                skillDir,
			CreatedAt:               fileModTime(defPath),
			ProducesArtifact:        producesArtifact,
			Operations:              operations,
			Params:                  skillParams,
			RequiredArgs:            sf.RequiredArgs,
			RequiredEnv:             sf.RequiredEnv,
			PreferredShell:          sf.PreferredShell,
			RequiresTools:           sf.RequiresTools,
			FallbackForTools:        sf.FallbackForTools,
			RequiresToolsets:        sf.RequiresToolsets,
			FallbackForToolsets:     sf.FallbackForToolsets,
			RequiredCredentialFiles: sf.RequiredCredentialFiles,
			RequiresPython:          requiresPythonFromYAML(sf.Requires),
			RequiresNode:            requiresNodeFromYAML(sf.Requires),
			RequiresBins:            requiresBinsFromYAML(sf.Requires),
			Stateful:                sf.Stateful,
			Pipeline:                convertPipelineSteps(sf.Pipeline),
			References:              scanReferences(skillDir),
		}
		NormalizeSkillForRunner(entry)
		return entry, defPath, nil
	}

	return loadMarkdownSkillFromDir(skillDir, fallbackName)
}

func loadMarkdownSkillFromDir(skillDir, fallbackName string) (*corelib.NLSkillEntry, string, error) {
	parsed, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{
		NameFallback: fallbackName,
		Source:       "file",
		SkillDir:     skillDir,
	})
	if err != nil {
		// Fallback: try Claude SKILL.md format (YAML frontmatter with
		// allowed-tools / tools definitions). This enables skills from
		// awesome-claude-skills and similar community repos.
		mdPath, mdErr := skillMarkdownPath(skillDir)
		if mdErr == nil {
			data, readErr := os.ReadFile(mdPath)
			if readErr == nil {
				if IsClaudeSKILLMD(data) {
					claudeEntry, claudeErr := ParseClaudeSKILLMD(skillDir, data)
					if claudeErr == nil {
						if claudeEntry.Name != fallbackName {
							claudeEntry.DirName = fallbackName
						}
						claudeEntry.CreatedAt = fileModTime(mdPath)
						return claudeEntry, mdPath, nil
					}
				}

				// Ultimate fallback: when all structured parsers fail, feed
				// the raw markdown to LLM via craft_tool. This gives unknown
				// skill formats (OpenAI skill.md, third-party formats, etc.)
				// a chance to work; the LLM reads the instructions and
				// decides how to execute. Deterministic execution is lost,
				// but the skill doesn't silently disappear.
				entry := buildCraftToolFallback(skillDir, fallbackName, data)
				if entry != nil {
					entry.CreatedAt = fileModTime(mdPath)
					log.Printf("[skill-scanner] %s: all parsers failed, using craft_tool LLM fallback", fallbackName)
					return entry, mdPath, nil
				}
			}
		}

		// Last resort before giving up: check for KNOWLEDGE.md (no skill.yaml present).
		if knowledgeEntry, knowledgePath, knowledgeErr := loadKnowledgeSkill(skillDir, fallbackName, fallbackName, "", nil, "active", nil, false, ""); knowledgeErr == nil {
			return knowledgeEntry, knowledgePath, nil
		}

		return nil, "", err
	}
	// Set DirName when SKILL.md name differs from directory name
	if parsed.Name != fallbackName {
		parsed.DirName = fallbackName
	}
	NormalizeSkillForRunner(parsed)
	if mdPath, mdErr := skillMarkdownPath(skillDir); mdErr == nil {
		return parsed, mdPath, nil
	}
	return parsed, "", nil
}

// loadKnowledgeSkill checks for a KNOWLEDGE.md file in the skill directory and
// returns a knowledge-type NLSkillEntry if found. This enables skills that contain
// procedural knowledge (Markdown instructions) rather than executable steps.
// The defPath parameter is used for CreatedAt when a skill.yaml exists alongside
// KNOWLEDGE.md; pass empty string when no YAML is present.
func loadKnowledgeSkill(skillDir, name, fallbackName, description string, triggers []string, status string, platforms []string, requiresGUI bool, defPath string) (*corelib.NLSkillEntry, string, error) {
	knowledgePath := filepath.Join(skillDir, "KNOWLEDGE.md")
	data, err := os.ReadFile(knowledgePath)
	if err != nil {
		return nil, "", err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, "", fmt.Errorf("KNOWLEDGE.md is empty")
	}

	createdAt := fileModTime(knowledgePath)
	if defPath != "" {
		createdAt = fileModTime(defPath)
	}

	return &corelib.NLSkillEntry{
		Name:        name,
		DirName:     fallbackName,
		Description: description,
		Triggers:    triggers,
		Status:      status,
		Source:      "file",
		Platforms:   platforms,
		RequiresGUI: requiresGUI,
		SkillDir:    skillDir,
		CreatedAt:   createdAt,
		Type:        "knowledge",
		Content:     content,
	}, knowledgePath, nil
}

func isKnowledgeSkillType(raw string) bool {
	typ := strings.ToLower(strings.TrimSpace(raw))
	typ = strings.NewReplacer("-", "_", " ", "_").Replace(typ)
	switch typ {
	case "knowledge", "knowledge_skill", "documentation", "documentation_skill", "doc", "docs":
		return true
	default:
		return false
	}
}

func IsKnowledgeSkillType(raw string) bool {
	return isKnowledgeSkillType(raw)
}

func buildYAMLKnowledgeSkillEntry(skillDir, name, fallbackName, status, content, contentPath string, sf SkillYAMLFile) *corelib.NLSkillEntry {
	createdAt := fileModTime(contentPath)
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().Format(time.RFC3339)
	}
	return &corelib.NLSkillEntry{
		Name:                    name,
		DirName:                 fallbackName,
		Description:             sf.Description,
		Triggers:                sf.Triggers,
		Status:                  status,
		Source:                  "file",
		Platforms:               sf.Platforms,
		RequiresGUI:             sf.RequiresGUI,
		SkillDir:                skillDir,
		CreatedAt:               createdAt,
		Type:                    "knowledge",
		Content:                 content,
		RequiresTools:           sf.RequiresTools,
		FallbackForTools:        sf.FallbackForTools,
		RequiresToolsets:        sf.RequiresToolsets,
		FallbackForToolsets:     sf.FallbackForToolsets,
		RequiredCredentialFiles: sf.RequiredCredentialFiles,
		References:              scanReferences(skillDir),
	}
}

// buildCraftToolFallback creates an NLSkillEntry that delegates the entire
// skill markdown to LLM via a single craft_tool step. This is the last-resort
// fallback when no structured parser (skill.yaml, our markdown, Claude SKILL.md)
// can handle the format. The LLM reads the raw instructions and figures out
// what to do, like goskills' approach, but only as a fallback.
func buildCraftToolFallback(skillDir, fallbackName string, data []byte) *corelib.NLSkillEntry {
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	// Parse YAML frontmatter as the single source of truth.
	yamlFM, body := ParseMarkdownFrontmatterYAML(content)

	name := yamlString(yamlFM["name"])
	if name == "" {
		name = firstMarkdownHeading(body)
	}
	if name == "" {
		name = fallbackName
	}
	desc := yamlString(yamlFM["description"])
	if desc == "" {
		desc = firstMarkdownParagraph(body)
	}
	if desc == "" {
		desc = name
	}

	// Replace Claude-specific paths for broader compatibility.
	content = replaceClaudePaths(content)

	// Extract all typed metadata from the single YAML map.
	meta := extractSkillMetadata(yamlFM)

	triggers := meta.triggers
	if len(triggers) == 0 {
		triggers = []string{name}
	}

	params := map[string]interface{}{
		"instructions":      content,
		"verification_mode": "artifact_optional",
		"register_policy":   "manual",
	}
	if skillDir != "" {
		params["working_dir"] = skillDir
	}

	return &corelib.NLSkillEntry{
		Name:        name,
		DirName:     fallbackName,
		Description: desc,
		Triggers:    triggers,
		Steps: []corelib.NLSkillStep{
			{Action: "craft_tool", Params: params},
		},
		Status:         "active",
		Source:         "file",
		SkillDir:       skillDir,
		Platforms:      meta.platforms,
		Mode:           meta.mode,
		RequiredArgs:   meta.requiredArgs,
		RequiredEnv:    meta.requiredEnv,
		PreferredShell: meta.preferredShell,
		GlobalTimeout:  meta.timeout,
		RequiresPython: meta.requiresPython,
		RequiresNode:   meta.requiresNode,
	}
}

// uploadStatusFile mirrors the GUI-side upload_status.json format.
type uploadStatusFile struct {
	SubmissionID string `json:"submission_id"`
}

// mapGOOSToPlatform maps runtime.GOOS values to the platform strings
// used in skill YAML definitions.
func mapGOOSToPlatform(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

// currentPlatform returns the platform string for the current OS.
var currentPlatform = mapGOOSToPlatform(runtime.GOOS)

// isSkillCompatibleWithPlatform checks whether a skill should be included
// based on its Platforms field and the given platform string.
// If Platforms is empty, the skill is compatible with all platforms (backward compatible).
func isSkillCompatibleWithPlatform(platforms []string, platform string) bool {
	if len(platforms) == 0 {
		return true
	}
	current := normalizeSkillPlatform(platform)
	sawSpecific := false
	for _, p := range platforms {
		p = normalizeSkillPlatform(p)
		if p == "" {
			continue
		}
		if isUniversalSkillPlatform(p) {
			return true
		}
		sawSpecific = true
		if p == current {
			return true
		}
	}
	return !sawSpecific
}

func normalizeSkillPlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	platform = strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(platform)
	switch platform {
	case "darwin", "mac", "mac_os", "osx":
		return "macos"
	case "win", "win32", "win64":
		return "windows"
	case "gnu_linux":
		return "linux"
	default:
		return platform
	}
}

func isUniversalSkillPlatform(platform string) bool {
	switch platform {
	case "universal", "all", "any", "*", "cross_platform", "crossplatform", "portable":
		return true
	default:
		return false
	}
}

// ScanSkillDir scans a single directory for skill.yaml / skill.yml / skill.md files
// in immediate subdirectories and returns parsed NLSkillEntry list.
// Skills incompatible with the current OS platform are excluded.
// Permission errors and symlink issues are logged and skipped gracefully.
func ScanSkillDir(root string) []corelib.NLSkillEntry {
	return scanSkillDirInternal(root, true)
}

// ScanSkillDirAll is like ScanSkillDir but does not apply platform filtering.
// Use this when the caller needs to see every skill on disk regardless of
// compatibility, e.g. for deletion or diagnostics.
func ScanSkillDirAll(root string) []corelib.NLSkillEntry {
	return scanSkillDirInternal(root, false)
}

func scanSkillDirInternal(root string, filterPlatform bool) []corelib.NLSkillEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[skill-scanner] cannot read %s: %v", root, err)
		}
		return nil
	}

	var result []corelib.NLSkillEntry
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(root, entry.Name()))
		if err != nil {
			log.Printf("[skill-scanner] skip %s/%s: %v", root, entry.Name(), err)
			continue
		}
		if !info.IsDir() {
			continue
		}
		skillDir := filepath.Join(root, entry.Name())
		parsed, defPath, err := loadSkillFromDir(skillDir, entry.Name())
		if err != nil {
			log.Printf("[skill-scanner] skip %s: load failed: %v", skillDir, err)
			continue
		}

		var hubSkillID string
		if statusData, err := os.ReadFile(filepath.Join(skillDir, "upload_status.json")); err == nil {
			var us uploadStatusFile
			if json.Unmarshal(statusData, &us) == nil && us.SubmissionID != "" {
				hubSkillID = us.SubmissionID
			}
		}
		parsed.HubSkillID = hubSkillID
		if parsed.SkillDir == "" {
			parsed.SkillDir = skillDir
		}
		if parsed.Source == "" {
			parsed.Source = "file"
		}
		if defPath != "" {
			parsed.CreatedAt = fileModTime(defPath)
		}

		// Platform filtering: skip skills incompatible with the current OS.
		if filterPlatform && !isSkillCompatibleWithPlatform(parsed.Platforms, currentPlatform) {
			log.Printf("[skill-scanner] skip %s: incompatible platform (skill=%v, current=%s)", parsed.Name, parsed.Platforms, currentPlatform)
			continue
		}

		result = append(result, *parsed)
	}
	return result
}

func fileModTime(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Now().Format(time.RFC3339)
	}
	return fi.ModTime().Format(time.RFC3339)
}

// requiresPythonFromYAML extracts Python package requirements from the YAML requires field.
func convertSkillYAMLSteps(yamlSteps []SkillYAMLStep) []corelib.NLSkillStep {
	steps := make([]corelib.NLSkillStep, 0, len(yamlSteps))
	for _, s := range yamlSteps {
		params := s.Params
		if params == nil {
			params = make(map[string]interface{})
		}
		if s.TimeoutSeconds > 0 {
			params["timeout"] = float64(s.TimeoutSeconds)
		}
		onError := s.OnError
		if onError == "" {
			if s.ContinueOnErr {
				onError = "continue"
			} else {
				onError = "stop"
			}
		}
		step := corelib.NLSkillStep{
			Action:    s.Action,
			Params:    params,
			OnError:   onError,
			Name:      s.Name,
			Condition: s.Condition,
			When:      s.When,
			Label:     s.Label,
			Capture:   s.Capture,
		}
		if s.Poll != nil {
			step.Poll = &corelib.StepPollConfig{
				Interval:    s.Poll.Interval,
				MaxAttempts: s.Poll.MaxAttempts,
				UntilMatch:  s.Poll.UntilMatch,
				UntilStatus: s.Poll.UntilStatus,
			}
		}
		if s.Loop != nil {
			step.Loop = &corelib.StepLoopConfig{
				MaxIterations: s.Loop.MaxIterations,
				UntilStep:     s.Loop.UntilStep,
				UntilMatch:    s.Loop.UntilMatch,
				OnFailStep:    s.Loop.OnFailStep,
			}
		}
		steps = append(steps, step)
	}
	return steps
}

func convertSkillYAMLOperations(yamlOps []SkillYAMLOperation) []corelib.NLSkillOperation {
	if len(yamlOps) == 0 {
		return nil
	}
	operations := make([]corelib.NLSkillOperation, 0, len(yamlOps))
	for _, op := range yamlOps {
		operations = append(operations, corelib.NLSkillOperation{
			Name:        op.Name,
			Description: op.Description,
			Params:      op.Params,
			Labels:      op.Labels,
		})
	}
	return operations
}

func convertSkillYAMLParams(yamlParams []SkillYAMLParam) []corelib.NLSkillParam {
	if len(yamlParams) == 0 {
		return nil
	}
	params := make([]corelib.NLSkillParam, 0, len(yamlParams))
	for _, p := range yamlParams {
		params = append(params, corelib.NLSkillParam{
			Name:        p.Name,
			Description: p.Description,
			Aliases:     p.Aliases,
			CLIFlag:     p.CLIFlag,
			Default:     p.Default,
			Required:    p.Required,
		})
	}
	return params
}

func requiresPythonFromYAML(req *SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Python
}

// requiresNodeFromYAML extracts Node.js package requirements from the YAML requires field.
func requiresNodeFromYAML(req *SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Node
}

func requiresBinsFromYAML(req *SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Bins
}

// FindSimilarSkill searches all active skills for one similar to the given
// description. Uses BM25 scoring against each skill's description + triggers.
// Returns the best match and its score, or nil if no match exceeds threshold.
func FindSimilarSkill(description string, threshold float64) (*corelib.NLSkillEntry, float64) {
	if strings.TrimSpace(description) == "" {
		return nil, 0
	}

	allSkills := ScanAllSkillDirs()
	if len(allSkills) == 0 {
		return nil, 0
	}

	// Build BM25 index over skill descriptions + triggers.
	type doc struct {
		id   int
		text string
	}
	docs := make([]doc, 0, len(allSkills))
	for i, s := range allSkills {
		if s.Status == "disabled" {
			continue
		}
		text := s.Name + " " + s.Description + " " + strings.Join(s.Triggers, " ")
		docs = append(docs, doc{id: i, text: text})
	}
	if len(docs) == 0 {
		return nil, 0
	}

	// Simple BM25 scoring using tokenized overlap.
	bestIdx := -1
	bestScore := 0.0
	queryTokens := tokenizeSimple(description)
	for _, d := range docs {
		docTokens := tokenizeSimple(d.text)
		score := bm25ScoreSimple(queryTokens, docTokens)
		if score > bestScore {
			bestScore = score
			bestIdx = d.id
		}
	}

	if bestIdx < 0 || bestScore < threshold {
		return nil, bestScore
	}
	return &allSkills[bestIdx], bestScore
}

// tokenizeSimple splits text into lowercase tokens for simple BM25 matching.
func tokenizeSimple(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	seen := make(map[string]bool, len(words))
	var result []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}")
		if w != "" && !seen[w] {
			seen[w] = true
			result = append(result, w)
		}
	}
	return result
}

// bm25ScoreSimple computes a BM25-like score with IDF weighting.
// docCount is the total number of documents in the corpus.
func bm25ScoreSimple(queryTokens, docTokens []string) float64 {
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}
	docSet := make(map[string]int, len(docTokens))
	for _, t := range docTokens {
		docSet[t]++
	}
	var score float64
	for _, qt := range queryTokens {
		if count, ok := docSet[qt]; ok {
			// BM25 TF with k1=1.2 saturation
			tf := float64(count) * 2.2 / (float64(count) + 1.2)
			score += tf
		}
	}
	// Normalize by query length to make scores comparable across queries.
	return score / float64(len(queryTokens))
}

// newSkillBM25Index is a placeholder for future BM25 index integration.
// Currently unused; kept for API compatibility.
// func newSkillBM25Index() interface{} { return nil }

// convertPipelineSteps converts YAML pipeline steps to corelib types.
func convertPipelineSteps(yamlSteps []SkillYAMLPipelineStep) []corelib.SkillPipelineStep {
	if len(yamlSteps) == 0 {
		return nil
	}
	steps := make([]corelib.SkillPipelineStep, len(yamlSteps))
	for i, s := range yamlSteps {
		steps[i] = corelib.SkillPipelineStep{
			Skill:              s.Skill,
			Params:             s.Params,
			Checkpoint:         s.Checkpoint,
			CheckpointMessage:  s.CheckpointMessage,
			ContinueOnFail:     s.ContinueOnFail,
			TimeImpactOnReject: s.TimeImpactOnReject,
		}
	}
	return steps
}

// scanReferences scans the {skillDir}/references/ directory for .md files
// and returns a SkillReference for each. Description is extracted from the
// first markdown heading. Token count is estimated from file size.
func scanReferences(skillDir string) []corelib.SkillReference {
	refDir := filepath.Join(skillDir, "references")
	entries, err := os.ReadDir(refDir)
	if err != nil {
		return nil // no references/ directory; normal case
	}
	var refs []corelib.SkillReference
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		ref := corelib.SkillReference{
			Filename: e.Name(),
		}
		// Extract description from first heading and estimate tokens
		path := filepath.Join(refDir, e.Name())
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			content := string(data)
			ref.Description = firstMarkdownHeading(content)
			// Rough token estimate: ~2.5 bytes per token for mixed CJK/English
			ref.TokenCount = len(data) * 10 / 25
		}
		refs = append(refs, ref)
	}
	return refs
}
