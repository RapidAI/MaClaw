package ws

import "encoding/json"

// OnRemoteControlResult receives a desktop's reply to remote_control.exec.
// The HTTP layer installs it while the grant service is alive.
var OnRemoteControlResult func(machineID, requestID string, payload json.RawMessage)

func deliverRemoteControlResult(machineID, requestID string, payload json.RawMessage) {
	if OnRemoteControlResult == nil {
		return
	}
	OnRemoteControlResult(machineID, requestID, payload)
}

// OnRemoteControlStream receives a controlled desktop's live mirror event.
// The HTTP layer installs it; delivery is fire-and-forget, so a nil hook or a
// slow relay simply drops the event.
var OnRemoteControlStream func(machineID string, payload json.RawMessage)

func deliverRemoteControlStream(machineID string, payload json.RawMessage) {
	if OnRemoteControlStream == nil {
		return
	}
	OnRemoteControlStream(machineID, payload)
}
