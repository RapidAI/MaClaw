package tensor

// gemmaQuantizeQ8URowScalar quantizes with one scale per ROW (s[r] =
// rowAmax/127). Portable fallback for gemmaQuantizeQ8URow.
func gemmaQuantizeQ8URowScalar(q []byte, s []float32, a []float32, rows, K int) {
	for r := 0; r < rows; r++ {
		row := a[r*K : (r+1)*K]
		qs := q[r*K : (r+1)*K]
		amax := float32(0)
		for i := 0; i < K; i++ {
			v := row[i]
			if v < 0 {
				v = -v
			}
			if v > amax {
				amax = v
			}
		}
		if amax <= 1e-8 {
			s[r] = 0
			for i := 0; i < K; i++ {
				qs[i] = 128
			}
			continue
		}
		s[r] = amax / 127
		inv := 127 / amax
		for i := 0; i < K; i++ {
			x := row[i] * inv
			qi := int(x + 0.5)
			if x < 0 {
				qi = int(x - 0.5)
			}
			if qi > 127 {
				qi = 127
			}
			if qi < -127 {
				qi = -127
			}
			qs[i] = byte(qi + 128)
		}
	}
}

// gemmaQuantizeS16Scalar quantizes with one scale per ROW into s16 payloads
// for the AVX2 VPMADDWD M8 kernels: s[r] = rowAmax/16383,
// q[r*K+i] = clamp(round(a[r][i]*16383/amax), -16383, 16383).
// Portable fallback for gemmaQuantizeS16.
func gemmaQuantizeS16Scalar(q []int16, s []float32, a []float32, rows, K int) {
	const max = float32(16383)
	for r := 0; r < rows; r++ {
		row := a[r*K : (r+1)*K]
		dst := q[r*K : (r+1)*K]
		amax := float32(0)
		for i := 0; i < K; i++ {
			v := row[i]
			if v < 0 {
				v = -v
			}
			if v > amax {
				amax = v
			}
		}
		if amax <= 1e-8 {
			s[r] = 0
			for i := 0; i < K; i++ {
				dst[i] = 0
			}
			continue
		}
		s[r] = amax / max
		inv := max / amax
		for i := 0; i < K; i++ {
			x := row[i] * inv
			qi := int32(x + 0.5)
			if x < 0 {
				qi = int32(x - 0.5)
			}
			if qi > 16383 {
				qi = 16383
			}
			if qi < -16383 {
				qi = -16383
			}
			dst[i] = int16(qi)
		}
	}
}

// gemmaQuantizeQ8UScalar is the portable fallback for gemmaQuantizeQ8U:
// per-32-block absmax scaling, round-half-away, u8 = signed+128.
func gemmaQuantizeQ8UScalar(q []byte, s []float32, a []float32, rows, K int) {
	nBlocks := K / q8BlockSize
	for r := 0; r < rows; r++ {
		row := a[r*K : (r+1)*K]
		qs := q[r*K : (r+1)*K]
		ss := s[r*nBlocks : (r+1)*nBlocks]
		for b := 0; b < nBlocks; b++ {
			blk := row[b*32 : (b+1)*32]
			amax := float32(0)
			for i := 0; i < 32; i++ {
				v := blk[i]
				if v < 0 {
					v = -v
				}
				if v > amax {
					amax = v
				}
			}
			if amax <= 1e-8 {
				ss[b] = 0
				for i := 0; i < 32; i++ {
					qs[b*32+i] = 128
				}
				continue
			}
			ss[b] = amax / 127
			inv := 127 / amax
			dst := qs[b*32 : (b+1)*32]
			for i := 0; i < 32; i++ {
				x := blk[i] * inv
				qi := int(x + 0.5)
				if x < 0 {
					qi = int(x - 0.5)
				}
				if qi > 127 {
					qi = 127
				}
				if qi < -127 {
					qi = -127
				}
				dst[i] = byte(qi + 128)
			}
		}
	}
}
