package llmservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const AdminAPIKeySettingKey = "llm_admin_api_keys"

const adminAPIKeyPrefix = "hck_"

// AdminAPIKey is a stored automation credential. The secret is returned only
// from CreateAdminAPIKey; the registry keeps a hash and a short prefix.
type AdminAPIKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	Hash       string    `json:"hash"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	Revoked    bool      `json:"revoked,omitempty"`
}

// AdminAPIKeyCreated is the one-time response that includes the secret.
type AdminAPIKeyCreated struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	APIKey    string    `json:"api_key"`
	CreatedAt time.Time `json:"created_at"`
}

type adminAPIKeyFile struct {
	Keys []AdminAPIKey `json:"keys"`
}

func (s *Service) adminKeyLock() func() {
	if s == nil {
		return func() {}
	}
	s.adminKeyMu.Lock()
	return s.adminKeyMu.Unlock
}

// CreateAdminAPIKey stores a new automation key. The plaintext is not stored.
func (s *Service) CreateAdminAPIKey(ctx context.Context, name string) (*AdminAPIKeyCreated, error) {
	if s == nil || s.system == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(name) > 80 {
		return nil, fmt.Errorf("name is too long")
	}
	secret, err := newAdminAPIKeySecret()
	if err != nil {
		return nil, err
	}
	id, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := AdminAPIKey{
		ID:        "key_" + id,
		Name:      name,
		Prefix:    adminAPIKeyPrefix + secret[len(adminAPIKeyPrefix):len(adminAPIKeyPrefix)+8],
		Hash:      hashAdminAPIKey(secret),
		CreatedAt: now,
	}
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	file.Keys = append(file.Keys, record)
	if err := s.saveAdminAPIKeys(ctx, file); err != nil {
		return nil, err
	}
	return &AdminAPIKeyCreated{
		ID:        record.ID,
		Name:      record.Name,
		Prefix:    record.Prefix,
		APIKey:    secret,
		CreatedAt: record.CreatedAt,
	}, nil
}

// ListAdminAPIKeys returns active keys without secrets.
func (s *Service) ListAdminAPIKeys(ctx context.Context) ([]AdminAPIKey, error) {
	if s == nil || s.system == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdminAPIKey, 0, len(file.Keys))
	for i := len(file.Keys) - 1; i >= 0; i-- {
		item := file.Keys[i]
		if item.Revoked {
			continue
		}
		item.Hash = ""
		out = append(out, item)
	}
	return out, nil
}

// RevokeAdminAPIKey disables a key. Revoked secrets cannot be used again.
func (s *Service) RevokeAdminAPIKey(ctx context.Context, id string) error {
	if s == nil || s.system == nil {
		return fmt.Errorf("llm service is required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("api key id required")
	}
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return err
	}
	found := false
	for i := range file.Keys {
		if file.Keys[i].ID != id || file.Keys[i].Revoked {
			continue
		}
		file.Keys[i].Revoked = true
		found = true
		break
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	return s.saveAdminAPIKeys(ctx, file)
}

// AuthenticateAdminAPIKey reports whether secret is an active automation key.
// A matching key records last use at most once a minute.
func (s *Service) AuthenticateAdminAPIKey(ctx context.Context, secret string) error {
	if s == nil || s.system == nil {
		return fmt.Errorf("llm service is required")
	}
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, adminAPIKeyPrefix) || len(secret) < len(adminAPIKeyPrefix)+16 {
		return fmt.Errorf("invalid api key")
	}
	sum := hashAdminAPIKey(secret)
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range file.Keys {
		item := &file.Keys[i]
		if item.Revoked || item.Hash == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(item.Hash), []byte(sum)) != 1 {
			continue
		}
		if item.LastUsedAt.IsZero() || now.Sub(item.LastUsedAt) >= time.Minute {
			item.LastUsedAt = now
			_ = s.saveAdminAPIKeys(ctx, file)
		}
		return nil
	}
	return fmt.Errorf("invalid api key")
}

func (s *Service) loadAdminAPIKeys(ctx context.Context) (*adminAPIKeyFile, error) {
	raw, err := s.system.Get(ctx, AdminAPIKeySettingKey)
	if err != nil {
		return nil, fmt.Errorf("load admin api keys: %w", err)
	}
	file := &adminAPIKeyFile{}
	if strings.TrimSpace(raw) == "" {
		return file, nil
	}
	if err := json.Unmarshal([]byte(raw), file); err != nil {
		return nil, fmt.Errorf("parse admin api keys: %w", err)
	}
	return file, nil
}

func (s *Service) saveAdminAPIKeys(ctx context.Context, file *adminAPIKeyFile) error {
	if file == nil {
		file = &adminAPIKeyFile{}
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if err := s.system.Set(ctx, AdminAPIKeySettingKey, string(raw)); err != nil {
		return fmt.Errorf("save admin api keys: %w", err)
	}
	return nil
}

func newAdminAPIKeySecret() (string, error) {
	body, err := randomHex(32)
	if err != nil {
		return "", err
	}
	return adminAPIKeyPrefix + body, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashAdminAPIKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
