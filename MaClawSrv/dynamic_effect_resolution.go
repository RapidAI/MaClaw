package main

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// dynamicEffectResolutionRequest is an operator's finding about one operation
// that ended unknown. It names no tenant, user, plan, selection or provider:
// those are recovered from the operation ledger, so the request cannot aim a
// verdict at an operation other than the one in the path.
type dynamicEffectResolutionRequest struct {
	Confirm bool `json:"confirm"`
	// Succeeded must be stated explicitly. There is no default, because the
	// state this replaces is precisely "nobody knows", and a zero value that
	// silently means "it failed" would turn an omitted field into a verdict.
	Succeeded  *bool  `json:"succeeded"`
	Evidence   string `json:"evidence"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// handleResolveUnknownDynamicEffect is the out-of-band exit for an operation
// the system dispatched but could never observe. Everywhere else the answer to
// "did this happen" comes from the channel; here it comes from a person, and
// the point of the endpoint is to make that substitution visible rather than
// convenient. It is owner-only, demands confirm=true, requires the evidence
// the operator actually checked, and records both in the admin audit trail.
func (s *HTTPServer) handleResolveUnknownDynamicEffect(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in dynamicEffectResolutionRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	if in.Succeeded == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "succeeded must be stated explicitly"})
		return
	}
	if strings.TrimSpace(in.Evidence) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "evidence is required"})
		return
	}
	operationID := strings.TrimSpace(r.PathValue("operationId"))
	// The operator is the authenticated admin, never a name supplied in the
	// body. A verdict this path can produce is only as accountable as the
	// identity attached to it.
	resolvedBy := dynamicEffectResolutionOperator(r)
	if resolvedBy == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "resolution operator could not be identified"})
		return
	}
	if err := s.svc.ResolveUnknownDynamicEffect(agentservice.DynamicSemanticManualResolution{
		OperationID: operationID, Succeeded: *in.Succeeded,
		Evidence: in.Evidence, ResolvedBy: resolvedBy, ReasonCode: in.ReasonCode,
	}); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	outcome := "failed"
	if *in.Succeeded {
		outcome = "succeeded"
	}
	_ = s.recordAdminAudit(r.Context(), "admin.dynamic_effect.unknown_resolved", "dynamic_effect_operation", operationID, map[string]string{
		"outcome": outcome, "resolved_by": resolvedBy,
		"reason_code": strings.TrimSpace(in.ReasonCode), "remote_ip": requestClientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved", "outcome": outcome})
}

// dynamicEffectResolutionOperator names who is answerable for a verdict, from
// the authenticated admin identity only.
//
// requireAdminOwner also admits a bare shared secret, which authorizes the
// call but identifies nobody. That is acceptable for a publication, whose
// content is checked against the runtime anyway; it is not acceptable here,
// where the content is a human claim and the identity is the only thing
// backing it. An unidentified caller therefore gets no verdict.
func dynamicEffectResolutionOperator(r *http.Request) string {
	identity, ok := adminAuditIdentityFromContext(r.Context())
	if !ok {
		return ""
	}
	if name := strings.TrimSpace(identity.Username); name != "" {
		return name
	}
	return strings.TrimSpace(identity.UserID)
}
