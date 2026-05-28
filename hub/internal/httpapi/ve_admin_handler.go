package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreliba2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
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
	AccessPolicy       string   `json:"access_policy"`
	Whitelist          []string `json:"whitelist,omitempty"`
	Blacklist          []string `json:"blacklist,omitempty"`
	Status             string   `json:"status"`
	OnlineStatus       string   `json:"online_status"`
	RegisteredAt       string   `json:"registered_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	DisabledAt         string   `json:"disabled_at,omitempty"`
	RejectedAt         string   `json:"rejected_at,omitempty"`
	RejectReason       string   `json:"reject_reason,omitempty"`
}

type digitalEmployeeRegistry struct {
	Employees []digitalEmployeeEntry `json:"employees"`
}

type veMachineAuthenticator interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
}

type veOwnerLookup interface {
	GetByID(ctx context.Context, id string) (*store.User, error)
}

type veMachineEventSender interface {
	SendToMachine(machineID string, msg any) error
}

type veHistorySearchMatch struct {
	Employee    digitalEmployeeEntry              `json:"employee"`
	Discussions []coreliba2a.HubDiscussionSummary `json:"discussions"`
}

const (
	veGroupConfigKey = "ve_group_config"
	veRegistryKey    = "ve_registry"

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
func VEAdminListHandler(system store.SystemSettingsRepository, ownerLookups ...veOwnerLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := veSystemSettingsForRequest(r, system)
		cfg := loadVEGroupConfig(r.Context(), system)
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, firstVEOwnerLookup(ownerLookups...))
		enrichVERegistryEmployeeTypes(&registry)
		employees := registry.Employees
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
				approved, err := autoApprovePendingVERegistrations(r.Context(), system)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
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
		}
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
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
		entry.Status = firstNonEmptyVE(previous.Status, veStatusPending)
		entry.RegisteredAt = previous.RegisteredAt
		entry.OnlineStatus = veOnlineStatusOnline
		entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		registry.Employees[idx] = entry
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

func VEDiscoverableHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, authenticator)
		if !ok {
			return
		}
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
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive || strings.EqualFold(strings.TrimSpace(entry.MachineID), strings.TrimSpace(principal.MachineID)) {
				continue
			}
			if !veAccessAllowed(entry, principal.UserID) {
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

// VEInitiateHandler handles POST /api/ve/{id}/initiate for a machine-owned direct discussion.
func VEInitiateHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, authenticator veMachineAuthenticator) http.HandlerFunc {
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
		if target.Status != veStatusActive {
			writeError(w, http.StatusConflict, "VE_NOT_ACTIVE", "digital employee is not active")
			return
		}
		if strings.EqualFold(strings.TrimSpace(target.MachineID), strings.TrimSpace(principal.MachineID)) {
			writeError(w, http.StatusBadRequest, "VE_SELF_CHAT_REJECTED", "cannot start a digital employee conversation with the same machine")
			return
		}
		if !veAccessAllowed(target, principal.UserID) {
			writeError(w, http.StatusForbidden, "VE_ACCESS_DENIED", "digital employee access is denied")
			return
		}
		topic := strings.TrimSpace(target.Name)
		if topic == "" {
			topic = "Digital employee conversation"
		}
		if session := findReusableVEDirectSession(groupSvc, store.NormalizeTenantID(principal.TenantID), principal.MachineID, target.MachineID); session != nil {
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
			entry.RejectReason = strings.TrimSpace(req.Reason)
			entry.RejectedAt = now
		case "disable":
			entry.Status = veStatusDisabled
			entry.DisabledAt = now
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
func VEHistorySearchHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, ownerLookups ...veOwnerLookup) http.HandlerFunc {
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
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeJSON(w, http.StatusOK, map[string]any{"query": query, "matches": []veHistorySearchMatch{}, "discussions": []coreliba2a.HubDiscussionSummary{}})
			return
		}
		limit := intQuery(r.URL.Query(), "limit")
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, firstVEOwnerLookup(ownerLookups...))
		matches := make([]veHistorySearchMatch, 0)
		flattened := make([]coreliba2a.HubDiscussionSummary, 0)
		seenDiscussions := make(map[string]bool)
		for _, employee := range registry.Employees {
			if !veEmployeeMatchesQuery(employee, query) {
				continue
			}
			items, err := groupSvc.ListDiscussionSummaries(requestGroupDiscussionTenantID(r), ListSessionsFilter{ParticipantID: employee.MachineID, Limit: limit})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			matches = append(matches, veHistorySearchMatch{Employee: employee, Discussions: items})
			for _, item := range items {
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
func VEHistoryHandler(system store.SystemSettingsRepository, groupSvc *GroupDiscussionService, ownerLookups ...veOwnerLookup) http.HandlerFunc {
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
		veID := strings.TrimSpace(r.PathValue("id"))
		registry := loadVERegistry(r.Context(), system)
		enrichVERegistryOwners(r.Context(), &registry, firstVEOwnerLookup(ownerLookups...))
		idx := registry.findByIDOrMachineIDOrPlatformEmployeeID(veID)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "VE_NOT_FOUND", "digital employee not found")
			return
		}
		employee := registry.Employees[idx]
		limit := intQuery(r.URL.Query(), "limit")
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		items, err := groupSvc.ListDiscussionSummaries(requestGroupDiscussionTenantID(r), ListSessionsFilter{ParticipantID: employee.MachineID, Limit: limit})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"employee": employee, "discussions": items})
	}
}

// VEHistoryDetailHandler handles GET /api/ve/history/{id}/detail for admin preview.
func VEHistoryDetailHandler(groupSvc *GroupDiscussionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, detail)
	}
}

type veSettingsRequest struct {
	Name             string   `json:"name"`
	SkillDescription string   `json:"skill_description"`
	AccessPolicy     string   `json:"access_policy"`
	Whitelist        []string `json:"whitelist"`
	Blacklist        []string `json:"blacklist"`
}

func digitalEmployeeFromRequest(principal *auth.MachinePrincipal, req veSettingsRequest) (digitalEmployeeEntry, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return digitalEmployeeEntry{}, fmt.Errorf("name is required")
	}
	policy := normalizeVEAccessPolicy(req.AccessPolicy)
	return digitalEmployeeEntry{
		ID:               veIDForMachine(principal.MachineID),
		MachineID:        principal.MachineID,
		EmployeeType:     veEmployeeTypePhysical,
		OwnerUserID:      principal.UserID,
		Name:             name,
		SkillDescription: strings.TrimSpace(req.SkillDescription),
		AccessPolicy:     policy,
		Whitelist:        normalizeVEStringList(req.Whitelist),
		Blacklist:        normalizeVEStringList(req.Blacklist),
	}, nil
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
	return scopedSystemSettingsForRequest(r, system)
}

func veSystemSettingsForMachine(system store.SystemSettingsRepository, principal *auth.MachinePrincipal) store.SystemSettingsRepository {
	if principal == nil {
		return system
	}
	return scopedSystemSettingsForTenant(principal.TenantID, system)
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

func autoApprovePendingVERegistrations(ctx context.Context, system store.SystemSettingsRepository) ([]digitalEmployeeEntry, error) {
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
	if normalizeVERegistryOnlineStatuses(&registry) || enrichVERegistryEmployeeTypes(&registry) {
		_ = saveVERegistry(ctx, system, registry)
	}
	return registry
}

func saveVERegistry(ctx context.Context, system store.SystemSettingsRepository, registry digitalEmployeeRegistry) error {
	normalizeVERegistryOnlineStatuses(&registry)
	enrichVERegistryEmployeeTypes(&registry)
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	return system.Set(ctx, veRegistryKey, string(data))
}

func normalizeVERegistryOnlineStatuses(registry *digitalEmployeeRegistry) bool {
	if registry == nil {
		return false
	}
	changed := false
	for i := range registry.Employees {
		status := strings.TrimSpace(registry.Employees[i].OnlineStatus)
		if shouldNormalizeVirtualRuntimeEmployeeOnline(registry.Employees[i], status) {
			registry.Employees[i].OnlineStatus = veOnlineStatusOnline
			changed = true
			continue
		}
		if status == "" {
			registry.Employees[i].OnlineStatus = veOnlineStatusOffline
			changed = true
		}
	}
	return changed
}

func shouldNormalizeVirtualRuntimeEmployeeOnline(entry digitalEmployeeEntry, status string) bool {
	if entry.Status != veStatusActive {
		return false
	}
	if status == "platform" {
		return true
	}
	if status != "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.PlatformID), maclawSrvRuntimePlatformID) || strings.EqualFold(strings.TrimSpace(entry.RuntimeProviderID), maclawSrvRuntimePlatformID)
}

func (r digitalEmployeeRegistry) findByMachineID(machineID string) int {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return -1
	}
	for i, entry := range r.Employees {
		if strings.EqualFold(strings.TrimSpace(entry.MachineID), machineID) {
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
		if strings.EqualFold(strings.TrimSpace(entry.ID), id) {
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
		if strings.EqualFold(strings.TrimSpace(entry.ID), id) || strings.EqualFold(strings.TrimSpace(entry.MachineID), id) {
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
		if strings.EqualFold(strings.TrimSpace(entry.ID), id) || strings.EqualFold(strings.TrimSpace(entry.MachineID), id) || strings.EqualFold(strings.TrimSpace(entry.PlatformEmployeeID), id) {
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
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func veAccessAllowed(entry digitalEmployeeEntry, requesterUserID string) bool {
	switch entry.AccessPolicy {
	case "whitelist":
		return containsVEValue(entry.Whitelist, requesterUserID)
	case "blacklist":
		return !containsVEValue(entry.Blacklist, requesterUserID)
	default:
		return true
	}
}

func containsVEValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
