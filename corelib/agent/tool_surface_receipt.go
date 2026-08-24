package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// ToolSurfaceReceipt is an audit record for one concrete outbound model
// request. It records only canonical digests and counts: executable
// definitions themselves remain request-local and are never a successor's
// authority source.
//
// A receipt is not an identity, grant, or execution authorization. Dynamic
// execution remains bound to its verified identity and live transport tuple.
type ToolSurfaceReceipt struct {
	ManifestDigest    string
	WirePayloadDigest string
	// PayloadDigest and WirePayloadHash are the replacement-payload digests.
	// They cover definitions *and* invocation policy. The older fields above
	// remain populated during migration so existing audit readers cannot mistake
	// a partial receipt for a new one.
	PayloadDigest   string
	WirePayloadHash string
	// AuditDigest is deliberately request-local diagnostic evidence. It must
	// never be used as an identity, grant, alias, or successor input. It covers
	// PayloadDigest plus an explicit available/unavailable plan-evidence state,
	// without changing the wire-payload proof.
	AuditDigest       string
	ExpectedToolCount int
	WireToolCount     int
	ReplacementMode   string
	Verified          bool
	Failure           string
	// FailureKind is the bounded metric classification of Failure. Failure keeps
	// a local diagnostic for the request owner; telemetry must use only this
	// enum and never export arbitrary error text.
	FailureKind ToolSurfaceFailureKind
	// Handoff records the transport state after payload verification. It is
	// intentionally separate from Verified: a serializer can prove the payload
	// is complete before a write is attempted, while a failed write may still
	// have ambiguous delivery.
	Handoff ToolSurfaceHandoffState
}

// ToolSurfaceFailureKind is a redacted classification for a failed surface
// receipt. It intentionally has no tool/schema/transport-specific values.
type ToolSurfaceFailureKind string

const (
	ToolSurfaceFailureIntegrity          ToolSurfaceFailureKind = "surface_integrity_failure"
	ToolSurfaceFailureReplaceUnsupported ToolSurfaceFailureKind = "surface_replace_unsupported"
)

// ToolSurfaceOmission is an audit-only summary of an unselected capability.
// It contains neither definitions nor execution authority.
type ToolSurfaceOmission struct {
	NeedID     string
	ReasonCode string
}

// ToolSurfacePlanEvidence describes the immutable plan that produced a
// request-local dynamic surface. Unavailable is the explicit, honest state for
// static S0.5 requests that only have rendered definitions.
type ToolSurfacePlanEvidence struct {
	Available          bool
	PlanID             string
	PlanSnapshotDigest string
	CatalogGeneration  uint64
	Omitted            []ToolSurfaceOmission
}

// ToolSurfaceAuditEvidenceProvider supplies evidence only after the current
// reservation has rendered its request-owned surface. It is not an identity,
// grant, route key, or successor input.
type ToolSurfaceAuditEvidenceProvider interface {
	ToolSurfaceAuditEvidence(execution ToolCallExecutionContext) ToolSurfacePlanEvidence
}

// ToolSurfaceDispatchPreparation is the complete immutable non-definition
// setup required by a correlation-bound dispatch. Audit evidence and
// invocation policy affect different digests, but both must be accepted as
// one request-owned snapshot before a final serializer can write bytes.
type ToolSurfaceDispatchPreparation struct {
	AuditEvidence    ToolSurfacePlanEvidence
	InvocationPolicy ToolSurfaceInvocationPolicy
}

// ToolSurfaceDispatchPreparationRequestChannel accepts the complete setup in
// one operation. New correlation-bound channels must prefer this contract over
// independently ordered setters, which otherwise leave a future transport
// implementation free to observe a half-configured reservation.
type ToolSurfaceDispatchPreparationRequestChannel interface {
	SetToolSurfaceDispatchPreparation(preparation ToolSurfaceDispatchPreparation) error
}

// NormalizeToolSurfacePlanEvidence validates and copies an audit-only plan
// record into its canonical, request-owned form. Callers that retain evidence
// beyond this call must retain this value, not a caller-owned slice: the
// omission list is mutable even though the record is never authorization.
func NormalizeToolSurfacePlanEvidence(evidence ToolSurfacePlanEvidence) (ToolSurfacePlanEvidence, error) {
	return normalizeToolSurfacePlanEvidence(evidence)
}

// ValidateToolSurfacePlanEvidence rejects an ambiguous audit record before a
// request channel accepts it. New channel implementations should call
// NormalizeToolSurfacePlanEvidence and retain the returned immutable copy.
func ValidateToolSurfacePlanEvidence(evidence ToolSurfacePlanEvidence) error {
	_, err := NormalizeToolSurfacePlanEvidence(evidence)
	return err
}

// ToolSurfaceEnvelope identifies the provider-native request shape that a
// policy projection is allowed to use. It is selected by the host request
// path, never inferred from a provider URL, model, task, or tool name.
type ToolSurfaceEnvelope string

const (
	ToolSurfaceEnvelopeUnspecified ToolSurfaceEnvelope = ""
	ToolSurfaceEnvelopeOpenAIChat  ToolSurfaceEnvelope = "openai-chat"
	ToolSurfaceEnvelopeResponses   ToolSurfaceEnvelope = "responses"
	ToolSurfaceEnvelopeAnthropic   ToolSurfaceEnvelope = "anthropic"
)

// ToolSurfaceToolChoice is the provider-neutral callable-surface meaning of
// tool_choice. Absent/default is intentionally distinct from explicit auto.
type ToolSurfaceToolChoice struct {
	Mode string
	Name string
}

const (
	ToolSurfaceToolChoiceProviderDefault = "provider_default"
	ToolSurfaceToolChoiceAuto            = "auto"
	ToolSurfaceToolChoiceRequired        = "required"
	ToolSurfaceToolChoiceNone            = "none"
	ToolSurfaceToolChoiceSpecific        = "specific"
)

// ToolSurfaceOptionalBool preserves the difference between an omitted policy
// field and an explicit false. That distinction is material to providers that
// retain request settings in a session.
type ToolSurfaceOptionalBool struct {
	Present bool
	Value   bool
}

// ToolSurfaceInvocationPolicy is the complete non-definition portion of the
// model-visible tool surface. It contains only final wire semantics, not user
// intent inferred after rendering.
type ToolSurfaceInvocationPolicy struct {
	Envelope          ToolSurfaceEnvelope
	ToolChoice        ToolSurfaceToolChoice
	ParallelToolCalls ToolSurfaceOptionalBool
}

// ToolSurfaceHandoffState describes only the lifetime of one dispatch after
// its tool payload has been checked. It is not an identity, authorization, or
// provider acknowledgement.
type ToolSurfaceHandoffState string

const (
	ToolSurfaceHandoffNotStarted ToolSurfaceHandoffState = "not_started"
	ToolSurfaceHandoffStarted    ToolSurfaceHandoffState = "started"
	ToolSurfaceHandoffAmbiguous  ToolSurfaceHandoffState = "ambiguous"
)

