package skill

// requirement_extract.go bridges the old per-field dependency declarations
// to the unified Requirement model, and infers implicit requirements from
// step commands.

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// CheckContext carries execution-environment information that affects how
// requirements are extracted. This is the single place where caller-specific
// context (e.g., "proxy provides OPENAI_API_KEY") is declared.
//
// Callers construct a CheckContext once; ExtractRequirements reads it.
// Checkers and Fixers never see CheckContext — they see the Requirement's
// Provided and Context fields, which ExtractRequirements populates.
type CheckContext struct {
	// ProvidedEnvVars is the set of environment variable names that are
	// provided by the execution context at runtime (e.g., the local proxy
	// injects OPENAI_API_KEY). Requirements for these vars are marked
	// Provided=true and skipped by CheckAll.
	ProvidedEnvVars map[string]bool

	// SkillDir is the skill's working directory. Used to populate
	// Requirement.Context["skill_dir"] for NpmFixer (local install).
	// If empty, falls back to skill.SkillDir.
	SkillDir string
}

// DefaultCheckContext returns an empty execution context. Runner-specific
// virtual env providers, such as the GUI OpenAI proxy, are added by
// BuildRunCheckContextForRunner so prechecks only skip env requirements that
// the selected runner can actually provide.
func DefaultCheckContext() *CheckContext {
	return &CheckContext{
		ProvidedEnvVars: map[string]bool{},
	}
}

// ExtractRequirements extracts all requirements from a skill entry.
// Sources:
//  1. Explicit fields: RequiresPython, RequiresNode, RequiresBins, RequiredEnv, Platforms
//  2. Inferred from step commands: system commands used in bash steps
//
// ctx may be nil for callers that don't need context-aware extraction.
func ExtractRequirements(skill *corelib.NLSkillEntry, ctx ...*CheckContext) []Requirement {
	if skill == nil {
		return nil
	}

	var cc *CheckContext
	if len(ctx) > 0 {
		cc = ctx[0]
	}

	skillDir := skill.SkillDir
	if cc != nil && cc.SkillDir != "" {
		skillDir = cc.SkillDir
	}

	var reqs []Requirement

	for _, pkg := range skill.RequiresPython {
		name, version := splitPkgVersion(pkg)
		reqs = append(reqs, Requirement{Type: "pip", Name: name, Version: version, Source: "explicit"})
	}

	for _, pkg := range skill.RequiresNode {
		name, version := splitPkgVersion(pkg)
		req := Requirement{Type: "npm", Name: name, Version: version, Source: "explicit"}
		// Carry skill_dir so NpmFixer installs locally, not to process cwd.
		if skillDir != "" {
			req.Context = map[string]string{"skill_dir": skillDir}
		}
		reqs = append(reqs, req)
	}

	for _, bin := range skill.RequiresBins {
		bin = strings.TrimSpace(bin)
		if bin == "" {
			continue
		}
		reqs = append(reqs, Requirement{Type: "command", Name: bin, Source: "explicit"})
	}

	for _, env := range extractRequiredEnvNames(skill) {
		if requiredEnvNameLooksLikeCommand(env) {
			reqs = append(reqs, Requirement{Type: "command", Name: env, Source: "explicit"})
			continue
		}
		req := Requirement{Type: "env", Name: env, Source: "explicit"}
		if checkContextProvidesEnv(cc, env) {
			req.Provided = true
		}
		reqs = append(reqs, req)
	}

	if len(skill.Platforms) > 0 {
		reqs = append(reqs, Requirement{Type: "platform", Values: skill.Platforms, Source: "explicit"})
	}

	if skill.RequiresGUI {
		reqs = append(reqs, Requirement{Type: "gui", Name: "display", Source: "explicit"})
	}

	inferred := inferCommandRequirements(skill)
	reqs = append(reqs, inferred...)

	return reqs
}

func extractRequiredEnvNames(skill *corelib.NLSkillEntry) []string {
	if skill == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToUpper(name)
		if seen[key] {
			return
		}
		seen[key] = true
		names = append(names, name)
	}

	for _, env := range skill.RequiredEnv {
		add(env)
	}
	for _, step := range skill.Steps {
		step = NormalizeStepForRunnerCopy(step, "")
		for _, env := range stringListParam(firstNonNilStepParam(step.Params, "required_env", "requires_env", "required_environment")) {
			add(env)
		}
	}
	return names
}

