package agentservice

import (
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type Store interface {
	SaveTenant(Tenant) error
	GetTenant(string) (Tenant, error)
	ListTenants() ([]Tenant, error)
	DeleteTenant(string) error
	SaveUser(User) error
	GetUser(string, string) (User, error)
	ListUsers(string) ([]User, error)
	DeleteUser(string, string) error
	SaveCredential(Credential) error
	GetCredential(string, string, string) (Credential, error)
	ListCredentials(string, string) ([]Credential, error)
	GetCredentialByAPIKey(string) (Credential, error)
	SaveUserConfig(UserConfig) error
	GetUserConfig(string, string) (UserConfig, error)
	SaveInstance(Instance) error
	GetInstance(string, string, string) (Instance, error)
	ListInstances(string, string) ([]Instance, error)
	DeleteInstance(string, string, string) error
	SaveSession(Session) error
	GetSession(string, string, string, string) (Session, error)
	ListSessions(string, string, string) ([]Session, error)
	DeleteSession(string, string, string, string) error
	SaveMessage(Message) error
	ListMessages(string) ([]Message, error)
	SaveRun(Run) error
	GetRun(string, string, string, string) (Run, error)
	ListRuns(string, string, string) ([]Run, error)
	GetRunByUserMessageID(string, string, string, string) (Run, error)
	SaveAuditEvent(AuditEvent) error
	ListAuditEvents(string, string) ([]AuditEvent, error)
}

// MemoryStore is an in-process implementation of the agentservice control-plane
// Store interface. Despite the historical name, it is not Maclaw long-term memory;
// user/agent memory must go through corelib/memory.Store.
type MemoryStore struct {
	mu          sync.RWMutex
	tenants     map[string]Tenant
	users       map[string]User
	credentials map[string]Credential
	userConfigs map[string]UserConfig
	instances   map[string]Instance
	sessions    map[string]Session
	messages    map[string][]Message
	runs        map[string]Run
	auditEvents []AuditEvent
}

type storeState struct {
	Tenants     map[string]Tenant     `json:"tenants,omitempty"`
	Users       map[string]User       `json:"users,omitempty"`
	Credentials map[string]Credential `json:"credentials,omitempty"`
	UserConfigs map[string]UserConfig `json:"user_configs,omitempty"`
	Instances   map[string]Instance   `json:"instances,omitempty"`
	Sessions    map[string]Session    `json:"sessions,omitempty"`
	Messages    map[string][]Message  `json:"messages,omitempty"`
	Runs        map[string]Run        `json:"runs,omitempty"`
	AuditEvents []AuditEvent          `json:"audit_events,omitempty"`
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tenants:     map[string]Tenant{},
		users:       map[string]User{},
		credentials: map[string]Credential{},
		userConfigs: map[string]UserConfig{},
		instances:   map[string]Instance{},
		sessions:    map[string]Session{},
		messages:    map[string][]Message{},
		runs:        map[string]Run{},
		auditEvents: []AuditEvent{},
	}
}

func newMemoryStoreFromState(state storeState) *MemoryStore {
	s := NewMemoryStore()
	if state.Tenants != nil {
		s.tenants = state.Tenants
	}
	if state.Users != nil {
		s.users = state.Users
	}
	if state.Credentials != nil {
		s.credentials = make(map[string]Credential, len(state.Credentials))
		for k, v := range state.Credentials {
			stored := normalizeStoredCredential(v, k)
			lookupKey := credentialLookupKey(stored)
			if lookupKey == "" {
				continue
			}
			s.credentials[lookupKey] = stored
		}
	}
	if state.UserConfigs != nil {
		s.userConfigs = state.UserConfigs
	}
	if state.Instances != nil {
		s.instances = state.Instances
	}
	if state.Sessions != nil {
		s.sessions = state.Sessions
	}
	if state.Messages != nil {
		s.messages = state.Messages
	}
	if state.Runs != nil {
		s.runs = state.Runs
	}
	if state.AuditEvents != nil {
		s.auditEvents = state.AuditEvents
	}
	return s
}

func (s *MemoryStore) snapshot() storeState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := storeState{
		Tenants:     make(map[string]Tenant, len(s.tenants)),
		Users:       make(map[string]User, len(s.users)),
		Credentials: make(map[string]Credential, len(s.credentials)),
		UserConfigs: make(map[string]UserConfig, len(s.userConfigs)),
		Instances:   make(map[string]Instance, len(s.instances)),
		Sessions:    make(map[string]Session, len(s.sessions)),
		Messages:    make(map[string][]Message, len(s.messages)),
		Runs:        make(map[string]Run, len(s.runs)),
		AuditEvents: append([]AuditEvent(nil), s.auditEvents...),
	}
	for k, v := range s.tenants {
		state.Tenants[k] = v
	}
	for k, v := range s.users {
		state.Users[k] = v
	}
	for k, v := range s.credentials {
		state.Credentials[k] = v
	}
	for k, v := range s.userConfigs {
		state.UserConfigs[k] = v
	}
	for k, v := range s.instances {
		state.Instances[k] = v
	}
	for k, v := range s.sessions {
		state.Sessions[k] = v
	}
	for k, v := range s.messages {
		state.Messages[k] = append([]Message(nil), v...)
	}
	for k, v := range s.runs {
		state.Runs[k] = v
	}
	return state
}

