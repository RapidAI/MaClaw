package desktopd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// proxytoken.go backs the forward-proxy key with a state file so the admin
// panel can view and replace it, the same way api_tokens.json backs the Hub
// keys. The panel file wins over the process env fallback
// (DESKTOPD_PROXY_TOKEN, else DESKTOPD_TOKEN): an operator replaces the key
// in the panel and the change takes effect on the NEXT request — no restart,
// and no .env surgery. The file is written through the same tmp+rename
// discipline as the other state files.

const proxyTokenFileName = "proxy_token.json"

// ProxyKeySource resolves the forward-proxy key for one desktopd process.
type ProxyKeySource struct {
	// path is the panel file; "" means panel management is disabled (no state
	// directory) and the fallback is the only key.
	path     string
	fallback string

	cacheMu   sync.Mutex
	cacheSize int64
	cacheMod  time.Time
	cacheKey  string
}

// NewProxyKeySource builds the key source. stateDir may be empty, which keeps
// the env-only behaviour of deployments without the admin panel.
func NewProxyKeySource(stateDir, fallback string) *ProxyKeySource {
	k := &ProxyKeySource{fallback: fallback}
	if stateDir != "" {
		k.path = filepath.Join(stateDir, proxyTokenFileName)
	}
	return k
}

// Get returns the effective key and whether it came from the panel file.
// An unreadable or malformed file is an error, not a silent fallback: pulls
// failing with 407 while the panel shows an error beats a proxy that
// silently flipped back to the old key.
func (k *ProxyKeySource) Get() (token string, fromPanel bool, err error) {
	if k.path == "" {
		return k.fallback, false, nil
	}
	info, err := os.Stat(k.path)
	if err != nil {
		if os.IsNotExist(err) {
			return k.fallback, false, nil
		}
		return "", false, err
	}
	k.cacheMu.Lock()
	if k.cacheKey != "" && k.cacheSize == info.Size() && k.cacheMod.Equal(info.ModTime()) {
		key := k.cacheKey
		k.cacheMu.Unlock()
		return key, true, nil
	}
	k.cacheMu.Unlock()
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return "", false, err
	}
	var file struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", false, fmt.Errorf("%s is malformed: %w", proxyTokenFileName, err)
	}
	if !validTokenCharset(file.Token) {
		return "", false, fmt.Errorf("%s holds an invalid token", proxyTokenFileName)
	}
	k.cacheMu.Lock()
	k.cacheKey = file.Token
	k.cacheSize = info.Size()
	k.cacheMod = info.ModTime()
	k.cacheMu.Unlock()
	return file.Token, true, nil
}

// Set validates and stores the panel-chosen key. It takes effect on the next
// proxied connection.
func (k *ProxyKeySource) Set(token string) error {
	if k.path == "" {
		return fmt.Errorf("proxy key management is disabled (no state directory)")
	}
	if !validTokenCharset(token) {
		return fmt.Errorf("token must be printable ASCII without whitespace")
	}
	raw, err := json.MarshalIndent(map[string]string{
		"token":      token,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeStateFile(filepath.Dir(k.path), proxyTokenFileName, raw); err != nil {
		return err
	}
	// Keep the cache exact rather than invalidating it: coarse filesystems
	// may report the old mtime for a moment.
	info, err := os.Stat(k.path)
	if err != nil {
		return err
	}
	k.cacheMu.Lock()
	k.cacheKey = token
	k.cacheSize = info.Size()
	k.cacheMod = info.ModTime()
	k.cacheMu.Unlock()
	return nil
}

// Clear removes the panel-set key, restoring the env fallback.
func (k *ProxyKeySource) Clear() error {
	if k.path == "" {
		return fmt.Errorf("proxy key management is disabled (no state directory)")
	}
	if err := os.Remove(k.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	k.cacheMu.Lock()
	k.cacheKey = ""
	k.cacheSize = 0
	k.cacheMod = time.Time{}
	k.cacheMu.Unlock()
	return nil
}