func checkContextProvidesEnv(cc *CheckContext, name string) bool {
	if cc == nil || cc.ProvidedEnvVars == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, candidate := range []string{name, strings.ToUpper(name), strings.ToLower(name)} {
		if cc.ProvidedEnvVars[candidate] {
			return true
		}
	}
	for provided, ok := range cc.ProvidedEnvVars {
		if ok && strings.EqualFold(provided, name) {
			return true
		}
	}
	return false
}

func requiredEnvNameLooksLikeCommand(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	switch name {
	case "python", "python3", "node", "npm", "npx", "pnpm", "yarn",
		"git", "ffmpeg", "pandoc", "magick", "convert", "tesseract",
		"xparse", "xparse-cli":
		return true
	default:
		return false
	}
}

// inferCommandRequirements scans bash step commands for system commands.
// Commands that are covered by explicit RequiresPython/RequiresNode are
// excluded dynamically (not via a hardcoded skip list).
//
// Inferred command requirements are warnings in generic validation; runners
// can promote them when they are checking a skill that is about to execute.
func inferCommandRequirements(skill *corelib.NLSkillEntry) []Requirement {
	coveredRuntimes := buildCoveredRuntimes(skill)

	seen := make(map[string]bool)
	var reqs []Requirement

	for _, step := range skill.Steps {
		step = NormalizeStepForRunnerCopy(step, "")
		if step.Action != "bash" {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}
		for _, first := range extractCommandWords(cmd) {
			first = normalizeInferredCommandName(first)
			seenKey := strings.ToLower(first)
			if first == "" || seen[seenKey] {
				continue
			}
			lower := strings.ToLower(first)
			if shellBuiltins[lower] || coveredRuntimes[lower] || skipInferredCommand(first) {
				continue
			}
			seen[seenKey] = true
			reqs = append(reqs, Requirement{Type: "command", Name: first, Source: "inferred"})
		}
	}
	return reqs
}

func normalizeInferredCommandName(command string) string {
	return strings.Trim(strings.TrimSpace(command), "\"'`")
}

func extractCommandWords(command string) []string {
	seen := map[string]bool{}
	var words []string
	for _, line := range commandPrecheckLines(command) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, segment := range splitCommandSegments(line) {
			for _, first := range extractCommandWordsFromSegment(strings.TrimSpace(segment)) {
				if first == "" {
					continue
				}
				key := strings.ToLower(first)
				if seen[key] {
					continue
				}
				seen[key] = true
				words = append(words, first)
			}
		}
	}
	return words
}

func extractCommandWordsFromSegment(segment string) []string {
	fields := splitCommandFields(segment)
	commandIndex := firstShellCommandFieldIndex(fields)
	if commandIndex < 0 {
		return nil
	}
	var words []string
	for commandIndex >= 0 && commandIndex < len(fields) {
		first := normalizeInferredCommandName(fields[commandIndex])
		first = strings.TrimLeft(first, "(")
		first = strings.TrimRight(first, ")")
		if first == "" {
			return words
		}
		words = append(words, first)
		nextIndex := wrappedCommandIndex(fields, commandIndex)
		if nextIndex <= commandIndex {
			return words
		}
		commandIndex = nextIndex
	}
	return words
}

func wrappedCommandIndex(fields []string, commandIndex int) int {
	if commandIndex < 0 || commandIndex >= len(fields) {
		return -1
	}
	switch strings.ToLower(normalizeInferredCommandName(fields[commandIndex])) {
	case "env":
		for i := commandIndex + 1; i < len(fields); i++ {
			field := strings.TrimSpace(fields[i])
			if field == "" {
				continue
			}
			if field == "--" || isShellEnvAssignmentField(field) {
				continue
			}
			if envOptionConsumesNext(field) {
				i++
				continue
			}
			if strings.HasPrefix(field, "-") {
				continue
			}
			return i
		}
	case "exec", "time", "nohup", "sudo", "doas":
		return nextWrappedCommandField(fields, commandIndex+1, strings.ToLower(normalizeInferredCommandName(fields[commandIndex])))
	}
	return -1
}

func nextWrappedCommandField(fields []string, start int, wrapper string) int {
	for i := start; i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		if field == "--" || isShellEnvAssignmentField(field) {
			continue
		}
		if wrapperOptionConsumesNext(wrapper, field) {
			i++
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return i
	}
	return -1
}

func wrapperOptionConsumesNext(wrapper, field string) bool {
	field = strings.TrimSpace(field)
	if strings.Contains(field, "=") {
		return false
	}
	switch wrapper {
	case "exec":
		return field == "-a"
	case "time":
		return field == "-f" || field == "--format" || field == "-o" || field == "--output"
	case "sudo":
		switch field {
		case "-A", "-b", "-B", "-C", "-c", "-D", "-g", "-h", "-p", "-R", "-r", "-T", "-t", "-U", "-u":
			return true
		case "--askpass", "--close-from", "--chdir", "--group", "--host", "--prompt", "--role", "--type", "--other-user", "--user":
			return true
		}
	case "doas":
		return field == "-u"
	}
	return false
}

