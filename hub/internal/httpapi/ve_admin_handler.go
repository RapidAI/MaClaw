package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreliba2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// veGroupConfig holds the hub-level digital employee group chat configuration.
type veGroupConfig struct {
	MaxGroupParticipants int  `json:"max_group_participants"`
	AutoApprove          bool `json:"auto_approve"`
}

type digitalEmployeeEntry struct {
	ID                 string   `json:"id"`
	MachineID          string   `json:"machine_id"`
	EmployeeType       string   `json:"employee_type,omitempty"`
	PlatformID         string   `json:"platform_id,omitempty"`
	PlatformEmployeeID string   `json:"platform_employee_id,omitempty"`
	RuntimeProviderID  string   `json:"runtime_provider_id,omitempty"`
	OwnerUserID        string   `json:"owner_user_id"`
	OwnerEmail         string   `json:"owner_email,omitempty"`
	Name               string   `json:"name"`
	SkillDescription   string   `json:"skill_description"`
	AvatarDataURL      string   `json:"avatar_data_url,omitempty"`
	AccessPolicy       string   `json:"access_policy"`
	Whitelist          []string `json:"whitelist,omitempty"`
	Blacklist          []string `json:"blacklist,omitempty"`
	VisibleGroupIDs    []string `json:"visible_group_ids,omitempty"`
	Resident           bool     `json:"resident,omitempty"`
	Status             string   `json:"status"`
	OnlineStatus       string   `json:"online_status"`
	RegisteredAt       string   `json:"registered_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	DisabledAt         string   `json:"disabled_at,omitempty"`
	RejectedAt         string   `json:"rejected_at,omitempty"`
	RejectReason       string   `json:"reject_reason,omitempty"`
	RuntimeMissing     bool     `json:"runtime_missing,omitempty"`
	HistoryRetained    bool     `json:"history_retained,omitempty"`
}

type digitalEmployeeRegistry struct {
	Employees []digitalEmployeeEntry `json:"employees"`
}

type digitalEmployeeAccessRequest struct {
	ID                 string `json:"id"`
	RequesterUserID    string `json:"requester_user_id"`
	RequesterMachineID string `json:"requester_machine_id"`
	RequesterName      string `json:"requester_name,omitempty"`
	TargetVEID         string `json:"target_ve_id"`
	TargetMachineID    string `json:"target_machine_id"`
	TargetVEName       string `json:"target_ve_name"`
	Status             string `json:"status"`
	Decision           string `json:"decision,omitempty"`
	CreatedAt          string `json:"created_at"`
	ExpiresAt          string `json:"expires_at"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type digitalEmployeeAccessRequestStore struct {
	Requests []digitalEmployeeAccessRequest `json:"requests"`
}

type veMachineAuthenticator interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
}

type veOwnerLookup interface {
	GetByID(ctx context.Context, id string) (*store.User, error)
}

type veMachineLookup interface {
	GetByID(ctx context.Context, id string) (*store.Machine, error)
}

type veMachineEventSender interface {
	SendToMachine(machineID string, msg any) error
}

type veMachinePresenceGetter interface {
	GetMachineInfo(ctx context.Context, machineID string) (*device.MachineRuntimeInfo, error)
}

type veVisibilityResolver interface {
	RequesterGroupPath(ctx context.Context, tenantID, userID string) ([]string, error)
}

type veHistorySearchMatch struct {
	Employee    digitalEmployeeEntry         `json:"employee"`
	Discussions []veHistoryDiscussionSummary `json:"discussions"`
}

type veHistoryDiscussionSummary struct {
	coreliba2a.HubDiscussionSummary
	CounterpartEmails []string `json:"counterpart_emails,omitempty"`
}

type veHistoryEmailResolver struct {
	ctx           context.Context
	tenantID      string
	ownerLookup   veOwnerLookup
	machineLookup veMachineLookup
	machineEmails map[string]string
	userEmails    map[string]string
}

func newVEHistoryEmailResolver(ctx context.Context, tenantID string, ownerLookup veOwnerLookup, machineLookup veMachineLookup) *veHistoryEmailResolver {
	return &veHistoryEmailResolver{
		ctx:           ctx,
		tenantID:      tenantID,
		ownerLookup:   ownerLookup,
		machineLookup: machineLookup,
		machineEmails: make(map[string]string),
		userEmails:    make(map[string]string),
	}
}

func decorateVEHistorySummaries(ctx context.Context, tenantID string, items []coreliba2a.HubDiscussionSummary, employee digitalEmployeeEntry, ownerLookup veOwnerLookup, machineLookup veMachineLookup) []veHistoryDiscussionSummary {
	resolver := newVEHistoryEmailResolver(ctx, tenantID, ownerLookup, machineLookup)
	out := make([]veHistoryDiscussionSummary, 0, len(items))
	for _, item := range items {
		out = append(out, veHistoryDiscussionSummary{
			HubDiscussionSummary: item,
			CounterpartEmails:    resolver.counterpartEmails(item.ParticipantIDs, employee),
		})
	}
	return out
}

func (r *veHistoryEmailResolver) counterpartEmails(participantIDs []string, employee digitalEmployeeEntry) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 1)
	for _, participantID := range participantIDs {
		id := strings.TrimSpace(participantID)
		if id == "" || veHistoryParticipantIsEmployee(id, employee) {
			continue
		}
		email := r.participantEmail(id)
		if email == "" {
			email = id
		}
		key := strings.ToLower(email)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, email)
	}
	return out
}

func (r *veHistoryEmailResolver) participantEmail(participantID string) string {
	if strings.Contains(participantID, "@") {
		return participantID
	}
	if r == nil || r.machineLookup == nil || r.ownerLookup == nil {
		return ""
	}
	machineKey := strings.ToLower(participantID)
	if email, ok := r.machineEmails[machineKey]; ok {
		return email
	}
	machine, err := r.machineLookup.GetByID(r.ctx, participantID)
	if err != nil || machine == nil {
		r.machineEmails[machineKey] = ""
		return ""
	}
	if r.tenantID != "" && strings.TrimSpace(machine.TenantID) != "" && !strings.EqualFold(strings.TrimSpace(machine.TenantID), r.tenantID) {
		r.machineEmails[machineKey] = ""
		return ""
	}
	userID := strings.TrimSpace(machine.UserID)
	if userID == "" {
		r.machineEmails[machineKey] = ""
		return ""
	}
	userKey := strings.ToLower(userID)
	email, ok := r.userEmails[userKey]
	if !ok {
		user, err := r.ownerLookup.GetByID(r.ctx, userID)
		if err != nil || user == nil || (r.tenantID != "" && strings.TrimSpace(user.TenantID) != "" && !strings.EqualFold(strings.TrimSpace(user.TenantID), r.tenantID)) {
			email = ""
		} else {
			email = strings.TrimSpace(user.Email)
		}
		r.userEmails[userKey] = email
	}
	r.machineEmails[machineKey] = email
	return email
}

func veHistoryParticipantIsEmployee(participantID string, employee digitalEmployeeEntry) bool {
	for _, id := range []string{employee.ID, employee.MachineID, employee.PlatformEmployeeID} {
		if groupDiscussionParticipantIdentityMatches(participantID, id) {
			return true
		}
	}
	return false
}

func veHistoryEmployeeForParticipants(registry digitalEmployeeRegistry, participantIDs []string) digitalEmployeeEntry {
	for _, participantID := range participantIDs {
		for _, employee := range registry.Employees {
			if veHistoryParticipantIsEmployee(participantID, employee) {
				return employee
			}
		}
	}
	return digitalEmployeeEntry{}
}

const (
	veGroupConfigKey    = "ve_group_config"
	veRegistryKey       = "ve_registry"
	veAccessRequestsKey = "ve_access_requests"

	veStatusPending  = "pending"
	veStatusActive   = "active"
	veStatusRejected = "rejected"
	veStatusDisabled = "disabled"

	veOnlineStatusOnline  = "online"
	veOnlineStatusOffline = "offline"

	veEmployeeTypeVirtual  = "virtual"
	veEmployeeTypePhysical = "physical"
)

// VEAdminListHandler handles GET /api/ve/list.
func VEAdminListHandler(system store.SystemSettingsRepository, presenceGetter veMachinePresenceGetter, ownerLookups ...veOwnerLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		tenantID := RequestTenantID(r)
		cfg := loadVEGroupConfig(r.Context(), system)
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, firstVEOwnerLookup(ownerLookups...))
		enrichVERegistryEmployeeTypes(&registry)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, false) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, tenantID)
		}
		employees := registry.Employees
		for i := range employees {
			employees[i] = applyVEDiscoverablePresence(r.Context(), employees[i], presenceGetter, runtimePresence)
		}
		sort.SliceStable(employees, func(i, j int) bool {
			return employees[i].RegisteredAt > employees[j].RegisteredAt
		})
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), system)
		writeJSON(w, http.StatusOK, map[string]any{
			"employees":     employees,
			"active_count":  countVEByStatus(employees, veStatusActive),
			"quota":         veAuthorizedQuota(authz),
			"authorization": authz,
			"group_config":  cfg,
		})
	}
}

