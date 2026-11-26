//go:build !wails3

package main

// Default (v2) build: wails3Enabled=false so the dispatcher keeps the v2 path.

const wails3Enabled = false

func Wails3Enabled() bool { return wails3Enabled }

func main() { main2() }
