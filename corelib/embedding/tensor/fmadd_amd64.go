//go:build amd64

package tensor

func fmaddInto(out, a, b []float32) {
	n := len(out)
	if hasAVX2andFMA && n >= 8 {
		// Process aligned 8-wide body in asm; scalar tail.
		body := n &^ 7
		if body > 0 {
			fmaddIntoAVX2(&out[0], &a[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			out[i] += a[i] * b[i]
		}
		return
	}
	fmaddIntoScalar(out, a, b)
}

func fmaddPlusOneInto(out, a, b []float32) {
	n := len(out)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			fmaddPlusOneIntoAVX2(&out[0], &a[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			out[i] += a[i]*b[i] + a[i]
		}
		return
	}
	fmaddPlusOneIntoScalar(out, a, b)
}

// mul2Into: o_r[i] = a_r[i]*b[i] for r=0..1; loads b once per SIMD chunk.
func mul2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			mul2IntoAVX2(&o0[0], &o1[0], &a0[0], &a1[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bi := b[i]
			o0[i] = a0[i] * bi
			o1[i] = a1[i] * bi
		}
		return
	}
	mul2IntoScalar(o0, o1, a0, a1, b)
}

func fmadd2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			fmadd2IntoAVX2(&o0[0], &o1[0], &a0[0], &a1[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bi := b[i]
			o0[i] += a0[i] * bi
			o1[i] += a1[i] * bi
		}
		return
	}
	fmadd2IntoScalar(o0, o1, a0, a1, b)
}

func fmaddPlusOne2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			fmaddPlusOne2IntoAVX2(&o0[0], &o1[0], &a0[0], &a1[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bp1 := b[i] + 1
			o0[i] += a0[i] * bp1
			o1[i] += a1[i] * bp1
		}
		return
	}
	fmaddPlusOne2IntoScalar(o0, o1, a0, a1, b)
}

//go:noescape
func mul2IntoAVX2(o0, o1, a0, a1, b *float32, n int)

//go:noescape
func fmadd2IntoAVX2(o0, o1, a0, a1, b *float32, n int)

//go:noescape
func fmaddPlusOne2IntoAVX2(o0, o1, a0, a1, b *float32, n int)

// mul4Into: o_r[i] = a_r[i]*b[i] for r=0..3; loads b once per SIMD chunk.
func mul4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			mul4IntoAVX2(&o0[0], &o1[0], &o2[0], &o3[0], &a0[0], &a1[0], &a2[0], &a3[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bi := b[i]
			o0[i] = a0[i] * bi
			o1[i] = a1[i] * bi
			o2[i] = a2[i] * bi
			o3[i] = a3[i] * bi
		}
		return
	}
	mul4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

// fmadd4Into: o_r[i] += a_r[i]*b[i] for r=0..3; loads b once per SIMD chunk.
func fmadd4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			fmadd4IntoAVX2(&o0[0], &o1[0], &o2[0], &o3[0], &a0[0], &a1[0], &a2[0], &a3[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bi := b[i]
			o0[i] += a0[i] * bi
			o1[i] += a1[i] * bi
			o2[i] += a2[i] * bi
			o3[i] += a3[i] * bi
		}
		return
	}
	fmadd4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

// fmaddPlusOne4Into: o_r[i] += a_r[i]*(b[i]+1) for r=0..3.
func fmaddPlusOne4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if hasAVX2andFMA && n >= 8 {
		body := n &^ 7
		if body > 0 {
			fmaddPlusOne4IntoAVX2(&o0[0], &o1[0], &o2[0], &o3[0], &a0[0], &a1[0], &a2[0], &a3[0], &b[0], body)
		}
		for i := body; i < n; i++ {
			bp1 := b[i] + 1
			o0[i] += a0[i] * bp1
			o1[i] += a1[i] * bp1
			o2[i] += a2[i] * bp1
			o3[i] += a3[i] * bp1
		}
		return
	}
	fmaddPlusOne4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b)
}

//go:noescape
func fmaddIntoAVX2(out, a, b *float32, n int)

//go:noescape
func fmaddPlusOneIntoAVX2(out, a, b *float32, n int)

//go:noescape
func mul4IntoAVX2(o0, o1, o2, o3, a0, a1, a2, a3, b *float32, n int)

//go:noescape
func fmadd4IntoAVX2(o0, o1, o2, o3, a0, a1, a2, a3, b *float32, n int)

//go:noescape
func fmaddPlusOne4IntoAVX2(o0, o1, o2, o3, a0, a1, a2, a3, b *float32, n int)

