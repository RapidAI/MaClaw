//go:build windows

package database

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Item 11 (minor): a missing ODBC registry key means "driver not installed",
// not a probe failure — it must surface as an empty driver list so the
// installation hint is shown instead of being suppressed by ProbeError.
func TestReadODBCDriverNamesMissingKeyIsNotProbeError(t *testing.T) {
	names, err := readODBCDriverNames(registry.LOCAL_MACHINE, `SOFTWARE\ODBC\ODBCINST.INI\__missing_reviewfix__`, registry.WOW64_64KEY)
	if err != nil {
		t.Fatalf("missing registry key reported as error: %v", err)
	}
	if names != nil {
		t.Fatalf("missing registry key returned names: %v", names)
	}
}
