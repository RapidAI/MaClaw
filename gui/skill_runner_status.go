package main

import (
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

type skillRunLifecycleStatus string

const (
	skillRunStatusUnknown   skillRunLifecycleStatus = ""
	skillRunStatusRunning   skillRunLifecycleStatus = "running"
	skillRunStatusSuccess   skillRunLifecycleStatus = "success"
	skillRunStatusFailed    skillRunLifecycleStatus = "failed"
	skillRunStatusCancelled skillRunLifecycleStatus = "cancelled"
)

func normalizeSkillRunLifecycleStatus(status string) skillRunLifecycleStatus {
	switch skillRunLifecycleStatus(strings.TrimSpace(status)) {
	case skillRunStatusRunning:
		return skillRunStatusRunning
	case skillRunStatusSuccess:
		return skillRunStatusSuccess
	case skillRunStatusFailed:
		return skillRunStatusFailed
	case skillRunStatusCancelled:
		return skillRunStatusCancelled
	default:
		return skillRunStatusUnknown
	}
}

func (s skillRunLifecycleStatus) Normalized() skillRunLifecycleStatus {
	switch s {
	case skillRunStatusRunning, skillRunStatusSuccess, skillRunStatusFailed, skillRunStatusCancelled:
		return s
	default:
		return skillRunStatusUnknown
	}
}

func (s skillRunLifecycleStatus) String() string {
	return string(s)
}

func (s *SkillRunStatus) LifecycleStatus() skillRunLifecycleStatus {
	if s == nil {
		return skillRunStatusUnknown
	}
	return s.Status.Normalized()
}

func (s *SkillRunStatus) IsRunning() bool {
	return s.LifecycleStatus() == skillRunStatusRunning
}

func (s *SkillRunStatus) IsFailed() bool {
	return s.LifecycleStatus() == skillRunStatusFailed
}

func (s *SkillRunStatus) IsFinished() bool {
	switch s.LifecycleStatus() {
	case skillRunStatusSuccess, skillRunStatusFailed, skillRunStatusCancelled:
		return true
	default:
		return false
	}
}

type skillStepStatus string

const (
	skillStepStatusUnknown skillStepStatus = ""
	skillStepStatusPending skillStepStatus = "pending"
	skillStepStatusRunning skillStepStatus = "running"
	skillStepStatusSuccess skillStepStatus = "success"
	skillStepStatusFailed  skillStepStatus = "failed"
	skillStepStatusSkipped skillStepStatus = "skipped"
	skillStepStatusTimeout skillStepStatus = "timeout"
)

func normalizeSkillStepStatus(status string) skillStepStatus {
	switch skillStepStatus(strings.TrimSpace(status)) {
	case skillStepStatusPending:
		return skillStepStatusPending
	case skillStepStatusRunning:
		return skillStepStatusRunning
	case skillStepStatusSuccess:
		return skillStepStatusSuccess
	case skillStepStatusFailed:
		return skillStepStatusFailed
	case skillStepStatusSkipped:
		return skillStepStatusSkipped
	case skillStepStatusTimeout:
		return skillStepStatusTimeout
	default:
		return skillStepStatusUnknown
	}
}

func (s skillStepStatus) Normalized() skillStepStatus {
	switch s {
	case skillStepStatusPending, skillStepStatusRunning, skillStepStatusSuccess, skillStepStatusFailed, skillStepStatusSkipped, skillStepStatusTimeout:
		return s
	default:
		return skillStepStatusUnknown
	}
}

func (s skillStepStatus) OrElse(fallback skillStepStatus) skillStepStatus {
	if normalized := s.Normalized(); normalized != skillStepStatusUnknown {
		return normalized
	}
	return fallback
}

func (s skillStepStatus) String() string {
	return string(s)
}

func (s StepResult) LifecycleStatus() skillStepStatus {
	return s.Status.Normalized()
}

func (s StepResult) IsFailed() bool {
	return s.LifecycleStatus() == skillStepStatusFailed
}

func (s StepResult) IsTerminal() bool {
	switch s.LifecycleStatus() {
	case skillStepStatusSuccess, skillStepStatusFailed, skillStepStatusSkipped, skillStepStatusTimeout:
		return true
	default:
		return false
	}
}

type skillArtifactStatus string

const (
	skillArtifactStatusUnknown  skillArtifactStatus = ""
	skillArtifactStatusPending  skillArtifactStatus = "pending"
	skillArtifactStatusVerified skillArtifactStatus = "verified"
	skillArtifactStatusMissing  skillArtifactStatus = "missing"
)

func normalizeSkillArtifactStatus(status string) skillArtifactStatus {
	switch skillArtifactStatus(strings.TrimSpace(status)) {
	case skillArtifactStatusPending:
		return skillArtifactStatusPending
	case skillArtifactStatusVerified:
		return skillArtifactStatusVerified
	case skillArtifactStatusMissing:
		return skillArtifactStatusMissing
	default:
		return skillArtifactStatusUnknown
	}
}

func (s skillArtifactStatus) String() string {
	return string(s)
}

func (s skillArtifactStatus) IsDecided() bool {
	return s == skillArtifactStatusVerified || s == skillArtifactStatusMissing
}

type skillPipelineStatus string

const (
	skillPipelineStatusCompleted skillPipelineStatus = "completed"
	skillPipelineStatusFailed    skillPipelineStatus = "failed"
	skillPipelineStatusSkipped   skillPipelineStatus = "skipped"
	skillPipelineStatusCancelled skillPipelineStatus = "cancelled"
)

func normalizeSkillPipelineStatus(status string) skillPipelineStatus {
	switch skillPipelineStatus(strings.TrimSpace(status)) {
	case skillPipelineStatusCompleted:
		return skillPipelineStatusCompleted
	case skillPipelineStatusSkipped:
		return skillPipelineStatusSkipped
	case skillPipelineStatusCancelled:
		return skillPipelineStatusCancelled
	default:
		return skillPipelineStatusFailed
	}
}

func normalizeSkillPipelineStatusFromCore(status cskill.PipelineStatus) skillPipelineStatus {
	switch status {
	case cskill.PipelineStatusCompleted:
		return skillPipelineStatusCompleted
	case cskill.PipelineStatusCancelled:
		return skillPipelineStatusCancelled
	default:
		return skillPipelineStatusFailed
	}
}

func normalizeSkillPipelineStepStatus(status cskill.PipelineStepStatus) skillPipelineStatus {
	switch status {
	case cskill.PipelineStepStatusCompleted:
		return skillPipelineStatusCompleted
	case cskill.PipelineStepStatusSkipped:
		return skillPipelineStatusSkipped
	case cskill.PipelineStepStatusCancelled:
		return skillPipelineStatusCancelled
	default:
		return skillPipelineStatusFailed
	}
}

func (s skillPipelineStatus) Normalized() skillPipelineStatus {
	switch s {
	case skillPipelineStatusCompleted, skillPipelineStatusFailed, skillPipelineStatusSkipped, skillPipelineStatusCancelled:
		return s
	default:
		return skillPipelineStatusFailed
	}
}

func (s skillPipelineStatus) StepStatus() skillStepStatus {
	switch s.Normalized() {
	case skillPipelineStatusCompleted:
		return skillStepStatusSuccess
	case skillPipelineStatusSkipped, skillPipelineStatusCancelled:
		return skillStepStatusSkipped
	default:
		return skillStepStatusFailed
	}
}

type skillEntryStatus string

const (
	skillEntryStatusUnknown    skillEntryStatus = ""
	skillEntryStatusActive     skillEntryStatus = "active"
	skillEntryStatusDisabled   skillEntryStatus = "disabled"
	skillEntryStatusNeedsSetup skillEntryStatus = "needs_setup"
)

func normalizeSkillEntryStatus(status string) skillEntryStatus {
	switch skillEntryStatus(strings.TrimSpace(status)) {
	case skillEntryStatusActive:
		return skillEntryStatusActive
	case skillEntryStatusDisabled:
		return skillEntryStatusDisabled
	case skillEntryStatusNeedsSetup:
		return skillEntryStatusNeedsSetup
	default:
		return skillEntryStatusUnknown
	}
}

func (s skillEntryStatus) String() string {
	return string(s)
}

type skillEntrySource string

const (
	skillEntrySourceUnknown skillEntrySource = ""
	skillEntrySourceHub     skillEntrySource = "hub"
	skillEntrySourceFile    skillEntrySource = "file"
	skillEntrySourceClawHub skillEntrySource = "clawhub"
	skillEntrySourceGitHub  skillEntrySource = "github"
	skillEntrySourceManual  skillEntrySource = "manual"
	skillEntrySourceAgent   skillEntrySource = "agent_skill"
)

func normalizeSkillEntrySource(source string) skillEntrySource {
	switch skillEntrySource(strings.TrimSpace(source)) {
	case skillEntrySourceHub:
		return skillEntrySourceHub
	case skillEntrySourceFile:
		return skillEntrySourceFile
	case skillEntrySourceClawHub:
		return skillEntrySourceClawHub
	case skillEntrySourceGitHub:
		return skillEntrySourceGitHub
	case skillEntrySourceManual:
		return skillEntrySourceManual
	case skillEntrySourceAgent:
		return skillEntrySourceAgent
	default:
		return skillEntrySourceUnknown
	}
}

func (s skillEntrySource) String() string {
	return string(s)
}

func (s skillEntrySource) IsAgentMarkdownSkillSource() bool {
	switch s {
	case skillEntrySourceGitHub, skillEntrySourceClawHub, skillEntrySourceAgent:
		return true
	default:
		return false
	}
}
