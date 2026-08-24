package agentservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DynamicCapabilityContractResolver is the read-only runtime view of trusted
// dynamic declarations. Agent execution receives only this interface; it has
// no mutation authority and therefore cannot turn a discovered provider into
// a routable capability.
type DynamicCapabilityContractResolver interface {
	MCPDynamicContractResolver
	SkillDynamicContractResolver
}

// DynamicCapabilityContractRegistry is the private control-plane persistence
// boundary for dynamic capability declarations. Service owns it and gives
// publication authority only to DynamicCapabilityContractPublisher; normal
// Agent execution receives DynamicCapabilityContractResolver instead.
// Implementations must fail closed: an unavailable or corrupt declaration is
// not routable.
type DynamicCapabilityContractRegistry interface {
	DynamicCapabilityContractResolver
	PublishMCPContract(p Principal, serverID, toolName string, contract DynamicCapabilityContract) error
	PublishSkillContract(p Principal, stableID string, contract DynamicCapabilityContract) error
	RevokeMCPContract(p Principal, serverID, toolName string) error
	RevokeMCPServerContracts(p Principal, serverID string) error
	RevokeSkillContract(p Principal, stableID string) error
	ClearPrincipal(p Principal) error
}

// dynamicCapabilityContractSnapshotProvider is an internal consistency
// boundary for catalog builders. A per-binding Resolve call is sufficient for
// an individual execution-time revalidation, but it can combine contracts
// from different control-plane generations while a single catalog is being
// built. Dynamic inventories use this optional snapshot when available.
//
// It is intentionally not part of the public mutation API: callers receive a
// read-only resolver, never a way to manufacture or publish contracts.
type dynamicCapabilityContractSnapshotProvider interface {
	SnapshotDynamicCapabilityContracts(Principal) (dynamicCapabilityContractSnapshot, error)
}

type dynamicCapabilityContractSnapshot struct {
	principal Principal
	contracts map[string]DynamicCapabilityContract
}

func (s dynamicCapabilityContractSnapshot) ResolveMCPDynamicContract(_ context.Context, p Principal, serverID, toolName string) (DynamicCapabilityContract, bool) {
	if !dynamicContractSnapshotPrincipalMatches(s.principal, p) || strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return DynamicCapabilityContract{}, false
	}
	return s.resolve(dynamicContractRegistryKey(p, "mcp", serverID, toolName))
}

func (s dynamicCapabilityContractSnapshot) ResolveSkillDynamicContract(_ context.Context, p Principal, stableID string) (DynamicCapabilityContract, bool) {
	if !dynamicContractSnapshotPrincipalMatches(s.principal, p) || strings.TrimSpace(stableID) == "" {
		return DynamicCapabilityContract{}, false
	}
	return s.resolve(dynamicContractRegistryKey(p, "skill", stableID))
}

func (s dynamicCapabilityContractSnapshot) resolve(key string) (DynamicCapabilityContract, bool) {
	contract, ok := s.contracts[key]
	if !ok || contract.validate() != nil {
		return DynamicCapabilityContract{}, false
	}
	return cloneDynamicCapabilityContract(contract), true
}

func dynamicContractSnapshotPrincipalMatches(a, b Principal) bool {
	return dynamicContractScopeValue(a.TenantID) == dynamicContractScopeValue(b.TenantID) && dynamicContractScopeValue(a.UserID) == dynamicContractScopeValue(b.UserID) && validateDynamicContractScope(b) == nil
}

// DynamicCapabilityRegistry is the service-owned control-plane publication
// point for dynamic provider contracts. It is deliberately independent from
// agent prompts, discovery responses and runtime tool schemas. The registry
// keys registrations by principal scope and exact provider binding, so a
// contract granted to one user's server or Skill cannot leak to another.
//
// The in-memory implementation is restricted to tests and explicit
// single-process development. File-backed Services use the durable SQLite
// implementation below.
type DynamicCapabilityRegistry struct {
	mu        sync.RWMutex
	contracts map[string]DynamicCapabilityContract
}

