package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	srvHardwareAssistantGeneral = "general"
	srvHardwareAssistantExpert  = "expert"
	srvHardwareInitialPromptMax = 8 * 1024
	srvHardwareRuntimeKey       = "maclawsrv:device-gateway"
)

// srvHardwareExpert is a server-side snapshot of an expert that can safely be
// selected by hardware. It deliberately excludes client-local paths and other
// desktop-only state. A binding only stores its stable ID and resolves the
// current snapshot before every turn.
type srvHardwareExpert struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id"`
	Name         string    `json:"name"`
	SystemPrompt string    `json:"system_prompt"`
	Tools        []string  `json:"tools,omitempty"`
	Skills       []string  `json:"skills,omitempty"`
	Revision     string    `json:"revision"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// srvDeviceAgentBinding is the durable, owner-scoped configuration for one
// physical hardware client. Empty TTSVoiceID means inherit the user's default.
type srvDeviceAgentBinding struct {
	DeviceID      string    `json:"device_id"`
	ClientID      string    `json:"client_id"`
	TenantID      string    `json:"tenant_id"`
	UserID        string    `json:"user_id"`
	AssistantMode string    `json:"assistant_mode"`
	ExpertID      string    `json:"expert_id,omitempty"`
	InitialPrompt string    `json:"initial_prompt,omitempty"`
	TTSVoiceID    string    `json:"tts_voice_id,omitempty"`
	InstanceID    string    `json:"instance_id,omitempty"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// DeletedAt is an owner-scoped revocation tombstone.  We keep no assistant
	// policy in a tombstone, but retain the identity long enough to make an old
	// gateway credential fail closed rather than lazily recreating the device.
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

type srvHardwareBindingUpdate struct {
	AssistantMode string `json:"assistant_mode"`
	ExpertID      string `json:"expert_id,omitempty"`
	InitialPrompt string `json:"initial_prompt,omitempty"`
	TTSVoiceID    string `json:"tts_voice_id,omitempty"`
	Version       int64  `json:"version,omitempty"`
}

