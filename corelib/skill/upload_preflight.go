package skill

import (
	"fmt"
	"io"
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

// Skill-market package limits (HubCenter SafeUnzip + Hub preflight).
//
// Product policy: many small necessary assets (templates, fonts, SVGs, …) are
// allowed when overall volume is reasonable. Entry count is only a DoS
// backstop against millions of empty files — not a product cap of “1000 files”.
// Keep hubcenter/internal/skillmarket.SafeUnzip in sync via these constants.
const (
	// MaxSkillMarketZipEntries is a hard DoS ceiling on zip central-directory
	// entries (files + directories). Legitimate multi-thousand asset packs must
	// stay under this and under MaxSkillMarketZipTotalBytes.
	MaxSkillMarketZipEntries = 20000

	// MaxSkillMarketZipTotalBytes is the primary size gate (uncompressed).
	MaxSkillMarketZipTotalBytes int64 = 500 << 20 // 500 MiB

	// MaxSkillMarketZipSingleFileBytes is the per-entry uncompressed limit.
	MaxSkillMarketZipSingleFileBytes int64 = 50 << 20 // 50 MiB

	// MaxSkillHubSearchJSONBytes caps search/list/metadata JSON (no file maps).
	MaxSkillHubSearchJSONBytes int64 = 2 << 20 // 2 MiB

	// MaxSkillPackageDownloadBytes caps skill install wire payloads
	// (JSON with base64 file maps, or equivalent download bodies).
	// Base64 expands ~4/3 over raw files; 96 MiB on the wire supports roughly
	// ~70 MiB of raw skill content in one install response. Multi-asset skills
	// (e.g. ppt-master) exceed the old 5 MiB client cap. Keep desktop RAM
	// bounded — larger market zips should use zip download when available.
	MaxSkillPackageDownloadBytes int64 = 96 << 20 // 96 MiB
)

// FormatSkillByteCount formats a byte count for admin/user-facing errors.
func FormatSkillByteCount(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ErrSkillPackageDownloadTooLarge returns a stable, user-facing limit error.
func ErrSkillPackageDownloadTooLarge(limit int64) error {
	return fmt.Errorf("response exceeds client download limit of %s; multi-asset skill packages need room for base64 file maps",
		FormatSkillByteCount(limit))
}

// CheckSkillPackageDownloadLimit fails fast when Content-Length already exceeds
// the client install budget (avoids reading a huge body only to discard it).
// contentLength < 0 means unknown (chunked / missing header) and is ignored.
func CheckSkillPackageDownloadLimit(contentLength, limit int64) error {
	if limit <= 0 || contentLength < 0 {
		return nil
	}
	if contentLength > limit {
		return ErrSkillPackageDownloadTooLarge(limit)
	}
	return nil
}

// ReadLimitedHTTPBody reads at most limit bytes from body.
// When contentLength >= 0 and exceeds limit, fails immediately without reading.
// Pass contentLength = -1 to skip the header pre-check (e.g. error responses
// where a large Content-Length must not mask the real HTTP status).
// limit <= 0 means unbounded read.
func ReadLimitedHTTPBody(body io.Reader, contentLength, limit int64) ([]byte, error) {
	if err := CheckSkillPackageDownloadLimit(contentLength, limit); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return io.ReadAll(body)
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrSkillPackageDownloadTooLarge(limit)
	}
	return data, nil
}

func IsSkillRuntimePackageFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return strings.HasSuffix(base, ".bak") ||
		base == "upload_status.json" ||
		base == "quality_status.json" ||
		base == "skill_package_manifest.json" ||
		base == PackageManifestFileName ||
		base == ".patches.json" ||
		base == ".maclaw_deps_ok" ||
		base == ".ds_store" ||
		base == "thumbs.db" ||
		base == "desktop.ini"
}

func IsSkillRuntimePackageDir(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case ".git", ".hg", ".svn",
		"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".cache",
		"node_modules",
		".venv", "venv", ".tox", ".eggs",
		// Local install / build debris should never ship in a market package.
		".npm", ".yarn", ".pnpm-store",
		// macOS zip clutter.
		"__macosx":
		return true
	default:
		// Also exclude .prev backup directories created during CommitStaging updates.
		return strings.HasSuffix(base, ".prev")
	}
}

// SkillPackagePathHasRuntimeArtifact reports whether a zip-relative path
// crosses a runtime/cache directory or is a runtime-only file that should
// not be uploaded to SkillMarket / HubCenter.
func SkillPackagePathHasRuntimeArtifact(name string) bool {
	name = filepath.ToSlash(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	// Fast path: single-segment names.
	if !strings.Contains(name, "/") {
		return IsSkillRuntimePackageFile(name) || IsSkillRuntimePackageDir(name)
	}
	parts := strings.Split(name, "/")
	last := -1
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		last = i
	}
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if i == last {
			return IsSkillRuntimePackageFile(part) || IsSkillRuntimePackageDir(part)
		}
		if IsSkillRuntimePackageDir(part) {
			return true
		}
	}
	return false
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
