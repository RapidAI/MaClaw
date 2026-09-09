//go:build !windows

package database

// Access/ODBC is an optional capability. The ODBC driver is intentionally not
// linked on non-Windows builds so CGO-free TUI binaries remain buildable.

// ODBCDriverLinked reports whether the optional Access adapter is available in
// this build. It is deliberately a compile-time capability so doctor checks
// do not attempt to open a DSN or reveal connection details.
func ODBCDriverLinked() bool { return false }
