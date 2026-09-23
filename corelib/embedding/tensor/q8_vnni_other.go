//go:build !amd64

package tensor

import "sync"

// Non-amd64 counterparts for the AVX-512 VNNI panel machinery that the
// platform-neutral q8.go references from prebuiltAFor / q8PrebuiltA /
// q8PrebuiltAPut. Mirrors the q8_multidot_arm64.go / _generic.go split.
//
// There are no AVX-512 kernels off amd64, so every gate is false, every kernel
// reports "not taken" and the panel builders yield nothing. Behaviour is then
// identical to the pre-panel code path, because prebuiltAFor short-circuits on
// hasAVX512VNNI == false and returns nil: callers fall through to the generic
// F32 kernels, and every `len(pre.kXXX) > 0` guard stays false.
//
// Keep this file in sync when new amd64-only panel symbols are added to
// q8z_vnni_amd64.go / q8_multidot_amd64.go and referenced from q8.go.

// No AVX-512 VNNI without amd64 (cpu_other.go exports the public accessor).
var hasAVX512VNNI = false

// Per-layer K=512 VNNI gates — inert without the kernels.
var (
	enableQ8ZK512FFNUp = false
	enableQ8ZK512QKV   = false
	enableQ8ZK512Out   = false
)

// Mirrors the amd64 var (toggled there by SetQ8ZK5124x4ForTest).
var enableQ8ZK5124x4 = false

// Mirrors the amd64 constant.
const enableFusedAccumVNNI = false

// Panel storage referenced only by q8PrebuiltA (q8.go). Empty here: the panels
// are never populated, so the q8PrebuiltA slices stay at len 0.
type q8APanel8K512Z struct{}

type q8APanel4K512Z struct{}

type q8APanel8 struct{}

var (
	q8APanel8K512ZPool = sync.Pool{New: func() any { return new(q8APanel8K512Z) }}
	q8APanel4K512ZPool = sync.Pool{New: func() any { return new(q8APanel4K512Z) }}
	q8APanelPool       = sync.Pool{New: func() any { return new(q8APanel8) }}
)

// Panel builders yield nothing off amd64.
func q8K512ZPanelsFor(a []float32, M int) []*q8APanel8K512Z { return nil }

func q8K512ZPanels4For(a []float32, M int) []*q8APanel4K512Z { return nil }

func q8K2048PanelsFor(a []float32, M int) []*q8APanel8 { return nil }

// PrewarmQ8Stripped builds the scale-stripped VNNI sidecar at model load so the
// first frame does not pay the copy inline. Off amd64 there is no VNNI path to
// feed, so this is a no-op (see corelib/asr, which calls it on every Q8 weight).
func PrewarmQ8Stripped(q *Q8Tensor) {}

// Kernels are amd64-only; report "not taken" so the F32 fallbacks run.
func tryFusedK2048ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, ns, ne int) bool {
	return false
}

func fusedAccumVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, ns, ne, nBlocks int, panels []*q8APanel8) bool {
	return false
}

func fusedK512ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, panels []*q8APanel8K512Z) bool {
	return false
}

func tryFusedK512ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, pre *q8PrebuiltA) bool {
	return false
}

func tryFusedK512RowVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool) bool {
	return false
}