func NewDynamicCapabilityRegistry() *DynamicCapabilityRegistry {
	return &DynamicCapabilityRegistry{contracts: make(map[string]DynamicCapabilityContract)}
}

// PublishMCPContract is a control-plane operation. It must be called by an
// authenticated lifecycle/configuration path, never from Agent tool execution.
func (r *DynamicCapabilityRegistry) PublishMCPContract(p Principal, serverID, toolName string, contract DynamicCapabilityContract) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("MCP contract requires server and tool identity")
	}
	if err := contract.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	r.contracts[dynamicContractRegistryKey(p, "mcp", serverID, toolName)] = cloneDynamicCapabilityContract(contract)
	r.mu.Unlock()
	return nil
}

// PublishSkillContract is a control-plane operation keyed by immutable Skill
// identity, never by the user-facing/alias name.
func (r *DynamicCapabilityRegistry) PublishSkillContract(p Principal, stableID string, contract DynamicCapabilityContract) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if strings.TrimSpace(stableID) == "" {
		return fmt.Errorf("Skill contract requires stable identity")
	}
	if err := contract.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	r.contracts[dynamicContractRegistryKey(p, "skill", stableID)] = cloneDynamicCapabilityContract(contract)
	r.mu.Unlock()
	return nil
}

// RevokeMCPContract and RevokeSkillContract remove a routability declaration.
// Existing bound calls fail their execution-time revalidation rather than
// continuing to use a revoked contract.
func (r *DynamicCapabilityRegistry) RevokeMCPContract(p Principal, serverID, toolName string) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("MCP contract requires server and tool identity")
	}
	r.mu.Lock()
	delete(r.contracts, dynamicContractRegistryKey(p, "mcp", serverID, toolName))
	r.mu.Unlock()
	return nil
}

// RevokeMCPServerContracts removes every declaration bound to one immutable
// MCP server identity. Server configuration changes invalidate the observed
// transport/schema identity of every tool beneath that server; retaining a
// per-tool declaration would let a replacement endpoint inherit authority it
// was never reviewed for.
func (r *DynamicCapabilityRegistry) RevokeMCPServerContracts(p Principal, serverID string) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if strings.TrimSpace(serverID) == "" {
		return fmt.Errorf("MCP contract requires server identity")
	}
	prefix := dynamicContractRegistryKey(p, "mcp", serverID) + "\x00"
	r.mu.Lock()
	for key := range r.contracts {
		if strings.HasPrefix(key, prefix) {
			delete(r.contracts, key)
		}
	}
	r.mu.Unlock()
	return nil
}

func (r *DynamicCapabilityRegistry) RevokeSkillContract(p Principal, stableID string) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if strings.TrimSpace(stableID) == "" {
		return fmt.Errorf("Skill contract requires stable identity")
	}
	r.mu.Lock()
	delete(r.contracts, dynamicContractRegistryKey(p, "skill", stableID))
	r.mu.Unlock()
	return nil
}

// ClearPrincipal removes all dynamic contracts belonging to a principal. It
// is called after a control-plane configuration update so a newly configured
// server/Skill cannot inherit a contract published for the previous config.
// The next trusted publisher must re-bind and re-validate every declaration.
func (r *DynamicCapabilityRegistry) ClearPrincipal(p Principal) error {
	if r == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	prefix := dynamicContractRegistryPrincipalPrefix(p)
	r.mu.Lock()
	for key := range r.contracts {
		if strings.HasPrefix(key, prefix) {
			delete(r.contracts, key)
		}
	}
	r.mu.Unlock()
	return nil
}

