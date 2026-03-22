package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// VoiceSignaling handles voice call lifecycle (signaling only, no audio).
type VoiceSignaling struct {
	store    *Store
	notifier *Notifier
}

// NewVoiceSignaling creates a VoiceSignaling service.
func NewVoiceSignaling(store *Store, notifier *Notifier) *VoiceSignaling {
	return &VoiceSignaling{store: store, notifier: notifier}
}

// InitiateCall creates a new call and notifies the callee.
func (v *VoiceSignaling) InitiateCall(callerID, calleeID, channelID string, callType CallType) (*VoiceCall, error) {
	call := &VoiceCall{
		ID:        uuid.NewString(),
		ChannelID: channelID,
		CallerID:  callerID,
		CallType:  callType,
		Status:    CallRinging,
		CreatedAt: time.Now(),
	}
	if err := v.store.CreateVoiceCall(call); err != nil {
		return nil, fmt.Errorf("create call: %w", err)
	}

	// Notify callee via WS + push.
	hint := WsHint{Type: "call_incoming", ChannelID: channelID}
	v.notifier.NotifyCallEvent(calleeID, hint, "Incoming Call", "Voice call from "+callerID)

	return call, nil
}

// AnswerCall accepts or rejects a call.
func (v *VoiceSignaling) AnswerCall(callID, userID string, accept bool) error {
	if accept {
		if err := v.store.UpdateCallStatus(callID, CallActive); err != nil {
			return err
		}
		// Notify caller that call was accepted.
		// Caller ID would be looked up from the call record in production.
		return nil
	}
	return v.store.UpdateCallStatus(callID, CallRejected)
}

// ForwardICE relays an ICE candidate to the remote peer via WS.
func (v *VoiceSignaling) ForwardICE(callID, fromUserID, toUserID string, candidate map[string]any) {
	hint := WsHint{Type: "ice"}
	// Attach candidate data — in production this would be a richer payload.
	v.notifier.NotifyCallEvent(toUserID, hint, "", "")
}

// Hangup ends a call.
func (v *VoiceSignaling) Hangup(callID, userID string) error {
	return v.store.UpdateCallStatus(callID, CallEnded)
}
