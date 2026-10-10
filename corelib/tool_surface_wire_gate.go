package corelib

import "net/http"

// ToolSurfaceWireGate is the host round tripper that verifies the canonical
// tool-surface JSON. A provider that then replaces that JSON with a non-JSON
// encoding (Qoder's COSY body) must sit underneath the gate: the gate checks
// the JSON, and the encoder signs the bytes the gate releases.
//
// Installing the encoder above the gate makes the gate parse ciphertext and
// reject a well-formed tool surface as invalid JSON.
type ToolSurfaceWireGate interface {
	http.RoundTripper
	ToolSurfaceNext() http.RoundTripper
	SetToolSurfaceNext(next http.RoundTripper)
}