//go:noescape
func copy128AVX2(dst, src *float32)

//go:noescape
func copy128MulAVX2(dst, src *float32, scale float32)

//go:noescape
func packQKV128AVX2(dstQ, dstK, dstV, srcQ, srcK, srcV *float32, scale float32)

//go:noescape
func addIntoAVX2(out, a *float32, n int)

//go:noescape
func add2IntoAVX2(out, a, b *float32, n int)

// AddInto: out[i] += a[i].
func AddInto(out, a []float32) {
	n := len(out)
	if n > len(a) {
		n = len(a)
	}
	if n == 0 {
		return
	}
	body := n &^ 15
	if hasAVX2andFMA && body >= 16 {
		addIntoAVX2(&out[0], &a[0], body)
		for i := body; i < n; i++ {
			out[i] += a[i]
		}
		return
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
	if n == 0 {
		return
	}
	body := n &^ 15
	if hasAVX2andFMA && body >= 16 {
		add2IntoAVX2(&out[0], &a[0], &b[0], body)
		for i := body; i < n; i++ {
			out[i] += a[i] + b[i]
		}
		return
	}
	for i := 0; i < n; i++ {
		out[i] += a[i] + b[i]
	}
}

// Copy128 copies exactly 128 float32s (SenseVoice headDim) via AVX2 when available.
func Copy128(dst, src []float32) {
	if len(dst) < 128 || len(src) < 128 {
		return
	}
	if hasAVX2andFMA {
		copy128AVX2(&dst[0], &src[0])
		return
	}
	copy(dst[:128], src[:128])
}

// Copy128Mul writes dst[i] = src[i]*scale for 128 floats (Q pack + attention scale).
func Copy128Mul(dst, src []float32, scale float32) {
	if len(dst) < 128 || len(src) < 128 {
		return
	}
	if scale == 1 {
		Copy128(dst, src)
		return
	}
	if hasAVX2andFMA {
		copy128MulAVX2(&dst[0], &src[0], scale)
		return
	}
	for i := 0; i < 128; i++ {
		dst[i] = src[i] * scale
	}
}

// PackQKV128 packs one head: dstQ = srcQ*scale, dstK = srcK, dstV = srcV.
// Fused micro-kernel for SenseVoice attention pack (headDim=128).
func PackQKV128(dstQ, dstK, dstV, srcQ, srcK, srcV []float32, scale float32) {
	if len(dstQ) < 128 || len(dstK) < 128 || len(dstV) < 128 ||
		len(srcQ) < 128 || len(srcK) < 128 || len(srcV) < 128 {
		return
	}
	if hasAVX2andFMA {
		packQKV128AVX2(&dstQ[0], &dstK[0], &dstV[0], &srcQ[0], &srcK[0], &srcV[0], scale)
		return
	}
	Copy128Mul(dstQ, srcQ, scale)
	Copy128(dstK, srcK)
	Copy128(dstV, srcV)
}

//go:noescape
func wsumBatched4Add128AVX2(o0, o1, o2, o3, v *float32, w0, w1, w2, w3 float32)

//go:noescape
func wsumBatched8Add128AVX2(o0, o1, o2, o3, o4, o5, o6, o7, v *float32, w0, w1, w2, w3, w4, w5, w6, w7 float32)

//go:noescape
func wsumBatched4Add128DualAVX2(o0, o1, o2, o3, va, vb *float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32)

//go:noescape
func wsumBatched4SetAdd128DualAVX2(o0, o1, o2, o3, va, vb *float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32)

//go:noescape
func wsumBatched8Add128DualAVX2(o0, o1, o2, o3, o4, o5, o6, o7, va, vb, wa, wb *float32)

//go:noescape
func wsumBatched8SetAdd128DualAVX2(o0, o1, o2, o3, o4, o5, o6, o7, va, vb, wa, wb *float32)

func wsumBatched4Add128(o0, o1, o2, o3, v []float32, w0, w1, w2, w3 float32) {
	if hasAVX2andFMA && len(v) >= 128 && len(o0) >= 128 {
		wsumBatched4Add128AVX2(&o0[0], &o1[0], &o2[0], &o3[0], &v[0], w0, w1, w2, w3)
		return
	}
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] += w0 * vi
		o1[i] += w1 * vi
		o2[i] += w2 * vi
		o3[i] += w3 * vi
	}
}