func (r *DynamicCapabilityRegistry) ResolveMCPDynamicContract(_ context.Context, p Principal, serverID, toolName string) (DynamicCapabilityContract, bool) {
	if validateDynamicContractScope(p) != nil || strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return DynamicCapabilityContract{}, false
	}
	return r.resolve(dynamicContractRegistryKey(p, "mcp", serverID, toolName))
}

func (r *DynamicCapabilityRegistry) ResolveSkillDynamicContract(_ context.Context, p Principal, stableID string) (DynamicCapabilityContract, bool) {
	if validateDynamicContractScope(p) != nil || strings.TrimSpace(stableID) == "" {
		return DynamicCapabilityContract{}, false
	}
	return r.resolve(dynamicContractRegistryKey(p, "skill", stableID))
}

func (r *DynamicCapabilityRegistry) resolve(key string) (DynamicCapabilityContract, bool) {
	if r == nil {
		return DynamicCapabilityContract{}, false
	}
	r.mu.RLock()
	contract, ok := r.contracts[key]
	r.mu.RUnlock()
	if !ok || contract.validate() != nil {
		return DynamicCapabilityContract{}, false
	}
	return cloneDynamicCapabilityContract(contract), true
}

// SnapshotDynamicCapabilityContracts captures one principal-scoped immutable
// view for a catalog generation. It never exposes contracts from another
// tenant or user, and a later publication cannot alter this value.
func (r *DynamicCapabilityRegistry) SnapshotDynamicCapabilityContracts(p Principal) (dynamicCapabilityContractSnapshot, error) {
	if r == nil {
		return dynamicCapabilityContractSnapshot{}, fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return dynamicCapabilityContractSnapshot{}, err
	}
	prefix := dynamicContractRegistryPrincipalPrefix(p)
	contracts := make(map[string]DynamicCapabilityContract)
	r.mu.RLock()
	for key, contract := range r.contracts {
		if strings.HasPrefix(key, prefix) {
			contracts[key] = cloneDynamicCapabilityContract(contract)
		}
	}
	r.mu.RUnlock()
	return dynamicCapabilityContractSnapshot{principal: p, contracts: contracts}, nil
}

func validateDynamicContractScope(p Principal) error {
	if strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("dynamic capability contract requires tenant and user scope")
	}
	return nil
}

func dynamicContractRegistryKey(p Principal, kind string, identities ...string) string {
	parts := []string{strings.TrimSuffix(dynamicContractRegistryPrincipalPrefix(p), "\x00"), strings.ToLower(strings.TrimSpace(kind))}
	for _, identity := range identities {
		parts = append(parts, strings.ToLower(strings.TrimSpace(identity)))
	}
	return strings.Join(parts, "\x00")
}

func dynamicContractRegistryPrincipalPrefix(p Principal) string {
	return strings.ToLower(strings.TrimSpace(p.TenantID)) + "\x00" + strings.ToLower(strings.TrimSpace(p.UserID)) + "\x00"
}

// SQLiteDynamicCapabilityRegistry provides durable trusted-contract
// publication for file-backed Services. It never returns a row until both the
// decoded contract and the digest persisted beside it validate.
type SQLiteDynamicCapabilityRegistry struct {
	db *sql.DB
}

func NewSQLiteDynamicCapabilityRegistry(dbPath string) (*SQLiteDynamicCapabilityRegistry, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("dynamic capability registry path is required")
	}
	if err := secureMkdirAll(filepath.Dir(dbPath)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &SQLiteDynamicCapabilityRegistry{db: db}
	if err := r.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *SQLiteDynamicCapabilityRegistry) init() error {
	if r == nil || r.db == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	for _, stmt := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`CREATE TABLE IF NOT EXISTS dynamic_capability_contracts (
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			provider_kind TEXT NOT NULL,
			binding_key TEXT NOT NULL,
			contract_json TEXT NOT NULL,
			contract_digest TEXT NOT NULL,
			published_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, user_id, provider_kind, binding_key)
		)`,
	} {
		if _, err := r.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteDynamicCapabilityRegistry) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *SQLiteDynamicCapabilityRegistry) PublishMCPContract(p Principal, serverID, toolName string, contract DynamicCapabilityContract) error {
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("MCP contract requires server and tool identity")
	}
	return r.publish(p, "mcp", dynamicContractBindingKey(serverID, toolName), contract)
}

