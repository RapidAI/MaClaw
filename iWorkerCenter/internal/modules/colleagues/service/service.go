package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	roleSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// ColleagueService handles colleague business logic.
type ColleagueService struct {
	repo     *repo.ColleagueRepo
	roleRepo *roleRepo.RoleRepo
	roleSvc  *roleSvc.RoleService
}

// New creates a ColleagueService.
func New(r *repo.ColleagueRepo, rr *roleRepo.RoleRepo, rs *roleSvc.RoleService) *ColleagueService {
	return &ColleagueService{repo: r, roleRepo: rr, roleSvc: rs}
}

// CreateRequest holds the fields for creating a colleague.
type CreateRequest struct {
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	RoleID      string   `json:"role_id"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
}

// UpdateRequest holds the fields for updating a colleague.
type UpdateRequest struct {
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
}

// AssignRoleRequest holds the fields for assigning a role to a colleague.
type AssignRoleRequest struct {
	RoleID string `json:"role_id"`
	Reason string `json:"reason"`
}

// Create validates and persists a new colleague.
func (s *ColleagueService) Create(tenantID string, req CreateRequest) (*domain.Colleague, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		return nil, fmt.Errorf("role_id is required")
	}
	// validate role exists and is active
	role, err := s.roleRepo.GetByID(tenantID, roleID)
	if err != nil {
		return nil, fmt.Errorf("role not found: %s", roleID)
	}
	if role.Status != "active" {
		return nil, fmt.Errorf("role %s is not active", roleID)
	}

	strengths := req.Strengths
	if strengths == nil {
		strengths = []string{}
	}
	tasks := req.Tasks
	if tasks == nil {
		tasks = []string{}
	}

	now := time.Now()
	c := &domain.Colleague{
		ID:          idgen.New("col"),
		Name:        name,
		Avatar:      strings.TrimSpace(req.Avatar),
		RoleID:      roleID,
		Description: strings.TrimSpace(req.Description),
		Strengths:   strengths,
		Tasks:       tasks,
		Status:      domain.StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.Insert(tenantID, c); err != nil {
		return nil, fmt.Errorf("create colleague: %w", err)
	}

	// record initial assignment
	_ = s.roleSvc.RecordAssignment(tenantID, c.ID, "", roleID, "initial creation")

	return c, nil
}

// Update modifies an existing colleague (does not change role — use AssignRole for that).
func (s *ColleagueService) Update(tenantID string, id string, req UpdateRequest) (*domain.Colleague, error) {
	existing, err := s.repo.GetByID(tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("colleague not found: %w", err)
	}

	if name := strings.TrimSpace(req.Name); name != "" {
		existing.Name = name
	}
	if avatar := strings.TrimSpace(req.Avatar); avatar != "" {
		existing.Avatar = avatar
	}
	if desc := strings.TrimSpace(req.Description); desc != "" {
		existing.Description = desc
	}
	if req.Strengths != nil {
		existing.Strengths = req.Strengths
	}
	if req.Tasks != nil {
		existing.Tasks = req.Tasks
	}
	existing.UpdatedAt = time.Now()

	if err := s.repo.Update(tenantID, existing); err != nil {
		return nil, fmt.Errorf("update colleague: %w", err)
	}
	return existing, nil
}

// AssignRole changes a colleague's role and records the change.
func (s *ColleagueService) AssignRole(tenantID string, colleagueID string, req AssignRoleRequest) error {
	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		return fmt.Errorf("role_id is required")
	}

	// validate new role
	role, err := s.roleRepo.GetByID(tenantID, roleID)
	if err != nil {
		return fmt.Errorf("role not found: %s", roleID)
	}
	if role.Status != "active" {
		return fmt.Errorf("role %s is not active", roleID)
	}

	// get current colleague
	colleague, err := s.repo.GetByID(tenantID, colleagueID)
	if err != nil {
		return fmt.Errorf("colleague not found: %s", colleagueID)
	}

	oldRoleID := colleague.RoleID
	if oldRoleID == roleID {
		return nil // no change
	}

	// update
	if err := s.repo.UpdateRoleID(tenantID, colleagueID, roleID); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}

	// audit log
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "role reassignment"
	}
	_ = s.roleSvc.RecordAssignment(tenantID, colleagueID, oldRoleID, roleID, reason)

	return nil
}

// GetByID returns a colleague by ID.
func (s *ColleagueService) GetByID(tenantID string, id string) (*domain.Colleague, error) {
	return s.repo.GetByID(tenantID, id)
}

// List returns all colleagues.
func (s *ColleagueService) List(tenantID string) ([]*domain.Colleague, error) {
	return s.repo.List(tenantID)
}

// ListActive returns only active colleagues (for client API).
func (s *ColleagueService) ListActive(tenantID string) ([]*domain.Colleague, error) {
	return s.repo.ListActive(tenantID)
}

// ListByRoleID returns active colleagues assigned to a specific role.
func (s *ColleagueService) ListByRoleID(tenantID string, roleID string) ([]*domain.Colleague, error) {
	return s.repo.ListByRoleID(tenantID, roleID)
}

// SetStatus changes a colleague's status.
func (s *ColleagueService) SetStatus(tenantID string, id, status string) error {
	if status != domain.StatusActive && status != domain.StatusDisabled {
		return fmt.Errorf("invalid status: %s", status)
	}
	return s.repo.UpdateStatus(tenantID, id, status)
}
