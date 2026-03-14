package store

import (
	"context"
	"time"
)

type AdminUser struct {
	ID           string
	Username     string
	PasswordHash string
	Email        string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AdminAuditLog struct {
	ID          string
	AdminUserID string
	Action      string
	PayloadJSON string
	CreatedAt   time.Time
}

type User struct {
	ID               string
	Email            string
	SN               string
	Status           string
	EnrollmentStatus string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UserEnrollment struct {
	ID        string
	Email     string
	Status    string
	Note      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EmailBlockItem struct {
	ID        string
	Email     string
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EmailInvite struct {
	ID        string
	Email     string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Machine struct {
	ID               string
	UserID           string
	Name             string
	Platform         string
	Hostname         string
	Arch             string
	AppVersion       string
	HeartbeatSec     int
	MachineTokenHash string
	Status           string
	LastSeenAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MachineMetadata struct {
	Name                 string
	Platform             string
	Hostname             string
	Arch                 string
	AppVersion           string
	HeartbeatIntervalSec int
}

type ViewerToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	RevokedAt *time.Time
}

type LoginToken struct {
	ID         string
	Email      string
	TokenHash  string
	Purpose    string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

type Session struct {
	ID          string
	MachineID   string
	UserID      string
	Tool        string
	Title       string
	ProjectPath string
	Status      string
	SummaryJSON string
	PreviewText string
	OutputSeq   int64
	HostOnline  bool
	StartedAt   time.Time
	UpdatedAt   time.Time
	EndedAt     *time.Time
	ExitCode    *int
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

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context) ([]*User, error)
}

type EnrollmentRepository interface {
	Create(ctx context.Context, item *UserEnrollment) error
	GetPendingByEmail(ctx context.Context, email string) (*UserEnrollment, error)
	ListPending(ctx context.Context) ([]*UserEnrollment, error)
	Approve(ctx context.Context, id string, updatedAt time.Time) error
	Reject(ctx context.Context, id string, updatedAt time.Time) error
}

type EmailBlocklistRepository interface {
	Create(ctx context.Context, item *EmailBlockItem) error
	DeleteByEmail(ctx context.Context, email string) error
	GetByEmail(ctx context.Context, email string) (*EmailBlockItem, error)
	List(ctx context.Context) ([]*EmailBlockItem, error)
}

type EmailInviteRepository interface {
	Create(ctx context.Context, item *EmailInvite) error
	UpdateStatus(ctx context.Context, id string, status string) error
	GetByEmail(ctx context.Context, email string) ([]*EmailInvite, error)
	List(ctx context.Context) ([]*EmailInvite, error)
}

type MachineRepository interface {
	Create(ctx context.Context, machine *Machine) error
	GetByID(ctx context.Context, id string) (*Machine, error)
	ListByUserID(ctx context.Context, userID string) ([]*Machine, error)
	UpdateMetadata(ctx context.Context, machineID string, metadata MachineMetadata) error
	UpdateStatus(ctx context.Context, machineID string, status string) error
	UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error
}

type ViewerTokenRepository interface {
	Create(ctx context.Context, token *ViewerToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*ViewerToken, error)
}

type LoginTokenRepository interface {
	Create(ctx context.Context, token *LoginToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*LoginToken, error)
	Consume(ctx context.Context, tokenID string, consumedAt time.Time) error
}

type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	UpdateSummary(ctx context.Context, sessionID string, summaryJSON string, status string, updatedAt time.Time) error
	UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error
	UpdateHostOnline(ctx context.Context, sessionID string, hostOnline bool, updatedAt time.Time) error
	Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error
}

type Store struct {
	Admins       AdminUserRepository
	System       SystemSettingsRepository
	AdminAudit   AdminAuditRepository
	Users        UserRepository
	Enrollments  EnrollmentRepository
	EmailBlocks  EmailBlocklistRepository
	EmailInvites EmailInviteRepository
	Machines     MachineRepository
	ViewerTokens ViewerTokenRepository
	LoginTokens  LoginTokenRepository
	Sessions     SessionRepository
}
