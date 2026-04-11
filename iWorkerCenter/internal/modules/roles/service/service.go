package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// RoleService handles role business logic.
type RoleService struct {
	repo *repo.RoleRepo
}

// New creates a RoleService.
func New(r *repo.RoleRepo) *RoleService {
	return &RoleService{repo: r}
}

// CreateRequest holds fields for creating a role.
type CreateRequest struct {
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
	SortOrder        int      `json:"sort_order"`
}

// UpdateRequest holds fields for updating a role.
type UpdateRequest struct {
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
	SortOrder        *int     `json:"sort_order"`
}

// Create validates and persists a new role.
func (s *RoleService) Create(req CreateRequest) (*domain.Role, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}
	// check uniqueness
	if existing, _ := s.repo.GetByCode(code); existing != nil {
		return nil, fmt.Errorf("role code %q already exists", code)
	}

	strengths := req.DefaultStrengths
	if strengths == nil {
		strengths = []string{}
	}
	tasks := req.ApplicableTasks
	if tasks == nil {
		tasks = []string{}
	}

	now := time.Now()
	role := &domain.Role{
		ID:               idgen.New("role"),
		Name:             name,
		Code:             code,
		Description:      strings.TrimSpace(req.Description),
		DefaultStrengths: strengths,
		ApplicableTasks:  tasks,
		Status:           domain.StatusActive,
		SortOrder:        req.SortOrder,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.Insert(role); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	return role, nil
}

// Update modifies an existing role.
func (s *RoleService) Update(id string, req UpdateRequest) (*domain.Role, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("role not found: %w", err)
	}

	if name := strings.TrimSpace(req.Name); name != "" {
		existing.Name = name
	}
	if code := strings.TrimSpace(req.Code); code != "" {
		// check uniqueness if code changed
		if code != existing.Code {
			if dup, _ := s.repo.GetByCode(code); dup != nil {
				return nil, fmt.Errorf("role code %q already exists", code)
			}
		}
		existing.Code = code
	}
	if desc := strings.TrimSpace(req.Description); desc != "" {
		existing.Description = desc
	}
	if req.DefaultStrengths != nil {
		existing.DefaultStrengths = req.DefaultStrengths
	}
	if req.ApplicableTasks != nil {
		existing.ApplicableTasks = req.ApplicableTasks
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}
	existing.UpdatedAt = time.Now()

	if err := s.repo.Update(existing); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}
	return existing, nil
}

// GetByID returns a role by ID.
func (s *RoleService) GetByID(id string) (*domain.Role, error) {
	return s.repo.GetByID(id)
}

// GetByCode returns a role by code.
func (s *RoleService) GetByCode(code string) (*domain.Role, error) {
	return s.repo.GetByCode(code)
}

// List returns all roles.
func (s *RoleService) List() ([]*domain.Role, error) {
	return s.repo.List()
}

// ListActive returns only active roles.
func (s *RoleService) ListActive() ([]*domain.Role, error) {
	return s.repo.ListActive()
}

// SetStatus changes a role's status.
func (s *RoleService) SetStatus(id, status string) error {
	if status != domain.StatusActive && status != domain.StatusDisabled {
		return fmt.Errorf("invalid status: %s", status)
	}
	return s.repo.UpdateStatus(id, status)
}

// RecordAssignment logs a role change for audit.
func (s *RoleService) RecordAssignment(colleagueID, oldRoleID, newRoleID, reason string) error {
	log := &domain.RoleAssignmentLog{
		ID:          idgen.New("ralog"),
		ColleagueID: colleagueID,
		OldRoleID:   oldRoleID,
		NewRoleID:   newRoleID,
		Reason:      reason,
		AssignedAt:  time.Now(),
	}
	return s.repo.InsertAssignmentLog(log)
}

// GetAssignmentHistory returns role change history for a colleague.
func (s *RoleService) GetAssignmentHistory(colleagueID string) ([]*domain.RoleAssignmentLog, error) {
	return s.repo.ListAssignmentLogs(colleagueID)
}
