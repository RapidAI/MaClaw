package skill

// requirement.go defines the unified Skill Requirements mechanism.
//
// Design: All dependency types are expressed as Requirement values. A Registry
// of Checker implementations validates them. The Runner calls
// Registry.CheckAll() — it doesn't know which checkers exist.
//
// Check and Fix are separate concerns:
//   - Checker only validates (pure function, no side effects)
//   - Fixer is an optional, separate interface for auto-repair
//
// This separation means:
//   - Checkers that can't auto-fix don't need to implement a stub method
//   - Fix strategy can vary by context (venv install vs global install)
//     without changing the checker
//   - New fix strategies can be registered independently of checkers
//
// Boundary: The requirement system validates system-level preconditions
// (packages installed, env vars set, platform compatible, commands available).
// Execution-context checks (file references in step commands, credential
// files for remote mounting) stay in the Runner because they depend on
// runtime state (skillDir resolution, template variable substitution) that
// isn't available at requirement-extraction time.

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Requirement is the unified representation of a skill precondition.
type Requirement struct {
	Type    string   // checker type key: "pip", "npm", "env", "command", "platform", "gui"
	Name    string   // package name / variable name / command name
	Version string   // version constraint (optional, e.g. ">=0.9")
	Values  []string // multi-value field (e.g. platform list); nil for single-value types

	// Source indicates where this requirement came from:
	//   "explicit" = declared in skill.yaml / SKILL.md frontmatter
	//   "inferred" = extracted from step commands automatically
	Source string

	// Provided marks requirements that are satisfied by the selected execution
	// context rather than the system environment. For example, the GUI runner
	// can satisfy OPENAI_API_KEY by starting its local proxy, while the TUI
	// runner must see a real env var or caller-supplied extra_env.
	//
	// This replaces per-caller SkipNames configuration. The decision of what
	// is "provided" lives in ExtractRequirements (single data source), not
	// in each caller's Registry setup.
	Provided bool

	// Context carries execution-environment metadata that Fixers need but
	// Checkers don't. For example, "skill_dir" tells NpmFixer where to run
	// `npm install` (local to the skill directory, not the process cwd).
	//
	// Keys are defined by convention per requirement type:
	//   "npm" → "skill_dir": directory for local npm install
	Context map[string]string
}

// Violation describes an unsatisfied precondition.
type Violation struct {
	Requirement Requirement
	Message     string // user-friendly error message
	Severity    string // "error" (blocks execution) | "warning" (logged only)
}

// Checker validates one type of Requirement. Implementations are pure
// validators — they check whether a requirement is satisfied, nothing more.
//
// This is the extension point for new dependency types: implement Checker,
// call Registry.Register(). No changes to the Runner.
type Checker interface {
	// Type returns the Requirement.Type this checker handles.
	Type() string
	// Check verifies whether a requirement is satisfied.
	// Returns nil when satisfied, a Violation when not.
	Check(req Requirement) *Violation
}

// Fixer can auto-repair a specific requirement type. This is separate from
// Checker because:
//  1. Not all requirement types support auto-fix (commands, platforms don't)
//  2. Fix strategy may vary by context (the same pip package might be
//     installed globally, in a venv, or in the skill directory)
//  3. A Fixer can be registered/replaced independently of its Checker
type Fixer interface {
	// Type returns the Requirement.Type this fixer handles.
	Type() string
	// Fix attempts to satisfy the requirement. Returns nil on success.
	Fix(req Requirement) error
}

// FixProgressCallback reports auto-fix progress to the caller.
// Used by FixAllWithProgress to provide real-time feedback during
// potentially long-running operations (pip install, npm install).
type FixProgressCallback func(message string)

// Registry is the single dispatch point for requirement checking and fixing.
type Registry struct {
	mu       sync.RWMutex
	checkers map[string]Checker
	fixers   map[string]Fixer
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		checkers: make(map[string]Checker),
		fixers:   make(map[string]Fixer),
	}
}

// Register adds a Checker to the registry.
func (r *Registry) Register(c Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[c.Type()] = c
}

// RegisterFixer adds a Fixer to the registry. Fixers are optional —
// requirement types without a fixer simply can't be auto-repaired.
func (r *Registry) RegisterFixer(f Fixer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixers[f.Type()] = f
}

