package llmservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const AdminAPIKeySettingKey = "llm_admin_api_keys"

const adminAPIKeyLastUsedSettingKey = "llm_admin_api_key_last_used"

const adminAPIKeyPrefix = "hck_"

const maxActiveAdminAPIKeys = 32

// ErrAdminAPIKeyExpired and ErrAdminAPIKeyScope classify auth failures
// without matching unrelated error text.
var (
	ErrAdminAPIKeyExpired = errors.New("api key expired")
	ErrAdminAPIKeyScope   = errors.New("api key scope")
)

// Admin API key scopes. An empty scope list means every provider-management
// scope. Keys cannot create or revoke other admin keys.
const (
	AdminAPIScopeRead   = "read"
	AdminAPIScopeWrite  = "write"
	AdminAPIScopeDelete = "delete"
	AdminAPIScopeTest   = "test"
)

// AdminAPIKey is a stored automation credential. APIKey is the saved secret so
// the admin console can show, hide, and copy it later. Hash authenticates the
// key and is omitted from list responses. Authorization copies clear APIKey.
// Keys created before secrets were stored have an empty APIKey.
type AdminAPIKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	APIKey     string    `json:"api_key,omitempty"`
	Hash       string    `json:"hash,omitempty"`
	Scopes     []string  `json:"scopes,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	Revoked    bool      `json:"revoked,omitempty"`
}

// AdminAPIKeySpec is the input for a new automation key.
// Nil Scopes means every provider-management scope.
type AdminAPIKeySpec struct {
	Name      string
	Scopes    []string
	ExpiresAt time.Time
}

// AdminAPIKeyCreated is the create response. The same secret is stored on the
// key record for the admin console.
type AdminAPIKeyCreated struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	APIKey    string    `json:"api_key"`
	Scopes    []string  `json:"scopes,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
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

// CreateAdminAPIKey stores a new automation key with every provider-management
// scope and no expiry. The secret is saved for the admin console.
func (s *Service) CreateAdminAPIKey(ctx context.Context, name string) (*AdminAPIKeyCreated, error) {
	return s.CreateAdminAPIKeySpec(ctx, AdminAPIKeySpec{Name: name})
}

// CreateAdminAPIKeySpec stores a new automation key and its secret.
func (s *Service) CreateAdminAPIKeySpec(ctx context.Context, spec AdminAPIKeySpec) (*AdminAPIKeyCreated, error) {
	if s == nil || s.system == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > 80 {
		return nil, fmt.Errorf("name is too long")
	}
	scopes, err := normalizeAdminAPIScopes(spec.Scopes)
	if err != nil {
		return nil, err
	}
	if !spec.ExpiresAt.IsZero() && !spec.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("expires_at must be in the future")
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
		APIKey:    secret,
		Hash:      hashAdminAPIKey(secret),
		Scopes:    scopes,
		ExpiresAt: spec.ExpiresAt.UTC(),
		CreatedAt: now,
	}
	if spec.ExpiresAt.IsZero() {
		record.ExpiresAt = time.Time{}
	}
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	active := 0
	for _, item := range file.Keys {
		if !item.Revoked {
			active++
		}
	}
	if active >= maxActiveAdminAPIKeys {
		return nil, fmt.Errorf("too many admin api keys")
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
		Scopes:    append([]string(nil), record.Scopes...),
		ExpiresAt: record.ExpiresAt,
		CreatedAt: record.CreatedAt,
	}, nil
}

