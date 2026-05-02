package kokoro

import (
	"math"
	"os"
	"strings"

	"github.com/viterin/vek/vek32"
)

var kokoroSIMDEnabled = cpuSupportsKokoroSIMD() && !envBool("KOKORO_DISABLE_SIMD")
var kokoroQ8DirectEnabled = envBool("KOKORO_Q8_DIRECT")
var kokoroConvMatMulEnabled = envBool("KOKORO_CONV_MATMUL")
var kokoroBufferPoolEnabled = envBool("KOKORO_BUFFER_POOL")

func envBool(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func useKokoroSIMD() bool { return kokoroSIMDEnabled }

func useKokoroQ8Direct() bool { return kokoroQ8DirectEnabled }

func useKokoroConvMatMul() bool { return kokoroConvMatMulEnabled }

func useKokoroBufferPool() bool { return kokoroBufferPoolEnabled }

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

func sum32(x []float32) float32 {
	if useKokoroSIMD() {
		return vek32.Sum(x)
	}
	sum := float32(0)
	for _, v := range x {
		sum += v
	}
	return sum
}

func addInto32(dst, a, b []float32) {
	if useKokoroSIMD() {
		vek32.Add_Into(dst, a, b)
		return
	}
	for i, v := range a {
		dst[i] = v + b[i]
	}
}

func addInplace32(dst, x []float32) {
	if useKokoroSIMD() {
		vek32.Add_Inplace(dst, x)
		return
	}
	for i, v := range x {
		dst[i] += v
	}
}

func mulNumberInplace32(x []float32, a float32) {
	if useKokoroSIMD() {
		vek32.MulNumber_Inplace(x, a)
		return
	}
	for i := range x {
		x[i] *= a
	}
}

func mulNumberInto32(dst, x []float32, a float32) {
	if useKokoroSIMD() {
		vek32.MulNumber_Into(dst, x, a)
		return
	}
	for i, v := range x {
		dst[i] = v * a
	}
}

func sinInplace32(x []float32) {
	if useKokoroSIMD() {
		vek32.Sin_Inplace(x)
		return
	}
	for i, v := range x {
		x[i] = float32(math.Sin(float64(v)))
	}
}

func matMulInto32(dst, x, y []float32, n int) {
	if useKokoroSIMD() {
		vek32.MatMul_Into(dst, x, y, n)
		return
	}
	m := len(x) / n
	p := len(y) / n
	for i := 0; i < m; i++ {
		for j := 0; j < p; j++ {
			var sum float32
			for k := 0; k < n; k++ {
				sum += x[i*n+k] * y[k*p+j]
			}
			dst[i*p+j] = sum
		}
	}
}
