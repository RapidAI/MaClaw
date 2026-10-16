//go:build arm64 && !darwin

package tensor

// armHasDotProd: on Linux/Windows x/sys/cpu already reads HWCAP.
func armHasDotProd() bool { return false }

// armHasI8MM: x/sys/cpu reads HWCAP2 on Linux.
func armHasI8MM() bool { return false }
