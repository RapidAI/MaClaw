package skill

import (
	"fmt"
	"path/filepath"
	"strings"
)

// UploadPreflightResult is the structured outcome of preparing a skill
// directory for publishing to SkillMarket / HubCenter / Hub.
//
// It is produced by PrepareSkillForUpload and is intentionally
// platform-agnostic so the GUI tool handler, the TUI tool handler, and the
// lifecycle queue all share one preparation mechanism instead of each
// re-implementing path sanitization and completeness checks.
type UploadPreflightResult struct {
	SkillName string `json:"skill_name"`
	SkillDir  string `json:"skill_dir"`
	SkillID   string `json:"skill_id,omitempty"` // Validated skill ID (empty if not declared)

	// AutoFixed lists the safe, reversible rewrites applied to the skill dir
	// in place (e.g. an absolute path under the skill dir rewritten to
	// {baseDir}/..., a home-dir path rewritten to $HOME/...). The on-disk
	// definition is modified and a .bak backup is created for each touched file.
	AutoFixed []PortabilityChange `json:"auto_fixed,omitempty"`

	// BlockingPaths lists absolute paths that remain after auto-fix and cannot
	// be rewritten automatically because they point outside the skill package
	// and outside the user's home directory (machine-specific paths). These
	// must be converted by the agent to a {baseDir} macro, a relative path, or
	// a runtime parameter before the skill is portable.
	BlockingPaths []PortabilityPathIssue `json:"blocking_paths,omitempty"`

	// MissingFiles lists local script/file references that the skill commands
	// depend on but are not bundled inside the skill directory. Such a skill
	// would fail on any other machine.
	MissingFiles []string `json:"missing_files,omitempty"`

	// Warnings carries non-blocking portability warnings (e.g. platform/shell
	// mismatches, encoding diagnostics) for the agent's awareness.
	Warnings []string `json:"warnings,omitempty"`

	// Report is the full re-validated portability report after auto-fix.
	Report *PortabilityReport `json:"report,omitempty"`
}

// PortabilityPathIssue describes a single blocking absolute path together with
// the suggestion on how to make it portable.
type PortabilityPathIssue struct {
	Path       string `json:"path"`
	File       string `json:"file"`
	Suggestion string `json:"suggestion"`
}

type UploadPreflightOptions struct {
	AutoFix bool
}

// Portable reports whether the skill is safe to upload: no remaining absolute
// paths and no missing bundled files.
func (r *UploadPreflightResult) Portable() bool {
	if r == nil {
		return false
	}
	return len(r.BlockingPaths) == 0 && len(r.MissingFiles) == 0
}

// PrepareSkillForUpload runs the pre-upload portability gate against a skill
// directory. It is the single source of truth for "is this skill safe to share
// with other machines" used by both GUI and TUI upload handlers.
//
// Steps:
//  1. Auto-fix safe portability issues in place (absolute paths inside the
//     skill dir -> {baseDir}, home-dir paths -> $HOME, backslash separators,
//     missing platforms). This persists to the skill definition with .bak
//     backups, exactly like manage_skill(action="validate", auto_fix=true).
//  2. Re-validate and collect any absolute paths that could NOT be auto-fixed
//     (machine-specific paths outside the package and outside $HOME).
//  3. Verify that every local file/script the commands reference is actually
//     bundled in the skill directory.
//
// The skill is considered upload-ready when result.Portable() is true. The
// caller is responsible for the security scan, quality score, and the actual
// network submission.
func PrepareSkillForUpload(skillDir string) (*UploadPreflightResult, error) {
	return PrepareSkillForUploadWithOptions(skillDir, UploadPreflightOptions{AutoFix: true})
}

func PrepareSkillForUploadWithOptions(skillDir string, opts UploadPreflightOptions) (*UploadPreflightResult, error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return nil, fmt.Errorf("skill directory is empty")
	}

	result := &UploadPreflightResult{SkillDir: skillDir}

	if opts.AutoFix {
		changes, fixErr := AutoFixPortability(skillDir)
		if fixErr != nil {
			return nil, fmt.Errorf("auto-fix portability: %w", fixErr)
		}
		result.AutoFixed = changes
	}

	report, err := ValidateSkillPortability(skillDir)
	if err != nil {
		return nil, fmt.Errorf("validate portability: %w", err)
	}
	result.Report = report
	result.SkillName = report.SkillName

	for _, issue := range report.Issues {
		switch issue.Category {
		case "hardcoded_path", "missing_basedir":
			result.BlockingPaths = append(result.BlockingPaths, PortabilityPathIssue{
				Path:       extractIssuePath(issue.Message),
				File:       issue.File,
				Suggestion: issue.Suggestion,
			})
		default:
			if issue.Severity == SeverityWarning || issue.Severity == SeverityInfo {
				result.Warnings = append(result.Warnings, formatPreflightWarning(issue))
			}
		}
	}

	result.MissingFiles = missingBundledFileReferences(skillDir, report.SkillName)

	// Validate skill ID if declared.
	// Load the skill entry to read the id field.
	entry, _, _ := loadSkillFromDir(skillDir, report.SkillName)
	if entry != nil && entry.SkillID != "" {
		result.SkillID = entry.SkillID
		if !IsValidSkillID(entry.SkillID) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("[skill_id] id %q 格式无效（要求: <publisher>.<skill-name>，仅小写字母、数字、连字符，如 lovstudio.any2pdf）", entry.SkillID))
		}
	}

	// Generate package integrity manifest (SHA256 of all files).
	// Written to the skill directory so it's included in the uploaded zip.
	if result.Portable() {
		skillID := ""
		version := ""
		if entry != nil {
			skillID = entry.SkillID
			version = entry.Version
			if version == "" {
				version = entry.HubVersion
			}
		}
		manifest, manifestErr := GeneratePackageManifest(skillDir, skillID, version)
		if manifestErr == nil && manifest != nil {
			_ = WritePackageManifest(skillDir, manifest)
		}
	}

	return result, nil
}