// VEAdminConfigHandler handles GET/PUT /api/ve/config.
func VEAdminConfigHandler(system store.SystemSettingsRepository, senders ...veMachineEventSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		tenantID := RequestTenantID(r)
		switch r.Method {
		case http.MethodGet:
			cfg := loadVEGroupConfig(r.Context(), system)
			writeJSON(w, http.StatusOK, cfg)

		case http.MethodPut:
			var req struct {
				MaxGroupParticipants int   `json:"max_group_participants"`
				AutoApprove          *bool `json:"auto_approve"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
				return
			}
			if req.MaxGroupParticipants < 1 || req.MaxGroupParticipants > 10 {
				writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "max_group_participants must be 1-10")
				return
			}
			cfg := loadVEGroupConfig(r.Context(), system)
			cfg.MaxGroupParticipants = req.MaxGroupParticipants
			if req.AutoApprove != nil {
				cfg.AutoApprove = *req.AutoApprove
			}
			data, _ := json.Marshal(cfg)
			if err := system.Set(r.Context(), veGroupConfigKey, string(data)); err != nil {
				writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
				return
			}
			if cfg.AutoApprove {
				approved, err := autoApprovePendingVERegistrations(r.Context(), system, baseSystem, tenantID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
					return
				}
				for _, entry := range approved {
					emitVEAdminActionEvent(firstVEMachineEventSender(senders...), "approve", entry)
					postPlatformEmployeeActionCallback(r.Context(), baseSystem, tenantID, "approve", entry)
				}
			}
			writeJSON(w, http.StatusOK, cfg)

		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
		}
	}
}

func VERegisterHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator, ownerLookups ...veOwnerLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		system := veSystemSettingsForMachine(system, principal)
		if !requireVEDigitalEmployeeAuthorization(w, r, system) {
			return
		}
		var req veSettingsRequest
		if !decodeVEJSON(w, r, &req, veSettingsBodyMaxBytes) {
			return
		}
		entry, err := digitalEmployeeFromRequest(principal, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		entry.OwnerEmail = lookupVEOwnerEmail(r.Context(), firstVEOwnerLookup(ownerLookups...), entry.OwnerUserID)

		now := time.Now().UTC().Format(time.RFC3339)
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByMachineID(principal.MachineID)
		entry.RegisteredAt = now
		entry.UpdatedAt = now
		entry.OnlineStatus = veOnlineStatusOnline
		autoApprove := loadVEGroupConfig(r.Context(), system).AutoApprove
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), system)
		if idx >= 0 {
			previous := registry.Employees[idx]
			entry.ID = previous.ID
			entry.OwnerEmail = firstNonEmptyVE(entry.OwnerEmail, previous.OwnerEmail)
			entry.VisibleGroupIDs = normalizeVEStringList(previous.VisibleGroupIDs)
			entry.Resident = previous.Resident
			if req.AvatarDataURL == nil {
				entry.AvatarDataURL = previous.AvatarDataURL
			}
			entry.RegisteredAt = firstNonEmptyVE(previous.RegisteredAt, now)
			entry.Status = previous.Status
			if entry.Status == "" || entry.Status == veStatusRejected || entry.Status == veStatusDisabled {
				entry.Status = veRegistrationStatus(autoApprove, authz, registry.Employees, entry.Status)
			}
			registry.Employees[idx] = entry
		} else {
			entry.ID = veIDForMachine(principal.MachineID)
			entry.Status = veRegistrationStatus(autoApprove, authz, registry.Employees, "")
			registry.Employees = append(registry.Employees, entry)
			idx = len(registry.Employees) - 1
		}
		normalizeVERegistryResidentFlags(&registry)
		entry = registry.Employees[idx]
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"registered": true, "employee": entry})
	}
}

func VESettingsHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator, ownerLookups ...veOwnerLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		system := veSystemSettingsForMachine(system, principal)
		if !requireVEDigitalEmployeeAuthorization(w, r, system) {
			return
		}
		var req veSettingsRequest
		if !decodeVEJSON(w, r, &req, veSettingsBodyMaxBytes) {
			return
		}
		entry, err := digitalEmployeeFromRequest(principal, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		entry.OwnerEmail = lookupVEOwnerEmail(r.Context(), firstVEOwnerLookup(ownerLookups...), entry.OwnerUserID)
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByMachineID(principal.MachineID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_REGISTERED", "digital employee is not registered")
			return
		}
		previous := registry.Employees[idx]
		entry.ID = previous.ID
		entry.OwnerEmail = firstNonEmptyVE(entry.OwnerEmail, previous.OwnerEmail)
		if req.AvatarDataURL == nil {
			entry.AvatarDataURL = previous.AvatarDataURL
		}
		entry.Status = firstNonEmptyVE(previous.Status, veStatusPending)
		entry.RegisteredAt = previous.RegisteredAt
		entry.OnlineStatus = veOnlineStatusOnline
		entry.VisibleGroupIDs = normalizeVEStringList(previous.VisibleGroupIDs)
		entry.Resident = previous.Resident
		entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		registry.Employees[idx] = entry
		normalizeVERegistryResidentFlags(&registry)
		entry = registry.Employees[idx]
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"employee": entry})
	}
}

func VEStatusHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		system := veSystemSettingsForMachine(system, principal)
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), system)
		if !veAuthorizationActive(authz) {
			writeJSON(w, http.StatusOK, map[string]any{"registered": false, "authorization": authz})
			return
		}
		registry := loadVERegistry(r.Context(), system)
		if idx := registry.findByMachineID(principal.MachineID); idx >= 0 {
			entry := registry.Employees[idx]
			entry.OnlineStatus = veOnlineStatusOnline
			writeJSON(w, http.StatusOK, map[string]any{"registered": true, "employee": entry})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"registered": false})
	}
}

func VEDiscoverableHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator, options ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForMachine(system, principal)
		cfg := loadVEGroupConfig(r.Context(), system)
		if !veAuthorizationActive(loadVEDigitalEmployeeAuthorization(r.Context(), system)) {
			writeJSON(w, http.StatusOK, map[string]any{
				"employees":              []digitalEmployeeEntry{},
				"max_group_participants": cfg.MaxGroupParticipants,
			})
			return
		}
		registry := loadVERegistry(r.Context(), system)
		employees := make([]digitalEmployeeEntry, 0, len(registry.Employees))
		accessID := veRequesterAccessID(principal)
		presenceGetter, visibilityResolver := veDiscoverableOptions(options...)
		requesterGroupPath, requesterGroupPathResolved := []string(nil), false
		if veRegistryHasVisibleGroupRestrictions(registry) {
			requesterGroupPath, requesterGroupPathResolved = requesterVEGroupPath(r.Context(), visibilityResolver, principal)
		}
		runtimePresence := emptyMacLawSrvRuntimePresence()
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
		}
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive || groupDiscussionParticipantIdentityMatches(entry.MachineID, principal.MachineID) {
				continue
			}
			if !veVisibleToRequester(entry, requesterGroupPath, requesterGroupPathResolved) {
				continue
			}
			if !veAccessAllowed(entry, accessID) {
				continue
			}
			entry = applyVEDiscoverablePresence(r.Context(), entry, presenceGetter, runtimePresence)
			if !strings.EqualFold(strings.TrimSpace(entry.OnlineStatus), veOnlineStatusOnline) {
				continue
			}
			employees = append(employees, entry)
		}
		sort.SliceStable(employees, func(i, j int) bool { return employees[i].Name < employees[j].Name })
		writeJSON(w, http.StatusOK, map[string]any{
			"employees":              employees,
			"max_group_participants": cfg.MaxGroupParticipants,
		})
	}
}

func veRegistryHasVisibleGroupRestrictions(registry digitalEmployeeRegistry) bool {
	for _, entry := range registry.Employees {
		if entry.Status == veStatusActive && len(normalizeVEStringList(entry.VisibleGroupIDs)) > 0 {
			return true
		}
	}
	return false
}

func veDiscoverableOptions(options ...any) (veMachinePresenceGetter, veVisibilityResolver) {
	var presenceGetter veMachinePresenceGetter
	var visibilityResolver veVisibilityResolver
	for _, option := range options {
		if option == nil {
			continue
		}
		if getter, ok := option.(veMachinePresenceGetter); ok && presenceGetter == nil {
			presenceGetter = getter
		}
		if resolver, ok := option.(veVisibilityResolver); ok && visibilityResolver == nil {
			visibilityResolver = resolver
		}
	}
	return presenceGetter, visibilityResolver
}

func requesterVEGroupPath(ctx context.Context, resolver veVisibilityResolver, principal *auth.MachinePrincipal) ([]string, bool) {
	if resolver == nil || principal == nil {
		return nil, false
	}
	path, err := resolver.RequesterGroupPath(ctx, principal.TenantID, principal.UserID)
	if err != nil {
		return nil, false
	}
	return normalizeVEStringList(path), true
}

func veVisibleToRequester(entry digitalEmployeeEntry, requesterGroupPath []string, requesterGroupPathResolved bool) bool {
	visibleGroups := normalizeVEStringList(entry.VisibleGroupIDs)
	if len(visibleGroups) == 0 {
		return true
	}
	if !requesterGroupPathResolved {
		return false
	}
	for _, groupID := range visibleGroups {
		if containsVEValue(requesterGroupPath, groupID) {
			return true
		}
	}
	return false
}

type veSecurityVisibilityResolver struct {
	securitySvc *security.SecurityService
	users       veOwnerLookup
}

func (r veSecurityVisibilityResolver) RequesterGroupPath(ctx context.Context, tenantID, userID string) ([]string, error) {
	if r.securitySvc == nil {
		return nil, fmt.Errorf("security service is not configured")
	}
	email := strings.TrimSpace(userID)
	if r.users != nil && strings.TrimSpace(userID) != "" {
		user, err := r.users.GetByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("lookup requester user: %w", err)
		}
		if user == nil || strings.TrimSpace(user.Email) == "" {
			return nil, fmt.Errorf("requester user email not found")
		}
		if store.NormalizeTenantID(user.TenantID) != store.NormalizeTenantID(tenantID) {
			return nil, fmt.Errorf("requester user tenant mismatch")
		}
		email = strings.TrimSpace(user.Email)
	}
	if email == "" {
		return nil, fmt.Errorf("requester email is empty")
	}
	groupID, groupPath, _, _, err := r.securitySvc.GetUserPolicyView(security.WithTenant(ctx, tenantID), email)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(groupPath)+1)
	for _, item := range groupPath {
		ids = append(ids, item.ID)
	}
	if groupID != "" {
		ids = append(ids, groupID)
	}
	return ids, nil
}

type macLawSrvRuntimePresence struct {
	Loaded   bool
	Reported map[string]bool
	Ready    map[string]bool
}

type cachedMacLawSrvRuntimePresence struct {
	presence  macLawSrvRuntimePresence
	expiresAt time.Time
}

const macLawSrvRuntimePresenceCacheMaxItems = 128

var macLawSrvRuntimePresenceCache = struct {
	sync.Mutex
	items map[string]cachedMacLawSrvRuntimePresence
}{items: map[string]cachedMacLawSrvRuntimePresence{}}

var macLawSrvRuntimeReportHTTPClient = &http.Client{Timeout: time.Second}

func loadMacLawSrvRuntimePresence(ctx context.Context, system store.SystemSettingsRepository, tenantID string) macLawSrvRuntimePresence {
	runtime, ok := loadMacLawSrvRuntimeRegistry(ctx, system).findForTenant(tenantID)
	if !ok || strings.TrimSpace(runtime.BaseURL) == "" {
		return emptyMacLawSrvRuntimePresence()
	}
	cacheKey := macLawSrvRuntimePresenceCacheKey(runtime, tenantID)
	if cached, ok := getCachedMacLawSrvRuntimePresence(cacheKey, time.Now()); ok {
		return cached
	}
	report, err := fetchMacLawSrvRuntimeReport(ctx, runtime)
	if err != nil {
		presence := emptyMacLawSrvRuntimePresence()
		setCachedMacLawSrvRuntimePresence(cacheKey, presence, time.Now().Add(1*time.Second))
		return presence
	}
	presence := macLawSrvRuntimePresence{Loaded: true, Reported: map[string]bool{}, Ready: map[string]bool{}}
	for _, user := range report.Users {
		employeeID := strings.TrimSpace(user.EmployeeID)
		if employeeID == "" {
			continue
		}
		key := strings.ToLower(employeeID)
		presence.Reported[key] = true
		presence.Ready[key] = strings.EqualFold(strings.TrimSpace(user.RuntimeStatus), "ready")
	}
	setCachedMacLawSrvRuntimePresence(cacheKey, presence, time.Now().Add(2*time.Second))
	return presence
}

func macLawSrvRuntimePresenceCacheKey(runtime macLawSrvRuntimeEntry, tenantID string) string {
	secretHash := ""
	if secret := strings.TrimSpace(runtime.AdminSecret); secret != "" {
		sum := sha256.Sum256([]byte(secret))
		secretHash = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return strings.TrimRight(strings.TrimSpace(runtime.BaseURL), "/") + "\x00" + strings.TrimSpace(tenantID) + "\x00" + secretHash
}

func getCachedMacLawSrvRuntimePresence(key string, now time.Time) (macLawSrvRuntimePresence, bool) {
	macLawSrvRuntimePresenceCache.Lock()
	defer macLawSrvRuntimePresenceCache.Unlock()
	cached, ok := macLawSrvRuntimePresenceCache.items[key]
	if !ok || !cached.expiresAt.After(now) {
		if ok {
			delete(macLawSrvRuntimePresenceCache.items, key)
		}
		return emptyMacLawSrvRuntimePresence(), false
	}
	return copyMacLawSrvRuntimePresence(cached.presence), true
}

func setCachedMacLawSrvRuntimePresence(key string, presence macLawSrvRuntimePresence, expiresAt time.Time) {
	macLawSrvRuntimePresenceCache.Lock()
	defer macLawSrvRuntimePresenceCache.Unlock()
	now := time.Now()
	for cachedKey, cached := range macLawSrvRuntimePresenceCache.items {
		if !cached.expiresAt.After(now) {
			delete(macLawSrvRuntimePresenceCache.items, cachedKey)
		}
	}
	if len(macLawSrvRuntimePresenceCache.items) >= macLawSrvRuntimePresenceCacheMaxItems {
		var oldestKey string
		var oldestTime time.Time
		for cachedKey, cached := range macLawSrvRuntimePresenceCache.items {
			if oldestKey == "" || cached.expiresAt.Before(oldestTime) {
				oldestKey = cachedKey
				oldestTime = cached.expiresAt
			}
		}
		if oldestKey != "" {
			delete(macLawSrvRuntimePresenceCache.items, oldestKey)
		}
	}
	macLawSrvRuntimePresenceCache.items[key] = cachedMacLawSrvRuntimePresence{presence: copyMacLawSrvRuntimePresence(presence), expiresAt: expiresAt}
}

func copyMacLawSrvRuntimePresence(src macLawSrvRuntimePresence) macLawSrvRuntimePresence {
	dst := macLawSrvRuntimePresence{
		Loaded:   src.Loaded,
		Reported: map[string]bool{},
		Ready:    map[string]bool{},
	}
	for key, value := range src.Reported {
		dst.Reported[key] = value
	}
	for key, value := range src.Ready {
		dst.Ready[key] = value
	}
	return dst
}

func emptyMacLawSrvRuntimePresence() macLawSrvRuntimePresence {
	return macLawSrvRuntimePresence{Reported: map[string]bool{}, Ready: map[string]bool{}}
}

type macLawSrvRuntimeReport struct {
	Users []macLawSrvRuntimeReportUser `json:"users"`
}

type macLawSrvRuntimeReportUser struct {
	EmployeeID    string `json:"employee_id"`
	RuntimeStatus string `json:"runtime_status"`
	RuntimeUserID string `json:"runtime_user_id"`
	VirtualEmail  string `json:"virtual_email"`
}

type macLawSrvRuntimeReportWire struct {
	Users *[]macLawSrvRuntimeReportUser `json:"users"`
}

func fetchMacLawSrvRuntimeReport(ctx context.Context, runtime macLawSrvRuntimeEntry) (macLawSrvRuntimeReport, error) {
	var report macLawSrvRuntimeReport
	baseURL := strings.TrimRight(strings.TrimSpace(runtime.BaseURL), "/")
	if baseURL == "" {
		return report, fmt.Errorf("maclawsrv runtime base url is empty")
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+"/api/platform/runtime/report", nil)
	if err != nil {
		return report, err
	}
	if secret := strings.TrimSpace(runtime.AdminSecret); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := macLawSrvRuntimeReportHTTPClient.Do(req)
	if err != nil {
		return report, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return report, fmt.Errorf("maclawsrv runtime report status %d", resp.StatusCode)
	}
	var wire macLawSrvRuntimeReportWire
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&wire); err != nil {
		return report, err
	}
	if wire.Users == nil {
		return report, fmt.Errorf("maclawsrv runtime report missing users")
	}
	report.Users = *wire.Users
	return report, nil
}

func applyVEDiscoverablePresence(ctx context.Context, entry digitalEmployeeEntry, getter veMachinePresenceGetter, runtimePresence macLawSrvRuntimePresence) digitalEmployeeEntry {
	if isMacLawSrvRuntimeEmployee(entry) {
		entry.OnlineStatus = veOnlineStatusOffline
		reported, ready := macLawSrvRuntimePresenceState(entry, runtimePresence)
		if ready {
			entry.OnlineStatus = veOnlineStatusOnline
		}
		if runtimePresence.Loaded && !reported {
			entry.Status = veStatusDisabled
			entry.RuntimeMissing = true
			entry.HistoryRetained = true
		}
		return entry
	}
	if getter == nil || inferVEEmployeeType(entry) != veEmployeeTypePhysical {
		return entry
	}
	info, err := getter.GetMachineInfo(ctx, entry.MachineID)
	if err != nil {
		return entry
	}
	if info != nil && info.Online {
		entry.OnlineStatus = veOnlineStatusOnline
	} else {
		entry.OnlineStatus = veOnlineStatusOffline
	}
	return entry
}

func macLawSrvRuntimePresenceState(entry digitalEmployeeEntry, runtimePresence macLawSrvRuntimePresence) (bool, bool) {
	reported := false
	ready := false
	for _, id := range []string{entry.PlatformEmployeeID, entry.ID, entry.MachineID} {
		key := strings.ToLower(strings.TrimSpace(id))
		if key == "" {
			continue
		}
		reported = reported || runtimePresence.Reported[key]
		ready = ready || runtimePresence.Ready[key]
	}
	return reported, ready
}

func verifyMacLawSrvRuntimeReadyForActivation(ctx context.Context, system store.SystemSettingsRepository, tenantID string, entry digitalEmployeeEntry) (digitalEmployeeEntry, bool, string, string) {
	if !isMacLawSrvRuntimeEmployee(entry) {
		return entry, true, "", ""
	}
	presence := loadMacLawSrvRuntimePresence(ctx, system, tenantID)
	if !presence.Loaded {
		entry.OnlineStatus = veOnlineStatusOffline
		return entry, true, "", ""
	}
	reported, ready := macLawSrvRuntimePresenceState(entry, presence)
	entry = applyVEDiscoverablePresence(ctx, entry, nil, presence)
	if !reported {
		return entry, false, "VE_RUNTIME_MISSING", "digital employee runtime is not registered"
	}
	if !ready {
		return entry, false, "VE_NOT_ONLINE", "digital employee runtime is not online"
	}
	return entry, true, "", ""
}

func macLawSrvRuntimeMissingForPurge(ctx context.Context, system store.SystemSettingsRepository, tenantID string, entry digitalEmployeeEntry) bool {
	if !isMacLawSrvRuntimeEmployee(entry) {
		return false
	}
	presence := loadMacLawSrvRuntimePresence(ctx, system, tenantID)
	if !presence.Loaded {
		return false
	}
	reported, _ := macLawSrvRuntimePresenceState(entry, presence)
	return !reported
}

func isMacLawSrvRuntimeEmployee(entry digitalEmployeeEntry) bool {
	if normalizeVEEmployeeType(entry.EmployeeType) == veEmployeeTypePhysical {
		return false
	}
	if strings.TrimSpace(entry.PlatformEmployeeID) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.PlatformID), maclawSrvRuntimePlatformID) || strings.EqualFold(strings.TrimSpace(entry.RuntimeProviderID), maclawSrvRuntimePlatformID)
}

func veRegistryHasMacLawSrvRuntimeEmployees(registry digitalEmployeeRegistry, activeOnly bool) bool {
	for _, entry := range registry.Employees {
		if activeOnly && entry.Status != veStatusActive {
			continue
		}
		if isMacLawSrvRuntimeEmployee(entry) {
			return true
		}
	}
	return false
}

// VEInitiateHandler handles POST /api/ve/{id}/initiate for a machine-owned direct discussion.
func VEInitiateHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, authenticator veMachineAuthenticator, senders ...veMachineEventSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		if groupSvc == nil {
			writeError(w, http.StatusInternalServerError, "GROUP_DISCUSSION_UNAVAILABLE", "group discussion service is unavailable")
			return
		}
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForMachine(system, principal)
		if !requireVEDigitalEmployeeAuthorization(w, r, system) {
			return
		}
		veID := strings.TrimSpace(r.PathValue("id"))
		if veID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		target := registry.Employees[idx]
		runtimePresence := emptyMacLawSrvRuntimePresence()
		if isMacLawSrvRuntimeEmployee(target) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
			target = applyVEDiscoverablePresence(r.Context(), target, nil, runtimePresence)
		}
		if target.Status != veStatusActive {
			writeError(w, http.StatusConflict, "VE_NOT_ACTIVE", "digital employee is not active")
			return
		}
		if runtimePresence.Loaded && isMacLawSrvRuntimeEmployee(target) && target.OnlineStatus != veOnlineStatusOnline {
			writeError(w, http.StatusConflict, "VE_NOT_ONLINE", "digital employee runtime is not online")
			return
		}
		if groupDiscussionParticipantIdentityMatches(target.MachineID, principal.MachineID) {
			writeError(w, http.StatusBadRequest, "VE_SELF_CHAT_REJECTED", "cannot start a digital employee conversation with the same machine")
			return
		}
		accessID := veRequesterAccessID(principal)
		if !veAccessAllowed(target, accessID) {
			writeError(w, http.StatusForbidden, "VE_ACCESS_DENIED", "digital employee access is denied")
			return
		}
		var accessRequests *digitalEmployeeAccessRequestStore
		allowOnceRequestIndex := -1
		if target.AccessPolicy == "per_request" && !containsVEValue(target.Whitelist, accessID) {
			requests := loadVEAccessRequests(r.Context(), system)
			now := time.Now().UTC()
			expiredRequests := expirePendingVEAccessRequests(&requests, now)
			if idx := findConsumableVEAllowOnceRequestIndex(&requests, target, accessID, principal.MachineID, now); idx >= 0 {
				accessRequests = &requests
				allowOnceRequestIndex = idx
				if expiredRequests {
					if err := saveVEAccessRequests(r.Context(), system, requests); err != nil {
						writeError(w, http.StatusInternalServerError, "VE_AUTH_REQUEST_SAVE_FAILED", err.Error())
						return
					}
				}
			} else {
				requestID := newVEAccessRequestID(requests, principal.MachineID, now)
				req := digitalEmployeeAccessRequest{
					ID:                 requestID,
					RequesterUserID:    accessID,
					RequesterMachineID: principal.MachineID,
					RequesterName:      firstNonEmptyVE(principal.UserID, principal.MachineID),
					TargetVEID:         target.ID,
					TargetMachineID:    target.MachineID,
					TargetVEName:       target.Name,
					Status:             "pending",
					CreatedAt:          now.Format(time.RFC3339),
					ExpiresAt:          now.Add(5 * time.Minute).Format(time.RFC3339),
				}
				req = upsertVEAccessRequest(&requests, req)
				if err := saveVEAccessRequests(r.Context(), system, requests); err != nil {
					writeError(w, http.StatusInternalServerError, "VE_AUTH_REQUEST_SAVE_FAILED", err.Error())
					return
				}
				if sender := firstVEMachineEventSender(senders...); sender != nil {
					_ = sender.SendToMachine(target.MachineID, map[string]any{"type": "ve:auth_request", "ts": time.Now().Unix(), "payload": map[string]any{
						"request_id":           req.ID,
						"requester_name":       req.RequesterName,
						"requester_machine_id": req.RequesterMachineID,
						"target_ve_id":         req.TargetVEID,
						"target_ve_name":       req.TargetVEName,
						"source":               "digital_employee_chat",
						"created_at":           req.CreatedAt,
						"expires_at":           req.ExpiresAt,
					}})
				}
				writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending_confirmation", "request_id": req.ID, "expires_at": req.ExpiresAt, "message": "waiting for digital employee owner confirmation"})
				return
			}
		}
		topic := strings.TrimSpace(target.Name)
		if topic == "" {
			topic = "Digital employee conversation"
		}
		if session := findReusableVEDirectSession(groupSvc, store.NormalizeTenantID(principal.TenantID), principal.MachineID, target.MachineID); session != nil {
			if allowOnceRequestIndex >= 0 {
				markVEAccessRequestUsed(accessRequests, allowOnceRequestIndex, time.Now().UTC())
				if err := saveVEAccessRequests(r.Context(), system, *accessRequests); err != nil {
					writeError(w, http.StatusInternalServerError, "VE_AUTH_REQUEST_SAVE_FAILED", err.Error())
					return
				}
			}
			summary := discussionSummaryFromSession(session)
			decorateSummaryForParticipant(&summary, session, principal.MachineID)
			writeJSON(w, http.StatusOK, map[string]any{
				"session_id": session.ID,
				"ve_id":      target.ID,
				"ve_name":    target.Name,
				"discussion": summary,
			})
			return
		}
		session, err := groupSvc.CreateSession(store.NormalizeTenantID(principal.TenantID), CreateSessionRequest{
			Topic: "数字员工会话：" + topic,
			Goal:  "Direct discussion with " + topic,
			Participants: []coreliba2a.Participant{
				{ID: strings.TrimSpace(principal.MachineID), RoleCode: "initiator"},
				{ID: strings.TrimSpace(target.MachineID), RoleCode: "speak", Name: target.Name},
			},
			DecisionPolicy: coreliba2a.PolicyMajority,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "SESSION_CREATE_FAILED", err.Error())
			return
		}
		if allowOnceRequestIndex >= 0 {
			markVEAccessRequestUsed(accessRequests, allowOnceRequestIndex, time.Now().UTC())
			if err := saveVEAccessRequests(r.Context(), system, *accessRequests); err != nil {
				writeError(w, http.StatusInternalServerError, "VE_AUTH_REQUEST_SAVE_FAILED", err.Error())
				return
			}
		}
		summary := discussionSummaryFromSession(session)
		decorateSummaryForParticipant(&summary, session, principal.MachineID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"session_id": session.ID,
			"ve_id":      target.ID,
			"ve_name":    target.Name,
			"discussion": summary,
		})
	}
}

func findReusableVEDirectSession(groupSvc *GroupDiscussionService, tenantID, initiatorID, targetID string) *coreliba2a.Session {
	if groupSvc == nil {
		return nil
	}
	initiatorID = strings.TrimSpace(initiatorID)
	targetID = strings.TrimSpace(targetID)
	if initiatorID == "" || targetID == "" {
		return nil
	}
	sessions, err := groupSvc.ListSessions(tenantID, ListSessionsFilter{ParticipantID: initiatorID, Status: coreliba2a.SessionOpen})
	if err != nil {
		return nil
	}
	for _, session := range sessions {
		if isReusableVEDirectSession(session, initiatorID, targetID) {
			return session
		}
	}
	return nil
}

func VEAuthRespondHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator, senders ...veMachineEventSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
		system := veSystemSettingsForMachine(system, principal)
		var body struct {
			RequestID string `json:"request_id"`
			Decision  string `json:"decision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		body.RequestID = strings.TrimSpace(body.RequestID)
		body.Decision = strings.TrimSpace(body.Decision)
		if body.RequestID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "request_id is required")
			return
		}
		switch body.Decision {
		case "allow", "allow_once", "allow_long", "deny", "block":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_DECISION", "decision must be allow_once, allow_long, deny, or block")
			return
		}

		requests := loadVEAccessRequests(r.Context(), system)
		expiredRequests := expirePendingVEAccessRequests(&requests, time.Now().UTC())
		idx := -1
		for i, req := range requests.Requests {
			if req.ID == body.RequestID {
				idx = i
				break
			}
		}
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_AUTH_REQUEST_NOT_FOUND", "authorization request not found")
			return
		}
		req := requests.Requests[idx]
		if !groupDiscussionParticipantIdentityMatches(req.TargetMachineID, principal.MachineID) {
			writeError(w, http.StatusForbidden, "VE_AUTH_REQUEST_FORBIDDEN", "only the target digital employee owner can respond")
			return
		}
		if req.Status != "pending" {
			if expiredRequests {
				_ = saveVEAccessRequests(r.Context(), system, requests)
			}
			if req.Status == "expired" {
				emitVEAuthResult(firstVEMachineEventSender(senders...), req, "timeout", "expired")
			}
			writeError(w, http.StatusConflict, "VE_AUTH_REQUEST_ALREADY_HANDLED", "authorization request was already handled")
			return
		}
		decision := body.Decision
		if decision == "allow" {
			decision = "allow_once"
		}
		req.Decision = decision
		req.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		switch decision {
		case "allow_once", "allow_long":
			req.Status = "allowed"
		case "block":
			req.Status = "blocked"
		default:
			req.Status = "denied"
		}
		requests.Requests[idx] = req

		registry := loadVERegistry(r.Context(), system)
		if targetIdx := registry.findByIDOrMachineIDOrPlatformEmployeeID(req.TargetVEID); targetIdx >= 0 {
			target := &registry.Employees[targetIdx]
			requesterAccessID := firstNonEmptyVE(req.RequesterUserID, req.RequesterMachineID)
			switch decision {
			case "allow_long":
				if requesterAccessID != "" && !containsVEValue(target.Whitelist, requesterAccessID) {
					target.Whitelist = append(target.Whitelist, requesterAccessID)
				}
				target.Blacklist = removeVEValue(target.Blacklist, requesterAccessID)
			case "block":
				target.Whitelist = removeVEValue(target.Whitelist, requesterAccessID)
				if requesterAccessID != "" && !containsVEValue(target.Blacklist, requesterAccessID) {
					target.Blacklist = append(target.Blacklist, requesterAccessID)
				}
			}
			if err := saveVERegistry(r.Context(), system, registry); err != nil {
				writeError(w, http.StatusInternalServerError, "VE_REGISTRY_SAVE_FAILED", err.Error())
				return
			}
		}
		if err := saveVEAccessRequests(r.Context(), system, requests); err != nil {
			writeError(w, http.StatusInternalServerError, "VE_AUTH_REQUEST_SAVE_FAILED", err.Error())
			return
		}

		emitVEAuthResult(firstVEMachineEventSender(senders...), req, decision, req.Status)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": req.Status, "decision": decision})
	}
}

