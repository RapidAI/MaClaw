package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const (
	platformHubLLMProviderName = "hub-llm"
	platformHubLLMModel        = "auto"
)

func (s *HTTPServer) withPlatformAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-MaClaw-Admin-Secret")) == "" {
			if token := bearerToken(r.Header.Get("Authorization")); token != "" {
				r = cloneRequestWithHeader(r, "X-MaClaw-Admin-Secret", token)
			}
		}
		s.withAdmin(next)(w, r)
	}
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || strings.Contains(token, " ") {
		return ""
	}
	return token
}

func cloneRequestWithHeader(r *http.Request, key, value string) *http.Request {
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	clone.Header.Set(key, value)
	return clone
}

type platformVirtualEmployeeRequest struct {
	EmployeeID        string            `json:"employee_id"`
	TenantID          string            `json:"tenant_id"`
	PlatformTenantID  string            `json:"platform_tenant_id"`
	TenantName        string            `json:"tenant_name"`
	TenantCode        string            `json:"tenant_code"`
	HubTenantCode     string            `json:"hub_tenant_code"`
	Name              string            `json:"name"`
	Handle            string            `json:"handle"`
	VirtualEmail      string            `json:"virtual_email"`
	SkillDescription  string            `json:"skill_description"`
	SkillTags         platformSkillTags `json:"skill_tags"`
	DefaultLLM        string            `json:"default_llm"`
	LLMServiceGroupID string            `json:"llm_service_group_id"`
	HubLLMEndpoint    string            `json:"hub_llm_endpoint"`
	HubLLMAPIKey      string            `json:"hub_llm_api_key"`
	LLMModel          string            `json:"llm_model"`
	HubLLMViewerToken string            `json:"hub_llm_viewer_token"`
	ViewerToken       string            `json:"viewer_token"`
	AccessToken       string            `json:"access_token"`
}

type platformSkillTags []string

func (t *platformSkillTags) UnmarshalJSON(data []byte) error {
	var tags []string
	if err := json.Unmarshal(data, &tags); err == nil {
		*t = cleanPlatformSkillTags(tags)
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*t = cleanPlatformSkillTags(strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\uff0c' || r == '\uff1b' || r == '\n' || r == '\t'
	}))
	return nil
}

func cleanPlatformSkillTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

type platformRuntimeBinding struct {
	Tenant   agentservice.Tenant
	User     agentservice.User
	Instance agentservice.Instance
}

type platformSourceUserRequest struct {
	TenantID          string               `json:"tenant_id"`
	SourceUser        platformSourceUser   `json:"source_user"`
	SourceUsers       []platformSourceUser `json:"source_users,omitempty"`
	Name              string               `json:"name,omitempty"`
	Description       string               `json:"description,omitempty"`
	InstanceID        string               `json:"instance_id,omitempty"`
	Target            string               `json:"target,omitempty"`
	DefaultLLM        string               `json:"default_llm,omitempty"`
	LLMServiceGroupID string               `json:"llm_service_group_id,omitempty"`
	LLMModel          string               `json:"llm_model,omitempty"`
	HubLLMEndpoint    string               `json:"hub_llm_endpoint,omitempty"`
	HubLLMAPIKey      string               `json:"hub_llm_api_key,omitempty"`
	HubLLMViewerToken string               `json:"hub_llm_viewer_token,omitempty"`
	ViewerToken       string               `json:"viewer_token,omitempty"`
	AccessToken       string               `json:"access_token,omitempty"`
}

