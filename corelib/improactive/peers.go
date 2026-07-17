// Package improactive persists last-known IM peers for owner proactive delivery
// (盯人 forward, scheduled tasks). Survives Maclaw process restarts so channels
// like Lansenger/Telegram/QQ do not require re-chatting the bot every launch.
package improactive

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

const (
	peersVersion  = 1
	peersFileName = "peers.json"
	peersDirName  = "im_proactive"
)

// Peers holds last-known owner peer IDs per IM channel.
type Peers struct {
	Version                 int       `json:"version"`
	LansengerPrivateUserID  string    `json:"lansenger_private_user_id,omitempty"`
	TelegramLastChatID      int64     `json:"telegram_last_chat_id,omitempty"`
	QQLastOpenID            string    `json:"qq_last_open_id,omitempty"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
}

// Store reads/writes peers under <maclaw data>/im_proactive/peers.json.
type Store struct {
	mu   sync.Mutex
	path string
}

// DefaultPath returns the standard peers.json path.
func DefaultPath() string {
	return filepath.Join(maclawpath.DataDir(), peersDirName, peersFileName)
}

// NewStore creates a store at the default path (or overridePath if non-empty).
func NewStore(overridePath string) *Store {
	path := strings.TrimSpace(overridePath)
	if path == "" {
		path = DefaultPath()
	}
	return &Store{path: path}
}

// Path returns the file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads peers; missing file yields empty Peers (not an error).
func (s *Store) Load() (Peers, error) {
	if s == nil {
		return Peers{}, fmt.Errorf("improactive: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Peers{Version: peersVersion}, nil
		}
		return Peers{}, err
	}
	var p Peers
	if err := json.Unmarshal(data, &p); err != nil {
		return Peers{}, fmt.Errorf("improactive: parse peers: %w", err)
	}
	if p.Version == 0 {
		p.Version = peersVersion
	}
	return p, nil
}

// Save writes peers atomically.
func (s *Store) Save(p Peers) error {
	if s == nil {
		return fmt.Errorf("improactive: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Version = peersVersion
	p.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writePeersFile(s.path, data)
}

// LoadOrEmpty loads peers, logging and returning empty on error.
func (s *Store) LoadOrEmpty() Peers {
	p, err := s.Load()
	if err != nil {
		log.Printf("[improactive] load peers: %v", err)
		return Peers{Version: peersVersion}
	}
	return p
}

// Patch loads, applies fn, and saves. Safe for concurrent callers.
func (s *Store) Patch(fn func(*Peers)) error {
	if s == nil {
		return fmt.Errorf("improactive: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := Peers{Version: peersVersion}
	if data, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("improactive: parse peers: %w", err)
		}
		if p.Version == 0 {
			p.Version = peersVersion
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	before := p
	fn(&p)
	// Skip disk I/O when the patch was a no-op (common for same-peer re-notes).
	if peersEqual(before, p) {
		return nil
	}
	p.Version = peersVersion
	p.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writePeersFile(s.path, data)
}

func peersEqual(a, b Peers) bool {
	return strings.TrimSpace(a.LansengerPrivateUserID) == strings.TrimSpace(b.LansengerPrivateUserID) &&
		a.TelegramLastChatID == b.TelegramLastChatID &&
		strings.TrimSpace(a.QQLastOpenID) == strings.TrimSpace(b.QQLastOpenID)
}

func writePeersFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// Peer IDs are moderately sensitive → owner-only.
	return fileutil.AtomicWriteFile(path, data, 0o600)
}
