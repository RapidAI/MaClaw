package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/industryexpert"
)

// ManagedIndustryExpertHandler exposes a tenant's HubCenter-managed expert
// catalogue. It deliberately has no mutation routes: personal experts and
// managed industry experts must never share an LWW/writeback plane.
type ManagedIndustryExpertHandler struct{ Store *industryexpert.Store }

func NewManagedIndustryExpertHandler(store *industryexpert.Store) *ManagedIndustryExpertHandler {
	return &ManagedIndustryExpertHandler{Store: store}
}

func (h *ManagedIndustryExpertHandler) Register(mux *http.ServeMux, identity veMachineAuthenticator) {
	mux.HandleFunc("GET /api/v1/managed-industry-experts", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		h.list(w, r, principal)
	})
}

func (h *ManagedIndustryExpertHandler) list(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	catalog, err := h.Store.List(r.Context(), p.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MANAGED_INDUSTRY_EXPERT_LIST_FAILED", "internal error")
		return
	}
	// Definitions stay in Hub's verified cache for sync integrity, but must not
	// be sent to a machine. In particular, a paid industry expert is only
	// available after the GUI's authenticated Expert Market purchase/install
	// workflow has established that user's entitlement.
	for i := range catalog.Experts {
		catalog.Experts[i].Definition = nil
	}
	writeJSON(w, http.StatusOK, catalog)
}
