package main

import "strings"

type gatewayConnectionStatus string

const (
	gatewayConnectionStatusUnknown        gatewayConnectionStatus = ""
	gatewayConnectionStatusDisconnected   gatewayConnectionStatus = "disconnected"
	gatewayConnectionStatusConnecting     gatewayConnectionStatus = "connecting"
	gatewayConnectionStatusConnected      gatewayConnectionStatus = "connected"
	gatewayConnectionStatusError          gatewayConnectionStatus = "error"
	gatewayConnectionStatusReconnecting   gatewayConnectionStatus = "reconnecting"
	gatewayConnectionStatusSessionExpired gatewayConnectionStatus = "session_expired"
	gatewayConnectionStatusConfirmed      gatewayConnectionStatus = "confirmed"
)

func normalizeGatewayConnectionStatus(status string) gatewayConnectionStatus {
	switch gatewayConnectionStatus(strings.ToLower(strings.TrimSpace(status))) {
	case gatewayConnectionStatusDisconnected:
		return gatewayConnectionStatusDisconnected
	case gatewayConnectionStatusConnecting:
		return gatewayConnectionStatusConnecting
	case gatewayConnectionStatusConnected:
		return gatewayConnectionStatusConnected
	case gatewayConnectionStatusError:
		return gatewayConnectionStatusError
	case gatewayConnectionStatusReconnecting:
		return gatewayConnectionStatusReconnecting
	case gatewayConnectionStatusSessionExpired:
		return gatewayConnectionStatusSessionExpired
	case gatewayConnectionStatusConfirmed:
		return gatewayConnectionStatusConfirmed
	default:
		return gatewayConnectionStatusUnknown
	}
}

func (status gatewayConnectionStatus) String() string {
	return string(status)
}

func (status gatewayConnectionStatus) IsConnected() bool {
	return status == gatewayConnectionStatusConnected
}
