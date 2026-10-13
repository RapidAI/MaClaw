//go:build darwin && arm64

package tensor

import "golang.org/x/sys/unix"

// armHasDotProd probes ARMv8.2 SDOT/UDOT support on macOS (all Apple
// Silicon M1+ report 1). Pure Go sysctl, works with CGO_ENABLED=0.
func armHasDotProd() bool {
	for _, k := range []string{"hw.optional.arm.FEAT_DotProd", "hw.optional.armv8_2_dotprod"} {
		if v, err := unix.SysctlUint32(k); err == nil && v != 0 {
			return true
		}
	}
	return false
}

// armHasI8MM probes FEAT_I8MM (SMMLA) on macOS (Apple M2 and later).
func armHasI8MM() bool {
	v, err := unix.SysctlUint32("hw.optional.arm.FEAT_I8MM")
	return err == nil && v != 0
}
