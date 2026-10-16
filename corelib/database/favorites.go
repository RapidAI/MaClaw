package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxFavoritesPerOwner = 50

// FavoriteStore persists owner-bound query favorites. The file is rewritten
// atomically at 0600 and never stores credentials.
type FavoriteStore struct {
	mu    sync.Mutex
	path  string
	items map[string]QueryFavorite
}

func NewFavoriteStore(path string) (*FavoriteStore, error) {
	store := &FavoriteStore{items: make(map[string]QueryFavorite)}
	path = strings.TrimSpace(path)
	if path == "" {
		return store, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	store.path = abs
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FavoriteStore) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var items []QueryFavorite
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("favorites store is unreadable: %w", err)
	}
	s.items = make(map[string]QueryFavorite, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			s.items[id] = item
		}
	}
	return nil
}

func (s *FavoriteStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	items := make([]QueryFavorite, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FavoriteStore) List(ownerID string) []QueryFavorite {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QueryFavorite, 0)
	for _, item := range s.items {
		if ownerID == "" || item.OwnerID == "" || item.OwnerID == ownerID {
			out = append(out, item)
		}
	}
	return out
}

func (s *FavoriteStore) Get(id, ownerID string) (QueryFavorite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return QueryFavorite{}, fmt.Errorf("permission: favorite not found")
	}
	if item.OwnerID != "" && ownerID != "" && item.OwnerID != ownerID {
		return QueryFavorite{}, fmt.Errorf("permission: favorite belongs to another owner")
	}
	return item, nil
}

func (s *FavoriteStore) Save(fav QueryFavorite) (QueryFavorite, error) {
	name := strings.TrimSpace(fav.Name)
	sqlText := strings.TrimSpace(fav.SQL)
	if name == "" || sqlText == "" {
		return QueryFavorite{}, fmt.Errorf("syntax: name and sql are required")
	}
	if err := validateReadSQL(sqlText); err != nil {
		return QueryFavorite{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(fav.ID) == "" {
		count := 0
		for _, item := range s.items {
			if item.OwnerID == fav.OwnerID {
				count++
			}
		}
		if count >= maxFavoritesPerOwner {
			return QueryFavorite{}, fmt.Errorf("quota_exceeded: at most %d favorites per owner", maxFavoritesPerOwner)
		}
		fav.ID = "db-fav-" + randomID()
		if fav.CreatedAt.IsZero() {
			fav.CreatedAt = time.Now().UTC()
		}
	} else if existing, ok := s.items[fav.ID]; ok {
		if existing.OwnerID != "" && fav.OwnerID != "" && existing.OwnerID != fav.OwnerID {
			return QueryFavorite{}, fmt.Errorf("permission: favorite belongs to another owner")
		}
		if fav.CreatedAt.IsZero() {
			fav.CreatedAt = existing.CreatedAt
		}
	}
	fav.Name = name
	fav.SQL = sqlText
	s.items[fav.ID] = fav
	if err := s.persistLocked(); err != nil {
		return QueryFavorite{}, err
	}
	return fav, nil
}

func (s *FavoriteStore) Delete(id, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(id)]
	if !ok {
		return fmt.Errorf("permission: favorite not found")
	}
	if item.OwnerID != "" && ownerID != "" && item.OwnerID != ownerID {
		return fmt.Errorf("permission: favorite belongs to another owner")
	}
	delete(s.items, item.ID)
	return s.persistLocked()
}

func (m *Manager) SetFavoriteStore(store *FavoriteStore) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.favorites = store
	}
	m.mu.Unlock()
}

func (m *Manager) favoriteStore() *FavoriteStore {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if m.favorites == nil {
		m.favorites = &FavoriteStore{items: make(map[string]QueryFavorite)}
	}
	return m.favorites
}
