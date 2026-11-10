package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type remoteControlMachines struct {
	devices *device.Service
}

func (m remoteControlMachines) BelongsTo(ctx context.Context, userID, machineID string) (remotecontrol.Machine, bool) {
	if m.devices == nil {
		return remotecontrol.Machine{}, false
	}
	items, err := m.devices.ListMachines(ctx, userID)
	if err != nil {
		return remotecontrol.Machine{}, false
	}
	machineID = strings.TrimSpace(machineID)
	for _, item := range items {
		if item.MachineID != machineID {
			continue
		}
		return remotecontrol.Machine{ID: item.MachineID, Name: remoteControlMachineName(item), Online: m.Online(item.MachineID)}, true
	}
	return remotecontrol.Machine{}, false
}

func (m remoteControlMachines) Online(machineID string) bool {
	return m.devices != nil && m.devices.IsMachineOnline(machineID)
}

func (m remoteControlMachines) Send(machineID string, msg any) error {
	if m.devices == nil {
		return remotecontrol.ErrOffline
	}
	if err := m.devices.SendToOnlineMachine(machineID, msg); err != nil {
		return remotecontrol.ErrOffline
	}
	return nil
}

type remoteControlAccounts struct {
	identity *auth.IdentityService
}

func (a remoteControlAccounts) Lookup(ctx context.Context, tenantID, contact string) (string, bool) {
	id := companionContactUserID(ctx, a.identity, tenantID, contact)
	return id, id != ""
}

type remoteControlNotifier struct {
	svc *notification.Service
}

func (n remoteControlNotifier) Notify(ctx context.Context, notice remotecontrol.Notice) (string, error) {
	if n.svc == nil || strings.TrimSpace(notice.AudienceUserID) == "" {
		return "", nil
	}
	priority := notification.PriorityNormal
	if notice.Important {
		priority = notification.PriorityImportant
	}
	source := notification.SourceRemoteControlResult
	switch notice.Source {
	case remotecontrol.NoticeInvite:
		source = notification.SourceRemoteControlInvite
	case remotecontrol.NoticeWatch:
		source = notification.SourceRemoteControlNotice
	}
	created, err := n.svc.CreateNotification(ctx, notification.CreateRequest{
		Title:        notice.Title,
		Content:      notice.Content,
		Category:     notification.CategoryCustom,
		Priority:     priority,
		AudienceType: notification.AudienceUser,
		AudienceIDs:  []string{notice.AudienceUserID},
		CreatedBy:    "remote-control",
		Source:       source,
		SourceID:     notice.SourceID,
	})
	if err != nil || created == nil {
		return "", err
	}
	_ = n.svc.PublishNotification(ctx, created.ID)
	return created.ID, nil
}

func (n remoteControlNotifier) Revoke(ctx context.Context, notificationID string) {
	if n.svc == nil || strings.TrimSpace(notificationID) == "" {
		return
	}
	_ = n.svc.RevokeNotification(ctx, notificationID)
	_ = n.svc.DeleteNotification(ctx, notificationID)
}

type remoteControlHandler struct {
	svc      *remotecontrol.Service
	devices  *device.Service
	identity *auth.IdentityService
}

func registerRemoteControl(mux *http.ServeMux, db *sql.DB, identity *auth.IdentityService, devices *device.Service, notifSvc *notification.Service) {
	if db == nil || identity == nil {
		return
	}
	adapter := remoteControlMachines{devices: devices}
	svc := remotecontrol.New(db, remoteControlAccounts{identity: identity}, adapter, adapter, remoteControlNotifier{svc: notifSvc})
	if svc == nil {
		return
	}
	ws.OnRemoteControlResult = svc.Complete
	h := remoteControlHandler{svc: svc, devices: devices, identity: identity}
	mux.HandleFunc("GET /api/remote-control/state", h.state)
	mux.HandleFunc("GET /api/remote-control/machines", h.machines)
	mux.HandleFunc("POST /api/remote-control/same-account", h.sameAccount)
	mux.HandleFunc("POST /api/remote-control/invites", h.invite)
	mux.HandleFunc("POST /api/remote-control/grants/{id}/accept", h.accept)
	mux.HandleFunc("POST /api/remote-control/grants/{id}/reject", h.reject)
	mux.HandleFunc("POST /api/remote-control/grants/{id}/ignore", h.ignore)
	mux.HandleFunc("POST /api/remote-control/grants/{id}/revoke", h.revoke)
	mux.HandleFunc("POST /api/remote-control/grants/{id}/op", h.op)
	mux.HandleFunc("POST /api/remote-control/federation/invites", h.federationInvite)
	mux.HandleFunc("POST /api/remote-control/federation/proof", h.federationProof)
	mux.HandleFunc("POST /api/remote-control/federation/grants/{id}/decision", h.federationDecision)
	mux.HandleFunc("POST /api/remote-control/federation/grants/{id}/op", h.federationOp)
	mux.HandleFunc("POST /api/remote-control/federation/grants/{id}/status", h.federationStatus)
}

