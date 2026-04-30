package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

func exp_rotation1(X *celt_norm, len_ int, stride int, c opus_val16, s opus_val16) {
	var (
		i    int
		ms   opus_val16
		Xptr *celt_norm
	)
	Xptr = X
	ms = -s
	for i = 0; i < len_-stride; i++ {
		var (
			x1 celt_norm
			x2 celt_norm
		)
		x1 = *(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*0))
		x2 = *(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*uintptr(stride)))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*uintptr(stride))) = celt_norm((opus_val32(c) * opus_val32(x2)) + opus_val32(s)*opus_val32(x1))
		*func() *celt_norm {
			p := &Xptr
			x := *p
			*p = (*celt_norm)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(celt_norm(0))*1))
			return x
		}() = celt_norm((opus_val32(c) * opus_val32(x1)) + opus_val32(ms)*opus_val32(x2))
	}
	Xptr = (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(len_-stride*2-1)))
	for i = len_ - stride*2 - 1; i >= 0; i-- {
		var (
			x1 celt_norm
			x2 celt_norm
		)
		x1 = *(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*0))
		x2 = *(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*uintptr(stride)))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(Xptr), unsafe.Sizeof(celt_norm(0))*uintptr(stride))) = celt_norm((opus_val32(c) * opus_val32(x2)) + opus_val32(s)*opus_val32(x1))
		*func() *celt_norm {
			p := &Xptr
			x := *p
			*p = (*celt_norm)(unsafe.Add(unsafe.Pointer(*p), -int(unsafe.Sizeof(celt_norm(0))*1)))
			return x
		}() = celt_norm((opus_val32(c) * opus_val32(x1)) + opus_val32(ms)*opus_val32(x2))
	}
}
func exp_rotation(X *celt_norm, len_ int, dir int, stride int, K int, spread int) {
	var (
		SPREAD_FACTOR [3]int = [3]int{15, 10, 5}
		i             int
		c             opus_val16
		s             opus_val16
		gain          opus_val16
		theta         opus_val16
		stride2       int = 0
		factor        int
	)
	if K*2 >= len_ || spread == 0 {
		return
	}
	factor = SPREAD_FACTOR[spread-1]
	gain = opus_val16((opus_val32(len_) * opus_val32(opus_val16(1.0))) / (opus_val32(len_ + factor*K)))
	theta = (gain * gain) * opus_val16(0.5)
	c = opus_val16(float32(math.Cos(float64((PI * 0.5) * theta))))
	s = opus_val16(float32(math.Cos(float64((PI * 0.5) * (Q15ONE - theta)))))
	if len_ >= stride*8 {
		stride2 = 1
		for (stride2*stride2+stride2)*stride+(stride>>2) < len_ {
			stride2++
		}
	}
	len_ = int(celt_udiv(uint32(int32(len_)), uint32(int32(stride))))
	for i = 0; i < stride; i++ {
		if dir < 0 {
			if stride2 != 0 {
				exp_rotation1((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i*len_))), len_, stride2, s, c)
			}
			exp_rotation1((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i*len_))), len_, 1, c, s)
		} else {
			exp_rotation1((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i*len_))), len_, 1, c, -s)
			if stride2 != 0 {
				exp_rotation1((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i*len_))), len_, stride2, s, -c)
			}
		}
	}
}
func normalise_residual(iy *int, X *celt_norm, N int, Ryy opus_val32, gain opus_val16) {
	var (
		i int
		t opus_val32
		g opus_val16
	)
	t = Ryy
	g = opus_val16((1.0 / (float32(math.Sqrt(float64(t))))) * float32(gain))
	i = 0
	for {
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i))) = celt_norm(opus_val32(g) * opus_val32(*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(i)))))
		if func() int {
			p := &i
			*p++
			return *p
		}() >= N {
			break
		}
	}
}
func extract_collapse_mask(iy *int, N int, B int) uint {
	var (
		collapse_mask uint
		N0            int
		i             int
	)
	if B <= 1 {
		return 1
	}
	N0 = int(celt_udiv(uint32(int32(N)), uint32(int32(B))))
	collapse_mask = 0
	i = 0
	for {
		{
			var (
				j   int
				tmp uint = 0
			)
			j = 0
			for {
				tmp |= uint(*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(i*N0+j))))
				if func() int {
					p := &j
					*p++
					return *p
				}() >= N0 {
					break
				}
			}
			collapse_mask |= uint(int(libc.BoolToInt(tmp != 0)) << i)
		}
		if func() int {
			p := &i
			*p++
			return *p
		}() >= B {
			break
		}
	}
	return collapse_mask
}
func op_pvq_search_c(X *celt_norm, iy *int, K int, N int, arch int) opus_val16 {
	var (
		y          *celt_norm
		signx      *int
		i          int
		j          int
		pulsesLeft int
		sum        opus_val32
		xy         opus_val32
		yy         opus_val16
	)
	_ = arch
	y = (*celt_norm)(libc.Malloc(N * int(unsafe.Sizeof(celt_norm(0)))))
	signx = (*int)(libc.Malloc(N * int(unsafe.Sizeof(int(0)))))
	sum = 0
	j = 0
	for {
		*(*int)(unsafe.Add(unsafe.Pointer(signx), unsafe.Sizeof(int(0))*uintptr(j))) = int(libc.BoolToInt(float32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))) < 0))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(float32(math.Abs(float64(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))))))
		*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j))) = 0
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = 0
		if func() int {
			p := &j
			*p++
			return *p
		}() >= N {
			break
		}
	}
	xy = opus_val32(func() opus_val16 {
		yy = 0
		return yy
	}())
	pulsesLeft = K
	if K > (N >> 1) {
		var rcp opus_val16
		j = 0
		for {
			sum += opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))))
			if func() int {
				p := &j
				*p++
				return *p
			}() >= N {
				break
			}
		}
		if sum <= EPSILON || float32(sum) >= 64 {
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0)) = celt_norm(1.0)
			j = 1
			for {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = 0
				if func() int {
					p := &j
					*p++
					return *p
				}() >= N {
					break
				}
			}
			sum = opus_val32(1.0)
		}
		rcp = opus_val16((float64(K) + 0.8) * float64(opus_val32(1.0)/sum))
		j = 0
		for {
			*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j))) = int(math.Floor(float64(rcp * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))))))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j))))
			yy = yy + opus_val16(opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))))*opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j)))))
			xy = xy + opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))))*opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) *= 2
			pulsesLeft -= *(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j)))
			if func() int {
				p := &j
				*p++
				return *p
			}() >= N {
				break
			}
		}
	}
	if pulsesLeft > N+3 {
		var tmp opus_val16 = opus_val16(pulsesLeft)
		yy = yy + opus_val16(opus_val32(tmp)*opus_val32(tmp))
		yy = yy + opus_val16(opus_val32(tmp)*opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*0))))
		*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*0)) += pulsesLeft
		pulsesLeft = 0
	}
	for i = 0; i < pulsesLeft; i++ {
		var (
			Rxy      opus_val16
			Ryy      opus_val16
			best_id  int
			best_num opus_val32
			best_den opus_val16
		)
		best_id = 0
		yy = opus_val16(float32(yy) + 1)
		Rxy = opus_val16(xy + opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0))))
		Ryy = yy + opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*0)))
		Rxy = Rxy * Rxy
		best_den = Ryy
		best_num = opus_val32(Rxy)
		j = 1
		for {
			Rxy = opus_val16(xy + opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))))
			Ryy = yy + opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(j))))
			Rxy = Rxy * Rxy
			if (opus_val32(best_den) * opus_val32(Rxy)) > (opus_val32(Ryy) * best_num) {
				best_den = Ryy
				best_num = opus_val32(Rxy)
				best_id = j
			}
			if func() int {
				p := &j
				*p++
				return *p
			}() >= N {
				break
			}
		}
		xy = xy + opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(best_id))))
		yy = yy + opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(best_id))))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(y), unsafe.Sizeof(celt_norm(0))*uintptr(best_id))) += 2
		*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(best_id)))++
	}
	j = 0
	for {
		*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j))) = (*(*int)(unsafe.Add(unsafe.Pointer(iy), unsafe.Sizeof(int(0))*uintptr(j))) ^ (-*(*int)(unsafe.Add(unsafe.Pointer(signx), unsafe.Sizeof(int(0))*uintptr(j))))) + *(*int)(unsafe.Add(unsafe.Pointer(signx), unsafe.Sizeof(int(0))*uintptr(j)))
		if func() int {
			p := &j
			*p++
			return *p
		}() >= N {
			break
		}
	}
	return yy
}
func alg_quant(X *celt_norm, N int, K int, spread int, B int, enc *ec_enc, gain opus_val16, resynth int, arch int) uint {
	var (
		iy            *int
		yy            opus_val16
		collapse_mask uint
	)
	iy = (*int)(libc.Malloc((N + 3) * int(unsafe.Sizeof(int(0)))))
	exp_rotation(X, N, 1, B, K, spread)
	yy = op_pvq_search_c(X, iy, K, N, arch)
	encode_pulses(iy, N, K, enc)
	if resynth != 0 {
		normalise_residual(iy, X, N, opus_val32(yy), gain)
		exp_rotation(X, N, -1, B, K, spread)
	}
	collapse_mask = extract_collapse_mask(iy, N, B)
	return collapse_mask
}
func alg_unquant(X *celt_norm, N int, K int, spread int, B int, dec *ec_dec, gain opus_val16) uint {
	var (
		Ryy           opus_val32
		collapse_mask uint
		iy            *int
	)
	iy = (*int)(libc.Malloc(N * int(unsafe.Sizeof(int(0)))))
	Ryy = decode_pulses(iy, N, K, dec)
	normalise_residual(iy, X, N, Ryy, gain)
	exp_rotation(X, N, -1, B, K, spread)
	collapse_mask = extract_collapse_mask(iy, N, B)
	return collapse_mask
}
func renormalise_vector(X *celt_norm, N int, gain opus_val16, arch int) {
	var (
		i    int
		E    opus_val32
		g    opus_val16
		t    opus_val32
		xptr *celt_norm
	)
	E = EPSILON + (func() opus_val32 {
		_ = arch
		return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), N)
	}())
	t = E
	g = opus_val16((1.0 / (float32(math.Sqrt(float64(t))))) * float32(gain))
	xptr = X
	for i = 0; i < N; i++ {
		*xptr = celt_norm(opus_val32(g) * opus_val32(*xptr))
		xptr = (*celt_norm)(unsafe.Add(unsafe.Pointer(xptr), unsafe.Sizeof(celt_norm(0))*1))
	}
}
func stereo_itheta(X *celt_norm, Y *celt_norm, stereo int, N int, arch int) int {
	var (
		i      int
		itheta int
		mid    opus_val16
		side   opus_val16
		Emid   opus_val32
		Eside  opus_val32
	)
	Emid = func() opus_val32 {
		Eside = EPSILON
		return Eside
	}()
	if stereo != 0 {
		for i = 0; i < N; i++ {
			var (
				m celt_norm
				s celt_norm
			)
			m = (*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i)))) + (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(i))))
			s = (*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i)))) - (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(i))))
			Emid = Emid + opus_val32(m)*opus_val32(m)
			Eside = Eside + opus_val32(s)*opus_val32(s)
		}
	} else {
		Emid += func() opus_val32 {
			_ = arch
			return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), N)
		}()
		Eside += func() opus_val32 {
			_ = arch
			return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(Y)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(Y)), N), N)
		}()
	}
	mid = opus_val16(float32(math.Sqrt(float64(Emid))))
	side = opus_val16(float32(math.Sqrt(float64(Eside))))
	itheta = int(math.Floor(float64(fast_atan2f(float32(side), float32(mid))*(16384*0.63662) + 0.5)))
	return itheta
}
