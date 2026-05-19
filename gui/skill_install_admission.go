package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func skillInstallRiskFactors(report *cskill.ScanReport) []string {
	if report == nil {
		return nil
	}
	var factors []string
	factors = append(factors, report.PatternAssessment.Factors...)
	for _, finding := range report.Findings {
		desc := strings.TrimSpace(finding.Description)
		if desc != "" {
			factors = append(factors, desc)
		}
	}
	return factors
}

func (a *App) emitSkillInstallProgress(skillName, phase, status string, report *cskill.ScanReport) {
	if a == nil {
		return
	}
	lang := a.skillConfirmLang()
	payload := map[string]interface{}{
		"skill":   skillName,
		"phase":   phase,
		"status":  status,
		"percent": skillInstallProgressPercent(phase),
		"lang":    lang,
	}
	if report != nil {
		payload["level"] = string(report.FinalLevel)
		payload["summary"] = report.Summary
		payload["scanned_by"] = report.ScannedBy
	}
	a.emitEvent("skill-install-progress", payload)
}

func skillInstallProgressPercent(phase string) int {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "queued":
		return 5
	case "scan-start":
		return 12
	case "extract":
		return 25
	case "scanning":
		return 45
	case "awaiting-confirmation":
		return 65
	case "approved":
		return 80
	case "installing":
		return 90
	case "scan-complete", "done":
		return 100
	case "blocked", "rejected":
		return 100
	default:
		return 20
	}
}

func (a *App) logSkillInstallSecurityEvent(action security.AuditAction, toolName string, level security.RiskLevel, policy security.PolicyAction, result string) {
	if a == nil || a.auditLog == nil {
		return
	}
	_ = a.auditLog.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		Action:       action,
		ToolName:     toolName,
		RiskLevel:    level,
		PolicyAction: policy,
		Result:       result,
	})
}

func (a *App) isSecurityDeveloperMode() bool {
	return a != nil && a.policyEngine != nil && a.policyEngine.IsDeveloperMode()
}

func (a *App) securityPolicyMode() string {
	if a != nil && a.policyEngine != nil {
		return a.policyEngine.Mode()
	}
	return "standard"
}

func (a *App) skillInstallReviewNeedsConfirmation(report *cskill.ScanReport) bool {
	if report == nil || report.IsSafe() || a.isSecurityDeveloperMode() {
		return false
	}
	switch a.securityPolicyMode() {
	case "standard":
		return report.IsDangerous()
	case "strict":
		return report.IsDangerous() || report.NeedsUserReview()
	default:
		return false
	}
}

func (a *App) skillInstallScanShouldBlock(report *cskill.ScanReport) bool {
	if report == nil || report.IsSafe() || a.isSecurityDeveloperMode() {
		return false
	}
	return a.securityPolicyMode() == "strict" && (report.IsDangerous() || report.NeedsUserReview())
}

func (a *App) skillInstallMissingScanShouldBlock() bool {
	return a.securityPolicyMode() == "strict"
}

