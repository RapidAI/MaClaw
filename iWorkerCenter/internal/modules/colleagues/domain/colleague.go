package domain

import "time"

// Status values.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Colleague is the core domain entity for a digital worker.
// Role is managed via role_id referencing the roles table.
type Colleague struct {
	ID          string
	Name        string
	Avatar      string
	RoleID      string   // FK to roles.id
	Description string
	Strengths   []string
	Tasks       []string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsActive returns true if the colleague is in active status.
func (c *Colleague) IsActive() bool {
	return c.Status == StatusActive
}