// CheckAll validates all requirements, returning all violations.
// Requirements with Provided=true are skipped — they are satisfied by the
// execution context, not the system environment.
func (r *Registry) CheckAll(reqs []Requirement) []Violation {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var violations []Violation
	for _, req := range reqs {
		if req.Provided {
			continue
		}
		checker, ok := r.checkers[req.Type]
		if !ok {
			violations = append(violations, Violation{
				Requirement: req,
				Message:     fmt.Sprintf("unknown requirement type %q for %q", req.Type, req.Name),
				Severity:    "warning",
			})
			continue
		}
		if v := checker.Check(req); v != nil {
			violations = append(violations, *v)
		}
	}
	return violations
}

// FixAll attempts to auto-fix violations that have a registered Fixer.
// After each fix, re-checks with the Checker to verify the fix worked.
// Returns violations that remain after fix attempts.
//
// FixAll accepts all violations (not just errors). It decides internally
// which violations to attempt fixing based on whether a Fixer is registered.
// Callers should not pre-filter — pass the full CheckAll result.
func (r *Registry) FixAll(violations []Violation) []Violation {
	return r.FixAllWithProgress(violations, nil)
}

// FixAllWithProgress is like FixAll but reports progress via callback.
// The callback receives messages like "正在安装 Python 包 pdfplumber..."
// during potentially long-running fix operations.
//
// Mechanism: For transient failures (network timeout, DNS resolution, connection
// refused), retries once with 3-second backoff. Permanent failures (package not
// found, version conflict, permission denied) fail immediately.
func (r *Registry) FixAllWithProgress(violations []Violation, progress FixProgressCallback) []Violation {
	// Snapshot the fixers and checkers under lock, then release immediately.
	// Fix/Check operations spawn subprocesses that may take seconds — holding
	// the lock during that time would block concurrent CheckAll/Register calls.
	r.mu.RLock()
	fixerSnapshot := make(map[string]Fixer, len(r.fixers))
	for k, v := range r.fixers {
		fixerSnapshot[k] = v
	}
	checkerSnapshot := make(map[string]Checker, len(r.checkers))
	for k, v := range r.checkers {
		checkerSnapshot[k] = v
	}
	r.mu.RUnlock()

	var remaining []Violation
	for _, v := range violations {
		fixer, ok := fixerSnapshot[v.Requirement.Type]
		if !ok {
			remaining = append(remaining, v)
			continue
		}
		if progress != nil {
			progress(fixProgressMessage(v.Requirement))
		}
		err := fixer.Fix(v.Requirement)
		if err != nil && isTransientFixError(err) {
			// Transient failure: retry once after 3s backoff.
			log.Printf("[requirement] transient fix failure for %s:%s, retrying in 3s: %v", v.Requirement.Type, v.Requirement.Name, err)
			if progress != nil {
				progress(fmt.Sprintf("⏳ 安装 %s 遇到网络问题，3秒后重试...", v.Requirement.Name))
			}
			time.Sleep(3 * time.Second)
			err = fixer.Fix(v.Requirement)
		}
		if err != nil {
			log.Printf("[requirement] fix failed for %s:%s: %v", v.Requirement.Type, v.Requirement.Name, err)
			failed := v
			failed.Message = formatFixFailureMessage(v.Requirement, err)
			remaining = append(remaining, failed)
			continue
		}
		// Verify the fix actually worked by re-checking.
		checker, hasChecker := checkerSnapshot[v.Requirement.Type]
		if hasChecker {
			if recheck := checker.Check(v.Requirement); recheck != nil {
				log.Printf("[requirement] fix for %s:%s returned success but re-check still fails: %s",
					v.Requirement.Type, v.Requirement.Name, recheck.Message)
				remaining = append(remaining, *recheck)
				continue
			}
		}
		log.Printf("[requirement] fixed %s:%s", v.Requirement.Type, v.Requirement.Name)
	}
	return remaining
}

