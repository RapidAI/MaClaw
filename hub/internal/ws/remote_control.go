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