func (h remoteControlHandler) state(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	state, err := h.svc.Snapshot(r.Context(), principal.TenantID, principal.UserID, machineID)
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	state.Machines = h.otherMachines(r.Context(), principal.UserID, machineID)
	h.refreshFederatedGrants(r.Context(), principal.TenantID, principal.UserID, machineID, &state)
	writeJSON(w, http.StatusOK, state)
}

func remoteControlMachineName(item device.MachineRuntimeInfo) string {
	if name := strings.TrimSpace(item.Name); name != "" {
		return name
	}
	if hostname := strings.TrimSpace(item.Hostname); hostname != "" {
		return hostname
	}
	return strings.TrimSpace(item.Alias)
}

func (h remoteControlHandler) otherMachines(ctx context.Context, userID, machineID string) []remotecontrol.MachineView {
	if h.devices == nil {
		return []remotecontrol.MachineView{}
	}
	items, err := h.devices.ListMachines(ctx, userID)
	if err != nil {
		return []remotecontrol.MachineView{}
	}
	out := make([]remotecontrol.MachineView, 0, len(items))
	for _, item := range items {
		if item.MachineID == "" || item.MachineID == machineID {
			continue
		}
		out = append(out, remotecontrol.MachineView{ID: item.MachineID, Name: remoteControlMachineName(item), Online: h.devices.IsMachineOnline(item.MachineID)})
	}
	return out
}