// ToolSurfaceReceiptObserver receives the result of verifying the *actual*
// HTTP payload immediately before it is handed to the transport. Observers
// must treat it as diagnostic evidence only; it is not a lifecycle or
// authorization input.
type ToolSurfaceReceiptObserver interface {
	OnToolSurfaceReceipt(receipt ToolSurfaceReceipt)
}

func toolSurfaceReceiptObserverFor(callbacks LoopCallbacks) ToolSurfaceReceiptObserver {
	observer, _ := callbacks.(ToolSurfaceReceiptObserver)
	return observer
}

// ToolSurfaceEventKind is the stable, low-cardinality lifecycle vocabulary for
// tool-surface telemetry. Events are diagnostic evidence only: they must never
// be used as an identity, grant, alias, or successor input.
type ToolSurfaceEventKind string

const (
	ToolSurfaceEventManifestCreated    ToolSurfaceEventKind = "surface_manifest_created"
	ToolSurfaceEventPayloadVerified    ToolSurfaceEventKind = "surface_payload_verified"
	ToolSurfaceEventIntegrityFailure   ToolSurfaceEventKind = "surface_integrity_failure"
	ToolSurfaceEventReplaceUnsupported ToolSurfaceEventKind = "surface_replace_unsupported"
	ToolSurfaceEventOmissionReason     ToolSurfaceEventKind = "surface_omission_reason"
	ToolSurfaceEventTerminalReason     ToolSurfaceEventKind = "surface_terminal_reason"
)

// ToolSurfaceEvent is a redacted lifecycle metric. It deliberately contains
// only payload/audit digests, counts, and bounded reason enums. In particular,
// it must not carry tool schemas, arguments, aliases, grants, plan IDs,
// provider response IDs, or transport/connection identity.
type ToolSurfaceEvent struct {
	Kind              ToolSurfaceEventKind
	PayloadDigest     string
	AuditDigest       string
	ExpectedToolCount int
	WireToolCount     int
	ReplacementMode   string
	Handoff           ToolSurfaceHandoffState
	Delivery          ToolSurfaceDeliveryState
	TerminalReason    ToolSurfaceDisposition
	OmissionReason    string
	FailureKind       ToolSurfaceFailureKind
}

// ToolSurfaceEventObserver receives redacted lifecycle metrics. Observers are
// intentionally optional so telemetry cannot make a request executable or
// change its lifecycle result.
type ToolSurfaceEventObserver interface {
	OnToolSurfaceEvent(event ToolSurfaceEvent)
}

func toolSurfaceEventObserverFor(callbacks LoopCallbacks) ToolSurfaceEventObserver {
	observer, _ := callbacks.(ToolSurfaceEventObserver)
	return observer
}

func emitToolSurfaceEvent(observer ToolSurfaceEventObserver, event ToolSurfaceEvent) {
	if observer != nil {
		observer.OnToolSurfaceEvent(event)
	}
}

// emitToolSurfaceManifestCreated emits the immutable request-surface summary
// immediately after a concrete owner has rendered it. Omission telemetry keeps
// only the normalized reason code: NeedID is plan-internal detail and must not
// become a metric label.
func emitToolSurfaceManifestCreated(observer ToolSurfaceEventObserver, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence) (ToolSurfaceEvent, error) {
	_, event, normalizedEvidence, err := newToolSurfaceLifecycleManifest(tools, policy, evidence)
	if err != nil {
		return ToolSurfaceEvent{}, err
	}
	emitToolSurfaceEvent(observer, event)
	if normalizedEvidence.Available {
		for _, omission := range normalizedEvidence.Omitted {
			emitToolSurfaceEvent(observer, ToolSurfaceEvent{
				Kind:              ToolSurfaceEventOmissionReason,
				PayloadDigest:     event.PayloadDigest,
				AuditDigest:       event.AuditDigest,
				ExpectedToolCount: event.ExpectedToolCount,
				ReplacementMode:   event.ReplacementMode,
				OmissionReason:    omission.ReasonCode,
			})
		}
	}
	return event, nil
}

// newToolSurfaceLifecycleManifest freezes the exact manifest used by both the
// lifecycle event and the final-boundary receipt transport. Keeping this
// construction single-shot prevents an observer or another mutable caller from
// changing the input slice between telemetry creation and later terminal
// accounting.
func newToolSurfaceLifecycleManifest(tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence) (toolSurfaceManifest, ToolSurfaceEvent, ToolSurfacePlanEvidence, error) {
	manifest, err := newToolSurfaceManifestWithInvocationPolicy(tools, policy)
	if err != nil {
		return toolSurfaceManifest{}, ToolSurfaceEvent{}, ToolSurfacePlanEvidence{}, err
	}
	normalizedEvidence, err := normalizeToolSurfacePlanEvidence(evidence)
	if err != nil {
		return toolSurfaceManifest{}, ToolSurfaceEvent{}, ToolSurfacePlanEvidence{}, err
	}
	auditDigest, err := digestToolSurfaceAuditEvidence(manifest.digest, normalizedEvidence)
	if err != nil {
		return toolSurfaceManifest{}, ToolSurfaceEvent{}, ToolSurfacePlanEvidence{}, err
	}
	return manifest, ToolSurfaceEvent{
		Kind:              ToolSurfaceEventManifestCreated,
		PayloadDigest:     manifest.digest,
		AuditDigest:       auditDigest,
		ExpectedToolCount: manifest.toolCount,
		ReplacementMode:   manifest.replacement,
	}, normalizedEvidence, nil
}

// toolSurfaceTerminalEventFromManifest keeps the terminal metric anchored to
// the exact immutable manifest already emitted for a bound reservation. It is
// intentionally a projection copy rather than a second render/digest pass: a
// channel terminal must not depend on mutable renderer inputs after durable
// publication.
func toolSurfaceTerminalEventFromManifest(manifest ToolSurfaceEvent, disposition ToolSurfaceDisposition) ToolSurfaceEvent {
	return ToolSurfaceEvent{
		Kind:              ToolSurfaceEventTerminalReason,
		PayloadDigest:     manifest.PayloadDigest,
		AuditDigest:       manifest.AuditDigest,
		ExpectedToolCount: manifest.ExpectedToolCount,
		ReplacementMode:   manifest.ReplacementMode,
		TerminalReason:    disposition,
	}
}

func emitToolSurfaceReceiptEvents(observer ToolSurfaceEventObserver, receipt ToolSurfaceReceipt) {
	event := ToolSurfaceEvent{
		PayloadDigest:     receipt.PayloadDigest,
		AuditDigest:       receipt.AuditDigest,
		ExpectedToolCount: receipt.ExpectedToolCount,
		WireToolCount:     receipt.WireToolCount,
		ReplacementMode:   receipt.ReplacementMode,
		Handoff:           receipt.Handoff,
		FailureKind:       receipt.FailureKind,
	}
	if receipt.Verified {
		event.Kind = ToolSurfaceEventPayloadVerified
		emitToolSurfaceEvent(observer, event)
		return
	}
	if receipt.FailureKind == ToolSurfaceFailureReplaceUnsupported {
		event.Kind = ToolSurfaceEventReplaceUnsupported
		emitToolSurfaceEvent(observer, event)
	}
	if event.FailureKind == "" {
		event.FailureKind = ToolSurfaceFailureIntegrity
	}
	event.Kind = ToolSurfaceEventIntegrityFailure
	emitToolSurfaceEvent(observer, event)
}

