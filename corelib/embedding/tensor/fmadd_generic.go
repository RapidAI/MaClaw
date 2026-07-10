//go:build !amd64

package tensor

func fmaddInto(out, a, b []float32) {
	fmaddIntoScalar(out, a, b)
}

func fmaddPlusOneInto(out, a, b []float32) {
	fmaddPlusOneIntoScalar(out, a, b)
}

func mul2Into(o0, o1, a0, a1, b []float32) {
	mul2IntoScalar(o0, o1, a0, a1, b)
}

func fmadd2Into(o0, o1, a0, a1, b []float32) {
	fmadd2IntoScalar(o0, o1, a0, a1, b)
}

func fmaddPlusOne2Into(o0, o1, a0, a1, b []float32) {
	fmaddPlusOne2IntoScalar(o0, o1, a0, a1, b)
}

func mul4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	mul4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

func fmadd4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	fmadd4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

func fmaddPlusOne4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	fmaddPlusOne4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

// AddInto: out[i] += a[i].
func AddInto(out, a []float32) {
	n := len(out)
	if n > len(a) {
		n = len(a)
	}
	for i := 0; i < n; i++ {
		out[i] += a[i]
	}
}

// Add2Into: out[i] += a[i] + b[i].
func Add2Into(out, a, b []float32) {
	n := len(out)
	if n > len(a) {
		n = len(a)
	}
	if n > len(b) {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		out[i] += a[i] + b[i]
	}
}

// Copy128 copies exactly 128 float32s.
func Copy128(dst, src []float32) {
	if len(dst) < 128 || len(src) < 128 {
		return
	}
	copy(dst[:128], src[:128])
}

// Copy128Mul writes dst[i] = src[i]*scale for 128 floats.
func Copy128Mul(dst, src []float32, scale float32) {
	if len(dst) < 128 || len(src) < 128 {
		return
	}
	if scale == 1 {
		Copy128(dst, src)
		return
	}
	for i := 0; i < 128; i++ {
		dst[i] = src[i] * scale
	}
}

// PackQKV128 packs one head (generic fallback).
func PackQKV128(dstQ, dstK, dstV, srcQ, srcK, srcV []float32, scale float32) {
	Copy128Mul(dstQ, srcQ, scale)
	Copy128(dstK, srcK)
	Copy128(dstV, srcV)
}

func wsumBatched4Add128(o0, o1, o2, o3, v []float32, w0, w1, w2, w3 float32) {
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] += w0 * vi
		o1[i] += w1 * vi
		o2[i] += w2 * vi
		o3[i] += w3 * vi
	}
}

func wsumBatched4Add128Dual(o0, o1, o2, o3, va, vb []float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32) {
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] += wa0*a + wb0*b
		o1[i] += wa1*a + wb1*b
		o2[i] += wa2*a + wb2*b
		o3[i] += wa3*a + wb3*b
	}
}

func wsumBatched4SetAdd128Dual(o0, o1, o2, o3, va, vb []float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32) {
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] = wa0*a + wb0*b
		o1[i] = wa1*a + wb1*b
		o2[i] = wa2*a + wb2*b
		o3[i] = wa3*a + wb3*b
	}
}

func wsumBatched8Add128Dual(o0, o1, o2, o3, o4, o5, o6, o7, va, vb []float32, wa, wb *[8]float32) {
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] += wa[0]*a + wb[0]*b
		o1[i] += wa[1]*a + wb[1]*b
		o2[i] += wa[2]*a + wb[2]*b
		o3[i] += wa[3]*a + wb[3]*b
		o4[i] += wa[4]*a + wb[4]*b
		o5[i] += wa[5]*a + wb[5]*b
		o6[i] += wa[6]*a + wb[6]*b
		o7[i] += wa[7]*a + wb[7]*b
	}
}

func wsumBatched8SetAdd128Dual(o0, o1, o2, o3, o4, o5, o6, o7, va, vb []float32, wa, wb *[8]float32) {
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] = wa[0]*a + wb[0]*b
		o1[i] = wa[1]*a + wb[1]*b
		o2[i] = wa[2]*a + wb[2]*b
		o3[i] = wa[3]*a + wb[3]*b
		o4[i] = wa[4]*a + wb[4]*b
		o5[i] = wa[5]*a + wb[5]*b
		o6[i] = wa[6]*a + wb[6]*b
		o7[i] = wa[7]*a + wb[7]*b
	}
}

func wsumBatched4Set128(o0, o1, o2, o3, v []float32, w0, w1, w2, w3 float32) {
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] = w0 * vi
		o1[i] = w1 * vi
		o2[i] = w2 * vi
		o3[i] = w3 * vi
	}
}

func wsumBatched8Add128(o0, o1, o2, o3, o4, o5, o6, o7, v []float32, w0, w1, w2, w3, w4, w5, w6, w7 float32) {
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] += w0 * vi
		o1[i] += w1 * vi
		o2[i] += w2 * vi
		o3[i] += w3 * vi
		o4[i] += w4 * vi
		o5[i] += w5 * vi
		o6[i] += w6 * vi
		o7[i] += w7 * vi
	}
}

func wsumBatched8Set128(o0, o1, o2, o3, o4, o5, o6, o7, v []float32, w0, w1, w2, w3, w4, w5, w6, w7 float32) {
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] = w0 * vi
		o1[i] = w1 * vi
		o2[i] = w2 * vi
		o3[i] = w3 * vi
		o4[i] = w4 * vi
		o5[i] = w5 * vi
		o6[i] = w6 * vi
		o7[i] = w7 * vi
	}
}
