//go:build windows && amd64

// embed_npu_windows.go — wires the AMD XDNA NPU provider into the GUI build.
//
// The provider lives in corelib/embedding (npu_amd_*_windows.go) and registers
// itself there, so this file only has to exist for the doc trail: the GUI is
// the build that wants the NPU in the device chain, and the embedding library
// must not reach for a vendor driver without a host asking.
//
// Cost when no NPU is present: detectNPU answers from the device tree, so a
// machine without an XDNA device never loads RadeonML_ipu.dll at all.
//
// Opting out at runtime: MACLAW_ACCEL=off (HWAccelPreferred) keeps the chain on
// CPU/GPU even when a compiled .amodel is present.

package guiapp

import "github.com/RapidAI/CodeClaw/corelib/embedding"

func init() {
	// Idempotent: the embedding package already registered the factory, and
	// re-registering also installs the strict prober that reports "ready" only
	// when a compiled .amodel is actually on disk.
	embedding.RegisterAMDNPUProvider()
}