type lifecycleToolSurfaceReceiptObserver struct {
	receipts ToolSurfaceReceiptObserver
	events   ToolSurfaceEventObserver
}

func (observer lifecycleToolSurfaceReceiptObserver) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	if observer.receipts != nil {
		observer.receipts.OnToolSurfaceReceipt(receipt)
	}
	emitToolSurfaceReceiptEvents(observer.events, receipt)
}

func toolSurfaceLifecycleReceiptObserverFor(callbacks LoopCallbacks) ToolSurfaceReceiptObserver {
	receipts := toolSurfaceReceiptObserverFor(callbacks)
	events := toolSurfaceEventObserverFor(callbacks)
	if receipts == nil && events == nil {
		return nil
	}
	return lifecycleToolSurfaceReceiptObserver{receipts: receipts, events: events}
}

type toolSurfaceStaticRequestLifecycle struct {
	client      *http.Client
	manifest    ToolSurfaceEvent
	definitions []map[string]interface{}
}

// NewToolSurfaceReceiptHTTPClientWithLifecycleEvents creates a receipt client
// for one owner-visible request and emits its redacted manifest metric before
// the request can leave the host. The evidence remains audit-only; its
// immutable digest is carried into the final HTTP receipt so the lifecycle has
// one audit projection from manifest through terminal accounting.
func NewToolSurfaceReceiptHTTPClientWithLifecycleEvents(base *http.Client, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence, observer ToolSurfaceReceiptObserver, events ToolSurfaceEventObserver) (*http.Client, error) {
	lifecycle, err := newToolSurfaceReceiptHTTPClientWithLifecycleEvents(base, tools, policy, evidence, observer, events)
	if err != nil {
		return nil, err
	}
	return lifecycle.client, nil
}

// newToolSurfaceReceiptHTTPClientWithLifecycleEvents creates one static
// request lifecycle from a single immutable manifest. RunLoop retains the
// returned manifest projection for the matching terminal event; the receipt
// client uses the same manifest at RoundTrip time.
func newToolSurfaceReceiptHTTPClientWithLifecycleEvents(base *http.Client, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence, observer ToolSurfaceReceiptObserver, events ToolSurfaceEventObserver) (toolSurfaceStaticRequestLifecycle, error) {
	// Freeze the rendered definitions before publishing the manifest. Lifecycle
	// observers are diagnostic-only, but they can synchronously mutate callback
	// state which originally supplied tools. A request must therefore send this
	// request-owned snapshot, never re-read a shared callback slice after the
	// manifest digest has been created.
	frozenTools, err := freezeToolSurfaceDefinitions(tools)
	if err != nil {
		emitToolSurfaceEvent(events, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
		emitToolSurfaceEvent(events, ToolSurfaceEvent{
			Kind:           ToolSurfaceEventTerminalReason,
			TerminalReason: ToolSurfaceIntegrityFailure,
			FailureKind:    ToolSurfaceFailureIntegrity,
		})
		return toolSurfaceStaticRequestLifecycle{}, err
	}
	manifest, event, normalizedEvidence, err := newToolSurfaceLifecycleManifest(frozenTools, policy, evidence)
	if err != nil {
		emitToolSurfaceEvent(events, ToolSurfaceEvent{Kind: ToolSurfaceEventIntegrityFailure, FailureKind: ToolSurfaceFailureIntegrity})
		// Manifest construction happens before a static request can leave the
		// host. It is still an owner-visible attempt to create a surface, so close
		// the metric lifecycle explicitly instead of leaving a dangling failure
		// that a later aggregator/static terminal could be mistaken to settle.
		// No digest is emitted because no immutable manifest exists to anchor it.
		emitToolSurfaceEvent(events, ToolSurfaceEvent{
			Kind:           ToolSurfaceEventTerminalReason,
			TerminalReason: ToolSurfaceIntegrityFailure,
			FailureKind:    ToolSurfaceFailureIntegrity,
		})
		return toolSurfaceStaticRequestLifecycle{}, err
	}
	emitToolSurfaceEvent(events, event)
	if normalizedEvidence.Available {
		for _, omission := range normalizedEvidence.Omitted {
			emitToolSurfaceEvent(events, ToolSurfaceEvent{
				Kind:              ToolSurfaceEventOmissionReason,
				PayloadDigest:     event.PayloadDigest,
				AuditDigest:       event.AuditDigest,
				ExpectedToolCount: event.ExpectedToolCount,
				ReplacementMode:   event.ReplacementMode,
				OmissionReason:    omission.ReasonCode,
			})
		}
	}
	return toolSurfaceStaticRequestLifecycle{
		client:      newToolSurfaceReceiptHTTPClientForManifestWithAuditEvidence(base, manifest, normalizedEvidence, observer),
		manifest:    event,
		definitions: frozenTools,
	}, nil
}

// serializedToolSurfaceReceiptObserver preserves the request-local observer
// contract when one RunLoop fans out independent advisor requests in parallel.
// A receipt remains diagnostic only; serialization neither orders transport
// attempts nor creates a shared identity, grant, or successor authority. It
// merely prevents concurrently completed attempts from racing an ordinary host
// audit sink such as an append-only slice.
type serializedToolSurfaceReceiptObserver struct {
	mu   sync.Mutex
	next ToolSurfaceReceiptObserver
}

type serializedToolSurfaceEventObserver struct {
	mu   sync.Mutex
	next ToolSurfaceEventObserver
}

func newSerializedToolSurfaceEventObserver(observer ToolSurfaceEventObserver) ToolSurfaceEventObserver {
	if observer == nil {
		return nil
	}
	return &serializedToolSurfaceEventObserver{next: observer}
}

func (observer *serializedToolSurfaceEventObserver) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	if observer == nil || observer.next == nil {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.next.OnToolSurfaceEvent(event)
}

func newSerializedToolSurfaceReceiptObserver(observer ToolSurfaceReceiptObserver) ToolSurfaceReceiptObserver {
	if observer == nil {
		return nil
	}
	return &serializedToolSurfaceReceiptObserver{next: observer}
}

func (observer *serializedToolSurfaceReceiptObserver) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	if observer == nil || observer.next == nil {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.next.OnToolSurfaceReceipt(receipt)
}

// VerifyToolSurfaceWirePayload verifies a provider-specific final tools array
// against the rendered request surface. Channel-owned transports call it just
// before writing their frame; ordinary HTTP transports use the RoundTripper
// wrapper below. It is exported because a channel must not recreate this
// security contract from its own cache or name map.
func VerifyToolSurfaceWirePayload(tools []map[string]interface{}, wireTools []map[string]interface{}) (ToolSurfaceReceipt, error) {
	return VerifyToolSurfaceWirePayloadWithInvocationPolicy(tools, wireTools, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeUnspecified))
}

