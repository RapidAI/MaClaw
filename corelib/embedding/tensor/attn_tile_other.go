//go:build !arm64

package tensor

const attnTile4Avail = false

func attnQK4x4(out []float32, ldS int, q []float32, qStride int, k []float32, kStride, nblk int, scale float32) {
	panic("attnQK4x4: arm64 only")
}
func attnPV4(out []float32, outStride int, pt, v []float32, vStride, n int) { panic("arm64 only") }
func softmaxExpSum(x []float32, max float32) float32                        { panic("arm64 only") }
