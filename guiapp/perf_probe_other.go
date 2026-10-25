//go:build !windows

package guiapp

// The process-tree sampler is Windows-only: WebView2 is the renderer there.
// Other platforms report nothing rather than approximating with a different
// process model, so a comparison never mixes two measurement methods.

const perfSampleInterval = 30 * 0 // unused on this platform

func startPerfSampler() {}