type platformSourceUser struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	ExternalID        string `json:"external_id"`
	Email             string `json:"email"`
	DisplayName       string `json:"display_name"`
	Department        string `json:"department"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	AccountType       string `json:"account_type,omitempty"`
	Provider          string `json:"provider,omitempty"`
	IsVirtualEmployee bool   `json:"is_virtual_employee,omitempty"`
}

type platformSourceUserBinding struct {
	Tenant agentservice.Tenant
	User   agentservice.User
	Source platformSourceUser
}

func (s *HTTPServer) handlePlatformCreateVirtualEmployee(w http.ResponseWriter, r *http.Request) {
	var in platformVirtualEmployeeRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	employeeID := strings.TrimSpace(in.EmployeeID)
	if employeeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "employee_id is required"})
		return
	}
	runtimeTenantKey := firstPlatformNonEmpty(in.TenantID, in.PlatformTenantID, "default")
	tenant, err := s.findOrCreatePlatformTenant(r, runtimeTenantKey, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	user, err := s.findOrCreatePlatformUser(r, tenant.ID, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	llmURL := firstPlatformNonEmpty(in.HubLLMEndpoint, "http://127.0.0.1/managed-by-hub")
	llmModel := platformLLMModelFromRequest(in)
	llmKey := firstPlatformNonEmpty(platformLLMCredential(in), "managed-by-hub")
	if err := s.updatePlatformUserLLMConfig(r, principal, llmURL, llmKey, llmModel); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	inst, created, err := s.findOrCreatePlatformInstance(r, principal, in)
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			if s.writePlatformInvalidConfig(w, r, principal) {
				return
			}
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	status := "ready"
	if inst.Readiness.ConfigValid == false || !inst.Readiness.Ready {
		status = "attention"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "created": created, "tenant_id": tenant.ID, "user_id": user.ID, "instance_id": inst.ID, "employee_id": employeeID, "readiness": inst.Readiness})
}

func platformLLMModelFromRequest(in platformVirtualEmployeeRequest) string {
	if model := strings.TrimSpace(in.LLMModel); model != "" {
		return model
	}
	if strings.TrimSpace(in.HubLLMEndpoint) != "" || strings.TrimSpace(in.LLMServiceGroupID) != "" {
		return platformHubLLMModel
	}
	return firstPlatformNonEmpty(in.DefaultLLM, platformHubLLMModel)
}

func platformLLMCredential(in platformVirtualEmployeeRequest) string {
	return firstPlatformNonEmpty(in.HubLLMViewerToken, in.ViewerToken, in.AccessToken, in.HubLLMAPIKey)
}

func (s *HTTPServer) updatePlatformUserLLMConfig(r *http.Request, p agentservice.Principal, llmURL, llmKey, llmModel string) error {
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := platformLLMAppConfig(cfg.AppConfig, llmURL, llmKey, llmModel)
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func platformLLMAppConfig(app corelib.AppConfig, llmURL, llmKey, llmModel string) corelib.AppConfig {
	llmURL = strings.TrimSpace(llmURL)
	llmKey = strings.TrimSpace(llmKey)
	llmModel = strings.TrimSpace(llmModel)
	app.MaclawLLMUrl = llmURL
	app.MaclawLLMKey = llmKey
	app.MaclawLLMModel = llmModel
	app.MaclawLLMCurrentProvider = platformHubLLMProviderName
	app.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:  platformHubLLMProviderName,
		URL:   llmURL,
		Key:   llmKey,
		Model: llmModel,
	}}
	return app
}

func platformSourceUserLLMModelFromRequest(in platformSourceUserRequest) string {
	if model := strings.TrimSpace(in.LLMModel); model != "" {
		return model
	}
	if strings.TrimSpace(in.HubLLMEndpoint) != "" || strings.TrimSpace(in.LLMServiceGroupID) != "" {
		return platformHubLLMModel
	}
	return firstPlatformNonEmpty(in.DefaultLLM, platformHubLLMModel)
}

func platformSourceUserLLMCredential(in platformSourceUserRequest) string {
	return firstPlatformNonEmpty(in.HubLLMViewerToken, in.ViewerToken, in.AccessToken, in.HubLLMAPIKey)
}

func (s *HTTPServer) handlePlatformSourceUserAssistantInstances(w http.ResponseWriter, r *http.Request) {
	binding, ok := s.requirePlatformSourceUserBinding(w, r, platformSourceUserRequest{TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id"))})
	if !ok {
		return
	}
	instances, err := s.platformSourceUserInstances(r, binding)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": instances, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID})
}

func (s *HTTPServer) handlePlatformSourceUserRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	binding, ok := s.requirePlatformSourceUserBinding(w, r, platformSourceUserRequest{TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id"))})
	if !ok {
		return
	}
	item, err := s.platformSourceUserRuntimeStatus(r, binding)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handlePlatformSourceUsersRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.TenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id is required"})
		return
	}
	items := make([]map[string]any, 0, len(in.SourceUsers))
	for _, source := range in.SourceUsers {
		if strings.TrimSpace(source.ID) == "" {
			continue
		}
		binding, err := s.platformSourceUserBindingFromRequest(r, platformSourceUserRequest{TenantID: in.TenantID, SourceUser: source})
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		item, err := s.platformSourceUserRuntimeStatus(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "tenant_id": in.TenantID, "items": items})
}

func (s *HTTPServer) platformSourceUserRuntimeStatus(r *http.Request, binding platformSourceUserBinding) (map[string]any, error) {
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instances, err := s.platformSourceUserInstances(r, binding)
	if err != nil {
		return nil, err
	}
	ready := 0
	var latest time.Time
	for _, inst := range instances {
		if inst.Ready {
			ready++
		}
		latest = maxPlatformTime(latest, inst.UpdatedAt)
	}
	validation, err := s.svc.ValidateUserConfig(r.Context(), principal)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "ready",
		"tenant_id":        binding.Tenant.ID,
		"user_id":          binding.User.ID,
		"source_user_id":   binding.Source.ID,
		"instance_count":   len(instances),
		"ready_instances":  ready,
		"latest_active_at": latest,
		"config_status":    validation,
	}, nil
}

func (s *HTTPServer) handlePlatformCreateSourceUserAssistantInstance(w http.ResponseWriter, r *http.Request) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	binding, ok := s.requirePlatformSourceUserBinding(w, r, in)
	if !ok {
		return
	}
	inst, err := s.svc.CreateInstance(r.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(in.Name, binding.Source.DisplayName, binding.Source.Email, binding.Source.ExternalID, binding.Source.ID), Description: strings.TrimSpace(in.Description), Metadata: platformSourceUserInstanceMetadata(in.TenantID, binding.Source)})
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			if s.writePlatformInvalidConfig(w, r, agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}) {
				return
			}
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": inst, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID})
}

func (s *HTTPServer) handlePlatformSourceUserAssistantLink(w http.ResponseWriter, r *http.Request) {
	s.handlePlatformSourceUserLink(w, r, "assistant")
}

func (s *HTTPServer) handlePlatformSourceUserSettingsLink(w http.ResponseWriter, r *http.Request) {
	s.handlePlatformSourceUserLink(w, r, "settings")
}

func (s *HTTPServer) handlePlatformSourceUserLink(w http.ResponseWriter, r *http.Request, view string) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	binding, ok := s.requirePlatformSourceUserBinding(w, r, in)
	if !ok {
		return
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instanceID := strings.TrimSpace(in.InstanceID)
	createdInstance := false
	if view == "assistant" && instanceID != "" {
		instances, err := s.platformSourceUserInstances(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if !platformInstanceIDExists(instances, instanceID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "assistant instance not found"})
			return
		}
	}
	if view == "assistant" && instanceID == "" {
		instances, err := s.platformSourceUserInstances(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if len(instances) > 0 {
			instanceID = instances[0].ID
		} else {
			inst, err := s.svc.CreateInstance(r.Context(), principal, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(binding.Source.DisplayName, binding.Source.Email, binding.Source.ExternalID, binding.Source.ID), Metadata: platformSourceUserInstanceMetadata(in.TenantID, binding.Source)})
			if err != nil {
				if errors.Is(err, agentservice.ErrInvalidConfig) {
					if s.writePlatformInvalidConfig(w, r, principal) {
						return
					}
				}
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
			instanceID = inst.ID
			createdInstance = true
		}
	}
	credExp := time.Now().UTC().Add(15 * time.Minute)
	cred, err := s.svc.CreateCredential(r.Context(), agentservice.CreateCredentialInput{TenantID: binding.Tenant.ID, UserID: binding.User.ID, Name: "VE Platform web launch", ExpiresAt: &credExp})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	tok, err := s.svc.IssueToken(r.Context(), agentservice.IssueTokenInput{APIKey: cred.APIKey, APISecret: cred.APISecret})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	launchMeta := webLaunchTokenRecord{TenantID: binding.Tenant.ID, UserID: binding.User.ID, SourceUserID: binding.Source.ID, InstanceID: instanceID, View: view}
	launchToken, launchTokenExpiresAt, launchTokenHash, err := s.newWebLaunchToken(tok.AccessToken, tok.ExpiresAt, time.Now().UTC(), launchMeta)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "web.launch_token.created", "web_launch_token", binding.Source.ID, map[string]string{"tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID, "instance_id": instanceID, "view": view, "launch_token_hash_prefix": shortWebLaunchTokenHash(launchTokenHash), "remote_ip": requestClientIP(r)})
	launchURL := platformWebLaunchURL(r, launchToken, view, instanceID, binding)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"url": launchURL, "launch_url": launchURL, "view": view, "expires_at": launchTokenExpiresAt, "access_expires_at": tok.ExpiresAt, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID, "instance_id": instanceID, "created_instance": createdInstance})
}

func (s *HTTPServer) writePlatformInvalidConfig(w http.ResponseWriter, r *http.Request, principal agentservice.Principal) bool {
	validation, err := s.svc.ValidateUserConfig(r.Context(), principal)
	if err != nil || validation == nil {
		return false
	}
	safe := sanitizeConfigValidationForAPI(s.svc.DataRoot(), *validation)
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid config", "config_validation": safe, "issues": safe.Issues})
	return true
}

func platformInstanceIDExists(instances []agentservice.Instance, id string) bool {
	id = strings.TrimSpace(id)
	for _, inst := range instances {
		if inst.ID == id {
			return true
		}
	}
	return false
}

func (s *HTTPServer) platformSourceUserInstances(r *http.Request, binding platformSourceUserBinding) ([]agentservice.Instance, error) {
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instances, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		return nil, err
	}
	filtered := make([]agentservice.Instance, 0, len(instances))
	for _, inst := range instances {
		if platformSourceUserInstanceMatches(binding.Source, inst) {
			filtered = append(filtered, inst)
		}
	}
	return filtered, nil
}

func platformSourceUserInstanceMatches(source platformSourceUser, inst agentservice.Instance) bool {
	sourceID := strings.TrimSpace(source.ID)
	if sourceID == "" {
		return false
	}
	if platformSourceUserIsVirtualEmployee(source) && strings.TrimSpace(inst.Metadata["ve_employee_id"]) == sourceID {
		return true
	}
	if strings.TrimSpace(inst.Metadata["ve_source_user_id"]) == "" && strings.TrimSpace(inst.Metadata["ve_employee_id"]) == sourceID {
		return true
	}
	return strings.TrimSpace(inst.Metadata["ve_source_user_id"]) == sourceID
}

func (s *HTTPServer) handlePlatformDeleteVirtualEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(r.PathValue("employeeId"))
	binding, ok, err := s.findPlatformRuntimeBinding(r, employeeID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !ok {
		binding, ok, err = s.findPlatformRuntimeUserBindingFromDeletePayload(r, employeeID)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "virtual employee runtime not found"})
		return
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	userID := binding.User.ID
	instances, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	managedUser := platformManagedUser(binding.User)
	deletedInstanceIDs := make([]string, 0, len(instances))
	for _, inst := range instances {
		if !managedUser && inst.Metadata["ve_employee_id"] != employeeID && inst.Metadata["ve_source_user_id"] != employeeID {
			continue
		}
		if err := s.svc.DeleteInstance(r.Context(), principal, inst.ID); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		deletedInstanceIDs = append(deletedInstanceIDs, inst.ID)
	}
	remaining, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	userDeleted := false
	userDeleteWarning := ""
	if len(remaining) == 0 && managedUser {
		unprotected := false
		if _, err := s.svc.UpdateUser(r.Context(), binding.Tenant.ID, userID, agentservice.UpdateUserInput{DeleteProtected: &unprotected}); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if err := s.svc.DeleteUser(r.Context(), binding.Tenant.ID, userID); err != nil {
			protected := true
			reason := binding.User.DeleteProtectionReason
			_, _ = s.svc.UpdateUser(r.Context(), binding.Tenant.ID, userID, agentservice.UpdateUserInput{DeleteProtected: &protected, DeleteProtectionReason: &reason})
			userDeleteWarning = err.Error()
		} else {
			userDeleted = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "employee_id": employeeID, "tenant_id": binding.Tenant.ID, "user_id": userID, "instance_id": binding.Instance.ID, "deleted_instance_ids": deletedInstanceIDs, "deleted_instances": len(deletedInstanceIDs), "user_deleted": userDeleted, "remaining_instances": len(remaining), "user_delete_warning": userDeleteWarning})
}

func platformManagedUser(user agentservice.User) bool {
	return user.DeleteProtected && strings.EqualFold(strings.TrimSpace(user.DeleteProtectionReason), "Managed by VE Platform")
}

func (s *HTTPServer) findPlatformRuntimeUserBindingFromDeletePayload(r *http.Request, employeeID string) (platformRuntimeBinding, bool, error) {
	var payload struct {
		TenantID         string `json:"tenant_id"`
		PlatformTenantID string `json:"platform_tenant_id"`
		VirtualEmail     string `json:"virtual_email"`
		HubAccountID     string `json:"hub_account_id"`
	}
	if r.Body == nil {
		return platformRuntimeBinding{}, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return platformRuntimeBinding{}, false, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return platformRuntimeBinding{}, false, nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return platformRuntimeBinding{}, false, err
	}
	email := strings.ToLower(strings.TrimSpace(payload.VirtualEmail))
	hubAccountID := strings.TrimSpace(payload.HubAccountID)
	if email == "" && hubAccountID == "" {
		return platformRuntimeBinding{}, false, nil
	}
	tenantKeys := map[string]bool{}
	for _, key := range []string{payload.TenantID, payload.PlatformTenantID} {
		if key = strings.TrimSpace(key); key != "" {
			tenantKeys[key] = true
		}
	}
	targetTenantScoped := len(tenantKeys) > 0
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return platformRuntimeBinding{}, false, err
	}
	for _, tenant := range tenants {
		if targetTenantScoped && !tenantKeys[tenant.ID] && !strings.Contains(strings.ToLower(tenant.Name), strings.ToLower(payload.TenantID)) && !strings.Contains(strings.ToLower(tenant.Name), strings.ToLower(payload.PlatformTenantID)) {
			continue
		}
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			return platformRuntimeBinding{}, false, err
		}
		for _, user := range users {
			if !platformManagedUser(user) {
				continue
			}
			if email != "" && !strings.EqualFold(strings.TrimSpace(user.Email), email) {
				continue
			}
			if hubAccountID != "" && strings.TrimSpace(user.ID) != hubAccountID {
				continue
			}
			return platformRuntimeBinding{Tenant: tenant, User: user}, true, nil
		}
	}
	_ = employeeID
	return platformRuntimeBinding{}, false, nil
}

func (s *HTTPServer) handlePlatformRuntimeReport(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	usersOut := make([]map[string]any, 0)
	instancesOut := make([]map[string]any, 0)
	readyUsers := 0
	for _, tenant := range tenants {
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		for _, user := range users {
			principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
			instances, err := s.svc.ListInstances(r.Context(), principal)
			if err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
			for _, inst := range instances {
				employeeID := strings.TrimSpace(inst.Metadata["ve_employee_id"])
				if employeeID == "" {
					continue
				}
				status := platformRuntimeStatusFor(tenant, user, inst)
				if status == "ready" {
					readyUsers++
				}
				usersOut = append(usersOut, map[string]any{"employee_id": employeeID, "runtime_user_id": user.ID, "name": firstPlatformNonEmpty(inst.Name, user.Name), "virtual_email": user.Email, "runtime_status": status, "updated_at": maxPlatformTime(user.UpdatedAt, inst.UpdatedAt)})
				instancesOut = append(instancesOut, map[string]any{"instance_id": inst.ID, "employee_id": employeeID, "runtime_user_id": user.ID, "name": inst.Name, "status": inst.Status, "ready": inst.Ready, "ready_reason": inst.ReadyReason, "updated_at": inst.UpdatedAt})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "generated_at": now, "service": map[string]any{"status": "ok", "message": "MaClawSrv platform runtime report ready"}, "summary": map[string]any{"active_users": len(usersOut), "ready_users": readyUsers, "error_users": 0, "runtime_errors": 0}, "users": usersOut, "instances": instancesOut, "errors": []any{}})
}

func platformRuntimeStatusFor(tenant agentservice.Tenant, user agentservice.User, inst agentservice.Instance) string {
	if tenant.Status != agentservice.TenantStatusActive || user.Status != agentservice.UserStatusActive {
		return "attention"
	}
	if !inst.Ready {
		return "attention"
	}
	if strings.TrimSpace(string(inst.Status)) == "" {
		return "attention"
	}
	return string(inst.Status)
}

func (s *HTTPServer) requirePlatformSourceUserBinding(w http.ResponseWriter, r *http.Request, in platformSourceUserRequest) (platformSourceUserBinding, bool) {
	sourceID := strings.TrimSpace(r.PathValue("sourceUserId"))
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source user id is required"})
		return platformSourceUserBinding{}, false
	}
	if strings.TrimSpace(in.TenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id is required"})
		return platformSourceUserBinding{}, false
	}
	if strings.TrimSpace(in.SourceUser.ID) == "" {
		in.SourceUser.ID = sourceID
	}
	if in.SourceUser.ID != sourceID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source user id mismatch"})
		return platformSourceUserBinding{}, false
	}
	binding, err := s.platformSourceUserBindingFromRequest(r, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return platformSourceUserBinding{}, false
	}
	return binding, true
}

func (s *HTTPServer) platformSourceUserBindingFromRequest(r *http.Request, in platformSourceUserRequest) (platformSourceUserBinding, error) {
	tenant, user, err := s.findOrCreatePlatformSourceUser(r, in)
	if err != nil {
		return platformSourceUserBinding{}, err
	}
	if err := s.updatePlatformSourceUserLLMConfig(r, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, in); err != nil {
		return platformSourceUserBinding{}, err
	}
	return platformSourceUserBinding{Tenant: *tenant, User: *user, Source: in.SourceUser}, nil
}

func (s *HTTPServer) findOrCreatePlatformSourceUser(r *http.Request, in platformSourceUserRequest) (*agentservice.Tenant, *agentservice.User, error) {
	if binding, ok, err := s.findExistingVirtualSourceUserBinding(r, in); err != nil {
		return nil, nil, err
	} else if ok {
		if err := s.repairPlatformHubServiceGroupModel(r, binding); err != nil {
			return nil, nil, err
		}
		return &binding.Tenant, &binding.User, nil
	}
	virtualEmployee := platformSourceUserIsVirtualEmployee(in.SourceUser)
	virtualEmail := platformSourceUserRuntimeEmail(in.SourceUser)
	skillDescription := firstPlatformNonEmpty(in.Description, "VE Platform source user web assistant")
	if virtualEmployee {
		virtualEmail = firstPlatformNonEmpty(in.SourceUser.Email, virtualEmail)
		skillDescription = firstPlatformNonEmpty(in.Description, in.SourceUser.Title, "VE Platform virtual employee web assistant")
	}
	ve := platformVirtualEmployeeRequest{EmployeeID: in.SourceUser.ID, TenantID: in.TenantID, PlatformTenantID: in.TenantID, TenantName: in.TenantID, Name: firstPlatformNonEmpty(in.SourceUser.DisplayName, in.SourceUser.Email, in.SourceUser.ExternalID, in.SourceUser.ID), Handle: sanitizePlatformEmailLocal(firstPlatformNonEmpty(in.SourceUser.ExternalID, in.SourceUser.Email, in.SourceUser.ID)), VirtualEmail: virtualEmail, SkillDescription: skillDescription, DefaultLLM: in.DefaultLLM, LLMServiceGroupID: in.LLMServiceGroupID, LLMModel: in.LLMModel, HubLLMEndpoint: in.HubLLMEndpoint, HubLLMAPIKey: in.HubLLMAPIKey, HubLLMViewerToken: in.HubLLMViewerToken, ViewerToken: in.ViewerToken, AccessToken: in.AccessToken}
	tenant, err := s.findOrCreatePlatformTenant(r, in.TenantID, ve)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.findOrCreatePlatformUser(r, tenant.ID, ve)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ensurePlatformSourceUserDefaultConfig(r, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}); err != nil {
		return nil, nil, err
	}
	return tenant, user, nil
}

func (s *HTTPServer) updatePlatformSourceUserLLMConfig(r *http.Request, p agentservice.Principal, in platformSourceUserRequest) error {
	llmURL := strings.TrimSpace(in.HubLLMEndpoint)
	llmKey := strings.TrimSpace(platformSourceUserLLMCredential(in))
	llmModel := platformSourceUserLLMModelFromRequest(in)
	if llmURL == "" || llmKey == "" || llmModel == "" {
		return nil
	}
	return s.updatePlatformUserLLMConfig(r, p, llmURL, llmKey, llmModel)
}

func (s *HTTPServer) repairPlatformHubServiceGroupModel(r *http.Request, binding platformRuntimeBinding) error {
	groupID := strings.TrimSpace(binding.Instance.Metadata["llm_service_group_id"])
	if groupID == "" {
		return nil
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	cfg, err := s.svc.GetUserConfig(r.Context(), principal)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	changed := false
	if (platformHubLLMEndpoint(app.MaclawLLMUrl) || strings.EqualFold(strings.TrimSpace(app.MaclawLLMCurrentProvider), platformHubLLMProviderName)) && strings.EqualFold(strings.TrimSpace(app.MaclawLLMModel), groupID) {
		app.MaclawLLMModel = platformHubLLMModel
		changed = true
	}
	for i := range app.MaclawLLMProviders {
		provider := app.MaclawLLMProviders[i]
		isCurrentHubProvider := strings.EqualFold(strings.TrimSpace(provider.Name), strings.TrimSpace(app.MaclawLLMCurrentProvider)) && strings.EqualFold(strings.TrimSpace(provider.Name), platformHubLLMProviderName)
		if (platformHubLLMEndpoint(provider.URL) || isCurrentHubProvider) && strings.EqualFold(strings.TrimSpace(provider.Model), groupID) {
			app.MaclawLLMProviders[i].Model = platformHubLLMModel
			changed = true
		}
	}
	if !changed {
		return nil
	}
	_, err = s.svc.UpdateUserConfig(r.Context(), principal, app)
	return err
}

func platformHubLLMEndpoint(rawURL string) bool {
	value := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(value, "/api/llm/v1") || strings.Contains(value, "managed-by-hub")
}

func (s *HTTPServer) findExistingVirtualSourceUserBinding(r *http.Request, in platformSourceUserRequest) (platformRuntimeBinding, bool, error) {
	if strings.TrimSpace(in.SourceUser.ID) == "" {
		return platformRuntimeBinding{}, false, nil
	}
	if !platformSourceUserIsVirtualEmployee(in.SourceUser) && platformSourceUserHasIdentity(in.SourceUser) {
		return platformRuntimeBinding{}, false, nil
	}
	binding, ok, err := s.findPlatformRuntimeBinding(r, in.SourceUser.ID)
	if err != nil || !ok {
		return platformRuntimeBinding{}, ok, err
	}
	platformTenantID := strings.TrimSpace(binding.Instance.Metadata["ve_platform_tenant_id"])
	hubTenantID := strings.TrimSpace(binding.Instance.Metadata["ve_hub_tenant_id"])
	requestedTenantID := strings.TrimSpace(in.TenantID)
	if requestedTenantID != "" && platformTenantID != requestedTenantID && hubTenantID != requestedTenantID {
		if platformTenantID != "" || hubTenantID != "" {
			return platformRuntimeBinding{}, false, nil
		}
		if email := strings.TrimSpace(in.SourceUser.Email); email != "" && !strings.EqualFold(strings.TrimSpace(binding.User.Email), email) {
			return platformRuntimeBinding{}, false, nil
		}
	}
	return binding, true, nil
}

func platformSourceUserHasIdentity(source platformSourceUser) bool {
	return strings.TrimSpace(source.Email) != "" || strings.TrimSpace(source.ExternalID) != "" || strings.TrimSpace(source.DisplayName) != "" || strings.TrimSpace(source.AccountType) != "" || strings.TrimSpace(source.Provider) != ""
}

func platformSourceUserIsVirtualEmployee(source platformSourceUser) bool {
	if source.IsVirtualEmployee {
		return true
	}
	accountType := strings.ToLower(strings.TrimSpace(source.AccountType))
	provider := strings.ToLower(strings.TrimSpace(source.Provider))
	return accountType == "virtual_employee" || accountType == "digital_employee" || provider == "virtualemployee-platform" || provider == "virtual_employee_platform" || provider == "virtualemployee"
}

func (s *HTTPServer) ensurePlatformSourceUserDefaultConfig(r *http.Request, p agentservice.Principal) error {
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	if strings.TrimSpace(app.MaclawLLMUrl) != "" && strings.TrimSpace(app.MaclawLLMKey) != "" && strings.TrimSpace(app.MaclawLLMModel) != "" {
		return nil
	}
	if strings.TrimSpace(app.MaclawLLMUrl) == "" {
		app.MaclawLLMUrl = "http://127.0.0.1/managed-by-hub"
	}
	if strings.TrimSpace(app.MaclawLLMKey) == "" {
		app.MaclawLLMKey = "managed-by-hub"
	}
	if strings.TrimSpace(app.MaclawLLMModel) == "" {
		app.MaclawLLMModel = platformHubLLMModel
	}
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func platformSourceUserInstanceMetadata(platformTenantID string, source platformSourceUser) map[string]string {
	metadata := map[string]string{"ve_source_user_id": source.ID, "ve_source_user_external_id": source.ExternalID, "ve_source_user_email": source.Email, "ve_platform_tenant_id": platformTenantID, "ve_source_user_department": source.Department, "ve_source_user_title": source.Title}
	if platformSourceUserIsVirtualEmployee(source) {
		metadata["ve_employee_id"] = source.ID
	}
	return compactPlatformMetadata(metadata)
}

func platformSourceUserRuntimeEmail(source platformSourceUser) string {
	seed := firstPlatformNonEmpty(source.ID, source.ExternalID, source.Email, "source-user")
	local := sanitizePlatformEmailLocal(firstPlatformNonEmpty(source.ID, source.ExternalID, source.Email, "source-user")) + "-" + shortPlatformHash(seed)
	return local + "@ve-platform.local"
}

func platformWebLaunchURL(r *http.Request, launchToken, view, instanceID string, binding platformSourceUserBinding) string {
	scheme := platformLaunchScheme(r.Header.Get("X-Forwarded-Proto"))
	host := platformLaunchHost(firstPlatformNonEmpty(r.Header.Get("X-Forwarded-Host"), r.Host))
	q := url.Values{}
	q.Set("launch_token", launchToken)
	q.Set("view", view)
	q.Set("tenant_id", binding.Tenant.ID)
	q.Set("user_id", binding.User.ID)
	q.Set("source_user_id", binding.Source.ID)
	if instanceID != "" {
		q.Set("instance_id", instanceID)
	}
	return scheme + "://" + host + "/app/?" + q.Encode()
}

func platformLaunchScheme(value string) string {
	scheme := strings.ToLower(platformForwardedHeaderFirst(value))
	if scheme == "https" {
		return "https"
	}
	return "http"
}

func platformLaunchHost(value string) string {
	host := platformForwardedHeaderFirst(value)
	if host == "" || strings.ContainsAny(host, " \t\r\n/@?#\\%\"'") {
		return "127.0.0.1"
	}
	return host
}

func platformForwardedHeaderFirst(value string) string {
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func maxPlatformTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (s *HTTPServer) handlePlatformKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	binding, ok := s.requirePlatformRuntimeBinding(w, r, r.PathValue("employeeId"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "employee_id": r.PathValue("employeeId"), "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "instance_id": binding.Instance.ID, "kind": "knowledge_import"})
}

func (s *HTTPServer) handlePlatformMigrationImport(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	binding, ok := s.requirePlatformRuntimeBinding(w, r, r.PathValue("employeeId"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "employee_id": r.PathValue("employeeId"), "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "instance_id": binding.Instance.ID, "kind": "migration_import"})
}

func (s *HTTPServer) handlePlatformSyncJobRun(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	if employeeID := platformString(payload, "employee_id"); employeeID != "" {
		if _, ok := s.requirePlatformRuntimeBinding(w, r, employeeID); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "job_id": r.PathValue("jobId"), "conflicts": []any{}, "next_cursor": ""})
}

func (s *HTTPServer) handlePlatformSyncConflictResolve(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	if employeeID := platformString(payload, "employee_id"); employeeID != "" {
		if _, ok := s.requirePlatformRuntimeBinding(w, r, employeeID); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "resolved", "conflict_id": r.PathValue("conflictId")})
}

func (s *HTTPServer) requirePlatformRuntimeBinding(w http.ResponseWriter, r *http.Request, employeeID string) (platformRuntimeBinding, bool) {
	binding, ok, err := s.findPlatformRuntimeBinding(r, employeeID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return platformRuntimeBinding{}, false
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "virtual employee runtime not found"})
		return platformRuntimeBinding{}, false
	}
	return binding, true
}

func (s *HTTPServer) findOrCreatePlatformTenant(r *http.Request, tenantKey string, in platformVirtualEmployeeRequest) (*agentservice.Tenant, error) {
	name := platformTenantDisplayName(tenantKey, in)
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return nil, err
	}
	for i := range tenants {
		if platformTenantMatches(r, s, tenants[i], tenantKey, in, name) {
			return s.renamePlatformTenantIfNeeded(r, tenants[i], name)
		}
	}
	return s.svc.CreateTenant(r.Context(), agentservice.CreateTenantInput{Name: name, DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
}

func (s *HTTPServer) renamePlatformTenantIfNeeded(r *http.Request, tenant agentservice.Tenant, name string) (*agentservice.Tenant, error) {
	if tenant.Name == name {
		return &tenant, nil
	}
	return s.svc.UpdateTenant(r.Context(), tenant.ID, agentservice.UpdateTenantInput{Name: &name})
}

func platformTenantMatches(r *http.Request, s *HTTPServer, tenant agentservice.Tenant, tenantKey string, in platformVirtualEmployeeRequest, displayName string) bool {
	if tenant.Name == "VE Platform "+strings.TrimSpace(tenantKey) {
		return true
	}
	if !platformManagedTenant(tenant) {
		return false
	}
	if tenant.Name == displayName {
		return true
	}
	code := firstPlatformNonEmpty(in.HubTenantCode, in.TenantCode)
	if code != "" && strings.Contains(tenant.Name, "("+code+")") {
		return true
	}
	return platformTenantHasRuntimeIdentity(r, s, tenant.ID, tenantKey, in)
}

func platformManagedTenant(tenant agentservice.Tenant) bool {
	return tenant.DeleteProtected && strings.EqualFold(strings.TrimSpace(tenant.DeleteProtectionReason), "Managed by VE Platform")
}

func platformTenantHasRuntimeIdentity(r *http.Request, s *HTTPServer, tenantID, tenantKey string, in platformVirtualEmployeeRequest) bool {
	users, err := s.svc.ListUsers(r.Context(), tenantID, agentservice.ListUsersAdminInput{})
	if err != nil {
		return false
	}
	for _, user := range users {
		instances, err := s.svc.ListInstances(r.Context(), agentservice.Principal{TenantID: tenantID, UserID: user.ID})
		if err != nil {
			continue
		}
		for _, inst := range instances {
			hubTenantID := strings.TrimSpace(tenantKey)
			platformTenantID := strings.TrimSpace(in.PlatformTenantID)
			hubTenantCode := strings.TrimSpace(in.HubTenantCode)
			if hubTenantID != "" && inst.Metadata["ve_hub_tenant_id"] == hubTenantID {
				return true
			}
			if platformTenantID != "" && inst.Metadata["ve_platform_tenant_id"] == platformTenantID {
				return true
			}
			if hubTenantCode != "" && inst.Metadata["ve_hub_tenant_code"] == hubTenantCode {
				return true
			}
		}
	}
	return false
}

func platformTenantDisplayName(tenantKey string, in platformVirtualEmployeeRequest) string {
	name := strings.TrimSpace(in.TenantName)
	code := firstPlatformNonEmpty(in.HubTenantCode, in.TenantCode)
	if name == "" {
		name = code
	}
	if name == "" {
		name = strings.TrimSpace(tenantKey)
	}
	if code != "" && !strings.EqualFold(name, code) {
		return "VE Platform " + name + " (" + code + ")"
	}
	return "VE Platform " + name
}

func (s *HTTPServer) findOrCreatePlatformUser(r *http.Request, tenantID string, in platformVirtualEmployeeRequest) (*agentservice.User, error) {
	email := platformRuntimeEmail(in)
	name := firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID)
	users, err := s.svc.ListUsers(r.Context(), tenantID, agentservice.ListUsersAdminInput{Email: email})
	if err != nil {
		return nil, err
	}
	for i := range users {
		if strings.EqualFold(users[i].Email, email) {
			return s.updatePlatformUserIfNeeded(r, tenantID, users[i], name)
		}
	}
	return s.svc.CreateUser(r.Context(), agentservice.CreateUserInput{TenantID: tenantID, Name: name, Email: email, DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
}

func platformRuntimeEmail(in platformVirtualEmployeeRequest) string {
	if email := strings.TrimSpace(in.VirtualEmail); email != "" {
		return email
	}
	seed := firstPlatformNonEmpty(in.EmployeeID, in.Handle, "employee")
	base := sanitizePlatformEmailLocal(firstPlatformNonEmpty(in.Handle, in.EmployeeID, "employee"))
	local := base + "-" + shortPlatformHash(seed)
	return local + "@ve-platform.local"
}

func shortPlatformHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:8]
}

func sanitizePlatformEmailLocal(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "employee"
	}
	return out
}

func (s *HTTPServer) updatePlatformUserIfNeeded(r *http.Request, tenantID string, user agentservice.User, name string) (*agentservice.User, error) {
	if !platformManagedUser(user) || user.Name == name {
		return &user, nil
	}
	return s.svc.UpdateUser(r.Context(), tenantID, user.ID, agentservice.UpdateUserInput{Name: &name})
}

func (s *HTTPServer) findOrCreatePlatformInstance(r *http.Request, p agentservice.Principal, in platformVirtualEmployeeRequest) (*agentservice.Instance, bool, error) {
	instances, err := s.svc.ListInstances(r.Context(), p)
	if err != nil {
		return nil, false, err
	}
	for i := range instances {
		if instances[i].Metadata["ve_employee_id"] == strings.TrimSpace(in.EmployeeID) {
			inst, err := s.updatePlatformInstanceIfNeeded(r, p, instances[i], in)
			return inst, false, err
		}
	}
	inst, err := s.svc.CreateInstance(r.Context(), p, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID), Description: strings.TrimSpace(in.SkillDescription), Metadata: platformInstanceMetadata(in), AllowInvalidConfig: platformLLMCredential(in) == ""})
	return inst, true, err
}

func (s *HTTPServer) updatePlatformInstanceIfNeeded(r *http.Request, p agentservice.Principal, inst agentservice.Instance, in platformVirtualEmployeeRequest) (*agentservice.Instance, error) {
	name := firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID)
	description := strings.TrimSpace(in.SkillDescription)
	metadata := mergePlatformInstanceMetadata(inst.Metadata, platformInstanceMetadata(in))
	update := agentservice.UpdateInstanceInput{}
	if inst.Name != name {
		update.Name = &name
	}
	if inst.Description != description {
		update.Description = &description
	}
	if !stringMapEqual(inst.Metadata, metadata) {
		update.Metadata = metadata
	}
	if update.Name == nil && update.Description == nil && update.Metadata == nil {
		return &inst, nil
	}
	return s.svc.UpdateInstance(r.Context(), p, inst.ID, update)
}

func mergePlatformInstanceMetadata(existing, platform map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range platform {
		if strings.TrimSpace(value) != "" {
			merged[key] = value
			continue
		}
		delete(merged, key)
	}
	return merged
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		if b[key] != av {
			return false
		}
	}
	return true
}

func platformInstanceMetadata(in platformVirtualEmployeeRequest) map[string]string {
	return compactPlatformMetadata(map[string]string{
		"ve_employee_id":        strings.TrimSpace(in.EmployeeID),
		"ve_handle":             strings.TrimSpace(in.Handle),
		"ve_platform_tenant_id": strings.TrimSpace(in.PlatformTenantID),
		"ve_hub_tenant_id":      strings.TrimSpace(in.TenantID),
		"ve_tenant_code":        strings.TrimSpace(in.TenantCode),
		"ve_hub_tenant_code":    strings.TrimSpace(in.HubTenantCode),
		"llm_service_group_id":  firstPlatformNonEmpty(in.LLMServiceGroupID, in.DefaultLLM),
	})
}

func compactPlatformMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		value = strings.TrimSpace(value)
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func (s *HTTPServer) findPlatformRuntimeBinding(r *http.Request, employeeID string) (platformRuntimeBinding, bool, error) {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" {
		return platformRuntimeBinding{}, false, nil
	}
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return platformRuntimeBinding{}, false, err
	}
	for _, tenant := range tenants {
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			return platformRuntimeBinding{}, false, err
		}
		for _, user := range users {
			principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
			instances, err := s.svc.ListInstances(r.Context(), principal)
			if err != nil {
				return platformRuntimeBinding{}, false, err
			}
			for _, inst := range instances {
				if inst.Metadata["ve_employee_id"] == employeeID {
					return platformRuntimeBinding{Tenant: tenant, User: user, Instance: inst}, true, nil
				}
			}
		}
	}
	return platformRuntimeBinding{}, false, nil
}

func platformString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstPlatformNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodePlatformJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body", "detail": err.Error()})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body", "detail": "multiple json values"})
		return false
	}
	return true
}
