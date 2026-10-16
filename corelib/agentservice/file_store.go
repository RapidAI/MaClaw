package agentservice

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// FileStore keeps the service control-plane state durable while reusing the
// in-process control-plane Store implementation for query semantics and sorting.
// It is intentionally separate from corelib/memory.Store, which owns long-term
// user/agent memory.
type FileStore struct {
	mu    sync.Mutex
	path  string
	inner *MemoryStore
}

func NewFileStore(path string) (*FileStore, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	inner := NewMemoryStore()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if len(data) > 0 {
		var state storeState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
		inner = newMemoryStoreFromState(state)
	}
	return &FileStore{path: path, inner: inner}, nil
}

func (s *FileStore) SaveTenant(v Tenant) error {
	return s.mutate(func() error { return s.inner.SaveTenant(v) })
}

func (s *FileStore) GetTenant(id string) (Tenant, error) {
	return s.inner.GetTenant(id)
}

func (s *FileStore) ListTenants() ([]Tenant, error) {
	return s.inner.ListTenants()
}

func (s *FileStore) DeleteTenant(tenantID string) error {
	return s.mutate(func() error { return s.inner.DeleteTenant(tenantID) })
}

func (s *FileStore) SaveUser(v User) error {
	return s.mutate(func() error { return s.inner.SaveUser(v) })
}

func (s *FileStore) GetUser(tenantID, userID string) (User, error) {
	return s.inner.GetUser(tenantID, userID)
}

func (s *FileStore) ListUsers(tenantID string) ([]User, error) {
	return s.inner.ListUsers(tenantID)
}

func (s *FileStore) DeleteUser(tenantID, userID string) error {
	return s.mutate(func() error { return s.inner.DeleteUser(tenantID, userID) })
}

func (s *FileStore) SaveCredential(v Credential) error {
	return s.mutate(func() error { return s.inner.SaveCredential(v) })
}

func (s *FileStore) GetCredential(tenantID, userID, credentialID string) (Credential, error) {
	return s.inner.GetCredential(tenantID, userID, credentialID)
}

func (s *FileStore) ListCredentials(tenantID, userID string) ([]Credential, error) {
	return s.inner.ListCredentials(tenantID, userID)
}

func (s *FileStore) GetCredentialByAPIKey(apiKey string) (Credential, error) {
	return s.inner.GetCredentialByAPIKey(apiKey)
}

func (s *FileStore) SaveUserConfig(v UserConfig) error {
	return s.mutate(func() error { return s.inner.SaveUserConfig(v) })
}

func (s *FileStore) GetUserConfig(tenantID, userID string) (UserConfig, error) {
	return s.inner.GetUserConfig(tenantID, userID)
}

func (s *FileStore) SaveInstance(v Instance) error {
	return s.mutate(func() error { return s.inner.SaveInstance(v) })
}

func (s *FileStore) GetInstance(tenantID, userID, instanceID string) (Instance, error) {
	return s.inner.GetInstance(tenantID, userID, instanceID)
}

func (s *FileStore) ListInstances(tenantID, userID string) ([]Instance, error) {
	return s.inner.ListInstances(tenantID, userID)
}

func (s *FileStore) DeleteInstance(tenantID, userID, instanceID string) error {
	return s.mutate(func() error { return s.inner.DeleteInstance(tenantID, userID, instanceID) })
}

func (s *FileStore) SaveSession(v Session) error {
	return s.mutate(func() error { return s.inner.SaveSession(v) })
}

func (s *FileStore) GetSession(tenantID, userID, instanceID, sessionID string) (Session, error) {
	return s.inner.GetSession(tenantID, userID, instanceID, sessionID)
}

func (s *FileStore) ListSessions(tenantID, userID, instanceID string) ([]Session, error) {
	return s.inner.ListSessions(tenantID, userID, instanceID)
}

func (s *FileStore) DeleteSession(tenantID, userID, instanceID, sessionID string) error {
	return s.mutate(func() error { return s.inner.DeleteSession(tenantID, userID, instanceID, sessionID) })
}

func (s *FileStore) SaveMessage(v Message) error {
	return s.mutate(func() error { return s.inner.SaveMessage(v) })
}

func (s *FileStore) ListMessages(sessionID string) ([]Message, error) {
	return s.inner.ListMessages(sessionID)
}

func (s *FileStore) DeleteMessages(sessionID string, ids []string) (int, error) {
	if s == nil || s.inner == nil {
		return 0, ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.inner.snapshot()
	deleted, err := s.inner.DeleteMessages(sessionID, ids)
	if err != nil || deleted == 0 {
		return deleted, err
	}
	if err := s.flushLocked(); err != nil {
		s.inner.restore(before)
		return 0, err
	}
	return deleted, nil
}

func (s *FileStore) SaveRun(v Run) error {
	return s.mutate(func() error { return s.inner.SaveRun(v) })
}

func (s *FileStore) GetRun(tenantID, userID, instanceID, runID string) (Run, error) {
	return s.inner.GetRun(tenantID, userID, instanceID, runID)
}

func (s *FileStore) ListRuns(tenantID, userID, instanceID string) ([]Run, error) {
	return s.inner.ListRuns(tenantID, userID, instanceID)
}

func (s *FileStore) GetRunByUserMessageID(tenantID, userID, instanceID, userMessageID string) (Run, error) {
	return s.inner.GetRunByUserMessageID(tenantID, userID, instanceID, userMessageID)
}

func (s *FileStore) SaveAuditEvent(v AuditEvent) error {
	return s.mutate(func() error { return s.inner.SaveAuditEvent(v) })
}

func (s *FileStore) ListAuditEvents(tenantID, userID string) ([]AuditEvent, error) {
	return s.inner.ListAuditEvents(tenantID, userID)
}

func (s *FileStore) DeleteAuditEvents(tenantID, userID string) (int, error) {
	if s == nil || s.inner == nil {
		return 0, ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.inner.snapshot()
	deleted, err := s.inner.DeleteAuditEvents(tenantID, userID)
	if err != nil {
		return 0, err
	}
	if err := s.flushLocked(); err != nil {
		s.inner.restore(before)
		return 0, err
	}
	return deleted, nil
}

// mutate serializes all FileStore writes before touching the inner store. The
// lock order (file mutex -> memory mutex) is shared with lifecycle/outbox
// transactions and prevents a SaveMessage/Append deadlock under concurrency.
func (s *FileStore) mutate(fn func() error) error {
	if s == nil || s.inner == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.inner.snapshot()
	if err := fn(); err != nil {
		s.inner.restore(before)
		return err
	}
	if err := s.flushLocked(); err != nil {
		s.inner.restore(before)
		return err
	}
	return nil
}

func (s *FileStore) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *FileStore) flushLocked() error {
	if s == nil || s.inner == nil {
		return ErrServiceClosed
	}

	data, err := json.MarshalIndent(s.inner.snapshot(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(s.path, data, 0o600)
}
