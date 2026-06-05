package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreliba2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
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

type veMachineEventSender interface {
	SendToMachine(machineID string, msg any) error
}

type veMachinePresenceGetter interface {
	GetMachineInfo(ctx context.Context, machineID string) (*device.MachineRuntimeInfo, error)
}

type veHistorySearchMatch struct {
	Employee    digitalEmployeeEntry              `json:"employee"`
	Discussions []coreliba2a.HubDiscussionSummary `json:"discussions"`
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

func VEDiscoverableHandler(system store.SystemSettingsRepository, authenticator veMachineAuthenticator, presenceGetters ...veMachinePresenceGetter) http.HandlerFunc {
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
		accessID := veRequesterAccessID(principal)
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive || groupDiscussionParticipantIdentityMatches(entry.MachineID, principal.MachineID) {
				continue
			}
			if !veAccessAllowed(entry, accessID) {
				continue
			}
			entry = applyVEDiscoverablePresence(r.Context(), entry, firstVEMachinePresenceGetter(presenceGetters...))
			employees = append(employees, entry)
		}
		sort.SliceStable(employees, func(i, j int) bool { return employees[i].Name < employees[j].Name })
		writeJSON(w, http.StatusOK, map[string]any{
			"employees":              employees,
			"max_group_participants": cfg.MaxGroupParticipants,
		})
	}
}

func applyVEDiscoverablePresence(ctx context.Context, entry digitalEmployeeEntry, getter veMachinePresenceGetter) digitalEmployeeEntry {
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

func firstVEMachinePresenceGetter(getters ...veMachinePresenceGetter) veMachinePresenceGetter {
	for _, getter := range getters {
		if getter != nil {
			return getter
		}
	}
	return nil
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
	onlineChanged := normalizeVERegistryOnlineStatuses(&registry)
	typeChanged := enrichVERegistryEmployeeTypes(&registry)
	avatarChanged := normalizeVERegistryAvatarDataURLs(&registry)
	if onlineChanged || typeChanged || avatarChanged {
		_ = saveVERegistry(ctx, system, registry)
	}
	return registry
}

func saveVERegistry(ctx context.Context, system store.SystemSettingsRepository, registry digitalEmployeeRegistry) error {
	normalizeVERegistryOnlineStatuses(&registry)
	enrichVERegistryEmployeeTypes(&registry)
	normalizeVERegistryAvatarDataURLs(&registry)
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	return system.Set(ctx, veRegistryKey, string(data))
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
