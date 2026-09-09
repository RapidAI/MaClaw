//go:build !darwin

package guiapp

func macOSMajorVersion() int    { return 0 }
func isMacOSTahoeOrLater() bool { return false }
