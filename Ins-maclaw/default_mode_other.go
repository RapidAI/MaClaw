//go:build !windows

package main

func defaultRunMode() string {
	if !isTerminal() {
		return "gui"
	}
	return "tui"
}