func (a *App) confirmManualSkillInstall(ctx context.Context, skillName, source string, level security.RiskLevel, factors []string) bool {
	if a == nil {
		return false
	}
	if a.ctx == nil {
		if strings.TrimSpace(a.testHomeDir) != "" || a.securityPolicyMode() != "strict" {
			log.Printf("[skill-install-confirm] no UI context for skill %q; allowing install under %s mode", skillName, a.securityPolicyMode())
			return true
		}
		log.Printf("[skill-install-confirm] no UI context for skill %q; rejecting high-risk install in strict mode", skillName)
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	confirmID := nextSkillConfirmID("skill_install")
	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	a.skillInstallConfirm.Store(confirmID, entry)

	go func() {
		time.Sleep(confirmTimeout)
		if entry.tryResolve() {
			a.skillInstallConfirm.Delete(confirmID)
			close(entry.Ch)
			log.Printf("[skill-install-confirm] cleanup: confirmID=%s expired after %v", confirmID, confirmTimeout)
		}
	}()

	emitFactors := factors
	if emitFactors == nil {
		emitFactors = []string{}
	}
	lang := a.skillConfirmLang()
	confirmLabel, rejectLabel := localizedSkillInstallActionLabels(lang)
	payload := map[string]interface{}{
		"confirm_id": confirmID,
		"skill_name": skillName,
		"source":     source,
		"level":      string(level),
		"factors":    localizeSkillRiskFactors(lang, emitFactors),
		"lang":       lang,
		"labels": map[string]string{
			"confirm": confirmLabel,
			"reject":  rejectLabel,
		},
	}
	a.emitEvent("skill-install-risk-confirm", payload)
	log.Printf("[skill-install-confirm] desktop event emitted confirm_id=%s skill=%q level=%s", confirmID, skillName, level)

	select {
	case resp, ok := <-entry.Ch:
		if !ok {
			log.Printf("[skill-install-confirm] timeout confirm_id=%s skill=%q", confirmID, skillName)
			return false
		}
		log.Printf("[skill-install-confirm] received response confirm_id=%s confirmed=%v", confirmID, resp.Confirmed)
		return resp.Confirmed
	case <-ctx.Done():
		log.Printf("[skill-install-confirm] context cancelled confirm_id=%s skill=%q", confirmID, skillName)
		return false
	}
}

func (a *App) resolveSkillInstallConfirm(confirmID string, confirmed bool) error {
	if a == nil {
		return fmt.Errorf("skill install confirmation unavailable")
	}
	v, ok := a.skillInstallConfirm.Load(confirmID)
	if !ok {
		return fmt.Errorf("skill install confirmation expired or already handled")
	}
	entry, ok := v.(*pendingCriticalConfirmEntry)
	if !ok {
		return fmt.Errorf("internal error: invalid skill install confirmation state")
	}
	if !entry.tryResolve() {
		return fmt.Errorf("skill install confirmation timed out")
	}
	a.skillInstallConfirm.Delete(confirmID)
	entry.Ch <- criticalRiskConfirmResponse{Confirmed: confirmed}
	return nil
}

func (a *App) admitManualSkillInstall(ctx context.Context, entry *corelib.NLSkillEntry, source string, report *cskill.ScanReport) error {
	if entry == nil {
		return fmt.Errorf("skill entry is required")
	}
	if a.isSecurityDeveloperMode() {
		status := "Developer mode enabled; security scan will not block installation."
		phase := "scan-complete"
		level := security.RiskLow
		summary := "no scan report recorded"
		if report != nil {
			level = report.FinalLevel
			summary = report.Summary
			if report.IsDangerous() || report.NeedsUserReview() {
				status = "Developer mode enabled; high-risk scan result allowed."
				phase = "approved"
			}
		}
		a.emitSkillInstallProgress(entry.Name, phase, status, report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"manual_skill_install",
			level,
			security.PolicyAllow,
			fmt.Sprintf("developer mode allowed skill %s after pre-install scan: %s", entry.Name, summary),
		)
		return nil
	}
	if report == nil {
		if !a.skillInstallMissingScanShouldBlock() {
			a.emitSkillInstallProgress(entry.Name, "scan-complete", "Security scan did not produce a report; current policy allows installation.", nil)
			a.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillInstall,
				"manual_skill_install",
				security.RiskCritical,
				security.PolicyAudit,
				fmt.Sprintf("current policy allowed skill %s even though pre-install scan report was missing", entry.Name),
			)
			return nil
		}
		a.emitSkillInstallProgress(entry.Name, "blocked", "Security scan did not produce a report. Installation blocked by policy.", nil)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillReject,
			"manual_skill_install",
			security.RiskCritical,
			security.PolicyDeny,
			fmt.Sprintf("pre-install scan rejected skill %s because scan report was missing", entry.Name),
		)
		return fmt.Errorf("skill security scan rejected installation: scan report is missing")
	}
	if report.IsSafe() {
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Security scan passed.", report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"manual_skill_install",
			report.FinalLevel,
			security.PolicyAllow,
			fmt.Sprintf("pre-install scan allowed skill %s, scanned_by=%s, level=%s", entry.Name, report.ScannedBy, report.FinalLevel),
		)
		return nil
	}

	factors := skillInstallRiskFactors(report)
	if a.skillInstallReviewNeedsConfirmation(report) {
		statusMsg := "High risk found. Waiting for your allow or reject decision."
		if report.IsDangerous() {
			statusMsg = "Critical risk found. Waiting for your allow or reject decision."
		}
		a.emitSkillInstallProgress(entry.Name, "awaiting-confirmation", statusMsg, report)
		confirmed := a.confirmManualSkillInstall(ctx, entry.Name, source, report.FinalLevel, factors)
		if !confirmed {
			a.emitSkillInstallProgress(entry.Name, "rejected", "Installation rejected.", report)
			a.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"manual_skill_install",
				report.FinalLevel,
				security.PolicyDeny,
				fmt.Sprintf("user rejected skill %s after pre-install scan: %s", entry.Name, report.Summary),
			)
			return fmt.Errorf("skill security scan requires user approval and installation was rejected: level=%s summary=%s", report.FinalLevel, report.Summary)
		}
		a.emitSkillInstallProgress(entry.Name, "approved", "User approved high-risk installation.", report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"manual_skill_install",
			report.FinalLevel,
			security.PolicyUserOverride,
			fmt.Sprintf("user allowed skill %s after pre-install scan, scanned_by=%s, level=%s", entry.Name, report.ScannedBy, report.FinalLevel),
		)
	}
	if report.NeedsUserReview() {
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Security scan recorded risk and allowed installation by current policy.", report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"manual_skill_install",
			report.FinalLevel,
			security.PolicyAudit,
			fmt.Sprintf("pre-install scan allowed skill %s by current policy, scanned_by=%s, level=%s", entry.Name, report.ScannedBy, report.FinalLevel),
		)
	}
	return nil
}

