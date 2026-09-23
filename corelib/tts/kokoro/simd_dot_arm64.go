//go:build arm64

package kokoro

//go:noescape
func dotNEON(a, b []float32) float32

// dot32 is the platform vector dot product. On arm64 the pure-Go fallback in
// viterin/vek has no NEON acceleration, so route through the local NEON
// kernel instead. Results may differ from sequential summation in the low
// bits; callers already tolerate that on the amd64 SIMD path.
func dot32(a, b []float32) float32 {
	if useKokoroSIMD() {
		return dotNEON(a, b)
	}
	sum := float32(0)
	for i, v := range a {
		sum += v * b[i]
	}
	return sum
}
