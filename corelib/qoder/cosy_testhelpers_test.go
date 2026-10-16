package qoder

import "github.com/RapidAI/CodeClaw/corelib/qoder/cosy"

// cosyNewForTest builds a wasm-backed signing context for tests; real network
// calls are never made by the caller.
func cosyNewForTest() (*cosy.Context, error) {
	return cosy.New(MachineID(), ClientVersion, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743")
}
