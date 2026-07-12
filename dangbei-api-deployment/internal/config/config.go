package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Config is the persisted JSON shape for keys + accounts.
type Config struct {
	Keys            []string          `json:"keys"`
	Accounts        []Account         `json:"accounts"`
	Compat          Compat            `json:"compat,omitempty"`
	ClaudeMapping   map[string]string `json:"claude_mapping,omitempty"`
	ClaudeModelMap  map[string]string `json:"claude_model_map,omitempty"`
	EmbeddingsProv  string            `json:"embeddings_provider,omitempty"`
	ToolcallModeVal string            `json:"toolcall_mode,omitempty"`
	ToolcallEarly   string            `json:"toolcall_early_emit_confidence,omitempty"`
	ResponsesTTL    int               `json:"responses_store_ttl_seconds,omitempty"`
	VercelSyncHash  string            `json:"vercel_sync_hash,omitempty"`
	VercelSyncTime  int64             `json:"vercel_sync_time,omitempty"`
}

// Compat holds optional compatibility flags.
type Compat struct {
	WideInputStrictOutput *bool `json:"wide_input_strict_output,omitempty"`
}

// Account is a managed DeepSeek session token holder.
type Account struct {
	Name        string    `json:"name,omitempty"`
	Email       string    `json:"email,omitempty"`
	Mobile      string    `json:"mobile,omitempty"`
	Password    string    `json:"password,omitempty"`
	Token       string    `json:"token"`
	Status      string    `json:"status,omitempty"`
	LastUsed    time.Time `json:"lastUsed,omitempty"`
	LastChecked time.Time `json:"lastChecked,omitempty"`
	ErrorCount  int       `json:"errorCount,omitempty"`
}

// Identifier returns a stable lookup key for the account.
func (a Account) Identifier() string {
	if e := strings.TrimSpace(a.Email); e != "" {
		return e
	}
	if m := strings.TrimSpace(a.Mobile); m != "" {
		return m
	}
	if n := strings.TrimSpace(a.Name); n != "" {
		return n
	}
	tok := strings.TrimSpace(a.Token)
	if tok == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tok))
	return "token:" + hex.EncodeToString(sum[:8])
}

// Store is the in-memory config holder used by adapters/auth/admin.
type Store struct {
	mu            sync.RWMutex
	cfg           Config
	claudeMapping map[string]string
	envBacked     bool
	// aliases maps old identifiers to current tokens after token rotation.
	aliases map[string]string
}

func logKV(level, msg string, kv ...any) {
	parts := make([]string, 0, len(kv)/2+2)
	parts = append(parts, level, msg)
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf("%v=%v", kv[i], kv[i+1]))
	}
	log.Print(strings.Join(parts, " "))
}

// Logger is a tiny structured logger used across adapters/auth/webui.
var Logger = struct {
	Info  func(msg string, kv ...any)
	Warn  func(msg string, kv ...any)
	Error func(msg string, kv ...any)
}{
	Info:  func(msg string, kv ...any) { logKV("INFO", msg, kv...) },
	Warn:  func(msg string, kv ...any) { logKV("WARN", msg, kv...) },
	Error: func(msg string, kv ...any) { logKV("ERROR", msg, kv...) },
}

// StaticAdminDir returns the admin SPA static directory.
func StaticAdminDir() string {
	if v := strings.TrimSpace(os.Getenv("DS2API_STATIC_ADMIN_DIR")); v != "" {
		return v
	}
	return "webui/dist"
}

// IsVercel reports whether the process runs on Vercel.
func IsVercel() bool {
	return strings.TrimSpace(os.Getenv("VERCEL")) != "" || strings.TrimSpace(os.Getenv("VERCEL_ENV")) != ""
}

// WASMPath returns the optional WASM module path for DeepSeek PoW.
func WASMPath() string {
	if v := strings.TrimSpace(os.Getenv("DS2API_WASM_PATH")); v != "" {
		return v
	}
	return "sha3_wasm_bg.7b9ca65ddd.wasm"
}

var globalConfig *Config

func LoadConfig() (*Config, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	globalConfig = &cfg
	log.Printf("Loaded config: %d accounts", len(cfg.Accounts))
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile("config.json", data, 0644)
}

