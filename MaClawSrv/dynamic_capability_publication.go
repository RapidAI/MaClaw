package main

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// dynamicCapabilityPublicationRequest contains only a reviewed semantic
// declaration. It deliberately excludes provider description, schema, content
// digest, and tool metadata: those are observed from the Service runtime by
// the publisher and never trusted from an HTTP caller.
type dynamicCapabilityPublicationRequest struct {
	Confirm    bool                           `json:"confirm"`
	Provisions []coretool.CapabilityProvision `json:"provisions"`
	Effects    []coretool.EffectClass         `json:"effects"`
	Consumes   []coretool.ArtifactContract    `json:"consumes,omitempty"`
	Produces   []coretool.ArtifactContract    `json:"produces,omitempty"`
}

func (s *HTTPServer) handlePublishDynamicMCPContract(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in dynamicCapabilityPublicationRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	p := dynamicCapabilityPublicationPrincipal(r)
	serverID, toolName := strings.TrimSpace(r.PathValue("serverId")), strings.TrimSpace(r.PathValue("toolName"))
	if err := s.dynamicCapabilityPublisher.PublishObservedMCP(r.Context(), p, serverID, toolName, dynamicCapabilityPublicationContract(in)); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	contract, ok := s.svc.DynamicCapabilityContracts().ResolveMCPDynamicContract(r.Context(), p, serverID, toolName)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "published contract could not be resolved"})
		return
	}
	s.recordDynamicCapabilityPublicationAudit(r, "admin.dynamic_capability.mcp_published", "mcp_dynamic_capability", serverID+":"+toolName, p, contract)
	writeJSON(w, http.StatusCreated, dynamicCapabilityPublicationResponse(contract))
}

func (s *HTTPServer) handlePublishDynamicSkillContract(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in dynamicCapabilityPublicationRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	p := dynamicCapabilityPublicationPrincipal(r)
	stableID := strings.TrimSpace(r.PathValue("stableId"))
	if err := s.dynamicCapabilityPublisher.PublishObservedSkill(r.Context(), p, stableID, dynamicCapabilityPublicationContract(in)); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	contract, ok := s.svc.DynamicCapabilityContracts().ResolveSkillDynamicContract(r.Context(), p, stableID)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "published contract could not be resolved"})
		return
	}
	s.recordDynamicCapabilityPublicationAudit(r, "admin.dynamic_capability.skill_published", "skill_dynamic_capability", stableID, p, contract)
	writeJSON(w, http.StatusCreated, dynamicCapabilityPublicationResponse(contract))
}

func dynamicCapabilityPublicationPrincipal(r *http.Request) agentservice.Principal {
	return agentservice.Principal{TenantID: strings.TrimSpace(r.PathValue("tenantId")), UserID: strings.TrimSpace(r.PathValue("userId"))}
}

func dynamicCapabilityPublicationContract(in dynamicCapabilityPublicationRequest) agentservice.DynamicCapabilityContract {
	return agentservice.DynamicCapabilityContract{
		Provisions: append([]coretool.CapabilityProvision(nil), in.Provisions...),
		Effects:    append([]coretool.EffectClass(nil), in.Effects...),
		Consumes:   append([]coretool.ArtifactContract(nil), in.Consumes...),
		Produces:   append([]coretool.ArtifactContract(nil), in.Produces...),
	}
}

func dynamicCapabilityPublicationResponse(contract agentservice.DynamicCapabilityContract) map[string]any {
	return map[string]any{
		"status": "published", "contract_digest": contract.Digest(),
		"observed_binding_digest": contract.ObservedBindingDigest,
	}
}

func (s *HTTPServer) recordDynamicCapabilityPublicationAudit(r *http.Request, action, resourceType, resourceID string, p agentservice.Principal, contract agentservice.DynamicCapabilityContract) {
	_ = s.recordAdminAudit(r.Context(), action, resourceType, resourceID, map[string]string{
		"tenant_id": p.TenantID, "user_id": p.UserID,
		"registry_version": agentservice.ReviewedDynamicCapabilityRegistryVersion,
		"contract_digest":  contract.Digest(), "observed_binding_digest": contract.ObservedBindingDigest,
		"remote_ip": requestClientIP(r),
	})
}
