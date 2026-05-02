package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	experience "github.com/RapidAI/CodeClaw/corelib/experience"
)

func TestExperienceAuditRecordsRedactedExtractionError(t *testing.T) {
	extractor := &ExperienceExtractor{}
	session := &RemoteSession{ID: "sess-redact"}
	snapshot := experience.SessionSnapshot{Tool: "codex", Title: "auth debug"}

	extractor.recordAuditError(session, snapshot, errors.New("request failed with api key sk-12345678901234567890"), 125*time.Millisecond)

	audit := extractor.ListAudit()
	if len(audit) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(audit))
	}
	if audit[0].Error == "" || strings.Contains(audit[0].Error, "sk-12345678901234567890") {
		t.Fatalf("expected redacted error, got %q", audit[0].Error)
	}
	if !strings.Contains(audit[0].Error, "[REDACTED]") {
		t.Fatalf("expected redaction marker, got %q", audit[0].Error)
	}
	if audit[0].DurationMS != 125 {
		t.Fatalf("expected duration to be recorded, got %d", audit[0].DurationMS)
	}
}

func TestExperienceAuditListReturnsDeepCopy(t *testing.T) {
	extractor := &ExperienceExtractor{}
	session := &RemoteSession{ID: "sess-copy"}
	result := experience.Result{
		Upserted: nil,
		Decisions: []experience.Decision{{
			PatternName: "run-tests",
			Action:      experience.DecisionSkipped,
			Reason:      "insufficient session evidence",
			Quality:     experience.QualityReport{Reasons: []string{"too weak"}},
			Evidence:    experience.EvidenceReport{UnsupportedSteps: []string{"go:vet"}},
		}},
	}
	extractor.recordAudit(session, experience.SessionSnapshot{Tool: "codex"}, result, 42*time.Millisecond)

	first := extractor.ListAudit()
	if len(first) != 1 || len(first[0].Decisions) != 1 {
		t.Fatalf("unexpected first audit snapshot: %#v", first)
	}
	first[0].Summary.SkipReasons["insufficient session evidence"] = 99
	first[0].Summary.UnsupportedSteps["go:vet"] = 99
	first[0].Decisions[0].Quality.Reasons[0] = "mutated"
	first[0].Decisions[0].Evidence.UnsupportedSteps[0] = "mutated"

	second := extractor.ListAudit()
	if second[0].Summary.SkipReasons["insufficient session evidence"] == 99 {
		t.Fatalf("skip reasons map should be deep-copied: %#v", second[0].Summary.SkipReasons)
	}
	if second[0].Summary.UnsupportedSteps["go:vet"] == 99 {
		t.Fatalf("unsupported steps map should be deep-copied: %#v", second[0].Summary.UnsupportedSteps)
	}
	if second[0].Decisions[0].Quality.Reasons[0] == "mutated" {
		t.Fatalf("quality reasons should be deep-copied: %#v", second[0].Decisions[0].Quality.Reasons)
	}
	if second[0].Decisions[0].Evidence.UnsupportedSteps[0] == "mutated" {
		t.Fatalf("evidence unsupported steps should be deep-copied: %#v", second[0].Decisions[0].Evidence.UnsupportedSteps)
	}
}

func TestExperienceAuditStatusNoCandidates(t *testing.T) {
	extractor := &ExperienceExtractor{}
	session := &RemoteSession{ID: "sess-empty"}
	extractor.recordAudit(session, experience.SessionSnapshot{Tool: "codex"}, experience.Result{}, 7*time.Millisecond)

	audit := extractor.ListAudit()
	if len(audit) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(audit))
	}
	if audit[0].Status != "no_candidates" || audit[0].Summary.TotalCandidates != 0 {
		t.Fatalf("unexpected no-candidate audit: %#v", audit[0])
	}
	if audit[0].DurationMS != 7 {
		t.Fatalf("expected no-candidate duration, got %d", audit[0].DurationMS)
	}
}

func TestExperienceAuditStatusCompleted(t *testing.T) {
	extractor := &ExperienceExtractor{}
	session := &RemoteSession{ID: "sess-complete"}
	extractor.recordAudit(session, experience.SessionSnapshot{Tool: "codex"}, experience.Result{Decisions: []experience.Decision{{PatternName: "run-tests", Action: experience.DecisionRegistered}}}, 9*time.Millisecond)

	audit := extractor.ListAudit()
	if len(audit) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(audit))
	}
	if audit[0].Status != "completed" || audit[0].Summary.Registered != 1 {
		t.Fatalf("unexpected completed audit: %#v", audit[0])
	}
}

func TestExperienceAuditHealthAggregatesEntries(t *testing.T) {
	extractor := &ExperienceExtractor{}
	session := &RemoteSession{ID: "sess-health"}
	extractor.recordAudit(session, experience.SessionSnapshot{Tool: "codex"}, experience.Result{Decisions: []experience.Decision{{PatternName: "run-tests", Action: experience.DecisionRegistered}}}, 20*time.Millisecond)
	extractor.recordAuditError(session, experience.SessionSnapshot{Tool: "codex"}, errors.New("request failed"), 40*time.Millisecond)

	health := extractor.AuditHealth()
	if health.Runs != 2 || health.Completed != 1 || health.Failed != 1 {
		t.Fatalf("unexpected audit health counts: %#v", health)
	}
	if health.Registered != 1 || health.AvgDurationMS != 30 {
		t.Fatalf("unexpected audit health aggregate: %#v", health)
	}
}
