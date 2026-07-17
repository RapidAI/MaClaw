package scheduler

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DeliveryStateFileName is stored under the app data root.
const DeliveryStateFileName = "scheduled_delivery_state.json"

// DeliveryState remembers last successful delivery peer per channel so
// user_id=self works across GUI/TUI restarts without a live gateway session.
type DeliveryState struct {
	// LastPeer maps channel → platform id (chat_id / openid / staff id).
	LastPeer  map[string]string `json:"last_peer,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

// DeliveryStateStore is a small JSON-backed store with an in-memory cache.
type DeliveryStateStore struct {
	mu    sync.Mutex
	path  string
	cache *DeliveryState // nil until first load/save
}

// NewDeliveryStateStore creates a store at dataDir/scheduled_delivery_state.json.
func NewDeliveryStateStore(dataDir string) *DeliveryStateStore {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return &DeliveryStateStore{}
	}
	return &DeliveryStateStore{path: filepath.Join(dataDir, DeliveryStateFileName)}
}

// GetLastPeer returns the last successful peer id for channel.
func (s *DeliveryStateStore) GetLastPeer(channel string) string {
	if s == nil || s.path == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCacheLocked()
	if s.cache == nil || s.cache.LastPeer == nil {
		return ""
	}
	return strings.TrimSpace(s.cache.LastPeer[normalizeChannelKey(channel)])
}

// RememberPeer records a successful private-chat peer for channel so
// user_id=self can resolve after restart. Do not pass group chat ids
// (see CanRememberAsSelfPeer) — they would break private self resolution.
func (s *DeliveryStateStore) RememberPeer(channel, peerID string) {
	if s == nil || s.path == "" {
		return
	}
	channel = normalizeChannelKey(channel)
	peerID = strings.TrimSpace(peerID)
	if channel == "" || peerID == "" || IsSelfPeerID(peerID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCacheLocked()
	if s.cache.LastPeer == nil {
		s.cache.LastPeer = map[string]string{}
	}
	// Skip no-op writes.
	if s.cache.LastPeer[channel] == peerID {
		return
	}
	s.cache.LastPeer[channel] = peerID
	s.cache.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(*s.cache); err != nil {
		log.Printf("[scheduler] delivery state save failed path=%s: %v", s.path, err)
	}
}

// ResolveSelfPeer returns peerID if not self, else last remembered peer for channel.
// Safe on nil store (returns empty when peer is self).
func (s *DeliveryStateStore) ResolveSelfPeer(channel, peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if !IsSelfPeerID(peerID) {
		return peerID
	}
	if s == nil || s.path == "" {
		return ""
	}
	// Single lock (avoid GetLastPeer re-entry) for hot delivery paths.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCacheLocked()
	if s.cache == nil || s.cache.LastPeer == nil {
		return ""
	}
	return strings.TrimSpace(s.cache.LastPeer[normalizeChannelKey(channel)])
}

// ensureCacheLocked loads state from disk once. Caller must hold s.mu.
func (s *DeliveryStateStore) ensureCacheLocked() {
	if s.cache != nil {
		return
	}
	st, err := s.loadFromDisk()
	if err != nil {
		st = DeliveryState{LastPeer: map[string]string{}}
	}
	if st.LastPeer == nil {
		st.LastPeer = map[string]string{}
	}
	s.cache = &st
}

func (s *DeliveryStateStore) loadFromDisk() (DeliveryState, error) {
	if s.path == "" {
		return DeliveryState{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return DeliveryState{LastPeer: map[string]string{}}, nil
		}
		return DeliveryState{}, err
	}
	var st DeliveryState
	if err := json.Unmarshal(data, &st); err != nil {
		return DeliveryState{LastPeer: map[string]string{}}, nil
	}
	if st.LastPeer == nil {
		st.LastPeer = map[string]string{}
	}
	return st, nil
}

func (s *DeliveryStateStore) saveLocked(st DeliveryState) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func normalizeChannelKey(ch string) string {
	ch = strings.TrimSpace(strings.ToLower(ch))
	if ch == "" {
		return DeliveryChannelLansenger
	}
	return ch
}

// IsSelfPeerID reports whether id means "last known / owner session".
func IsSelfPeerID(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "", "self", "owner", "me":
		return true
	default:
		return false
	}
}