func formatFixFailureMessage(req Requirement, err error) string {
	detail := strings.TrimSpace(fmt.Sprint(err))
	if detail == "" {
		detail = "unknown error"
	}

	// Classify the error for actionable diagnostics
	diag := ClassifyInstallError(detail)
	diagSuffix := FormatInstallDiagnosis(diag)

	switch req.Type {
	case "pip":
		name := strings.TrimSpace(req.Name + req.Version)
		if name == "" {
			return fmt.Sprintf("failed to install Python package dependency: %s [action: install_dependency] Install the missing Python dependency, then retry the skill.%s", detail, diagSuffix)
		}
		return fmt.Sprintf("failed to install Python package %s: %s [action: install_dependency] Install Python package %s, then retry the skill.%s", name, detail, name, diagSuffix)
	case "npm":
		name := strings.TrimSpace(req.Name + req.Version)
		if name == "" {
			return fmt.Sprintf("failed to install Node package dependency: %s [action: install_dependency] Install the missing Node dependency, then retry the skill.%s", detail, diagSuffix)
		}
		return fmt.Sprintf("failed to install Node package %s: %s [action: install_dependency] Install Node package %s, then retry the skill.%s", name, detail, name, diagSuffix)
	case "command":
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return fmt.Sprintf("failed to install command: %s [action: install_dependency] Install the command manually.%s", detail, diagSuffix)
		}
		return fmt.Sprintf("failed to install command %s: %s [action: install_dependency] %s%s", name, detail, platformInstallHint(name), diagSuffix)
	default:
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return fmt.Sprintf("failed to repair dependency: %s [action: inspect_skill] Inspect the skill requirements and execution environment.%s", detail, diagSuffix)
		}
		return fmt.Sprintf("failed to repair dependency %s: %s [action: inspect_skill] Inspect the skill requirements and execution environment.%s", name, detail, diagSuffix)
	}
}

// platformInstallHint returns a platform-aware manual install suggestion for a command.
func platformInstallHint(name string) string {
	recipe, ok := knownCommandInstallRecipes[strings.ToLower(name)]
	if !ok {
		return fmt.Sprintf("Install %s and ensure it is available on PATH.", name)
	}
	return recipe.ManualHint
}

// isTransientFixError distinguishes transient network errors (retry-worthy)
// from permanent errors (package not found, version conflict, permissions).
// This is the single decision point — all Fixers benefit from this classification.
//
// False positive cost is low (one 3s retry on a deterministic error), but we
// still use specific patterns to minimize wasted time.
func isTransientFixError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Network/connectivity issues — transient, worth retrying.
	transientPatterns := []string{
		"connection refused", "connection reset", "connection timed out",
		"timeout", "timed out",
		"dns", "name resolution",
		"network is unreachable", "network unreachable",
		"no route to host",
		"reset by peer",
		"temporary failure in name resolution",
		"broken pipe",
		"503 service", "502 bad gateway",
		"429", "rate limit", "too many requests",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// fixProgressMessage generates a user-friendly progress message for a fix operation.
func fixProgressMessage(req Requirement) string {
	switch req.Type {
	case "pip":
		return fmt.Sprintf("📦 正在安装 Python 包 %s%s...", req.Name, req.Version)
	case "npm":
		return fmt.Sprintf("📦 正在安装 Node 包 %s%s...", req.Name, req.Version)
	default:
		return fmt.Sprintf("🔧 正在修复依赖 %s...", req.Name)
	}
}

// HasFixer returns true if a fixer is registered for the given type.
func (r *Registry) HasFixer(typ string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.fixers[typ]
	return ok
}

// FilterErrors returns only error-severity violations.
func FilterErrors(violations []Violation) []Violation {
	var errors []Violation
	for _, v := range violations {
		if v.Severity == "error" {
			errors = append(errors, v)
		}
	}
	return errors
}

// PromoteRunnerBlockingViolations tightens requirement results for an imminent
// skill execution. Inferred command requirements stay warnings in generic
// validation because static command parsing can be conservative, but a missing
// command in the selected executable steps will fail immediately at runtime.
func PromoteRunnerBlockingViolations(violations []Violation) []Violation {
	if len(violations) == 0 {
		return nil
	}
	promoted := make([]Violation, len(violations))
	copy(promoted, violations)
	for i := range promoted {
		req := promoted[i].Requirement
		// Promote inferred command requirements to errors (they'll definitely
		// fail at runtime if missing).
		if promoted[i].Severity == "warning" && req.Type == "command" && req.Source == "inferred" {
			promoted[i].Severity = "error"
		}
		// Demote inferred_script pip requirements to warnings. These are
		// best-effort guesses from scanning script files — false positives
		// (local modules, uncommon package names) should not block execution.
		// Layer 2 (runtime auto-install) handles any remaining misses.
		if promoted[i].Severity == "error" && req.Type == "pip" && req.Source == "inferred_script" {
			promoted[i].Severity = "warning"
		}
		// Demote manifest_file npm requirements to warnings. npm's `npm list`
		// returns exit code 1 for peer dependency warnings even when the package
		// IS installed (npm 7+ known issue). False positives would block skill
		// execution unnecessarily. Layer 2 (runtime auto-install) handles true
		// misses if the package really isn't available at runtime.
		if promoted[i].Severity == "error" && req.Type == "npm" && req.Source == "manifest_file" {
			promoted[i].Severity = "warning"
		}
	}
	return promoted
}

// FormatViolations builds a user-friendly error message from violations.
func FormatViolations(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}
	var parts []string
	for _, v := range violations {
		if message := FormatViolation(v); message != "" {
			parts = append(parts, message)
		}
	}
	return strings.Join(parts, "\n")
}

