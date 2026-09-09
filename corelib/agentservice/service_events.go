package agentservice

import (
	"context"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"strings"
)

// AppendRunEvent persists one Runtime event. Event delivery is best-effort for
// legacy stores; execution result state remains authoritative.
func (s *Service) AppendRunEvent(event RunEvent) (RunEvent, error) {
	if err := s.beginRequest(); err != nil {
		if s != nil {
			s.RuntimeMetrics().RecordEvent(event.TenantID, false, false)
		}
		return RunEvent{}, err
	}
	defer s.activeRequests.Done()
	if s.runEvents == nil {
		s.RuntimeMetrics().RecordEvent(event.TenantID, false, false)
		return RunEvent{}, errors.New("run event store is unavailable")
	}
	replayed := false
	if strings.TrimSpace(event.ID) != "" {
		if resolver, ok := s.runEvents.(runEventSequenceResolver); ok {
			_, replayed, _ = resolver.SequenceForID(event.TenantID, event.UserID, event.RunID, event.ID)
		}
	}
	canonical, err := s.runEvents.Append(event)
	if metrics := s.RuntimeMetrics(); metrics != nil {
		metrics.RecordEvent(event.TenantID, err == nil, replayed && err == nil)
		if err == nil {
			switch canonical.Type {
			case agentruntime.EventToolCall:
				metrics.RecordToolCall(event.TenantID, false)
			case agentruntime.EventToolResult:
				if value, ok := canonical.Payload["error"].(bool); ok && value {
					metrics.RecordToolError(event.TenantID)
				}
			}
		}
	}
	return canonical, err
}

func (s *Service) ListRunEvents(ctx context.Context, p Principal, runID string, after uint64, limit int) ([]RunEvent, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.runEvents == nil {
		return nil, errors.New("run event store is unavailable")
	}
	return s.runEvents.ListAfter(p.TenantID, p.UserID, runID, after, limit)
}

// ListRunEventsForInstance is the scope-checked variant used by HTTP and
// other authenticated transports. It verifies both ownership and the
// instance/run relationship before reading the event outbox, preventing a
// caller from probing an arbitrary run id under the same tenant/user.
func (s *Service) ListRunEventsForInstance(ctx context.Context, p Principal, instanceID, runID string, after uint64, limit int) ([]RunEvent, error) {
	if err := s.beginRequest(); err != nil {
		return nil, err
	}
	defer s.activeRequests.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil || s.runEvents == nil {
		return nil, errors.New("run event store is unavailable")
	}
	run, err := s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)
	if err != nil {
		return nil, err
	}
	if run.ID != runID || run.InstanceID != instanceID {
		return nil, ErrRunNotFound
	}
	events, err := s.runEvents.ListAfter(p.TenantID, p.UserID, runID, after, limit)
	if err != nil {
		return nil, err
	}
	// A custom store may contain legacy rows without instance metadata; retain
	// those rows for compatibility, but never return a row explicitly tagged
	// for another instance.
	filtered := events[:0]
	for _, event := range events {
		if event.RunID != "" && event.RunID != runID {
			continue
		}
		if event.InstanceID != "" && event.InstanceID != instanceID {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}

// RunEventSequenceForID resolves an opaque event id to its per-run sequence
// when the configured store supports cursor lookup. The bool result is false
// for an unknown id and should be treated as a fresh stream by transports.
func (s *Service) RunEventSequenceForID(ctx context.Context, p Principal, runID, eventID string) (uint64, bool, error) {
	if err := s.beginRequest(); err != nil {
		return 0, false, err
	}
	defer s.activeRequests.Done()
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if s == nil || s.runEvents == nil {
		return 0, false, errors.New("run event store is unavailable")
	}
	resolver, ok := s.runEvents.(runEventSequenceResolver)
	if !ok {
		return 0, false, nil
	}
	return resolver.SequenceForID(p.TenantID, p.UserID, runID, eventID)
}

// serviceRuntimeEventSink bridges transport-neutral Runtime events into the
// durable per-run outbox. The sink owns run identity and persistence scope so
// Runtime modules cannot accidentally publish an event for another tenant or
// run. Persistence remains best-effort, matching the legacy callback path.
type serviceRuntimeEventSink struct {
	service *Service
	run     Run
}

func (sink serviceRuntimeEventSink) Emit(ctx context.Context, event agentruntime.Event) error {
	if sink.service == nil {
		return errors.New("agent runtime event sink is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := agentruntime.NormalizeEventEnvelope(event)
	if err != nil {
		return err
	}
	event = normalized
	// The sink owns the authenticated run boundary. Runtime modules may omit
	// optional scope fields, but a supplied tenant/user/instance/session/run
	// value must agree with the run being persisted; otherwise a buggy module
	// could make an event look as if it belonged to another principal while the
	// sink silently rewrote only part of the envelope.
	if event.RunID != "" && event.RunID != sink.run.ID {
		return fmt.Errorf("agent runtime event run id does not match sink run")
	}
	if scope := event.Scope; (scope.TenantID != "" && scope.TenantID != sink.run.TenantID) ||
		(scope.UserID != "" && scope.UserID != sink.run.UserID) ||
		(scope.InstanceID != "" && scope.InstanceID != sink.run.InstanceID) ||
		(scope.SessionID != "" && scope.SessionID != sink.run.SessionID) ||
		(scope.RunID != "" && scope.RunID != sink.run.ID) {
		return fmt.Errorf("agent runtime event scope does not match sink run")
	}
	typ := event.Type
	eventID := event.EventID
	payload := event.Payload
	if correlationID := agentruntime.CorrelationID(ctx); correlationID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["request_id"]; !exists {
			payload["request_id"] = correlationID
		}
	}
	if traceID := agentruntime.TraceID(ctx); traceID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["trace_id"]; !exists {
			payload["trace_id"] = traceID
		}
	}
	if parentSpanID := strings.TrimSpace(sink.run.Metadata["parent_span_id"]); parentSpanID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["parent_span_id"]; !exists {
			payload["parent_span_id"] = parentSpanID
		}
	}
	runSpanID := strings.TrimSpace(sink.run.Metadata["run_span_id"])
	if runSpanID == "" {
		runSpanID = agentruntime.RunSpanID(ctx)
	}
	if runSpanID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["run_span_id"]; !exists {
			payload["run_span_id"] = runSpanID
		}
	}
	occurredAt := event.OccurredAt
	if !occurredAt.IsZero() {
		occurredAt = occurredAt.UTC()
	}
	_, err = sink.service.AppendRunEvent(RunEvent{
		TenantID: sink.run.TenantID, UserID: sink.run.UserID,
		InstanceID: sink.run.InstanceID, SessionID: sink.run.SessionID,
		RunID: sink.run.ID, ID: eventID, Type: typ, SchemaVersion: RunEventSchemaVersion,
		OccurredAt: occurredAt,
		Payload:    payload,
	})
	return err
}

