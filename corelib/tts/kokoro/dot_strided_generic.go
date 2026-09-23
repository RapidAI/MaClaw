//go:build !amd64 && !arm64 && !arm64

package kokoro

func useKokoroStridedFMA() bool { return false }

func leakyReLUInplace(x []float32, slope float32) {
	for i, v := range x {
		x[i] = leakyReLU(v, slope)
	}
}

func normScaleInto(dst, x []float32, mean, scale, beta float32) {
	for i, v := range x {
		dst[i] = (v-mean)*scale + beta
	}
}

func axpyInplace(dst, x []float32, a float32) {
	for i, v := range x {
		dst[i] += a * v
	}
}
