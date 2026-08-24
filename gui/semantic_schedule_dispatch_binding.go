package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const scheduleDispatchFireSelectionID = "selection:schedule-fire"

// managedScheduleDispatchFireWired reports that due-time fire can CAS-claim a
// per-run DeliveryRecord for a host-owned binding. The original create-turn
// record stays prepared-only; each occurrence gets its own operation.
func managedScheduleDispatchFireWired() bool { return true }

// scheduleDispatchBinding is a host-owned durable link from a local scheduled
// job to the trusted inbound destination captured when dispatch prepared.
// It is never written onto ScheduledTask.Delivery and never reads group names
// from user text or model arguments.
type scheduleDispatchBinding struct {
	TaskID        string    `json:"task_id"`
	ChannelScope  string    `json:"channel_scope"`
	DestinationID string    `json:"destination_id"`
	PrincipalID   string    `json:"principal_id,omitempty"`
	BoundAt       time.Time `json:"bound_at"`
}

type scheduleDispatchBindingStore struct {
	mu       sync.Mutex
	bindings map[string]scheduleDispatchBinding
	path     string
}

func newScheduleDispatchBindingStore(path string) *scheduleDispatchBindingStore {
	store := &scheduleDispatchBindingStore{bindings: map[string]scheduleDispatchBinding{}, path: strings.TrimSpace(path)}
	if store.path != "" {
		if data, err := os.ReadFile(store.path); err == nil && len(data) > 0 {
			var loaded map[string]scheduleDispatchBinding
			if json.Unmarshal(data, &loaded) == nil && loaded != nil {
				store.bindings = loaded
			}
		}
	}
	return store
}

func (s *scheduleDispatchBindingStore) Put(binding scheduleDispatchBinding) error {
	if s == nil {
		return fmt.Errorf("schedule_dispatch_binding_store_unavailable")
	}
	binding.TaskID = strings.TrimSpace(binding.TaskID)
	binding.ChannelScope = strings.TrimSpace(binding.ChannelScope)
	binding.DestinationID = strings.TrimSpace(binding.DestinationID)
	binding.PrincipalID = strings.TrimSpace(binding.PrincipalID)
	if binding.TaskID == "" || !semanticTrustedDispatchDestination(binding.DestinationID) {
		return fmt.Errorf("schedule_dispatch_binding_invalid")
	}
	if binding.BoundAt.IsZero() {
		binding.BoundAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[binding.TaskID] = binding
	return s.persistLocked()
}

func (s *scheduleDispatchBindingStore) Get(taskID string) (scheduleDispatchBinding, bool) {
	if s == nil {
		return scheduleDispatchBinding{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[strings.TrimSpace(taskID)]
	return binding, ok
}

func (s *scheduleDispatchBindingStore) Delete(taskID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, strings.TrimSpace(taskID))
	_ = s.persistLocked()
}

func (s *scheduleDispatchBindingStore) persistLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.bindings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func scheduleDispatchFireChannel(channelScope string) (string, bool) {
	switch normalizeIMMessagePlatformKind(channelScope) {
	case imMessagePlatformLansenger, imMessagePlatformLansengerLocal:
		return scheduler.DeliveryChannelLansenger, true
	default:
		return "", false
	}
}

func trustedDestinationToSchedulerTarget(destinationID string) (scheduler.DeliveryTarget, error) {
	destinationID = strings.TrimSpace(destinationID)
	switch {
	case strings.HasPrefix(destinationID, "group:") && len(destinationID) > len("group:"):
		return scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindGroup, GroupID: strings.TrimPrefix(destinationID, "group:")}, nil
	case strings.HasPrefix(destinationID, "user:") && len(destinationID) > len("user:"):
		return scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindUser, UserID: strings.TrimPrefix(destinationID, "user:")}, nil
	default:
		return scheduler.DeliveryTarget{}, fmt.Errorf("trusted_delivery_target_missing")
	}
}

type scheduleDispatchFireDeps struct {
	Bindings    *scheduleDispatchBindingStore
	Store       tool.ArtifactStore
	Coordinator *tool.SQLiteSemanticExecutionCoordinator
	Send        func(context.Context, string, []scheduler.DeliveryTarget, string) error
	Now         func() time.Time
}

