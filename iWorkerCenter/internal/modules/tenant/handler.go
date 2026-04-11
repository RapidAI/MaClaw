package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// Handler provides HTTP endpoints for tenant management.
type Handler struct {
	svc *TenantService
}

func NewHandler(svc *TenantService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers public (unauthenticated) tenant routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/tenant-status", h.handleTenantStatus)
	mux.HandleFunc("/auth/tenants", h.handleListTenants)
	mux.HandleFunc("/auth/setup-tenant", h.handleSetupTenant)
	mux.HandleFunc("/api/tenants/provision", h.handleProvision)
}

// handleTenantStatus returns {count, needs_setup}.
func (h *Handler) handleTenantStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	count, err := h.svc.TenantCount(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":      count,
		"needs_setup": count == 0,
	})
}

// handleListTenants returns active tenants for login page selection.
func (h *Handler) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tenants, err := h.svc.ListActiveTenants(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	type item struct {
		ID          string `json:"id"`
		CompanyName string `json:"company_name"`
	}
	items := make([]item, len(tenants))
	for i, t := range tenants {
		items[i] = item{ID: t.ID, CompanyName: t.CompanyName}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": items})
}

// handleSetupTenant handles first-time tenant creation.
func (h *Handler) handleSetupTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateTenantRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}

	t, err := h.svc.SetupFirstTenant(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrSetupAlreadyDone) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "初始设置已完成"})
			return
		}
		if errors.Is(err, ErrCompanyExists) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "企业名称已存在"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	// Async register to cloud (use background context since request context will be cancelled)
	tenantID := t.ID
	go h.svc.RegisterToCloud(context.Background(), tenantID)

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": t.ID,
		"status":    t.Status,
		"message":   "企业创建成功",
	})
}

// handleProvision handles signed provision requests from iWorkerCloud.
func (h *Handler) handleProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body failed"})
		return
	}

	var req ProvisionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}

	// Build body-without-signature for hash verification
	bodyWithoutSig := stripSignatureField(bodyBytes)

	t, err := h.svc.ProvisionFromCloud(r.Context(), req, bodyWithoutSig)
	if err != nil {
		switch {
		case errors.Is(err, ErrSignatureInvalid):
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "签名验证失败"})
		case errors.Is(err, ErrTimestampExpired):
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "请求已过期"})
		case errors.Is(err, ErrNonceReplay):
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "重复请求"})
		case errors.Is(err, ErrCloudNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "未配置iWorkerCloud连接"})
		case errors.Is(err, ErrCompanyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": "企业名称已存在"})
		default:
			log.Printf("[tenant] provision error: %v", err)
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": t.ID,
		"status":    t.Status,
		"message":   "开户成功",
	})
}

// stripSignatureField removes the "signature" field from JSON and returns
// the canonical body bytes for hash computation.
// Uses json.Decoder with UseNumber to preserve numeric precision.
func stripSignatureField(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return raw
	}
	delete(m, "signature")
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
