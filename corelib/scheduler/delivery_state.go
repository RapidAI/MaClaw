package scheduler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

// errDeliveryLegacyUnread means a readable pre-migration file is still on disk
// and this load did not copy it. The empty snapshot must not become the cache.
var errDeliveryLegacyUnread = errors.New("scheduler: delivery legacy file is still unread")

// DeliveryStateFileName is stored under the app data root.
const DeliveryStateFileName = "scheduled_delivery_state.json"

// DeliveryState remembers last successful delivery peer per channel so
// user_id=self works across GUI/TUI restarts without a live gateway session.
type DeliveryState struct {
	// LastPeer maps channel → platform id (chat_id / openid / staff id).
	LastPeer  map[string]string `json:"last_peer,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

// DeliveryStateStore keeps last peers in SQLite, with an in-memory cache.
// scheduled_delivery_state.json is only the pre-migration file.
type DeliveryStateStore struct {
	mu        sync.Mutex
	path      string
	cache     *DeliveryState // nil until a load that finished the legacy file
	loadNoted bool
}

// NewDeliveryStateStore creates a store at dataDir/scheduled_delivery_state.db.
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
	if !s.ensureCacheLocked() || s.cache.LastPeer == nil {
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
	if !s.ensureCacheLocked() {
		return
	}
	if s.cache.LastPeer == nil {
		s.cache.LastPeer = map[string]string{}
	}
	// Skip no-op writes.
	if s.cache.LastPeer[channel] == peerID {
		return
	}
	prevID, hadPrev := s.cache.LastPeer[channel]
	prevUpdated := s.cache.UpdatedAt
	s.cache.LastPeer[channel] = peerID
	s.cache.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(*s.cache); err != nil {
		if hadPrev {
			s.cache.LastPeer[channel] = prevID
		} else {
			delete(s.cache.LastPeer, channel)
		}
		s.cache.UpdatedAt = prevUpdated
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
	if !s.ensureCacheLocked() || s.cache.LastPeer == nil {
		return ""
	}
	return strings.TrimSpace(s.cache.LastPeer[normalizeChannelKey(channel)])
}

// ensureCacheLocked loads state from disk once. Caller must hold s.mu.
// A failed load stays uncached so the next call can import the legacy file.
func (s *DeliveryStateStore) ensureCacheLocked() bool {
	if s.cache != nil {
		return true
	}
	st, err := s.loadFromDisk()
	if err != nil {
		if !s.loadNoted {
			s.loadNoted = true
			log.Printf("[scheduler] delivery state not loaded path=%s: %v", s.path, err)
		}
		return false
	}
	s.loadNoted = false
	if st.LastPeer == nil {
		st.LastPeer = map[string]string{}
	}
	s.cache = &st
	return true
}

func deliveryStateDBPath(jsonPath string) string {
	if strings.HasSuffix(strings.ToLower(jsonPath), ".json") {
		return strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".db"
	}
	return jsonPath + ".db"
}

func (s *DeliveryStateStore) loadFromDisk() (DeliveryState, error) {
	if s.path == "" {
		return DeliveryState{}, nil
	}
	db, err := openDeliveryStateDB(deliveryStateDBPath(s.path))
	if err != nil {
		return DeliveryState{}, err
	}
	defer db.Close()
	if err := importDeliveryStateJSON(db, s.path); err != nil {
		return DeliveryState{}, err
	}
	st, err := readDeliveryState(db)
	if err != nil {
		return DeliveryState{}, err
	}
	if deliveryLegacyPending(s.path) {
		var marker string
		mErr := db.QueryRow(`SELECT value FROM delivery_meta WHERE key = 'legacy_import'`).Scan(&marker)
		if mErr != nil || marker != "1" {
			return DeliveryState{}, errDeliveryLegacyUnread
		}
	}
	return st, nil
}

func (s *DeliveryStateStore) saveLocked(st DeliveryState) error {
	if s.path == "" {
		return nil
	}
	db, err := openDeliveryStateDB(deliveryStateDBPath(s.path))
	if err != nil {
		return err
	}
	defer db.Close()
	seal := deliveryLegacySeal(db, s.path)
	if err := writeDeliveryState(db, st, seal); err != nil {
		return err
	}
	if seal {
		_ = os.Remove(s.path)
		_ = os.Remove(s.path + ".tmp")
	}
	return nil
}

// deliveryLegacySeal reports whether this save may record the import and
// remove the pre-migration file. A readable file that has not been copied
// stays, so the next open can still import it.
func deliveryLegacySeal(db *sql.DB, jsonPath string) bool {
	var marker string
	err := db.QueryRow(`SELECT value FROM delivery_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err == nil && marker == "1" {
		return true
	}
	return !deliveryLegacyPending(jsonPath)
}

func deliveryLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalDeliveryState(data)
		return err == nil
	})
}

func unmarshalDeliveryState(data []byte) (DeliveryState, error) {
	var st DeliveryState
	err := json.Unmarshal(data, &st)
	return st, err
}

func openDeliveryStateDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS delivery_peers (
  channel TEXT PRIMARY KEY,
  peer_id TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS delivery_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func importDeliveryStateJSON(db *sql.DB, jsonPath string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM delivery_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, err = db.Exec(`INSERT INTO delivery_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
			return err
		}
		return nil
	}
	st, err := unmarshalDeliveryState(data)
	if err != nil {
		return nil
	}
	if err := writeDeliveryState(db, st, true); err != nil {
		return err
	}
	_ = os.Remove(jsonPath)
	_ = os.Remove(jsonPath + ".tmp")
	return nil
}

func writeDeliveryState(db *sql.DB, st DeliveryState, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM delivery_peers`); err != nil {
		_ = tx.Rollback()
		return err
	}
	stamp := st.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if st.UpdatedAt.IsZero() {
		stamp = ""
	}
	stmt, err := tx.Prepare(`INSERT INTO delivery_peers(channel, peer_id, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(channel) DO UPDATE SET peer_id = excluded.peer_id, updated_at = excluded.updated_at`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for channel, peer := range st.LastPeer {
		channel = strings.TrimSpace(channel)
		peer = strings.TrimSpace(peer)
		if channel == "" || peer == "" {
			continue
		}
		channel = normalizeChannelKey(channel)
		if _, err := stmt.Exec(channel, peer, stamp); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if mark {
		if _, err := tx.Exec(`INSERT INTO delivery_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readDeliveryState(db *sql.DB) (DeliveryState, error) {
	rows, err := db.Query(`SELECT channel, peer_id, updated_at FROM delivery_peers`)
	if err != nil {
		return DeliveryState{}, err
	}
	defer rows.Close()
	st := DeliveryState{LastPeer: map[string]string{}}
	for rows.Next() {
		var channel, peer, updated string
		if err := rows.Scan(&channel, &peer, &updated); err != nil {
			return DeliveryState{}, err
		}
		channel = strings.TrimSpace(channel)
		peer = strings.TrimSpace(peer)
		if channel == "" || peer == "" {
			continue
		}
		st.LastPeer[normalizeChannelKey(channel)] = peer
		if parsed := parseTaskTime(updated); parsed.After(st.UpdatedAt) {
			st.UpdatedAt = parsed
		}
	}
	return st, rows.Err()
}

func normalizeChannelKey(ch string) string {
	// Same canonical keys as delivery/send/catalog (蓝信 → lansenger).
	return DefaultDeliveryChannel(ch)
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
