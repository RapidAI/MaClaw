package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// mobileDynamicEffectResolutionRequest is an operator's finding about one
// operation that ended unknown. It names no tenant, user, plan, selection or
// provider: those are recovered from the operation ledger, so the request
// cannot aim a verdict at an operation other than the one in the path.
type mobileDynamicEffectResolutionRequest struct {
	Confirm bool `json:"confirm"`
	// Succeeded must be stated explicitly. There is no default, because the
	// state this replaces is precisely "nobody knows", and a zero value that
	// silently means "it failed" would turn an omitted field into a verdict.
	Succeeded  *bool  `json:"succeeded"`
	Evidence   string `json:"evidence"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// mobileResolveUnknownDynamicEffectHandler is the out-of-band exit for an
// operation the mobile core agent dispatched but could never observe. It is the
// Hub counterpart of the MaClawSrv admin route and keeps the same bargain:
// everywhere else the answer to "did this happen" comes from the channel, and
// here it comes from a person, so the substitution is made visible rather than
// convenient. Owner-only (registration wraps it with RequireAdmin +
// RequireGlobalAdmin), confirm=true, evidence required, audited.
func mobileResolveUnknownDynamicEffectHandler(audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in mobileDynamicEffectResolutionRequest
		if !decodeJSON(w, r, &in) {
			return
		}
		if !in.Confirm {
			writeError(w, http.StatusBadRequest, "CONFIRM_REQUIRED", "confirm=true is required")
			return
		}
		if in.Succeeded == nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "succeeded must be stated explicitly")
			return
		}
		if strings.TrimSpace(in.Evidence) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "evidence is required")
			return
		}
		// The operator is the authenticated admin, never a name from the body.
		// A verdict this path can produce is only as accountable as the
		// identity attached to it, so an unnamed caller gets none.
		resolvedBy := mobileDynamicEffectResolutionOperator(r)
		if resolvedBy == "" {
			writeError(w, http.StatusForbidden, "OPERATOR_UNIDENTIFIED", "resolution operator could not be identified")
			return
		}
		_, svc, err := mobileEnsureCoreAgent()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "SEMANTIC_ROUTING_UNAVAILABLE", "Mobile semantic routing is unavailable")
			return
		}
		operationID := strings.TrimSpace(r.PathValue("operationId"))
		if err := svc.ResolveUnknownDynamicEffect(agentservice.DynamicSemanticManualResolution{
			OperationID: operationID, Succeeded: *in.Succeeded,
			Evidence: in.Evidence, ResolvedBy: resolvedBy, ReasonCode: in.ReasonCode,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "DYNAMIC_EFFECT_RESOLVE_FAILED", err.Error())
			return
		}
		outcome := "failed"
		if *in.Succeeded {
			outcome = "succeeded"
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "admin.dynamic_effect.unknown_resolved", map[string]any{
			"resource": operationID, "outcome": outcome, "resolved_by": resolvedBy,
			"reason_code": strings.TrimSpace(in.ReasonCode), "remote_ip": r.RemoteAddr,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "resolved", "outcome": outcome})
	}
}

// mobileDynamicEffectResolutionOperator names who is answerable for a verdict,
// from the authenticated admin identity only.
func mobileDynamicEffectResolutionOperator(r *http.Request) string {
	admin := AdminFromContext(r.Context())
	if admin == nil {
		return ""
	}
	if name := strings.TrimSpace(admin.Username); name != "" {
		return name
	}
	return strings.TrimSpace(admin.ID)
}