// wsumBatched4Add128Dual: out_t += wa_t*va + wb_t*vb (two V rows, one pass).
func wsumBatched4Add128Dual(o0, o1, o2, o3, va, vb []float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32) {
	if hasAVX2andFMA && len(va) >= 128 && len(vb) >= 128 && len(o0) >= 128 {
		wsumBatched4Add128DualAVX2(&o0[0], &o1[0], &o2[0], &o3[0], &va[0], &vb[0],
			wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3)
		return
	}
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] += wa0*a + wb0*b
		o1[i] += wa1*a + wb1*b
		o2[i] += wa2*a + wb2*b
		o3[i] += wa3*a + wb3*b
	}
}

// wsumBatched4SetAdd128Dual: out_t = wa_t*va + wb_t*vb (seed two V rows).
func wsumBatched4SetAdd128Dual(o0, o1, o2, o3, va, vb []float32, wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3 float32) {
	if hasAVX2andFMA && len(va) >= 128 && len(vb) >= 128 && len(o0) >= 128 {
		wsumBatched4SetAdd128DualAVX2(&o0[0], &o1[0], &o2[0], &o3[0], &va[0], &vb[0],
			wa0, wa1, wa2, wa3, wb0, wb1, wb2, wb3)
		return
	}
	for i := 0; i < 128; i++ {
		a, b := va[i], vb[i]
		o0[i] = wa0*a + wb0*b
		o1[i] = wa1*a + wb1*b
		o2[i] = wa2*a + wb2*b
		o3[i] = wa3*a + wb3*b
	}
}

// wsumBatched8Add128Dual: out_t += wa[t]*va + wb[t]*vb.
func wsumBatched8Add128Dual(o0, o1, o2, o3, o4, o5, o6, o7, va, vb []float32, wa, wb *[8]float32) {
	if hasAVX2andFMA && len(va) >= 128 && len(vb) >= 128 && len(o0) >= 128 {
		wsumBatched8Add128DualAVX2(
			&o0[0], &o1[0], &o2[0], &o3[0], &o4[0], &o5[0], &o6[0], &o7[0],
			&va[0], &vb[0], &wa[0], &wb[0],
		)
		return
	}
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

// wsumBatched8SetAdd128Dual: out_t = wa[t]*va + wb[t]*vb (seed).
func wsumBatched8SetAdd128Dual(o0, o1, o2, o3, o4, o5, o6, o7, va, vb []float32, wa, wb *[8]float32) {
	if hasAVX2andFMA && len(va) >= 128 && len(vb) >= 128 && len(o0) >= 128 {
		wsumBatched8SetAdd128DualAVX2(
			&o0[0], &o1[0], &o2[0], &o3[0], &o4[0], &o5[0], &o6[0], &o7[0],
			&va[0], &vb[0], &wa[0], &wb[0],
		)
		return
	}
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
	if hasAVX2andFMA && len(v) >= 128 && len(o0) >= 128 {
		wsumBatched4Set128AVX2(&o0[0], &o1[0], &o2[0], &o3[0], &v[0], w0, w1, w2, w3)
		return
	}
	for i := 0; i < 128; i++ {
		vi := v[i]
		o0[i] = w0 * vi
		o1[i] = w1 * vi
		o2[i] = w2 * vi
		o3[i] = w3 * vi
	}
}

func wsumBatched8Add128(o0, o1, o2, o3, o4, o5, o6, o7, v []float32, w0, w1, w2, w3, w4, w5, w6, w7 float32) {
	if hasAVX2andFMA && len(v) >= 128 && len(o0) >= 128 {
		wsumBatched8Add128AVX2(
			&o0[0], &o1[0], &o2[0], &o3[0], &o4[0], &o5[0], &o6[0], &o7[0],
			&v[0], w0, w1, w2, w3, w4, w5, w6, w7,
		)
		return
	}
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
	if hasAVX2andFMA && len(v) >= 128 && len(o0) >= 128 {
		wsumBatched8Set128AVX2(
			&o0[0], &o1[0], &o2[0], &o3[0], &o4[0], &o5[0], &o6[0], &o7[0],
			&v[0], w0, w1, w2, w3, w4, w5, w6, w7,
		)
		return
	}
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

//go:noescape
func wsumBatched4Set128AVX2(o0, o1, o2, o3, v *float32, w0, w1, w2, w3 float32)

//go:noescape
func wsumBatched8Set128AVX2(o0, o1, o2, o3, o4, o5, o6, o7, v *float32, w0, w1, w2, w3, w4, w5, w6, w7 float32)