func emitVEAuthResult(sender veMachineEventSender, req digitalEmployeeAccessRequest, decision, status string) {
	if sender == nil {
		return
	}
	payload := map[string]any{"request_id": req.ID, "decision": decision, "status": status, "target_ve_id": req.TargetVEID, "target_machine_id": req.TargetMachineID, "target_ve_name": req.TargetVEName}
	_ = sender.SendToMachine(req.RequesterMachineID, map[string]any{"type": "ve:auth_result", "ts": time.Now().Unix(), "payload": payload})
	_ = sender.SendToMachine(req.TargetMachineID, map[string]any{"type": "ve:list_update", "ts": time.Now().Unix(), "payload": payload})
}

func isReusableVEDirectSession(session *coreliba2a.Session, initiatorID, targetID string) bool {
	if session == nil || session.Status != coreliba2a.SessionOpen {
		return false
	}
	initiatorID = strings.TrimSpace(initiatorID)
	targetID = strings.TrimSpace(targetID)
	if initiatorID == "" || targetID == "" || reusableVEDirectSessionParticipantCount(session) != 2 {
		return false
	}
	initiatorRole := participantRole(session, initiatorID)
	targetRole := participantRole(session, targetID)
	return strings.EqualFold(strings.TrimSpace(initiatorRole), "initiator") && targetRole != "" && groupDiscussionRoleContributesAnswer(targetRole)
}

