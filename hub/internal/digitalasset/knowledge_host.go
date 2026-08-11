package digitalasset

import (
	"container/list"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// KnowledgeHost manages per-library knowledge.SQLiteStore handles with LRU eviction.
// Entries currently checked out via WithLibrary* are never closed (refcount).
type KnowledgeHost struct {
	root    string
	maxOpen int

	mu      sync.Mutex
	entries map[string]*hostEntry
	lru     *list.List // front = most recent
	locks   map[string]*sync.Mutex
}

type hostEntry struct {
	key   string
	store *knowledge.SQLiteStore
	elem  *list.Element
	refs  int // active WithLibrary* callers
}

// NewKnowledgeHost creates a host rooted at dataDir/digital_assets.
func NewKnowledgeHost(dataDir string, maxOpen int) *KnowledgeHost {
	if maxOpen <= 0 {
		maxOpen = 16
	}
	return &KnowledgeHost{
		root:    filepath.Join(dataDir, "digital_assets"),
		maxOpen: maxOpen,
		entries: make(map[string]*hostEntry),
		lru:     list.New(),
		locks:   make(map[string]*sync.Mutex),
	}
}

// LibraryDir returns {root}/{tenant}/{library}.
func (h *KnowledgeHost) LibraryDir(tenantID, libraryID string) string {
	return filepath.Join(h.root, tenantID, libraryID)
}

// DBPath returns knowledge.db path for a library.
func (h *KnowledgeHost) DBPath(tenantID, libraryID string) string {
	return filepath.Join(h.LibraryDir(tenantID, libraryID), "knowledge.db")
}

// PackagesDir returns packages directory for a library.
func (h *KnowledgeHost) PackagesDir(tenantID, libraryID string) string {
	return filepath.Join(h.LibraryDir(tenantID, libraryID), "packages")
}

// TmpDir returns temporary work directory for a job.
func (h *KnowledgeHost) TmpDir(tenantID, jobID string) string {
	return filepath.Join(h.root, "_tmp", tenantID, jobID)
}

// Root returns the digital_assets data root.
func (h *KnowledgeHost) Root() string {
	if h == nil {
		return ""
	}
	return h.root
}

func (h *KnowledgeHost) openKey(tenantID, libraryID string) string {
	return tenantID + "/" + libraryID
}

func (h *KnowledgeHost) lockFor(key string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, ok := h.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		h.locks[key] = lock
	}
	return lock
}

// WithLibraryWrite opens (or reuses) the library store, runs fn under exclusive write lock.
func (h *KnowledgeHost) WithLibraryWrite(ctx context.Context, tenantID, libraryID string, fn func(*knowledge.SQLiteStore) error) error {
	if h == nil {
		return fmt.Errorf("knowledge host is nil")
	}
	_ = ctx
	key := h.openKey(tenantID, libraryID)
	lock := h.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	store, err := h.acquire(tenantID, libraryID)
	if err != nil {
		return err
	}
	defer h.release(key)
	return fn(store)
}

// WithLibraryRead opens store for read (shares write lock v1 for simplicity).
func (h *KnowledgeHost) WithLibraryRead(ctx context.Context, tenantID, libraryID string, fn func(*knowledge.SQLiteStore) error) error {
	return h.WithLibraryWrite(ctx, tenantID, libraryID, fn)
}

// acquire increments refcount and returns an open store. Caller must release.
func (h *KnowledgeHost) acquire(tenantID, libraryID string) (*knowledge.SQLiteStore, error) {
	key := h.openKey(tenantID, libraryID)
	h.mu.Lock()
	defer h.mu.Unlock()

	if e, ok := h.entries[key]; ok {
		e.refs++
		h.lru.MoveToFront(e.elem)
		return e.store, nil
	}

	dbPath := h.DBPath(tenantID, libraryID)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir library: %w", err)
	}
	if err := os.MkdirAll(h.PackagesDir(tenantID, libraryID), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir packages: %w", err)
	}
	st, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open knowledge store: %w", err)
	}
	e := &hostEntry{key: key, store: st, refs: 1}
	e.elem = h.lru.PushFront(e)
	h.entries[key] = e
	h.evictIdleLocked()
	return st, nil
}

func (h *KnowledgeHost) release(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[key]
	if !ok {
		return
	}
	if e.refs > 0 {
		e.refs--
	}
	h.evictIdleLocked()
}

// evictIdleLocked closes least-recent idle entries (refs==0) until under maxOpen.
func (h *KnowledgeHost) evictIdleLocked() {
	for h.lru.Len() > h.maxOpen {
		var victim *list.Element
		for el := h.lru.Back(); el != nil; el = el.Prev() {
			e := el.Value.(*hostEntry)
			if e.refs == 0 {
				victim = el
				break
			}
		}
		if victim == nil {
			// All open stores are checked out; keep them.
			return
		}
		e := victim.Value.(*hostEntry)
		h.lru.Remove(victim)
		delete(h.entries, e.key)
		_ = e.store.Close()
	}
}

// Evict closes and removes a library handle (e.g. after delete).
// Safe only when no callers hold WithLibrary* for this library.
func (h *KnowledgeHost) Evict(tenantID, libraryID string) {
	if h == nil {
		return
	}
	key := h.openKey(tenantID, libraryID)
	// Take per-library lock so we don't close under an active writer.
	lock := h.lockFor(key)
	lock.Lock()
	defer lock.Unlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	if e, ok := h.entries[key]; ok {
		h.lru.Remove(e.elem)
		delete(h.entries, key)
		_ = e.store.Close()
	}
}

// CloseAll closes every open store. Blocks until per-library locks free.
func (h *KnowledgeHost) CloseAll() {
	if h == nil {
		return
	}
	h.mu.Lock()
	keys := make([]string, 0, len(h.entries))
	for k := range h.entries {
		keys = append(keys, k)
	}
	h.mu.Unlock()

	for _, key := range keys {
		lock := h.lockFor(key)
		lock.Lock()
		h.mu.Lock()
		if e, ok := h.entries[key]; ok {
			h.lru.Remove(e.elem)
			delete(h.entries, key)
			_ = e.store.Close()
		}
		h.mu.Unlock()
		lock.Unlock()
	}
}

// OpenCount returns currently cached store count (for tests).
func (h *KnowledgeHost) OpenCount() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lru.Len()
}
