package skill

import (
	"fmt"
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
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return nil, fmt.Errorf("skill directory is empty")
	}

	result := &UploadPreflightResult{SkillDir: skillDir}

	// Step 1: apply safe auto-fixes in place (persisting, with .bak backups).
	changes, fixErr := AutoFixPortability(skillDir)
	if fixErr != nil {
		return nil, fmt.Errorf("auto-fix portability: %w", fixErr)
	}
	result.AutoFixed = changes

	// Step 2: re-validate against the post-fix state.
	report, err := ValidateSkillPortability(skillDir)
	if err != nil {
		return nil, fmt.Errorf("validate portability: %w", err)
	}
	result.Report = report
	result.SkillName = report.SkillName

	for _, issue := range report.Issues {
		switch issue.Category {
		case "hardcoded_path", "missing_basedir":
			// Absolute paths that survived auto-fix are machine-specific and
			// must be converted by the agent.
			result.BlockingPaths = append(result.BlockingPaths, PortabilityPathIssue{
				Path:       extractIssuePath(issue.Message),
				File:       issue.File,
				Suggestion: issue.Suggestion,
			})
		default:
			if issue.Severity == SeverityWarning {
				result.Warnings = append(result.Warnings, formatPreflightWarning(issue))
			}
		}
	}

	// Step 3: verify referenced local files are bundled inside the package.
	result.MissingFiles = missingBundledFileReferences(skillDir, report.SkillName)

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
		// SKILL.md-only or markdown skills: fall back to the markdown loader.
		if parsed, mdErr := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{NameFallback: fallbackName}); mdErr == nil {
			entry = parsed
		} else {
			return nil
		}
	}
	entry.SkillDir = skillDir
	return CollectMissingStepFileReferences(entry)
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

// FormatUploadPreflight renders an UploadPreflightResult as an agent-readable
// report. When the skill is not portable, the report lists the concrete
// absolute paths / missing files and how to fix each one so the agent can
// patch the skill and retry the upload.
func FormatUploadPreflight(result *UploadPreflightResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder

	if len(result.AutoFixed) > 0 {
		b.WriteString(fmt.Sprintf("🔧 已自动修正 %d 处可移植性问题（原路径已替换为 {baseDir}/相对路径/$HOME，并生成 .bak 备份）：\n", len(result.AutoFixed)))
		for _, c := range result.AutoFixed {
			where := c.File
			if c.Field != "" {
				where = fmt.Sprintf("%s [%s]", c.File, c.Field)
			}
			b.WriteString(fmt.Sprintf("  • %s\n      - %s\n      + %s\n", where, c.Original, c.Replacement))
		}
		b.WriteByte('\n')
	}

	if result.Portable() {
		b.WriteString("✅ 可移植性检查通过：未发现影响其它机器安装/运行的绝对路径或缺失文件。\n")
		if len(result.Warnings) > 0 {
			b.WriteString("\n⚠️ 以下为非阻塞警告，建议关注：\n")
			for _, w := range result.Warnings {
				b.WriteString("  • " + w + "\n")
			}
		}
		return b.String()
	}

	b.WriteString("❌ 上传被阻止：发现会影响其它机器使用的问题，请修正后重试。\n")

	if len(result.BlockingPaths) > 0 {
		b.WriteString(fmt.Sprintf("\n绝对路径（%d 处，需改为 SkillRunner 支持的宏或相对路径）：\n", len(result.BlockingPaths)))
		for _, p := range result.BlockingPaths {
			b.WriteString(fmt.Sprintf("  • %s（位于 %s）\n", p.Path, p.File))
			suggestion := strings.TrimSpace(p.Suggestion)
			if suggestion == "" {
				suggestion = "改为 {baseDir}/相对路径（指向 skill 包内文件），或改为运行时参数 {{key}}（指向用户提供的路径）"
			}
			b.WriteString("      💡 " + suggestion + "\n")
		}
	}

	if len(result.MissingFiles) > 0 {
		b.WriteString(fmt.Sprintf("\n缺失的依赖文件（%d 处，skill 引用了但未打包进 skill 目录）：\n", len(result.MissingFiles)))
		for _, f := range result.MissingFiles {
			b.WriteString("  • " + f + "\n")
		}
		b.WriteString("      💡 将这些脚本/文件复制到 skill 目录内，并在命令中用 {baseDir}/文件名 引用\n")
	}

	b.WriteString("\nSkillRunner 支持的可移植引用方式：\n")
	b.WriteString("  • {baseDir}/scripts/run.py —— 指向 skill 包内的文件（推荐，运行时自动展开为 skill 目录）\n")
	b.WriteString("  • $HOME/... —— 指向用户主目录下的文件\n")
	b.WriteString("  • {{input}} / {{output}} / {{key}} —— 运行时由用户/调用方提供的参数\n")
	b.WriteString("\n修正后用 manage_skill(action=\"upload\", name=\"" + result.SkillName + "\") 重新上传。\n")

	if len(result.Warnings) > 0 {
		b.WriteString("\n⚠️ 另有非阻塞警告：\n")
		for _, w := range result.Warnings {
			b.WriteString("  • " + w + "\n")
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