// FormatViolation renders one requirement violation with a runner-action hint.
// Checkers keep their messages small and factual; this function adds the
// consistent next-step marker used by GUI/TUI skill runners.
func FormatViolation(v Violation) string {
	message := stableRequirementViolationMessage(v)
	if message == "" {
		return ""
	}
	if strings.Contains(message, "[action:") {
		return message
	}
	if hint := RequirementActionHint(v.Requirement); hint != "" {
		return message + " " + hint
	}
	return message
}

func stableRequirementViolationMessage(v Violation) string {
	if strings.Contains(v.Message, "[action:") {
		return strings.TrimSpace(v.Message)
	}
	req := v.Requirement
	switch req.Type {
	case "command":
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return "required command was not found on PATH"
		}
		return fmt.Sprintf("required command %s was not found on PATH", name)
	case "env":
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return "required environment variable is not set"
		}
		return fmt.Sprintf("required environment variable %s is not set", name)
	case "pip":
		name := strings.TrimSpace(req.Name + req.Version)
		if name == "" {
			return "required Python package is not installed"
		}
		return fmt.Sprintf("required Python package %s is not installed", name)
	case "npm":
		name := strings.TrimSpace(req.Name + req.Version)
		if name == "" {
			return "required Node package is not installed"
		}
		return fmt.Sprintf("required Node package %s is not installed", name)
	case "platform":
		currentPlatform := mapGOOSToPlatform(runtime.GOOS)
		if len(req.Values) == 0 {
			return "current platform is not supported by this skill"
		}
		return fmt.Sprintf("current platform %s is not supported by this skill; supported platforms: %s", currentPlatform, strings.Join(req.Values, ", "))
	case "gui":
		return "this skill requires a GUI-capable environment"
	default:
		return strings.TrimSpace(v.Message)
	}
}

func RequirementActionHint(req Requirement) string {
	switch req.Type {
	case "command":
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return "[action: install_dependency] Install the missing command or update the skill command."
		}
		return fmt.Sprintf("[action: install_dependency] Install %s and ensure it is available on PATH, or update the skill to use an available command.", name)
	case "pip", "npm":
		if strings.TrimSpace(req.Name) == "" {
			return "[action: install_dependency] Install the missing package dependency."
		}
		return fmt.Sprintf("[action: install_dependency] Install package %s%s, then retry the skill.", req.Name, req.Version)
	case "env":
		if strings.TrimSpace(req.Name) == "" {
			return "[action: provide_env] Provide the required environment variable before running the skill."
		}
		return fmt.Sprintf("[action: provide_env] Provide environment variable %s via run env/config, then retry.", req.Name)
	case "platform":
		return "[action: inspect_skill] Run this skill on a supported platform or adjust its platforms declaration."
	case "gui":
		return "[action: open_gui] Run this skill in a GUI-capable environment."
	default:
		return "[action: inspect_skill] Inspect the skill requirements and execution environment."
	}
}
