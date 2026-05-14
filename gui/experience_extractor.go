package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	experience "github.com/RapidAI/CodeClaw/corelib/experience"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// ExperienceExtractor adapts GUI remote sessions to the corelib experience pipeline.
type ExperienceExtractor struct {
	llmConfig corelib.MaclawLLMConfig
	client    *http.Client
	core      *experience.Extractor
	audit     *experience.AuditTrail
}

// ExperienceAuditEntry is the Wails-facing audit record for one experience extraction run.
type ExperienceAuditEntry = experience.AuditEntry

// NewExperienceExtractor creates a new ExperienceExtractor.
func NewExperienceExtractor(app *App, skillExecutor *SkillExecutor, cfg corelib.MaclawLLMConfig) *ExperienceExtractor {
	e := &ExperienceExtractor{
		llmConfig: cfg,
		client:    &http.Client{Timeout: 30 * time.Second},
		audit:     experience.NewAuditTrail(50),
	}
	e.core = experience.NewExtractor(experienceLLMClient{cfg: cfg, client: e.client}, experienceSkillStore{executor: skillExecutor})
	return e
}

// Extract analyses the session history via the core experience pipeline and
// registers any discovered reusable patterns as NL Skills.
func (e *ExperienceExtractor) Extract(session *RemoteSession) error {
	if e == nil || e.core == nil || !e.isConfigured() || session == nil {
		return nil
	}
	snapshot := e.snapshot(session)
	if !experience.Eligible(snapshot) {
		return nil
	}
	started := time.Now()
	result, err := e.core.ExtractDetailed(context.Background(), snapshot)
	duration := time.Since(started)
	if err != nil {
		log.Printf("[experience] extraction failed: %v", experience.RedactExperienceText(err.Error()))
		e.recordAuditError(session, snapshot, err, duration)
		return err
	}
	summary := result.Summary()
	log.Printf("[experience] candidates=%d registered=%d updated=%d skipped=%d skip_reasons=%v unsupported_steps=%v",
		summary.TotalCandidates, summary.Registered, summary.Updated, summary.Skipped, summary.SkipReasons, summary.UnsupportedSteps)
	e.recordAudit(session, snapshot, result, duration)
	return nil
}

func (e *ExperienceExtractor) isConfigured() bool {
	return strings.TrimSpace(e.llmConfig.URL) != "" && strings.TrimSpace(e.llmConfig.Model) != ""
}

func (e *ExperienceExtractor) snapshot(session *RemoteSession) experience.SessionSnapshot {
	session.mu.RLock()
	defer session.mu.RUnlock()

	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	events := make([]experience.ImportantEvent, 0, len(session.Events))
	for _, ev := range session.Events {
		events = append(events, experience.ImportantEvent{
			Type:    ev.Type,
			Title:   ev.Title,
			Summary: ev.Summary,
		})
	}

	var exitCode *int
	if session.ExitCode != nil {
		cp := *session.ExitCode
		exitCode = &cp
	}

	return experience.SessionSnapshot{
		Tool:              session.Tool,
		Title:             session.Title,
		ProjectPath:       session.ProjectPath,
		ExitCode:          exitCode,
		StructuredSession: session.isStructuredSession(),
		Events:            events,
		RawOutputLines:    rawLines,
	}
}

func (e *ExperienceExtractor) recordAuditError(session *RemoteSession, snapshot experience.SessionSnapshot, err error, duration time.Duration) {
	if e == nil || session == nil || err == nil {
		return
	}
	e.ensureAuditTrail().RecordError(experience.AuditContext{
		SessionID:  session.ID,
		Snapshot:   snapshot,
		DurationMS: duration.Milliseconds(),
	}, err)
}

func (e *ExperienceExtractor) recordAudit(session *RemoteSession, snapshot experience.SessionSnapshot, result experience.Result, duration time.Duration) {
	if e == nil || session == nil {
		return
	}
	e.ensureAuditTrail().RecordResult(experience.AuditContext{
		SessionID:  session.ID,
		Snapshot:   snapshot,
		DurationMS: duration.Milliseconds(),
	}, result)
}

func (e *ExperienceExtractor) appendAudit(entry ExperienceAuditEntry) {
	if e == nil {
		return
	}
	e.ensureAuditTrail().Append(entry)
}