func (s *Service) emitRunEvent(run Run, eventType string, payload map[string]any) {
	if s == nil || strings.TrimSpace(run.ID) == "" {
		return
	}
	_, _ = s.AppendRunEvent(RunEvent{
		TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID,
		SessionID: run.SessionID, RunID: run.ID, Type: eventType,
		SchemaVersion: RunEventSchemaVersion, Payload: payload,
	})
}

// saveRunAdmission is the single service boundary for admitting a user
// message together with its initial running Run. Stores that implement
// RunAdmissionStore can commit both records in one repository operation;
// legacy stores retain the compatible sequential fallback until they migrate.
func (s *Service) saveRunAdmission(message Message, run Run) error {
	if s == nil || s.store == nil {
		return ErrServiceClosed
	}
	if atomicStore, ok := s.store.(RunLifecycleTransactionStore); ok {
		return atomicStore.CommitRunLifecycle(RunLifecycleMutation{
			Phase: RunLifecyclePhaseAdmission, Message: &message, Run: run,
		})
	}
	if atomicStore, ok := s.store.(RunAdmissionStore); ok {
		return atomicStore.SaveRunAdmission(message, run)
	}
	if err := s.store.SaveMessage(message); err != nil {
		return err
	}
	if err := s.store.SaveRun(run); err != nil {
		// Compatibility stores cannot provide a database transaction, but they
		// can still avoid exposing an orphaned user message when the second
		// write fails. Ignore rollback errors and return the authoritative write
		// failure; lifecycle persistence remains advertised as best_effort.
		_, _ = s.store.DeleteMessages(message.SessionID, []string{message.ID})
		return err
	}
	return nil
}

