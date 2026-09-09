package agentservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const skillExternalSnapshotSchema = "maclaw.external-snapshot/v1"

const skillExternalSnapshotKind = "dynamic_skill_contract"

// skillContractExternalSnapshot is the versioned, principal-bound envelope
// stored in the generic compensation queue.  The queue treats the value as
// opaque; AgentService validates the envelope before it can mutate the
// dynamic-contract registry.  PreImage deliberately contains only the
// contract declaration, never credentials or provider payloads.
type skillContractExternalSnapshot struct {
	Schema        string          `json:"schema"`
	Kind          string          `json:"kind"`
	TenantID      string          `json:"tenant_id"`
	UserID        string          `json:"user_id"`
	StableID      string          `json:"stable_id"`
	PayloadDigest string          `json:"payload_digest"`
	PreImage      json.RawMessage `json:"pre_image"`
	CapturedAt    string          `json:"captured_at"`
	RequestID     string          `json:"request_id"`
}

func encodeSkillContractExternalSnapshot(principal Principal, stableID string, contract DynamicCapabilityContract, requestID string, capturedAt time.Time) (string, error) {
	stableID = strings.TrimSpace(stableID)
	if !validRecoveryScopeSegment(principal.TenantID) || !validRecoveryScopeSegment(principal.UserID) || stableID == "" {
		return "", fmt.Errorf("invalid Skill contract snapshot identity")
	}
	if err := contract.validate(); err != nil {
		return "", fmt.Errorf("invalid Skill contract snapshot: %w", err)
	}
	payload, err := json.Marshal(contract)
	if err != nil {
		return "", fmt.Errorf("encode Skill contract snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	snapshot := skillContractExternalSnapshot{
		Schema: skillExternalSnapshotSchema, Kind: skillExternalSnapshotKind,
		TenantID: principal.TenantID, UserID: principal.UserID, StableID: stableID,
		PayloadDigest: hex.EncodeToString(digest[:]), PreImage: payload,
		CapturedAt: capturedAt.UTC().Format(time.RFC3339Nano), RequestID: strings.TrimSpace(requestID),
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode Skill contract snapshot envelope: %w", err)
	}
	return string(encoded), nil
}

// recoverAgentSkillInstallCompensations replays only AgentService-owned
// directory transactions. AgentService's registry is derived from the
// filesystem, so restoring the serialized config snapshot is intentionally a
// no-op; the shared recovery routine still restores directories and removes
// only transaction-owned artifacts. A non-empty pending count is an admission
// failure for future writes, while read-only listing remains available.
func (s *Service) recoverAgentSkillInstallCompensations() (recovered int, pending int, err error) {
	if s == nil {
		return 0, 0, fmt.Errorf("service is unavailable")
	}
	p := skill.NewEvolutionPipeline()
	// Keep the cleanup seam attached to scoped recovery as well as the import
	// path. Production leaves this nil; tests use it to prove that repeated
	// post-commit cleanup failures escalate to needs_review without rolling back
	// the already-audited directory.
	if s.skillPostCommitCleanup != nil {
		p.CommittedCleanup = func(skill.EvolutionCompensationRecord) error {
			return s.skillPostCommitCleanup()
		}
	}
	return p.RecoverPendingCompensationsForActionPrefixAndScopeWithExternalRecovery(
		"agentservice_install", s.dataRoot, s.restoreSkillExternalCompensation)
}

// restoreSkillExternalCompensation restores principal-scoped dynamic Skill
// contracts captured by an interrupted directory transaction. Malformed keys
// or snapshots fail closed instead of being silently ignored.
func (s *Service) restoreSkillExternalCompensation(record skill.EvolutionCompensationRecord) error {
	if s == nil || s.dynamicCapabilities == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	type restoreCandidate struct {
		principal Principal
		stableID  string
		contract  DynamicCapabilityContract
	}
	keys := make([]string, 0, len(record.ExternalSnapshots))
	for key := range record.ExternalSnapshots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidates := make([]restoreCandidate, 0, len(keys))
	// Validate every external snapshot before publishing any contract. A
	// malformed/ambiguous entry is a fail-closed integrity violation and must
	// not produce a partially restored contract set merely because map
	// iteration reached a valid sibling first.
	for _, key := range keys {
		encoded := record.ExternalSnapshots[key]
		principal, stableID, parseErr := parseSkillExternalSnapshotKey(key)
		if parseErr != nil {
			return parseErr
		}
		if !skillCompensationPathBelongsToPrincipal(record, s.dataRoot, principal.TenantID, principal.UserID) {
			return fmt.Errorf("external Skill contract snapshot is outside principal scope")
		}
		var envelope skillContractExternalSnapshot
		if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
			return fmt.Errorf("decode external Skill contract snapshot: %w", err)
		}
		legacyRaw := envelope.Schema == "" && envelope.Kind == "" && len(envelope.PreImage) == 0
		if legacyRaw {
			// Records written before the envelope existed are accepted only as a
			// one-way compatibility read. They still require a valid contract,
			// principal-bound key and scope-checked durable paths; new writes always
			// use the versioned envelope below.
			envelope.PreImage = json.RawMessage(encoded)
		} else if envelope.Schema != skillExternalSnapshotSchema || envelope.Kind != skillExternalSnapshotKind || envelope.TenantID != principal.TenantID || envelope.UserID != principal.UserID || envelope.StableID != stableID {
			return fmt.Errorf("external Skill contract snapshot identity/schema mismatch")
		}
		if strings.TrimSpace(envelope.RequestID) != "" && strings.TrimSpace(record.RequestID) != "" && envelope.RequestID != record.RequestID {
			return fmt.Errorf("external Skill contract snapshot request mismatch")
		}
		if !legacyRaw && strings.TrimSpace(envelope.PayloadDigest) == "" {
			return fmt.Errorf("external Skill contract snapshot digest is missing")
		}
		digest := sha256.Sum256(envelope.PreImage)
		if !legacyRaw && !strings.EqualFold(envelope.PayloadDigest, hex.EncodeToString(digest[:])) {
			return fmt.Errorf("external Skill contract snapshot digest mismatch")
		}
		var contract DynamicCapabilityContract
		if err := json.Unmarshal(envelope.PreImage, &contract); err != nil {
			return fmt.Errorf("decode external Skill contract pre-image: %w", err)
		}
		if err := contract.validate(); err != nil {
			return fmt.Errorf("validate external Skill contract pre-image: %w", err)
		}
		candidates = append(candidates, restoreCandidate{principal: principal, stableID: stableID, contract: contract})
	}
	var restoreErrs []error
	for _, candidate := range candidates {
		if err := s.dynamicCapabilities.PublishSkillContract(candidate.principal, candidate.stableID, candidate.contract); err != nil {
			// Preserve the queue as pending by returning a joined error, but do
			// not leave unrelated verified snapshots revoked merely because one
			// external provider is temporarily unavailable.
			restoreErrs = append(restoreErrs, fmt.Errorf("restore external Skill contract %q: %w", candidate.stableID, err))
		}
	}
	return errors.Join(restoreErrs...)
}

