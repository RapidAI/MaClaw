package store

import (
	"context"
	"time"
)

type AdminUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Email        string    `json:"email"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AdminAuditLog struct {
	ID          string    `json:"id"`
	AdminUserID string    `json:"admin_user_id"`
	Action      string    `json:"action"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
}

type HubInstance struct {
	ID               string     `json:"id"`
	InstallationID   string     `json:"installation_id"`
	OwnerEmail       string     `json:"owner_email"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	BaseURL          string     `json:"base_url"`
	Host             string     `json:"host"`
	Port             int        `json:"port"`
	Visibility       string     `json:"visibility"`
	EnrollmentMode   string     `json:"enrollment_mode"`
	Status           string     `json:"status"`
	IsDisabled       bool       `json:"is_disabled"`
	DisabledReason   string     `json:"disabled_reason"`
	CapabilitiesJSON string     `json:"capabilities_json,omitempty"`
	HubSecretHash    string     `json:"hub_secret_hash,omitempty"`
	InvitationCodeRequired bool       `json:"invitation_code_required"`
	LastSeenAt             *time.Time `json:"last_seen_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type HubUserLink struct {
	ID        string    `json:"id"`
	HubID     string    `json:"hub_id"`
	Email     string    `json:"email"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BlockedEmail struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BlockedIP struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AdminUserRepository interface {
	Create(ctx context.Context, admin *AdminUser) error
	GetByUsername(ctx context.Context, username string) (*AdminUser, error)
	Count(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, username, passwordHash string, updatedAt time.Time) error
	UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error
	DeleteAll(ctx context.Context) error
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type AdminAuditRepository interface {
	Create(ctx context.Context, log *AdminAuditLog) error
}

type HubRepository interface {
	Create(ctx context.Context, hub *HubInstance) error
	GetByID(ctx context.Context, id string) (*HubInstance, error)
	GetByInstallationID(ctx context.Context, installationID string) (*HubInstance, error)
	UpdateHeartbeat(ctx context.Context, hubID string, at time.Time) error
	ListByEmail(ctx context.Context, email string) ([]*HubInstance, error)
	ListAll(ctx context.Context) ([]*HubInstance, error)
	UpdateVisibility(ctx context.Context, hubID string, visibility string, updatedAt time.Time) error
	SetDisabled(ctx context.Context, hubID string, disabled bool, reason string, updatedAt time.Time) error
	UpdateRegistration(ctx context.Context, hub *HubInstance) error
	UpdateInvitationCodeRequired(ctx context.Context, hubID string, required bool, updatedAt time.Time) error
	DeleteByID(ctx context.Context, hubID string) error
}

type HubUserLinkRepository interface {
	Create(ctx context.Context, link *HubUserLink) error
	ListByEmail(ctx context.Context, email string) ([]*HubUserLink, error)
	GetDefaultByEmail(ctx context.Context, email string) (*HubUserLink, error)
	DeleteByHubID(ctx context.Context, hubID string) error
}

type BlockedEmailRepository interface {
	GetByEmail(ctx context.Context, email string) (*BlockedEmail, error)
	Create(ctx context.Context, item *BlockedEmail) error
	DeleteByEmail(ctx context.Context, email string) error
	List(ctx context.Context) ([]*BlockedEmail, error)
}

type BlockedIPRepository interface {
	GetByIP(ctx context.Context, ip string) (*BlockedIP, error)
	Create(ctx context.Context, item *BlockedIP) error
	DeleteByIP(ctx context.Context, ip string) error
	List(ctx context.Context) ([]*BlockedIP, error)
}

type Store struct {
	Admins        AdminUserRepository
	System        SystemSettingsRepository
	AdminAudit    AdminAuditRepository
	Hubs          HubRepository
	HubUserLinks  HubUserLinkRepository
	BlockedEmails BlockedEmailRepository
	BlockedIPs    BlockedIPRepository
}
