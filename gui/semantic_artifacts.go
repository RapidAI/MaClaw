package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticArtifactBroker is deliberately only a scoped façade over the common
// durable ArtifactStore. It has no in-memory artifact map: a restarted surface
// must resolve the same ArtifactRef rather than silently losing capture output
// or inventing a new delivery result from callback-local base64.
type semanticArtifactBroker struct {
	scope       tool.InvocationScope
	store       tool.ArtifactStore
	routes      tool.RouteStateStore
	coordinator *tool.SQLiteSemanticExecutionCoordinator
}

// consumeTrustedInput resolves one exact planner-selected host input. It never
// accepts a model-provided artifact ID, filesystem location, or source scope:
// all provenance is embedded in the immutable plan dependency. This is shared
// by future attachment consumers; the caller decides how executor-private
// bytes are safely adapted for its native implementation.
func (b *semanticArtifactBroker) consumeTrustedInput(consumer tool.PlannedSelection, contract tool.ArtifactContract) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	dependencies := make([]tool.ArtifactDependency, 0, 1)
	for _, dependency := range consumer.ArtifactDependencies {
		if strings.TrimSpace(dependency.ArtifactID) != "" && artifactContractMatches(dependency.Contract, contract) {
			dependencies = append(dependencies, dependency)
		}
	}
	if len(dependencies) == 0 {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_unbound")
	}
	if len(dependencies) != 1 {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_ambiguous")
	}
	dependency := dependencies[0]
	if dependency.Artifact.ID != dependency.ArtifactID || !artifactContractMatches(tool.ArtifactContract{Kind: dependency.Artifact.Kind, MIMEType: dependency.Artifact.MIMEType, Required: true}, contract) {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_invalid")
	}
	grant, err := b.store.IssueProjectedAccessGrant(dependency.Artifact.ArtifactRef(), b.scope, strings.TrimSpace(consumer.ID), contract, time.Minute)
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	payload, err := b.store.ConsumeAccessGrant(grant, contract)
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	return payload, nil
}

// materializeTrustedDocument writes already-authorized bytes to a short-lived
// executor-private file. The model never receives this path or an input ID.
func materializeTrustedDocument(payload tool.ArtifactPayload, suffix string) (string, func(), error) {
	bytes, err := base64.StdEncoding.DecodeString(payload.Base64)
	if err != nil || len(bytes) == 0 {
		return "", nil, fmt.Errorf("trusted_document_payload_invalid")
	}
	file, err := os.CreateTemp("", "semantic-document-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("trusted_document_temp_create_failed")
	}
	path := filepath.Clean(file.Name())
	if _, err := file.Write(bytes); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("trusted_document_temp_write_failed")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("trusted_document_temp_close_failed")
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// semanticDeliveryProjection carries only trusted execution-plane identity
// across the response projection. Neither the model nor an IM payload can
// nominate a selection ID, scope, or delivery state.
type semanticDeliveryProjection struct {
	Scope         tool.InvocationScope
	SelectionID   string
	Store         tool.ArtifactStore
	Executor      *tool.PlanExecutor
	Coordinator   *tool.SQLiteSemanticExecutionCoordinator
	ChannelScope  string
	DestinationID string
	OnSettled     func(tool.DeliveryState)
}

func (p *semanticDeliveryProjection) recordOutcome(outcome tool.DeliveryState, receiptDigest ...string) error {
	if p == nil || p.Store == nil || p.Executor == nil {
		return fmt.Errorf("semantic delivery projection is unavailable")
	}
	if p.Coordinator != nil {
		digest := ""
		if len(receiptDigest) > 0 {
			digest = strings.TrimSpace(receiptDigest[0])
		}
		_, err := p.Coordinator.SettleDelivery(p.Scope, p.SelectionID, outcome, digest, "channel_delivery_"+string(outcome), time.Now().UTC())
		if err == nil && p.OnSettled != nil {
			p.OnSettled(outcome)
		}
		return err
	}
	record, err := p.Store.RecordDeliveryOutcome(p.Scope, p.SelectionID, outcome)
	if err != nil {
		return err
	}
	state := tool.PlanExecutionUnknown
	switch outcome {
	case tool.DeliveryAccepted:
		state = tool.PlanExecutionSucceeded
	case tool.DeliveryFailed:
		state = tool.PlanExecutionFailed
	case tool.DeliveryUnknown:
		state = tool.PlanExecutionUnknown
	default:
		return fmt.Errorf("semantic delivery outcome is invalid")
	}
	_, err = p.Executor.SettleAwaitingReceipt(p.Scope, p.SelectionID, state, tool.SchemaDigest([]byte(record.OperationKey)), "channel_delivery_"+string(outcome))
	if err == nil && p.OnSettled != nil {
		p.OnSettled(outcome)
	}
	return err
}

func newSemanticArtifactBroker(scope tool.InvocationScope, store tool.ArtifactStore, routes tool.RouteStateStore, coordinators ...*tool.SQLiteSemanticExecutionCoordinator) *semanticArtifactBroker {
	var coordinator *tool.SQLiteSemanticExecutionCoordinator
	if len(coordinators) > 0 {
		coordinator = coordinators[0]
	}
	return &semanticArtifactBroker{scope: scope, store: store, routes: routes, coordinator: coordinator}
}

func (b *semanticArtifactBroker) newPNGPayload(producerSelection, imageBase64 string) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(imageBase64))
	if err != nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact PNG decode failed: %w", err)
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact is not a PNG image")
	}
	payload, err := tool.NewArtifactPayload(b.scope, producerSelection, "image", "image/png", imageBase64, time.Now().UTC())
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	return payload, nil
}

