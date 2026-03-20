package skillmarket

import (
	"context"
	"database/sql"
	"errors"
)

// ── AdminConfigRepository implementation ────────────────────────────────

// GetConfig 获取配置值。
func (s *Store) GetConfig(ctx context.Context, key string) (string, error) {
	var val string
	err := s.readDB.QueryRowContext(ctx, `SELECT value FROM sm_admin_config WHERE key = ?`, key).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return val, nil
}

// SetConfig 设置配置值（UPSERT）。
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_admin_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetConfigWithDefault 获取配置值，不存在时返回默认值。
func (s *Store) GetConfigWithDefault(ctx context.Context, key, defaultVal string) string {
	val, err := s.GetConfig(ctx, key)
	if err != nil {
		return defaultVal
	}
	return val
}
