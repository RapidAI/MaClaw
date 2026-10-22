package onnxrt

import "github.com/RapidAI/CodeClaw/corelib/modelmanager"

// Every model host that uses onnxrt drops its scratch arena whenever
// modelmanager.ReleaseMemory runs (after inference or model unload).
func init() { modelmanager.RegisterReleaser(ReleaseScratch) }