func (r *SQLiteDynamicCapabilityRegistry) PublishSkillContract(p Principal, stableID string, contract DynamicCapabilityContract) error {
	if strings.TrimSpace(stableID) == "" {
		return fmt.Errorf("Skill contract requires stable identity")
	}
	return r.publish(p, "skill", dynamicContractBindingKey(stableID), contract)
}

func (r *SQLiteDynamicCapabilityRegistry) publish(p Principal, kind, bindingKey string, contract DynamicCapabilityContract) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	if err := contract.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(cloneDynamicCapabilityContract(contract))
	if err != nil {
		return fmt.Errorf("encode dynamic capability contract: %w", err)
	}
	_, err = r.db.Exec(`INSERT INTO dynamic_capability_contracts
		(tenant_id, user_id, provider_kind, binding_key, contract_json, contract_digest, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, user_id, provider_kind, binding_key) DO UPDATE SET
		contract_json=excluded.contract_json, contract_digest=excluded.contract_digest, published_at=excluded.published_at`,
		dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID), kind, bindingKey, string(data), contract.Digest(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("publish dynamic capability contract: %w", err)
	}
	return nil
}

func (r *SQLiteDynamicCapabilityRegistry) RevokeMCPContract(p Principal, serverID, toolName string) error {
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("MCP contract requires server and tool identity")
	}
	return r.revoke(p, "mcp", dynamicContractBindingKey(serverID, toolName))
}

// RevokeMCPServerContracts is the durable counterpart of the in-memory
// server-scope revocation. Use a transaction and exact binding-key comparisons
// rather than a SQL LIKE prefix so a server ID containing wildcard characters
// can never revoke another server's declarations.
func (r *SQLiteDynamicCapabilityRegistry) RevokeMCPServerContracts(p Principal, serverID string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	serverID = strings.ToLower(strings.TrimSpace(serverID))
	if serverID == "" {
		return fmt.Errorf("MCP contract requires server identity")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin MCP contract revocation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT binding_key FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ? AND provider_kind = 'mcp'`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID))
	if err != nil {
		return fmt.Errorf("list MCP contracts for revocation: %w", err)
	}
	var keys []string
	for rows.Next() {
		var bindingKey string
		if err := rows.Scan(&bindingKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read MCP contract for revocation: %w", err)
		}
		parts := strings.Split(bindingKey, "\x00")
		if len(parts) == 2 && strings.EqualFold(parts[0], serverID) {
			keys = append(keys, bindingKey)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate MCP contracts for revocation: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close MCP contract revocation rows: %w", err)
	}
	for _, bindingKey := range keys {
		if _, err := tx.Exec(`DELETE FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ? AND provider_kind = 'mcp' AND binding_key = ?`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID), bindingKey); err != nil {
			return fmt.Errorf("revoke MCP contract: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit MCP contract revocation: %w", err)
	}
	return nil
}

func (r *SQLiteDynamicCapabilityRegistry) RevokeSkillContract(p Principal, stableID string) error {
	if strings.TrimSpace(stableID) == "" {
		return fmt.Errorf("Skill contract requires stable identity")
	}
	return r.revoke(p, "skill", dynamicContractBindingKey(stableID))
}

func (r *SQLiteDynamicCapabilityRegistry) revoke(p Principal, kind, bindingKey string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ? AND provider_kind = ? AND binding_key = ?`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID), kind, bindingKey)
	return err
}

