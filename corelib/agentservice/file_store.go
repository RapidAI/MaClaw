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
// in-memory Store implementation for query semantics and sorting.
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
	if err := s.inner.SaveTenant(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) GetTenant(id string) (Tenant, error) {
	return s.inner.GetTenant(id)
}

func (s *FileStore) ListTenants() ([]Tenant, error) {
	return s.inner.ListTenants()
}

func (s *FileStore) SaveUser(v User) error {
	if err := s.inner.SaveUser(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) GetUser(tenantID, userID string) (User, error) {
	return s.inner.GetUser(tenantID, userID)
}

func (s *FileStore) ListUsers(tenantID string) ([]User, error) {
	return s.inner.ListUsers(tenantID)
}

func (s *FileStore) SaveCredential(v Credential) error {
	if err := s.inner.SaveCredential(v); err != nil {
		return err
	}
	return s.flush()
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
	if err := s.inner.SaveUserConfig(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) GetUserConfig(tenantID, userID string) (UserConfig, error) {
	return s.inner.GetUserConfig(tenantID, userID)
}

func (s *FileStore) SaveInstance(v Instance) error {
	if err := s.inner.SaveInstance(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) GetInstance(tenantID, userID, instanceID string) (Instance, error) {
	return s.inner.GetInstance(tenantID, userID, instanceID)
}

func (s *FileStore) ListInstances(tenantID, userID string) ([]Instance, error) {
	return s.inner.ListInstances(tenantID, userID)
}

func (s *FileStore) SaveSession(v Session) error {
	if err := s.inner.SaveSession(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) GetSession(tenantID, userID, instanceID, sessionID string) (Session, error) {
	return s.inner.GetSession(tenantID, userID, instanceID, sessionID)
}

func (s *FileStore) ListSessions(tenantID, userID, instanceID string) ([]Session, error) {
	return s.inner.ListSessions(tenantID, userID, instanceID)
}

func (s *FileStore) SaveMessage(v Message) error {
	if err := s.inner.SaveMessage(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) ListMessages(sessionID string) ([]Message, error) {
	return s.inner.ListMessages(sessionID)
}

func (s *FileStore) SaveRun(v Run) error {
	if err := s.inner.SaveRun(v); err != nil {
		return err
	}
	return s.flush()
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
	if err := s.inner.SaveAuditEvent(v); err != nil {
		return err
	}
	return s.flush()
}

func (s *FileStore) ListAuditEvents(tenantID, userID string) ([]AuditEvent, error) {
	return s.inner.ListAuditEvents(tenantID, userID)
}

func (s *FileStore) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.inner.snapshot(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(s.path, data, 0o600)
}
