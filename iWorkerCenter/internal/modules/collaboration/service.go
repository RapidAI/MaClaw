package collaboration

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// Service handles collaboration task business logic.
type Service struct {
	repo *Repo
}

// NewService creates a Service.
func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

// Repo exposes the underlying repo for transaction-aware callers (e.g. workflow engine).
func (s *Service) Repo() *Repo { return s.repo }

// CreateRequest holds fields for creating a collaboration task.
type CreateRequest struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	FromColleagueID string `json:"from_colleague_id"`
	ToColleagueID   string `json:"to_colleague_id"`
	ToRoleCode      string `json:"to_role_code"`
	Priority        int    `json:"priority"`
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
	if strings.TrimSpace(req.ToColleagueID) == "" {
		return nil, fmt.Errorf("to_colleague_id is required")
	}

	now := time.Now()
	task := &Task{
		ID:              idgen.New("collab"),
		Title:           title,
		Description:     strings.TrimSpace(req.Description),
		FromColleagueID: strings.TrimSpace(req.FromColleagueID),
		ToColleagueID:   strings.TrimSpace(req.ToColleagueID),
		ToRoleCode:      strings.TrimSpace(req.ToRoleCode),
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