// missingBundledFileReferences loads the skill from disk and reports local
// file/script references in its steps that are not present in the package.
// It reuses the same reference-extraction primitives as the runner precheck
// (via CollectMissingStepFileReferences) so the upload gate matches runtime
// behavior, and reports ALL missing files at once instead of just the first.
func missingBundledFileReferences(skillDir, fallbackName string) []string {
	entry, _, err := loadSkillFromDir(skillDir, fallbackName)
	if err != nil || entry == nil {
		if parsed, mdErr := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{NameFallback: fallbackName}); mdErr == nil {
			entry = parsed
		} else {
			return nil
		}
	}
	entry.SkillDir = skillDir
	return CollectMissingPackageFileReferences(entry)
}

// extractIssuePath pulls the quoted path out of a portability issue message
// like: Command contains absolute path "C:\Users\bob\x.py".
func extractIssuePath(message string) string {
	start := strings.IndexByte(message, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(message[start+1:], '"')
	if end < 0 {
		return ""
	}
	return message[start+1 : start+1+end]
}

func formatPreflightWarning(issue PortabilityIssue) string {
	file := strings.TrimSpace(issue.File)
	if file != "" {
		return fmt.Sprintf("[%s] %s: %s", issue.Category, file, issue.Message)
	}
	return fmt.Sprintf("[%s] %s", issue.Category, issue.Message)
}

func IsSkillRuntimePackageFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return strings.HasSuffix(base, ".bak") ||
		base == "upload_status.json" ||
		base == "quality_status.json" ||
		base == "skill_package_manifest.json" ||
		base == PackageManifestFileName ||
		base == ".patches.json" ||
		base == ".maclaw_deps_ok"
}

func IsSkillRuntimePackageDir(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case ".git", ".hg", ".svn", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".cache", "node_modules":
		return true
	default:
		// Also exclude .prev backup directories created during CommitStaging updates.
		return strings.HasSuffix(base, ".prev")
	}
}

// FormatUploadPreflight renders an UploadPreflightResult as an agent-readable
// report. When the skill is not portable, the report lists concrete absolute
// paths / missing files and how to fix each one.
func FormatUploadPreflight(result *UploadPreflightResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder

	if len(result.AutoFixed) > 0 {
		b.WriteString(fmt.Sprintf("Auto-fixed %d portable path issue(s). Backups were written as .bak files.\n", len(result.AutoFixed)))
		for _, c := range result.AutoFixed {
			where := c.File
			if c.Field != "" {
				where = fmt.Sprintf("%s [%s]", c.File, c.Field)
			}
			b.WriteString(fmt.Sprintf("  - %s\n      - %s\n      + %s\n", where, c.Original, c.Replacement))
		}
		b.WriteByte('\n')
	}

	if result.Portable() {
		b.WriteString("Upload preflight passed: no blocking absolute paths or missing bundled files were found.\n")
		if len(result.Warnings) > 0 {
			b.WriteString("\nNon-blocking warnings:\n")
			for _, w := range result.Warnings {
				b.WriteString("  - " + w + "\n")
			}
		}
		return b.String()
	}

	b.WriteString("Upload blocked: preflight found portability/completeness problems that would break on another machine.\n")

	if len(result.BlockingPaths) > 0 {
		b.WriteString(fmt.Sprintf("\nBlocking absolute paths (%d):\n", len(result.BlockingPaths)))
		for _, p := range result.BlockingPaths {
			b.WriteString(fmt.Sprintf("  - %s (in %s)\n", p.Path, p.File))
			suggestion := strings.TrimSpace(p.Suggestion)
			if suggestion == "" {
				suggestion = "Change it to {baseDir}/relative/path for bundled files, or to a runtime parameter such as {{input}}."
			}
			b.WriteString("      Fix: " + suggestion + "\n")
		}
	}

	if len(result.MissingFiles) > 0 {
		b.WriteString(fmt.Sprintf("\nMissing bundled files (%d):\n", len(result.MissingFiles)))
		for _, f := range result.MissingFiles {
			b.WriteString("  - " + f + "\n")
		}
		b.WriteString("      Fix: copy these scripts/assets into the skill directory and reference them with {baseDir}/...\n")
	}

	b.WriteString("\nPortable references supported by SkillRunner:\n")
	b.WriteString("  - {baseDir}/scripts/run.py: bundled file inside the skill package.\n")
	b.WriteString("  - $HOME/...: user-home file, when truly user-specific.\n")
	b.WriteString("  - {{input}} / {{output}} / {{key}}: runtime parameter supplied by caller/user.\n")
	b.WriteString("\nAfter fixing, retry with manage_skill(action=\"upload\", name=\"" + result.SkillName + "\").\n")

	if len(result.Warnings) > 0 {
		b.WriteString("\nNon-blocking warnings:\n")
		for _, w := range result.Warnings {
			b.WriteString("  - " + w + "\n")
		}
	}

	return b.String()
}

// PreflightSummaryLine returns a one-line summary suitable for logs.
func PreflightSummaryLine(result *UploadPreflightResult) string {
	if result == nil {
		return "preflight: <nil>"
	}
	return fmt.Sprintf("preflight skill=%s autofixed=%d blocking_paths=%d missing_files=%d warnings=%d portable=%t",
		result.SkillName, len(result.AutoFixed), len(result.BlockingPaths), len(result.MissingFiles), len(result.Warnings), result.Portable())
}