func (r *SQLiteDynamicCapabilityRegistry) ClearPrincipal(p Principal) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ?`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID))
	return err
}

func (r *SQLiteDynamicCapabilityRegistry) ResolveMCPDynamicContract(_ context.Context, p Principal, serverID, toolName string) (DynamicCapabilityContract, bool) {
	return r.resolve(p, "mcp", dynamicContractBindingKey(serverID, toolName))
}

func (r *SQLiteDynamicCapabilityRegistry) ResolveSkillDynamicContract(_ context.Context, p Principal, stableID string) (DynamicCapabilityContract, bool) {
	return r.resolve(p, "skill", dynamicContractBindingKey(stableID))
}

func (r *SQLiteDynamicCapabilityRegistry) resolve(p Principal, kind, bindingKey string) (DynamicCapabilityContract, bool) {
	if r == nil || r.db == nil || validateDynamicContractScope(p) != nil || strings.TrimSpace(bindingKey) == "" {
		return DynamicCapabilityContract{}, false
	}
	var raw, digest string
	err := r.db.QueryRow(`SELECT contract_json, contract_digest FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ? AND provider_kind = ? AND binding_key = ?`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID), kind, bindingKey).Scan(&raw, &digest)
	if err != nil {
		return DynamicCapabilityContract{}, false
	}
	var contract DynamicCapabilityContract
	if json.Unmarshal([]byte(raw), &contract) != nil || contract.validate() != nil || contract.Digest() != digest {
		return DynamicCapabilityContract{}, false
	}
	return cloneDynamicCapabilityContract(contract), true
}

// SnapshotDynamicCapabilityContracts loads every contract in a principal
// scope in one SQLite read. Invalid/corrupt rows remain absent (quarantined)
// in the returned snapshot; a database error rejects the whole inventory so a
// catalog never quietly joins a partial control-plane read to live discovery.
func (r *SQLiteDynamicCapabilityRegistry) SnapshotDynamicCapabilityContracts(p Principal) (dynamicCapabilityContractSnapshot, error) {
	if r == nil || r.db == nil {
		return dynamicCapabilityContractSnapshot{}, fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := validateDynamicContractScope(p); err != nil {
		return dynamicCapabilityContractSnapshot{}, err
	}
	rows, err := r.db.Query(`SELECT provider_kind, binding_key, contract_json, contract_digest FROM dynamic_capability_contracts WHERE tenant_id = ? AND user_id = ?`, dynamicContractScopeValue(p.TenantID), dynamicContractScopeValue(p.UserID))
	if err != nil {
		return dynamicCapabilityContractSnapshot{}, fmt.Errorf("load dynamic capability contracts: %w", err)
	}
	defer rows.Close()
	contracts := make(map[string]DynamicCapabilityContract)
	for rows.Next() {
		var kind, bindingKey, raw, digest string
		if err := rows.Scan(&kind, &bindingKey, &raw, &digest); err != nil {
			return dynamicCapabilityContractSnapshot{}, fmt.Errorf("read dynamic capability contract: %w", err)
		}
		var contract DynamicCapabilityContract
		if json.Unmarshal([]byte(raw), &contract) != nil || contract.validate() != nil || contract.Digest() != digest {
			continue // A bad row is quarantined; it cannot affect valid bindings.
		}
		identities := strings.Split(bindingKey, "\x00")
		contracts[dynamicContractRegistryKey(p, kind, identities...)] = cloneDynamicCapabilityContract(contract)
	}
	if err := rows.Err(); err != nil {
		return dynamicCapabilityContractSnapshot{}, fmt.Errorf("iterate dynamic capability contracts: %w", err)
	}
	return dynamicCapabilityContractSnapshot{principal: p, contracts: contracts}, nil
}

func dynamicContractBindingKey(identities ...string) string {
	parts := make([]string, len(identities))
	for i, identity := range identities {
		parts[i] = strings.ToLower(strings.TrimSpace(identity))
	}
	return strings.Join(parts, "\x00")
}

func dynamicContractScopeValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
