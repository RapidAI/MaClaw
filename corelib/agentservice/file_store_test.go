package agentservice

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newTestFileStore opens a FileStore and registers Close so Windows releases
// the SQLite file lock before t.TempDir cleanup runs.
func newTestFileStore(t *testing.T, path string) *FileStore {
	t.Helper()
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestFileStorePersistsControlPlaneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "store.json")
	store := newTestFileStore(t, path)

	now := time.Now().UTC()
	tenant := Tenant{ID: "tenant_1", Name: "Tenant", Status: TenantStatusActive, CreatedAt: now, UpdatedAt: now}
	user := User{ID: "user_1", TenantID: tenant.ID, Name: "User", Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	cred := Credential{ID: "cred_1", TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKeyPrefix: deriveAPIKeyPrefix("key"), APIKeyHash: hashAPIKey("key"), Status: CredentialStatusActive, SecretDigest: "digest", CreatedAt: now, UpdatedAt: now}
	inst := Instance{ID: "inst_1", TenantID: tenant.ID, UserID: user.ID, Name: "Instance", Status: InstanceStatusReady, CreatedAt: now, UpdatedAt: now}
	sess := Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	msg := Message{ID: "msg_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: MessageRoleUser, Content: "hello", CreatedAt: now}
	run := Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, UserMessageID: msg.ID, Status: RunStatusSucceeded, StartedAt: now}
	audit := AuditEvent{ID: "audit_1", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "tenant.created", ResourceType: "tenant", ResourceID: tenant.ID, CreatedAt: now}

	for name, err := range map[string]error{
		"tenant":     store.SaveTenant(tenant),
		"user":       store.SaveUser(user),
		"credential": store.SaveCredential(cred),
		"instance":   store.SaveInstance(inst),
		"session":    store.SaveSession(sess),
		"message":    store.SaveMessage(msg),
		"run":        store.SaveRun(run),
		"audit":      store.SaveAuditEvent(audit),
	} {
		if err != nil {
			t.Fatalf("Save%s: %v", name, err)
		}
	}

	data := readFileStoreArtifact(t, path)
	if strings.Contains(string(data), `"api_key":"key"`) {
		t.Fatalf("store.db should not persist plaintext api_key: %s", string(data))
	}

	reopened := newTestFileStore(t, path)
	if _, err := reopened.GetTenant(tenant.ID); err != nil {
		t.Fatalf("GetTenant after reopen: %v", err)
	}
	tenants, err := reopened.ListTenants()
	if err != nil {
		t.Fatalf("ListTenants after reopen: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != tenant.ID {
		t.Fatalf("ListTenants after reopen = %#v", tenants)
	}
	if _, err := reopened.GetUser(tenant.ID, user.ID); err != nil {
		t.Fatalf("GetUser after reopen: %v", err)
	}
	users, err := reopened.ListUsers(tenant.ID)
	if err != nil {
		t.Fatalf("ListUsers after reopen: %v", err)
	}
	if len(users) != 1 || users[0].ID != user.ID {
		t.Fatalf("ListUsers after reopen = %#v", users)
	}
	if _, err := reopened.GetCredentialByAPIKey("key"); err != nil {
		t.Fatalf("GetCredentialByAPIKey after reopen: %v", err)
	}
	credentials, err := reopened.ListCredentials(tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("ListCredentials after reopen: %v", err)
	}
	if len(credentials) != 1 || credentials[0].ID != cred.ID || credentials[0].Status != CredentialStatusActive {
		t.Fatalf("ListCredentials after reopen = %#v", credentials)
	}
	if _, err := reopened.GetInstance(tenant.ID, user.ID, inst.ID); err != nil {
		t.Fatalf("GetInstance after reopen: %v", err)
	}
	if _, err := reopened.GetSession(tenant.ID, user.ID, inst.ID, sess.ID); err != nil {
		t.Fatalf("GetSession after reopen: %v", err)
	}
	messages, err := reopened.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages after reopen: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != msg.ID {
		t.Fatalf("ListMessages after reopen = %#v", messages)
	}
	if _, err := reopened.GetRun(tenant.ID, user.ID, inst.ID, run.ID); err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	runs, err := reopened.ListRuns(tenant.ID, user.ID, inst.ID)
	if err != nil {
		t.Fatalf("ListRuns after reopen: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID || runs[0].Status != RunStatusSucceeded {
		t.Fatalf("ListRuns after reopen = %#v", runs)
	}
	if _, err := reopened.GetRunByUserMessageID(tenant.ID, user.ID, inst.ID, msg.ID); err != nil {
		t.Fatalf("GetRunByUserMessageID after reopen: %v", err)
	}
	auditEvents, err := reopened.ListAuditEvents(tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("ListAuditEvents after reopen: %v", err)
	}
	if len(auditEvents) != 1 || auditEvents[0].ID != audit.ID || auditEvents[0].Action != audit.Action {
		t.Fatalf("ListAuditEvents after reopen = %#v", auditEvents)
	}
}

func TestFileStoreMigratesLegacyPlaintextAPIKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "store.json")
	legacy := `{
  "credentials": {
    "legacy-key": {
      "id": "cred_legacy",
      "tenant_id": "tenant_1",
      "user_id": "user_1",
      "name": "Legacy",
      "api_key": "legacy-key",
      "status": "active",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  }
}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := newTestFileStore(t, path)
	cred, err := store.GetCredentialByAPIKey("legacy-key")
	if err != nil {
		t.Fatalf("GetCredentialByAPIKey: %v", err)
	}
	if cred.APIKeyHash == "" || cred.APIKeyPrefix == "" {
		t.Fatalf("expected migrated credential hash/prefix, got %#v", cred)
	}
	if err := store.SaveCredential(cred); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	// The legacy JSON is the pre-migration file: a fresh database imports it
	// once and then removes it, so the plaintext key leaves disk entirely.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy store.json should be removed after import, stat err=%v", err)
	}
	data := readFileStoreArtifact(t, path)
	if strings.Contains(string(data), "legacy-key") {
		t.Fatalf("legacy plaintext api_key should be gone from store.db: %s", string(data))
	}
}

func TestNewFileStoreCreatesPrivateStateDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "store.json")
	newTestFileStore(t, path)
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat state dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state dir perms = %#o, want 0700", info.Mode().Perm())
	}
}

func TestFileStoreNewCredentialNeverPersistsPlaintextAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "store.json")
	store := newTestFileStore(t, path)
	now := time.Now().UTC()
	cred := Credential{
		ID:           "cred_plain",
		TenantID:     "tenant_1",
		UserID:       "user_1",
		Name:         "API",
		APIKeyPrefix: deriveAPIKeyPrefix("plain-key-123"),
		APIKeyHash:   hashAPIKey("plain-key-123"),
		Status:       CredentialStatusActive,
		SecretDigest: "digest",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.SaveCredential(cred); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	data := readFileStoreArtifact(t, path)
	text := string(data)
	if strings.Contains(text, "plain-key-123") || strings.Contains(text, "\"api_key\"") {
		t.Fatalf("store.db should not persist plaintext api_key fields: %s", text)
	}
}

// readFileStoreArtifact reads the SQLite database file together with its WAL
// sidecar so assertions observe every byte the store wrote.
func readFileStoreArtifact(t *testing.T, jsonPath string) []byte {
	t.Helper()
	dbPath := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".db"
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", dbPath, err)
	}
	if wal, err := os.ReadFile(dbPath + "-wal"); err == nil {
		data = append(data, wal...)
	}
	return data
}
