package im

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// deviceEventAckEnvelopeType is the Hub -> GUI push that carries a decision the
// user made on a paired terminal (plan N1-6).
//
// It is a device-gateway envelope rather than an `im.gateway_reply` on purpose.
// `im.gateway_reply` is the IM adapter's outbound channel and the GUI routes it
// by platform into qqBot/weixin/lansenger; a button press is not an IM message
// and must not be handed to those adapters. `im.device_gateway_playback_receipt`
// already established this envelope family, and guiapp mirrors this literal the
// same way it mirrors that one (the Hub's internal package is not importable
// from guiapp).
const deviceEventAckEnvelopeType = "im.device_gateway_event_ack"

// handleEventAck implements `POST /api/im-gateway/v1/event-ack` (plan section
// 4.3): the single endpoint that carries both the transport receipt
// (`status=received`) and the user's decision (`approved|rejected|expired`).
//
// It is the only path by which a choice made on the terminal reaches the brain,
// so it is deliberately suspicious about everything the device claims:
//
//   - the ack must name an event the Hub actually sent to *this* machine
//     (correlation by eventId, not a client-supplied hint);
//   - a decision must name an action the card actually offered;
//   - the Hub -- not the device -- decides whether the window is still open,
//     because "timed out means not approved" (plan D5) must not be defeated by
//     a device whose clock is wrong.
func (g *DeviceGateway) handleEventAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
		return
	}
	var req coreim.ThirdPartyEventAckRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ClientID) != p.ClientID {
		writeDeviceError(w, http.StatusForbidden, "forbidden", "clientId does not match credential")
		return
	}
	if err := coreim.NormalizeThirdPartyEventAckRequest(&req); err != nil {
		LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventRejected, "malformed_ack")
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	g.mu.Lock()
	stored, known := g.pendingDeviceEventLocked(p.MachineID, req.EventID)
	g.mu.Unlock()
	if !known {
		// An ack naming an event this machine never received is either a retry
		// that outlived the snapshot or an injected decision. Both are refused
		// rather than applied, so a device cannot vote on someone else's prompt.
		LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventRejected, "unknown_event")
		writeDeviceError(w, http.StatusConflict, "unknown_event", "no pending event with that eventId for this machine")
		return
	}
	if !deviceEventActionDeclared(stored, req.ActionID) {
		LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventRejected, "unknown_action")
		writeDeviceError(w, http.StatusConflict, "unknown_action", "actionId was not offered by this event")
		return
	}
	if deviceEventWindowClosed(stored, time.Now()) {
		// Rewriting rather than rejecting keeps the endpoint's answer honest:
		// the event *was* real, it simply has no valid decision left. It is
		// delivered as a timeout so the audit trail records "nobody answered"
		// instead of the late approval the user happened to press.
		req.Status = coreim.DeviceEventAckStatusExpired
		req.ActionID = ""
		req.DecidedBy = coreim.DeviceEventDecidedByTimeout
	}

	dedupeKey := coreim.ThirdPartyEventAckEventID(req)
	if g.deviceEventHandled(p.ClientID, dedupeKey) {
		LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventDuplicate, "")
		writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "duplicate": true})
		return
	}
	// Deliver before recording. The dedupe entry must only exist once the
	// decision has actually been handed over: marking first would turn a failed
	// hand-off into "already delivered", and the user's answer would vanish
	// while every retry got a cheerful duplicate.
	if err := g.deliverDeviceEventAck(p, req); err != nil {
		LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventRejected, "gui_unavailable")
		writeDeviceError(w, http.StatusServiceUnavailable, "gui_unavailable", err.Error())
		return
	}
	g.markDeviceEvent(p.ClientID, dedupeKey)
	if coreim.DeviceEventAckIsTerminal(req.Status) {
		// A terminal answer ends the event's life, so the handshake replay must
		// stop offering it. `received` deliberately does not clear it: the card
		// is still waiting for an answer, and a reboot in between should bring
		// it back.
		g.clearMachineEvent(p.MachineID)
	}
	LinkHealth().ObserveDeviceEvent(p.TenantID, DeviceEventAccepted, "")
	writeDeviceJSON(w, http.StatusOK, map[string]any{
		"ok": true, "accepted": true, "duplicate": false, "status": req.Status,
	})
}

