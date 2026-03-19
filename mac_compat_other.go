//go:build !darwin

package main

func macOSMajorVersion() int    { return 0 }
func isMacOSTahoeOrLater() bool { return false }