func (h remoteControlHandler) machines(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	if h.devices == nil {
		writeJSON(w, http.StatusOK, map[string]any{"machines": []remotecontrol.Machine{}})
		return
	}
	items, err := h.devices.ListMachines(r.Context(), principal.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	out := make([]remotecontrol.Machine, 0, len(items))
	for _, item := range items {
		if item.MachineID == "" || item.MachineID == machineID {
			continue
		}
		out = append(out, remotecontrol.Machine{ID: item.MachineID, Name: remoteControlMachineName(item), Online: h.devices.IsMachineOnline(item.MachineID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": out})
}

func (h remoteControlHandler) sameAccount(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body struct {
		TargetMachineID string `json:"target_machine_id"`
		Permission      string `json:"permission"`
	}
	if !decodeRemoteControlBody(w, r, &body) {
		return
	}
	grant, err := h.svc.GrantSameAccount(r.Context(), principal.TenantID, principal.UserID, machineID, body.TargetMachineID, body.Permission)
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": h.svcView(grant)})
}

func (h remoteControlHandler) invite(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body struct {
		Account    string `json:"account"`
		Permission string `json:"permission"`
	}
	if !decodeRemoteControlBody(w, r, &body) {
		return
	}
	grant, err := h.svc.Invite(r.Context(), principal.TenantID, principal.UserID, machineID, body.Account, body.Permission)
	if err != nil {
		grant, err = h.inviteAcrossHub(r.Context(), principal.TenantID, principal.UserID, machineID, body.Account, body.Permission, err)
	}
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": remotecontrol.GrantView{
		ID: grant.ID, Account: grant.TargetAccount, Permission: grant.Permission, Status: grant.Status,
	}})
}

func (h remoteControlHandler) accept(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	grantID := r.PathValue("id")
	federated, federatedErr := h.svc.FederatedGrantForTarget(r.Context(), principal.UserID, grantID)
	if federatedErr == nil && (federated.Status == remotecontrol.StatusPending || federated.Status == remotecontrol.StatusActive) {
		if federated.Status == remotecontrol.StatusPending {
			machine, owned := (remoteControlMachines{devices: h.devices}).BelongsTo(r.Context(), principal.UserID, machineID)
			if !owned {
				writeRemoteControlError(w, &remotecontrol.StatusError{Code: "INVALID", Message: "请先登录这台电脑"})
				return
			}
			federated.TargetMachineID = machine.ID
			federated.TargetMachineName = machine.Name
		}
		if err := h.relayTargetDecision(r.Context(), federated, "accept"); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		if federated.Status == remotecontrol.StatusActive {
			writeJSON(w, http.StatusOK, map[string]any{"grant": h.svcView(federated)})
			return
		}
	}
	grant, err := h.svc.Accept(r.Context(), principal.TenantID, principal.UserID, machineID, grantID)
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant": h.svcView(grant)})
}

func (h remoteControlHandler) reject(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.viewer(w, r)
	if !ok {
		return
	}
	grantID := r.PathValue("id")
	federated, federatedErr := h.svc.FederatedGrantForTarget(r.Context(), principal.UserID, grantID)
	if federatedErr == nil {
		if federated.Status != remotecontrol.StatusPending && federated.Status != remotecontrol.StatusRejected {
			writeRemoteControlError(w, remotecontrol.ErrNotFound)
			return
		}
		if err := h.relayTargetDecision(r.Context(), federated, "decline"); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		if federated.Status == remotecontrol.StatusRejected {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
	}
	if err := h.svc.Reject(r.Context(), principal.TenantID, principal.UserID, grantID); err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h remoteControlHandler) ignore(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(ctx context.Context, tenantID, userID, grantID string) error {
		return h.svc.Ignore(ctx, tenantID, userID, grantID)
	})
}

func (h remoteControlHandler) decide(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string, string) error) {
	principal, _, ok := h.viewer(w, r)
	if !ok {
		return
	}
	if err := fn(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h remoteControlHandler) revoke(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	grantID := r.PathValue("id")
	target, targetErr := h.svc.FederatedGrantForTarget(r.Context(), principal.UserID, grantID)
	controller, controllerErr := h.svc.ControllerGrant(r.Context(), principal.TenantID, principal.UserID, machineID, grantID)
	if targetErr == nil {
		if err := h.relayTargetDecision(r.Context(), target, "revoke"); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		if target.Status == remotecontrol.StatusRevoked {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
	} else if controllerErr == nil && controller.TargetHub != "" {
		if err := h.relayControllerRevoke(r.Context(), controller); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		if controller.Status == remotecontrol.StatusRevoked {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
	}
	if err := h.svc.Revoke(r.Context(), principal.TenantID, principal.UserID, machineID, grantID); err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h remoteControlHandler) op(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body struct {
		Op   string          `json:"op"`
		Body json.RawMessage `json:"body"`
	}
	if !decodeRemoteControlBody(w, r, &body) {
		return
	}
	grantID := r.PathValue("id")
	grant, grantErr := h.svc.ControllerGrant(r.Context(), principal.TenantID, principal.UserID, machineID, grantID)
	var result json.RawMessage
	var err error
	if grantErr == nil && grant.TargetHub != "" {
		result, err = h.execAcrossHub(r.Context(), grant, body.Op, body.Body)
	} else {
		result, err = h.svc.Exec(r.Context(), principal.TenantID, principal.UserID, machineID, grantID, body.Op, body.Body)
	}
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": json.RawMessage(result)})
}

func (h remoteControlHandler) svcView(grant remotecontrol.Grant) remotecontrol.GrantView {
	online := h.devices != nil && h.devices.IsMachineOnline(grant.TargetMachineID)
	name := grant.TargetMachineName
	if name == "" {
		name = grant.ControllerMachineName
	}
	return remotecontrol.GrantView{
		ID:          grant.ID,
		MachineID:   grant.TargetMachineID,
		MachineName: name,
		Account:     grant.TargetAccount,
		Permission:  grant.Permission,
		Status:      grant.Status,
		SameAccount: grant.SameAccount,
		Online:      online,
	}
}

func (h remoteControlHandler) viewer(w http.ResponseWriter, r *http.Request) (*auth.ViewerPrincipal, string, bool) {
	principal, err := authenticateViewerRequest(r, h.identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, "", false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-Id"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	}
	return principal, machineID, true
}

func decodeRemoteControlBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "远控请求无效")
		return false
	}
	return true
}

func writeRemoteControlError(w http.ResponseWriter, err error) {
	var status *remotecontrol.StatusError
	switch {
	case errors.As(err, &status):
		code := http.StatusBadRequest
		switch status.Code {
		case "FORBIDDEN":
			code = http.StatusForbidden
		case "OFFLINE":
			code = http.StatusConflict
		case "TIMEOUT":
			code = http.StatusGatewayTimeout
		}
		writeError(w, code, status.Code, status.Message)
	case errors.Is(err, remotecontrol.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, remotecontrol.ErrOffline):
		writeError(w, http.StatusConflict, "OFFLINE", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "REMOTE_CONTROL", err.Error())
	}
}