func (a *App) scanAndAdmitSkillBeforeRegister(ctx context.Context, entry *corelib.NLSkillEntry, source string) (*cskill.ScanReport, error) {
	if entry == nil {
		return nil, fmt.Errorf("skill entry is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(entry.TrustLevel) == "" {
		entry.TrustLevel = "community"
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = "skill"
	}
	a.emitSkillInstallProgress(name, "scan-start", "Starting pre-install security scan.", nil)
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanInstallStaged(ctx, entry, entry.SkillDir, func(status string) {
		if a != nil {
			a.log(status)
			a.emitSkillInstallProgress(name, "scanning", status, nil)
		}
	})
	if err := a.admitManualSkillInstall(ctx, entry, source, report); err != nil {
		return report, err
	}
	return report, nil
}

func writeSkillScanCacheForInstalledZip(name, description, destRoot string, fallbackReport *cskill.ScanReport, reports *skillZipInstallScanResult) error {
	if (fallbackReport == nil && reports == nil) || strings.TrimSpace(destRoot) == "" {
		return nil
	}
	var errs []error
	for _, dir := range candidateInstalledSkillDirs(destRoot) {
		entry, err := loadImportedSkillEntry(dir)
		if err != nil {
			entry = &corelib.NLSkillEntry{
				Name:        name,
				Description: description,
				SkillDir:    dir,
				Source:      "manual_zip",
				TrustLevel:  "community",
			}
		}
		if strings.TrimSpace(entry.Description) == "" {
			entry.Description = description
		}
		entry.SkillDir = dir
		report := scanReportForInstalledZipEntry(entry, dir, fallbackReport, reports)
		if report == nil {
			continue
		}
		if err := writeSkillScanCacheForReportStatus(entry, dir, "", report, skillScanCacheStatusAllowed); err != nil {
			log.Printf("[skill-install] failed to write scan cache for %s in %s: %v", entry.Name, dir, err)
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))
		}
	}
	return errors.Join(errs...)
}

func scanReportForInstalledZipEntry(entry *corelib.NLSkillEntry, dir string, fallback *cskill.ScanReport, reports *skillZipInstallScanResult) *cskill.ScanReport {
	if reports != nil {
		if entry != nil {
			if report := reports.ByName[strings.TrimSpace(entry.Name)]; report != nil {
				return report
			}
		}
		if report := reports.ByDirName[filepath.Base(dir)]; report != nil {
			return report
		}
		if reports.HighestReport != nil {
			return reports.HighestReport
		}
	}
	return fallback
}

func writeSkillScanCacheForInstalledEntry(entry *corelib.NLSkillEntry, report *cskill.ScanReport) error {
	if entry == nil || report == nil || strings.TrimSpace(entry.SkillDir) == "" {
		return nil
	}
	if err := writeSkillScanCacheForReportStatus(entry, entry.SkillDir, "", report, skillScanCacheStatusAllowed); err != nil {
		log.Printf("[skill-install] failed to write scan cache for %s in %s: %v", entry.Name, entry.SkillDir, err)
		return err
	}
	return nil
}

func (a *App) skillNameAlreadyRegistered(name string) bool {
	if a == nil || a.skillExecutor == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, existing := range a.skillExecutor.loadSkills() {
		if existing.Name == name {
			return true
		}
	}
	return false
}

func candidateInstalledSkillDirs(destRoot string) []string {
	var dirs []string
	if directoryHasSkillDoc(destRoot) {
		dirs = append(dirs, destRoot)
	}
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		return dirs
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.EqualFold(name, "__MACOSX") {
			continue
		}
		dir := filepath.Join(destRoot, name)
		if directoryHasSkillDoc(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func directoryHasSkillDoc(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isSkillMarkdownDocFileName(entry.Name()) {
			return true
		}
	}
	return false
}