// VerifyToolSurfaceWirePayloadWithInvocationPolicy verifies both definitions
// and the fields that determine whether/how those definitions are callable.
// It is shared by final HTTP-body and WebSocket-frame verification points.
func VerifyToolSurfaceWirePayloadWithInvocationPolicy(tools []map[string]interface{}, wireTools []map[string]interface{}, policy ToolSurfaceInvocationPolicy) (ToolSurfaceReceipt, error) {
	return VerifyToolSurfaceWirePayloadWithAuditEvidence(tools, wireTools, policy, ToolSurfacePlanEvidence{})
}

// VerifyToolSurfaceWirePayloadWithAuditEvidence verifies the final callable
// payload while preserving a separate audit digest for the immutable plan and
// intentionally omitted needs.
func VerifyToolSurfaceWirePayloadWithAuditEvidence(tools []map[string]interface{}, wireTools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence) (ToolSurfaceReceipt, error) {
	manifest, err := newToolSurfaceManifestWithInvocationPolicy(tools, policy)
	if err != nil {
		return ToolSurfaceReceipt{ReplacementMode: "replace", Failure: "surface_integrity_failure: " + err.Error()}, err
	}
	receipt := manifest.receiptForWirePayloadWithAuditEvidence(wireTools, policy, evidence)
	if !receipt.Verified {
		return receipt, fmt.Errorf("%s", receipt.Failure)
	}
	return receipt, nil
}

// VerifyToolSurfaceRequestPayload observes the final serialized request map
// and compares its complete callable-surface projection with the host-owned
// expected policy. It is the shared final-boundary verifier for HTTP and WS.
func VerifyToolSurfaceRequestPayload(tools []map[string]interface{}, payload map[string]interface{}, expected ToolSurfaceInvocationPolicy) (ToolSurfaceReceipt, error) {
	return VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools, payload, expected, ToolSurfacePlanEvidence{})
}

// VerifyToolSurfaceRequestPayloadWithAuditEvidence is the final-boundary
// verifier for a full request payload and request-local plan audit evidence.
func VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools []map[string]interface{}, payload map[string]interface{}, expected ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence) (ToolSurfaceReceipt, error) {
	manifest, err := newToolSurfaceManifestWithInvocationPolicy(tools, expected)
	if err != nil {
		return ToolSurfaceReceipt{ReplacementMode: "replace", Failure: "surface_integrity_failure: " + err.Error()}, err
	}
	failureReceipt := manifest.receiptForAuditEvidence(evidence)
	if failureReceipt.Failure != "" {
		return failureReceipt, fmt.Errorf("%s", failureReceipt.Failure)
	}
	// Empty expected surfaces still require an explicit `tools: []` replacement.
	// The HTTP wrapper injects it for legacy builders before this function runs;
	// WS builders must serialize it themselves.
	if _, present := payload["tools"]; !present && manifest.toolCount == 0 {
		failureReceipt.Failure = "surface_integrity_failure: outbound payload omitted explicit empty replacement"
		failureReceipt.FailureKind = ToolSurfaceFailureIntegrity
		return failureReceipt, fmt.Errorf("%s", failureReceipt.Failure)
	}
	wireTools, present, err := toolSurfaceWireDefinitions(payload)
	if err != nil {
		failureReceipt.Failure = "surface_integrity_failure: " + err.Error()
		failureReceipt.FailureKind = ToolSurfaceFailureIntegrity
		return failureReceipt, fmt.Errorf("surface_integrity_failure: %w", err)
	}
	if !present {
		failureReceipt.Failure = "surface_integrity_failure: outbound payload omitted tools"
		failureReceipt.FailureKind = ToolSurfaceFailureIntegrity
		return failureReceipt, fmt.Errorf("%s", failureReceipt.Failure)
	}
	observed, err := toolSurfaceInvocationPolicyFromPayload(payload, manifest.policy.Envelope)
	if err != nil {
		failureReceipt.Failure = "surface_integrity_failure: " + err.Error()
		failureReceipt.FailureKind = ToolSurfaceFailureIntegrity
		return failureReceipt, fmt.Errorf("surface_integrity_failure: %w", err)
	}
	receipt := manifest.receiptForWirePayloadWithAuditEvidence(wireTools, observed, evidence)
	if !receipt.Verified {
		return receipt, fmt.Errorf("%s", receipt.Failure)
	}
	return receipt, nil
}

// VerifyToolSurfaceReceiptForRenderedTools makes a receipt an input to the
// request owner rather than a best-effort transport log. The rendered surface
// is recalculated here so a channel cannot return a receipt for another
// request, a stale definition list, or an implicit merge.
func VerifyToolSurfaceReceiptForRenderedTools(tools []map[string]interface{}, receipt ToolSurfaceReceipt) error {
	return VerifyToolSurfaceReceiptForRenderedToolsWithInvocationPolicy(tools, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeUnspecified), receipt)
}

// VerifyToolSurfaceReceiptForRenderedToolsWithInvocationPolicy rejects a
// receipt for a different invocation policy before a response can bind a
// correlation-bound surface.
func VerifyToolSurfaceReceiptForRenderedToolsWithInvocationPolicy(tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, receipt ToolSurfaceReceipt) error {
	return VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(tools, policy, ToolSurfacePlanEvidence{}, receipt)
}

// VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence checks both the
// model-visible payload proof and the audit-only plan proof before response
// binding. Audit evidence never enters the payload digest.
func VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence, receipt ToolSurfaceReceipt) error {
	manifest, err := newToolSurfaceManifestWithInvocationPolicy(tools, policy)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: render manifest: %w", err)
	}
	if !receipt.Verified {
		if strings.TrimSpace(receipt.Failure) != "" {
			return fmt.Errorf("%s", receipt.Failure)
		}
		return fmt.Errorf("surface_integrity_failure: dispatch did not return a verified receipt")
	}
	if receipt.ReplacementMode != "replace" {
		return fmt.Errorf("surface_integrity_failure: unsupported replacement mode %q", receipt.ReplacementMode)
	}
	if receipt.ExpectedToolCount != manifest.toolCount || receipt.WireToolCount != manifest.toolCount {
		return fmt.Errorf("surface_integrity_failure: receipt tool count mismatch expected=%d receipt_expected=%d wire=%d", manifest.toolCount, receipt.ExpectedToolCount, receipt.WireToolCount)
	}
	// PayloadDigest/WirePayloadHash are the complete post-migration proof. The
	// older definition-only fields may still be logged for observability, but
	// are intentionally not accepted as bind authority.
	if receipt.PayloadDigest == "" || receipt.PayloadDigest != manifest.digest || receipt.WirePayloadHash == "" || receipt.WirePayloadHash != manifest.digest {
		return fmt.Errorf("surface_integrity_failure: receipt does not match rendered surface")
	}
	auditDigest, err := digestToolSurfaceAuditEvidence(manifest.digest, evidence)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: audit manifest: %w", err)
	}
	if receipt.AuditDigest == "" || receipt.AuditDigest != auditDigest {
		return fmt.Errorf("surface_integrity_failure: receipt audit evidence does not match rendered surface")
	}
	return nil
}

type toolSurfaceManifest struct {
	digest      string
	toolCount   int
	replacement string
	policy      ToolSurfaceInvocationPolicy
}

type toolSurfaceCanonicalDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// freezeToolSurfaceDefinitions creates the request-owned definition snapshot
// used after a surface is rendered. Tool definitions must be JSON-shaped to be
// sent to a model anyway; JSON round-tripping consequently gives us both a
// deep copy and the exact numeric representation the serializer will use.
// Refusing a value which cannot cross this boundary is safer than retaining a
// mutable reference and later hashing/sending different surfaces.
func freezeToolSurfaceDefinitions(tools []map[string]interface{}) ([]map[string]interface{}, error) {
	data, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("freeze tool surface definitions: %w", err)
	}
	var frozen []map[string]interface{}
	if err := json.Unmarshal(data, &frozen); err != nil {
		return nil, fmt.Errorf("freeze tool surface definitions: %w", err)
	}
	// Preserve the explicit empty-replacement distinction at the Go boundary.
	// JSON has only one representation for nil and [] here, but downstream
	// builders receive an empty slice in either case and write tools: [].
	if frozen == nil {
		frozen = []map[string]interface{}{}
	}
	return frozen, nil
}

func newToolSurfaceManifest(tools []map[string]interface{}) (toolSurfaceManifest, error) {
	return newToolSurfaceManifestWithInvocationPolicy(tools, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeUnspecified))
}

func newToolSurfaceManifestWithInvocationPolicy(tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy) (toolSurfaceManifest, error) {
	policy, err := normalizeToolSurfaceInvocationPolicy(policy)
	if err != nil {
		return toolSurfaceManifest{}, err
	}
	canonical, err := canonicalToolSurfaceDefinitions(tools)
	if err != nil {
		return toolSurfaceManifest{}, err
	}
	if err := validateToolSurfaceInvocationPolicyAgainstDefinitions(policy, canonical); err != nil {
		return toolSurfaceManifest{}, err
	}
	digest, err := digestToolSurfacePayload(canonical, "replace", policy)
	if err != nil {
		return toolSurfaceManifest{}, err
	}
	return toolSurfaceManifest{
		digest:      digest,
		toolCount:   len(canonical),
		replacement: "replace",
		policy:      policy,
	}, nil
}

// validateToolSurfaceInvocationPolicyAgainstDefinitions ensures an explicit
// function selector is a member of this exact immutable replacement surface.
// Without this, a validly shaped `tool_choice` could name a function omitted
// from `tools`, which changes the callable contract even when the payload
// digest itself is internally consistent.
func validateToolSurfaceInvocationPolicyAgainstDefinitions(policy ToolSurfaceInvocationPolicy, definitions []toolSurfaceCanonicalDefinition) error {
	if policy.ToolChoice.Mode == ToolSurfaceToolChoiceRequired && len(definitions) == 0 {
		return fmt.Errorf("required tool choice is not satisfiable on an empty tool surface")
	}
	if policy.ToolChoice.Mode != ToolSurfaceToolChoiceSpecific {
		return nil
	}
	for _, definition := range definitions {
		if definition.Name == policy.ToolChoice.Name {
			return nil
		}
	}
	return fmt.Errorf("specific tool choice %q is not present in tool surface", policy.ToolChoice.Name)
}

// DefaultToolSurfaceInvocationPolicy represents a deliberately absent
// tool_choice and parallel_tool_calls policy. It is not shorthand for auto or
// false; both are distinct callable surfaces.
func DefaultToolSurfaceInvocationPolicy(envelope ToolSurfaceEnvelope) ToolSurfaceInvocationPolicy {
	return ToolSurfaceInvocationPolicy{
		Envelope:   envelope,
		ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceProviderDefault},
	}
}

// NormalizeToolSurfaceInvocationPolicy validates the complete invocation
// portion of a model-visible tool surface and returns its canonical form.
// Request owners retain this value; they must not retain caller-owned maps as
// a substitute for the policy.
func NormalizeToolSurfaceInvocationPolicy(policy ToolSurfaceInvocationPolicy) (ToolSurfaceInvocationPolicy, error) {
	return normalizeToolSurfaceInvocationPolicy(policy)
}

// ToolSurfaceInvocationPolicyWireFields returns a fresh provider-native
// projection of the invocation controls. It contains no definitions and no
// provider/model inference: callers add it to the final request immediately
// before serializing and then verify that exact payload.
func ToolSurfaceInvocationPolicyWireFields(policy ToolSurfaceInvocationPolicy) (map[string]interface{}, error) {
	policy, err := NormalizeToolSurfaceInvocationPolicy(policy)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]interface{}, 2)
	switch policy.ToolChoice.Mode {
	case ToolSurfaceToolChoiceAuto, ToolSurfaceToolChoiceRequired, ToolSurfaceToolChoiceNone:
		fields["tool_choice"] = policy.ToolChoice.Mode
	case ToolSurfaceToolChoiceSpecific:
		if policy.Envelope == ToolSurfaceEnvelopeOpenAIChat {
			fields["tool_choice"] = map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": policy.ToolChoice.Name}}
		} else {
			fields["tool_choice"] = map[string]interface{}{"type": "function", "name": policy.ToolChoice.Name}
		}
	}
	if policy.ParallelToolCalls.Present {
		fields["parallel_tool_calls"] = policy.ParallelToolCalls.Value
	}
	return fields, nil
}

func normalizeToolSurfaceInvocationPolicy(policy ToolSurfaceInvocationPolicy) (ToolSurfaceInvocationPolicy, error) {
	policy.Envelope = ToolSurfaceEnvelope(strings.TrimSpace(string(policy.Envelope)))
	switch policy.Envelope {
	case ToolSurfaceEnvelopeUnspecified, ToolSurfaceEnvelopeOpenAIChat, ToolSurfaceEnvelopeResponses, ToolSurfaceEnvelopeAnthropic:
	default:
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("unsupported tool surface envelope %q", policy.Envelope)
	}
	policy.ToolChoice.Mode = strings.TrimSpace(policy.ToolChoice.Mode)
	policy.ToolChoice.Name = strings.TrimSpace(policy.ToolChoice.Name)
	if policy.ToolChoice.Mode == "" {
		policy.ToolChoice.Mode = ToolSurfaceToolChoiceProviderDefault
	}
	switch policy.ToolChoice.Mode {
	case ToolSurfaceToolChoiceProviderDefault, ToolSurfaceToolChoiceAuto, ToolSurfaceToolChoiceRequired, ToolSurfaceToolChoiceNone:
		if policy.ToolChoice.Name != "" {
			return ToolSurfaceInvocationPolicy{}, fmt.Errorf("tool choice %q must not name a function", policy.ToolChoice.Mode)
		}
	case ToolSurfaceToolChoiceSpecific:
		if policy.ToolChoice.Name == "" {
			return ToolSurfaceInvocationPolicy{}, fmt.Errorf("specific tool choice requires a function name")
		}
	default:
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("unsupported tool choice %q", policy.ToolChoice.Mode)
	}
	return policy, nil
}

