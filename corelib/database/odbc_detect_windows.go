//go:build windows

package database

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// ODBCDetection is the runtime, cgo-free probe of the machine's Access ODBC
// capability. It reads the ODBC driver registry instead of linking or calling
// the driver, so it works in CGO_ENABLED=0 builds and cannot crash on ABI
// mismatches.
type ODBCDetection struct {
	// AdapterLinked reports whether this binary can actually open ODBC
	// connections (cgo bridge linked).
	AdapterLinked bool `json:"adapter_linked"`
	// DriverInstalled reports whether a Microsoft Access (ACE) ODBC driver is
	// registered on this machine, in either bitness view.
	DriverInstalled bool `json:"driver_installed"`
	// DriverNames holds the registered ACE driver names (both bitness views,
	// deduplicated), e.g. "Microsoft Access Driver (*.mdb, *.accdb)".
	DriverNames []string `json:"driver_names,omitempty"`
	// ProcessBitness is "64" or "32"; the ACE driver bitness must match the
	// process for a DSN-less connection string to load.
	ProcessBitness string `json:"process_bitness"`
	// MatchingBitness reports whether an ACE driver was found in the registry
	// view matching this process's bitness.
	MatchingBitness bool `json:"matching_bitness"`
	// ProbeError records a registry read failure. A probe error never blocks
	// connection attempts — detection is advisory, not authoritative.
	ProbeError string `json:"probe_error,omitempty"`
}

// Hint returns an actionable, non-sensitive installation hint for the current
// detection state, or "" when Access connections should work.
func (d ODBCDetection) Hint() string {
	if d.ProbeError != "" {
		return ""
	}
	if !d.DriverInstalled {
		return "Install the Microsoft Access Database Engine redistributable matching this process (" + d.ProcessBitness + "-bit), or configure a DSN"
	}
	if !d.MatchingBitness {
		return "An Access ODBC driver is installed but only for the other bitness; install the " + d.ProcessBitness + "-bit Access Database Engine"
	}
	if !d.AdapterLinked {
		return "Access ODBC driver is installed, but this build has no ODBC adapter; use a cgo-enabled Windows build"
	}
	return ""
}

const accessDriverNameMarker = "microsoft access driver"

// DetectAccessODBC probes the ODBC driver registry without linking or calling
// any ODBC code (pure registry reads via x/sys/windows/registry — no cgo).
func DetectAccessODBC() ODBCDetection {
	d := ODBCDetection{AdapterLinked: ODBCDriverLinked(), ProcessBitness: processBitness()}
	seen := map[string]bool{}
	var firstErr error
	// 64-bit processes see the 64-bit view by default; WOW6432Node holds the
	// 32-bit registrations. Probe both so bitness mismatches are diagnosable.
	views := []struct {
		flags   uint32
		matches bool
	}{
		{registry.WOW64_64KEY, d.ProcessBitness == "64"},
		{registry.WOW64_32KEY, d.ProcessBitness == "32"},
	}
	for _, view := range views {
		names, err := readODBCDriverNames(registry.LOCAL_MACHINE, `SOFTWARE\ODBC\ODBCINST.INI\ODBC Drivers`, view.flags)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, name := range names {
			if !strings.Contains(strings.ToLower(name), accessDriverNameMarker) {
				continue
			}
			d.DriverInstalled = true
			if view.matches {
				d.MatchingBitness = true
			}
			if !seen[name] {
				seen[name] = true
				d.DriverNames = append(d.DriverNames, name)
			}
		}
	}
	// A 64-bit driver on a 64-bit OS lives in the default view; when the
	// process is 32-bit the 64-bit view still reports the installed driver for
	// diagnostic completeness even though it cannot load it.
	if firstErr != nil && !d.DriverInstalled {
		d.ProbeError = firstErr.Error()
	}
	return d
}

func readODBCDriverNames(root registry.Key, path string, flags uint32) ([]string, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|flags)
	if err != nil {
		// A missing key means no driver of this category is registered — that
		// is "not installed", not a probe failure, and must not suppress the
		// installation hint.
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer key.Close()
	info, err := key.Stat()
	if err != nil {
		return nil, err
	}
	if info.ValueCount == 0 {
		return nil, nil
	}
	return key.ReadValueNames(0)
}

func processBitness() string {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return "64"
	}
	return "32"
}
