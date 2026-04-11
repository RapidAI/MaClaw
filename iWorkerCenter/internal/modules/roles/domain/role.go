package domain

import "time"

// Status values for a role.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Role is an independent entity representing a job function in the organization.
// Workflow steps are assigned by role, not by individual colleague.
type Role struct {
	ID               string
	Name             string   // human-readable, e.g. "办公同事"
	Code             string   // machine key, e.g. "office", unique
	Description      string
	DefaultStrengths []string // default strengths inherited by colleagues assigned this role
	ApplicableTasks  []string // task types this role can handle
	Status           string
	SortOrder        int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsActive returns true when the role is usable.
func (r *Role) IsActive() bool { return r.Status == StatusActive }

// RoleAssignmentLog records every role change on a colleague for audit.
type RoleAssignmentLog struct {
	ID           string
	ColleagueID  string
	OldRoleID    string
	NewRoleID    string
	Reason       string
	AssignedAt   time.Time
}
