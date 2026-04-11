package store

import "time"

// Center represents a registered iWorkerCenter instance.
type Center struct {
	ID            string    `json:"id"`
	CompanyName   string    `json:"company_name"`
	AdminEmail    string    `json:"admin_email"`
	AdminPhone    string    `json:"admin_phone"`
	Address       string    `json:"address"`
	LegalPerson   string    `json:"legal_person"`
	Status        string    `json:"status"` // pending, active, disabled
	SecretHash    string    `json:"-"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// License represents an authorization certificate for a Center.
type License struct {
	ID          string    `json:"id"`
	CenterID    string    `json:"center_id"`
	Modules     string    `json:"modules"` // JSON array of module names
	Type        string    `json:"type"`    // trial, manual
	ExpiresAt   time.Time `json:"expires_at"`
	IsLongTerm  bool      `json:"is_long_term"` // 0 days = permanent
	Certificate string    `json:"certificate"`  // signed JSON
	CreatedAt   time.Time `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// Admin represents a cloud admin account.
type Admin struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// SystemSetting is a key-value pair for system configuration.
type SystemSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
