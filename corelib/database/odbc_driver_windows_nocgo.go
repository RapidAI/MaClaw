//go:build windows && !cgo

package database

// Access/ODBC is an optional capability. CGO_ENABLED=0 Windows builds do not
// link the cgo ODBC bridge; runtime detection (DetectAccessODBC) still works
// so doctor and error paths can give actionable installation guidance.

// ODBCDriverLinked reports whether the optional Access adapter is available in
// this build.
func ODBCDriverLinked() bool { return false }