func canonicalToolSurfaceDefinitions(definitions []map[string]interface{}) ([]toolSurfaceCanonicalDefinition, error) {
	canonical := make([]toolSurfaceCanonicalDefinition, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		item, err := canonicalToolSurfaceDefinition(definition)
		if err != nil {
			return nil, fmt.Errorf("tool surface definition %d: %w", index, err)
		}
		if _, duplicate := seen[item.Name]; duplicate {
			return nil, fmt.Errorf("duplicate tool surface definition %q", item.Name)
		}
		seen[item.Name] = struct{}{}
		canonical = append(canonical, item)
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Name < canonical[j].Name })
	return canonical, nil
}

// canonicalToolSurfaceDefinition accepts the three final wire shapes emitted
// by the request builders: OpenAI Chat (function nested), Anthropic
// (input_schema), and Responses (function fields flattened). Comparing this
// logical form means provider-specific envelope conversion is allowed while a
// dropped, renamed, or schema-mutated definition is rejected before bytes
// leave the host.
func canonicalToolSurfaceDefinition(definition map[string]interface{}) (toolSurfaceCanonicalDefinition, error) {
	if definition == nil {
		return toolSurfaceCanonicalDefinition{}, fmt.Errorf("definition is nil")
	}
	// This contract owns function-call surfaces only. Anthropic's native tool
	// shape legitimately omits type, but every supported wire shape that does
	// state it must say "function". Accepting a different provider-visible type
	// merely because it happens to carry a name and schema would let a sender
	// change the model's callable primitive without changing the digest.
	if rawType, present := definition["type"]; present && rawType != nil {
		toolType, ok := rawType.(string)
		if !ok || strings.TrimSpace(toolType) != "function" {
			return toolSurfaceCanonicalDefinition{}, fmt.Errorf("unsupported tool type for function surface")
		}
	}
	function := asStringInterfaceMap(definition["function"])
	if function == nil {
		function = definition
	}
	name, _ := function["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return toolSurfaceCanonicalDefinition{}, fmt.Errorf("function name is required")
	}
	description, hasDescription := function["description"]
	if hasDescription && description != nil {
		var ok bool
		if description, ok = description.(string); !ok {
			return toolSurfaceCanonicalDefinition{}, fmt.Errorf("description must be a string for %q", name)
		}
	}
	parameters, hasParameters := function["parameters"]
	if !hasParameters {
		parameters, hasParameters = definition["input_schema"]
	}
	if !hasParameters || parameters == nil {
		return toolSurfaceCanonicalDefinition{}, fmt.Errorf("parameters are required for %q", name)
	}
	// Descriptions are model-visible routing instructions, not disposable UI
	// metadata. A payload that keeps an alias and schema but changes its
	// description can still route the model to a different capability, so it
	// must change the surface digest just as a schema change would. Treat an
	// omitted description and an explicit empty description alike because the
	// request builders intentionally elide empty strings.
	descriptionText := ""
	if hasDescription && description != nil {
		descriptionText = description.(string)
	}
	return toolSurfaceCanonicalDefinition{Name: name, Description: descriptionText, Parameters: canonicalizeToolSurfaceSchema(parameters)}, nil
}

// json.Unmarshal represents numeric values as float64 while in-memory tool
// schemas commonly use int. Normalize through JSON once so the manifest is
// invariant to that transport-only representation change.
func normalizeToolSurfaceJSON(value interface{}) interface{} {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized interface{}
	if err := json.Unmarshal(data, &normalized); err != nil {
		return value
	}
	return normalized
}

// Request builders may apply provider-safe schema reductions (for example
// remove unsupported JSON-Schema annotations) without changing the callable
// parameter contract. Canonicalize both the rendered and wire forms through
// that same projection. Dropping a definition, changing its name, or altering
// the surviving schema still changes the digest and remains fail-closed.
func canonicalizeToolSurfaceSchema(value interface{}) interface{} {
	normalized := normalizeToolSurfaceJSON(value)
	return projectToolSurfaceSchemaContract(normalized)
}

// projectToolSurfaceSchemaContract mirrors only the protocol-safe schema
// normalizations performed by llm.sanitizeOpenAIToolParametersForSDK: missing
// object properties become {}, and missing array items become string items.
// These are wire-shape repairs, not capability additions; all declared fields
// and constraints stay in the digest.
func projectToolSurfaceSchemaContract(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed)+2)
		for key, item := range typed {
			switch key {
			case "required":
				if required := normalizeToolSurfaceRequired(item); len(required) > 0 {
					out[key] = required
				}
			case "properties":
				out[key] = projectToolSurfaceSchemaProperties(item)
			default:
				out[key] = projectToolSurfaceSchemaContract(item)
			}
		}
		typ, _ := out["type"].(string)
		if strings.TrimSpace(typ) == "" {
			out["type"] = "object"
			typ = "object"
		}
		if typ == "object" {
			if _, found := out["properties"]; !found {
				out["properties"] = map[string]interface{}{}
			}
		}
		if typ == "array" {
			if _, found := out["items"]; !found {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for index, item := range typed {
			out[index] = projectToolSurfaceSchemaContract(item)
		}
		return out
	default:
		return value
	}
}

func projectToolSurfaceSchemaProperties(value interface{}) interface{} {
	properties, ok := value.(map[string]interface{})
	if !ok {
		return projectToolSurfaceSchemaContract(value)
	}
	out := make(map[string]interface{}, len(properties))
	for name, schema := range properties {
		out[name] = projectToolSurfaceSchemaContract(schema)
	}
	return out
}