// deliverDeviceEventAck pushes one accepted ack to the GUI bound to this
// machine.
//
// An offline GUI is *not* a failure: the Hub buffers machine messages and
// replays them on reconnect, so the decision still arrives. The one case that
// really loses the message is a full send buffer, and that is reported so the
// device retries instead of the answer disappearing silently.
func (g *DeviceGateway) deliverDeviceEventAck(p devicePrincipal, req coreim.ThirdPartyEventAckRequest) error {
	g.mu.Lock()
	sender := g.machineSender
	g.mu.Unlock()
	if sender == nil {
		return errors.New("GUI relay is unavailable")
	}
	payload := map[string]any{
		"clientId":  p.ClientID,
		"eventId":   req.EventID,
		"status":    req.Status,
		"decidedBy": req.DecidedBy,
	}
	if req.ActionID != "" {
		payload["actionId"] = req.ActionID
	}
	err := sender.SendToMachine(p.MachineID, map[string]any{
		"type":    deviceEventAckEnvelopeType,
		"payload": payload,
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, device.ErrMachineSendBufferFull) {
		return err
	}
	log.Printf("[device-gateway] event ack buffered for offline GUI machine=%s client=%s event=%s status=%s: %v",
		p.MachineID, p.ClientID, req.EventID, req.Status, err)
	return nil
}

// pendingDeviceEventLocked returns the machine's snapshot entry when its
// eventId matches. Correlation by eventId is what makes "an ack must name an
// event we sent" enforceable at all.
func (g *DeviceGateway) pendingDeviceEventLocked(machineID, eventID string) (storedDeviceEvent, bool) {
	stored, ok := g.eventsByMachine[machineID]
	if !ok || stored.Event == nil {
		return storedDeviceEvent{}, false
	}
	normalized, ok := normalizeDeviceEventPush(stored.Event)
	if !ok {
		return storedDeviceEvent{}, false
	}
	if id, _ := normalized["eventId"].(string); id != strings.TrimSpace(eventID) {
		return storedDeviceEvent{}, false
	}
	return stored, true
}

// clearMachineEvent drops the pending snapshot entry once a terminal answer has
// been accepted. Persisting is best-effort for the same reason it is in
// UpdateMachineEvent: a write failure only costs the reboot replay.
func (g *DeviceGateway) clearMachineEvent(machineID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.eventsByMachine[machineID]; !ok {
		return
	}
	delete(g.eventsByMachine, machineID)
	if err := g.persistTokensLocked(); err != nil {
		log.Printf("device gateway: clear event for machine %q: %v", machineID, err)
	}
}

// deviceEventWindowClosed reports whether the absolute expiry stamped at
// dispatch time has passed. The comparison is against the Hub's own clock, so a
// device that skipped a day forward cannot revive a stale approval.
func deviceEventWindowClosed(stored storedDeviceEvent, now time.Time) bool {
	return stored.ExpiresAtUnixMs > 0 && stored.ExpiresAtUnixMs <= now.UnixMilli()
}

// deviceEventActionDeclared reports whether the event offered the action the
// ack claims was pressed. A decision naming a button the card never showed is
// refused: the audit would otherwise record approval of something the user was
// never asked about. Receipts and timeouts carry no action and pass.
func deviceEventActionDeclared(stored storedDeviceEvent, actionID string) bool {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return true
	}
	normalized, ok := normalizeDeviceEventPush(stored.Event)
	if !ok {
		return false
	}
	actions, _ := normalized["actions"].([]map[string]any)
	for _, action := range actions {
		if id, _ := action["id"].(string); id == actionID {
			return true
		}
	}
	return false
}
