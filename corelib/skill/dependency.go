package skill

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ────────────────────────────────────────────────────────────────────────────
// Skill Dependency Resolution
//
// Used by MaClaw App and Pipeline skills to declare and resolve dependencies
// on other skills by their stable skill_id + version constraint.
// ────────────────────────────────────────────────────────────────────────────

// SkillDependency declares a dependency on another skill.
type SkillDependency struct {
	// SkillID is the publisher.skill-name identifier (primary lookup key).
	SkillID string `json:"skill_id" yaml:"skill_id"`
	// Version is a semver constraint string (e.g. ">=1.2.0", "^1.0.0", "*").
	// Empty = any version.
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	// Required indicates whether the dependency is mandatory.
	// When true, the App/Pipeline fails if the dependency cannot be resolved.
	// When false, the system degrades gracefully.
	Required bool `json:"required,omitempty" yaml:"required,omitempty"`
	// Name is the human-readable display name (informational only, not for matching).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// InstallRef is the legacy UUID or URL for backward-compatible download.
	InstallRef string `json:"install_ref,omitempty" yaml:"install_ref,omitempty"`
}

// DependencyResolution is the result of resolving a single dependency.
type DependencyResolution struct {
	Dependency SkillDependency
	// Resolved is the matched local skill entry (nil if not found).
	Resolved *corelib.NLSkillEntry
	// Satisfied indicates whether the version constraint is met.
	Satisfied bool
	// NeedsUpgrade is true when the skill is installed but version is too old.
	NeedsUpgrade bool
	// Error is non-nil when resolution completely failed (skill not found + required).
	Error error
}

// ResolveDependency finds the best local match for a single dependency.
// Lookup priority:
//  1. Exact skill_id match (entry.SkillID == dep.SkillID)
//  2. QualifiedID match (entry.QualifiedID() == dep.SkillID)
//  3. Name/DirName match (legacy fallback for deps without skill_id)
//
// After finding a match, the version constraint is checked.
func ResolveDependency(dep SkillDependency, installed []corelib.NLSkillEntry) DependencyResolution {
	result := DependencyResolution{Dependency: dep}

	if dep.SkillID == "" {
		result.Error = fmt.Errorf("dependency has no skill_id")
		return result
	}

	// Find matching installed skill
	var match *corelib.NLSkillEntry
	for i := range installed {
		entry := &installed[i]
		// Priority 1: exact SkillID match
		if entry.SkillID != "" && strings.EqualFold(entry.SkillID, dep.SkillID) {
			match = entry
			break
		}
		// Priority 2: QualifiedID match
		if strings.EqualFold(entry.QualifiedID(), dep.SkillID) {
			match = entry
			break
		}
		// Priority 3: Name match (legacy)
		if entry.MatchesName(dep.SkillID) {
			match = entry
			// Don't break — a later exact SkillID match takes precedence
		}
	}

	if match == nil {
		if dep.Required {
			result.Error = fmt.Errorf("required dependency %s not found locally", dep.SkillID)
		}
		return result
	}

	result.Resolved = match

	// Check version constraint
	if dep.Version == "" || dep.Version == "*" {
		result.Satisfied = true
		return result
	}

	constraint, err := ParseVersionConstraint(dep.Version)
	if err != nil {
		result.Error = fmt.Errorf("invalid version constraint %q: %w", dep.Version, err)
		return result
	}

	// Use the skill's version: prefer Version (semver from skill.yaml),
	// fall back to HubVersion (may be semver or Hub-internal int counter).
	installedVersion := match.Version
	if installedVersion == "" {
		installedVersion = match.HubVersion
	}

	if constraint.Satisfies(installedVersion) {
		result.Satisfied = true
	} else {
		result.NeedsUpgrade = true
		if dep.Required {
			result.Error = fmt.Errorf("dependency %s requires %s but installed version is %s",
				dep.SkillID, dep.Version, installedVersion)
		}
	}

	return result
}

// ResolveDependencies resolves all dependencies and returns the results.
// Does not perform any installation — only checks local availability.
func ResolveDependencies(deps []SkillDependency, installed []corelib.NLSkillEntry) []DependencyResolution {
	results := make([]DependencyResolution, len(deps))
	for i, dep := range deps {
		results[i] = ResolveDependency(dep, installed)
	}
	return results
}

// UnresolvedDependencies returns only the dependencies that could not be satisfied locally.
func UnresolvedDependencies(results []DependencyResolution) []DependencyResolution {
	var unresolved []DependencyResolution
	for _, r := range results {
		if !r.Satisfied {
			unresolved = append(unresolved, r)
		}
	}
	return unresolved
}