type srvHardwareDeviceView struct {
	DeviceID            string    `json:"device_id"`
	ClientID            string    `json:"client_id"`
	AssistantMode       string    `json:"assistant_mode"`
	ExpertID            string    `json:"expert_id,omitempty"`
	ExpertName          string    `json:"expert_name,omitempty"`
	InitialPrompt       string    `json:"initial_prompt,omitempty"`
	TTSVoiceID          string    `json:"tts_voice_id,omitempty"`
	EffectiveTTSVoiceID string    `json:"effective_tts_voice_id"`
	InstanceID          string    `json:"instance_id,omitempty"`
	Status              string    `json:"status"`
	Version             int64     `json:"version"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (s *srvDeviceAgentBindingStore) view(p agentservice.Principal, binding srvDeviceAgentBinding, fallbackVoice string) srvHardwareDeviceView {
	view := srvHardwareDeviceView{DeviceID: binding.DeviceID, ClientID: binding.ClientID, AssistantMode: binding.AssistantMode, ExpertID: binding.ExpertID, InitialPrompt: binding.InitialPrompt, TTSVoiceID: binding.TTSVoiceID, InstanceID: binding.InstanceID, Version: binding.Version, UpdatedAt: binding.UpdatedAt, Status: "ready"}
	view.EffectiveTTSVoiceID = binding.TTSVoiceID
	if view.EffectiveTTSVoiceID == "" {
		view.EffectiveTTSVoiceID = normalizeSrvTTSVoiceID(fallbackVoice)
	}
	if binding.AssistantMode == srvHardwareAssistantExpert {
		if expert, ok := s.resolveExpert(p, binding.ExpertID); ok && strings.TrimSpace(expert.SystemPrompt) != "" {
			view.ExpertName = expert.Name
		} else {
			view.Status = "degraded"
		}
	}
	return view
}

type srvDeviceAgentBindingStore struct {
	path        string
	expertsPath string
	mu          sync.Mutex
	bindings    map[string]srvDeviceAgentBinding
	experts     map[string]srvHardwareExpert
}

func newSrvDeviceAgentBindingStore(dataRoot string) *srvDeviceAgentBindingStore {
	s := &srvDeviceAgentBindingStore{
		path:        filepath.Join(dataRoot, "device_agent_bindings.json"),
		expertsPath: filepath.Join(dataRoot, "hardware_experts.json"),
		bindings:    map[string]srvDeviceAgentBinding{},
		experts:     map[string]srvHardwareExpert{},
	}
	s.load()
	return s
}

func (s *srvDeviceAgentBindingStore) load() {
	if s == nil {
		return
	}
	load := func(path string, out any) {
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) == 0 || len(raw) > 8*1024*1024 {
			return
		}
		_ = json.Unmarshal(raw, out)
	}
	load(s.path, &s.bindings)
	load(s.expertsPath, &s.experts)
	if s.bindings == nil {
		s.bindings = map[string]srvDeviceAgentBinding{}
	}
	if s.experts == nil {
		s.experts = map[string]srvHardwareExpert{}
	}
}

func srvHardwareBindingKey(p agentservice.Principal, clientID string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + strings.TrimSpace(clientID)
}

func srvHardwareExpertKey(p agentservice.Principal, id string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + strings.TrimSpace(id)
}

func normalizeSrvHardwareAssistantMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", srvHardwareAssistantGeneral:
		return srvHardwareAssistantGeneral, nil
	case srvHardwareAssistantExpert:
		return srvHardwareAssistantExpert, nil
	default:
		return "", fmt.Errorf("assistant_mode must be general or expert")
	}
}

func normalizeSrvHardwareInitialPrompt(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > srvHardwareInitialPromptMax || !utf8.ValidString(value) {
		return "", fmt.Errorf("initial_prompt must be valid UTF-8 and at most %d bytes", srvHardwareInitialPromptMax)
	}
	return value, nil
}

func normalizeSrvHardwareTTSVoice(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !tts.IsSupportedTTSVoiceID(value) {
		return "", fmt.Errorf("tts_voice_id is not supported")
	}
	return value, nil
}

func (s *srvDeviceAgentBindingStore) ensure(p agentservice.Principal, clientID string) (srvDeviceAgentBinding, error) {
	clientID = strings.TrimSpace(clientID)
	if s == nil || clientID == "" || p.TenantID == "" || p.UserID == "" {
		return srvDeviceAgentBinding{}, errors.New("hardware device binding is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	if binding, ok := s.bindings[key]; ok {
		if !binding.DeletedAt.IsZero() {
			return srvDeviceAgentBinding{}, errors.New("hardware device has been unpaired")
		}
		return binding, nil
	}
	now := time.Now().UTC()
	binding := srvDeviceAgentBinding{DeviceID: clientID, ClientID: clientID, TenantID: p.TenantID, UserID: p.UserID, AssistantMode: srvHardwareAssistantGeneral, Version: 1, CreatedAt: now, UpdatedAt: now}
	s.bindings[key] = binding
	if err := s.saveBindingsLocked(); err != nil {
		delete(s.bindings, key)
		return srvDeviceAgentBinding{}, err
	}
	return binding, nil
}

// activate is only called by the authenticated pairing flow.  It is the sole
// operation that can clear a deletion tombstone, ensuring that old device
// traffic cannot resurrect a removed assistant or its conversation space.
func (s *srvDeviceAgentBindingStore) activate(p agentservice.Principal, clientID string) (srvDeviceAgentBinding, error) {
	clientID = strings.TrimSpace(clientID)
	if s == nil || clientID == "" || p.TenantID == "" || p.UserID == "" {
		return srvDeviceAgentBinding{}, errors.New("hardware device binding is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	if binding, ok := s.bindings[key]; ok && binding.DeletedAt.IsZero() {
		return binding, nil
	}
	now := time.Now().UTC()
	binding := srvDeviceAgentBinding{DeviceID: clientID, ClientID: clientID, TenantID: p.TenantID, UserID: p.UserID, AssistantMode: srvHardwareAssistantGeneral, Version: 1, CreatedAt: now, UpdatedAt: now}
	previous, found := s.bindings[key]
	s.bindings[key] = binding
	if err := s.saveBindingsLocked(); err != nil {
		if found {
			s.bindings[key] = previous
		} else {
			delete(s.bindings, key)
		}
		return srvDeviceAgentBinding{}, err
	}
	return binding, nil
}

func (s *srvDeviceAgentBindingStore) get(p agentservice.Principal, clientID string) (srvDeviceAgentBinding, bool) {
	if s == nil {
		return srvDeviceAgentBinding{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[srvHardwareBindingKey(p, clientID)]
	return binding, ok && binding.DeletedAt.IsZero()
}

func (s *srvDeviceAgentBindingStore) list(p agentservice.Principal) []srvDeviceAgentBinding {
	if s == nil {
		return []srvDeviceAgentBinding{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]srvDeviceAgentBinding, 0)
	for _, binding := range s.bindings {
		if binding.TenantID == p.TenantID && binding.UserID == p.UserID && binding.DeletedAt.IsZero() {
			out = append(out, binding)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *srvDeviceAgentBindingStore) update(p agentservice.Principal, clientID string, in srvHardwareBindingUpdate) (srvDeviceAgentBinding, error) {
	if s == nil {
		return srvDeviceAgentBinding{}, errors.New("hardware device binding is unavailable")
	}
	mode, err := normalizeSrvHardwareAssistantMode(in.AssistantMode)
	if err != nil {
		return srvDeviceAgentBinding{}, err
	}
	prompt, err := normalizeSrvHardwareInitialPrompt(in.InitialPrompt)
	if err != nil {
		return srvDeviceAgentBinding{}, err
	}
	voiceID, err := normalizeSrvHardwareTTSVoice(in.TTSVoiceID)
	if err != nil {
		return srvDeviceAgentBinding{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	binding, ok := s.bindings[key]
	if ok && !binding.DeletedAt.IsZero() {
		ok = false
	}
	if !ok {
		return srvDeviceAgentBinding{}, errors.New("hardware device not found")
	}
	if in.Version > 0 && in.Version != binding.Version {
		return srvDeviceAgentBinding{}, errors.New("hardware device binding was updated by another client")
	}
	expertID := strings.TrimSpace(in.ExpertID)
	if mode == srvHardwareAssistantExpert {
		if expertID == "" {
			return srvDeviceAgentBinding{}, errors.New("expert_id is required for expert mode")
		}
		if _, ok := s.experts[srvHardwareExpertKey(p, expertID)]; !ok {
			return srvDeviceAgentBinding{}, errors.New("selected AI expert is not available")
		}
	} else {
		expertID = ""
	}
	previous := binding
	binding.AssistantMode, binding.ExpertID = mode, expertID
	binding.InitialPrompt, binding.TTSVoiceID = prompt, voiceID
	binding.Version++
	binding.UpdatedAt = time.Now().UTC()
	s.bindings[key] = binding
	if err := s.saveBindingsLocked(); err != nil {
		s.bindings[key] = previous
		return srvDeviceAgentBinding{}, err
	}
	return binding, nil
}

func (s *srvDeviceAgentBindingStore) setInstance(p agentservice.Principal, clientID, instanceID string) error {
	if s == nil {
		return errors.New("hardware device binding is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	binding, ok := s.bindings[key]
	if ok && !binding.DeletedAt.IsZero() {
		ok = false
	}
	if !ok {
		return errors.New("hardware device not found")
	}
	if binding.InstanceID == instanceID {
		return nil
	}
	previous := binding
	binding.InstanceID, binding.UpdatedAt = strings.TrimSpace(instanceID), time.Now().UTC()
	s.bindings[key] = binding
	if err := s.saveBindingsLocked(); err != nil {
		s.bindings[key] = previous
		return err
	}
	return nil
}

func (s *srvDeviceAgentBindingStore) delete(p agentservice.Principal, clientID string) (srvDeviceAgentBinding, bool, error) {
	if s == nil {
		return srvDeviceAgentBinding{}, false, errors.New("hardware device binding is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	binding, ok := s.bindings[key]
	if !ok {
		return srvDeviceAgentBinding{}, false, nil
	}
	// A tombstone intentionally contains no prompt, expert, voice, or instance
	// ID.  It preserves only the owner/device identity needed to reject stale
	// device traffic until a new explicit pairing occurs.
	deleted := srvDeviceAgentBinding{DeviceID: binding.DeviceID, ClientID: binding.ClientID, TenantID: binding.TenantID, UserID: binding.UserID, Version: binding.Version + 1, CreatedAt: binding.CreatedAt, UpdatedAt: time.Now().UTC(), DeletedAt: time.Now().UTC()}
	s.bindings[key] = deleted
	if err := s.saveBindingsLocked(); err != nil {
		s.bindings[key] = binding
		return srvDeviceAgentBinding{}, false, err
	}
	return binding, true, nil
}

func (s *srvDeviceAgentBindingStore) resolveExpert(p agentservice.Principal, id string) (srvHardwareExpert, bool) {
	if s == nil {
		return srvHardwareExpert{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expert, ok := s.experts[srvHardwareExpertKey(p, id)]
	return expert, ok
}

func (s *srvDeviceAgentBindingStore) listExperts(p agentservice.Principal) []srvHardwareExpert {
	if s == nil {
		return []srvHardwareExpert{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]srvHardwareExpert, 0)
	for _, expert := range s.experts {
		if expert.TenantID == p.TenantID && expert.UserID == p.UserID {
			out = append(out, expert)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *srvDeviceAgentBindingStore) upsertExpert(p agentservice.Principal, expert srvHardwareExpert) (srvHardwareExpert, error) {
	if s == nil {
		return srvHardwareExpert{}, errors.New("hardware expert store is unavailable")
	}
	expert.ID, expert.Name, expert.SystemPrompt = strings.TrimSpace(expert.ID), strings.TrimSpace(expert.Name), strings.TrimSpace(expert.SystemPrompt)
	if expert.ID == "" || expert.Name == "" || expert.SystemPrompt == "" {
		return srvHardwareExpert{}, errors.New("id, name, and system_prompt are required")
	}
	if len(expert.SystemPrompt) > 32*1024 {
		return srvHardwareExpert{}, errors.New("system_prompt is too long")
	}
	expert.TenantID, expert.UserID = p.TenantID, p.UserID
	expert.UpdatedAt = time.Now().UTC()
	expert.Revision = expert.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareExpertKey(p, expert.ID)
	previous, found := s.experts[key]
	s.experts[key] = expert
	if err := s.saveExpertsLocked(); err != nil {
		if found {
			s.experts[key] = previous
		} else {
			delete(s.experts, key)
		}
		return srvHardwareExpert{}, err
	}
	return expert, nil
}

func (s *srvDeviceAgentBindingStore) deleteExpert(p agentservice.Principal, id string) (bool, error) {
	if s == nil {
		return false, errors.New("hardware expert store is unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, errors.New("expert id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareExpertKey(p, id)
	expert, found := s.experts[key]
	if !found {
		return false, nil
	}
	delete(s.experts, key)
	if err := s.saveExpertsLocked(); err != nil {
		s.experts[key] = expert
		return false, err
	}
	return true, nil
}

func (s *srvDeviceAgentBindingStore) saveBindingsLocked() error {
	return srvWritePrivateJSON(s.path, s.bindings)
}
func (s *srvDeviceAgentBindingStore) saveExpertsLocked() error {
	return srvWritePrivateJSON(s.expertsPath, s.experts)
}

func srvWritePrivateJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *srvDeviceAgentBindingStore) ensureInstance(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, clientID string) (srvDeviceAgentBinding, *agentservice.Instance, error) {
	binding, err := s.ensure(p, clientID)
	if err != nil {
		return srvDeviceAgentBinding{}, nil, err
	}
	instances, err := svc.ListInstances(ctx, p)
	if err != nil {
		return srvDeviceAgentBinding{}, nil, err
	}
	for _, inst := range instances {
		if inst.Metadata != nil && inst.Metadata["im_runtime_key"] == srvHardwareRuntimeKey && inst.Metadata["device_binding_id"] == binding.DeviceID {
			inst, err = s.syncInstancePolicy(ctx, svc, p, binding, inst)
			if err != nil {
				return srvDeviceAgentBinding{}, nil, err
			}
			if inst.Status == agentservice.InstanceStatusStopped {
				resumed, err := svc.ResumeInstance(ctx, p, inst.ID)
				if err != nil {
					return srvDeviceAgentBinding{}, nil, err
				}
				inst = *resumed
			}
			if err := s.setInstance(p, clientID, inst.ID); err != nil {
				return srvDeviceAgentBinding{}, nil, err
			}
			return binding, &inst, nil
		}
	}
	metadata, err := s.instanceMetadata(p, binding)
	if err != nil {
		return srvDeviceAgentBinding{}, nil, err
	}
	inst, err := svc.CreateInstance(ctx, p, agentservice.CreateInstanceInput{Name: "Hardware Assistant · " + binding.ClientID, Description: "MaClawSrv hardware device assistant", Metadata: metadata})
	if err != nil {
		return srvDeviceAgentBinding{}, nil, err
	}
	if err := s.setInstance(p, clientID, inst.ID); err != nil {
		return srvDeviceAgentBinding{}, nil, err
	}
	binding.InstanceID = inst.ID
	return binding, inst, nil
}

func (s *srvDeviceAgentBindingStore) instanceMetadata(p agentservice.Principal, binding srvDeviceAgentBinding) (map[string]string, error) {
	metadata := map[string]string{
		"im_runtime_key":           srvHardwareRuntimeKey,
		"im_platform":              "thirdparty",
		"device_binding_id":        binding.DeviceID,
		"device_client_id":         binding.ClientID,
		"hardware_assistant_mode":  binding.AssistantMode,
		"hardware_initial_prompt":  binding.InitialPrompt,
		"hardware_binding_version": fmt.Sprint(binding.Version),
	}
	if binding.AssistantMode != srvHardwareAssistantExpert {
		return metadata, nil
	}
	expert, ok := s.resolveExpert(p, binding.ExpertID)
	if !ok || strings.TrimSpace(expert.SystemPrompt) == "" {
		return nil, errors.New("the AI expert bound to this hardware device is unavailable")
	}
	metadata["hardware_expert_id"] = expert.ID
	metadata["hardware_expert_name"] = expert.Name
	metadata["hardware_expert_revision"] = expert.Revision
	metadata["hardware_expert_system_prompt"] = expert.SystemPrompt
	if len(expert.Tools) > 0 {
		data, _ := json.Marshal(expert.Tools)
		metadata["hardware_expert_tools_json"] = string(data)
	}
	if len(expert.Skills) > 0 {
		data, _ := json.Marshal(expert.Skills)
		metadata["hardware_expert_skills_json"] = string(data)
	}
	return metadata, nil
}

func (s *srvDeviceAgentBindingStore) syncInstancePolicy(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, binding srvDeviceAgentBinding, inst agentservice.Instance) (agentservice.Instance, error) {
	metadata, err := s.instanceMetadata(p, binding)
	if err != nil {
		return inst, err
	}
	if srvStringMapEqual(inst.Metadata, metadata) {
		return inst, nil
	}
	updated, err := svc.UpdateInstance(ctx, p, inst.ID, agentservice.UpdateInstanceInput{Metadata: metadata})
	if err != nil {
		return inst, err
	}
	return *updated, nil
}

func (s *srvDeviceAgentBindingStore) syncBindingInstancePolicy(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, binding srvDeviceAgentBinding) error {
	if strings.TrimSpace(binding.InstanceID) == "" {
		return nil
	}
	inst, err := svc.GetInstance(ctx, p, binding.InstanceID)
	if err != nil {
		return err
	}
	_, err = s.syncInstancePolicy(ctx, svc, p, binding, *inst)
	return err
}

// clearInstanceIf matches clears a stale instance reference only when it still
// points at the supplied instance.  This lets the gateway recreate a deleted
// runtime without treating every missing instance as an internal failure.
func (s *srvDeviceAgentBindingStore) clearInstanceIf(p agentservice.Principal, clientID, instanceID string) error {
	if s == nil {
		return errors.New("hardware device binding is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := srvHardwareBindingKey(p, clientID)
	binding, ok := s.bindings[key]
	if !ok || !binding.DeletedAt.IsZero() || binding.InstanceID != strings.TrimSpace(instanceID) {
		return nil
	}
	previous := binding
	binding.InstanceID = ""
	binding.UpdatedAt = time.Now().UTC()
	s.bindings[key] = binding
	if err := s.saveBindingsLocked(); err != nil {
		s.bindings[key] = previous
		return err
	}
	return nil
}
