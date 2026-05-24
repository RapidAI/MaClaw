package memory

// StorageBackend abstracts the persistence layer for memory entries.
// The Store uses this interface for all disk I/O, allowing different
// implementations (JSON files for GUI/TUI, SQLite for maclawsrv multi-instance).
//
// Thread safety: The Store serializes all calls to the backend via its own
// mutex. Backend implementations do NOT need to be safe for concurrent use
// from multiple goroutines (but SQLite WAL handles multi-process concurrency).
type StorageBackend interface {
	// LoadAll returns all non-deleted entries. Called once at Store startup
	// to populate the in-memory index. Entries should be returned in no
	// particular order; the Store will sort/index them as needed.
	LoadAll() ([]Entry, error)

	// SaveEntry persists a new entry. The backend assigns entry.Version
	// (monotonically increasing) before returning. The caller has already
	// set all other fields (ID, Content, Category, Tags, etc.).
	SaveEntry(entry *Entry) error

	// UpdateEntry persists changes to an existing entry (matched by ID).
	// The backend increments the entry's Version so other instances can
	// detect the change via Since().
	UpdateEntry(entry *Entry) error

	// DeleteEntry soft-deletes an entry by ID. The backend records the
	// deletion with a new Version so other instances can detect it via Since().
	// Returns nil if the entry does not exist (idempotent).
	DeleteEntry(id string) error

	// Since returns entries modified (created or updated) after the given
	// version, plus IDs of entries soft-deleted after that version.
	// Used by the sync loop for incremental cross-instance synchronization.
	// Results are ordered by version ascending.
	// Returns (nil, nil, nil) if the backend does not support sync (e.g. JSON files).
	Since(version int64) (modified []Entry, deletedIDs []string, err error)

	// MaxVersion returns the current maximum version number across all entries.
	// Returns 0 if the store is empty or the backend does not support versioning.
	MaxVersion() (int64, error)

	// SupportsSync returns true if this backend supports cross-instance
	// synchronization via Since()/MaxVersion(). JSON backends return false;
	// SQLite backends return true.
	SupportsSync() bool

	// Close releases resources (DB connections, file handles).
	// Called when Store.Stop() is invoked.
	Close() error
}

// BatchStorageBackend is an optional extension for backends that can persist
// several existing entries atomically. Store falls back to its in-memory JSON
// path when this extension is unavailable.
type BatchStorageBackend interface {
	UpdateEntries(entries []*Entry) error
}

// BatchMutationStorageBackend is an optional extension for backends that can
// persist entry updates and soft deletes in one transaction.
type BatchMutationStorageBackend interface {
	UpdateEntriesAndDeleteIDs(entries []*Entry, deleteIDs []string) error
}
