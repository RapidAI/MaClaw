package goalwatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

const maxGoalWatchJSONBodyBytes = 64 << 10

var (
	errGoalWatchJSONTooLarge = errors.New("goalwatch json body exceeds size limit")
	errGoalWatchJSONTrailing = errors.New("goalwatch json body contains trailing data")
)

type RecoveryExecutor interface {
	StartOrResumeStep(tenantID string, stepInstanceID, actorID, note string) error
}

type Handler struct {
	svc      *Service
	monitor  *Monitor
	recovery RecoveryExecutor
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) SetMonitor(monitor *Monitor) {
	if h != nil {
		h.monitor = monitor
	}
}

func (h *Handler) SetRecoveryExecutor(recovery RecoveryExecutor) {
	if h != nil {
		h.recovery = recovery
	}
}

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/goalwatch/check", h.handleCheck)
}

func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/goalwatch/pushes", h.handleClientPushes)
	mux.HandleFunc("/client/goalwatch/pushes/", h.handleClientPushAction)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/goalwatch/check", h.handleCheck)
	mux.HandleFunc("/admin/goalwatch/status", h.handleStatus)
	mux.HandleFunc("/admin/goalwatch/health", h.handleHealth)
	mux.HandleFunc("/admin/goalwatch/policy", h.handlePolicy)
}

func parsePushAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/client/goalwatch/pushes/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}
	eventID, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", ""
	}
	action, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", ""
	}
	return strings.TrimSpace(eventID), strings.TrimSpace(action)
}

func (h *Handler) handleClientPushes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := requestTenantID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	pushes, err := h.svc.ListPushesForColleague(tid, r.URL.Query().Get("colleague_id"), limit)
	if err != nil {
		response.BadRequest(w, "LIST_PUSHES_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]any{"pushes": pushes})
}

func (h *Handler) handleClientPushAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	eventID, action := parsePushAction(r.URL.EscapedPath())
	if eventID == "" || (action != "ack" && action != "recover") {
		response.NotFound(w, "ACTION_NOT_FOUND", "expected /client/goalwatch/pushes/{event_id}/ack or /recover")
		return
	}
	var req struct {
		ColleagueID string `json:"colleague_id"`
		Status      string `json:"status"`
		Note        string `json:"note"`
	}
	if err := decodeGoalWatchJSON(r.Body, &req, false); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	tid := requestTenantID(r)
	if action == "recover" {
		result, err := h.recoverPush(tid, req.ColleagueID, eventID, req.Note, time.Now().UTC())
		if err != nil {
			response.BadRequest(w, "RECOVER_PUSH_FAILED", err.Error())
			return
		}
		response.OK(w, result)
		return
	}
	result, err := h.svc.AckPush(tid, req.ColleagueID, eventID, req.Status, req.Note, time.Now().UTC())
	if err != nil {
		response.BadRequest(w, "ACK_PUSH_FAILED", err.Error())
		return
	}
	response.OK(w, result)
}

type RecoverResult struct {
	Push           Push      `json:"push"`
	Ack            AckResult `json:"ack"`
	RecoveryAction string    `json:"recovery_action"`
	RecoveryMethod string    `json:"recovery_method"`
	RecoveryPath   string    `json:"recovery_path"`
	Status         string    `json:"status"`
}

func (h *Handler) recoverPush(tenantID, colleagueID, eventID, note string, now time.Time) (RecoverResult, error) {
	if h == nil || h.svc == nil {
		return RecoverResult{}, fmt.Errorf("goalwatch service is unavailable")
	}
	if h.recovery == nil {
		return RecoverResult{}, fmt.Errorf("goalwatch recovery executor is unavailable")
	}
	pushes, err := h.svc.ListPushesForColleague(tenantID, colleagueID, 100)
	if err != nil {
		return RecoverResult{}, err
	}
	var target *Push
	for i := range pushes {
		if pushes[i].EventID == eventID {
			target = &pushes[i]
			break
		}
	}
	if target == nil {
		return RecoverResult{}, fmt.Errorf("push not found for colleague: %s", eventID)
	}
	if strings.TrimSpace(target.WorkflowStepInstanceID) == "" {
		return RecoverResult{}, fmt.Errorf("push has no workflow_step_instance_id")
	}
	switch strings.TrimSpace(target.RecoveryAction) {
	case "start_workflow_step", "resume_workflow_step":
		if err := h.recovery.StartOrResumeStep(tenantID, target.WorkflowStepInstanceID, colleagueID, firstNonEmpty(note, target.RecoveryAction)); err != nil {
			return RecoverResult{}, err
		}
	default:
		return RecoverResult{}, fmt.Errorf("unsupported recovery action: %s", target.RecoveryAction)
	}
	ackNote := recoverAckNote(*target, note)
	ack, err := h.svc.AckPush(tenantID, colleagueID, eventID, "recovered", ackNote, now)
	if err != nil {
		return RecoverResult{}, err
	}
	return RecoverResult{Push: *target, Ack: ack, RecoveryAction: target.RecoveryAction, RecoveryMethod: target.RecoveryMethod, RecoveryPath: target.RecoveryPath, Status: "recovered"}, nil
}