func envOptionConsumesNext(field string) bool {
	field = strings.TrimSpace(field)
	switch field {
	case "-u", "--unset", "-C", "--chdir", "-S", "--split-string":
		return true
	default:
		return false
	}
}

func skipInferredCommand(command string) bool {
	command = strings.Trim(strings.TrimSpace(command), "\"'`")
	if command == "" {
		return true
	}
	if strings.ContainsAny(command, "{}$") {
		return true
	}
	if strings.HasPrefix(command, "-") {
		return true
	}
	if strings.HasPrefix(command, ".") || strings.HasPrefix(command, "/") || strings.HasPrefix(command, `\`) {
		return true
	}
	if strings.Contains(command, "/") || strings.Contains(command, `\`) {
		return true
	}
	return false
}

// buildCoveredRuntimes returns the set of command names that are already
// covered by the skill's explicit dependency declarations. This is data-driven:
// if RequiresPython is non-empty, python/python3/pip/pip3 are covered.
// If RequiresNode is non-empty, node/npm/npx are covered.
// RequiresBins covers the exact command names it declares.
//
// When a new dependency type is added (e.g., RequiresCargo → cargo/rustc),
// add a block here. This is O(dependency_types), not O(commands).
func buildCoveredRuntimes(skill *corelib.NLSkillEntry) map[string]bool {
	covered := make(map[string]bool)
	if len(skill.RequiresPython) > 0 {
		for _, cmd := range []string{"python", "python3", "pip", "pip3"} {
			covered[cmd] = true
		}
	}
	if len(skill.RequiresNode) > 0 {
		for _, cmd := range []string{"node", "npm", "npx"} {
			covered[cmd] = true
		}
	}
	for _, bin := range skill.RequiresBins {
		if bin = strings.ToLower(strings.TrimSpace(bin)); bin != "" {
			covered[bin] = true
		}
	}
	return covered
}

// shellBuiltins are commands that exist in every shell and should never be
// inferred as external requirements.
var shellBuiltins = map[string]bool{
	"echo": true, "cd": true, "mkdir": true, "rm": true, "cp": true,
	"mv": true, "cat": true, "ls": true, "pwd": true, "export": true,
	"set": true, "if": true, "for": true, "while": true, "test": true,
	"true": true, "false": true, "exit": true, "return": true,
	"source": true, "chmod": true, "chown": true, "touch": true,
	"head": true, "tail": true, "grep": true, "sed": true, "awk": true,
	"sort": true, "uniq": true, "wc": true, "tee": true, "xargs": true,
	"then": true, "else": true, "elif": true, "fi": true, "do": true,
	"done": true, "case": true, "esac": true, "function": true,
	"command": true, "exec": true, "printf": true, "read": true,
	"unset": true, "alias": true, "unalias": true, "type": true,
	"trap": true, "pushd": true, "popd": true, "dirs": true,
	"declare": true, "local": true, "let": true,
}

// splitPkgVersion splits "pdfplumber>=0.9" into ("pdfplumber", ">=0.9").
func splitPkgVersion(pkg string) (string, string) {
	for _, sep := range []string{">=", "<=", "==", "!=", ">", "<", "~="} {
		if idx := strings.Index(pkg, sep); idx > 0 {
			return strings.TrimSpace(pkg[:idx]), pkg[idx:]
		}
	}
	return strings.TrimSpace(pkg), ""
}

// --- Built-in Checkers ---

// DefaultRegistry returns a Registry pre-loaded with all built-in checkers
// and fixers. This is a factory — each call returns a fresh instance.
//
// Callers should NOT configure checkers on the returned registry. All
// caller-specific context goes through CheckContext → ExtractRequirements →
// Requirement fields (Provided, Context). The registry is context-free.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&PipChecker{})
	r.Register(&NpmChecker{})
	r.Register(&EnvVarChecker{})
	r.Register(&CommandChecker{})
	r.Register(&PlatformChecker{})
	r.Register(&GUIChecker{})
	// Fixers — only for types that support auto-repair.
	r.RegisterFixer(&PipFixer{})
	r.RegisterFixer(&NpmFixer{})
	return r
}

// --- Checkers (pure validation, no side effects) ---

type PipChecker struct{}

func (c *PipChecker) Type() string { return "pip" }
func (c *PipChecker) Check(req Requirement) *Violation {
	python := findPythonExecutable()
	if python == "" {
		return &Violation{Requirement: req, Message: "Python 未安装，无法检查 pip 包 " + req.Name, Severity: "error"}
	}
	if !checkPipInstalled(python, req.Name) {
		return &Violation{Requirement: req, Message: "Python 包 " + req.Name + req.Version + " 未安装", Severity: "error"}
	}
	return nil
}

type NpmChecker struct{}

func (c *NpmChecker) Type() string { return "npm" }
func (c *NpmChecker) Check(req Requirement) *Violation {
	if !commandExists("npm") {
		return &Violation{Requirement: req, Message: "npm 未安装，无法检查 Node 包 " + req.Name, Severity: "error"}
	}
	// Check in skill_dir if available (local install), then global.
	dir := ""
	if req.Context != nil {
		dir = req.Context["skill_dir"]
	}
	if !checkNpmInstalledInDir(req.Name, dir) {
		return &Violation{Requirement: req, Message: "Node 包 " + req.Name + " 未安装", Severity: "error"}
	}
	return nil
}

// EnvVarChecker validates that required environment variables are set.
// Requirements with Provided=true are already skipped by CheckAll, so
// this checker doesn't need SkipNames — the filtering happens at the
// Requirement level, not the Checker level.
type EnvVarChecker struct{}

func (c *EnvVarChecker) Type() string { return "env" }
func (c *EnvVarChecker) Check(req Requirement) *Violation {
	if envLookup(req.Name) == "" {
		return &Violation{Requirement: req, Message: "环境变量 " + req.Name + " 未设置", Severity: "error"}
	}
	return nil
}

type CommandChecker struct{}

func (c *CommandChecker) Type() string { return "command" }
func (c *CommandChecker) Check(req Requirement) *Violation {
	if commandExists(req.Name) {
		return nil
	}
	// Windows: python3 → python fallback.
	if runtime.GOOS == "windows" && req.Name == "python3" && commandExists("python") {
		return nil
	}
	severity := "error"
	if req.Source == "inferred" {
		severity = "warning"
	}
	return &Violation{Requirement: req, Message: "命令 " + req.Name + " 未找到", Severity: severity}
}

type PlatformChecker struct{}

func (c *PlatformChecker) Type() string { return "platform" }
func (c *PlatformChecker) Check(req Requirement) *Violation {
	platforms := req.Values
	if len(platforms) == 0 {
		return nil
	}
	currentPlatform := mapGOOSToPlatform(runtime.GOOS)
	if !isSkillCompatibleWithPlatform(platforms, currentPlatform) {
		return &Violation{
			Requirement: req,
			Message:     "Skill 不支持当前平台 " + currentPlatform + "（支持: " + strings.Join(platforms, ", ") + "）",
			Severity:    "error",
		}
	}
	return nil
}

// GUIChecker validates that a GUI display environment is available.
// On Linux, this checks for DISPLAY or WAYLAND_DISPLAY. On other platforms
// a GUI is always assumed available.
type GUIChecker struct{}

func (c *GUIChecker) Type() string { return "gui" }
func (c *GUIChecker) Check(req Requirement) *Violation {
	if runtime.GOOS != "linux" {
		return nil
	}
	if envLookup("DISPLAY") != "" || envLookup("WAYLAND_DISPLAY") != "" {
		return nil
	}
	return &Violation{
		Requirement: req,
		Message:     "Skill 需要 GUI 环境，但当前 Linux 未检测到 DISPLAY 或 WAYLAND_DISPLAY",
		Severity:    "error",
	}
}

// --- Fixers (side effects, separate from checkers) ---

type PipFixer struct{}

func (f *PipFixer) Type() string { return "pip" }
func (f *PipFixer) Fix(req Requirement) error {
	python := findPythonExecutable()
	if python == "" {
		return fmt.Errorf("python not found, cannot install %s", req.Name)
	}
	return installPipPkg(python, req.Name+req.Version)
}

// NpmFixer installs npm packages. It reads req.Context["skill_dir"] to
// determine the install directory. If set, packages are installed locally
// to the skill directory (no elevated permissions needed). If empty,
// falls back to the process working directory.
type NpmFixer struct{}

func (f *NpmFixer) Type() string { return "npm" }
func (f *NpmFixer) Fix(req Requirement) error {
	skillDir := ""
	if req.Context != nil {
		skillDir = req.Context["skill_dir"]
	}
	return installNpmPkgInDir(req.Name+req.Version, skillDir)
}
