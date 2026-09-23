//go:build !amd64

package kokoro

// dotStridedFMA4x2 computes four output channels at two adjacent output
// positions. Position 1 starts n elements (one row) after position 0;
// x rows are shared across the four channels and weight rows across the two
// positions. Pure-Go fallback for non-amd64 platforms; see
// dot_strided_amd64.s for the AVX2+FMA implementation.
func dotStridedFMA4x2(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3, r4, r5, r6, r7 float32) {
	for t := 0; t < taps; t++ {
		xo := t * xStride
		wo := t * wStride
		for i := 0; i < n; i++ {
			xa := x[xo+i]
			xb := x[xo+n+i]
			r0 += xa * w[wo+i]
			r1 += xb * w[wo+i]
			r2 += xa * w[wo+wOcStride+i]
			r3 += xb * w[wo+wOcStride+i]
			r4 += xa * w[wo+2*wOcStride+i]
			r5 += xb * w[wo+2*wOcStride+i]
			r6 += xa * w[wo+3*wOcStride+i]
			r7 += xb * w[wo+3*wOcStride+i]
		}
	}
	return r0, r1, r2, r3, r4, r5, r6, r7
}

// dotStridedFMA4 computes one output position for four output channels,
// sharing each x row across all four weight rows.
func dotStridedFMA4(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3 float32) {
	for t := 0; t < taps; t++ {
		xo := t * xStride
		wo := t * wStride
		for i := 0; i < n; i++ {
			xv := x[xo+i]
			r0 += xv * w[wo+i]
			r1 += xv * w[wo+wOcStride+i]
			r2 += xv * w[wo+2*wOcStride+i]
			r3 += xv * w[wo+3*wOcStride+i]
		}
	}
	return r0, r1, r2, r3
}

// transpose8x8F32AVX2 transposes one 8x8 float32 block between strided
// buffers. Strides are in float32 elements.
func transpose8x8F32AVX2(src []float32, srcStride int, dst []float32, dstStride int) {
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			dst[j*dstStride+i] = src[i*srcStride+j]
		}
	}
}

// normScaleScaledInto writes dst[i] = (x[i]-mean)*scale + beta and
// scaled[i] = a*dst[i] in a single pass.
func normScaleScaledInto(dst, scaled, x []float32, mean, scale, beta, a float32) {
	for i, v := range x {
		d := (v-mean)*scale + beta
		dst[i] = d
		scaled[i] = a * d
	}
}

// squareInplace squares each element of x in place.
func squareInplace(x []float32) {
	for i, v := range x {
		x[i] = v * v
	}
}

// conv4x2G evaluates two adjacent output positions for four output channels
// and stores bias(+residual)+dot results directly into out. Pure-Go version;
// the amd64 build replaces it with the AVX2+FMA kernel in dot_strided_amd64.s.
func conv4x2G(x []float32, xStride int, w []float32, wStride, wOcStride int, taps, n, xPosStride int, out []float32, outOcStride, outPosStride int, bias []float32, res []float32) {
	for oc := 0; oc < 4; oc++ {
		for p := 0; p < 2; p++ {
			sum := float32(0)
			if bias != nil {
				sum = bias[oc]
			}
			wo := oc * wOcStride
			for t := 0; t < taps; t++ {
				xo := t*xStride + p*xPosStride
				woff := wo + t*wStride
				for i := 0; i < n; i++ {
					sum += x[xo+i] * w[woff+i]
				}
			}
			if res != nil {
				sum += res[oc*outOcStride+p*outPosStride]
			}
			out[oc*outOcStride+p*outPosStride] = sum
		}
	}
}
