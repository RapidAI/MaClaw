package agentservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const reviewedHostScheduleDispatchFireSelectionID = "selection:schedule-fire"

// ScheduleDispatchBinding is a host-owned durable link from a local scheduled
// job to the trusted inbound destination captured when dispatch prepared.
// It is never written onto ScheduledTask.Delivery.
type ScheduleDispatchBinding struct {
	TaskID        string    `json:"task_id"`
	ChannelScope  string    `json:"channel_scope"`
	DestinationID string    `json:"destination_id"`
	PrincipalID   string    `json:"principal_id,omitempty"`
	BoundAt       time.Time `json:"bound_at"`
}

type ScheduleDispatchBindingStore struct {
	mu       sync.Mutex
	bindings map[string]ScheduleDispatchBinding
	path     string
}

func NewScheduleDispatchBindingStore(path string) *ScheduleDispatchBindingStore {
	store := &ScheduleDispatchBindingStore{bindings: map[string]ScheduleDispatchBinding{}, path: strings.TrimSpace(path)}
	if store.path != "" {
		if data, err := os.ReadFile(store.path); err == nil && len(data) > 0 {
			var loaded map[string]ScheduleDispatchBinding
			if json.Unmarshal(data, &loaded) == nil && loaded != nil {
				store.bindings = loaded
			}
		}
	}
	return store
}

func (s *ScheduleDispatchBindingStore) Put(binding ScheduleDispatchBinding) error {
	if s == nil {
		return fmt.Errorf("schedule_dispatch_binding_store_unavailable")
	}
	binding.TaskID = strings.TrimSpace(binding.TaskID)
	binding.ChannelScope = strings.TrimSpace(binding.ChannelScope)
	binding.DestinationID = strings.TrimSpace(binding.DestinationID)
	binding.PrincipalID = strings.TrimSpace(binding.PrincipalID)
	if binding.TaskID == "" || !reviewedHostTrustedDestination(binding.DestinationID) {
		return fmt.Errorf("schedule_dispatch_binding_invalid")
	}
	if _, ok := reviewedHostScheduleDispatchFireChannel(binding.ChannelScope); !ok {
		return fmt.Errorf("trusted_dispatch_channel_unavailable")
	}
	if binding.BoundAt.IsZero() {
		binding.BoundAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[binding.TaskID] = binding
	return s.persistLocked()
}

func (s *ScheduleDispatchBindingStore) Get(taskID string) (ScheduleDispatchBinding, bool) {
	if s == nil {
		return ScheduleDispatchBinding{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[strings.TrimSpace(taskID)]
	return binding, ok
}

func (s *ScheduleDispatchBindingStore) Armed() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, binding := range s.bindings {
		if !reviewedHostTrustedDestination(binding.DestinationID) {
			continue
		}
		if _, ok := reviewedHostScheduleDispatchFireChannel(binding.ChannelScope); !ok {
			continue
		}
		n++
	}
	return n
}

func (s *ScheduleDispatchBindingStore) Delete(taskID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, strings.TrimSpace(taskID))
	_ = s.persistLocked()
}

func (s *ScheduleDispatchBindingStore) persistLocked() error {
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

const reviewedHostScheduleDispatchBindingFile = "schedule-dispatch-bindings.json"

// DiscoverReviewedHostScheduleDispatchDataDirs finds user data directories that
// already have an armed host-owned dispatch binding. It never starts a fire
// executor and never reads user text.
func DiscoverReviewedHostScheduleDispatchDataDirs(roots ...string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if d.Name() != reviewedHostScheduleDispatchBindingFile {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) != "semantic-routing" {
				return nil
			}
			dataDir := filepath.Clean(filepath.Dir(filepath.Dir(path)))
			if dataDir == "" || dataDir == "." {
				return nil
			}
			if NewScheduleDispatchBindingStore(path).Armed() == 0 {
				return nil
			}
			if _, ok := seen[dataDir]; ok {
				return nil
			}
			seen[dataDir] = struct{}{}
			out = append(out, dataDir)
			return nil
		})
	}
	return out
}

func reviewedHostScheduleDispatchFireChannel(channelScope string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(channelScope)) {
	case scheduler.DeliveryChannelLansenger, "lansenger_local", "lansengerlocal", "maclaw":
		return scheduler.DeliveryChannelLansenger, true
	case scheduler.DeliveryChannelWeixin, "weixin_local", "wechat", "wx":
		return scheduler.DeliveryChannelWeixin, true
	case scheduler.DeliveryChannelTelegram, "tg":
		return scheduler.DeliveryChannelTelegram, true
	case scheduler.DeliveryChannelQQ, "qqbot", "qq_bot":
		return scheduler.DeliveryChannelQQ, true
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

type reviewedHostScheduleDispatchFireDeps struct {
	Bindings    *ScheduleDispatchBindingStore
	Coordinator *coretool.SQLiteSemanticExecutionCoordinator
	Send        func(context.Context, string, []scheduler.DeliveryTarget, string) error
	Now         func() time.Time
}

func reviewedHostScheduleDispatchFireOutcome(err error) coretool.DeliveryState {
	if err == nil {
		return coretool.DeliveryUnknown
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "message text is empty") || strings.Contains(message, "no delivery targets") || strings.Contains(message, "unsupported channel") || strings.Contains(message, "unavailable") {
		return coretool.DeliveryFailed
	}
	return coretool.DeliveryUnknown
}

// FireReviewedHostScheduleDispatch sends one due-time occurrence for a bound
// task. It never reads ScheduledTask.Delivery, never parses a group name, and
// never treats a nil send error as accepted.
func FireReviewedHostScheduleDispatch(ctx context.Context, deps reviewedHostScheduleDispatchFireDeps, task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if task == nil || deps.Bindings == nil || deps.Coordinator == nil || deps.Send == nil {
		return nil
	}
	binding, ok := deps.Bindings.Get(task.ID)
	if !ok {
		return nil
	}
	channel, ok := reviewedHostScheduleDispatchFireChannel(binding.ChannelScope)
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
	scope := coretool.InvocationScope{
		RootTaskID:  "schedule-fire:" + strings.TrimSpace(task.ID),
		PlanID:      "run:" + runID,
		SessionID:   principal,
		TurnID:      "fire:" + runID,
		PrincipalID: principal,
	}
	payload, err := coretool.NewArtifactPayload(scope, reviewedHostScheduleDispatchFireSelectionID, "document", "text/plain", base64.StdEncoding.EncodeToString([]byte(text)), now)
	if err != nil {
		return err
	}
	if _, err := deps.Coordinator.Artifacts.Publish(payload); err != nil {
		return err
	}
	if _, err := deps.Coordinator.PrepareStandaloneDelivery(coretool.DeliveryRecord{
		Scope: scope, SelectionID: reviewedHostScheduleDispatchFireSelectionID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: payload.Ref.Scope,
		ChannelScope: binding.ChannelScope, DestinationID: binding.DestinationID, State: coretool.DeliveryPrepared, CreatedAt: now,
	}, now); err != nil {
		return err
	}
	_, claimed, err := deps.Coordinator.ClaimDelivery(scope, reviewedHostScheduleDispatchFireSelectionID, now)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	sendErr := deps.Send(ctx, channel, []scheduler.DeliveryTarget{target}, text)
	outcome := reviewedHostScheduleDispatchFireOutcome(sendErr)
	if _, recErr := deps.Coordinator.SettleStandaloneDelivery(scope, reviewedHostScheduleDispatchFireSelectionID, outcome, "", "schedule_dispatch_"+string(outcome), now); recErr != nil && sendErr == nil {
		return recErr
	}
	return sendErr
}