func reusableVEDirectSessionParticipantCount(session *coreliba2a.Session) int {
	if session == nil {
		return 0
	}
	seen := map[string]struct{}{}
	for _, participant := range session.Participants {
		key := groupDiscussionCanonicalParticipantIdentityKey(participant.ID)
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

func VEAdminActionHandler(system store.SystemSettingsRepository, action string, senders ...veMachineEventSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		tenantID := RequestTenantID(r)
		veID := strings.TrimSpace(r.PathValue("id"))
		if veID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		entry := registry.Employees[idx]
		now := time.Now().UTC().Format(time.RFC3339)
		switch action {
		case "approve":
			authz := loadVEDigitalEmployeeAuthorization(r.Context(), system)
			if !veAuthorizationActive(authz) {
				writeError(w, http.StatusForbidden, "VE_AUTHORIZATION_INACTIVE", "digital employee authorization is inactive")
				return
			}
			if checked, ok, code, message := verifyMacLawSrvRuntimeReadyForActivation(r.Context(), baseSystem, tenantID, entry); !ok {
				registry.Employees[idx] = checked
				_ = saveVERegistry(r.Context(), system, registry)
				writeError(w, http.StatusConflict, code, message)
				return
			} else {
				entry = checked
			}
			quota := veAuthorizedQuota(authz)
			if countVEByStatus(registry.Employees, veStatusActive) >= quota && entry.Status != veStatusActive {
				writeError(w, http.StatusConflict, "VE_QUOTA_EXCEEDED", "digital employee quota exceeded")
				return
			}
			entry.Status = veStatusActive
			entry.RejectReason = ""
			entry.RejectedAt = ""
		case "reject":
			var req struct {
				Reason string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			entry.Status = veStatusRejected
			entry.Resident = false
			entry.RejectReason = strings.TrimSpace(req.Reason)
			entry.RejectedAt = now
		case "disable":
			entry.Status = veStatusDisabled
			entry.Resident = false
			entry.DisabledAt = now
		case "delete":
			if entry.Status == veStatusActive && !macLawSrvRuntimeMissingForPurge(r.Context(), baseSystem, tenantID, entry) {
				writeError(w, http.StatusConflict, "VE_DELETE_ACTIVE_FORBIDDEN", "active digital employee must be disabled before clearing")
				return
			}
			removed := entry
			registry.Employees = append(registry.Employees[:idx], registry.Employees[idx+1:]...)
			normalizeVERegistryResidentFlags(&registry)
			if err := saveVERegistry(r.Context(), system, registry); err != nil {
				writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
				return
			}
			emitVEAdminActionEvent(firstVEMachineEventSender(senders...), action, removed)
			postPlatformEmployeeActionCallback(r.Context(), baseSystem, tenantID, action, removed)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee": removed})
			return
		default:
			writeError(w, http.StatusBadRequest, "INVALID_ACTION", "unknown digital employee action")
			return
		}
		entry.UpdatedAt = now
		registry.Employees[idx] = entry
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		emitVEAdminActionEvent(firstVEMachineEventSender(senders...), action, entry)
		postPlatformEmployeeActionCallback(r.Context(), baseSystem, tenantID, action, entry)
		writeJSON(w, http.StatusOK, map[string]any{"employee": entry})
	}
}

func VEAdminForceDeleteHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, admins *auth.AdminService, senders ...veMachineEventSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		if admins == nil {
			writeError(w, http.StatusServiceUnavailable, "ADMIN_AUTH_UNAVAILABLE", "admin authentication is unavailable")
			return
		}
		admin := AdminFromContext(r.Context())
		if admin == nil || strings.TrimSpace(admin.Username) == "" {
			writeError(w, http.StatusForbidden, "TENANT_ADMIN_REQUIRED", "tenant admin authorization required")
			return
		}
		var req struct {
			AdminPassword string `json:"admin_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		if strings.TrimSpace(req.AdminPassword) == "" {
			writeError(w, http.StatusBadRequest, "ADMIN_PASSWORD_REQUIRED", "admin_password is required")
			return
		}
		tenantID := RequestTenantID(r)
		if _, err := admins.VerifyScopedCredentials(r.Context(), admin.Username, req.AdminPassword, tenantID); err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_ADMIN_PASSWORD", "admin password is incorrect")
			return
		}
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		veID := strings.TrimSpace(r.PathValue("id"))
		if veID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		removed := registry.Employees[idx]
		participantIDs := []string{removed.MachineID, removed.ID, removed.PlatformEmployeeID}
		deletedHistory := 0
		if groupSvc != nil {
			var err error
			deletedHistory, err = groupSvc.DeleteSessionsByParticipants(tenantID, participantIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "VE_HISTORY_DELETE_FAILED", err.Error())
				return
			}
		}
		registry.Employees = append(registry.Employees[:idx], registry.Employees[idx+1:]...)
		normalizeVERegistryResidentFlags(&registry)
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		emitVEAdminActionEvent(firstVEMachineEventSender(senders...), "force_delete", removed)
		postPlatformEmployeeActionCallback(r.Context(), baseSystem, tenantID, "delete", removed)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "employee": removed, "deleted_history_sessions": deletedHistory})
	}
}

func VEAdminResidentHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
			return
		}
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		tenantID := RequestTenantID(r)
		veID := strings.TrimSpace(r.PathValue("id"))
		if veID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		var req struct {
			Resident *bool `json:"resident"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		if req.Resident == nil {
			writeError(w, http.StatusBadRequest, "INVALID_RESIDENT", "resident is required")
			return
		}
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		entry := registry.Employees[idx]
		if *req.Resident && entry.Status != veStatusActive {
			writeError(w, http.StatusConflict, "VE_NOT_ACTIVE", "only active digital employee can be resident")
			return
		}
		if *req.Resident {
			if checked, ok, code, message := verifyMacLawSrvRuntimeReadyForActivation(r.Context(), baseSystem, tenantID, entry); !ok {
				registry.Employees[idx] = checked
				_ = saveVERegistry(r.Context(), system, registry)
				writeError(w, http.StatusConflict, code, message)
				return
			} else {
				registry.Employees[idx] = checked
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if *req.Resident {
			for i := range registry.Employees {
				nextResident := i == idx
				if registry.Employees[i].Resident != nextResident {
					registry.Employees[i].Resident = nextResident
					registry.Employees[i].UpdatedAt = now
				}
			}
		} else if registry.Employees[idx].Resident {
			registry.Employees[idx].Resident = false
			registry.Employees[idx].UpdatedAt = now
		}
		normalizeVERegistryResidentFlags(&registry)
		entry = registry.Employees[idx]
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"employee": entry})
	}
}

func VEAdminVisibilityHandler(system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
			return
		}
		system := veSystemSettingsForRequest(r, system)
		veID := strings.TrimSpace(r.PathValue("id"))
		if veID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		var req struct {
			VisibleGroupIDs      *[]string `json:"visible_group_ids"`
			VisibleDepartmentIDs *[]string `json:"visible_department_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		visibleGroupIDs := []string(nil)
		if req.VisibleGroupIDs != nil {
			visibleGroupIDs = normalizeVEStringList(*req.VisibleGroupIDs)
		} else if req.VisibleDepartmentIDs != nil {
			visibleGroupIDs = normalizeVEStringList(*req.VisibleDepartmentIDs)
		} else {
			writeError(w, http.StatusBadRequest, "INVALID_VISIBLE_GROUPS", "visible_group_ids is required")
			return
		}
		registry := loadVERegistry(r.Context(), system)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		if err := validateVEVisibleGroupIDs(securityRequestContext(r), securitySvc, visibleGroupIDs); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_VISIBLE_GROUPS", err.Error())
			return
		}
		entry := registry.Employees[idx]
		entry.VisibleGroupIDs = visibleGroupIDs
		entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		registry.Employees[idx] = entry
		if err := saveVERegistry(r.Context(), system, registry); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"employee": entry})
	}
}

func validateVEVisibleGroupIDs(ctx context.Context, securitySvc *security.SecurityService, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if securitySvc == nil {
		return fmt.Errorf("security service is not configured")
	}
	tree, err := securitySvc.GetGroupTree(ctx)
	if err != nil {
		return fmt.Errorf("load security groups: %w", err)
	}
	valid := map[string]struct{}{}
	collectVEVisibleDepartmentIDs(tree, valid)
	for _, id := range ids {
		if _, ok := valid[id]; !ok {
			return fmt.Errorf("unknown group id %q", id)
		}
	}
	return nil
}

func collectVEVisibleDepartmentIDs(root *security.GroupTreeNode, out map[string]struct{}) {
	if root == nil {
		return
	}
	for _, child := range root.Children {
		collectVEGroupTreeIDs(child, out)
	}
}

func collectVEGroupTreeIDs(node *security.GroupTreeNode, out map[string]struct{}) {
	if node == nil {
		return
	}
	if strings.TrimSpace(node.ID) != "" {
		out[strings.TrimSpace(node.ID)] = struct{}{}
	}
	for _, child := range node.Children {
		collectVEGroupTreeIDs(child, out)
	}
}

func postPlatformEmployeeActionCallback(ctx context.Context, system store.SystemSettingsRepository, tenantID, action string, entry digitalEmployeeEntry) {
	if system == nil || strings.TrimSpace(entry.PlatformID) == "" || strings.TrimSpace(entry.PlatformEmployeeID) == "" {
		return
	}
	reg := loadPlatformProviderRegistry(ctx, system)
	idx := reg.find(entry.PlatformID)
	if idx < 0 {
		return
	}
	provider := reg.Providers[idx]
	if strings.TrimSpace(provider.CallbackBaseURL) == "" || strings.TrimSpace(provider.CallbackSecret) == "" || strings.TrimSpace(provider.RegistrationStatus) != "active" {
		return
	}
	payload := map[string]any{
		"employee_id":     strings.TrimSpace(entry.PlatformEmployeeID),
		"hub_tenant_id":   strings.TrimSpace(tenantID),
		"hub_employee_id": firstNonEmpty(entry.ID, entry.MachineID),
		"hub_account_id":  strings.TrimSpace(entry.OwnerUserID),
		"hub_status":      platformEmployeeCallbackHubStatus(action, entry.Status),
		"message":         "digital employee " + strings.TrimSpace(action) + " in Hub",
	}
	go postPlatformCallback(provider, "/api/hub/callback/employee", payload)
}

func platformEmployeeCallbackHubStatus(action, status string) string {
	switch strings.TrimSpace(action) {
	case "approve":
		return "published"
	case "reject":
		return "failed"
	case "disable":
		return "disabled"
	case "delete":
		return "deleted"
	}
	switch strings.TrimSpace(status) {
	case veStatusActive:
		return "published"
	case veStatusDisabled:
		return "disabled"
	case veStatusRejected:
		return "failed"
	default:
		return ""
	}
}

// VEHistorySearchHandler handles GET /api/ve/history/search for admin review.
func VEHistorySearchHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, ownerLookup veOwnerLookup, machineLookups ...veMachineLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		requestTenantID := RequestTenantID(r)
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		if groupSvc == nil {
			writeError(w, http.StatusInternalServerError, "GROUP_DISCUSSION_UNAVAILABLE", "group discussion service is unavailable")
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeJSON(w, http.StatusOK, map[string]any{"query": query, "matches": []veHistorySearchMatch{}, "discussions": []veHistoryDiscussionSummary{}})
			return
		}
		limit := intQuery(r.URL.Query(), "limit")
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, ownerLookup)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, false) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, requestTenantID)
		}
		matches := make([]veHistorySearchMatch, 0)
		flattened := make([]veHistoryDiscussionSummary, 0)
		seenDiscussions := make(map[string]bool)
		machineLookup := firstVEMachineLookup(machineLookups...)
		tenantID := requestGroupDiscussionTenantID(r)
		for _, employee := range registry.Employees {
			if !veEmployeeMatchesQuery(employee, query) {
				continue
			}
			employee = applyVEDiscoverablePresence(r.Context(), employee, nil, runtimePresence)
			items, err := groupSvc.ListDiscussionSummaries(tenantID, ListSessionsFilter{ParticipantID: employee.MachineID, Limit: limit})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			decorated := decorateVEHistorySummaries(r.Context(), tenantID, items, employee, ownerLookup, machineLookup)
			matches = append(matches, veHistorySearchMatch{Employee: employee, Discussions: decorated})
			for _, item := range decorated {
				if item.ID != "" {
					if seenDiscussions[item.ID] {
						continue
					}
					seenDiscussions[item.ID] = true
				}
				flattened = append(flattened, item)
			}
		}
		sort.SliceStable(flattened, func(i, j int) bool { return flattened[i].UpdatedAt.After(flattened[j].UpdatedAt) })
		if len(flattened) > limit {
			flattened = flattened[:limit]
		}
		writeJSON(w, http.StatusOK, map[string]any{"query": query, "matches": matches, "discussions": flattened})
	}
}

// VEHistoryHandler handles GET /api/ve/{id}/history for admin review.
func VEHistoryHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, ownerLookup veOwnerLookup, machineLookups ...veMachineLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseSystem := globalSystemSettings(system)
		system := veSystemSettingsForRequest(r, system)
		requestTenantID := RequestTenantID(r)
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		if groupSvc == nil {
			writeError(w, http.StatusInternalServerError, "GROUP_DISCUSSION_UNAVAILABLE", "group discussion service is unavailable")
			return
		}
		veID := strings.TrimSpace(r.PathValue("id"))
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, ownerLookup)
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		employee := registry.Employees[idx]
		if isMacLawSrvRuntimeEmployee(employee) {
			employee = applyVEDiscoverablePresence(r.Context(), employee, nil, loadMacLawSrvRuntimePresence(r.Context(), baseSystem, requestTenantID))
		}
		limit := intQuery(r.URL.Query(), "limit")
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		tenantID := requestGroupDiscussionTenantID(r)
		items, err := groupSvc.ListDiscussionSummaries(tenantID, ListSessionsFilter{ParticipantID: employee.MachineID, Limit: limit})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"employee": employee, "discussions": decorateVEHistorySummaries(r.Context(), tenantID, items, employee, ownerLookup, firstVEMachineLookup(machineLookups...))})
	}
}

// VEHistoryDetailHandler handles GET /api/ve/history/{id}/detail for admin preview.
func VEHistoryDetailHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, ownerLookup veOwnerLookup, machineLookups ...veMachineLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := veSystemSettingsForRequest(r, system)
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		if groupSvc == nil {
			writeError(w, http.StatusInternalServerError, "GROUP_DISCUSSION_UNAVAILABLE", "group discussion service is unavailable")
			return
		}
		discussionID := strings.TrimSpace(r.PathValue("id"))
		if discussionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "discussion id is required")
			return
		}
		detail, err := groupSvc.GetDiscussionDetail(requestGroupDiscussionTenantID(r), discussionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "DISCUSSION_NOT_FOUND", err.Error())
			return
		}
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, ownerLookup)
		employee := veHistoryEmployeeForParticipants(registry, detail.Discussion.ParticipantIDs)
		resolver := newVEHistoryEmailResolver(r.Context(), requestGroupDiscussionTenantID(r), ownerLookup, firstVEMachineLookup(machineLookups...))
		discussion := veHistoryDiscussionSummary{
			HubDiscussionSummary: detail.Discussion,
			CounterpartEmails:    resolver.counterpartEmails(detail.Discussion.ParticipantIDs, employee),
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"discussion":       discussion,
			"session":          detail.Session,
			"messages":         detail.Messages,
			"proposals":        detail.Proposals,
			"reviews":          detail.Reviews,
			"review_summaries": detail.ReviewSummaries,
			"decision":         detail.Decision,
		})
	}
}

type veSettingsRequest struct {
	Name             string   `json:"name"`
	SkillDescription string   `json:"skill_description"`
	AvatarDataURL    *string  `json:"avatar_data_url"`
	AccessPolicy     string   `json:"access_policy"`
	Whitelist        []string `json:"whitelist"`
	Blacklist        []string `json:"blacklist"`
}

const (
	veAvatarImageMaxBytes  = 1024 * 1024
	veAvatarDataURLMaxSize = len("data:image/jpeg;base64,") + ((veAvatarImageMaxBytes+2)/3)*4
	veSettingsBodyMaxBytes = int64(veAvatarDataURLMaxSize + 128*1024)
)

func digitalEmployeeFromRequest(principal *auth.MachinePrincipal, req veSettingsRequest) (digitalEmployeeEntry, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return digitalEmployeeEntry{}, fmt.Errorf("name is required")
	}
	avatarDataURL := ""
	if req.AvatarDataURL != nil {
		avatarDataURL = strings.TrimSpace(*req.AvatarDataURL)
	}
	if avatarDataURL != "" {
		if len(avatarDataURL) > veAvatarDataURLMaxSize {
			return digitalEmployeeEntry{}, fmt.Errorf("avatar image is too large")
		}
		valid, tooLarge := validateVEAvatarDataURL(avatarDataURL)
		if tooLarge {
			return digitalEmployeeEntry{}, fmt.Errorf("avatar image is too large")
		}
		if !valid {
			return digitalEmployeeEntry{}, fmt.Errorf("avatar image must be a data URL")
		}
	}
	policy := normalizeVEAccessPolicy(req.AccessPolicy)
	return digitalEmployeeEntry{
		ID:               veIDForMachine(principal.MachineID),
		MachineID:        principal.MachineID,
		EmployeeType:     veEmployeeTypePhysical,
		OwnerUserID:      principal.UserID,
		Name:             name,
		SkillDescription: strings.TrimSpace(req.SkillDescription),
		AvatarDataURL:    avatarDataURL,
		AccessPolicy:     policy,
		Whitelist:        normalizeVEStringList(req.Whitelist),
		Blacklist:        normalizeVEStringList(req.Blacklist),
	}, nil
}

func decodeVEJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "multiple json values")
		return false
	}
	return true
}

func isValidVEAvatarDataURL(value string) bool {
	valid, _ := validateVEAvatarDataURL(value)
	return valid
}

func validateVEAvatarDataURL(value string) (bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" || len(lower) > veAvatarDataURLMaxSize {
		return false, len(lower) > veAvatarDataURLMaxSize
	}
	declaredType := ""
	switch {
	case strings.HasPrefix(lower, "data:image/png;base64,"):
		declaredType = "image/png"
	case strings.HasPrefix(lower, "data:image/jpeg;base64,"), strings.HasPrefix(lower, "data:image/jpg;base64,"):
		declaredType = "image/jpeg"
	case strings.HasPrefix(lower, "data:image/webp;base64,"):
		declaredType = "image/webp"
	default:
		return false, false
	}
	comma := strings.Index(value, ",")
	if comma < 0 || comma == len(value)-1 {
		return false, false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value[comma+1:])
	if err != nil {
		return false, false
	}
	if len(decoded) > veAvatarImageMaxBytes {
		return false, true
	}
	contentType := http.DetectContentType(decoded)
	return contentType == declaredType, false
}

func authenticateVEMachine(w http.ResponseWriter, r *http.Request, authenticator veMachineAuthenticator) (*auth.MachinePrincipal, bool) {
	if authenticator == nil {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
	token := extractBearerToken(r)
	if machineID == "" || token == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	principal, err := authenticator.AuthenticateMachine(r.Context(), machineID, token)
	if err != nil || principal == nil {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine authorization required")
		return nil, false
	}
	return principal, true
}

func loadVEDigitalEmployeeAuthorization(ctx context.Context, system store.SystemSettingsRepository) *corelib.DigitalEmployeeAuthorization {
	return center.LoadDigitalEmployeeAuthorization(ctx, system)
}

func veSystemSettingsForRequest(r *http.Request, system store.SystemSettingsRepository) store.SystemSettingsRepository {
	if r != nil && system != nil && isTenantScopedAdminRequest(r) && store.NormalizeTenantID(RequestTenantID(r)) == store.DefaultTenantID {
		return defaultTenantAwareSystemSettings{base: system}
	}
	return scopedSystemSettingsForRequest(r, system)
}

func veSystemSettingsForMachine(system store.SystemSettingsRepository, principal *auth.MachinePrincipal) store.SystemSettingsRepository {
	if principal == nil {
		return system
	}
	if store.NormalizeTenantID(principal.TenantID) == store.DefaultTenantID && system != nil {
		return defaultTenantAwareSystemSettings{base: system}
	}
	return scopedSystemSettingsForTenant(principal.TenantID, system)
}

type defaultTenantAwareSystemSettings struct {
	base store.SystemSettingsRepository
}

func (s defaultTenantAwareSystemSettings) Set(ctx context.Context, key, valueJSON string) error {
	return s.base.Set(ctx, key, valueJSON)
}

func (s defaultTenantAwareSystemSettings) Get(ctx context.Context, key string) (string, error) {
	return s.base.Get(ctx, key)
}

func (s defaultTenantAwareSystemSettings) TenantID() string {
	return store.DefaultTenantID
}

func requireVEDigitalEmployeeAuthorization(w http.ResponseWriter, r *http.Request, system store.SystemSettingsRepository) bool {
	if veAuthorizationActive(loadVEDigitalEmployeeAuthorization(r.Context(), system)) {
		return true
	}
	writeError(w, http.StatusForbidden, "VE_AUTHORIZATION_INACTIVE", "digital employee authorization is inactive")
	return false
}

func veAuthorizationActive(authz *corelib.DigitalEmployeeAuthorization) bool {
	return authz != nil && authz.Active && authz.Quota > 0
}

func veAuthorizedQuota(authz *corelib.DigitalEmployeeAuthorization) int {
	if !veAuthorizationActive(authz) {
		return 0
	}
	return authz.Quota
}

func veRegistrationStatus(autoApprove bool, authz *corelib.DigitalEmployeeAuthorization, employees []digitalEmployeeEntry, previousStatus string) string {
	if previousStatus == veStatusDisabled {
		return veStatusPending
	}
	if !autoApprove || !veAuthorizationActive(authz) {
		return veStatusPending
	}
	if previousStatus == veStatusActive || countVEByStatus(employees, veStatusActive) < veAuthorizedQuota(authz) {
		return veStatusActive
	}
	return veStatusPending
}

func autoApprovePendingVERegistrations(ctx context.Context, system, baseSystem store.SystemSettingsRepository, tenantID string) ([]digitalEmployeeEntry, error) {
	authz := loadVEDigitalEmployeeAuthorization(ctx, system)
	if !veAuthorizationActive(authz) {
		return nil, nil
	}
	registry := loadVERegistry(ctx, system)
	activeCount := countVEByStatus(registry.Employees, veStatusActive)
	quota := veAuthorizedQuota(authz)
	approved := make([]digitalEmployeeEntry, 0)
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range registry.Employees {
		if activeCount >= quota {
			break
		}
		if registry.Employees[i].Status != veStatusPending {
			continue
		}
		if checked, ok, _, _ := verifyMacLawSrvRuntimeReadyForActivation(ctx, baseSystem, tenantID, registry.Employees[i]); !ok {
			registry.Employees[i] = checked
			changed = true
			continue
		} else {
			registry.Employees[i] = checked
		}
		registry.Employees[i].Status = veStatusActive
		registry.Employees[i].UpdatedAt = now
		registry.Employees[i].RejectReason = ""
		registry.Employees[i].RejectedAt = ""
		approved = append(approved, registry.Employees[i])
		activeCount++
		changed = true
	}
	if !changed {
		return nil, nil
	}
	if err := saveVERegistry(ctx, system, registry); err != nil {
		return nil, err
	}
	return approved, nil
}

func firstVEMachineEventSender(senders ...veMachineEventSender) veMachineEventSender {
	for _, sender := range senders {
		if sender != nil {
			return sender
		}
	}
	return nil
}

func veAdminActionEventType(action string) string {
	switch strings.TrimSpace(action) {
	case "approve":
		return "ve:approved"
	case "reject":
		return "ve:rejected"
	case "disable":
		return "ve:disabled"
	default:
		return "ve:" + strings.TrimSpace(action)
	}
}

func emitVEAdminActionEvent(sender veMachineEventSender, action string, entry digitalEmployeeEntry) {
	if sender == nil || strings.TrimSpace(entry.MachineID) == "" {
		return
	}
	eventType := veAdminActionEventType(action)
	payload := map[string]any{"employee": entry, "action": action}
	for _, msgType := range []string{eventType, "ve:status_change", "ve:list_update"} {
		_ = sender.SendToMachine(entry.MachineID, map[string]any{
			"type":    msgType,
			"ts":      time.Now().Unix(),
			"payload": payload,
		})
	}
}

func veEmployeeMatchesQuery(employee digitalEmployeeEntry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	fields := []string{employee.ID, employee.MachineID, employee.Name, employee.OwnerEmail, employee.OwnerUserID}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), query) {
			return true
		}
	}
	return false
}

func firstVEOwnerLookup(ownerLookups ...veOwnerLookup) veOwnerLookup {
	for _, lookup := range ownerLookups {
		if lookup != nil {
			return lookup
		}
	}
	return nil
}

func firstVEMachineLookup(machineLookups ...veMachineLookup) veMachineLookup {
	for _, lookup := range machineLookups {
		if lookup != nil {
			return lookup
		}
	}
	return nil
}

func lookupVEOwnerEmail(ctx context.Context, lookup veOwnerLookup, ownerUserID string) string {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" || lookup == nil {
		return ""
	}
	user, err := lookup.GetByID(ctx, ownerUserID)
	if err != nil || user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}

func enrichVERegistryOwners(ctx context.Context, registry *digitalEmployeeRegistry, lookup veOwnerLookup) {
	if registry == nil || lookup == nil {
		return
	}
	for i := range registry.Employees {
		if strings.TrimSpace(registry.Employees[i].OwnerEmail) == "" {
			registry.Employees[i].OwnerEmail = lookupVEOwnerEmail(ctx, lookup, registry.Employees[i].OwnerUserID)
		}
	}
}

func enrichVERegistryEmployeeTypes(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	for i := range registry.Employees {
		typ := inferVEEmployeeType(registry.Employees[i])
		if registry.Employees[i].EmployeeType != typ {
			registry.Employees[i].EmployeeType = typ
			changed = true
		}
	}
	return changed
}

func inferVEEmployeeType(entry digitalEmployeeEntry) string {
	if typ := normalizeVEEmployeeType(entry.EmployeeType); typ != "" {
		return typ
	}
	if strings.TrimSpace(entry.PlatformEmployeeID) != "" {
		return veEmployeeTypeVirtual
	}
	return veEmployeeTypePhysical
}

func normalizeVEEmployeeType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case veEmployeeTypeVirtual:
		return veEmployeeTypeVirtual
	case veEmployeeTypePhysical:
		return veEmployeeTypePhysical
	default:
		return ""
	}
}

func loadVEGroupConfig(ctx context.Context, system store.SystemSettingsRepository) veGroupConfig {
	cfg := veGroupConfig{MaxGroupParticipants: 5}
	if system == nil {
		return cfg
	}
	raw, err := system.Get(ctx, veGroupConfigKey)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if cfg.MaxGroupParticipants < 1 || cfg.MaxGroupParticipants > 10 {
		cfg.MaxGroupParticipants = 5
	}
	return cfg
}

func loadVERegistry(ctx context.Context, system store.SystemSettingsRepository) digitalEmployeeRegistry {
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{}}
	if system == nil {
		return registry
	}
	raw, err := system.Get(ctx, veRegistryKey)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &registry)
	}
	if registry.Employees == nil {
		registry.Employees = []digitalEmployeeEntry{}
	}
	onlineChanged := normalizeVERegistryOnlineStatuses(&registry)
	typeChanged := enrichVERegistryEmployeeTypes(&registry)
	avatarChanged := normalizeVERegistryAvatarDataURLs(&registry)
	visibilityChanged := normalizeVERegistryVisibleGroupIDs(&registry)
	residentChanged := normalizeVERegistryResidentFlags(&registry)
	if onlineChanged || typeChanged || avatarChanged || visibilityChanged || residentChanged {
		_ = saveVERegistry(ctx, system, registry)
	}
	return registry
}

func saveVERegistry(ctx context.Context, system store.SystemSettingsRepository, registry digitalEmployeeRegistry) error {
	normalizeVERegistryOnlineStatuses(&registry)
	enrichVERegistryEmployeeTypes(&registry)
	normalizeVERegistryAvatarDataURLs(&registry)
	normalizeVERegistryVisibleGroupIDs(&registry)
	normalizeVERegistryResidentFlags(&registry)
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	return system.Set(ctx, veRegistryKey, string(data))
}

func normalizeVERegistryResidentFlags(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	seenResident := false
	for idx := range registry.Employees {
		if registry.Employees[idx].Status != veStatusActive && registry.Employees[idx].Resident {
			registry.Employees[idx].Resident = false
			changed = true
			continue
		}
		if !registry.Employees[idx].Resident {
			continue
		}
		if seenResident {
			registry.Employees[idx].Resident = false
			changed = true
			continue
		}
		seenResident = true
	}
	return changed
}

func normalizeVERegistryAvatarDataURLs(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	for idx := range registry.Employees {
		avatar := strings.TrimSpace(registry.Employees[idx].AvatarDataURL)
		if avatar == "" {
			if registry.Employees[idx].AvatarDataURL != "" {
				registry.Employees[idx].AvatarDataURL = ""
				changed = true
			}
			continue
		}
		if !isValidVEAvatarDataURL(avatar) {
			registry.Employees[idx].AvatarDataURL = ""
			changed = true
		} else if avatar != registry.Employees[idx].AvatarDataURL {
			registry.Employees[idx].AvatarDataURL = avatar
			changed = true
		}
	}
	return changed
}

func normalizeVERegistryVisibleGroupIDs(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	for idx := range registry.Employees {
		normalized := normalizeVEStringList(registry.Employees[idx].VisibleGroupIDs)
		if !equalVEStringList(registry.Employees[idx].VisibleGroupIDs, normalized) {
			registry.Employees[idx].VisibleGroupIDs = normalized
			changed = true
		}
	}
	return changed
}

func equalVEStringList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func loadVEAccessRequests(ctx context.Context, system store.SystemSettingsRepository) digitalEmployeeAccessRequestStore {
	requests := digitalEmployeeAccessRequestStore{Requests: []digitalEmployeeAccessRequest{}}
	if system == nil {
		return requests
	}
	raw, err := system.Get(ctx, veAccessRequestsKey)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &requests)
	}
	if requests.Requests == nil {
		requests.Requests = []digitalEmployeeAccessRequest{}
	}
	return requests
}

func saveVEAccessRequests(ctx context.Context, system store.SystemSettingsRepository, requests digitalEmployeeAccessRequestStore) error {
	data, err := json.Marshal(requests)
	if err != nil {
		return err
	}
	return system.Set(ctx, veAccessRequestsKey, string(data))
}

func upsertVEAccessRequest(requests *digitalEmployeeAccessRequestStore, request digitalEmployeeAccessRequest) digitalEmployeeAccessRequest {
	if requests == nil {
		return request
	}
	for i := range requests.Requests {
		item := requests.Requests[i]
		if item.Status == "pending" && groupDiscussionParticipantIdentityMatches(item.RequesterMachineID, request.RequesterMachineID) && groupDiscussionParticipantIdentityMatches(item.TargetMachineID, request.TargetMachineID) {
			request.ID = item.ID
			request.CreatedAt = item.CreatedAt
			requests.Requests[i] = request
			return request
		}
	}
	requests.Requests = append(requests.Requests, request)
	return request
}

func expirePendingVEAccessRequests(requests *digitalEmployeeAccessRequestStore, now time.Time) bool {
	if requests == nil {
		return false
	}
	changed := false
	for i := range requests.Requests {
		req := &requests.Requests[i]
		if req.Status == "pending" && veAccessRequestExpired(*req, now) {
			req.Status = "expired"
			req.UpdatedAt = now.Format(time.RFC3339)
			changed = true
		}
	}
	return changed
}

func newVEAccessRequestID(requests digitalEmployeeAccessRequestStore, requesterMachineID string, now time.Time) string {
	base := fmt.Sprintf("veauth_%s_%d", strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(strings.TrimSpace(requesterMachineID)), now.UnixNano())
	if base == "veauth__0" {
		base = fmt.Sprintf("veauth_%d", now.UnixNano())
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		exists := false
		for _, req := range requests.Requests {
			if req.ID == candidate {
				exists = true
				break
			}
		}
		if !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func findConsumableVEAllowOnceRequestIndex(requests *digitalEmployeeAccessRequestStore, target digitalEmployeeEntry, requesterUserID, requesterMachineID string, now time.Time) int {
	if requests == nil {
		return -1
	}
	for i := range requests.Requests {
		req := requests.Requests[i]
		if req.Status != "allowed" || req.Decision != "allow_once" {
			continue
		}
		if !groupDiscussionParticipantIdentityMatches(req.TargetMachineID, target.MachineID) && !groupDiscussionParticipantIdentityMatches(req.TargetVEID, target.ID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(req.RequesterUserID), strings.TrimSpace(requesterUserID)) && !groupDiscussionParticipantIdentityMatches(req.RequesterMachineID, requesterMachineID) {
			continue
		}
		if veAccessRequestExpired(req, now) {
			requests.Requests[i].Status = "expired"
			requests.Requests[i].UpdatedAt = now.Format(time.RFC3339)
			continue
		}
		return i
	}
	return -1
}

func markVEAccessRequestUsed(requests *digitalEmployeeAccessRequestStore, idx int, now time.Time) {
	if requests == nil || idx < 0 || idx >= len(requests.Requests) {
		return
	}
	requests.Requests[idx].Status = "used"
	requests.Requests[idx].UpdatedAt = now.Format(time.RFC3339)
}

func veAccessRequestExpired(req digitalEmployeeAccessRequest, now time.Time) bool {
	if strings.TrimSpace(req.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
	if err != nil {
		return false
	}
	return !expiresAt.After(now)
}

func normalizeVERegistryOnlineStatuses(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	for i := range registry.Employees {
		status := strings.TrimSpace(registry.Employees[i].OnlineStatus)
		if status == "" || (status != veOnlineStatusOnline && status != veOnlineStatusOffline) {
			registry.Employees[i].OnlineStatus = veOnlineStatusOffline
			changed = true
		}
	}
	return changed
}

func (r digitalEmployeeRegistry) findByMachineID(machineID string) int {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if groupDiscussionParticipantIdentityMatches(entry.MachineID, machineID) {
			return i
		}
	}
	return -1
}

func (r digitalEmployeeRegistry) findByID(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if groupDiscussionParticipantIdentityMatches(entry.ID, id) {
			return i
		}
	}
	return -1
}

func (r digitalEmployeeRegistry) findByIDOrMachineID(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if groupDiscussionParticipantIdentityMatches(entry.ID, id) || groupDiscussionParticipantIdentityMatches(entry.MachineID, id) {
			return i
		}
	}
	return -1
}

func (r digitalEmployeeRegistry) findByPlatformEmployeeID(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if strings.EqualFold(strings.TrimSpace(entry.PlatformEmployeeID), id) {
			return i
		}
	}
	return -1
}

func (r digitalEmployeeRegistry) findByIDOrMachineIDOrPlatformEmployeeID(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if groupDiscussionParticipantIdentityMatches(entry.ID, id) || groupDiscussionParticipantIdentityMatches(entry.MachineID, id) || strings.EqualFold(strings.TrimSpace(entry.PlatformEmployeeID), id) {
			return i
		}
	}
	return -1
}

func veIDForMachine(machineID string) string {
	cleaned := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(strings.TrimSpace(machineID))
	if cleaned == "" {
		return ""
	}
	return "ve_" + cleaned
}

func normalizeVEAccessPolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case "whitelist", "blacklist", "per_request":
		return strings.TrimSpace(policy)
	default:
		return "public"
	}
}

func normalizeVEStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func veAccessAllowed(entry digitalEmployeeEntry, requesterUserID string) bool {
	if containsVEValue(entry.Blacklist, requesterUserID) {
		return false
	}
	switch entry.AccessPolicy {
	case "whitelist":
		return containsVEValue(entry.Whitelist, requesterUserID)
	case "blacklist":
		return true
	case "per_request":
		return true
	default:
		return true
	}
}

func veRequesterAccessID(principal *auth.MachinePrincipal) string {
	if principal == nil {
		return ""
	}
	if userID := strings.TrimSpace(principal.UserID); userID != "" {
		return userID
	}
	return strings.TrimSpace(principal.MachineID)
}

func containsVEValue(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func removeVEValue(values []string, target string) []string {
	target = strings.TrimSpace(target)
	out := values[:0]
	for _, value := range values {
		if !strings.EqualFold(strings.TrimSpace(value), target) {
			out = append(out, value)
		}
	}
	return out
}

func countVEByStatus(employees []digitalEmployeeEntry, status string) int {
	count := 0
	for _, entry := range employees {
		if entry.Status == status {
			count++
		}
	}
	return count
}

func firstNonEmptyVE(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