// LoadStore loads config from DS2API_CONFIG_JSON when set, otherwise starts empty.
func LoadStore() *Store {
	s := &Store{
		claudeMapping: map[string]string{
			"fast": "deepseek-chat",
			"slow": "deepseek-reasoner",
		},
		aliases: map[string]string{},
	}
	raw := strings.TrimSpace(os.Getenv("DS2API_CONFIG_JSON"))
	if raw == "" {
		return s
	}
	s.envBacked = true
	raw = strings.Trim(raw, `"'`)
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		log.Printf("LoadStore: invalid DS2API_CONFIG_JSON: %v", err)
		return s
	}
	// Soft-fail invalid types: empty keys when not an array is already handled by unmarshal error above.
	// Accept nested embeddings.provider from historical config JSON.
	var rawMap map[string]any
	if err := json.Unmarshal([]byte(raw), &rawMap); err == nil {
		if emb, ok := rawMap["embeddings"].(map[string]any); ok {
			if p, ok := emb["provider"].(string); ok && strings.TrimSpace(cfg.EmbeddingsProv) == "" {
				cfg.EmbeddingsProv = strings.TrimSpace(p)
			}
		}
		if m, ok := rawMap["claude_mapping"].(map[string]any); ok && len(cfg.ClaudeMapping) == 0 {
			cfg.ClaudeMapping = map[string]string{}
			for k, v := range m {
				cfg.ClaudeMapping[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	s.cfg = cfg
	if len(cfg.ClaudeMapping) > 0 {
		s.claudeMapping = map[string]string{}
		for k, v := range cfg.ClaudeMapping {
			s.claudeMapping[k] = v
		}
	}
	return s
}

func (s *Store) Snapshot() Config {
	if s == nil {
		return Config{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func (s *Store) Keys() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.cfg.Keys...)
}

func (s *Store) Accounts() []Account {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Account(nil), s.cfg.Accounts...)
}

func (s *Store) HasAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	if s == nil || key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.cfg.Keys {
		if strings.TrimSpace(k) == key {
			return true
		}
	}
	return false
}

func (s *Store) FindAccount(identifier string) (Account, bool) {
	id := strings.TrimSpace(identifier)
	if s == nil || id == "" {
		return Account{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if tok, ok := s.aliases[id]; ok {
		for _, acc := range s.cfg.Accounts {
			if acc.Token == tok {
				return acc, true
			}
		}
	}
	for _, acc := range s.cfg.Accounts {
		if acc.Identifier() == id || strings.TrimSpace(acc.Email) == id || strings.TrimSpace(acc.Mobile) == id || strings.TrimSpace(acc.Name) == id || strings.TrimSpace(acc.Token) == id {
			return acc, true
		}
	}
	return Account{}, false
}

func (s *Store) UpdateAccountToken(identifier, token string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	id := strings.TrimSpace(identifier)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Accounts {
		acc := s.cfg.Accounts[i]
		if acc.Identifier() == id || strings.TrimSpace(acc.Email) == id || strings.TrimSpace(acc.Name) == id || strings.TrimSpace(acc.Token) == id {
			oldID := acc.Identifier()
			s.cfg.Accounts[i].Token = token
			if oldID != "" && token != "" {
				s.aliases[oldID] = token
			}
			return nil
		}
	}
	return fmt.Errorf("account not found: %s", id)
}

func (s *Store) ClaudeMapping() map[string]string {
	if s == nil {
		return map[string]string{"fast": "deepseek-chat", "slow": "deepseek-reasoner"}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.claudeMapping))
	for k, v := range s.claudeMapping {
		out[k] = v
	}
	return out
}

func (s *Store) CompatWideInputStrictOutput() bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Compat.WideInputStrictOutput == nil {
		return true
	}
	return *s.cfg.Compat.WideInputStrictOutput
}

func (s *Store) IsEnvBacked() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.envBacked
}

func (s *Store) Replace(cfg Config) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cloneConfig(cfg)
	s.aliases = map[string]string{}
}

// Update mutates config under lock via a callback.
func (s *Store) Update(fn func(c *Config) error) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if fn == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := cloneConfig(s.cfg)
	if err := fn(&cfg); err != nil {
		return err
	}
	s.cfg = cloneConfig(cfg)
	// Keep Claude mapping cache in sync with config.
	if len(cfg.ClaudeMapping) > 0 {
		s.claudeMapping = map[string]string{}
		for k, v := range cfg.ClaudeMapping {
			s.claudeMapping[k] = v
		}
	}
	return nil
}