func normalizeToolSurfaceRequired(value interface{}) []interface{} {
	items, ok := value.([]interface{})
	if !ok {
		if name, ok := value.(string); ok && strings.TrimSpace(name) != "" {
			return []interface{}{strings.TrimSpace(name)}
		}
		return nil
	}
	out := make([]interface{}, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		name, ok := item.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func asStringInterfaceMap(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	case map[string]string:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return nil
	}
}

func digestToolSurfacePayload(definitions []toolSurfaceCanonicalDefinition, replacement string, policy ToolSurfaceInvocationPolicy) (string, error) {
	payload := struct {
		Definitions []toolSurfaceCanonicalDefinition `json:"definitions"`
		Replacement string                           `json:"replacement"`
		Policy      ToolSurfaceInvocationPolicy      `json:"policy"`
	}{Definitions: definitions, Replacement: replacement, Policy: policy}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize tool surface: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (manifest toolSurfaceManifest) receiptForWirePayload(definitions []map[string]interface{}, policy ToolSurfaceInvocationPolicy) ToolSurfaceReceipt {
	return manifest.receiptForWirePayloadWithAuditEvidence(definitions, policy, ToolSurfacePlanEvidence{})
}

// receiptForAuditEvidence produces the immutable, redacted projection that a
// receipt must retain even when final payload parsing fails before definitions
// or invocation policy can be compared. It keeps failure telemetry anchored to
// the same request-owned plan evidence as the manifest without making that
// evidence part of callable-surface verification.
func (manifest toolSurfaceManifest) receiptForAuditEvidence(evidence ToolSurfacePlanEvidence) ToolSurfaceReceipt {
	receipt := ToolSurfaceReceipt{
		ManifestDigest:    manifest.digest,
		ExpectedToolCount: manifest.toolCount,
		ReplacementMode:   manifest.replacement,
		PayloadDigest:     manifest.digest,
	}
	auditDigest, err := digestToolSurfaceAuditEvidence(manifest.digest, evidence)
	if err != nil {
		receipt.Failure = "surface_integrity_failure: " + err.Error()
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	receipt.AuditDigest = auditDigest
	return receipt
}

func (manifest toolSurfaceManifest) receiptForWirePayloadWithAuditEvidence(definitions []map[string]interface{}, policy ToolSurfaceInvocationPolicy, evidence ToolSurfacePlanEvidence) ToolSurfaceReceipt {
	receipt := manifest.receiptForAuditEvidence(evidence)
	receipt.WireToolCount = len(definitions)
	if receipt.Failure != "" {
		return receipt
	}
	policy, err := normalizeToolSurfaceInvocationPolicy(policy)
	if err != nil {
		receipt.Failure = "surface_integrity_failure: " + err.Error()
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	if policy != manifest.policy {
		receipt.Failure = "surface_integrity_failure: invocation policy differs from manifest"
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	canonical, err := canonicalToolSurfaceDefinitions(definitions)
	if err != nil {
		receipt.Failure = "surface_integrity_failure: " + err.Error()
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	digest, err := digestToolSurfacePayload(canonical, manifest.replacement, policy)
	if err != nil {
		receipt.Failure = "surface_integrity_failure: " + err.Error()
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	receipt.WirePayloadDigest = digest
	receipt.WirePayloadHash = digest
	// Compatibility-only diagnostics retain the old field names, but with the
	// same complete payload digest; they are never accepted by the binder.
	receipt.ManifestDigest = manifest.digest
	if digest != manifest.digest {
		receipt.Failure = fmt.Sprintf("surface_integrity_failure: manifest and wire payload differ expected=%s actual=%s", manifest.digest, digest)
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		return receipt
	}
	receipt.Verified = true
	return receipt
}

func digestToolSurfaceAuditEvidence(payloadDigest string, evidence ToolSurfacePlanEvidence) (string, error) {
	evidence, err := normalizeToolSurfacePlanEvidence(evidence)
	if err != nil {
		return "", err
	}
	payload := struct {
		PayloadDigest string                  `json:"payload_digest"`
		Plan          ToolSurfacePlanEvidence `json:"plan"`
	}{PayloadDigest: payloadDigest, Plan: evidence}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize tool surface audit evidence: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeToolSurfacePlanEvidence(evidence ToolSurfacePlanEvidence) (ToolSurfacePlanEvidence, error) {
	evidence.PlanID = strings.TrimSpace(evidence.PlanID)
	evidence.PlanSnapshotDigest = strings.TrimSpace(evidence.PlanSnapshotDigest)
	if !evidence.Available {
		if evidence.PlanID != "" || evidence.PlanSnapshotDigest != "" || evidence.CatalogGeneration != 0 || len(evidence.Omitted) != 0 {
			return ToolSurfacePlanEvidence{}, fmt.Errorf("unavailable plan evidence must not contain plan or omission fields")
		}
		return ToolSurfacePlanEvidence{}, nil
	}
	if evidence.PlanID == "" || evidence.PlanSnapshotDigest == "" {
		return ToolSurfacePlanEvidence{}, fmt.Errorf("available plan evidence requires plan id and snapshot digest")
	}
	omitted := make([]ToolSurfaceOmission, 0, len(evidence.Omitted))
	seen := make(map[string]struct{}, len(evidence.Omitted))
	for _, item := range evidence.Omitted {
		item.NeedID = strings.TrimSpace(item.NeedID)
		item.ReasonCode = strings.TrimSpace(item.ReasonCode)
		if item.NeedID == "" || item.ReasonCode == "" {
			return ToolSurfacePlanEvidence{}, fmt.Errorf("plan omission requires need id and reason code")
		}
		key := item.NeedID + "\x00" + item.ReasonCode
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		omitted = append(omitted, item)
	}
	sort.Slice(omitted, func(i, j int) bool {
		if omitted[i].NeedID == omitted[j].NeedID {
			return omitted[i].ReasonCode < omitted[j].ReasonCode
		}
		return omitted[i].NeedID < omitted[j].NeedID
	})
	evidence.Omitted = omitted
	return evidence, nil
}

// newToolSurfaceReceiptHTTPClient wraps one request attempt. It verifies the
// JSON body at RoundTrip time, after every SDK/provider compatibility rewrite,
// which is the final point at which the host can prevent an incomplete tool
// surface from reaching the model. The callback receives the verified receipt
// before the underlying transport gets any bytes.
func newToolSurfaceReceiptHTTPClient(base *http.Client, tools []map[string]interface{}, observer ToolSurfaceReceiptObserver) (*http.Client, error) {
	return newToolSurfaceReceiptHTTPClientWithInvocationPolicy(base, tools, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeUnspecified), observer)
}

// NewToolSurfaceReceiptHTTPClientWithInvocationPolicy verifies the final JSON
// body for one request. Callers must pass the host-owned expected policy rather
// than derive it from a provider URL or mutable request metadata.
func NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(base *http.Client, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, observer ToolSurfaceReceiptObserver) (*http.Client, error) {
	return newToolSurfaceReceiptHTTPClientWithInvocationPolicy(base, tools, policy, observer)
}

func newToolSurfaceReceiptHTTPClientWithInvocationPolicy(base *http.Client, tools []map[string]interface{}, policy ToolSurfaceInvocationPolicy, observer ToolSurfaceReceiptObserver) (*http.Client, error) {
	manifest, err := newToolSurfaceManifestWithInvocationPolicy(tools, policy)
	if err != nil {
		return nil, err
	}
	return newToolSurfaceReceiptHTTPClientForManifest(base, manifest, observer), nil
}

func newToolSurfaceReceiptHTTPClientForManifest(base *http.Client, manifest toolSurfaceManifest, observer ToolSurfaceReceiptObserver) *http.Client {
	return newToolSurfaceReceiptHTTPClientForManifestWithAuditEvidence(base, manifest, ToolSurfacePlanEvidence{}, observer)
}

// newToolSurfaceReceiptHTTPClientForManifestWithAuditEvidence binds the
// already-normalized request-owned audit evidence to the final wire verifier.
// Evidence never affects the callable payload or authorization; retaining it
// here only prevents the manifest event and receipt from describing different
// audit facts for the same outbound request.
func newToolSurfaceReceiptHTTPClientForManifestWithAuditEvidence(base *http.Client, manifest toolSurfaceManifest, evidence ToolSurfacePlanEvidence, observer ToolSurfaceReceiptObserver) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	next := clone.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	clone.Transport = toolSurfaceReceiptRoundTripper{next: next, manifest: manifest, evidence: evidence, observer: observer}
	// A redirect is a second HTTP request. It cannot inherit this request's
	// rendered surface because RunLoop did not create a fresh plan/receipt for
	// the redirect target. Override even a caller-provided redirect policy so a
	// tool-bearing request fails at the first response rather than silently
	// following 301/302/303/307/308.
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fmt.Errorf("tool_surface_redirect_blocked: tool-bearing request requires a fresh surface")
	}
	return &clone
}

type toolSurfaceReceiptRoundTripper struct {
	next     http.RoundTripper
	manifest toolSurfaceManifest
	evidence ToolSurfacePlanEvidence
	observer ToolSurfaceReceiptObserver
}

func (transport toolSurfaceReceiptRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.Body == nil {
		return nil, transport.reject("surface_integrity_failure: missing outbound request body", 0, "")
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, transport.reject("surface_integrity_failure: read outbound request body", 0, "")
	}
	_ = request.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, transport.reject("surface_integrity_failure: invalid outbound request JSON", 0, "")
	}
	wireTools, toolsPresent, err := toolSurfaceWireDefinitions(payload)
	if err != nil {
		return nil, transport.reject("surface_integrity_failure: "+err.Error(), 0, "")
	}
	// An empty surface is an explicit replacement too. A fresh HTTP request
	// must not rely on a provider interpreting an omitted tools field as a
	// replacement for a prior request's surface.
	if !toolsPresent && transport.manifest.toolCount == 0 {
		payload["tools"] = []interface{}{}
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, transport.reject("surface_integrity_failure: marshal explicit empty surface", 0, "")
		}
		wireTools = nil
		toolsPresent = true
	}
	if !toolsPresent {
		return nil, transport.reject("surface_integrity_failure: outbound payload omitted tools", 0, "")
	}
	policy, policyErr := toolSurfaceInvocationPolicyFromPayload(payload, transport.manifest.policy.Envelope)
	if policyErr != nil {
		return nil, transport.reject("surface_integrity_failure: "+policyErr.Error(), len(wireTools), "")
	}
	receipt := transport.manifest.receiptForWirePayloadWithAuditEvidence(wireTools, policy, transport.evidence)
	if !receipt.Verified {
		return nil, transport.reject(receipt.Failure, receipt.WireToolCount, receipt.WirePayloadDigest)
	}
	// The following RoundTrip invokes the concrete transport for this exact
	// request. A later transport error is still potentially ambiguous, but any
	// observer can distinguish a verified pre-serialization failure from a
	// request handed to transport.
	receipt.Handoff = ToolSurfaceHandoffStarted
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	// A standard-library Transport is allowed to retry a replayable request on
	// a reused connection. Supplying GetBody here would let such a retry resend
	// this exact tool payload without returning to RunLoop for a fresh
	// manifest/receipt/terminal. Keep the body deliberately non-rewindable. A
	// retry-worthy error after the body is consumed must return to the owner,
	// which creates a new request surface. HTTP/2 may reuse this request only
	// when its connection becomes unusable before reading the body, which is not
	// a second outbound payload handoff.
	request.GetBody = nil
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		// The transport was invoked with bytes, but a write/read error cannot
		// prove whether the provider observed the request. Do not report this as
		// a clean handoff or let a retry reuse the same request surface.
		receipt.Handoff = ToolSurfaceHandoffAmbiguous
	}
	if transport.observer != nil {
		transport.observer.OnToolSurfaceReceipt(receipt)
	}
	// Disabling GetBody makes the standard client decline 307/308 replay, but
	// it would otherwise return the redirect response as an ordinary provider
	// result. Reject every redirect status at this final boundary so callers
	// cannot mistake it for a provider response or construct a successor from
	// it without returning through RunLoop's fresh-surface lifecycle.
	if err == nil && isToolSurfaceRedirectResponse(response) {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("tool_surface_redirect_blocked: tool-bearing request requires a fresh surface")
	}
	return response, err
}

func isToolSurfaceRedirectResponse(response *http.Response) bool {
	if response == nil {
		return false
	}
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// toolSurfaceInvocationPolicyFromPayload observes only the final serialized
// fields that control tool invocation. It intentionally treats absent and
// explicit false as different values.
func toolSurfaceInvocationPolicyFromPayload(payload map[string]interface{}, envelope ToolSurfaceEnvelope) (ToolSurfaceInvocationPolicy, error) {
	policy := DefaultToolSurfaceInvocationPolicy(envelope)
	if raw, found := payload["tool_choice"]; found {
		choice, err := toolSurfaceToolChoiceFromWire(raw, envelope)
		if err != nil {
			return ToolSurfaceInvocationPolicy{}, err
		}
		policy.ToolChoice = choice
	}
	if raw, found := payload["parallel_tool_calls"]; found {
		value, ok := raw.(bool)
		if !ok {
			return ToolSurfaceInvocationPolicy{}, fmt.Errorf("parallel_tool_calls must be boolean")
		}
		policy.ParallelToolCalls = ToolSurfaceOptionalBool{Present: true, Value: value}
	}
	return normalizeToolSurfaceInvocationPolicy(policy)
}

func toolSurfaceToolChoiceFromWire(raw interface{}, envelope ToolSurfaceEnvelope) (ToolSurfaceToolChoice, error) {
	if value, ok := raw.(string); ok {
		switch strings.TrimSpace(value) {
		case ToolSurfaceToolChoiceAuto, ToolSurfaceToolChoiceRequired, ToolSurfaceToolChoiceNone:
			return ToolSurfaceToolChoice{Mode: strings.TrimSpace(value)}, nil
		default:
			return ToolSurfaceToolChoice{}, fmt.Errorf("unsupported tool_choice %q", value)
		}
	}
	choice := asStringInterfaceMap(raw)
	if choice == nil || strings.TrimSpace(toolSurfaceStringValue(choice["type"])) != "function" {
		return ToolSurfaceToolChoice{}, fmt.Errorf("tool_choice must be a supported string or function selector")
	}
	var name string
	if envelope == ToolSurfaceEnvelopeOpenAIChat {
		function := asStringInterfaceMap(choice["function"])
		name = strings.TrimSpace(toolSurfaceStringValue(function["name"]))
	} else {
		name = strings.TrimSpace(toolSurfaceStringValue(choice["name"]))
	}
	if name == "" {
		return ToolSurfaceToolChoice{}, fmt.Errorf("specific tool_choice requires a function name")
	}
	return ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: name}, nil
}

func toolSurfaceStringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func toolSurfaceWireDefinitions(payload map[string]interface{}) ([]map[string]interface{}, bool, error) {
	raw, present := payload["tools"]
	if !present {
		return nil, false, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, true, fmt.Errorf("outbound tools must be an array")
	}
	definitions := make([]map[string]interface{}, 0, len(items))
	for index, rawDefinition := range items {
		definition, ok := rawDefinition.(map[string]interface{})
		if !ok {
			return nil, true, fmt.Errorf("outbound tool %d is not an object", index)
		}
		definitions = append(definitions, definition)
	}
	return definitions, true, nil
}

func (transport toolSurfaceReceiptRoundTripper) reject(failure string, wireToolCount int, wireDigest string) error {
	if transport.observer != nil {
		receipt := transport.manifest.receiptForAuditEvidence(transport.evidence)
		receipt.WirePayloadDigest = wireDigest
		receipt.WirePayloadHash = wireDigest
		receipt.WireToolCount = wireToolCount
		receipt.Failure = failure
		receipt.FailureKind = ToolSurfaceFailureIntegrity
		transport.observer.OnToolSurfaceReceipt(receipt)
	}
	return fmt.Errorf("%s", failure)
}
