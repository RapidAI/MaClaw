package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const resultStoreKeyFile = ".results.key"

// SetResultStoreDir enables encrypted on-disk persistence for query cursors
// so a result_handle survives process restart within TTL. Empty disables it.
func (m *Manager) SetResultStoreDir(dir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		m.resultDir = ""
		return
	}
	m.resultDir = filepath.Clean(dir)
}

func (m *Manager) persistStoredResult(id string, stored storedResult) {
	m.mu.Lock()
	dir := m.resultDir
	m.mu.Unlock()
	if dir == "" || id == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	key, err := resultStoreKey(dir)
	if err != nil {
		return
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return
	}
	sealed, err := sealResultBytes(key, payload)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, id+".enc.tmp")
	final := filepath.Join(dir, id+".enc")
	if err := os.WriteFile(tmp, sealed, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
}

func (m *Manager) deletePersistedResultLocked(id string) {
	if m == nil || m.resultDir == "" || id == "" {
		return
	}
	_ = os.Remove(filepath.Join(m.resultDir, id+".enc"))
	_ = os.Remove(filepath.Join(m.resultDir, id+".enc.tmp"))
}

func (m *Manager) loadStoredResult(id string) (storedResult, bool) {
	m.mu.Lock()
	dir := m.resultDir
	m.mu.Unlock()
	if dir == "" || id == "" {
		return storedResult{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".enc"))
	if err != nil {
		return storedResult{}, false
	}
	key, err := resultStoreKey(dir)
	if err != nil {
		return storedResult{}, false
	}
	plain, err := openResultBytes(key, data)
	if err != nil {
		return storedResult{}, false
	}
	var stored storedResult
	if err := json.Unmarshal(plain, &stored); err != nil {
		return storedResult{}, false
	}
	if !stored.Expires.After(time.Now()) {
		_ = os.Remove(filepath.Join(dir, id+".enc"))
		return storedResult{}, false
	}
	return stored, true
}

func resultStoreKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, resultStoreKeyFile)
	if data, err := os.ReadFile(path); err == nil {
		if len(data) == 32 {
			return data, nil
		}
		return nil, fmt.Errorf("invalid result store key")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func sealResultBytes(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func openResultBytes(key, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(sealed) < ns {
		return nil, fmt.Errorf("truncated result blob")
	}
	return gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
}