func parseSkillExternalSnapshotKey(key string) (Principal, string, error) {
	parts := strings.SplitN(strings.TrimSpace(key), "|", 4)
	if len(parts) != 4 || parts[0] != "skill_contract" || !validRecoveryScopeSegment(parts[1]) || !validRecoveryScopeSegment(parts[2]) || strings.TrimSpace(parts[3]) == "" {
		return Principal{}, "", fmt.Errorf("invalid external Skill contract snapshot key")
	}
	return Principal{TenantID: parts[1], UserID: parts[2]}, parts[3], nil
}

func validRecoveryScopeSegment(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\|`)
}

func skillCompensationPathBelongsToPrincipal(record skill.EvolutionCompensationRecord, dataRoot, tenantID, userID string) bool {
	root := filepath.Clean(strings.TrimSpace(dataRoot))
	if root == "." || !validRecoveryScopeSegment(tenantID) || !validRecoveryScopeSegment(userID) {
		return false
	}
	principalRoot := filepath.Join(root, "tenants", tenantID, "users", userID)
	paths := []string{record.YAMLPath, record.DraftPath, record.DirPath, record.DirBackupPath}
	for _, move := range record.DirectoryMoves {
		paths = append(paths, move.OriginalPath, move.BackupPath)
	}
	paths = append(paths, record.CreatedDirs...)
	seen := false
	for _, candidate := range paths {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if strings.TrimSpace(candidate) == "" || candidate == "." {
			continue
		}
		seen = true
		if !pathWithinRoot(candidate, principalRoot) {
			continue
		}
	}
	if !seen {
		return false
	}
	for _, candidate := range paths {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if strings.TrimSpace(candidate) == "" || candidate == "." {
			continue
		}
		if !pathWithinRoot(candidate, principalRoot) {
			return false
		}
	}
	return true
}

func pathWithinRoot(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// hasPendingSkillCompensation is a fail-closed admission check for runtime
// operations. The queue is process-global, so headless services must scope
// the check to their own data root before executing or uploading a Skill.
func (s *Service) hasPendingSkillCompensation(skillName string) bool {
	if s == nil {
		return true
	}
	s.skillInstallTxnMu.Lock()
	defer s.skillInstallTxnMu.Unlock()
	return s.hasPendingSkillCompensationLocked(skillName)
}

func (s *Service) hasPendingSkillCompensationLocked(skillName string) bool {
	if s == nil {
		return true
	}
	// Startup recovery failure means the service cannot prove filesystem
	// consistency. Retry recovery at the next admission check; a pending,
	// malformed, or out-of-scope record remains fail-closed.
	if s.skillInstallRecoveryBlocked {
		s.skillInstallRecoveryBlocked = false
		if _, pending, err := s.recoverAgentSkillInstallCompensations(); err != nil || pending > 0 {
			s.skillInstallRecoveryBlocked = true
			return true
		}
	}
	p := &skill.EvolutionPipeline{}
	return p.HasPendingCompensationForScope(skillName, s.dataRoot)
}

// DynamicCapabilityContracts returns the read-only Service-owned contract
// resolver for runtime bridges. Publication is intentionally unavailable from
// this surface; an authenticated lifecycle host must construct a
// DynamicCapabilityContractPublisher with its reviewed capability registry.
func (s *Service) DynamicCapabilityContracts() DynamicCapabilityContractResolver {
	if s == nil {
		return nil
	}
	return s.dynamicCapabilities
}

// revokeMCPServerDynamicContracts is used by the authenticated MCP lifecycle
// path before a server is changed or removed. It deliberately revokes every
// tool binding for that server: a server-level endpoint, credential, command,
// or environment change invalidates the trusted observation beneath it.
func (s *Service) revokeMCPServerDynamicContracts(p Principal, serverID string) error {
	if s == nil || s.dynamicCapabilities == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := s.dynamicCapabilities.RevokeMCPServerContracts(p, serverID); err != nil {
		return fmt.Errorf("revoke MCP dynamic capability contracts: %w", err)
	}
	return nil
}

// revokeSkillDynamicContract is the corresponding lifecycle fence for one
// immutable Skill identity. It is called before replacement/deletion so a
// failed filesystem transition is conservative (unavailable), never stale.
func (s *Service) revokeSkillDynamicContract(p Principal, stableID string) error {
	if s == nil || s.dynamicCapabilities == nil {
		return fmt.Errorf("dynamic capability registry is unavailable")
	}
	if err := s.dynamicCapabilities.RevokeSkillContract(p, stableID); err != nil {
		return fmt.Errorf("revoke Skill dynamic capability contract: %w", err)
	}
	return nil
}

// ConfigureDynamicSemanticRouting attaches the durable Core Agent semantic
// execution boundary for one or more governed dynamic capability families.
// The caller remains responsible for supplying the reviewed capability
// registry and request-level resolver; Service only owns the restart-safe
// state stores and host-local signing key.
func (s *Service) ConfigureDynamicSemanticRouting(registry *coretool.CapabilityRegistry, resolver DynamicCapabilityNeedResolver, policy DynamicCapabilityPolicyAdapter, ttl time.Duration, coordinators ...DynamicExternalEffectCoordinator) error {
	if s == nil {
		return fmt.Errorf("service is unavailable")
	}
	configurer, ok := s.executor.(interface {
		SetDynamicSemanticRouting(DynamicSemanticRouting) error
	})
	if !ok {
		return fmt.Errorf("executor does not support dynamic semantic routing")
	}
	s.dynamicSemanticMu.Lock()
	defer s.dynamicSemanticMu.Unlock()
	if s.dynamicSemantic == nil {
		resources, err := OpenDynamicSemanticRoutingResources(s.dataRoot)
		if err != nil {
			return err
		}
		s.dynamicSemantic = resources
	}
	if len(coordinators) == 0 {
		// The semantic coordinator owns operation, receipt, host-call and plan
		// state together. A provider's synchronous text response therefore
		// remains awaiting_receipt until a trusted integration settles it.
		coordinators = []DynamicExternalEffectCoordinator{LedgerDynamicExternalEffectCoordinator{SemanticCoordinator: s.dynamicSemantic.coordinator}}
	}
	routing, err := s.dynamicSemantic.Routing(registry, resolver, policy, ttl, coordinators...)
	if err != nil {
		return err
	}
	if err := configurer.SetDynamicSemanticRouting(routing); err != nil {
		return err
	}
	// Route publication and continuity projection share the same durable
	// coordinator, but projection is deliberately asynchronous. Start the
	// consumer only after the executor has accepted the routing configuration;
	// it can then drain rows left by a previous process without making a model
	// request depend on best-effort projection latency.
	if err := s.dynamicSemantic.StartContinuityProjectionWorker(context.Background(), func(format string, args ...interface{}) {
		log.Printf("[semantic-continuity] "+format, args...)
	}); err != nil {
		return fmt.Errorf("start semantic continuity projector: %w", err)
	}
	if setter, ok := s.executor.(interface {
		SetReviewedHostAuditReader(reviewedHostAuditReader)
	}); ok {
		setter.SetReviewedHostAuditReader(serviceReviewedHostAuditReader{svc: s})
	}
	if setter, ok := s.executor.(interface {
		SetReviewedHostConfigManager(reviewedHostConfigManager)
	}); ok {
		setter.SetReviewedHostConfigManager(s)
	}
	return nil
}

// ReconcileDynamicEffectReceiptSource is the trusted host entry point for one
// binding-specific provider/channel receipt integration. It re-derives the
// settlement-only routing view from the durable resources on every call: the
// worker that drives it holds no grants, adapter names, model call IDs, or
// dispatch closures, and an observation can never create permission to invoke
// a provider.
func (s *Service) ReconcileDynamicEffectReceiptSource(ctx context.Context, source DynamicEffectReceiptSource) error {
	if s == nil {
		return fmt.Errorf("service is unavailable")
	}
	s.dynamicSemanticMu.Lock()
	resources := s.dynamicSemantic
	s.dynamicSemanticMu.Unlock()
	if resources == nil {
		return fmt.Errorf("dynamic semantic routing is not configured")
	}
	resources.mu.Lock()
	routing := DynamicSemanticRouting{
		ExecutionStore: resources.executionStore, RouteState: resources.routeState,
		EffectCoordinator: resources.effectCoordinator,
	}
	resources.mu.Unlock()
	return routing.ReconcileDynamicEffectReceiptSource(ctx, source)
}

// ExpireDynamicEffectReceiptWaits converges operations that have waited past
// the receipt lease from awaiting_receipt to unknown, so that an effect nobody
// will ever confirm stops occupying a state nothing can leave. Hosts drive it
// from the receipt worker loop.
func (s *Service) ExpireDynamicEffectReceiptWaits(context.Context) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("service is unavailable")
	}
	s.dynamicSemanticMu.Lock()
	resources := s.dynamicSemantic
	s.dynamicSemanticMu.Unlock()
	if resources == nil {
		return 0, nil
	}
	resources.mu.Lock()
	coordinator := resources.coordinator
	resources.mu.Unlock()
	if coordinator == nil {
		return 0, nil
	}
	return coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC(), coretool.ExternalEffectReceiptLease)
}

// ResolveUnknownDynamicEffect is the trusted host entry point for an
// operator's out-of-band verdict on an operation that ended unknown. Like
// receipt reconciliation it re-derives a settlement-only routing view, so the
// caller holds no grants, no adapter names and no dispatch closure: the most
// this path can do is write down a finding.
//
// Authenticating the operator and deciding who is allowed to make such a
// finding belongs to the host. Service only guarantees that whoever it was is
// recorded alongside the verdict.
func (s *Service) ResolveUnknownDynamicEffect(resolution DynamicSemanticManualResolution) error {
	if s == nil {
		return fmt.Errorf("service is unavailable")
	}
	s.dynamicSemanticMu.Lock()
	resources := s.dynamicSemantic
	s.dynamicSemanticMu.Unlock()
	if resources == nil {
		return fmt.Errorf("dynamic semantic routing is not configured")
	}
	resources.mu.Lock()
	routing := DynamicSemanticRouting{
		ExecutionStore: resources.executionStore, RouteState: resources.routeState,
		EffectCoordinator: resources.effectCoordinator,
	}
	resources.mu.Unlock()
	return routing.ResolveUnknownDynamicSemanticExternalEffect(resolution)
}
