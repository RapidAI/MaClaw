package im

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// eventAckSender records every envelope the Hub pushes at the GUI and can be
// told to fail, so the "offline is buffered, buffer-full is lost" distinction
// can be asserted instead of assumed.
type eventAckSender struct {
	messages []map[string]any
	err      error
}

func (s *eventAckSender) SendToMachine(_ string, msg any) error {
	if s.err != nil {
		return s.err
	}
	envelope, _ := msg.(map[string]any)
	s.messages = append(s.messages, envelope)
	return nil
}

func (s *eventAckSender) lastPayload(t *testing.T) map[string]any {
	t.Helper()
	if len(s.messages) == 0 {
		t.Fatal("no envelope was pushed to the GUI")
	}
	envelope := s.messages[len(s.messages)-1]
	if envelope["type"] != deviceEventAckEnvelopeType {
		t.Fatalf("envelope type=%#v, want %s", envelope["type"], deviceEventAckEnvelopeType)
	}
	payload, _ := envelope["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("envelope carries no payload: %#v", envelope)
	}
	return payload
}

// newEventAckGateway wires one paired, event-capable device under machine-1 and
// snapshots a pending approval, which is the exact state N1-6 starts from.
func newEventAckGateway(t *testing.T, sender *eventAckSender) *DeviceGateway {
	t.Helper()
	gateway := NewDeviceGateway(nil)
	gateway.SetMachineMessageSender(sender)
	bindEventPushDevice(t, gateway, "pet-approval", "machine-1", eventPushCapabilities(240))
	gateway.UpdateMachineEvent("machine-1", fullEventReply(nil)["event"].(map[string]any))
	return gateway
}

func ackDeviceEvent(t *testing.T, gateway *DeviceGateway, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/event-ack", "tok-pet-approval", body)
}

func ackBody(overrides map[string]any) map[string]any {
	body := map[string]any{
		"clientId":  "pet-approval",
		"eventId":   "evt_01H8ZK",
		"status":    coreim.DeviceEventAckStatusApproved,
		"actionId":  "approve",
		"decidedBy": coreim.DeviceEventDecidedByButton,
	}
	for key, value := range overrides {
		if value == nil {
			delete(body, key)
			continue
		}
		body[key] = value
	}
	return body
}

func decodeAckResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func snapshotEvent(t *testing.T, gateway *DeviceGateway, machineID string) (storedDeviceEvent, bool) {
	t.Helper()
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	stored, ok := gateway.eventsByMachine[machineID]
	return stored, ok
}

func TestEventAckDeliversAnApprovalDecisionToTheGUI(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeAckResponse(t, recorder)
	if response["accepted"] != true || response["duplicate"] != false {
		t.Fatalf("response=%#v", response)
	}
	payload := sender.lastPayload(t)
	if payload["eventId"] != "evt_01H8ZK" || payload["status"] != coreim.DeviceEventAckStatusApproved ||
		payload["actionId"] != "approve" || payload["decidedBy"] != coreim.DeviceEventDecidedByButton ||
		payload["clientId"] != "pet-approval" {
		t.Fatalf("ack payload=%#v", payload)
	}
	if _, still := snapshotEvent(t, gateway, "machine-1"); still {
		t.Fatal("a terminal decision must clear the pending snapshot entry")
	}
}

func TestEventAckAcceptsAReceiptWithoutEndingTheEvent(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(map[string]any{
		"status": coreim.DeviceEventAckStatusReceived, "actionId": nil, "decidedBy": nil,
	}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	payload := sender.lastPayload(t)
	if payload["status"] != coreim.DeviceEventAckStatusReceived {
		t.Fatalf("payload=%#v", payload)
	}
	if _, hasAction := payload["actionId"]; hasAction {
		t.Fatalf("a receipt must not carry an action: %#v", payload)
	}
	// A receipt only proves the card arrived. Clearing the snapshot here would
	// make a reboot mid-decision lose the prompt the user still has to answer.
	if _, still := snapshotEvent(t, gateway, "machine-1"); !still {
		t.Fatal("a receipt must not clear the pending snapshot entry")
	}
}

func TestEventAckIsIdempotentForAReplay(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	body := ackBody(map[string]any{"status": coreim.DeviceEventAckStatusReceived, "actionId": nil, "decidedBy": nil})
	if recorder := ackDeviceEvent(t, gateway, body); recorder.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	second := ackDeviceEvent(t, gateway, body)
	if second.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", second.Code, second.Body.String())
	}
	if decodeAckResponse(t, second)["duplicate"] != true {
		t.Fatalf("replay response=%s", second.Body.String())
	}
	if len(sender.messages) != 1 {
		t.Fatalf("a replay must not be pushed twice: %d envelopes", len(sender.messages))
	}
}

