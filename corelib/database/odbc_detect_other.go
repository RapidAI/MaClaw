//go:build !windows

package database

// ODBCDetection mirrors the Windows probe shape so cross-platform callers
// (doctor, error paths) compile unchanged. On non-Windows hosts there is no
// ACE ODBC capability; unixODBC-based Access bridges are not supported.
type ODBCDetection struct {
	AdapterLinked   bool     `json:"adapter_linked"`
	DriverInstalled bool     `json:"driver_installed"`
	DriverNames     []string `json:"driver_names,omitempty"`
	ProcessBitness  string   `json:"process_bitness"`
	MatchingBitness bool     `json:"matching_bitness"`
	ProbeError      string   `json:"probe_error,omitempty"`
}

// Hint returns an actionable installation hint, or "" when nothing is needed.
func (d ODBCDetection) Hint() string {
	if !d.AdapterLinked {
		return "Access/ODBC is only supported on Windows builds"
	}
	return ""
}

// DetectAccessODBC is the non-Windows stub: no adapter, no driver registry.
func DetectAccessODBC() ODBCDetection {
	return ODBCDetection{AdapterLinked: ODBCDriverLinked()}
}
