package ve

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// AccessPolicy defines how a virtual employee controls access.
type AccessPolicy string

const (
	PolicyPublic     AccessPolicy = "public"
	PolicyWhitelist  AccessPolicy = "whitelist"
	PolicyBlacklist  AccessPolicy = "blacklist"
	PolicyPerRequest AccessPolicy = "per_request"
)

// VEStatus represents the lifecycle state of a virtual employee.
type VEStatus string

const (
	VEStatusPending  VEStatus = "pending"
	VEStatusActive   VEStatus = "active"
	VEStatusDisabled VEStatus = "disabled"
	VEStatusRejected VEStatus = "rejected"
)

// VirtualEmployee is the core data model for a registered virtual employee.
type VirtualEmployee struct {
	ID             string       `json:"id"`
	OwnerMachineID string       `json:"owner_machine_id"`
	OwnerAgentID   string       `json:"owner_agent_id"`
	Name           string       `json:"name"`
	SkillDesc      string       `json:"skill_description"`
	AccessPolicy   AccessPolicy `json:"access_policy"`
	Status         VEStatus     `json:"status"`
	OnlineStatus   string       `json:"online_status"` // "online" or "offline"
	Whitelist      []string     `json:"whitelist,omitempty"`
	Blacklist      []string     `json:"blacklist,omitempty"`
	RegisteredAt   time.Time    `json:"registered_at"`
	ApprovedAt     time.Time    `json:"approved_at,omitempty"`
	DisabledAt     time.Time    `json:"disabled_at,omitempty"`
	RejectedAt     time.Time    `json:"rejected_at,omitempty"`
	RejectReason   string       `json:"reject_reason,omitempty"`
	LastHeartbeat  time.Time    `json:"last_heartbeat,omitempty"`
}

// VERegistrationRequest is the input for registering a new virtual employee.
type VERegistrationRequest struct {
	OwnerMachineID string       `json:"owner_machine_id"`
	OwnerAgentID   string       `json:"owner_agent_id"`
	Name           string       `json:"name"`
	SkillDesc      string       `json:"skill_description"`
	AccessPolicy   AccessPolicy `json:"access_policy"`
	Whitelist      []string     `json:"whitelist,omitempty"`
	Blacklist      []string     `json:"blacklist,omitempty"`
}

// Registry manages virtual employee registration, approval, and discovery.
type Registry struct {
	mu         sync.RWMutex
	quotaStore *QuotaStore
	employees  map[string]*VirtualEmployee // key: VE ID
	filePath   string                      // JSON persistence path
	onChange   func()                      // optional callback when list changes
}

// NewRegistry creates a new VE registry.
func NewRegistry(quotaStore *QuotaStore, filePath string) *Registry {
	r := &Registry{
		quotaStore: quotaStore,
		employees:  make(map[string]*VirtualEmployee),
		filePath:   filePath,
	}
	_ = r.loadFromDisk() // best-effort load
	return r
}

// SetOnChange sets a callback invoked when the VE list changes (for WebSocket push).
func (r *Registry) SetOnChange(fn func()) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

