package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

var remoteControlFederationClient = &http.Client{
	Timeout: 23 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type remoteControlFederationRequest struct {
	GrantID               string          `json:"grant_id,omitempty"`
	OriginHub             string          `json:"origin_hub"`
	Secret                string          `json:"secret"`
	Contact               string          `json:"contact,omitempty"`
	Permission            string          `json:"permission,omitempty"`
	ControllerUserID      string          `json:"controller_user_id,omitempty"`
	ControllerMachineID   string          `json:"controller_machine_id,omitempty"`
	ControllerMachineName string          `json:"controller_machine_name,omitempty"`
	TargetMachineID       string          `json:"target_machine_id,omitempty"`
	TargetMachineName     string          `json:"target_machine_name,omitempty"`
	TenantID              string          `json:"tenant_id,omitempty"`
	ExpiresAt             time.Time       `json:"expires_at,omitempty"`
	Action                string          `json:"action,omitempty"`
	FromController        bool            `json:"from_controller,omitempty"`
	Op                    string          `json:"op,omitempty"`
	Body                  json.RawMessage `json:"body,omitempty"`
}

func remoteControlRelayPost(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := remoteControlFederationClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, remoteControlFederationBodyLimit+1))
	if len(raw) > remoteControlFederationBodyLimit {
		return nil, errors.New("跨 Hub 远控结果太大")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &failure)
		if failure.Message == "" {
			failure.Message = "对方 Hub 暂时无法处理远控"
		}
		if failure.Code == "" {
			failure.Code = "REMOTE"
		}
		return nil, &remotecontrol.StatusError{Code: failure.Code, Message: failure.Message}
	}
	return raw, nil
}

const remoteControlFederationBodyLimit = 8 << 20

func (h remoteControlHandler) inviteAcrossHub(ctx context.Context, tenantID, userID, machineID, account, permission string, localErr error) (remotecontrol.Grant, error) {
	home, ok := companionDiscoverHomeHub(ctx, account)
	if !ok {
		return remotecontrol.Grant{}, localErr
	}
	origin := companionLocalOrigin()
	if origin == "" {
		return remotecontrol.Grant{}, &remotecontrol.StatusError{Code: "INVALID", Message: "本 Hub 尚未配置公网地址，无法跨 Hub 远控"}
	}
	secret, err := newCompanionReaderGrant()
	if err != nil {
		return remotecontrol.Grant{}, err
	}
	grant, err := h.svc.InviteFederated(ctx, tenantID, userID, machineID, account, permission, origin, home.BaseURL, secret)
	if err != nil {
		return remotecontrol.Grant{}, err
	}
	_, err = remoteControlRelayPost(ctx, home.BaseURL+"/api/remote-control/federation/invites", remoteControlFederationRequest{
		GrantID: grant.ID, OriginHub: origin, Secret: grant.FederationSecret, Contact: account,
		Permission: grant.Permission, ControllerUserID: grant.ControllerUserID,
		ControllerMachineID: grant.ControllerMachineID, ControllerMachineName: grant.ControllerMachineName,
		TenantID: grant.TenantID, ExpiresAt: grant.ExpiresAt,
	})
	if err != nil {
		_ = h.svc.Revoke(context.Background(), tenantID, userID, machineID, grant.ID)
		return remotecontrol.Grant{}, err
	}
	return grant, nil
}

func decodeRemoteControlFederation(w http.ResponseWriter, r *http.Request) (remoteControlFederationRequest, string, bool) {
	defer r.Body.Close()
	var body remoteControlFederationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, remoteControlFederationBodyLimit)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "跨 Hub 远控请求无效")
		return body, "", false
	}
	body.OriginHub = companionHubBaseURL(body.OriginHub)
	body.Secret = strings.TrimSpace(body.Secret)
	if body.OriginHub == "" || len(body.Secret) < 32 || !companionOriginIsTrusted(r.Context(), body.OriginHub) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "无法确认远控来源 Hub")
		return body, "", false
	}
	return body, body.OriginHub, true
}