// publishPNG remains the legacy/non-coordinated path. App-backed semantic
// execution keeps the payload in the callback until CompleteWithArtifactPayloads
// commits it together with the producer's execution and route facts.
func (b *semanticArtifactBroker) newPDFPayload(producerSelection, pdfBase64, name string) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pdfBase64))
	if err != nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact PDF decode failed: %w", err)
	}
	if len(data) < 4 || string(data[:4]) != "%PDF" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact is not a PDF document")
	}
	payload, err := tool.NewArtifactPayload(b.scope, producerSelection, "document", "application/pdf", pdfBase64, time.Now().UTC())
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	payload.Ref.Name = strings.TrimSpace(name)
	return payload, nil
}

func (b *semanticArtifactBroker) newWAVPayload(producerSelection, wavBase64 string) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(wavBase64))
	if err != nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact WAV decode failed: %w", err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact is not a WAV audio file")
	}
	payload, err := tool.NewArtifactPayload(b.scope, producerSelection, "audio", "audio/wav", wavBase64, time.Now().UTC())
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	return payload, nil
}

func (b *semanticArtifactBroker) publishWAV(producerSelection, wavBase64 string) (tool.ArtifactRef, error) {
	payload, err := b.newWAVPayload(producerSelection, wavBase64)
	if err != nil {
		return tool.ArtifactRef{}, err
	}
	return b.store.Publish(payload)
}

func (b *semanticArtifactBroker) publishPDF(producerSelection, pdfBase64, name string) (tool.ArtifactRef, error) {
	payload, err := b.newPDFPayload(producerSelection, pdfBase64, name)
	if err != nil {
		return tool.ArtifactRef{}, err
	}
	return b.store.Publish(payload)
}

// newOfficeDocumentPayload registers a host-written spreadsheet or
// presentation. The office adapter wrote the bytes to a workspace path rather
// than returning a [file_base64] marker, so the caller reads the file back;
// both formats are ZIP containers and are validated by their magic bytes.
func (b *semanticArtifactBroker) newOfficeDocumentPayload(producerSelection, dataBase64, mimeType, name string) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation":
	default:
		return tool.ArtifactPayload{}, fmt.Errorf("artifact is not an office document")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(dataBase64))
	if err != nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact office document decode failed: %w", err)
	}
	if len(data) < 4 || string(data[:2]) != "PK" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact is not an office document")
	}
	payload, err := tool.NewArtifactPayload(b.scope, producerSelection, "document", mimeType, dataBase64, time.Now().UTC())
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	payload.Ref.Name = strings.TrimSpace(name)
	return payload, nil
}

func (b *semanticArtifactBroker) publishOfficeDocument(producerSelection, dataBase64, mimeType, name string) (tool.ArtifactRef, error) {
	payload, err := b.newOfficeDocumentPayload(producerSelection, dataBase64, mimeType, name)
	if err != nil {
		return tool.ArtifactRef{}, err
	}
	return b.store.Publish(payload)
}

func (b *semanticArtifactBroker) publishPNG(producerSelection, imageBase64 string) (tool.ArtifactRef, error) {
	payload, err := b.newPNGPayload(producerSelection, imageBase64)
	if err != nil {
		return tool.ArtifactRef{}, err
	}
	return b.store.Publish(payload)
}

// registerPublished records a payload only after PlanExecutor has durably
// committed the producer selection as succeeded. Keeping publication and the
// execution fact ordered avoids treating a callback-local artifact as proof of
// a completed selection. A crash between those two stores is recovered by
// RouteStateStore.ReconcileCurrentArtifacts on the next revision publication.
func (b *semanticArtifactBroker) registerPublished(ref tool.ArtifactRef) error {
	if b == nil || b.routes == nil {
		return fmt.Errorf("artifact route state is unavailable")
	}
	if ref.Scope != b.scope {
		return fmt.Errorf("artifact route scope mismatch")
	}
	_, err := b.routes.RecordArtifact(b.scope, b.scope.PlanID, ref, time.Now().UTC())
	return err
}