// Register creates a new pending virtual employee registration.
func (r *Registry) Register(req VERegistrationRequest) (*VirtualEmployee, error) {
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if len([]rune(req.Name)) > 50 {
		return nil, errors.New("name exceeds 50 characters")
	}
	if len([]rune(req.SkillDesc)) > 500 {
		return nil, errors.New("skill_description exceeds 500 characters")
	}
	if !isValidAccessPolicy(req.AccessPolicy) {
		return nil, fmt.Errorf("invalid access_policy: %q", req.AccessPolicy)
	}
	if req.OwnerMachineID == "" {
		return nil, errors.New("owner_machine_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check quota
	activeCount := r.countByStatusLocked(VEStatusActive)
	quota := r.quotaStore.GetEffectiveQuota()
	if activeCount >= quota {
		return nil, &QuotaExceededError{Active: activeCount, Quota: quota}
	}

	// Check duplicate: same owner_machine_id with pending/active status
	for _, ve := range r.employees {
		if ve.OwnerMachineID == req.OwnerMachineID && (ve.Status == VEStatusPending || ve.Status == VEStatusActive) {
			return nil, fmt.Errorf("machine %s already has a %s virtual employee registration", req.OwnerMachineID, ve.Status)
		}
	}

	ve := &VirtualEmployee{
		ID:             idgen.New("ve"),
		OwnerMachineID: req.OwnerMachineID,
		OwnerAgentID:   req.OwnerAgentID,
		Name:           req.Name,
		SkillDesc:      req.SkillDesc,
		AccessPolicy:   req.AccessPolicy,
		Status:         VEStatusPending,
		OnlineStatus:   "offline",
		Whitelist:      req.Whitelist,
		Blacklist:      req.Blacklist,
		RegisteredAt:   time.Now().UTC(),
	}

	r.employees[ve.ID] = ve
	r.persistLocked()
	r.notifyChange()
	return ve, nil
}

// Approve transitions a pending VE to active status.
func (r *Registry) Approve(veID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ve, ok := r.employees[veID]
	if !ok {
		return fmt.Errorf("virtual employee %q not found", veID)
	}
	if ve.Status != VEStatusPending {
		return fmt.Errorf("cannot approve: current status is %q (expected pending)", ve.Status)
	}

	// Re-check quota at approval time
	activeCount := r.countByStatusLocked(VEStatusActive)
	quota := r.quotaStore.GetEffectiveQuota()
	if activeCount >= quota {
		return &QuotaExceededError{Active: activeCount, Quota: quota}
	}

	ve.Status = VEStatusActive
	ve.ApprovedAt = time.Now().UTC()
	r.persistLocked()
	r.notifyChange()
	return nil
}

// Reject transitions a pending VE to rejected status.
func (r *Registry) Reject(veID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ve, ok := r.employees[veID]
	if !ok {
		return fmt.Errorf("virtual employee %q not found", veID)
	}
	if ve.Status != VEStatusPending {
		return fmt.Errorf("cannot reject: current status is %q (expected pending)", ve.Status)
	}

	ve.Status = VEStatusRejected
	ve.RejectReason = reason
	ve.RejectedAt = time.Now().UTC()
	r.persistLocked()
	r.notifyChange()
	return nil
}

// Disable transitions an active VE to disabled status.
func (r *Registry) Disable(veID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ve, ok := r.employees[veID]
	if !ok {
		return fmt.Errorf("virtual employee %q not found", veID)
	}
	if ve.Status != VEStatusActive {
		return fmt.Errorf("cannot disable: current status is %q (expected active)", ve.Status)
	}

	ve.Status = VEStatusDisabled
	ve.DisabledAt = time.Now().UTC()
	r.persistLocked()
	r.notifyChange()
	return nil
}

// ListAll returns all virtual employees (for admin panel).
func (r *Registry) ListAll() []*VirtualEmployee {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*VirtualEmployee, 0, len(r.employees))
	for _, ve := range r.employees {
		result = append(result, ve)
	}
	return result
}

// ListDiscoverable returns VEs visible to the given requester based on AccessPolicy.
func (r *Registry) ListDiscoverable(requesterMachineID string) []*VirtualEmployee {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*VirtualEmployee
	for _, ve := range r.employees {
		if ve.Status != VEStatusActive {
			continue
		}
		if r.isVisibleTo(ve, requesterMachineID) {
			result = append(result, ve)
		}
	}
	return result
}

// GetByID returns a single VE by ID.
func (r *Registry) GetByID(veID string) (*VirtualEmployee, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ve, ok := r.employees[veID]
	return ve, ok
}

// GetByOwner returns the VE owned by the given machine ID (active or pending).
func (r *Registry) GetByOwner(machineID string) (*VirtualEmployee, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ve := range r.employees {
		if ve.OwnerMachineID == machineID && (ve.Status == VEStatusActive || ve.Status == VEStatusPending) {
			return ve, true
		}
	}
	return nil, false
}

// UpdateSettings updates a VE's name, skill description, and access policy.
func (r *Registry) UpdateSettings(veID, name, skillDesc string, policy AccessPolicy, whitelist, blacklist []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ve, ok := r.employees[veID]
	if !ok {
		return fmt.Errorf("virtual employee %q not found", veID)
	}
	if name != "" {
		if len([]rune(name)) > 50 {
			return errors.New("name exceeds 50 characters")
		}
		ve.Name = name
	}
	if skillDesc != "" {
		if len([]rune(skillDesc)) > 500 {
			return errors.New("skill_description exceeds 500 characters")
		}
		ve.SkillDesc = skillDesc
	}
	if policy != "" {
		if !isValidAccessPolicy(policy) {
			return fmt.Errorf("invalid access_policy: %q", policy)
		}
		ve.AccessPolicy = policy
	}
	ve.Whitelist = whitelist
	ve.Blacklist = blacklist
	r.persistLocked()
	r.notifyChange()
	return nil
}

// SetOnlineStatus updates the online status of a VE.
func (r *Registry) SetOnlineStatus(veID, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ve, ok := r.employees[veID]; ok {
		if ve.OnlineStatus != status {
			ve.OnlineStatus = status
			r.notifyChange()
		}
	}
}

// ActiveCount returns the number of active virtual employees.
func (r *Registry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.countByStatusLocked(VEStatusActive)
}

// --- Access Policy filtering ---

func (r *Registry) isVisibleTo(ve *VirtualEmployee, requesterMachineID string) bool {
	switch ve.AccessPolicy {
	case PolicyPublic:
		return true
	case PolicyWhitelist:
		return containsStr(ve.Whitelist, requesterMachineID)
	case PolicyBlacklist:
		return !containsStr(ve.Blacklist, requesterMachineID)
	case PolicyPerRequest:
		return true // visible with "需授权" badge
	default:
		return false
	}
}

// CanAccess checks if a requester can establish a session with the VE.
// For per_request, this returns false — caller must go through auth flow.
func (r *Registry) CanAccess(veID, requesterMachineID string) (allowed bool, needsAuth bool, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ve, ok := r.employees[veID]
	if !ok {
		return false, false, fmt.Errorf("virtual employee %q not found", veID)
	}
	if ve.Status != VEStatusActive {
		return false, false, fmt.Errorf("virtual employee %q is not active (status: %s)", veID, ve.Status)
	}

	switch ve.AccessPolicy {
	case PolicyPublic:
		return true, false, nil
	case PolicyWhitelist:
		if containsStr(ve.Whitelist, requesterMachineID) {
			return true, false, nil
		}
		return false, false, nil
	case PolicyBlacklist:
		if containsStr(ve.Blacklist, requesterMachineID) {
			return false, false, nil
		}
		return true, false, nil
	case PolicyPerRequest:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unknown access policy: %q", ve.AccessPolicy)
	}
}

// --- Persistence ---

func (r *Registry) persistLocked() {
	if r.filePath == "" {
		return
	}
	data, err := json.MarshalIndent(r.employees, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(r.filePath, data, 0o600)
}

func (r *Registry) loadFromDisk() error {
	if r.filePath == "" {
		return nil
	}
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &r.employees)
}

func (r *Registry) notifyChange() {
	if r.onChange != nil {
		go r.onChange()
	}
}

func (r *Registry) countByStatusLocked(status VEStatus) int {
	count := 0
	for _, ve := range r.employees {
		if ve.Status == status {
			count++
		}
	}
	return count
}

// --- Helpers ---

func isValidAccessPolicy(p AccessPolicy) bool {
	switch p {
	case PolicyPublic, PolicyWhitelist, PolicyBlacklist, PolicyPerRequest:
		return true
	}
	return false
}

func containsStr(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// QuotaExceededError is returned when the VE quota is exhausted.
type QuotaExceededError struct {
	Active int
	Quota  int
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("virtual employee quota exceeded: %d active, quota=%d", e.Active, e.Quota)
}