// saveRunAdmissionWithEvents prefers a repository implementation that owns
// the run outbox. The bool reports whether the supplied events were committed
// by that transaction; callers must emit them through the legacy event sink
// only when it is false, preventing duplicate run.started events.
func (s *Service) saveRunAdmissionWithEvents(message Message, run Run, events []RunEvent) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrServiceClosed
	}
	if atomicStore, ok := s.store.(RunLifecycleTransactionStore); ok {
		err := atomicStore.CommitRunLifecycle(RunLifecycleMutation{
			Phase: RunLifecyclePhaseAdmission, Message: &message, Run: run, Events: events,
		})
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	if atomicStore, ok := s.store.(RunAdmissionEventStore); ok {
		err := atomicStore.SaveRunAdmissionWithEvents(message, run, events)
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	return false, s.saveRunAdmission(message, run)
}

func (s *Service) saveRunCompletion(message Message, run Run) error {
	if s == nil || s.store == nil {
		return ErrServiceClosed
	}
	if atomicStore, ok := s.store.(RunLifecycleTransactionStore); ok {
		return atomicStore.CommitRunLifecycle(RunLifecycleMutation{
			Phase: RunLifecyclePhaseCompletion, Message: &message, Run: run,
		})
	}
	if atomicStore, ok := s.store.(RunCompletionStore); ok {
		return atomicStore.SaveRunCompletion(message, run)
	}
	if err := s.store.SaveMessage(message); err != nil {
		return err
	}
	if err := s.store.SaveRun(run); err != nil {
		_, _ = s.store.DeleteMessages(message.SessionID, []string{message.ID})
		return err
	}
	return nil
}

// saveRunCompletionWithEvents is the terminal counterpart to
// saveRunAdmissionWithEvents. It is deliberately additive so custom Store
// implementations can migrate without changing the Store interface.
func (s *Service) saveRunCompletionWithEvents(message Message, run Run, events []RunEvent) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrServiceClosed
	}
	if atomicStore, ok := s.store.(RunLifecycleTransactionStore); ok {
		err := atomicStore.CommitRunLifecycle(RunLifecycleMutation{
			Phase: RunLifecyclePhaseCompletion, Message: &message, Run: run, Events: events,
		})
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	if atomicStore, ok := s.store.(RunCompletionEventStore); ok {
		err := atomicStore.SaveRunCompletionWithEvents(message, run, events)
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	return false, s.saveRunCompletion(message, run)
}

// saveRunTerminalWithEvents covers terminal runs without an assistant
// message (failure/cancellation). A transaction-capable store owns the
// terminal event; legacy stores retain the established sequential fallback.
func (s *Service) saveRunTerminalWithEvents(run Run, events []RunEvent) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrServiceClosed
	}
	if atomicStore, ok := s.store.(RunLifecycleTransactionStore); ok {
		err := atomicStore.CommitRunLifecycle(RunLifecycleMutation{
			Phase: RunLifecyclePhaseTerminal, Run: run, Events: events,
		})
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	if atomicStore, ok := s.store.(RunTerminalEventStore); ok {
		err := atomicStore.SaveRunTerminalWithEvents(run, events)
		if err == nil {
			s.recordCommittedRunEvents(run.TenantID, events)
		}
		return true, err
	}
	return false, s.store.SaveRun(run)
}

func (s *Service) recordCommittedRunEvents(tenantID string, events []RunEvent) {
	metrics := s.RuntimeMetrics()
	if metrics == nil {
		return
	}
	for range events {
		metrics.RecordEvent(tenantID, true, false)
	}
}

func runEventFor(run Run, eventType string, payload map[string]any) RunEvent {
	if correlationID := strings.TrimSpace(run.Metadata["request_id"]); correlationID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["request_id"]; !exists {
			payload["request_id"] = correlationID
		}
	}
	if traceID := strings.TrimSpace(run.Metadata["trace_id"]); traceID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["trace_id"]; !exists {
			payload["trace_id"] = traceID
		}
	}
	if spanID := strings.TrimSpace(run.Metadata["span_id"]); spanID != "" {
		payload = cloneRunEventPayload(payload)
		if payload == nil {
			payload = make(map[string]any)
		}
		if _, exists := payload["span_id"]; !exists {
			payload["span_id"] = spanID
		}
	}
	for _, key := range []string{"parent_span_id", "run_span_id"} {
		if value := strings.TrimSpace(run.Metadata[key]); value != "" {
			payload = cloneRunEventPayload(payload)
			if payload == nil {
				payload = make(map[string]any)
			}
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	return RunEvent{
		TenantID: run.TenantID, UserID: run.UserID, InstanceID: run.InstanceID,
		SessionID: run.SessionID, RunID: run.ID, Type: eventType,
		SchemaVersion: RunEventSchemaVersion, Payload: payload,
	}
}

// runTerminalEventFor gives lifecycle events a deterministic identity. This
// makes an explicit CancelRun request and the executor's eventual cancellation
// callback converge on one outbox row instead of producing duplicate terminal
// events.
func runTerminalEventFor(run Run, eventType string, payload map[string]any) RunEvent {
	event := runEventFor(run, eventType, payload)
	event.ID = "run:" + run.ID + ":" + eventType
	return event
}
