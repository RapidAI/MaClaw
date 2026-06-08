package corelib

const (
	DefaultRemoteHeartbeatSec = 30
	MinRemoteHeartbeatSec     = 5
)

// NormalizeRemoteHeartbeatIntervalSec converts a configured heartbeat
// interval to the effective value used by runtime and config writers.
func NormalizeRemoteHeartbeatIntervalSec(value int) int {
	if value <= 0 {
		return DefaultRemoteHeartbeatSec
	}
	if value < MinRemoteHeartbeatSec {
		return MinRemoteHeartbeatSec
	}
	return value
}
