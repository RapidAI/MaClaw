package cosy

import _ "embed"

// qoderAuthWASM is Qoder's qoder_auth_wasm_bg.wasm, extracted verbatim from the
// official qodercli-cn 1.1.64 Windows build (sha256 of the module bytes is
// asserted in runtime_test.go so a stale copy cannot ship silently).
//
//go:embed wasm/qoder_auth_wasm_bg.wasm
var qoderAuthWASM []byte