func remoteControlInviteProofMatches(invite, proof remoteControlFederationRequest) bool {
	return (proof.Permission == "" || proof.Permission == invite.Permission) &&
		(proof.ControllerUserID == "" || proof.ControllerUserID == invite.ControllerUserID) &&
		(proof.ControllerMachineID == "" || proof.ControllerMachineID == invite.ControllerMachineID) &&
		(proof.Contact == "" || proof.Contact == invite.Contact) &&
		(proof.TenantID == "" || proof.TenantID == invite.TenantID) &&
		(proof.ExpiresAt.IsZero() || invite.ExpiresAt.IsZero() || proof.ExpiresAt.Equal(invite.ExpiresAt))
}

func (h remoteControlHandler) federationInvite(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grantID := strings.TrimSpace(body.GrantID)
	if grantID == "" || body.ControllerUserID == "" || body.ControllerMachineID == "" || body.Contact == "" {
		writeError(w, http.StatusBadRequest, "INVALID", "跨 Hub 远控邀请无效")
		return
	}
	raw, err := remoteControlRelayPost(r.Context(), origin+"/api/remote-control/federation/proof", remoteControlFederationRequest{
		GrantID: grantID, OriginHub: origin, Secret: body.Secret,
	})
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	var proof remoteControlFederationRequest
	if json.Unmarshal(raw, &proof) != nil || !remoteControlInviteProofMatches(body, proof) {
		writeError(w, http.StatusBadRequest, "INVALID", "远控邀请证明与内容不一致")
		return
	}
	userID := companionContactUserIDAcrossTenants(r.Context(), h.identity, body.Contact)
	if userID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到这个帐号")
		return
	}
	targetHub := companionLocalOrigin()
	if targetHub == "" {
		writeError(w, http.StatusBadRequest, "INVALID", "本 Hub 尚未配置公网地址")
		return
	}
	now := time.Now().UTC()
	expiresAt := body.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(7 * 24 * time.Hour)
	}
	err = h.svc.ImportFederatedInvite(r.Context(), remotecontrol.Grant{
		ID: grantID, TenantID: body.TenantID, ControllerUserID: body.ControllerUserID,
		ControllerMachineID: body.ControllerMachineID, ControllerMachineName: body.ControllerMachineName,
		TargetUserID: userID, TargetAccount: body.Contact, Permission: body.Permission,
		ControllerHub: origin, TargetHub: targetHub, FederationSecret: body.Secret,
		CreatedAt: now, ExpiresAt: expiresAt, UpdatedAt: now,
	})
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h remoteControlHandler) federationProof(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grant, err := h.svc.FederationGrant(r.Context(), body.GrantID, body.Secret)
	if err != nil || grant.ControllerHub != origin || (grant.Status != remotecontrol.StatusPending && grant.Status != remotecontrol.StatusActive) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到这次远控")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "status": grant.Status, "permission": grant.Permission,
		"controller_user_id": grant.ControllerUserID, "controller_machine_id": grant.ControllerMachineID,
		"contact": grant.TargetAccount, "tenant_id": grant.TenantID, "expires_at": grant.ExpiresAt,
	})
}

func (h remoteControlHandler) federationDecision(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grant, err := h.svc.FederationGrant(r.Context(), body.GrantID, body.Secret)
	if err != nil || grant.ControllerHub != origin {
		writeRemoteControlError(w, remotecontrol.ErrNotFound)
		return
	}
	switch body.Action {
	case "accept":
		grant, err = h.svc.AcceptFederated(r.Context(), body.GrantID, body.Secret, body.TargetMachineID, body.TargetMachineName, body.Permission)
	case "decline":
		err = h.svc.RejectFederated(r.Context(), body.GrantID, body.Secret)
	case "revoke":
		err = h.svc.RevokeFederated(r.Context(), body.GrantID, body.Secret, body.FromController)
	default:
		err = &remotecontrol.StatusError{Code: "INVALID", Message: "未知的远控处理"}
	}
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "grant": h.svcView(grant)})
}

func (h remoteControlHandler) federationOp(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grant, err := h.svc.FederationGrant(r.Context(), body.GrantID, body.Secret)
	if err != nil || grant.ControllerHub != origin {
		writeRemoteControlError(w, remotecontrol.ErrNotFound)
		return
	}
	result, err := h.svc.Exec(r.Context(), grant.TenantID, grant.ControllerUserID, grant.ControllerMachineID, grant.ID, body.Op, body.Body)
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": json.RawMessage(result)})
}

