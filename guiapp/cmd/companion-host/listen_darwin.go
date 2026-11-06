//go:build darwin

package main

// listenSafari would accept one NSXPC connection on
// group.com.wails.<name>.companion.companion-host and exec the GUI with
// companion-paper-host. No signed appex is in this build, so the lookup is not
// attempted and the process does not report success.
func listenSafari() int {
	return 1
}
