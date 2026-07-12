package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// PatternStagingValidator implements StagingValidator using the deterministic
// pattern + static file security scan (no LLM). Suitable for auto-promotion
// and headless install paths that must not depend on a configured LLM.
type PatternStagingValidator struct {
	// BlockOnReview, when true (default), blocks skills that need user review
	// (final level >= high). When false, only critical/dangerous reports block.
	BlockOnReview bool
}

// NewPatternStagingValidator returns a StagingValidator that rejects high/critical risk.
// Use this for untrusted package install paths.
func NewPatternStagingValidator() *PatternStagingValidator {
	return &PatternStagingValidator{BlockOnReview: true}
}

// NewAutoPromotionStagingValidator returns a StagingValidator for nudge-to-skill
// promotion. Auto-promoted skills are derived from already-successful local tool
// sequences (often bash/write tools), so only critical/dangerous findings block
// promotion; "high" community escalations alone do not.
func NewAutoPromotionStagingValidator() *PatternStagingValidator {
	return &PatternStagingValidator{BlockOnReview: false}
}

// ScanSkillDir implements StagingValidator.
func (v *PatternStagingValidator) ScanSkillDir(skillDir string) error {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return fmt.Errorf("skill directory is empty")
	}
	info, err := os.Stat(skillDir)
	if err != nil {
		return fmt.Errorf("skill directory not readable: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("skill path is not a directory: %s", skillDir)
	}

	entry := loadEntryForStagingScan(skillDir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report := NewSecurityScanner(nil).ScanInstallStaged(ctx, entry, skillDir, nil)
	if report == nil {
		return fmt.Errorf("security scan returned no report")
	}
	if report.IsDangerous() {
		return fmt.Errorf("security scan blocked (critical): %s", strings.TrimSpace(report.Summary))
	}
	blockReview := true
	if v != nil {
		blockReview = v.BlockOnReview
	}
	if blockReview && report.NeedsUserReview() {
		return fmt.Errorf("security scan blocked (level=%s): %s", report.FinalLevel, strings.TrimSpace(report.Summary))
	}
	return nil
}

// loadEntryForStagingScan builds a minimal NLSkillEntry for scanning, preferring
// skill.yaml / SKILL.md content when present so pattern rules see real steps.
func loadEntryForStagingScan(skillDir string) *corelib.NLSkillEntry {
	name := filepath.Base(skillDir)
	entry := &corelib.NLSkillEntry{
		Name:     name,
		SkillDir: skillDir,
		Source:   "auto_discovered",
		Status:   "active",
	}
	if data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml")); err == nil {
		if sf, err := ParseSkillYAMLFile(data); err == nil {
			if strings.TrimSpace(sf.Name) != "" {
				entry.Name = sf.Name
			}
			entry.Description = sf.Description
			entry.Triggers = sf.Triggers
			if len(sf.Steps) > 0 {
				steps := make([]corelib.NLSkillStep, len(sf.Steps))
				for i, s := range sf.Steps {
					steps[i] = corelib.NLSkillStep{
						Action:  s.Action,
						Params:  s.Params,
						OnError: s.OnError,
						Name:    s.Name,
						When:    s.When,
						Label:   s.Label,
					}
				}
				entry.Steps = steps
			}
			return entry
		}
	}
	// Markdown-only package.
	for _, doc := range []string{"SKILL.md", "skill.md"} {
		if data, err := os.ReadFile(filepath.Join(skillDir, doc)); err == nil && len(data) > 0 {
			if parsed, err := ParseMarkdownSkill(string(data), MarkdownSkillOptions{
				NameFallback: name,
				Source:       "auto_discovered",
			}); err == nil && parsed != nil {
				return parsed
			}
		}
	}
	return entry
}