func TestEventAckRejectsAnUnknownEvent(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(map[string]any{"eventId": "evt_never_sent"}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("an unknown event must never reach the GUI: %#v", sender.messages)
	}
	// The real event is untouched, so the user can still answer it.
	if _, still := snapshotEvent(t, gateway, "machine-1"); !still {
		t.Fatal("an unknown ack must not clear the pending snapshot entry")
	}
}

func TestEventAckRejectsAnActionTheEventNeverOffered(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(map[string]any{"actionId": "approve_everything"}))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("an undeclared action must never reach the GUI: %#v", sender.messages)
	}
}

func TestEventAckRejectsAClientIDMismatch(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(map[string]any{"clientId": "someone-else"}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestEventAckRejectsAnAuditContractViolation(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	// A decision without a modality cannot be audited as "approved by voice"
	// or "approved by button", which is the whole point of N1-6's third
	// acceptance requirement.
	recorder := ackDeviceEvent(t, gateway, ackBody(map[string]any{"decidedBy": nil}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("a malformed ack must never reach the GUI: %#v", sender.messages)
	}
}

func TestEventAckForcesAClosedWindowToTimeout(t *testing.T) {
	sender := &eventAckSender{}
	gateway := newEventAckGateway(t, sender)

	// Close the window behind the device's back. The device still believes it
	// has time, which is exactly the case the Hub must adjudicate itself.
	gateway.mu.Lock()
	stored := gateway.eventsByMachine["machine-1"]
	stored.ExpiresAtUnixMs = time.Now().Add(-time.Second).UnixMilli()
	gateway.eventsByMachine["machine-1"] = stored
	gateway.mu.Unlock()

	recorder := ackDeviceEvent(t, gateway, ackBody(nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := decodeAckResponse(t, recorder)["status"]; got != coreim.DeviceEventAckStatusExpired {
		t.Fatalf("status=%#v, want expired", got)
	}
	payload := sender.lastPayload(t)
	if payload["status"] != coreim.DeviceEventAckStatusExpired ||
		payload["decidedBy"] != coreim.DeviceEventDecidedByTimeout {
		t.Fatalf("a late approval must become a timeout, got %#v", payload)
	}
	if _, hasAction := payload["actionId"]; hasAction {
		t.Fatalf("a timeout has no decider and no action: %#v", payload)
	}
	if _, still := snapshotEvent(t, gateway, "machine-1"); still {
		t.Fatal("a forced timeout is terminal and must clear the snapshot")
	}
}

func TestEventAckIsDeliveredWhenTheGUIIsOffline(t *testing.T) {
	sender := &eventAckSender{err: fmt.Errorf("%w: no desktop connection", device.ErrMachineOffline)}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("an offline GUI buffers the envelope, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, still := snapshotEvent(t, gateway, "machine-1"); still {
		t.Fatal("a decision accepted for later delivery must still close the event")
	}
}

func TestEventAckIsRefusedWhenTheSendBufferIsFull(t *testing.T) {
	sender := &eventAckSender{err: fmt.Errorf("%w: %w", device.ErrMachineOffline, device.ErrMachineSendBufferFull)}
	gateway := newEventAckGateway(t, sender)

	recorder := ackDeviceEvent(t, gateway, ackBody(nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("a dropped decision must be refused so the device retries, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// Keeping the snapshot is what makes the retry possible at all.
	if _, still := snapshotEvent(t, gateway, "machine-1"); !still {
		t.Fatal("a refused ack must not clear the pending snapshot entry")
	}
}

func TestEventAckRequiresPOST(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	recorder := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/event-ack", "", nil)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestDeviceEventActionDeclaredMatchesOnlyOfferedActions(t *testing.T) {
	stored := storedDeviceEvent{Event: fullEventReply(nil)["event"].(map[string]any)}
	cases := map[string]bool{
		"":         true, // receipts and timeouts carry no action
		"approve":  true,
		"reject":   true,
		"approve2": false,
		"APPROVE":  false, // ids are opaque, not case-folded
	}
	for actionID, want := range cases {
		if got := deviceEventActionDeclared(stored, actionID); got != want {
			t.Fatalf("action %q: got %v want %v", actionID, got, want)
		}
	}
}

func TestDeviceEventWindowClosedUsesTheAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if deviceEventWindowClosed(storedDeviceEvent{}, now) {
		t.Fatal("an event with no recorded expiry has no window to close")
	}
	open := storedDeviceEvent{ExpiresAtUnixMs: now.Add(time.Second).UnixMilli()}
	if deviceEventWindowClosed(open, now) {
		t.Fatal("a future expiry must stay open")
	}
	closed := storedDeviceEvent{ExpiresAtUnixMs: now.UnixMilli()}
	if !deviceEventWindowClosed(closed, now) {
		t.Fatal("the expiry instant itself is closed, so a boundary second cannot approve")
	}
}
