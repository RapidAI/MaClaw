package collaboration

import (
	"fmt"
	"strings"
	"time"

	colleagueDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// Service handles collaboration task business logic.
type Service struct {
	repo        *Repo
	colleagueRp *colleagueRepo.ColleagueRepo
}

// NewService creates a Service.
func NewService(repo *Repo, colleagueRp *colleagueRepo.ColleagueRepo) *Service {
	return &Service{repo: repo, colleagueRp: colleagueRp}
}

// Repo exposes the underlying repo for transaction-aware callers (e.g. workflow engine).
func (s *Service) Repo() *Repo { return s.repo }

// CreateRequest holds fields for creating a collaboration task.
const (
	RoleActionPromoteStandby = "promote_standby"
	RoleActionPreferPrimary  = "prefer_primary"
	RoleActionBalanceLoad    = "balance_load"
)

type CreateRequest struct {
	Title               string `json:"title"`
	Description         string `json:"description"`
	FromColleagueID     string `json:"from_colleague_id"`
	ToColleagueID       string `json:"to_colleague_id"`
	ToRoleCode          string `json:"to_role_code"`
	Priority            int    `json:"priority"`
	SourceType          string `json:"source_type"`
	SourceSkillID       string `json:"source_skill_id"`
	SourceSkillTitle    string `json:"source_skill_title"`
	SourceFocusTitle    string `json:"source_focus_title"`
	SourceFocusRoleCode string `json:"source_focus_role_code"`
}

// Create validates and persists a new collaboration task.
func (s *Service) Create(tenantID string, req CreateRequest) (*Task, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if strings.TrimSpace(req.FromColleagueID) == "" {
		return nil, fmt.Errorf("from_colleague_id is required")
	}

	toColleagueID, toRoleCode, err := s.resolveRecipient(tenantID, req)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	task := &Task{
		ID:              idgen.New("collab"),
		Title:           title,
		Description:     strings.TrimSpace(req.Description),
		FromColleagueID: strings.TrimSpace(req.FromColleagueID),
		ToColleagueID:   toColleagueID,
		ToRoleCode:      toRoleCode,
		Status:          StatusPending,
		Priority:        req.Priority,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.InsertTask(tenantID, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	_ = s.repo.InsertEvent(tenantID, &TaskEvent{
		ID: idgen.New("cevt"), TaskID: task.ID,
		Event: "created", ActorID: task.FromColleagueID, CreatedAt: now,
	})

	return task, nil
}

func (s *Service) resolveRecipient(tenantID string, req CreateRequest) (string, string, error) {
	toColleagueID := strings.TrimSpace(req.ToColleagueID)
	toRoleCode := strings.TrimSpace(req.ToRoleCode)
	if toColleagueID != "" {
		return toColleagueID, toRoleCode, nil
	}
	if toRoleCode == "" {
		return "", "", fmt.Errorf("to_colleague_id or to_role_code is required")
	}
	selected, _, err := ResolveRoleAssignee(s.repo, s.colleagueRp, tenantID, toRoleCode)
	if err != nil {
		return "", toRoleCode, err
	}
	return selected.ID, toRoleCode, nil
}

func (s *Service) GetRoutingSettings(tenantID string) (RoutingSettings, error) {
	if s.repo == nil {
		return DefaultRoutingSettings(), nil
	}
	return s.repo.LoadRoutingSettings(tenantID)
}

func (s *Service) GetRoutingOverview(tenantID string) (RoutingOverview, error) {
	settings, err := s.GetRoutingSettings(tenantID)
	if err != nil {
		return RoutingOverview{}, err
	}
	colleagues := []*colleagueDomain.Colleague{}
	if s.colleagueRp != nil {
		colleagues, err = s.colleagueRp.ListActive(tenantID)
		if err != nil {
			return RoutingOverview{}, err
		}
	}
	return BuildRoutingOverview(settings, colleagues, time.Now()), nil
}

func (s *Service) SaveRoutingSettings(tenantID string, settings RoutingSettings) error {
	if s.repo == nil {
		return fmt.Errorf("routing settings store is unavailable")
	}
	return s.repo.SaveRoutingSettings(tenantID, settings)
}

func (s *Service) RecordHeartbeat(tenantID, colleagueID string, observedAt time.Time) error {
	if s.repo == nil {
		return fmt.Errorf("routing settings store is unavailable")
	}
	if strings.TrimSpace(colleagueID) == "" {
		return fmt.Errorf("colleague_id is required")
	}
	if s.colleagueRp != nil {
		if _, err := s.colleagueRp.GetByID(tenantID, strings.TrimSpace(colleagueID)); err != nil {
			return fmt.Errorf("colleague not found: %s", colleagueID)
		}
	}
	settings, err := s.repo.LoadRoutingSettings(tenantID)
	if err != nil {
		return err
	}
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	settings.LastHeartbeatByColleague[strings.TrimSpace(colleagueID)] = observedAt.UTC().Format(time.RFC3339)
	return s.repo.SaveRoutingSettings(tenantID, settings)
}

func (s *Service) ExecuteRoleRoutingAction(tenantID, roleCode, action string) (RoutingSettings, error) {
	roleCode = strings.TrimSpace(roleCode)
	if roleCode == "" {
		return RoutingSettings{}, fmt.Errorf("role_code is required")
	}
	if s.repo == nil {
		return RoutingSettings{}, fmt.Errorf("routing settings store is unavailable")
	}
	settings, err := s.repo.LoadRoutingSettings(tenantID)
	if err != nil {
		return RoutingSettings{}, err
	}
	if s.colleagueRp == nil {
		return RoutingSettings{}, fmt.Errorf("colleague routing is unavailable")
	}
	colleagues, err := s.colleagueRp.ListByRoleCode(tenantID, roleCode)
	if err != nil {
		return RoutingSettings{}, err
	}
	if len(colleagues) == 0 {
		return RoutingSettings{}, fmt.Errorf("no active colleague found for role %s", roleCode)
	}

	now := time.Now()
	activeCandidates := make([]*colleagueDomain.Colleague, 0, len(colleagues))
	standbyCandidates := make([]*colleagueDomain.Colleague, 0, len(colleagues))
	for _, colleague := range colleagues {
		status := routingStatusForColleague(colleague.ID, settings, now)
		switch status.EffectiveState {
		case RuntimeStateStandby:
			standbyCandidates = append(standbyCandidates, colleague)
		case RuntimeStateActive:
			activeCandidates = append(activeCandidates, colleague)
		}
	}

	switch strings.TrimSpace(action) {
	case RoleActionPromoteStandby:
		if len(standbyCandidates) == 0 {
			return RoutingSettings{}, fmt.Errorf("no standby colleague available for role %s", roleCode)
		}
		candidate := standbyCandidates[0]
		settings.RuntimeStateByColleague[candidate.ID] = RuntimeStateActive
		settings.PrimaryColleagueByRole[roleCode] = candidate.ID
		settings.RoleStrategies[roleCode] = StrategyPrimaryFirst
	case RoleActionPreferPrimary:
		candidate := (*colleagueDomain.Colleague)(nil)
		if len(activeCandidates) > 0 {
			candidate = activeCandidates[0]
		} else if len(standbyCandidates) > 0 {
			candidate = standbyCandidates[0]
		}
		if candidate == nil {
			return RoutingSettings{}, fmt.Errorf("no healthy colleague available for role %s", roleCode)
		}
		settings.RoleStrategies[roleCode] = StrategyPrimaryFirst
		settings.PrimaryColleagueByRole[roleCode] = candidate.ID
	case RoleActionBalanceLoad:
		settings.RoleStrategies[roleCode] = StrategyLeastLoaded
	default:
		return RoutingSettings{}, fmt.Errorf("unsupported routing action: %s", action)
	}

	if err := s.repo.SaveRoutingSettings(tenantID, settings); err != nil {
		return RoutingSettings{}, err
	}
	return settings, nil
}

// validTransitions defines allowed status transitions.
var validTransitions = map[string][]string{
	StatusPending:    {StatusAccepted, StatusRejected},
	StatusAccepted:   {StatusInProgress, StatusRejected},
	StatusInProgress: {StatusCompleted, StatusRejected},
}

// Transition moves a task to a new status with validation.
func (s *Service) Transition(tenantID string, taskID, newStatus, actorID, result, note string) error {
	task, err := s.repo.GetByID(tenantID, taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if task.IsTerminal() {
		return fmt.Errorf("task %s is in terminal status %s", taskID, task.Status)
	}

	allowed := validTransitions[task.Status]
	ok := false
	for _, s := range allowed {
		if s == newStatus {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("cannot transition from %s to %s", task.Status, newStatus)
	}

	if err := s.repo.UpdateStatus(tenantID, taskID, newStatus, result); err != nil {
		return err
	}

	_ = s.repo.InsertEvent(tenantID, &TaskEvent{
		ID: idgen.New("cevt"), TaskID: taskID,
		Event: newStatus, ActorID: actorID, Note: note, CreatedAt: time.Now(),
	})

	return nil
}

// GetByID returns a task by ID.
func (s *Service) GetByID(tenantID string, id string) (*Task, error) {
	return s.repo.GetByID(tenantID, id)
}

// ListByColleague returns tasks assigned to a colleague.
func (s *Service) ListByColleague(tenantID string, colleagueID string) ([]*Task, error) {
	return s.repo.ListByColleague(tenantID, colleagueID)
}

// ListAll returns all tasks.
func (s *Service) ListAll(tenantID string) ([]*Task, error) {
	return s.repo.ListAll(tenantID)
}

// ListEvents returns events for a task.
func (s *Service) ListEvents(tenantID string, taskID string) ([]*TaskEvent, error) {
	return s.repo.ListEvents(tenantID, taskID)
}