func scheduleDispatchFireOutcome(err error) tool.DeliveryState {
	if err == nil {
		// DeliverIMText returning nil is not a channel receipt. Honest
		// settlement is unknown unless a later worker observes a media id.
		return tool.DeliveryUnknown
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "message text is empty") || strings.Contains(message, "no delivery targets") || strings.Contains(message, "app unavailable") || strings.Contains(message, "unsupported channel") {
		return tool.DeliveryFailed
	}
	return tool.DeliveryUnknown
}

// fireManagedScheduleDispatch sends one due-time occurrence for a bound task.
// It never reads ScheduledTask.Delivery, never parses a group name, and never
// reuses the create-turn DeliveryRecord. A failed CAS claim does not send.
func fireManagedScheduleDispatch(ctx context.Context, deps scheduleDispatchFireDeps, task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if deps.Coordinator != nil {
		return fireManagedScheduleDispatchWithCoordinator(ctx, deps, task, resultText, runErr)
	}
	if task == nil || deps.Bindings == nil || deps.Store == nil || deps.Send == nil {
		return nil
	}
	binding, ok := deps.Bindings.Get(task.ID)
	if !ok {
		return nil
	}
	channel, ok := scheduleDispatchFireChannel(binding.ChannelScope)
	if !ok {
		return nil
	}
	target, err := trustedDestinationToSchedulerTarget(binding.DestinationID)
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(resultText)
	if text == "" && runErr != nil {
		text = strings.TrimSpace(runErr.Error())
	}
	if text == "" {
		return nil
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	principal := strings.TrimSpace(binding.PrincipalID)
	if principal == "" {
		principal = "scheduler"
	}
	runID := now.Format("20060102T150405.000000000Z")
	scope := tool.InvocationScope{
		RootTaskID:  "schedule-fire:" + strings.TrimSpace(task.ID),
		PlanID:      "run:" + runID,
		SessionID:   principal,
		TurnID:      "fire:" + runID,
		PrincipalID: principal,
	}
	payload, err := tool.NewArtifactPayload(scope, scheduleDispatchFireSelectionID, "document", "text/plain", base64.StdEncoding.EncodeToString([]byte(text)), now)
	if err != nil {
		return err
	}
	ref, err := deps.Store.Publish(payload)
	if err != nil {
		return err
	}
	_, err = deps.Store.PrepareDelivery(tool.DeliveryRecord{
		Scope: scope, SelectionID: scheduleDispatchFireSelectionID, ArtifactID: ref.ID, ArtifactSourceScope: ref.Scope,
		ChannelScope: binding.ChannelScope, DestinationID: binding.DestinationID, State: tool.DeliveryPrepared, CreatedAt: now,
	})
	if err != nil {
		return err
	}
	_, claimed, err := deps.Store.ClaimDeliveryDispatch(scope, scheduleDispatchFireSelectionID, now)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	sendErr := deps.Send(ctx, channel, []scheduler.DeliveryTarget{target}, text)
	outcome := scheduleDispatchFireOutcome(sendErr)
	if _, recErr := deps.Store.RecordDeliveryOutcome(scope, scheduleDispatchFireSelectionID, outcome); recErr != nil && sendErr == nil {
		return recErr
	}
	return sendErr
}

func fireManagedScheduleDispatchWithCoordinator(ctx context.Context, deps scheduleDispatchFireDeps, task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if task == nil || deps.Bindings == nil || deps.Coordinator == nil || deps.Send == nil {
		return nil
	}
	binding, ok := deps.Bindings.Get(task.ID)
	if !ok {
		return nil
	}
	channel, ok := scheduleDispatchFireChannel(binding.ChannelScope)
	if !ok {
		return nil
	}
	target, err := trustedDestinationToSchedulerTarget(binding.DestinationID)
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(resultText)
	if text == "" && runErr != nil {
		text = strings.TrimSpace(runErr.Error())
	}
	if text == "" {
		return nil
	}
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	principal := strings.TrimSpace(binding.PrincipalID)
	if principal == "" {
		principal = "scheduler"
	}
	runID := now.Format("20060102T150405.000000000Z")
	scope := tool.InvocationScope{
		RootTaskID:  "schedule-fire:" + strings.TrimSpace(task.ID),
		PlanID:      "run:" + runID,
		SessionID:   principal,
		TurnID:      "fire:" + runID,
		PrincipalID: principal,
	}
	payload, err := tool.NewArtifactPayload(scope, scheduleDispatchFireSelectionID, "document", "text/plain", base64.StdEncoding.EncodeToString([]byte(text)), now)
	if err != nil {
		return err
	}
	if _, err := deps.Coordinator.Artifacts.Publish(payload); err != nil {
		return err
	}
	if _, err := deps.Coordinator.PrepareStandaloneDelivery(tool.DeliveryRecord{
		Scope: scope, SelectionID: scheduleDispatchFireSelectionID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: binding.ChannelScope, DestinationID: binding.DestinationID, State: tool.DeliveryPrepared, CreatedAt: now,
	}, now); err != nil {
		return err
	}
	_, claimed, err := deps.Coordinator.ClaimDelivery(scope, scheduleDispatchFireSelectionID, now)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	sendErr := deps.Send(ctx, channel, []scheduler.DeliveryTarget{target}, text)
	outcome := scheduleDispatchFireOutcome(sendErr)
	if _, recErr := deps.Coordinator.SettleStandaloneDelivery(scope, scheduleDispatchFireSelectionID, outcome, "", "schedule_dispatch_"+string(outcome), now); recErr != nil && sendErr == nil {
		return recErr
	}
	return sendErr
}

func (h *IMMessageHandler) rememberAdministeredTaskID(id string) {
	if h == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	h.semanticAdministeredTaskMu.Lock()
	h.semanticLastAdministeredTaskID = id
	h.semanticAdministeredTaskMu.Unlock()
}

func (h *IMMessageHandler) takeAdministeredTaskID() string {
	if h == nil {
		return ""
	}
	h.semanticAdministeredTaskMu.Lock()
	id := strings.TrimSpace(h.semanticLastAdministeredTaskID)
	h.semanticLastAdministeredTaskID = ""
	h.semanticAdministeredTaskMu.Unlock()
	return id
}

func (h *IMMessageHandler) scheduleDispatchBindingStore() *scheduleDispatchBindingStore {
	if h == nil {
		return nil
	}
	if h.app != nil {
		return h.app.scheduleDispatchBindingStore()
	}
	if h.scheduleDispatchBindings == nil {
		h.scheduleDispatchBindings = newScheduleDispatchBindingStore("")
	}
	return h.scheduleDispatchBindings
}

func (h *IMMessageHandler) bindScheduleDispatch(taskID, channelScope, destinationID, principalID string) error {
	store := h.scheduleDispatchBindingStore()
	if store == nil {
		return fmt.Errorf("schedule_dispatch_binding_store_unavailable")
	}
	return store.Put(scheduleDispatchBinding{
		TaskID: taskID, ChannelScope: channelScope, DestinationID: destinationID, PrincipalID: principalID, BoundAt: time.Now().UTC(),
	})
}

func (h *IMMessageHandler) unbindScheduleDispatch(taskID string) {
	if store := h.scheduleDispatchBindingStore(); store != nil {
		store.Delete(taskID)
	}
}

func (a *App) scheduleDispatchBindingStore() *scheduleDispatchBindingStore {
	if a == nil {
		return nil
	}
	a.scheduleDispatchBindingsOnce.Do(func() {
		path := ""
		if strings.TrimSpace(a.testHomeDir) != "" {
			path = filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "schedule-dispatch-bindings.json")
		} else if base, ok := a.effectiveBaseDir.Load().(string); ok && strings.TrimSpace(base) != "" {
			path = filepath.Join(strings.TrimSpace(base), "semantic-routing", "schedule-dispatch-bindings.json")
		}
		a.scheduleDispatchBindings = newScheduleDispatchBindingStore(path)
	})
	return a.scheduleDispatchBindings
}

func (a *App) deliverManagedScheduleDispatch(ctx context.Context, task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if a == nil {
		return nil
	}
	store, err := a.semanticArtifactStoreForApp()
	if err != nil || store == nil {
		return err
	}
	coordinator, _ := a.semanticExecutionCoordinatorForApp()
	return fireManagedScheduleDispatch(ctx, scheduleDispatchFireDeps{
		Bindings:    a.scheduleDispatchBindingStore(),
		Store:       store,
		Coordinator: coordinator,
		Send: func(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
			return a.DeliverIMText(ctx, channel, targets, text)
		},
	}, task, resultText, runErr)
}
