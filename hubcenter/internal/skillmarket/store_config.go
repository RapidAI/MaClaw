package skillmarket

import (
	"context"
	"database/sql"
	"errors"
)

// 鈹€鈹€ AdminConfigRepository implementation 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

// GetConfig 鑾峰彇閰嶇疆鍊笺€?
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

// SetConfig 璁剧疆閰嶇疆鍊硷紙UPSERT锛夈€?
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_admin_config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

// GetConfigWithDefault 鑾峰彇閰嶇疆鍊硷紝涓嶅瓨鍦ㄦ椂杩斿洖榛樿鍊笺€?
func (s *Store) GetConfigWithDefault(ctx context.Context, key, defaultVal string) string {
	val, err := s.GetConfig(ctx, key)
	if err != nil {
		return defaultVal
	}
	return val
}