func (e *ExperienceExtractor) ensureAuditTrail() *experience.AuditTrail {
	if e.audit == nil {
		e.audit = experience.NewAuditTrail(50)
	}
	return e.audit
}

func (e *ExperienceExtractor) ListAudit() []ExperienceAuditEntry {
	if e == nil || e.audit == nil {
		return nil
	}
	return e.audit.List()
}

func (e *ExperienceExtractor) AuditHealth() experience.AuditHealth {
	if e == nil || e.audit == nil {
		return experience.AuditHealth{}
	}
	return e.audit.Health()
}

// GetExperienceAuditHealth returns aggregate health for recent experience extraction runs.
func (a *App) GetExperienceAuditHealth() experience.AuditHealth {
	if a == nil {
		return experience.AuditHealth{}
	}
	a.ensureExperienceExtractor()
	if a.experienceExtractor == nil {
		return experience.AuditHealth{}
	}
	return a.experienceExtractor.AuditHealth()
}

// ListExperienceAudit returns recent experience extraction audit records.
func (a *App) ListExperienceAudit() []ExperienceAuditEntry {
	if a == nil {
		return nil
	}
	a.ensureExperienceExtractor()
	if a.experienceExtractor == nil {
		return nil
	}
	return a.experienceExtractor.ListAudit()
}

type experienceLLMClient struct {
	cfg    corelib.MaclawLLMConfig
	client *http.Client
}

func (c experienceLLMClient) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}
	result, err := doSimpleLLMRequest(ctx, c.cfg, messages, c.client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

type experienceSkillStore struct {
	executor *SkillExecutor
}

func (s experienceSkillStore) List() []corelib.NLSkillEntry {
	if s.executor == nil {
		return nil
	}
	return s.executor.loadSkills()
}

func (s experienceSkillStore) Register(entry corelib.NLSkillEntry) error {
	if s.executor == nil {
		return errors.New("experience skill store is not configured")
	}
	if _, err := s.scanBeforePersist(&entry, "register"); err != nil {
		return err
	}
	return s.executor.Register(entry)
}

func (s experienceSkillStore) Update(entry corelib.NLSkillEntry) error {
	if s.executor == nil {
		return errors.New("experience skill store is not configured")
	}
	if _, err := s.scanBeforePersist(&entry, "update"); err != nil {
		return err
	}
	return s.executor.Update(entry)
}

func (s experienceSkillStore) scanBeforePersist(entry *corelib.NLSkillEntry, operation string) (*cskill.ScanReport, error) {
	if entry == nil {
		return nil, errors.New("experience skill entry is required")
	}
	cp := *entry
	cp.TrustLevel = security.TrustLevelAgentCreated
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanStaged(context.Background(), &cp, cp.SkillDir, nil)
	if report == nil {
		return nil, fmt.Errorf("experience skill %s security scan produced no report", operation)
	}
	app := (*App)(nil)
	if s.executor != nil {
		app = s.executor.app
	}
	auditAction := security.AuditActionHubSkillInstall
	if operation == "update" {
		auditAction = security.AuditActionHubSkillUpdate
	}
	if report.IsDangerous() || report.NeedsUserReview() {
		if app != nil {
			app.logSkillInstallSecurityEvent(
				security.AuditActionHubSkillReject,
				"experience_skill_"+operation,
				report.FinalLevel,
				security.PolicyDeny,
				fmt.Sprintf("experience skill %s blocked before persist: %s", cp.Name, report.Summary),
			)
		}
		return report, fmt.Errorf("experience skill security scan rejected %s for %q: level=%s summary=%s", operation, cp.Name, report.FinalLevel, report.Summary)
	}
	if app != nil {
		app.logSkillInstallSecurityEvent(
			auditAction,
			"experience_skill_"+operation,
			report.FinalLevel,
			security.PolicyAllow,
			fmt.Sprintf("experience skill %s allowed before persist, scanned_by=%s, level=%s", cp.Name, report.ScannedBy, report.FinalLevel),
		)
	}
	if strings.TrimSpace(cp.SkillDir) != "" {
		if err := writeSkillScanCacheForReportStatus(&cp, cp.SkillDir, "", report, skillScanCacheStatusAllowed); err != nil {
			return report, fmt.Errorf("write experience skill scan cache: %w", err)
		}
	}
	return report, nil
}
