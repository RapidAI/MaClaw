package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// mobileDynamicCapabilityPublicationRequest contains only a reviewed semantic
// declaration. It deliberately excludes provider description, schema, content
// digest, and tool metadata: those are observed from the Service runtime by
// the publisher and never trusted from an HTTP caller.
type mobileDynamicCapabilityPublicationRequest struct {
	Confirm    bool                           `json:"confirm"`
	Provisions []coretool.CapabilityProvision `json:"provisions"`
	Effects    []coretool.EffectClass         `json:"effects"`
	Consumes   []coretool.ArtifactContract    `json:"consumes,omitempty"`
	Produces   []coretool.ArtifactContract    `json:"produces,omitempty"`
}

// mobilePublishDynamicMCPContractHandler publishes a reviewed capability
// contract for an observed, ready MCP binding of one mobile agent principal.
// Owner-only (Hub global admin, the Hub equivalent of the MaClawSrv owner
// role); registration wraps it with RequireAdmin + RequireGlobalAdmin.
func mobilePublishDynamicMCPContractHandler(audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in mobileDynamicCapabilityPublicationRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Invalid JSON body")
			return
		}
		if !in.Confirm {
			writeError(w, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm=true is required")
			return
		}
		svc, publisher, err := mobileDynamicCapabilityContractPublisher()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SEMANTIC_ROUTING_UNAVAILABLE", "Mobile semantic routing is unavailable")
			return
		}
		p := mobileDynamicCapabilityPublicationPrincipal(r)
		serverID, toolName := strings.TrimSpace(r.PathValue("serverId")), strings.TrimSpace(r.PathValue("toolName"))
		if err := publisher.PublishObservedMCP(r.Context(), p, serverID, toolName, mobileDynamicCapabilityPublicationContract(in)); err != nil {
			writeError(w, http.StatusBadRequest, "DYNAMIC_CAPABILITY_PUBLISH_FAILED", err.Error())
			return
		}
		contract, ok := svc.DynamicCapabilityContracts().ResolveMCPDynamicContract(r.Context(), p, serverID, toolName)
		if !ok {
			writeError(w, http.StatusInternalServerError, "DYNAMIC_CAPABILITY_PUBLISH_FAILED", "published contract could not be resolved")
			return
		}
		mobileRecordDynamicCapabilityPublicationAudit(r, audit, "admin.dynamic_capability.mcp_published", "mcp:"+serverID+":"+toolName, p, contract)
		writeJSON(w, http.StatusCreated, mobileDynamicCapabilityPublicationResponse(contract))
	}
}

// mobilePublishDynamicSkillContractHandler is the Skill counterpart of
// mobilePublishDynamicMCPContractHandler, keyed by the Skill's immutable
// stable ID.
func mobilePublishDynamicSkillContractHandler(audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in mobileDynamicCapabilityPublicationRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Invalid JSON body")
			return
		}
		if !in.Confirm {
			writeError(w, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm=true is required")
			return
		}
		svc, publisher, err := mobileDynamicCapabilityContractPublisher()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SEMANTIC_ROUTING_UNAVAILABLE", "Mobile semantic routing is unavailable")
			return
		}
		p := mobileDynamicCapabilityPublicationPrincipal(r)
		stableID := strings.TrimSpace(r.PathValue("stableId"))
		if err := publisher.PublishObservedSkill(r.Context(), p, stableID, mobileDynamicCapabilityPublicationContract(in)); err != nil {
			writeError(w, http.StatusBadRequest, "DYNAMIC_CAPABILITY_PUBLISH_FAILED", err.Error())
			return
		}
		contract, ok := svc.DynamicCapabilityContracts().ResolveSkillDynamicContract(r.Context(), p, stableID)
		if !ok {
			writeError(w, http.StatusInternalServerError, "DYNAMIC_CAPABILITY_PUBLISH_FAILED", "published contract could not be resolved")
			return
		}
		mobileRecordDynamicCapabilityPublicationAudit(r, audit, "admin.dynamic_capability.skill_published", "skill:"+stableID, p, contract)
		writeJSON(w, http.StatusCreated, mobileDynamicCapabilityPublicationResponse(contract))
	}
}

func mobileDynamicCapabilityPublicationPrincipal(r *http.Request) agentservice.Principal {
	return agentservice.Principal{TenantID: strings.TrimSpace(r.PathValue("tenantId")), UserID: strings.TrimSpace(r.PathValue("userId"))}
}

func mobileDynamicCapabilityPublicationContract(in mobileDynamicCapabilityPublicationRequest) agentservice.DynamicCapabilityContract {
	return agentservice.DynamicCapabilityContract{
		Provisions: append([]coretool.CapabilityProvision(nil), in.Provisions...),
		Effects:    append([]coretool.EffectClass(nil), in.Effects...),
		Consumes:   append([]coretool.ArtifactContract(nil), in.Consumes...),
		Produces:   append([]coretool.ArtifactContract(nil), in.Produces...),
	}
}

func mobileDynamicCapabilityPublicationResponse(contract agentservice.DynamicCapabilityContract) map[string]any {
	return map[string]any{
		"status": "published", "contract_digest": contract.Digest(),
		"observed_binding_digest": contract.ObservedBindingDigest,
	}
}

func mobileRecordDynamicCapabilityPublicationAudit(r *http.Request, audit store.AdminAuditRepository, action, resource string, p agentservice.Principal, contract agentservice.DynamicCapabilityContract) {
	writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), action, map[string]any{
		"tenant_id": p.TenantID, "user_id": p.UserID, "resource": resource,
		"registry_version": agentservice.ReviewedDynamicCapabilityRegistryVersion,
		"contract_digest":  contract.Digest(), "observed_binding_digest": contract.ObservedBindingDigest,
		"remote_ip": r.RemoteAddr,
	})
}
