package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/httpthreat"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func newHTTPThreatEngine(dataDir string) *httpthreat.Engine {
	dir := ""
	if strings.TrimSpace(dataDir) != "" {
		dir = filepath.Join(dataDir, "httpthreat")
	}
	eng := httpthreat.NewEngineAt(dir, httpthreat.DefaultEncoderID, httpthreat.HashEmbed)
	if url := strings.TrimSpace(os.Getenv("HTTPTHREAT_LLM_URL")); url != "" {
		eng.SetArbitrator(httpthreat.NewHTTPArbitrator(url, os.Getenv("HTTPTHREAT_LLM_TOKEN")))
	}
	return eng
}

func httpThreatIdentity(r *http.Request) (httpthreat.NodeIdentity, string, error) {
	tenant := strings.TrimSpace(AdminTenantID(r.Context()))
	if tenant == "" {
		return httpthreat.NodeIdentity{}, "", httpthreat.ErrUnauthorized
	}
	admin := AdminFromContext(r.Context())
	node := "admin"
	role := httpthreat.RoleAdmin
	if admin != nil {
		if strings.TrimSpace(admin.Username) != "" {
			node = strings.TrimSpace(admin.Username)
		} else if strings.TrimSpace(admin.ID) != "" {
			node = strings.TrimSpace(admin.ID)
		}
		if strings.EqualFold(strings.TrimSpace(admin.Role), "analyst") {
			role = httpthreat.RoleAnalyst
		}
	}
	return httpthreat.NodeIdentity{TenantID: tenant, NodeID: node}, role, nil
}

func mountHTTPThreatAdmin(mux *http.ServeMux, requireTenantAdmin func(http.HandlerFunc) http.HandlerFunc, dataDir string, identity *auth.IdentityService, audit store.AdminAuditRepository) {
	h := &httpthreat.Handler{Engine: newHTTPThreatEngine(dataDir), Who: httpThreatIdentity}
	h.Audit = func(ctx context.Context, tenant, actor, action, from, to, hash, reason string) {
		writeAdminAuditLog(ctx, audit, actor, "httpthreat."+action, map[string]any{
			"tenant_id": tenant, "from": from, "to": to, "serving_hash": hash, "reason": reason,
		})
	}
	inner := http.NewServeMux()
	h.Mount(inner, "/api/admin/httpthreat")
	wrap := func(w http.ResponseWriter, r *http.Request) { inner.ServeHTTP(w, r) }
	mux.HandleFunc("GET /api/admin/httpthreat/status", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/queue", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/gate", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/observe", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/serving", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/targets", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/runs", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/sample", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/cap", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/export", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/export", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/map", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/map", requireTenantAdmin(wrap))
	mux.HandleFunc("GET /api/admin/httpthreat/audit", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/intel", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/sites", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/import", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/label", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/label/batch", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/train", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/adopt", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/pipeline", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/rollback", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/ack", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/detect", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/arbitrate", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/llm/promote", requireTenantAdmin(wrap))
	mux.HandleFunc("POST /api/admin/httpthreat/llm/reject", requireTenantAdmin(wrap))
	if identity != nil {
		nodeID := func(w http.ResponseWriter, r *http.Request) (httpthreat.NodeIdentity, bool) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return httpthreat.NodeIdentity{}, false
			}
			return httpthreat.NodeIdentity{TenantID: principal.TenantID, NodeID: principal.MachineID}, true
		}
		mux.HandleFunc("GET /api/httpthreat/serving", func(w http.ResponseWriter, r *http.Request) {
			id, ok := nodeID(w, r)
			if !ok {
				return
			}
			b, err := h.Engine.Bundle(id)
			if err != nil {
				writeError(w, http.StatusForbidden, "SERVING_DENIED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, b)
		})
		mux.HandleFunc("POST /api/httpthreat/ack", func(w http.ResponseWriter, r *http.Request) {
			id, ok := nodeID(w, r)
			if !ok {
				return
			}
			var req struct {
				Hash string `json:"hash"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if err := h.Engine.ACK(id, req.Hash); err != nil {
				writeError(w, http.StatusForbidden, "ACK_DENIED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
		})
		mux.HandleFunc("POST /api/httpthreat/inspect", func(w http.ResponseWriter, r *http.Request) {
			id, ok := nodeID(w, r)
			if !ok {
				return
			}
			dec, err := h.Engine.Inspect(id, r)
			if err != nil {
				writeError(w, http.StatusForbidden, "INSPECT_DENIED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, dec)
		})
		mux.HandleFunc("POST /api/httpthreat/ingest", func(w http.ResponseWriter, r *http.Request) {
			id, ok := nodeID(w, r)
			if !ok {
				return
			}
			var req httpthreat.IngestRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID", "invalid transaction")
				return
			}
			if err := h.Engine.IngestSample(id, req); err != nil {
				writeError(w, http.StatusForbidden, "INGEST_DENIED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
		})
	}
}