func recoverAckNote(push Push, note string) string {
	parts := []string{}
	if strings.TrimSpace(push.RecoveryAction) != "" {
		parts = append(parts, "recovery_action="+strings.TrimSpace(push.RecoveryAction))
	}
	if strings.TrimSpace(push.RecoveryPath) != "" {
		parts = append(parts, "recovery_path="+strings.TrimSpace(push.RecoveryPath))
	}
	if strings.TrimSpace(note) != "" {
		parts = append(parts, "note="+strings.TrimSpace(note))
	}
	if len(parts) == 0 {
		return strings.TrimSpace(push.RecoveryAction)
	}
	return strings.Join(parts, " ")
}
func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		return
	}
	tid := requestTenantID(r)
	svc := h.svc
	if minutes, _ := strconv.Atoi(r.URL.Query().Get("stalled_after_minutes")); minutes > 0 {
		cfg := h.svc.Config()
		cfg.StalledAfter = time.Duration(minutes) * time.Minute
		svc = NewService(h.svc.collabRepo, cfg)
		svc.SetAgentRuntime(h.svc.agentRuntime)
	}
	result, err := svc.CheckTenant(tid, time.Now().UTC())
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, result)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	if h.monitor == nil {
		response.OK(w, MonitorStatus{Config: configStatus(h.svc)})
		return
	}
	response.OK(w, h.monitor.Status())
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	if h.monitor == nil {
		response.OK(w, MonitorHealth{Level: "warning", Reasons: []string{"goalwatch_monitor_not_configured"}, RecommendedActions: []string{"check_iworkercenter_bootstrap_goalwatch_monitor"}, Config: configStatus(h.svc), Status: MonitorStatus{Config: configStatus(h.svc)}})
		return
	}
	response.OK(w, h.monitor.Health(time.Now().UTC()))
}

func (h *Handler) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.svc == nil {
		response.Internal(w, "goalwatch service is unavailable")
		return
	}
	tid := requestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		policy, persisted, err := h.svc.GetTenantPolicy(r.Context(), tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"policy": policy, "persisted": persisted})
	case http.MethodPut:
		var req TenantPolicy
		if err := decodeGoalWatchJSON(r.Body, &req, false); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		policy, err := h.svc.SaveTenantPolicy(r.Context(), tid, req)
		if err != nil {
			response.BadRequest(w, "SAVE_POLICY_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]any{"policy": policy, "persisted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}
func configStatus(svc *Service) MonitorConfigStatus {
	if svc == nil {
		return MonitorConfigStatus{}
	}
	cfg := svc.Config()
	return MonitorConfigStatus{TickIntervalSeconds: int64(cfg.TickInterval.Seconds()), StalledAfterSeconds: int64(cfg.StalledAfter.Seconds()), PushCooldownSeconds: int64(cfg.PushCooldown.Seconds()), LeaseTTLSeconds: int64(cfg.LeaseTTL.Seconds()), WorkersPerShard: cfg.WorkersPerShard, MaxWatchers: cfg.MaxWatchers}
}

func requestTenantID(r *http.Request) string {
	return tenant.RequestTenantID(r)
}

func decodeGoalWatchJSON(body io.Reader, dst any, allowEmpty bool) error {
	data, err := io.ReadAll(io.LimitReader(body, maxGoalWatchJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxGoalWatchJSONBodyBytes {
		return errGoalWatchJSONTooLarge
	}
	if len(bytes.TrimSpace(data)) == 0 && allowEmpty {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errGoalWatchJSONTrailing
		}
		return err
	}
	return nil
}
