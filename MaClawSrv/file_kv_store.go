package main

import (
	"context"
	"encoding/json"
	"os"
	"sync"
)

// fileKVStore implements corelib/skill.KVStore using a single JSON file.
// Thread-safe. Suitable for low-write-frequency settings like source control.
type fileKVStore struct {
	path string
	mu   sync.Mutex
	data map[string]string
}

func newFileKVStore(path string) *fileKVStore {
	s := &fileKVStore{path: path, data: make(map[string]string)}
	s.loadFromDisk()
	return s
}

func (s *fileKVStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *fileKVStore) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == "" {
		delete(s.data, key)
	} else {
		s.data[key] = value
	}
	return s.saveToDisk()
}

func (s *fileKVStore) loadFromDisk() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // file doesn't exist yet — start empty
	}
	_ = json.Unmarshal(data, &s.data)
}

func (s *fileKVStore) saveToDisk() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
