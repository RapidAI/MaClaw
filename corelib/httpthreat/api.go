package httpthreat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// IdentityFunc resolves the authenticated principal. Tenant comes from identity.
type IdentityFunc func(*http.Request) (NodeIdentity, string, error)

// AuditFunc records adopt / pipeline / rollback. Who, when, from, to, hash, reason.
type AuditFunc func(ctx context.Context, tenant, actor, action, from, to, hash, reason string)

// Handler exposes trainer/admin HTTP for this package only (not LLM class-head).
type Handler struct {
	Engine *Engine
	Who    IdentityFunc
	Audit  AuditFunc
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func (h *Handler) identity(r *http.Request) (NodeIdentity, string, error) {
	if h == nil || h.Engine == nil || h.Who == nil {
		return NodeIdentity{}, "", ErrUnauthorized
	}
	return h.Who(r)
}

func (h *Handler) Mount(mux *http.ServeMux, prefix string) {
	prefix = strings.TrimRight(prefix, "/")
	mux.HandleFunc("GET "+prefix+"/status", h.getStatus)
	mux.HandleFunc("GET "+prefix+"/queue", h.getQueue)
	mux.HandleFunc("GET "+prefix+"/gate", h.getGate)
	mux.HandleFunc("GET "+prefix+"/observe", h.getObserve)
	mux.HandleFunc("GET "+prefix+"/serving", h.getServing)
	mux.HandleFunc("POST "+prefix+"/targets", h.postTargets)
	mux.HandleFunc("GET "+prefix+"/runs", h.getRuns)
	mux.HandleFunc("GET "+prefix+"/sample", h.getSample)
	mux.HandleFunc("POST "+prefix+"/cap", h.postCap)
	mux.HandleFunc("GET "+prefix+"/export", h.getExport)
	mux.HandleFunc("POST "+prefix+"/export", h.postExport)
	mux.HandleFunc("GET "+prefix+"/map", h.getMap)
	mux.HandleFunc("POST "+prefix+"/map", h.postMap)
	mux.HandleFunc("GET "+prefix+"/audit", h.getAudit)
	mux.HandleFunc("POST "+prefix+"/intel", h.postIntel)
	mux.HandleFunc("POST "+prefix+"/sites", h.postSites)
	mux.HandleFunc("POST "+prefix+"/import", h.postImport)
	mux.HandleFunc("POST "+prefix+"/label", h.postLabel)
	mux.HandleFunc("POST "+prefix+"/label/batch", h.postLabelBatch)
	mux.HandleFunc("POST "+prefix+"/train", h.postTrain)
	mux.HandleFunc("POST "+prefix+"/adopt", h.postAdopt)
	mux.HandleFunc("POST "+prefix+"/pipeline", h.postPipeline)
	mux.HandleFunc("POST "+prefix+"/rollback", h.postRollback)
	mux.HandleFunc("POST "+prefix+"/ack", h.postACK)
	mux.HandleFunc("POST "+prefix+"/detect", h.postDetect)
	mux.HandleFunc("POST "+prefix+"/arbitrate", h.postArbitrate)
	mux.HandleFunc("POST "+prefix+"/llm/promote", h.postPromoteLLM)
	mux.HandleFunc("POST "+prefix+"/llm/reject", h.postRejectLLM)
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := h.Engine.Status(id)
	st.Role = role
	writeJSON(w, http.StatusOK, st)
}

func (h *Handler) getQueue(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := h.Engine.QueueFilter(id, r.URL.Query().Get("rule_id"), r.URL.Query().Get("site_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(items) > BatchLabelLimit {
		items = items[:BatchLabelLimit]
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) getGate(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rep, err := h.Engine.Gate(id, r.URL.Query().Get("which"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *Handler) audit(r *http.Request, id NodeIdentity, action, from, to, hash, reason string) {
	if h == nil || h.Audit == nil || r == nil {
		return
	}
	h.Audit(r.Context(), id.TenantID, id.NodeID, action, from, to, hash, reason)
}

func (h *Handler) getExport(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := h.Engine.Export(id, role)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "export disabled", "code": "EXPORT_DISABLED"})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) postExport(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.SetExport(id, role, req.Enabled, req.Reason); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "export", "", fmtBool(req.Enabled), "", req.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postMap(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		RuleID string `json:"rule_id"`
		Class  string `json:"class"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.RemapRule(id, role, req.RuleID, req.Class); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "remap", req.RuleID, req.Class, "", "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postImport(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var head Head
	if err := json.NewDecoder(r.Body).Decode(&head); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.ImportCandidate(id, role, head); err != nil {
		writeErr(w, err)
		return
	}
	after := h.Engine.Status(id)
	h.audit(r, id, "import", "", after.CandidateHash, after.CandidateHash, "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func fmtBool(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func (h *Handler) getSample(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := h.Engine.Lookup(id, r.URL.Query().Get("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) postCap(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Cap int `json:"cap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.SetCorpusCap(id, role, req.Cap); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "cap", "", "", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) getRuns(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	runs, err := h.Engine.Runs(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *Handler) getObserve(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rep, err := h.Engine.ObserveKind(id, r.URL.Query().Get("kind"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *Handler) getServing(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := h.Engine.Bundle(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (h *Handler) postTargets(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Nodes  []string `json:"nodes"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.UpdateTargets(id, role, req.Nodes, req.Add, req.Remove); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "targets", "", strings.Join(req.Add, ","), "", strings.Join(req.Remove, ","))
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postLabel(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req LabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	req.Role = role
	if err := h.Engine.Label(id, req); err != nil {
		writeErr(w, err)
		return
	}
	action := "label"
	if req.Abstain {
		action = "abstain"
	} else if strings.TrimSpace(req.GoldClass) == "" {
		action = "unlabel"
	}
	h.audit(r, id, action, req.SampleID, req.GoldClass, "", req.GoldSource)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postLabelBatch(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req BatchLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	req.Role = role
	if err := h.Engine.LabelBatch(id, req); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "label_batch", "", req.GoldClass, "", fmt.Sprintf("%d", len(req.SampleIDs)))
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) getMap(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := h.Engine.RuleHits(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) postIntel(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Host  string `json:"host"`
		Class string `json:"class"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.SetIntel(id, role, req.Host, req.Class); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "intel", req.Host, req.Class, "", "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postSites(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.UpdateSites(id, role, req.Add, req.Remove); err != nil {
		writeErr(w, err)
		return
	}
	h.audit(r, id, "sites", strings.Join(req.Remove, ","), strings.Join(req.Add, ","), "", "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) getAudit(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := h.Engine.RecentDecisions(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if rows == nil {
		rows = []AuditRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) postTrain(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.Engine.StartTrain(id, role); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": "1", "training": true})
}

func (h *Handler) postAdopt(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Override string `json:"override"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	before := h.Engine.Status(id)
	if err := h.Engine.AdoptOverride(id, role, req.Override, req.Reason); err != nil {
		writeErr(w, err)
		return
	}
	after := h.Engine.Status(id)
	action := "adopt"
	if req.Override == PromoteOverride {
		action = "force_adopt"
	}
	h.audit(r, id, action, before.ServingHash, after.ServingHash, after.ServingHash, req.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postPipeline(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Mode     string `json:"mode"`
		Override string `json:"override"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	before := h.Engine.Status(id)
	if err := h.Engine.SetPipeline(id, req.Mode, req.Override, req.Reason, role); err != nil {
		writeErr(w, err)
		return
	}
	action := "pipeline"
	if req.Override == PromoteOverride {
		action = "force_promote"
	}
	h.audit(r, id, action, before.Pipeline, req.Mode, before.ServingHash, req.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postRollback(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	before := h.Engine.Status(id)
	if err := h.Engine.Rollback(id, role); err != nil {
		writeErr(w, err)
		return
	}
	after := h.Engine.Status(id)
	h.audit(r, id, "rollback", before.ServingHash, after.ServingHash, after.ServingHash, "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postACK(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		Hash string `json:"hash"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.Engine.ACK(id, req.Hash); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postDetect(w http.ResponseWriter, r *http.Request) {
	id, _, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var tx Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	dec, err := h.Engine.Detect(id, tx)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dec)
}

func (h *Handler) postArbitrate(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		SampleID string `json:"sample_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SampleID) == "" {
		writeErr(w, ErrInvalid)
		return
	}
	adv, err := h.Engine.Arbitrate(id, req.SampleID, role)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adv)
}

func (h *Handler) postPromoteLLM(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		SampleID string `json:"sample_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.PromoteLLM(id, req.SampleID, role); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (h *Handler) postRejectLLM(w http.ResponseWriter, r *http.Request) {
	id, role, err := h.identity(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req struct {
		SampleID string `json:"sample_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, ErrInvalid)
		return
	}
	if err := h.Engine.RejectLLM(id, req.SampleID, role); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}