func (b *semanticArtifactBroker) consumeImagePNG(consumer tool.PlannedSelection, contract tool.ArtifactContract) (tool.ArtifactPayload, error) {
	if !strings.EqualFold(strings.TrimSpace(contract.Kind), "image") || !strings.EqualFold(strings.TrimSpace(contract.MIMEType), "image/png") {
		return tool.ArtifactPayload{}, fmt.Errorf("selection is not authorized to consume an image/png artifact")
	}
	return b.consumePlannedArtifact(consumer, contract)
}

// consumePlannedArtifact is the common consumer boundary for producer output
// registered in RouteState and host-published input bound directly in a plan.
// In either case, a consumer gets exactly one opaque grant for the immutable
// dependency; there is no MIME-scoped or newest-artifact lookup.
func (b *semanticArtifactBroker) consumePlannedArtifact(consumer tool.PlannedSelection, contract tool.ArtifactContract) (tool.ArtifactPayload, error) {
	if b == nil || b.store == nil || b.routes == nil {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact broker is unavailable")
	}
	dependencies := make([]tool.ArtifactDependency, 0, 1)
	for _, dependency := range consumer.ArtifactDependencies {
		if artifactContractMatches(dependency.Contract, contract) {
			dependencies = append(dependencies, dependency)
		}
	}
	if len(dependencies) == 0 {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_unbound")
	}
	if len(dependencies) != 1 {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_ambiguous")
	}
	dependency := dependencies[0]
	if dependency.ArtifactID != "" {
		return b.consumeTrustedInput(consumer, contract)
	}
	if strings.TrimSpace(dependency.ProducerSelection) == "" || strings.TrimSpace(consumer.ID) == "" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_invalid")
	}
	refs, err := b.routes.ArtifactRefs(b.scope)
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	// Producer identity for a repeat family is the family, not one invocation:
	// a draft-then-revise office turn publishes one artifact per sibling, and
	// the delivery bound to the first sibling must still reach the revision
	// (production 2026-08-26: the revision deck was invisible to send_file).
	// Within the family the newest artifact is the latest revision of the same
	// meaning — that is the one to deliver. Across families the ambiguity
	// rule is unchanged.
	producerFamily := tool.RepeatFamilyID(dependency.ProducerSelection)
	var source tool.ArtifactRef
	for _, candidate := range refs {
		ref := tool.ArtifactRef{ID: candidate.ArtifactID, Kind: candidate.Kind, MIMEType: candidate.MIMEType, IntegrityDigest: candidate.IntegrityDigest, ProducerSelection: candidate.ProducerSelection, Scope: candidate.SourceScope, CreatedAt: candidate.CreatedAt}
		if tool.RepeatFamilyID(ref.ProducerSelection) != producerFamily || !artifactContractMatches(tool.ArtifactContract{Kind: ref.Kind, MIMEType: ref.MIMEType, Required: true}, contract) {
			continue
		}
		if source.ID != "" && !ref.CreatedAt.After(source.CreatedAt) {
			continue
		}
		source = ref
	}
	if source.ID == "" {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_missing")
	}
	grant, err := b.store.IssueProjectedAccessGrant(source, b.scope, strings.TrimSpace(consumer.ID), contract, time.Minute)
	if err != nil {
		return tool.ArtifactPayload{}, err
	}
	payload, err := b.store.ConsumeAccessGrant(grant, contract)
	if err == tool.ErrArtifactNotFound {
		return tool.ArtifactPayload{}, fmt.Errorf("artifact_dependency_missing")
	}
	return payload, err
}

func artifactContractMatches(left, right tool.ArtifactContract) bool {
	return strings.EqualFold(strings.TrimSpace(left.Kind), strings.TrimSpace(right.Kind)) &&
		(strings.TrimSpace(right.MIMEType) == "" || strings.EqualFold(strings.TrimSpace(left.MIMEType), strings.TrimSpace(right.MIMEType)))
}