func (s *Store) EmbeddingsProvider() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.EmbeddingsProv)
}

func (s *Store) ToolcallMode() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.ToolcallModeVal)
}

func (s *Store) ToolcallEarlyEmitConfidence() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.ToolcallEarly)
}

func (s *Store) ResponsesStoreTTLSeconds() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.ResponsesTTL
}

func (s *Store) SetVercelSync(hash string, ts int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.VercelSyncHash = strings.TrimSpace(hash)
	s.cfg.VercelSyncTime = ts
}

func (s *Store) ExportJSONAndBase64() (jsonText string, b64 string, err error) {
	if s == nil {
		return "", "", fmt.Errorf("store is nil")
	}
	snap := s.Snapshot()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", "", err
	}
	return string(data), encodeBase64(data), nil
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// ConfigPtr returns a snapshot pointer for APIs that historically took *Config.
func (s *Store) ConfigPtr() *Config {
	if s == nil {
		return &Config{}
	}
	snap := s.Snapshot()
	return &snap
}

func cloneConfig(cfg Config) Config {
	out := Config{
		Keys:            append([]string(nil), cfg.Keys...),
		Accounts:        append([]Account(nil), cfg.Accounts...),
		Compat:          cfg.Compat,
		EmbeddingsProv:  cfg.EmbeddingsProv,
		ToolcallModeVal: cfg.ToolcallModeVal,
		ToolcallEarly:   cfg.ToolcallEarly,
		ResponsesTTL:    cfg.ResponsesTTL,
		VercelSyncHash:  cfg.VercelSyncHash,
		VercelSyncTime:  cfg.VercelSyncTime,
	}
	if cfg.Compat.WideInputStrictOutput != nil {
		v := *cfg.Compat.WideInputStrictOutput
		out.Compat.WideInputStrictOutput = &v
	}
	if len(cfg.ClaudeMapping) > 0 {
		out.ClaudeMapping = make(map[string]string, len(cfg.ClaudeMapping))
		for k, v := range cfg.ClaudeMapping {
			out.ClaudeMapping[k] = v
		}
	}
	if len(cfg.ClaudeModelMap) > 0 {
		out.ClaudeModelMap = make(map[string]string, len(cfg.ClaudeModelMap))
		for k, v := range cfg.ClaudeModelMap {
			out.ClaudeModelMap[k] = v
		}
	}
	return out
}

func (c *Config) GetNextToken() string {
	if len(c.Accounts) == 0 {
		return ""
	}
	for i := range c.Accounts {
		if c.Accounts[i].Status == "active" {
			return c.Accounts[i].Token
		}
	}
	return c.Accounts[0].Token
}

func (c *Config) MarkTokenFailed(token string) {}
func (c *Config) MarkTokenActive(token string) {}

func (c *Config) AddAccount(name, cookieString string) error {
	token := extractTokenFromCookie(cookieString)
	if token == "" {
		return fmt.Errorf("无法从 cookie 中提取 token")
	}
	for _, acc := range c.Accounts {
		if acc.Token == token {
			return fmt.Errorf("该账号已存在")
		}
	}
	c.Accounts = append(c.Accounts, Account{
		Name:   name,
		Token:  token,
		Status: "active",
	})
	return SaveConfig(c)
}

func (c *Config) RemoveAccount(token string) error {
	for i, acc := range c.Accounts {
		if acc.Token == token {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return SaveConfig(c)
		}
	}
	return fmt.Errorf("账号不存在")
}

func (c *Config) GetAccounts() []Account {
	return c.Accounts
}

func (c *Config) GetAccountStats() map[string]interface{} {
	active, failed, unknown := 0, 0, 0
	for _, acc := range c.Accounts {
		switch acc.Status {
		case "active":
			active++
		case "failed":
			failed++
		default:
			unknown++
		}
	}
	return map[string]interface{}{
		"total":   len(c.Accounts),
		"active":  active,
		"failed":  failed,
		"unknown": unknown,
	}
}

func extractTokenFromCookie(cookieString string) string {
	re := regexp.MustCompile(`token=([a-f0-9]+)`)
	matches := re.FindStringSubmatch(cookieString)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}
