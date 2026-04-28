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
	"strings"
	"sync"
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

	// Provided marks requirements that are satisfied by the execution context
	// rather than the system environment. For example, OPENAI_API_KEY is
	// provided by the local proxy at runtime — the checker should skip it.
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
//   1. Not all requirement types support auto-fix (commands, platforms don't)
//   2. Fix strategy may vary by context (the same pip package might be
//      installed globally, in a venv, or in the skill directory)
//   3. A Fixer can be registered/replaced independently of its Checker
type Fixer interface {
	// Type returns the Requirement.Type this fixer handles.
	Type() string
	// Fix attempts to satisfy the requirement. Returns nil on success.
	Fix(req Requirement) error
}

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
	r.mu.RLock()
	defer r.mu.RUnlock()

	var remaining []Violation
	for _, v := range violations {
		fixer, ok := r.fixers[v.Requirement.Type]
		if !ok {
			remaining = append(remaining, v)
			continue
		}
		if err := fixer.Fix(v.Requirement); err != nil {
			log.Printf("[requirement] fix failed for %s:%s: %v", v.Requirement.Type, v.Requirement.Name, err)
			remaining = append(remaining, v)
			continue
		}
		// Verify the fix actually worked by re-checking.
		checker, hasChecker := r.checkers[v.Requirement.Type]
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

// FormatViolations builds a user-friendly error message from violations.
func FormatViolations(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}
	var parts []string
	for _, v := range violations {
		parts = append(parts, v.Message)
	}
	return strings.Join(parts, "; ")
}
