//go:build !darwin

package main

// listenSafari is the LaunchAgent entry. Windows and Linux have no agent.
// The process writes nothing: a length prefix on stdout would be a fake frame.
func listenSafari() int {
	return 1
}
