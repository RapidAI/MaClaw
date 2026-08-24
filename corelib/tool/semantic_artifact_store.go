package tool

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ArtifactRef is the durable, scope-bound handle that may cross a ToolPlan
// edge. It never contains a provider path, credential, or model-writable
// location. Content is only returned to the trusted executor after it has
// matched both the invocation scope and the consuming artifact contract.
type ArtifactRef struct {
	ID                string
	Kind              string
	MIMEType          string
	IntegrityDigest   string
	ProducerSelection string
	Scope             InvocationScope
	CreatedAt         time.Time
}

// ArtifactPayload is executor-private content associated with an ArtifactRef.
// It must never be rendered into an LLM tool definition or passed through a
// model-controlled argument. Base64 is temporary compatibility for the IM
// gateway, which still accepts image bytes as an ImageKey payload.
type ArtifactPayload struct {
	Ref    ArtifactRef
	Base64 string
}

// ArtifactAccessGrant is an opaque, one-time capability to consume exactly one
// ArtifactRef from exactly one selected plan node. The model never sees its
// token and therefore cannot turn a same-scope artifact lookup into a free
// read primitive.
type ArtifactAccessGrant struct {
	Token               string
	ArtifactID          string
	ConsumerSelectionID string
	ContractDigest      string
	Scope               InvocationScope
	// SourceScope identifies the immutable payload owner. It may differ from
	// Scope only when a trusted RouteState projection authorized a new child
	// scope grant. It is never model controlled.
	SourceScope InvocationScope
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

// DeliveryState distinguishes an executor hand-off from the outcome observed
// by a channel adapter. In particular, a ToolPlan selection can prepare a
// delivery but it cannot claim that a remote channel accepted it: only the
// adapter which performs the upload is allowed to advance the record.
type DeliveryState string

const (
	DeliveryPrepared DeliveryState = "prepared"
	// DeliveryDispatching is an exclusive, durable send lease held by a trusted
	// channel adapter. A prepared record is an outbox intent, not permission for
	// every response projection to call a remote API. A stale lease becomes
	// unknown during recovery rather than being replayed.
	DeliveryDispatching DeliveryState = "dispatching"
	// DeliveryAccepted means the channel adapter received a successful response
	// for the upload/send operation. It is not a claim that a human recipient
	// has read the message.
	DeliveryAccepted DeliveryState = "accepted"
	// DeliveryFailed means the adapter established that no channel send was
	// accepted (for example, a local preflight rejection before any upload).
	// Ambiguous network outcomes use DeliveryUnknown instead.
	DeliveryFailed DeliveryState = "failed"
	// DeliveryUnknown records an ambiguous transport outcome (for example a
	// timeout after the request may have reached the channel). It deliberately
	// prevents automatic replay of an external effect.
	DeliveryUnknown DeliveryState = "unknown"
)

type DeliveryRecord struct {
	Scope       InvocationScope
	SelectionID string
	ArtifactID  string
	// ArtifactSourceScope is immutable payload ownership metadata. It is
	// normally Scope; a projected child delivery records the authorized parent
	// source without copying payload bytes into the child scope.
	ArtifactSourceScope InvocationScope
	ChannelScope        string
	// DestinationID is a trusted channel-adapter identity (peer/group/etc.),
	// never a model-supplied argument. It completes the delivery idempotency
	// scope once adapters move from response projection to a durable outbox.
	DestinationID string
	// OperationKey is the stable logical external-effect identity. It is
	// computed by ArtifactStore from trusted scope, artifact provenance and
	// destination metadata; callers cannot nominate it. It deliberately
	// excludes a short-lived function token, plan revision and selection ID so
	// reconnecting or re-rendering a delivery cannot create a second send.
	OperationKey string
	// ReceiptDigest is a bounded digest of the trusted channel acceptance
	// receipt. The raw receipt remains with the channel integration; this is
	// sufficient for recovery/audit to tie a settled selection to evidence.
	ReceiptDigest string
	State         DeliveryState
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ArtifactStore is the execution-plane owner of durable payload references and
// prepared delivery hand-offs. The store has no provider discovery or model
// surface methods, so it cannot be used as a free artifact gateway.
type ArtifactStore interface {
	Publish(ArtifactPayload) (ArtifactRef, error)
	// PublishedArtifacts is a trusted recovery lookup, scoped to one immutable
	// producer selection. It is not an LLM-facing artifact search primitive.
	PublishedArtifacts(InvocationScope, string) ([]ArtifactRef, error)
	// IssueProjectedAccessGrant re-authorizes one immutable source ref for a
	// new scope. It validates the exact source payload and authority boundary;
	// callers obtain source from RouteState, never from model arguments.
	IssueProjectedAccessGrant(ArtifactRef, InvocationScope, string, ArtifactContract, time.Duration) (ArtifactAccessGrant, error)
	ConsumeAccessGrant(ArtifactAccessGrant, ArtifactContract) (ArtifactPayload, error)
	PrepareDelivery(DeliveryRecord) (DeliveryRecord, error)
	Delivery(InvocationScope, string) (DeliveryRecord, error)
	ClaimDeliveryDispatch(InvocationScope, string, time.Time) (DeliveryRecord, bool, error)
	RecordDeliveryOutcome(InvocationScope, string, DeliveryState) (DeliveryRecord, error)
	ReconcileStaleDeliveryDispatches(time.Time, time.Duration) (int, error)
}

var ErrArtifactNotFound = errors.New("artifact not found")
var ErrDeliveryNotFound = errors.New("delivery not found")
var ErrArtifactAccessNotFound = errors.New("artifact access grant not found")

func ValidateArtifactScope(scope InvocationScope) error {
	if strings.TrimSpace(scope.RootTaskID) == "" || strings.TrimSpace(scope.PlanID) == "" || strings.TrimSpace(scope.SessionID) == "" || strings.TrimSpace(scope.TurnID) == "" || strings.TrimSpace(scope.PrincipalID) == "" {
		return fmt.Errorf("artifact_scope_required")
	}
	return nil
}

func NewArtifactPayload(scope InvocationScope, producerSelection, kind, mimeType, contentBase64 string, now time.Time) (ArtifactPayload, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return ArtifactPayload{}, err
	}
	producerSelection = strings.TrimSpace(producerSelection)
	kind = strings.ToLower(strings.TrimSpace(kind))
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	contentBase64 = strings.TrimSpace(contentBase64)
	if producerSelection == "" || kind == "" || mimeType == "" || contentBase64 == "" {
		return ArtifactPayload{}, fmt.Errorf("artifact binding and content are required")
	}
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil || len(data) == 0 {
		return ArtifactPayload{}, fmt.Errorf("artifact content is not valid base64")
	}
	digestBytes := sha256.Sum256(data)
	digest := hex.EncodeToString(digestBytes[:])
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return ArtifactPayload{Ref: ArtifactRef{
		ID:                artifactRefID(scope, producerSelection, kind, mimeType, digest),
		Kind:              kind,
		MIMEType:          mimeType,
		IntegrityDigest:   digest,
		ProducerSelection: producerSelection,
		Scope:             scope,
		CreatedAt:         now.UTC(),
	}, Base64: contentBase64}, nil
}

func validateArtifactPayload(payload ArtifactPayload) error {
	if err := ValidateArtifactScope(payload.Ref.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(payload.Ref.ID) == "" || strings.TrimSpace(payload.Ref.Kind) == "" || strings.TrimSpace(payload.Ref.MIMEType) == "" || strings.TrimSpace(payload.Ref.IntegrityDigest) == "" || strings.TrimSpace(payload.Ref.ProducerSelection) == "" {
		return fmt.Errorf("artifact_ref_incomplete")
	}
	canonical, err := NewArtifactPayload(payload.Ref.Scope, payload.Ref.ProducerSelection, payload.Ref.Kind, payload.Ref.MIMEType, payload.Base64, payload.Ref.CreatedAt)
	if err != nil || canonical.Ref.IntegrityDigest != payload.Ref.IntegrityDigest || (canonical.Ref.ID != payload.Ref.ID && legacyArtifactRefID(payload.Ref.IntegrityDigest) != payload.Ref.ID) {
		return fmt.Errorf("artifact_integrity_invalid")
	}
	return nil
}

// artifactRefID is immutable provenance identity, not a content-address by
// itself. Content digest proves bytes; the reference also commits to the
// producing selection and full authority scope so two legitimate producers
// can publish equal bytes without collapsing their lineage.
func artifactRefID(scope InvocationScope, producerSelection, kind, mimeType, integrityDigest string) string {
	identity := strings.Join([]string{
		scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID,
		strings.TrimSpace(producerSelection), strings.ToLower(strings.TrimSpace(kind)), strings.ToLower(strings.TrimSpace(mimeType)), strings.TrimSpace(integrityDigest),
	}, "\x00")
	return "artifact:" + SchemaDigest([]byte(identity))[:32]
}

// legacyArtifactRefID keeps already-persisted local stores readable during the
// ID migration. New publication never creates this weaker content-only form;
// all new IDs bind producer provenance and scope through artifactRefID.
func legacyArtifactRefID(integrityDigest string) string {
	integrityDigest = strings.TrimSpace(integrityDigest)
	if len(integrityDigest) < 24 {
		return ""
	}
	return "artifact:" + integrityDigest[:24]
}

func artifactMatchesContract(ref ArtifactRef, contract ArtifactContract) bool {
	return strings.EqualFold(strings.TrimSpace(ref.Kind), strings.TrimSpace(contract.Kind)) && (strings.TrimSpace(contract.MIMEType) == "" || strings.EqualFold(strings.TrimSpace(ref.MIMEType), strings.TrimSpace(contract.MIMEType)))
}

func validateDeliveryRecord(record DeliveryRecord) error {
	if err := ValidateArtifactScope(record.Scope); err != nil {
		return err
	}
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if err := ValidateArtifactScope(record.ArtifactSourceScope); err != nil || !artifactSourceMayProject(record.ArtifactSourceScope, record.Scope) {
		return fmt.Errorf("delivery_artifact_scope_invalid")
	}
	if strings.TrimSpace(record.SelectionID) == "" || strings.TrimSpace(record.ArtifactID) == "" || strings.TrimSpace(record.ChannelScope) == "" || strings.TrimSpace(record.DestinationID) == "" || record.State != DeliveryPrepared {
		return fmt.Errorf("delivery_record_invalid")
	}
	return nil
}

// deliveryOperationKey identifies one externally visible delivery independent
// of a model-facing selection token. The target is supplied only by a trusted
// channel adapter; no model argument, adapter name or callback-local payload
// participates in this identity.
func deliveryOperationKey(record DeliveryRecord, artifact ArtifactRef) string {
	identity := strings.Join([]string{
		strings.TrimSpace(record.Scope.RootTaskID), strings.TrimSpace(record.Scope.SessionID), strings.TrimSpace(record.Scope.PrincipalID),
		strings.TrimSpace(record.ArtifactSourceScope.RootTaskID), strings.TrimSpace(record.ArtifactSourceScope.PlanID), strings.TrimSpace(record.ArtifactSourceScope.SessionID), strings.TrimSpace(record.ArtifactSourceScope.TurnID), strings.TrimSpace(record.ArtifactSourceScope.PrincipalID),
		strings.TrimSpace(artifact.ID), strings.TrimSpace(artifact.IntegrityDigest), strings.ToLower(strings.TrimSpace(record.ChannelScope)), strings.TrimSpace(record.DestinationID),
	}, "\x00")
	return "delivery:" + SchemaDigest([]byte(identity))[:32]
}

func validDeliveryOutcome(state DeliveryState) bool {
	return state == DeliveryAccepted || state == DeliveryFailed || state == DeliveryUnknown
}

// DeliveryDispatchLease is conservative by design. Lease expiry never proves
// that a channel did not accept the request; recovery changes it to unknown.
const DeliveryDispatchLease = 5 * time.Minute

func validateArtifactAccessGrant(grant ArtifactAccessGrant) error {
	if err := ValidateArtifactScope(grant.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(grant.Token) == "" || strings.TrimSpace(grant.ArtifactID) == "" || strings.TrimSpace(grant.ConsumerSelectionID) == "" || strings.TrimSpace(grant.ContractDigest) == "" || grant.IssuedAt.IsZero() || grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(grant.IssuedAt) {
		return fmt.Errorf("artifact_access_grant_invalid")
	}
	if err := ValidateArtifactScope(grant.SourceScope); err != nil {
		return fmt.Errorf("artifact_access_grant_invalid")
	}
	return nil
}

func artifactSourceMayProject(source, destination InvocationScope) bool {
	return source.RootTaskID == destination.RootTaskID && source.SessionID == destination.SessionID && source.PrincipalID == destination.PrincipalID
}

func validateProjectedArtifact(source ArtifactRef, destination InvocationScope, contract ArtifactContract) error {
	if err := ValidateArtifactScope(source.Scope); err != nil {
		return err
	}
	if err := ValidateArtifactScope(destination); err != nil {
		return err
	}
	if !artifactSourceMayProject(source.Scope, destination) || !artifactMatchesContract(source, contract) {
		return fmt.Errorf("artifact_projection_invalid")
	}
	return nil
}

func artifactContractDigest(contract ArtifactContract) string {
	return SchemaDigest([]byte(strings.Join([]string{strings.ToLower(strings.TrimSpace(contract.Kind)), strings.ToLower(strings.TrimSpace(contract.MIMEType)), fmt.Sprintf("%t", contract.Required)}, "\x00")))
}

type memoryArtifactStore struct {
	mu             sync.Mutex
	artifacts      map[string]ArtifactPayload
	deliveries     map[string]DeliveryRecord
	deliveryOps    map[string]string
	accessGrants   map[string]ArtifactAccessGrant
	accessConsumed map[string]bool
}

func NewMemoryArtifactStore() ArtifactStore {
	return &memoryArtifactStore{artifacts: make(map[string]ArtifactPayload), deliveries: make(map[string]DeliveryRecord), deliveryOps: make(map[string]string), accessGrants: make(map[string]ArtifactAccessGrant), accessConsumed: make(map[string]bool)}
}

func (s *memoryArtifactStore) Publish(payload ArtifactPayload) (ArtifactRef, error) {
	if err := validateArtifactPayload(payload); err != nil {
		return ArtifactRef{}, err
	}
	key := artifactStoreKey(payload.Ref.Scope, payload.Ref.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.artifacts[key]; ok {
		if !sameArtifactIdentity(existing.Ref, payload.Ref) || existing.Base64 != payload.Base64 {
			return ArtifactRef{}, fmt.Errorf("artifact_conflict")
		}
		return existing.Ref, nil
	}
	s.artifacts[key] = cloneArtifactPayload(payload)
	return payload.Ref, nil
}

func (s *memoryArtifactStore) PublishedArtifacts(scope InvocationScope, producerSelection string) ([]ArtifactRef, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return nil, err
	}
	producerSelection = strings.TrimSpace(producerSelection)
	if producerSelection == "" {
		return nil, fmt.Errorf("artifact_producer_selection_required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := make([]ArtifactRef, 0)
	for _, payload := range s.artifacts {
		if payload.Ref.Scope == scope && payload.Ref.ProducerSelection == producerSelection {
			refs = append(refs, payload.Ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].CreatedAt.Equal(refs[j].CreatedAt) {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].CreatedAt.Before(refs[j].CreatedAt)
	})
	return refs, nil
}

func (s *memoryArtifactStore) ConsumeAccessGrant(grant ArtifactAccessGrant, contract ArtifactContract) (ArtifactPayload, error) {
	if err := validateArtifactAccessGrant(grant); err != nil {
		return ArtifactPayload{}, err
	}
	if grant.ContractDigest != artifactContractDigest(contract) {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_contract_mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.accessGrants[grant.Token]
	if !ok || stored != grant {
		return ArtifactPayload{}, ErrArtifactAccessNotFound
	}
	if !time.Now().UTC().Before(stored.ExpiresAt) {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_grant_expired")
	}
	if s.accessConsumed[grant.Token] {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_grant_replayed")
	}
	payload, ok := s.artifacts[artifactStoreKey(stored.SourceScope, stored.ArtifactID)]
	if !ok || !artifactMatchesContract(payload.Ref, contract) {
		return ArtifactPayload{}, ErrArtifactNotFound
	}
	s.accessConsumed[grant.Token] = true
	return cloneArtifactPayload(payload), nil
}

func (s *memoryArtifactStore) IssueProjectedAccessGrant(source ArtifactRef, scope InvocationScope, consumerSelectionID string, contract ArtifactContract, ttl time.Duration) (ArtifactAccessGrant, error) {
	if err := validateProjectedArtifact(source, scope, contract); err != nil {
		return ArtifactAccessGrant{}, err
	}
	if strings.TrimSpace(consumerSelectionID) == "" || ttl <= 0 {
		return ArtifactAccessGrant{}, fmt.Errorf("artifact_access_grant_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.artifacts[artifactStoreKey(source.Scope, source.ID)]
	if !ok || !sameArtifactIdentity(payload.Ref, source) || !artifactMatchesContract(payload.Ref, contract) {
		return ArtifactAccessGrant{}, ErrArtifactNotFound
	}
	issuedAt := time.Now().UTC()
	grant := ArtifactAccessGrant{Token: newArtifactAccessToken(scope, consumerSelectionID, source.ID), ArtifactID: source.ID, ConsumerSelectionID: strings.TrimSpace(consumerSelectionID), ContractDigest: artifactContractDigest(contract), Scope: scope, SourceScope: source.Scope, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(ttl)}
	s.accessGrants[grant.Token] = grant
	return grant, nil
}

func (s *memoryArtifactStore) PrepareDelivery(record DeliveryRecord) (DeliveryRecord, error) {
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if err := validateDeliveryRecord(record); err != nil {
		return DeliveryRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	artifact, ok := s.artifacts[artifactStoreKey(record.ArtifactSourceScope, record.ArtifactID)]
	if !ok {
		return DeliveryRecord{}, ErrArtifactNotFound
	}
	record.OperationKey = deliveryOperationKey(record, artifact.Ref)
	key := deliveryStoreKey(record.Scope, record.SelectionID)
	if existing, ok := s.deliveries[key]; ok {
		if existing.ArtifactID != record.ArtifactID || existing.ArtifactSourceScope != record.ArtifactSourceScope || existing.ChannelScope != record.ChannelScope || existing.DestinationID != record.DestinationID || existing.OperationKey != record.OperationKey {
			return DeliveryRecord{}, fmt.Errorf("delivery_conflict")
		}
		return existing, nil
	}
	if existingKey, ok := s.deliveryOps[record.OperationKey]; ok {
		return s.deliveries[existingKey], nil
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt
	s.deliveries[key] = record
	s.deliveryOps[record.OperationKey] = key
	return record, nil
}

func (s *memoryArtifactStore) Delivery(scope InvocationScope, selectionID string) (DeliveryRecord, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.deliveries[deliveryStoreKey(scope, selectionID)]
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	return record, nil
}

func (s *memoryArtifactStore) ClaimDeliveryDispatch(scope InvocationScope, selectionID string, now time.Time) (DeliveryRecord, bool, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := deliveryStoreKey(scope, selectionID)
	record, ok := s.deliveries[key]
	if !ok {
		return DeliveryRecord{}, false, ErrDeliveryNotFound
	}
	if record.State != DeliveryPrepared {
		return record, false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record.State, record.UpdatedAt = DeliveryDispatching, now.UTC()
	s.deliveries[key] = record
	return record, true, nil
}

func (s *memoryArtifactStore) RecordDeliveryOutcome(scope InvocationScope, selectionID string, outcome DeliveryState) (DeliveryRecord, error) {
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	if !validDeliveryOutcome(outcome) {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := deliveryStoreKey(scope, selectionID)
	record, ok := s.deliveries[key]
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if record.State == outcome {
		return record, nil
	}
	if record.State != DeliveryDispatching {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
	}
	record.State = outcome
	record.UpdatedAt = time.Now().UTC()
	s.deliveries[key] = record
	return record, nil
}

func (s *memoryArtifactStore) ReconcileStaleDeliveryDispatches(now time.Time, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, fmt.Errorf("delivery dispatch maximum age must be positive")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.UTC().Add(-maxAge)
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := 0
	for key, record := range s.deliveries {
		if record.State != DeliveryDispatching || record.UpdatedAt.After(cutoff) {
			continue
		}
		record.State, record.UpdatedAt = DeliveryUnknown, now.UTC()
		s.deliveries[key] = record
		changed++
	}
	return changed, nil
}

// SQLiteArtifactStore is the restartable local implementation used by desktop
// semantic routing. Payloads remain local to the configured ~/.maclaw data
// root; no result table, log, or host-call journal stores them.
//
// Publish enforces a per-principal payload-byte quota (fail closed), and a
// host may inject an AEAD key so payloads are encrypted at rest; see
// semantic_artifact_encryption.go for the format and plaintext compatibility
// rules. The zero-value configuration keeps pre-existing behavior: plaintext
// storage with DefaultArtifactPrincipalQuotaBytes quota and no automatic
// deletion (SweepExpiredArtifacts requires an explicit retention).
type SQLiteArtifactStore struct {
	db *sql.DB
	// quotaBytes is the per-principal total decoded payload byte limit. <= 0
	// selects DefaultArtifactPrincipalQuotaBytes.
	quotaBytes int64
	// retention is the payload lifetime used by SweepExpiredArtifacts. <= 0
	// disables sweeping entirely; nothing is ever deleted implicitly.
	retention time.Duration
	// encryptionKey is the derived AEAD key, nil when the host did not inject
	// one (plaintext storage, legacy-compatible reads).
	encryptionKey []byte
	// hostEncryptionKey holds the injected key until init derives the AEAD key;
	// an invalid length fails store construction rather than silently storing
	// plaintext.
	hostEncryptionKey []byte
}

// DefaultArtifactPrincipalQuotaBytes bounds total retained payload bytes per
// principal. It is generous enough for interactive screenshot/document flows
// while still failing closed against unbounded model-driven publication.
const DefaultArtifactPrincipalQuotaBytes int64 = 512 << 20

// ArtifactStoreOption configures a SQLiteArtifactStore before its schema is
// initialized. Options are host-injected wiring; none is model reachable.
type ArtifactStoreOption func(*SQLiteArtifactStore)

// WithArtifactQuotaBytes sets the per-principal decoded payload byte quota.
// A non-positive value selects the default.
func WithArtifactQuotaBytes(bytes int64) ArtifactStoreOption {
	return func(s *SQLiteArtifactStore) { s.quotaBytes = bytes }
}

// WithArtifactRetention sets the payload retention period used by
// SweepExpiredArtifacts. A non-positive value disables sweeping.
func WithArtifactRetention(retention time.Duration) ArtifactStoreOption {
	return func(s *SQLiteArtifactStore) { s.retention = retention }
}

// WithArtifactEncryptionKey injects the host-owned 32-byte key from which the
// payload AEAD key is derived. The coordinator exposes the same wiring as
// WithCoordinatorArtifactEncryptionKey so agentservice can pass its data-root
// key in a later slice without touching this package's internals.
func WithArtifactEncryptionKey(key []byte) ArtifactStoreOption {
	return func(s *SQLiteArtifactStore) { s.hostEncryptionKey = append([]byte(nil), key...) }
}

func NewSQLiteArtifactStore(dbPath string, opts ...ArtifactStoreOption) (*SQLiteArtifactStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("artifact store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteArtifactStore{db: db}
	for _, opt := range opts {
		opt(store)
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteArtifactStore) init() error {
	if s.hostEncryptionKey != nil {
		derived, err := deriveArtifactEncryptionKey(s.hostEncryptionKey)
		if err != nil {
			return err
		}
		s.encryptionKey = derived
		s.hostEncryptionKey = nil
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS semantic_artifacts (
			artifact_key TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			artifact_id TEXT NOT NULL, kind TEXT NOT NULL, mime_type TEXT NOT NULL,
			integrity_digest TEXT NOT NULL, producer_selection TEXT NOT NULL, payload_base64 TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_artifacts_scope ON semantic_artifacts(root_task_id, plan_id, session_id, turn_id, principal_id, kind, mime_type, created_at)`,
		`CREATE TABLE IF NOT EXISTS semantic_delivery_preparations (
			delivery_key TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			selection_id TEXT NOT NULL, artifact_id TEXT NOT NULL, artifact_source_root_task_id TEXT NOT NULL DEFAULT '', artifact_source_plan_id TEXT NOT NULL DEFAULT '', artifact_source_session_id TEXT NOT NULL DEFAULT '', artifact_source_turn_id TEXT NOT NULL DEFAULT '', artifact_source_principal_id TEXT NOT NULL DEFAULT '', channel_scope TEXT NOT NULL, destination_id TEXT NOT NULL DEFAULT '', operation_key TEXT NOT NULL DEFAULT '',
			receipt_digest TEXT NOT NULL DEFAULT '', state TEXT NOT NULL CHECK(state IN ('prepared', 'dispatching', 'accepted', 'failed', 'unknown')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS semantic_artifact_access_grants (
			grant_token TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			artifact_id TEXT NOT NULL, consumer_selection_id TEXT NOT NULL, contract_digest TEXT NOT NULL,
			issued_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_artifact_access_expiry ON semantic_artifact_access_grants(expires_at)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	if err := s.upgradeArtifactAccessGrantTable(); err != nil {
		return err
	}
	if err := s.upgradeDeliveryTable(); err != nil {
		return err
	}
	if err := s.ensureDeliveryReceiptDigestColumn(); err != nil {
		return err
	}
	if err := s.ensurePayloadBytesColumn(); err != nil {
		return err
	}
	if err := s.ensureDeliveryFencingColumns(); err != nil {
		return err
	}
	_, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_semantic_delivery_operation ON semantic_delivery_preparations(operation_key) WHERE operation_key <> ''`)
	return err
}

// ensurePayloadBytesColumn adds the decoded-size accounting column used by the
// per-principal quota. Legacy rows are backfilled from the exact base64 length
// (including padding) so quota accounting is never undercounted; pre-existing
// databases remain fully readable.
func (s *SQLiteArtifactStore) ensurePayloadBytesColumn() error {
	_, err := s.db.Exec(`ALTER TABLE semantic_artifacts ADD COLUMN payload_bytes INTEGER NOT NULL DEFAULT -1`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = s.db.Exec(`UPDATE semantic_artifacts SET payload_bytes =
		3*(length(payload_base64)/4)
		- (substr(payload_base64, length(payload_base64), 1) = '=')
		- (substr(payload_base64, length(payload_base64)-1, 1) = '=')
		WHERE payload_bytes < 0`)
	return err
}

// ensureDeliveryFencingColumns adds the outbox fencing/claim-holder columns to
// the delivery ledger. Token 0 marks pre-fencing rows, which remain governed
// by the existing current-revision checks only.
func (s *SQLiteArtifactStore) ensureDeliveryFencingColumns() error {
	for _, statement := range []string{
		`ALTER TABLE semantic_delivery_preparations ADD COLUMN prepared_fencing_token INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE semantic_delivery_preparations ADD COLUMN claim_fencing_token INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE semantic_delivery_preparations ADD COLUMN claim_holder TEXT NOT NULL DEFAULT ''`,
		// Two different facts reach state='unknown' and the column alone
		// cannot tell them apart. A fire worker writes unknown as its final
		// word on a channel that issues no receipt at all; the dispatch lease
		// writes it because nobody was left to look. Only the second can be
		// answered later, so the origin has to be recorded rather than
		// guessed.
		`ALTER TABLE semantic_delivery_preparations ADD COLUMN unknown_origin TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := s.db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *SQLiteArtifactStore) ensureDeliveryReceiptDigestColumn() error {
	_, err := s.db.Exec(`ALTER TABLE semantic_delivery_preparations ADD COLUMN receipt_digest TEXT NOT NULL DEFAULT ''`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	return nil
}

func (s *SQLiteArtifactStore) upgradeArtifactAccessGrantTable() error {
	rows, err := s.db.Query(`PRAGMA table_info(semantic_artifact_access_grants)`)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		seen[strings.ToLower(name)] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []string{"source_root_task_id", "source_plan_id", "source_session_id", "source_turn_id", "source_principal_id"} {
		if !seen[column] {
			if _, err := s.db.Exec(`ALTER TABLE semantic_artifact_access_grants ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`UPDATE semantic_artifact_access_grants SET source_root_task_id=root_task_id, source_plan_id=plan_id, source_session_id=session_id, source_turn_id=turn_id, source_principal_id=principal_id WHERE source_root_task_id='' OR source_plan_id='' OR source_session_id='' OR source_turn_id='' OR source_principal_id=''`)
	if err != nil {
		return err
	}
	return nil
}

// upgradeDeliveryTable keeps pre-outcome databases usable. SQLite cannot
// widen a CHECK constraint in place, so the tiny delivery table is rebuilt in
// one transaction before an adapter ever attempts to record accepted/unknown.
func (s *SQLiteArtifactStore) upgradeDeliveryTable() error {
	var definition string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='semantic_delivery_preparations'`).Scan(&definition); err != nil {
		return err
	}
	lowerDefinition := strings.ToLower(definition)
	if strings.Contains(lowerDefinition, "'dispatching'") && strings.Contains(lowerDefinition, "'accepted'") && strings.Contains(lowerDefinition, "'failed'") && strings.Contains(lowerDefinition, "'unknown'") && strings.Contains(lowerDefinition, "destination_id") && strings.Contains(lowerDefinition, "operation_key") && strings.Contains(lowerDefinition, "artifact_source_root_task_id") {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`CREATE TABLE semantic_delivery_preparations_next (
			delivery_key TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
			session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			selection_id TEXT NOT NULL, artifact_id TEXT NOT NULL, artifact_source_root_task_id TEXT NOT NULL DEFAULT '', artifact_source_plan_id TEXT NOT NULL DEFAULT '', artifact_source_session_id TEXT NOT NULL DEFAULT '', artifact_source_turn_id TEXT NOT NULL DEFAULT '', artifact_source_principal_id TEXT NOT NULL DEFAULT '', channel_scope TEXT NOT NULL, destination_id TEXT NOT NULL DEFAULT '', operation_key TEXT NOT NULL DEFAULT '',
			receipt_digest TEXT NOT NULL DEFAULT '', state TEXT NOT NULL CHECK(state IN ('prepared', 'dispatching', 'accepted', 'failed', 'unknown')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`INSERT INTO semantic_delivery_preparations_next(delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, state, created_at, updated_at) SELECT delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, root_task_id, plan_id, session_id, turn_id, principal_id, channel_scope, '', state, created_at, updated_at FROM semantic_delivery_preparations`,
		`DROP TABLE semantic_delivery_preparations`,
		`ALTER TABLE semantic_delivery_preparations_next RENAME TO semantic_delivery_preparations`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX idx_semantic_delivery_operation ON semantic_delivery_preparations(operation_key) WHERE operation_key <> ''`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteArtifactStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteArtifactStore) Publish(payload ArtifactPayload) (ArtifactRef, error) {
	if s == nil || s.db == nil {
		return ArtifactRef{}, fmt.Errorf("artifact store is unavailable")
	}
	if err := validateArtifactPayload(payload); err != nil {
		return ArtifactRef{}, err
	}
	ref := payload.Ref
	key := artifactStoreKey(ref.Scope, ref.ID)
	// Idempotent republish of the exact same bytes never consumes quota.
	if existing, err := s.byID(ref.Scope, ref.ID); err == nil {
		if !sameArtifactIdentity(existing.Ref, ref) || existing.Base64 != payload.Base64 {
			return ArtifactRef{}, fmt.Errorf("artifact_conflict")
		}
		return existing.Ref, nil
	} else if !errors.Is(err, ErrArtifactNotFound) {
		return ArtifactRef{}, err
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Base64)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("artifact content is not valid base64")
	}
	if err := s.enforcePublishQuota(s.db, ref.Scope.PrincipalID, int64(len(decoded))); err != nil {
		return ArtifactRef{}, err
	}
	stored, err := s.encodeStoredPayload(key, payload.Base64)
	if err != nil {
		return ArtifactRef{}, err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO semantic_artifacts(artifact_key, root_task_id, plan_id, session_id, turn_id, principal_id, artifact_id, kind, mime_type, integrity_digest, producer_selection, payload_base64, payload_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, key, ref.Scope.RootTaskID, ref.Scope.PlanID, ref.Scope.SessionID, ref.Scope.TurnID, ref.Scope.PrincipalID, ref.ID, ref.Kind, ref.MIMEType, ref.IntegrityDigest, ref.ProducerSelection, stored, len(decoded), artifactStoreTime(ref.CreatedAt))
	if err != nil {
		return ArtifactRef{}, err
	}
	existing, err := s.byID(ref.Scope, ref.ID)
	if err != nil {
		return ArtifactRef{}, err
	}
	if !sameArtifactIdentity(existing.Ref, ref) || existing.Base64 != payload.Base64 {
		return ArtifactRef{}, fmt.Errorf("artifact_conflict")
	}
	return existing.Ref, nil
}

// artifactQuotaQuerier is satisfied by *sql.DB and *sql.Tx so the quota check
// can join the coordinator's atomic completion transaction.
type artifactQuotaQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// enforcePublishQuota fails closed when publishing additionalBytes for the
// principal would exceed the configured per-principal payload budget.
func (s *SQLiteArtifactStore) enforcePublishQuota(q artifactQuotaQuerier, principalID string, additionalBytes int64) error {
	quota := s.quotaBytes
	if quota <= 0 {
		quota = DefaultArtifactPrincipalQuotaBytes
	}
	var used int64
	if err := q.QueryRow(`SELECT COALESCE(SUM(payload_bytes), 0) FROM semantic_artifacts WHERE principal_id=?`, strings.TrimSpace(principalID)).Scan(&used); err != nil {
		return err
	}
	if additionalBytes < 0 || used+additionalBytes > quota {
		return fmt.Errorf("artifact_quota_exceeded")
	}
	return nil
}

func (s *SQLiteArtifactStore) encodeStoredPayload(artifactKey, plaintextBase64 string) (string, error) {
	return encodeStoredArtifactPayload(s.encryptionKey, artifactKey, plaintextBase64)
}

func (s *SQLiteArtifactStore) decodeStoredPayload(artifactKey, stored string) (string, error) {
	return decodeStoredArtifactPayload(s.encryptionKey, artifactKey, stored)
}

// SweepExpiredArtifacts deletes artifact rows (payload and metadata together)
// older than the configured retention. Full deletion is deliberate: the
// durable audit fact for a delivered artifact is its DeliveryRecord (artifact
// ID, operation key, receipt digest), which is never swept, so removing the
// payload row cannot erase delivery evidence. Rows still referenced by a
// prepared/dispatching delivery are kept regardless of age so an in-flight
// outbox intent never loses its payload. Consume/access-grant paths fail
// closed with artifact-not-found after a sweep, matching the semantics of an
// artifact that was never published.
func (s *SQLiteArtifactStore) SweepExpiredArtifacts(now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("artifact store is unavailable")
	}
	if s.retention <= 0 {
		return 0, fmt.Errorf("artifact_retention_disabled")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := artifactStoreTime(now.UTC().Add(-s.retention))
	result, err := s.db.Exec(`DELETE FROM semantic_artifacts WHERE created_at <= ? AND NOT EXISTS (
		SELECT 1 FROM semantic_delivery_preparations d
		WHERE d.state IN ('prepared','dispatching')
		AND d.artifact_id = semantic_artifacts.artifact_id
		AND d.artifact_source_root_task_id = semantic_artifacts.root_task_id
		AND d.artifact_source_plan_id = semantic_artifacts.plan_id
		AND d.artifact_source_session_id = semantic_artifacts.session_id
		AND d.artifact_source_turn_id = semantic_artifacts.turn_id
		AND d.artifact_source_principal_id = semantic_artifacts.principal_id
	)`, cutoff)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(changed), nil
}

func (s *SQLiteArtifactStore) PublishedArtifacts(scope InvocationScope, producerSelection string) ([]ArtifactRef, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("artifact store is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return nil, err
	}
	producerSelection = strings.TrimSpace(producerSelection)
	if producerSelection == "" {
		return nil, fmt.Errorf("artifact_producer_selection_required")
	}
	rows, err := s.db.Query(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, payload_base64, created_at FROM semantic_artifacts WHERE root_task_id=? AND plan_id=? AND session_id=? AND turn_id=? AND principal_id=? AND producer_selection=? ORDER BY created_at ASC, artifact_id ASC`, scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID, producerSelection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make([]ArtifactRef, 0)
	for rows.Next() {
		var payload ArtifactPayload
		var createdAt string
		if err := rows.Scan(&payload.Ref.ID, &payload.Ref.Kind, &payload.Ref.MIMEType, &payload.Ref.IntegrityDigest, &payload.Ref.ProducerSelection, &payload.Base64, &createdAt); err != nil {
			return nil, err
		}
		payload.Ref.Scope = scope
		payload.Ref.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		decoded, err := s.decodeStoredPayload(artifactStoreKey(scope, payload.Ref.ID), payload.Base64)
		if err != nil {
			return nil, err
		}
		payload.Base64 = decoded
		if err := validateArtifactPayload(payload); err != nil {
			return nil, fmt.Errorf("artifact_store_corrupt")
		}
		refs = append(refs, payload.Ref)
	}
	return refs, rows.Err()
}

func (s *SQLiteArtifactStore) IssueProjectedAccessGrant(source ArtifactRef, scope InvocationScope, consumerSelectionID string, contract ArtifactContract, ttl time.Duration) (ArtifactAccessGrant, error) {
	if s == nil || s.db == nil {
		return ArtifactAccessGrant{}, fmt.Errorf("artifact store is unavailable")
	}
	if err := validateProjectedArtifact(source, scope, contract); err != nil {
		return ArtifactAccessGrant{}, err
	}
	if strings.TrimSpace(consumerSelectionID) == "" || ttl <= 0 {
		return ArtifactAccessGrant{}, fmt.Errorf("artifact_access_grant_invalid")
	}
	payload, err := s.byID(source.Scope, source.ID)
	if err != nil || !sameArtifactIdentity(payload.Ref, source) || !artifactMatchesContract(payload.Ref, contract) {
		if errors.Is(err, ErrArtifactNotFound) {
			return ArtifactAccessGrant{}, ErrArtifactNotFound
		}
		if err != nil {
			return ArtifactAccessGrant{}, err
		}
		return ArtifactAccessGrant{}, ErrArtifactNotFound
	}
	issuedAt := time.Now().UTC()
	grant := ArtifactAccessGrant{Token: newArtifactAccessToken(scope, consumerSelectionID, source.ID), ArtifactID: source.ID, ConsumerSelectionID: strings.TrimSpace(consumerSelectionID), ContractDigest: artifactContractDigest(contract), Scope: scope, SourceScope: source.Scope, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(ttl)}
	if err := s.insertAccessGrant(grant); err != nil {
		return ArtifactAccessGrant{}, err
	}
	return grant, nil
}

func (s *SQLiteArtifactStore) insertAccessGrant(grant ArtifactAccessGrant) error {
	_, err := s.db.Exec(`INSERT INTO semantic_artifact_access_grants(grant_token, root_task_id, plan_id, session_id, turn_id, principal_id, source_root_task_id, source_plan_id, source_session_id, source_turn_id, source_principal_id, artifact_id, consumer_selection_id, contract_digest, issued_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, grant.Token, grant.Scope.RootTaskID, grant.Scope.PlanID, grant.Scope.SessionID, grant.Scope.TurnID, grant.Scope.PrincipalID, grant.SourceScope.RootTaskID, grant.SourceScope.PlanID, grant.SourceScope.SessionID, grant.SourceScope.TurnID, grant.SourceScope.PrincipalID, grant.ArtifactID, grant.ConsumerSelectionID, grant.ContractDigest, artifactStoreTime(grant.IssuedAt), artifactStoreTime(grant.ExpiresAt))
	return err
}

func (s *SQLiteArtifactStore) ConsumeAccessGrant(grant ArtifactAccessGrant, contract ArtifactContract) (ArtifactPayload, error) {
	if s == nil || s.db == nil {
		return ArtifactPayload{}, fmt.Errorf("artifact store is unavailable")
	}
	if err := validateArtifactAccessGrant(grant); err != nil {
		return ArtifactPayload{}, err
	}
	if grant.ContractDigest != artifactContractDigest(contract) {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_contract_mismatch")
	}
	result, err := s.db.Exec(`UPDATE semantic_artifact_access_grants SET consumed_at=? WHERE grant_token=? AND root_task_id=? AND plan_id=? AND session_id=? AND turn_id=? AND principal_id=? AND source_root_task_id=? AND source_plan_id=? AND source_session_id=? AND source_turn_id=? AND source_principal_id=? AND artifact_id=? AND consumer_selection_id=? AND contract_digest=? AND consumed_at='' AND expires_at>?`, artifactStoreTime(time.Now().UTC()), grant.Token, grant.Scope.RootTaskID, grant.Scope.PlanID, grant.Scope.SessionID, grant.Scope.TurnID, grant.Scope.PrincipalID, grant.SourceScope.RootTaskID, grant.SourceScope.PlanID, grant.SourceScope.SessionID, grant.SourceScope.TurnID, grant.SourceScope.PrincipalID, grant.ArtifactID, grant.ConsumerSelectionID, grant.ContractDigest, artifactStoreTime(time.Now().UTC()))
	if err != nil {
		return ArtifactPayload{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ArtifactPayload{}, err
	}
	if changed == 0 {
		return s.artifactAccessFailure(grant)
	}
	payload, err := s.byID(grant.SourceScope, grant.ArtifactID)
	if err != nil || !artifactMatchesContract(payload.Ref, contract) {
		return ArtifactPayload{}, ErrArtifactNotFound
	}
	return payload, nil
}

func (s *SQLiteArtifactStore) artifactAccessFailure(grant ArtifactAccessGrant) (ArtifactPayload, error) {
	var expiresAt, consumedAt string
	err := s.db.QueryRow(`SELECT expires_at, consumed_at FROM semantic_artifact_access_grants WHERE grant_token=?`, grant.Token).Scan(&expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactPayload{}, ErrArtifactAccessNotFound
	}
	if err != nil {
		return ArtifactPayload{}, err
	}
	if strings.TrimSpace(consumedAt) != "" {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_grant_replayed")
	}
	expires, parseErr := time.Parse(time.RFC3339Nano, expiresAt)
	if parseErr != nil || !time.Now().UTC().Before(expires) {
		return ArtifactPayload{}, fmt.Errorf("artifact_access_grant_expired")
	}
	return ArtifactPayload{}, ErrArtifactAccessNotFound
}

func (s *SQLiteArtifactStore) PrepareDelivery(record DeliveryRecord) (DeliveryRecord, error) {
	if s == nil || s.db == nil {
		return DeliveryRecord{}, fmt.Errorf("artifact store is unavailable")
	}
	if record.ArtifactSourceScope == (InvocationScope{}) {
		record.ArtifactSourceScope = record.Scope
	}
	if err := validateDeliveryRecord(record); err != nil {
		return DeliveryRecord{}, err
	}
	artifact, err := s.byID(record.ArtifactSourceScope, record.ArtifactID)
	if err != nil {
		if errors.Is(err, ErrArtifactNotFound) {
			return DeliveryRecord{}, ErrArtifactNotFound
		}
		return DeliveryRecord{}, err
	}
	record.OperationKey = deliveryOperationKey(record, artifact.Ref)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt
	_, err = s.db.Exec(`INSERT OR IGNORE INTO semantic_delivery_preparations(delivery_key, root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, deliveryStoreKey(record.Scope, record.SelectionID), record.Scope.RootTaskID, record.Scope.PlanID, record.Scope.SessionID, record.Scope.TurnID, record.Scope.PrincipalID, record.SelectionID, record.ArtifactID, record.ArtifactSourceScope.RootTaskID, record.ArtifactSourceScope.PlanID, record.ArtifactSourceScope.SessionID, record.ArtifactSourceScope.TurnID, record.ArtifactSourceScope.PrincipalID, record.ChannelScope, record.DestinationID, record.OperationKey, record.State, artifactStoreTime(record.CreatedAt), artifactStoreTime(record.UpdatedAt))
	if err != nil {
		return DeliveryRecord{}, err
	}
	existing, err := s.Delivery(record.Scope, record.SelectionID)
	if errors.Is(err, ErrDeliveryNotFound) {
		existing, err = s.deliveryByOperationKey(record.OperationKey)
	}
	if err != nil {
		return DeliveryRecord{}, err
	}
	if existing.ArtifactID != record.ArtifactID || existing.ArtifactSourceScope != record.ArtifactSourceScope || existing.ChannelScope != record.ChannelScope || existing.DestinationID != record.DestinationID || existing.OperationKey != record.OperationKey {
		return DeliveryRecord{}, fmt.Errorf("delivery_conflict")
	}
	return existing, nil
}

func (s *SQLiteArtifactStore) Delivery(scope InvocationScope, selectionID string) (DeliveryRecord, error) {
	if s == nil || s.db == nil {
		return DeliveryRecord{}, fmt.Errorf("artifact store is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	var record DeliveryRecord
	var created, updated string
	err := s.db.QueryRow(`SELECT artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, receipt_digest, state, created_at, updated_at FROM semantic_delivery_preparations WHERE delivery_key=?`, deliveryStoreKey(scope, selectionID)).Scan(&record.ArtifactID, &record.ArtifactSourceScope.RootTaskID, &record.ArtifactSourceScope.PlanID, &record.ArtifactSourceScope.SessionID, &record.ArtifactSourceScope.TurnID, &record.ArtifactSourceScope.PrincipalID, &record.ChannelScope, &record.DestinationID, &record.OperationKey, &record.ReceiptDigest, &record.State, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if err != nil {
		return DeliveryRecord{}, err
	}
	record.Scope, record.SelectionID = scope, strings.TrimSpace(selectionID)
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, nil
}

func (s *SQLiteArtifactStore) ClaimDeliveryDispatch(scope InvocationScope, selectionID string, now time.Time) (DeliveryRecord, bool, error) {
	if s == nil || s.db == nil {
		return DeliveryRecord{}, false, fmt.Errorf("artifact store is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, false, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.Exec(`UPDATE semantic_delivery_preparations SET state=?, updated_at=? WHERE delivery_key=? AND state=?`, DeliveryDispatching, artifactStoreTime(now), deliveryStoreKey(scope, selectionID), DeliveryPrepared)
	if err != nil {
		return DeliveryRecord{}, false, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return DeliveryRecord{}, false, err
	} else if changed == 1 {
		record, err := s.Delivery(scope, selectionID)
		return record, true, err
	}
	record, err := s.Delivery(scope, selectionID)
	return record, false, err
}

func (s *SQLiteArtifactStore) deliveryByOperationKey(operationKey string) (DeliveryRecord, error) {
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	var record DeliveryRecord
	var created, updated string
	err := s.db.QueryRow(`SELECT root_task_id, plan_id, session_id, turn_id, principal_id, selection_id, artifact_id, artifact_source_root_task_id, artifact_source_plan_id, artifact_source_session_id, artifact_source_turn_id, artifact_source_principal_id, channel_scope, destination_id, operation_key, receipt_digest, state, created_at, updated_at FROM semantic_delivery_preparations WHERE operation_key=?`, operationKey).Scan(&record.Scope.RootTaskID, &record.Scope.PlanID, &record.Scope.SessionID, &record.Scope.TurnID, &record.Scope.PrincipalID, &record.SelectionID, &record.ArtifactID, &record.ArtifactSourceScope.RootTaskID, &record.ArtifactSourceScope.PlanID, &record.ArtifactSourceScope.SessionID, &record.ArtifactSourceScope.TurnID, &record.ArtifactSourceScope.PrincipalID, &record.ChannelScope, &record.DestinationID, &record.OperationKey, &record.ReceiptDigest, &record.State, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if err != nil {
		return DeliveryRecord{}, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	record.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return record, nil
}

func (s *SQLiteArtifactStore) RecordDeliveryOutcome(scope InvocationScope, selectionID string, outcome DeliveryState) (DeliveryRecord, error) {
	if s == nil || s.db == nil {
		return DeliveryRecord{}, fmt.Errorf("artifact store is unavailable")
	}
	if err := ValidateArtifactScope(scope); err != nil {
		return DeliveryRecord{}, err
	}
	if !validDeliveryOutcome(outcome) {
		return DeliveryRecord{}, fmt.Errorf("delivery_outcome_invalid")
	}
	key := deliveryStoreKey(scope, selectionID)
	now := artifactStoreTime(time.Now().UTC())
	result, err := s.db.Exec(`UPDATE semantic_delivery_preparations SET state=?, updated_at=? WHERE delivery_key=? AND state=?`, outcome, now, key, DeliveryDispatching)
	if err != nil {
		return DeliveryRecord{}, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return DeliveryRecord{}, err
	} else if affected == 0 {
		record, err := s.Delivery(scope, selectionID)
		if err != nil {
			return DeliveryRecord{}, err
		}
		if record.State != outcome {
			return DeliveryRecord{}, fmt.Errorf("delivery_outcome_conflict")
		}
		return record, nil
	}
	return s.Delivery(scope, selectionID)
}

func (s *SQLiteArtifactStore) ReconcileStaleDeliveryDispatches(now time.Time, maxAge time.Duration) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("artifact store is unavailable")
	}
	if maxAge <= 0 {
		return 0, fmt.Errorf("delivery dispatch maximum age must be positive")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.Exec(`UPDATE semantic_delivery_preparations SET state=?, unknown_origin=?, updated_at=? WHERE state=? AND updated_at<=?`, DeliveryUnknown, deliveryUnknownFromLapsedLease, artifactStoreTime(now), DeliveryDispatching, artifactStoreTime(now.UTC().Add(-maxAge)))
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(changed), nil
}

func (s *SQLiteArtifactStore) byID(scope InvocationScope, artifactID string) (ArtifactPayload, error) {
	key := artifactStoreKey(scope, artifactID)
	payload, err := scanArtifactPayload(s.db.QueryRow(`SELECT artifact_id, kind, mime_type, integrity_digest, producer_selection, payload_base64, created_at FROM semantic_artifacts WHERE artifact_key=?`, key), scope)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactPayload{}, ErrArtifactNotFound
	}
	if err != nil {
		return ArtifactPayload{}, err
	}
	decoded, err := s.decodeStoredPayload(key, payload.Base64)
	if err != nil {
		return ArtifactPayload{}, err
	}
	payload.Base64 = decoded
	if err := validateArtifactPayload(payload); err != nil {
		return ArtifactPayload{}, fmt.Errorf("artifact_store_corrupt")
	}
	return payload, nil
}

// scanArtifactPayload reads the raw stored row. Callers must pass
// payload.Base64 through decodeStoredPayload before use: the stored form may
// be AEAD ciphertext, and validation only applies to the decoded plaintext.
func scanArtifactPayload(row *sql.Row, scope InvocationScope) (ArtifactPayload, error) {
	var payload ArtifactPayload
	var created string
	err := row.Scan(&payload.Ref.ID, &payload.Ref.Kind, &payload.Ref.MIMEType, &payload.Ref.IntegrityDigest, &payload.Ref.ProducerSelection, &payload.Base64, &created)
	if err != nil {
		return ArtifactPayload{}, err
	}
	payload.Ref.Scope = scope
	payload.Ref.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return payload, nil
}

func artifactStoreKey(scope InvocationScope, artifactID string) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID, strings.TrimSpace(artifactID)}, "\x00")))
}

func newArtifactAccessToken(scope InvocationScope, consumerSelectionID, artifactID string) string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err == nil {
		return "artifact_access_" + base64.RawURLEncoding.EncodeToString(buf)
	}
	return "artifact_access_" + SchemaDigest([]byte(strings.Join([]string{artifactStoreKey(scope, artifactID), strings.TrimSpace(consumerSelectionID), fmt.Sprintf("%d", time.Now().UTC().UnixNano())}, "\x00")))[:32]
}

func sameArtifactIdentity(left, right ArtifactRef) bool {
	return left.ID == right.ID && left.Kind == right.Kind && left.MIMEType == right.MIMEType && left.IntegrityDigest == right.IntegrityDigest && left.ProducerSelection == right.ProducerSelection && left.Scope == right.Scope
}

func deliveryStoreKey(scope InvocationScope, selectionID string) string {
	return SchemaDigest([]byte(strings.Join([]string{scope.RootTaskID, scope.PlanID, scope.SessionID, scope.TurnID, scope.PrincipalID, strings.TrimSpace(selectionID)}, "\x00")))
}

func artifactStoreTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func cloneArtifactPayload(in ArtifactPayload) ArtifactPayload { return in }
