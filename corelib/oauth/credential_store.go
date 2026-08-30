package oauth

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// CredentialStore — OAuth credential 的持久化接口。
// modify 是唯一的写路径，内部串行化，防止并发 double-refresh。
// ─────────────────────────────────────────────────────────────────────────────

// CredentialStore 定义 OAuth credential 的读写接口。
// 实现必须保证 Modify 的原子性（read-modify-write 在同一把锁内）。
type CredentialStore interface {
	// Read 读取指定 provider 的 credential。不存在时返回 nil, nil。
	Read(providerID string) (*StoredCredential, error)

	// Modify 是唯一的写入路径。fn 接收当前 credential（可能为 nil），
	// 返回更新后的 credential。fn 内部可以做 NeedsRefresh 检查——
	// 如果另一个 goroutine 已刷新，fn 可以直接返回 old。
	// 内部串行化保证不会 double-refresh。
	Modify(providerID string, fn func(old *StoredCredential) (*StoredCredential, error)) error

	// Delete 删除指定 provider 的 credential（logout）。
	Delete(providerID string) error
}

// StoredCredential 统一存储 OAuth 和 SSO 的 credential。
type StoredCredential struct {
	Type           string `json:"type"`                       // "oauth" | "sso"
	AccessToken    string `json:"access_token"`               // 主 token（sk-... 或 raw access_token）
	RawAccessToken string `json:"raw_access_token,omitempty"` // 原始 access_token（Responses API）；组织账单需要 Admin API Key
	RefreshToken   string `json:"refresh_token,omitempty"`
	ExpiresAt      int64  `json:"expires_at,omitempty"` // Unix timestamp
	// SSO-specific fields
	BaseURL string `json:"base_url,omitempty"`
	Email   string `json:"email,omitempty"`
	ModelID string `json:"model_id,omitempty"`
}

// IsExpired 检查 credential 是否已过期或即将过期（含 5 分钟 margin）。
func (c *StoredCredential) IsExpired() bool {
	if c == nil || c.ExpiresAt == 0 {
		return false // 无过期信息视为不过期
	}
	return time.Now().Unix()+int64(TokenRefreshMargin.Seconds()) >= c.ExpiresAt
}

// ─────────────────────────────────────────────────────────────────────────────
// FileCredentialStore — 文件持久化实现
// 存储到 ~/.maclaw/credentials.json，与 config.json 同级但独立。
// 使用独立的 sync.Mutex，不与 configMu 竞争。
// ─────────────────────────────────────────────────────────────────────────────

// FileCredentialStore 将 credentials 持久化到磁盘。
type FileCredentialStore struct {
	mu   sync.Mutex
	path string
}

// NewFileCredentialStore 创建一个文件存储。path 是 credentials.json 的完整路径。
func NewFileCredentialStore(path string) *FileCredentialStore {
	return &FileCredentialStore{path: path}
}

// DefaultCredentialStorePath 返回默认的 credentials.json 路径。
// 使用 DefaultBaseDir（~/.maclaw/）确保与 config.json 同目录。
func DefaultCredentialStorePath() string {
	// 延迟导入 maclawpath 避免循环依赖——直接用 home dir 拼接
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".maclaw", "credentials.json")
	}
	return filepath.Join(home, ".maclaw", "credentials.json")
}

func (s *FileCredentialStore) Read(providerID string) (*StoredCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	cred, ok := all[providerID]
	if !ok {
		return nil, nil
	}
	return cred, nil
}

func (s *FileCredentialStore) Modify(providerID string, fn func(old *StoredCredential) (*StoredCredential, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadLocked()
	if err != nil {
		return err
	}

	old := all[providerID] // 可能为 nil
	updated, err := fn(old)
	if err != nil {
		return err
	}

	// Callers may mutate the existing *StoredCredential in place and return
	// the same pointer. Pointer equality therefore cannot mean "no change".
	if updated == nil {
		if old == nil {
			return nil
		}
		delete(all, providerID)
	} else {
		all[providerID] = updated
	}
	return s.saveLocked(all)
}

func (s *FileCredentialStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadLocked()
	if err != nil {
		return err
	}
	if _, ok := all[providerID]; !ok {
		return nil // 不存在，no-op
	}
	delete(all, providerID)
	return s.saveLocked(all)
}

// loadLocked 从磁盘加载。文件不存在时返回空 map。
// 调用方必须持有 s.mu。
func (s *FileCredentialStore) loadLocked() (map[string]*StoredCredential, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*StoredCredential), nil
		}
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	if len(data) == 0 {
		return make(map[string]*StoredCredential), nil
	}
	var all map[string]*StoredCredential
	if err := json.Unmarshal(data, &all); err != nil {
		log.Printf("[credential-store] parse error (resetting): %v", err)
		return make(map[string]*StoredCredential), nil
	}
	if all == nil {
		all = make(map[string]*StoredCredential)
	}
	return all, nil
}

// saveLocked 原子写入磁盘（临时文件 + rename）。
// 调用方必须持有 s.mu。
func (s *FileCredentialStore) saveLocked(all map[string]*StoredCredential) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	// 原子写入：写临时文件 → rename
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write credentials tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return fmt.Errorf("rename credentials: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// MigrateFromConfig — 从旧 config.json 迁移 credential 到 credential store
// ─────────────────────────────────────────────────────────────────────────────

// MigrationSource 包含从旧 config 中提取的 credential 信息。
type MigrationSource struct {
	ProviderID     string
	Type           string // "oauth" | "sso"
	AccessToken    string
	RawAccessToken string
	RefreshToken   string
	ExpiresAt      int64
	BaseURL        string
	Email          string
	ModelID        string
}

// MigrateFromConfig 将旧 config.json 中的 credential 迁移到 CredentialStore。
// 仅迁移 store 中尚不存在的 provider（不覆盖已有的）。
// 返回实际迁移的 provider 数量。
func MigrateFromConfig(store CredentialStore, sources []MigrationSource) int {
	migrated := 0
	for _, src := range sources {
		if src.AccessToken == "" {
			continue
		}
		existing, _ := store.Read(src.ProviderID)
		if existing != nil {
			continue // store 中已有，不覆盖
		}
		cred := &StoredCredential{
			Type:           src.Type,
			AccessToken:    src.AccessToken,
			RawAccessToken: src.RawAccessToken,
			RefreshToken:   src.RefreshToken,
			ExpiresAt:      src.ExpiresAt,
			BaseURL:        src.BaseURL,
			Email:          src.Email,
			ModelID:        src.ModelID,
		}
		if err := store.Modify(src.ProviderID, func(_ *StoredCredential) (*StoredCredential, error) {
			return cred, nil
		}); err != nil {
			log.Printf("[credential-store] migration failed for %s: %v", src.ProviderID, err)
			continue
		}
		migrated++
		log.Printf("[credential-store] migrated %s credential from config", src.ProviderID)
	}
	return migrated
}
