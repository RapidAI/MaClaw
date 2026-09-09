//go:build windows && cgo

package database

import _ "github.com/alexbrainman/odbc"

// ODBCDriverLinked reports whether the optional Access adapter is available in
// this build. The cgo ODBC bridge is only linked for Windows builds with cgo
// enabled; CGO_ENABLED=0 Windows builds compile without it and report the
// Access capability as driver_missing instead of failing to build.
func ODBCDriverLinked() bool { return true }
