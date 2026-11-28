package kokoro

import (
	"math"
	"os"
	"runtime"
	"strings"
)

// kokoroFastMath replaces libm sin, exp, and tanh on the vocoder snake,
// the iSTFT polar conversion, the source STFT magnitude and phase, the
// LSTM gates, PL-BERT GELU, and attention softmax. Default on for Android.
// KOKORO_FAST_MATH=0 keeps vek and math so desktop audio stays on libm.
var kokoroFastMath = defaultKokoroFastMath()

func defaultKokoroFastMath() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("KOKORO_FAST_MATH"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	default:
		return runtime.GOOS == "android"
	}
}

const (
	fastInvPio2 = 6.36619772367581382433e-01
	fastPio2Hi  = 1.57079631090164184570e+00
	fastPio2Lo  = 1.58932547735281966916e-08
	fastRound   = 6755399441055744.0 // 1.5 * 2^52, rounds to nearest even
	fastInvLn2  = 1.4426950408889634074
)

func fastSin32(x float32) float32 {
	if x != x || x > 1e8 || x < -1e8 {
		return float32(math.Sin(float64(x)))
	}
	xd := float64(x)
	fn := (xd*fastInvPio2 + fastRound) - fastRound
	r := xd - fn*fastPio2Hi - fn*fastPio2Lo
	if r > 1 || r < -1 {
		return float32(math.Sin(float64(x)))
	}
	s, c := fastSinCosReduced(r)
	switch int32(fn) & 3 {
	case 0:
		return float32(s)
	case 1:
		return float32(c)
	case 2:
		return float32(-s)
	default:
		return float32(-c)
	}
}

func fastSinCosReduced(r float64) (sinR, cosR float64) {
	z := r * r
	s := 1.58969099521155010221e-10
	s = s*z - 2.50507602534068634195e-08
	s = s*z + 2.75573137070700676789e-06
	s = s*z - 1.98412698298579493134e-04
	s = s*z + 8.33333333332248946124e-03
	s = s*z - 1.66666666666666324348e-01
	sinR = r + r*z*s

	c := -1.13596475577881948265e-11
	c = c*z + 2.08757232129817482790e-09
	c = c*z - 2.75573143513906633035e-07
	c = c*z + 2.48015872894767294178e-05
	c = c*z - 1.38888888888741095749e-03
	c = c*z + 4.16666666666666019037e-02
	cosR = 1 - 0.5*z + z*z*c
	return sinR, cosR
}

// fastSinCos32 is the same Payne-Hanek reduction as fastSin32, keeping
// the cosine. istft uses it once per bin. |x|>1e8 or a failed reduction
// falls back to libm for both values.
func fastSinCos32(x float32) (sinX, cosX float32) {
	if x != x || x > 1e8 || x < -1e8 {
		xd := float64(x)
		return float32(math.Sin(xd)), float32(math.Cos(xd))
	}
	xd := float64(x)
	fn := (xd*fastInvPio2 + fastRound) - fastRound
	r := xd - fn*fastPio2Hi - fn*fastPio2Lo
	if r > 1 || r < -1 {
		return float32(math.Sin(xd)), float32(math.Cos(xd))
	}
	s, c := fastSinCosReduced(r)
	switch int32(fn) & 3 {
	case 0:
		return float32(s), float32(c)
	case 1:
		return float32(c), float32(-s)
	case 2:
		return float32(-s), float32(-c)
	default:
		return float32(-c), float32(s)
	}
}

func fastSinInplace(x []float32) {
	for i, v := range x {
		x[i] = fastSin32(v)
	}
}

func fastExp64(x float64) float64 {
	if x != x {
		return x
	}
	if x > 709 {
		return math.Inf(1)
	}
	if x < -745 {
		return 0
	}
	fn := (x*fastInvLn2 + fastRound) - fastRound
	r := x - fn*math.Ln2
	p := 1.0 / 40320
	p = p*r + 1.0/5040
	p = p*r + 1.0/720
	p = p*r + 1.0/120
	p = p*r + 1.0/24
	p = p*r + 1.0/6
	p = p*r + 0.5
	p = p*r + 1
	p = p*r + 1
	return ldexp64(p, int32(fn))
}

func fastExp32(x float32) float32 {
	if x != x {
		return x
	}
	if x > 88.722839 {
		return float32(math.Inf(1))
	}
	if x < -103.97208 {
		return 0
	}
	return float32(fastExp64(float64(x)))
}

func fastExpInto(dst, x []float32) {
	for i, v := range x {
		dst[i] = fastExp32(v)
	}
}

