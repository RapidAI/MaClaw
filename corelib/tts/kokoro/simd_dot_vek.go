//go:build !arm64

package kokoro

import "github.com/viterin/vek/vek32"

func dot32(a, b []float32) float32 {
	if useKokoroSIMD() {
		return vek32.Dot(a, b)
	}
	sum := float32(0)
	for i, v := range a {
		sum += v * b[i]
	}
	return sum
}
