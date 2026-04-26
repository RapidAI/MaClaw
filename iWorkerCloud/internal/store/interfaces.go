package store

import "context"

type CenterRepository interface {
	Create(ctx context.Context, c *Center) error
	GetByID(ctx context.Context, id string) (*Center, error)
	List(ctx context.Context) ([]*Center, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateHeartbeat(ctx context.Context, id string) error
	UpdateIntegration(ctx context.Context, c *Center) error
	Delete(ctx context.Context, id string) error
}

type LicenseRepository interface {
	Create(ctx context.Context, l *License) error
	GetByID(ctx context.Context, id string) (*License, error)
	GetByCenterID(ctx context.Context, centerID string) ([]*License, error)
	GetActiveByCenterID(ctx context.Context, centerID string) (*License, error)
	Revoke(ctx context.Context, id string) error
	List(ctx context.Context) ([]*License, error)
}

type AdminRepository interface {
	Create(ctx context.Context, a *Admin) error
	GetByUsername(ctx context.Context, username string) (*Admin, error)
	Count(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, id, passwordHash string) error
}

type SystemSettingsRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}