// ListAdminAPIKeys returns active keys for the admin console, including each
// saved secret. The hash is omitted. Automation keys cannot call this list.
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
	used, err := s.loadAdminAPIKeyLastUsed(ctx)
	if err != nil {
		used = nil
	}
	out := make([]AdminAPIKey, 0, len(file.Keys))
	for i := len(file.Keys) - 1; i >= 0; i-- {
		item := file.Keys[i]
		if item.Revoked {
			continue
		}
		if ts, ok := used[item.ID]; ok && !ts.IsZero() {
			item.LastUsedAt = ts
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
	for i := range file.Keys {
		if file.Keys[i].ID != id {
			continue
		}
		if file.Keys[i].Revoked {
			return nil
		}
		file.Keys[i].Revoked = true
		file.Keys[i].APIKey = ""
		if err := s.saveAdminAPIKeys(ctx, file); err != nil {
			return err
		}
		_ = s.forgetAdminAPIKeyLastUsed(ctx, id)
		return nil
	}
	return fmt.Errorf("api key not found")
}

// AuthenticateAdminAPIKey reports whether secret is an active automation key.
// A matching key records last use at most once a minute, outside the secret file.
func (s *Service) AuthenticateAdminAPIKey(ctx context.Context, secret string) error {
	_, err := s.AuthorizeAdminAPIKey(ctx, secret, "")
	return err
}

// AuthorizeAdminAPIKey checks the secret, expiry, and an optional scope.
// An empty scope only checks that the key is active. Empty stored scopes allow
// every provider-management scope.
func (s *Service) AuthorizeAdminAPIKey(ctx context.Context, secret, scope string) (*AdminAPIKey, error) {
	if s == nil || s.system == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, adminAPIKeyPrefix) || len(secret) < len(adminAPIKeyPrefix)+16 {
		return nil, fmt.Errorf("invalid api key")
	}
	sum := hashAdminAPIKey(secret)
	unlock := s.adminKeyLock()
	defer unlock()
	file, err := s.loadAdminAPIKeys(ctx)
	if err != nil {
		return nil, err
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
		if !item.ExpiresAt.IsZero() && !now.Before(item.ExpiresAt) {
			return nil, ErrAdminAPIKeyExpired
		}
		if !adminAPIKeyAllows(item.Scopes, scope) {
			return nil, fmt.Errorf("%w: missing the %s scope", ErrAdminAPIKeyScope, strings.TrimSpace(scope))
		}
		if ts, ok := s.touchAdminAPIKeyLastUsed(ctx, item.ID, now); ok {
			item.LastUsedAt = ts
		}
		out := *item
		out.Hash = ""
		out.APIKey = ""
		return &out, nil
	}
	return nil, fmt.Errorf("invalid api key")
}

func adminAPIKeyAllows(scopes []string, scope string) bool {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" || len(scopes) == 0 {
		return true
	}
	for _, item := range scopes {
		if item == scope {
			return true
		}
	}
	return false
}

func normalizeAdminAPIScopes(scopes []string) ([]string, error) {
	if scopes == nil {
		return nil, nil
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("scopes must include at least one of read, write, delete, test")
	}
	allowed := []string{AdminAPIScopeRead, AdminAPIScopeWrite, AdminAPIScopeDelete, AdminAPIScopeTest}
	seen := map[string]struct{}{}
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		ok := false
		for _, item := range allowed {
			if scope == item {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("unknown scope %q", strings.TrimSpace(raw))
		}
		seen[scope] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for _, item := range allowed {
		if _, ok := seen[item]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// loadAdminAPIKeyLastUsed reads usage times. Callers hold adminKeyMu.
// A missing or unreadable usage record does not affect the stored secrets.
func (s *Service) loadAdminAPIKeyLastUsed(ctx context.Context) (map[string]time.Time, error) {
	raw, err := s.system.Get(ctx, adminAPIKeyLastUsedSettingKey)
	if err != nil {
		return nil, err
	}
	used := map[string]time.Time{}
	if strings.TrimSpace(raw) == "" {
		return used, nil
	}
	var file struct {
		Used map[string]time.Time `json:"used"`
	}
	if err := json.Unmarshal([]byte(raw), &file); err != nil {
		return nil, err
	}
	if file.Used == nil {
		return used, nil
	}
	return file.Used, nil
}

func (s *Service) saveAdminAPIKeyLastUsed(ctx context.Context, used map[string]time.Time) error {
	if used == nil {
		used = map[string]time.Time{}
	}
	raw, err := json.Marshal(struct {
		Used map[string]time.Time `json:"used"`
	}{Used: used})
	if err != nil {
		return err
	}
	if err := s.system.Set(ctx, adminAPIKeyLastUsedSettingKey, string(raw)); err != nil {
		return fmt.Errorf("save admin api key usage: %w", err)
	}
	return nil
}

// touchAdminAPIKeyLastUsed records use at most once a minute in a separate
// setting, so authentication does not rewrite the stored secrets.
func (s *Service) touchAdminAPIKeyLastUsed(ctx context.Context, id string, now time.Time) (time.Time, bool) {
	used, err := s.loadAdminAPIKeyLastUsed(ctx)
	if err != nil {
		return time.Time{}, false
	}
	prev := used[id]
	if !prev.IsZero() && now.Sub(prev) < time.Minute {
		return prev, true
	}
	used[id] = now
	if err := s.saveAdminAPIKeyLastUsed(ctx, used); err != nil {
		if prev.IsZero() {
			return time.Time{}, false
		}
		return prev, true
	}
	return now, true
}

func (s *Service) forgetAdminAPIKeyLastUsed(ctx context.Context, id string) error {
	used, err := s.loadAdminAPIKeyLastUsed(ctx)
	if err != nil || used[id].IsZero() {
		return err
	}
	delete(used, id)
	return s.saveAdminAPIKeyLastUsed(ctx, used)
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