func (b *semanticArtifactBroker) prepareCurrentChannelDelivery(selection tool.PlannedSelection, channelScope, destinationID string, contract tool.ArtifactContract) (tool.ArtifactPayload, tool.DeliveryRecord, error) {
	payload, err := b.consumePlannedArtifact(selection, contract)
	if err != nil {
		// A storage-level "no rows" is the domain fact "nothing produced is
		// waiting for delivery" (production 2026-08-26: send_file retried after
		// the office grant was denied, and the model received the raw string
		// "sql: no rows in result set"). Translate at the broker boundary so
		// the model gets an actionable domain rejection, never storage guts.
		if errors.Is(err, sql.ErrNoRows) {
			return tool.ArtifactPayload{}, tool.DeliveryRecord{}, fmt.Errorf("artifact_dependency_missing: no produced artifact is waiting for delivery; produce or acquire the artifact first")
		}
		return tool.ArtifactPayload{}, tool.DeliveryRecord{}, err
	}
	delivery := tool.DeliveryRecord{
		Scope: b.scope, SelectionID: strings.TrimSpace(selection.ID), ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: strings.TrimSpace(channelScope), DestinationID: strings.TrimSpace(destinationID), State: tool.DeliveryPrepared, CreatedAt: time.Now().UTC(),
	}
	// The unified coordinator combines this intent with the host-call terminal
	// projection after the adapter returns. Do not create a standalone outbox
	// record here: doing so would reintroduce a crash window between prepare and
	// completion. The returned record is an executor-private intent only.
	if b.coordinator != nil {
		return payload, delivery, nil
	}
	var record tool.DeliveryRecord
	record, err = b.store.PrepareDelivery(delivery)
	if err != nil {
		return tool.ArtifactPayload{}, tool.DeliveryRecord{}, err
	}
	return payload, record, nil
}

// prepareScheduleDispatchIntent publishes a host-owned due-time dispatch
// intent and prepares a DeliveryRecord. It does not SendMedia. The target is
// taken only from the trusted transport destination.
func (b *semanticArtifactBroker) prepareScheduleDispatchIntent(selection tool.PlannedSelection, channelScope, destinationID string) (tool.DeliveryRecord, error) {
	if b == nil || b.store == nil {
		return tool.DeliveryRecord{}, fmt.Errorf("artifact broker is unavailable")
	}
	channelScope = strings.TrimSpace(channelScope)
	destinationID = strings.TrimSpace(destinationID)
	if channelScope == "" || !semanticTrustedDispatchDestination(destinationID) {
		return tool.DeliveryRecord{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	body, err := json.Marshal(map[string]string{
		"kind":           "schedule_dispatch_intent",
		"channel_scope":  channelScope,
		"destination_id": destinationID,
	})
	if err != nil {
		return tool.DeliveryRecord{}, err
	}
	payload, err := tool.NewArtifactPayload(b.scope, strings.TrimSpace(selection.ID), "document", "application/json", base64.StdEncoding.EncodeToString(body), time.Now().UTC())
	if err != nil {
		return tool.DeliveryRecord{}, err
	}
	ref, err := b.store.Publish(payload)
	if err != nil {
		return tool.DeliveryRecord{}, err
	}
	delivery := tool.DeliveryRecord{
		Scope: b.scope, SelectionID: strings.TrimSpace(selection.ID), ArtifactID: ref.ID, ArtifactSourceScope: ref.Scope,
		ChannelScope: channelScope, DestinationID: destinationID, State: tool.DeliveryPrepared, CreatedAt: time.Now().UTC(),
	}
	if b.coordinator != nil {
		return delivery, nil
	}
	return b.store.PrepareDelivery(delivery)
}

func (b *semanticArtifactBroker) prepareTrustedMessageSend(selection tool.PlannedSelection, channelScope, destinationID, text string) (tool.DeliveryRecord, error) {
	if b == nil || b.store == nil {
		return tool.DeliveryRecord{}, fmt.Errorf("artifact broker is unavailable")
	}
	channelScope = strings.TrimSpace(channelScope)
	destinationID = strings.TrimSpace(destinationID)
	text = strings.TrimSpace(text)
	if channelScope == "" || !semanticTrustedDispatchDestination(destinationID) || text == "" {
		return tool.DeliveryRecord{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	payload, err := tool.NewArtifactPayload(b.scope, strings.TrimSpace(selection.ID), "document", "text/plain", base64.StdEncoding.EncodeToString([]byte(text)), time.Now().UTC())
	if err != nil {
		return tool.DeliveryRecord{}, err
	}
	ref, err := b.store.Publish(payload)
	if err != nil {
		return tool.DeliveryRecord{}, err
	}
	delivery := tool.DeliveryRecord{
		Scope: b.scope, SelectionID: strings.TrimSpace(selection.ID), ArtifactID: ref.ID, ArtifactSourceScope: ref.Scope,
		ChannelScope: channelScope, DestinationID: destinationID, State: tool.DeliveryPrepared, CreatedAt: time.Now().UTC(),
	}
	if b.coordinator != nil {
		return delivery, nil
	}
	return b.store.PrepareDelivery(delivery)
}
