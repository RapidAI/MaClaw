package kokoro

import (
	"math"
	"os"
	"runtime"
	"strings"

	"github.com/viterin/vek/vek32"
)

var kokoroSIMDEnabled = cpuSupportsKokoroSIMD() && !envBool("KOKORO_DISABLE_SIMD")
var kokoroQ8DirectEnabled = envBool("KOKORO_Q8_DIRECT")
var kokoroConvMatMulEnabled = envBool("KOKORO_CONV_MATMUL")

// kokoroBufferPoolEnabled recycles the large vocoder and PL-BERT activations.
// Buffers come back dirty; callers overwrite every element before reading.
// Default on for Android. KOKORO_BUFFER_POOL=0 keeps a fresh make so desktop
// allocation behavior stays put. Short slices are never pooled.
var kokoroBufferPoolEnabled = defaultKokoroBufferPool()

func envBool(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func useKokoroSIMD() bool { return kokoroSIMDEnabled }

// optRound2 gates the round-2 optimizations (LSTM parallelism, conv-transpose
// pairing, vectorized tanh / istft trig) so benchmarks can A/B them in a
// single process. Defaults on; not env-configurable by design.
var optRound2 = true

func setRound2Opts(on bool) { optRound2 = on }

func useKokoroQ8Direct() bool { return kokoroQ8DirectEnabled }

func useKokoroConvMatMul() bool { return kokoroConvMatMulEnabled }

func defaultKokoroBufferPool() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_BUFFER_POOL"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android"
	}
}

func useKokoroBufferPool() bool { return kokoroBufferPoolEnabled }

// kokoroAlbertGEMM scores one PL-BERT head as QK^T and mixes values with a
// second GEMM. The per-token dots stay the desktop path. KOKORO_ALBERT_GEMM=1
// forces the GEMM; =0 keeps the dots. A long sequence lets the GEMM use
// every core, so heads then run one at a time.
var kokoroAlbertGEMM = defaultKokoroAlbertGEMM()

func defaultKokoroAlbertGEMM() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_ALBERT_GEMM"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android"
	}
}

// kokoroSeqGEMM folds a whole phoneme sequence into one GEMM. The old loop
// dotted one token at a time and re-read every weight row per token. Default
// on for the Android voice library. KOKORO_SEQ_GEMM=1 forces it; =0 keeps
// the per-token dots (desktop audio stays on that path).
var kokoroSeqGEMM = defaultKokoroSeqGEMM()

func defaultKokoroSeqGEMM() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_SEQ_GEMM"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android"
	}
}

// kokoroLSTMGEMM projects every phoneme step through weight_ih in one GEMM
// and accumulates weight_hh four gate rows at a time on the vector 4-wide
// dot. Default on for Android. KOKORO_LSTM_GEMM=0 keeps the per-step dots
// so desktop audio is unchanged. Darwin stays on per-row dots even when the
// flag is forced: its 4-wide kernel is scalar Go, slower than dotNEON.
var kokoroLSTMGEMM = defaultKokoroLSTMGEMM()

func defaultKokoroLSTMGEMM() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_LSTM_GEMM"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android"
	}
}

// useKokoroAndroidBlockedFMA selects the NEON 4-channel conv kernels. They
// are Android-only: the same source on darwin arm64 stays on the scalar
// dots, so Mac audio does not move.
func useKokoroAndroidBlockedFMA() bool {
	return runtime.GOOS == "android" && runtime.GOARCH == "arm64" && useKokoroFusedConvASM()
}

// kokoroConvTransposePair evaluates two upsample outputs that share a
// residue class in one 4-wide dot, so the weight rows are loaded once.
// Default on for Android. KOKORO_CONV_TRANSPOSE_PAIR=0 keeps one output
// time per call, which is what desktop audio already uses.
var kokoroConvTransposePair = defaultKokoroConvTransposePair()

func defaultKokoroConvTransposePair() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_CONV_TRANSPOSE_PAIR"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android" && runtime.GOARCH == "arm64"
	}
}

// useKokoroConvTransposePair is the vector pair kernel. Darwin arm64 stays
// on the single-position dot: its 4x2 body is scalar Go.
func useKokoroConvTransposePair() bool {
	if !kokoroConvTransposePair || !useKokoroStridedFMA() {
		return false
	}
	if runtime.GOARCH == "arm64" && !useKokoroAndroidBlockedFMA() {
		return false
	}
	return true
}

// AccelNote is a one-line description of the kernels the voice library is
// actually using, for the Android log.
func AccelNote() string {
	return "neon=" + bool01(useKokoroSIMD()) + " seqgemm=" + bool01(kokoroSeqGEMM) + " pointwise=" + bool01(useKokoroPointwiseGEMM()) + " blocked=" + bool01(useKokoroAndroidBlockedFMA()) + " lstm=" + bool01(kokoroLSTMGEMM) + " tpair=" + bool01(useKokoroConvTransposePair()) + " fast=" + bool01(kokoroFastMath) + " albert=" + bool01(kokoroAlbertGEMM) + " pool=" + bool01(kokoroBufferPoolEnabled)
}

func bool01(v bool) string {
	if v {
		return "1"
	}
	return "0"
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
	if kokoroFastMath {
		fastSinInplace(x)
		return
	}
	if useKokoroSIMD() {
		vek32.Sin_Inplace(x)
		return
	}
	for i, v := range x {
		x[i] = float32(math.Sin(float64(v)))
	}
}

// tanhInplace32 replaces x with tanh(x). The vectorized path uses
// tanh(x) = 1 - 2/(exp(2x)+1); the limits work out for large |x| because
// exp overflows to +Inf (giving 1) and underflows to 0 (giving -1).
func tanhInplace32(x []float32) {
	if kokoroFastMath {
		fastTanhInplace(x)
		return
	}
	if useKokoroSIMD() && optRound2 {
		vek32.MulNumber_Inplace(x, 2)
		vek32.Exp_Inplace(x)
		vek32.AddNumber_Inplace(x, 1)
		vek32.Inv_Inplace(x)
		vek32.MulNumber_Inplace(x, 2)
		vek32.Neg_Inplace(x)
		vek32.AddNumber_Inplace(x, 1)
		return
	}
	for i, v := range x {
		x[i] = tanh(v)
	}
}

func expInto32(dst, x []float32) {
	if kokoroFastMath {
		fastExpInto(dst, x)
		return
	}
	if useKokoroSIMD() {
		vek32.Exp_Into(dst, x)
		return
	}
	for i, v := range x {
		dst[i] = float32(math.Exp(float64(v)))
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