// remoteControlStreamPayloadLimit bounds one mirrored stream event. Token
// batches are tiny; the terminal response can be larger but stays far below
// this. Oversized envelopes are dropped rather than truncated — a cut JSON
// body would surface as a confusing parse failure on the controller.
const remoteControlStreamPayloadLimit = 256 << 10

// remoteControlStreamEnvelope wraps a controlled desktop's stream payload for
// delivery to the controlling machine's WebSocket.
func remoteControlStreamEnvelope(targetMachineID string, payload json.RawMessage) map[string]any {
	return map[string]any{
		"type":       "remote_control.stream",
		"ts":         time.Now().Unix(),
		"machine_id": targetMachineID,
		"payload":    payload,
	}
}

// relayControlledStream is installed as ws.OnRemoteControlStream. The payload
// is the opaque body produced by the controlled desktop; this layer only
// authenticates the grant it rides on and routes it to the controller —
// locally over WebSocket, or to the controller's own Hub when the grant is
// federated.
func (h remoteControlHandler) relayControlledStream(machineID string, payload json.RawMessage) {
	if len(payload) == 0 || len(payload) > remoteControlStreamPayloadLimit {
		return
	}
	var head struct {
		GrantID string `json:"grant_id"`
	}
	if err := json.Unmarshal(payload, &head); err != nil || head.GrantID == "" {
		return
	}
	if !h.svc.AllowStream(head.GrantID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	grant, err := h.svc.StreamGrant(ctx, head.GrantID, machineID)
	if err != nil {
		return
	}
	if grant.ControllerHub == "" {
		_ = (remoteControlMachines{devices: h.devices}).Send(grant.ControllerMachineID, remoteControlStreamEnvelope(grant.TargetMachineID, payload))
		return
	}
	// Federated grant: this Hub cannot reach the controller's machine. Hand
	// the event to the controller's Hub over the federation channel; loss is
	// acceptable (fire-and-forget mirror, transcript poll is the fallback).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := remoteControlRelayPost(ctx, grant.ControllerHub+"/api/remote-control/federation/grants/"+grant.ID+"/stream", remoteControlFederationRequest{
			GrantID: grant.ID, OriginHub: grant.TargetHub, Secret: grant.FederationSecret, Body: payload,
		}); err != nil {
			log.Printf("[remote-control] federated stream relay failed grant=%s hub=%s: %v", grant.ID, grant.ControllerHub, err)
		}
	}()
}