func ldexp64(p float64, n int32) float64 {
	if p == 0 || p != p {
		return p
	}
	bits := math.Float64bits(p)
	exp := int32(bits>>52)&0x7ff + n
	if exp <= 0 {
		return 0
	}
	if exp >= 0x7ff {
		if p < 0 {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	bits = (bits &^ (uint64(0x7ff) << 52)) | uint64(exp)<<52
	return math.Float64frombits(bits)
}

func fastTanh32(x float32) float32 {
	if x != x {
		return x
	}
	if x > 9 {
		return 1
	}
	if x < -9 {
		return -1
	}
	e := fastExp64(-2 * float64(x))
	return float32((1 - e) / (1 + e))
}

func fastTanhInplace(x []float32) {
	for i, v := range x {
		x[i] = fastTanh32(v)
	}
}

func fastSigmoid32(x float32) float32 {
	if x != x {
		return x
	}
	if x > 16 {
		return 1
	}
	if x < -16 {
		return 0
	}
	return float32(1 / (1 + fastExp64(-float64(x))))
}

// fastGelu32 is the tanh GELU approximation. The cubic matches gelu's
// float32 argument; |inner|>=9 saturates inside fastTanh32.
func fastGelu32(x float32) float32 {
	if x != x {
		return x
	}
	inner := float32(0.7978845608028654) * (x + float32(0.044715)*x*x*x)
	return 0.5 * x * (1 + fastTanh32(inner))
}

// fdlibm atanf coefficients. The hex bits keep the published float32 values.
var (
	fastAtanHi = [4]float32{
		math.Float32frombits(0x3eed6338),
		math.Float32frombits(0x3f490fda),
		math.Float32frombits(0x3f7b985e),
		math.Float32frombits(0x3fc90fda),
	}
	fastAtanLo = [4]float32{
		math.Float32frombits(0x31ac3769),
		math.Float32frombits(0x33222168),
		math.Float32frombits(0x33140fb4),
		math.Float32frombits(0x33a22168),
	}
	fastAtanPT = [11]float32{
		math.Float32frombits(0x3eaaaaaa),
		math.Float32frombits(0xbe4ccccd),
		math.Float32frombits(0x3e124925),
		math.Float32frombits(0xbde38e38),
		math.Float32frombits(0x3dba2e6e),
		math.Float32frombits(0xbd9d8795),
		math.Float32frombits(0x3d886b35),
		math.Float32frombits(0xbd6ef16b),
		math.Float32frombits(0x3d4bda59),
		math.Float32frombits(0xbd15a221),
		math.Float32frombits(0x3c8569d7),
	}
)

func fastAtanPoly(x float32) (s1, s2 float32) {
	z := x * x
	w := z * z
	p := fastAtanPT
	s1 = z * (p[0] + w*(p[2]+w*(p[4]+w*(p[6]+w*(p[8]+w*p[10])))))
	s2 = w * (p[1] + w*(p[3]+w*(p[5]+w*(p[7]+w*p[9]))))
	return s1, s2
}

// fastAtan32 is fdlibm atanf. |x|>=2^34 saturates at ±pi/2.
func fastAtan32(x float32) float32 {
	if x != x {
		return x
	}
	hx := math.Float32bits(x)
	ix := hx & 0x7fffffff
	if ix >= 0x50800000 {
		z := fastAtanHi[3] + fastAtanLo[3]
		if hx&0x80000000 != 0 {
			return -z
		}
		return z
	}
	if ix < 0x3ee00000 {
		if ix < 0x31000000 {
			return x
		}
		s1, s2 := fastAtanPoly(x)
		return x - x*(s1+s2)
	}
	ax := math.Float32frombits(ix)
	var id int
	switch {
	case ix < 0x3f980000:
		if ix < 0x3f300000 {
			id = 0
			ax = (2*ax - 1) / (2 + ax)
		} else {
			id = 1
			ax = (ax - 1) / (ax + 1)
		}
	case ix < 0x401c0000:
		id = 2
		ax = (ax - 1.5) / (1 + 1.5*ax)
	default:
		id = 3
		ax = -1 / ax
	}
	s1, s2 := fastAtanPoly(ax)
	z := fastAtanHi[id] - ((ax*(s1+s2) - fastAtanLo[id]) - ax)
	if hx&0x80000000 != 0 {
		return -z
	}
	return z
}

// fastAtan232 is atan2 for finite nonzero inputs. Zeros and non-finite
// values stay on libm so the branch cut matches math.Atan2.
func fastAtan232(y, x float32) float32 {
	if y != y || x != x || x == 0 || y == 0 || y > 1e30 || y < -1e30 || x > 1e30 || x < -1e30 {
		return float32(math.Atan2(float64(y), float64(x)))
	}
	ay := y
	if ay < 0 {
		ay = -ay
	}
	ax := x
	if ax < 0 {
		ax = -ax
	}
	a := fastAtan32(ay / ax)
	if x < 0 {
		a = float32(math.Pi) - a
	}
	if y < 0 {
		a = -a
	}
	return a
}

// fastHypot32 is hypot for ordinary magnitudes. Huge or non-finite inputs
// fall back to libm, which scales before squaring.
func fastHypot32(x, y float32) float32 {
	if x != x || y != y {
		return float32(math.NaN())
	}
	ax := x
	if ax < 0 {
		ax = -ax
	}
	ay := y
	if ay < 0 {
		ay = -ay
	}
	if ax < ay {
		ax, ay = ay, ax
	}
	if ax == 0 {
		return 0
	}
	if ax > 1e16 {
		return float32(math.Hypot(float64(x), float64(y)))
	}
	r := ay / ax
	return ax * float32(math.Sqrt(float64(1)+float64(r)*float64(r)))
}

func fastLSTMStep(gates, c, h, out []float32) {
	hidden := len(h)
	for i := 0; i < hidden; i++ {
		ig := fastSigmoid32(gates[i])
		fg := fastSigmoid32(gates[hidden+i])
		gg := fastTanh32(gates[2*hidden+i])
		og := fastSigmoid32(gates[3*hidden+i])
		c[i] = fg*c[i] + ig*gg
		th := fastTanh32(c[i])
		h[i] = og * th
		out[i] = h[i]
	}
}