func NewID(prefix string) string { return prefix + "_" + uuid.NewString() }

func composite(tenantID, userID string) string { return tenantID + ":" + userID }

func (s *MemoryStore) SaveTenant(v Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[v.ID] = v
	return nil
}

func (s *MemoryStore) GetTenant(id string) (Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.tenants[id]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	return v, nil
}

func (s *MemoryStore) ListTenants() ([]Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tenant, 0, len(s.tenants))
	for _, v := range s.tenants {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) DeleteTenant(tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[tenantID]; !ok {
		return ErrTenantNotFound
	}
	delete(s.tenants, tenantID)
	for key, user := range s.users {
		if user.TenantID == tenantID {
			delete(s.users, key)
		}
	}
	for key, cred := range s.credentials {
		if cred.TenantID == tenantID {
			delete(s.credentials, key)
		}
	}
	for key, cfg := range s.userConfigs {
		if cfg.TenantID == tenantID {
			delete(s.userConfigs, key)
		}
	}
	for instanceID, inst := range s.instances {
		if inst.TenantID == tenantID {
			delete(s.instances, instanceID)
		}
	}
	for sessionID, sess := range s.sessions {
		if sess.TenantID == tenantID {
			delete(s.sessions, sessionID)
			delete(s.messages, sessionID)
		}
	}
	for runID, run := range s.runs {
		if run.TenantID == tenantID {
			delete(s.runs, runID)
		}
	}
	return nil
}

func (s *MemoryStore) SaveUser(v User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[composite(v.TenantID, v.ID)] = v
	return nil
}

func (s *MemoryStore) GetUser(tenantID, userID string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.users[composite(tenantID, userID)]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return v, nil
}

func (s *MemoryStore) ListUsers(tenantID string) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0)
	for _, v := range s.users {
		if v.TenantID == tenantID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) DeleteUser(tenantID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := composite(tenantID, userID)
	if _, ok := s.users[key]; !ok {
		return ErrUserNotFound
	}
	delete(s.users, key)
	delete(s.userConfigs, key)
	for lookupKey, cred := range s.credentials {
		if cred.TenantID == tenantID && cred.UserID == userID {
			delete(s.credentials, lookupKey)
		}
	}
	for instanceID, inst := range s.instances {
		if inst.TenantID == tenantID && inst.UserID == userID {
			delete(s.instances, instanceID)
		}
	}
	for sessionID, sess := range s.sessions {
		if sess.TenantID == tenantID && sess.UserID == userID {
			delete(s.sessions, sessionID)
			delete(s.messages, sessionID)
		}
	}
	for runID, run := range s.runs {
		if run.TenantID == tenantID && run.UserID == userID {
			delete(s.runs, runID)
		}
	}
	return nil
}

func (s *MemoryStore) SaveCredential(v Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := normalizeStoredCredential(v, "")
	lookupKey := credentialLookupKey(stored)
	if lookupKey == "" {
		return ErrCredentialNotFound
	}
	for key, existing := range s.credentials {
		if existing.ID == stored.ID {
			delete(s.credentials, key)
		}
	}
	s.credentials[lookupKey] = stored
	return nil
}

func (s *MemoryStore) GetCredential(tenantID, userID, credentialID string) (Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.credentials {
		if v.ID == credentialID && v.TenantID == tenantID && v.UserID == userID {
			return v, nil
		}
	}
	return Credential{}, ErrCredentialNotFound
}

func (s *MemoryStore) ListCredentials(tenantID, userID string) ([]Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Credential, 0)
	for _, v := range s.credentials {
		if v.TenantID == tenantID && v.UserID == userID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) GetCredentialByAPIKey(apiKey string) (Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lookupKey := credentialLookupKey(Credential{APIKey: apiKey})
	if lookupKey == "" {
		return Credential{}, ErrCredentialNotFound
	}
	if v, ok := s.credentials[lookupKey]; ok {
		return v, nil
	}
	return Credential{}, ErrCredentialNotFound
}

func (s *MemoryStore) SaveUserConfig(v UserConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userConfigs[composite(v.TenantID, v.UserID)] = v
	return nil
}

func (s *MemoryStore) GetUserConfig(tenantID, userID string) (UserConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.userConfigs[composite(tenantID, userID)]
	if !ok {
		return UserConfig{}, ErrUserConfigNotFound
	}
	return v, nil
}