// federationStream receives a mirrored stream event from the target's Hub and
// pushes it to the controller machine on this Hub. Unlike federationOp it
// never blocks on the target: the answer is always ok.
func (h remoteControlHandler) federationStream(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grant, err := h.svc.FederationGrant(r.Context(), body.GrantID, body.Secret)
	if err != nil || grant.TargetHub != origin || grant.Status != remotecontrol.StatusActive {
		writeRemoteControlError(w, remotecontrol.ErrNotFound)
		return
	}
	if len(body.Body) == 0 || len(body.Body) > remoteControlStreamPayloadLimit || !h.svc.AllowStream(grant.ID) {
		writeError(w, http.StatusBadRequest, "INVALID", "远控流事件无效")
		return
	}
	if err := (remoteControlMachines{devices: h.devices}).Send(grant.ControllerMachineID, remoteControlStreamEnvelope(grant.TargetMachineID, body.Body)); err != nil {
		writeRemoteControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h remoteControlHandler) federationStatus(w http.ResponseWriter, r *http.Request) {
	body, origin, ok := decodeRemoteControlFederation(w, r)
	if !ok {
		return
	}
	grant, err := h.svc.FederationGrant(r.Context(), body.GrantID, body.Secret)
	if err != nil || grant.ControllerHub != origin || grant.Status != remotecontrol.StatusActive {
		writeRemoteControlError(w, remotecontrol.ErrNotFound)
		return
	}
	online := h.devices != nil && h.devices.IsMachineOnline(grant.TargetMachineID)
	name := grant.TargetMachineName
	if machine, found := (remoteControlMachines{devices: h.devices}).BelongsTo(r.Context(), grant.TargetUserID, grant.TargetMachineID); found {
		name = machine.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": online, "machine_name": name})
}

func (h remoteControlHandler) refreshFederatedGrants(ctx context.Context, tenantID, userID, machineID string, state *remotecontrol.State) {
	if state == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range state.Grants {
		view := &state.Grants[i]
		grant, err := h.svc.ControllerGrant(ctx, tenantID, userID, machineID, view.ID)
		if err != nil || grant.TargetHub == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := remoteControlRelayPost(ctx, grant.TargetHub+"/api/remote-control/federation/grants/"+grant.ID+"/status", remoteControlFederationRequest{
				GrantID: grant.ID, OriginHub: grant.ControllerHub, Secret: grant.FederationSecret,
			})
			if err != nil {
				view.Online = false
				h.dropStaleFederatedGrant(ctx, grant, err)
				return
			}
			var status struct {
				Online      bool   `json:"online"`
				MachineName string `json:"machine_name"`
			}
			if json.Unmarshal(raw, &status) == nil {
				view.Online = status.Online
				if strings.TrimSpace(status.MachineName) != "" {
					view.MachineName = strings.TrimSpace(status.MachineName)
				}
			}
		}()
	}
	wg.Wait()
}

func (h remoteControlHandler) relayTargetDecision(ctx context.Context, grant remotecontrol.Grant, action string) error {
	if grant.ControllerHub == "" {
		return nil
	}
	_, err := remoteControlRelayPost(ctx, grant.ControllerHub+"/api/remote-control/federation/grants/"+grant.ID+"/decision", remoteControlFederationRequest{
		GrantID: grant.ID, OriginHub: grant.ControllerHub, Secret: grant.FederationSecret, Action: action, Permission: grant.Permission,
		TargetMachineID: grant.TargetMachineID, TargetMachineName: grant.TargetMachineName,
	})
	return err
}

func (h remoteControlHandler) relayControllerRevoke(ctx context.Context, grant remotecontrol.Grant) error {
	if grant.TargetHub == "" {
		return nil
	}
	_, err := remoteControlRelayPost(ctx, grant.TargetHub+"/api/remote-control/federation/grants/"+grant.ID+"/decision", remoteControlFederationRequest{
		GrantID: grant.ID, OriginHub: grant.ControllerHub, Secret: grant.FederationSecret, Action: "revoke", FromController: true,
	})
	return err
}

func (h remoteControlHandler) execAcrossHub(ctx context.Context, grant remotecontrol.Grant, op string, body json.RawMessage) (json.RawMessage, error) {
	if grant.Status != remotecontrol.StatusActive {
		return nil, remotecontrol.ErrNotFound
	}
	raw, err := remoteControlRelayPost(ctx, grant.TargetHub+"/api/remote-control/federation/grants/"+grant.ID+"/op", remoteControlFederationRequest{
		GrantID: grant.ID, OriginHub: grant.ControllerHub, Secret: grant.FederationSecret, Op: op, Body: body,
	})
	if err != nil {
		h.dropStaleFederatedGrant(ctx, grant, err)
		return nil, err
	}
	var decoded struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("跨 Hub 远控结果无法读取")
	}
	return decoded.Result, nil
}

// dropStaleFederatedGrant ends this Hub's copy when the other Hub answers
// NOT_FOUND: the grant was revoked or displaced there, so it must not stay
// listed as controllable here. The controller user hears about the end only
// once because the first revoke flips the status.
func (h remoteControlHandler) dropStaleFederatedGrant(ctx context.Context, grant remotecontrol.Grant, err error) {
	var status *remotecontrol.StatusError
	if !errors.As(err, &status) || status.Code != "NOT_FOUND" {
		return
	}
	if revokeErr := h.svc.RevokeFederated(ctx, grant.ID, grant.FederationSecret, false); revokeErr != nil {
		log.Printf("[remote-control] stale federated grant %s revoke failed: %v", grant.ID, revokeErr)
	}
}