func (s *MemoryStore) SaveInstance(v Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[v.ID] = v
	return nil
}

func (s *MemoryStore) GetInstance(tenantID, userID, instanceID string) (Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.instances[instanceID]
	if !ok || v.TenantID != tenantID || v.UserID != userID {
		return Instance{}, ErrInstanceNotFound
	}
	return v, nil
}

func (s *MemoryStore) ListInstances(tenantID, userID string) ([]Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Instance, 0)
	for _, v := range s.instances {
		if v.TenantID == tenantID && v.UserID == userID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) DeleteInstance(tenantID, userID, instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.instances[instanceID]
	if !ok || v.TenantID != tenantID || v.UserID != userID {
		return ErrInstanceNotFound
	}
	delete(s.instances, instanceID)
	for sessionID, sess := range s.sessions {
		if sess.TenantID == tenantID && sess.UserID == userID && sess.InstanceID == instanceID {
			delete(s.sessions, sessionID)
			delete(s.messages, sessionID)
		}
	}
	for runID, run := range s.runs {
		if run.TenantID == tenantID && run.UserID == userID && run.InstanceID == instanceID {
			delete(s.runs, runID)
		}
	}
	return nil
}

func (s *MemoryStore) SaveSession(v Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[v.ID] = v
	return nil
}

func (s *MemoryStore) GetSession(tenantID, userID, instanceID, sessionID string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.sessions[sessionID]
	if !ok || v.TenantID != tenantID || v.UserID != userID || v.InstanceID != instanceID {
		return Session{}, ErrSessionNotFound
	}
	return v, nil
}

func (s *MemoryStore) ListSessions(tenantID, userID, instanceID string) ([]Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0)
	for _, v := range s.sessions {
		if v.TenantID == tenantID && v.UserID == userID && v.InstanceID == instanceID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) DeleteSession(tenantID, userID, instanceID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[sessionID]
	if !ok || v.TenantID != tenantID || v.UserID != userID || v.InstanceID != instanceID {
		return ErrSessionNotFound
	}
	delete(s.sessions, sessionID)
	delete(s.messages, sessionID)
	for runID, run := range s.runs {
		if run.TenantID == tenantID && run.UserID == userID && run.InstanceID == instanceID && run.SessionID == sessionID {
			delete(s.runs, runID)
		}
	}
	return nil
}

func (s *MemoryStore) SaveMessage(v Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[v.SessionID] = append(s.messages[v.SessionID], v)
	return nil
}

func (s *MemoryStore) ListMessages(sessionID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Message(nil), s.messages[sessionID]...)
	return out, nil
}

func (s *MemoryStore) SaveRun(v Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[v.ID] = v
	return nil
}

func (s *MemoryStore) GetRun(tenantID, userID, instanceID, runID string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.runs[runID]
	if !ok || v.TenantID != tenantID || v.UserID != userID || v.InstanceID != instanceID {
		return Run{}, ErrRunNotFound
	}
	return v, nil
}

func (s *MemoryStore) ListRuns(tenantID, userID, instanceID string) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Run, 0)
	for _, v := range s.runs {
		if v.TenantID == tenantID && v.UserID == userID && v.InstanceID == instanceID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

func (s *MemoryStore) GetRunByUserMessageID(tenantID, userID, instanceID, userMessageID string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.runs {
		if v.TenantID == tenantID && v.UserID == userID && v.InstanceID == instanceID && v.UserMessageID == userMessageID {
			return v, nil
		}
	}
	return Run{}, ErrRunNotFound
}

func (s *MemoryStore) SaveAuditEvent(v AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditEvents = append(s.auditEvents, v)
	return nil
}

func (s *MemoryStore) ListAuditEvents(tenantID, userID string) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEvent, 0, len(s.auditEvents))
	for _, v := range s.auditEvents {
		if tenantID != "" && v.TenantID != tenantID {
			continue
		}
		if userID != "" && v.UserID != userID {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func normalizeStoredCredential(v Credential, legacyLookupKey string) Credential {
	if strings.TrimSpace(v.APIKeyHash) == "" {
		source := strings.TrimSpace(v.APIKey)
		if source == "" {
			source = strings.TrimSpace(legacyLookupKey)
		}
		if source != "" {
			v.APIKeyHash = hashAPIKey(source)
		}
	}
	if strings.TrimSpace(v.APIKeyPrefix) == "" {
		source := strings.TrimSpace(v.APIKey)
		if source == "" {
			source = strings.TrimSpace(legacyLookupKey)
		}
		if source != "" {
			v.APIKeyPrefix = deriveAPIKeyPrefix(source)
		}
	}
	if strings.TrimSpace(v.APIKeyHash) != "" {
		v.APIKey = ""
	}
	v.APISecret = ""
	v.TokenVersion = credentialTokenVersion(v)
	return v
}
