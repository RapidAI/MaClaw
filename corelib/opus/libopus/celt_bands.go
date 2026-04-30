package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

const SPREAD_NONE = 0
const SPREAD_LIGHT = 1
const SPREAD_NORMAL = 2
const SPREAD_AGGRESSIVE = 3

func hysteresis_decision(val opus_val16, thresholds *opus_val16, hysteresis *opus_val16, N int, prev int) int {
	var i int
	for i = 0; i < N; i++ {
		if val < *(*opus_val16)(unsafe.Add(unsafe.Pointer(thresholds), unsafe.Sizeof(opus_val16(0))*uintptr(i))) {
			break
		}
	}
	if i > prev && val < *(*opus_val16)(unsafe.Add(unsafe.Pointer(thresholds), unsafe.Sizeof(opus_val16(0))*uintptr(prev)))+*(*opus_val16)(unsafe.Add(unsafe.Pointer(hysteresis), unsafe.Sizeof(opus_val16(0))*uintptr(prev))) {
		i = prev
	}
	if i < prev && val > *(*opus_val16)(unsafe.Add(unsafe.Pointer(thresholds), unsafe.Sizeof(opus_val16(0))*uintptr(prev-1)))-*(*opus_val16)(unsafe.Add(unsafe.Pointer(hysteresis), unsafe.Sizeof(opus_val16(0))*uintptr(prev-1))) {
		i = prev
	}
	return i
}
func celt_lcg_rand(seed uint32) uint32 {
	return uint32(int32(int(seed)*1664525 + 1013904223))
}
func bitexact_cos(x int16) int16 {
	var (
		tmp int32
		x2  int16
	)
	tmp = int32(((int(int32(x)) * int(x)) + 4096) >> 13)
	x2 = int16(tmp)
	x2 = int16((math.MaxInt16 - int(x2)) + (((int(int32(x2)) * int(int16((((int(int32(x2))*int(int16((((int(x2)*int(int32(-626)))+16384)>>15)+8277)))+16384)>>15)+(-7651)))) + 16384) >> 15))
	return int16(int(x2) + 1)
}
func bitexact_log2tan(isin int, icos int) int {
	var (
		lc int
		ls int
	)
	lc = ec_ilog(uint32(int32(icos)))
	ls = ec_ilog(uint32(int32(isin)))
	icos <<= 15 - lc
	isin <<= 15 - ls
	return (ls-lc)*(1<<11) + (((int(int32(int16(isin))) * int(int16((((int(int32(int16(isin)))*int(-2597))+16384)>>15)+7932))) + 16384) >> 15) - (((int(int32(int16(icos))) * int(int16((((int(int32(int16(icos)))*int(-2597))+16384)>>15)+7932))) + 16384) >> 15)
}
func compute_band_energies(m *OpusCustomMode, X *celt_sig, bandE *celt_ener, end int, C int, LM int, arch int) {
	var (
		i      int
		c      int
		N      int
		eBands *int16 = m.EBands
	)
	N = m.ShortMdctSize << LM
	c = 0
	for {
		for i = 0; i < end; i++ {
			var sum opus_val32
			sum = (func() opus_val32 {
				_ = arch
				bandLen := (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))<<LM
				bandOff := c*N+(int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM)
				return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_sig(0))*uintptr(bandOff))))), bandLen), unsafe.Slice((*opus_val16)(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_sig(0))*uintptr(bandOff))))), bandLen), bandLen)
			}()) + opus_val32(1e-27)
			*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i+c*m.NbEBands))) = celt_ener(float32(math.Sqrt(float64(sum))))
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
}
func normalise_bands(m *OpusCustomMode, freq *celt_sig, X *celt_norm, bandE *celt_ener, end int, C int, M int) {
	var (
		i      int
		c      int
		N      int
		eBands *int16 = m.EBands
	)
	N = M * m.ShortMdctSize
	c = 0
	for {
		for i = 0; i < end; i++ {
			var (
				j int
				g opus_val16 = opus_val16(celt_ener(1.0) / (*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i+c*m.NbEBands))) + celt_ener(1e-27)))
			)
			for j = M * int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))); j < M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))); j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j+c*N))) = celt_norm(*(*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(j+c*N))) * celt_sig(g))
			}
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
}
func denormalise_bands(m *OpusCustomMode, X *celt_norm, freq *celt_sig, bandLogE *opus_val16, start int, end int, M int, downsample int, silence int) {
	var (
		i      int
		N      int
		bound  int
		f      *celt_sig
		x      *celt_norm
		eBands *int16 = m.EBands
	)
	N = M * m.ShortMdctSize
	bound = M * int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(end))))
	if downsample != 1 {
		if bound < (N / downsample) {
			bound = bound
		} else {
			bound = N / downsample
		}
	}
	if silence != 0 {
		bound = 0
		start = func() int {
			end = 0
			return end
		}()
	}
	f = freq
	x = (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start)))))))
	for i = 0; i < M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start)))); i++ {
		*func() *celt_sig {
			p := &f
			x := *p
			*p = (*celt_sig)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(celt_sig(0))*1))
			return x
		}() = 0
	}
	for i = start; i < end; i++ {
		var (
			j        int
			band_end int
			g        opus_val16
			lg       opus_val16
		)
		j = M * int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))
		band_end = M * int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))
		lg = (*(*opus_val16)(unsafe.Add(unsafe.Pointer(bandLogE), unsafe.Sizeof(opus_val16(0))*uintptr(i)))) + opus_val16(opus_val32(eMeans[i]))
		g = opus_val16(float32(math.Exp(float64((func() opus_val16 {
			if opus_val16(32.0) < lg {
				return opus_val16(32.0)
			}
			return lg
		}()) * opus_val16(0.6931471805599453)))))
		for {
			*func() *celt_sig {
				p := &f
				x := *p
				*p = (*celt_sig)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(celt_sig(0))*1))
				return x
			}() = celt_sig(opus_val32(*func() *celt_norm {
				p := &x
				x := *p
				*p = (*celt_norm)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(celt_norm(0))*1))
				return x
			}()) * opus_val32(g))
			if func() int {
				p := &j
				*p++
				return *p
			}() >= band_end {
				break
			}
		}
	}
	libc.MemSet(unsafe.Pointer((*celt_sig)(unsafe.Add(unsafe.Pointer(freq), unsafe.Sizeof(celt_sig(0))*uintptr(bound)))), 0, (N-bound)*int(unsafe.Sizeof(celt_sig(0))))
}
func anti_collapse(m *OpusCustomMode, X_ *celt_norm, collapse_masks *uint8, LM int, C int, size int, start int, end int, logE *opus_val16, prev1logE *opus_val16, prev2logE *opus_val16, pulses *int, seed uint32, arch int) {
	var (
		c int
		i int
		j int
		k int
	)
	for i = start; i < end; i++ {
		var (
			N0     int
			thresh opus_val16
			sqrt_1 opus_val16
			depth  int
		)
		N0 = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))
		depth = int(celt_udiv(uint32(int32(*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i)))+1)), uint32(int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))) >> LM
		thresh = opus_val16((float32(math.Exp((float64(depth) * (-0.125)) * 0.6931471805599453))) * 0.5)
		sqrt_1 = opus_val16(1.0 / (float32(math.Sqrt(float64(N0 << LM)))))
		c = 0
		for {
			{
				var (
					X           *celt_norm
					prev1       opus_val16
					prev2       opus_val16
					Ediff       opus_val32
					r           opus_val16
					renormalize int = 0
				)
				prev1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(prev1logE), unsafe.Sizeof(opus_val16(0))*uintptr(c*m.NbEBands+i)))
				prev2 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(prev2logE), unsafe.Sizeof(opus_val16(0))*uintptr(c*m.NbEBands+i)))
				if C == 1 {
					if prev1 > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(prev1logE), unsafe.Sizeof(opus_val16(0))*uintptr(m.NbEBands+i)))) {
						prev1 = prev1
					} else {
						prev1 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(prev1logE), unsafe.Sizeof(opus_val16(0))*uintptr(m.NbEBands+i)))
					}
					if prev2 > (*(*opus_val16)(unsafe.Add(unsafe.Pointer(prev2logE), unsafe.Sizeof(opus_val16(0))*uintptr(m.NbEBands+i)))) {
						prev2 = prev2
					} else {
						prev2 = *(*opus_val16)(unsafe.Add(unsafe.Pointer(prev2logE), unsafe.Sizeof(opus_val16(0))*uintptr(m.NbEBands+i)))
					}
				}
				Ediff = opus_val32((*(*opus_val16)(unsafe.Add(unsafe.Pointer(logE), unsafe.Sizeof(opus_val16(0))*uintptr(c*m.NbEBands+i)))) - (func() opus_val16 {
					if prev1 < prev2 {
						return prev1
					}
					return prev2
				}()))
				if 0 > float32(Ediff) {
					Ediff = 0
				} else {
					Ediff = Ediff
				}
				r = opus_val16((float32(math.Exp(float64((-Ediff) * opus_val32(0.6931471805599453))))) * 2.0)
				if LM == 3 {
					r *= opus_val16(1.41421356)
				}
				if thresh < r {
					r = thresh
				} else {
					r = r
				}
				r = r * sqrt_1
				X = (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X_), unsafe.Sizeof(celt_norm(0))*uintptr(c*size)))), unsafe.Sizeof(celt_norm(0))*uintptr(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(i))))<<LM)))
				for k = 0; k < 1<<LM; k++ {
					if (int(*(*uint8)(unsafe.Add(unsafe.Pointer(collapse_masks), i*C+c))) & (1 << k)) == 0 {
						for j = 0; j < N0; j++ {
							seed = celt_lcg_rand(seed)
							if int(seed)&0x8000 != 0 {
								*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr((j<<LM)+k))) = celt_norm(r)
							} else {
								*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr((j<<LM)+k))) = celt_norm(-r)
							}
						}
						renormalize = 1
					}
				}
				if renormalize != 0 {
					renormalise_vector(X, N0<<LM, Q15ONE, arch)
				}
			}
			if func() int {
				p := &c
				*p++
				return *p
			}() >= C {
				break
			}
		}
	}
}
func compute_channel_weights(Ex celt_ener, Ey celt_ener, w [2]opus_val16) {
	var minE celt_ener
	if Ex < Ey {
		minE = Ex
	} else {
		minE = Ey
	}
	Ex = Ex + celt_ener(float32(minE)/3)
	Ey = Ey + celt_ener(float32(minE)/3)
	w[0] = opus_val16(Ex)
	w[1] = opus_val16(Ey)
}
func intensity_stereo(m *OpusCustomMode, X *celt_norm, Y *celt_norm, bandE *celt_ener, bandID int, N int) {
	var (
		i     int = bandID
		j     int
		a1    opus_val16
		a2    opus_val16
		left  opus_val16
		right opus_val16
		norm  opus_val16
	)
	left = opus_val16(*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))))
	right = opus_val16(*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i+m.NbEBands))))
	norm = opus_val16(EPSILON + (float32(math.Sqrt(float64(EPSILON + opus_val32(left)*opus_val32(left) + opus_val32(right)*opus_val32(right))))))
	a1 = opus_val16((opus_val32(left)) / opus_val32(norm))
	a2 = opus_val16((opus_val32(right)) / opus_val32(norm))
	for j = 0; j < N; j++ {
		var (
			r celt_norm
			l celt_norm
		)
		l = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))
		r = *(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j)))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm((opus_val32(a1) * opus_val32(l)) + opus_val32(a2)*opus_val32(r))
	}
}
func stereo_split(X *celt_norm, Y *celt_norm, N int) {
	var j int
	for j = 0; j < N; j++ {
		var (
			r opus_val32
			l opus_val32
		)
		l = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))) * opus_val32(0.70710678)
		r = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j)))) * opus_val32(0.70710678)
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(l + r)
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(r - l)
	}
}
func stereo_merge(X []celt_norm, Y []celt_norm, mid opus_val16, N int, arch int) {
	var (
		j     int
		xp    opus_val32 = 0
		side  opus_val32 = 0
		El    opus_val32
		Er    opus_val32
		mid2  opus_val16
		t     opus_val32
		lgain opus_val32
		rgain opus_val32
	)
	_ = arch
	dual_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(&Y[0])), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(&X[0])), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(&Y[0])), N), N, &xp, &side)
	xp = opus_val32(mid * opus_val16(xp))
	mid2 = mid
	El = (opus_val32(mid2) * opus_val32(mid2)) + side - opus_val32(float32(xp)*2)
	Er = (opus_val32(mid2) * opus_val32(mid2)) + side + opus_val32(float32(xp)*2)
	if Er < opus_val32(0.0006) || El < opus_val32(0.0006) {
		libc.MemCpy(unsafe.Pointer(&Y[0]), unsafe.Pointer(&X[0]), N*int(unsafe.Sizeof(celt_norm(0))))
		return
	}
	t = El
	lgain = opus_val32(1.0 / (float32(math.Sqrt(float64(t)))))
	t = Er
	rgain = opus_val32(1.0 / (float32(math.Sqrt(float64(t)))))
	for j = 0; j < N; j++ {
		var (
			r celt_norm
			l celt_norm
		)
		l = celt_norm(mid * opus_val16(X[j]))
		r = Y[j]
		X[j] = celt_norm(lgain * opus_val32(l-r))
		Y[j] = celt_norm(rgain * opus_val32(l+r))
	}
}
func spreading_decision(m *OpusCustomMode, X *celt_norm, average *int, last_decision int, hf_average *int, tapset_decision *int, update_hf int, end int, C int, M int, spread_weight *int) int {
	var (
		i        int
		c        int
		N0       int
		sum      int    = 0
		nbBands  int    = 0
		eBands   *int16 = m.EBands
		decision int
		hf_sum   int = 0
	)
	N0 = M * m.ShortMdctSize
	if M*(int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(end))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(end-1))))) <= 8 {
		return 0
	}
	c = 0
	for {
		for i = 0; i < end; i++ {
			var (
				j      int
				N      int
				tmp    int        = 0
				tcount [3]int     = [3]int{}
				x      *celt_norm = (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), unsafe.Sizeof(celt_norm(0))*uintptr(c*N0)))
			)
			N = M * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))
			if N <= 8 {
				continue
			}
			for j = 0; j < N; j++ {
				var x2N opus_val32
				x2N = opus_val32((*(*celt_norm)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_norm(0))*uintptr(j))))*(*(*celt_norm)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_norm(0))*uintptr(j))))) * opus_val32(N)
				if x2N < opus_val32(0.25) {
					tcount[0]++
				}
				if x2N < opus_val32(0.0625) {
					tcount[1]++
				}
				if x2N < opus_val32(0.015625) {
					tcount[2]++
				}
			}
			if i > m.NbEBands-4 {
				hf_sum += int(celt_udiv(uint32(int32((tcount[1]+tcount[0])*32)), uint32(int32(N))))
			}
			tmp = int(libc.BoolToInt(tcount[2]*2 >= N) + libc.BoolToInt(tcount[1]*2 >= N) + libc.BoolToInt(tcount[0]*2 >= N))
			sum += tmp * *(*int)(unsafe.Add(unsafe.Pointer(spread_weight), unsafe.Sizeof(int(0))*uintptr(i)))
			nbBands += *(*int)(unsafe.Add(unsafe.Pointer(spread_weight), unsafe.Sizeof(int(0))*uintptr(i)))
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= C {
			break
		}
	}
	if update_hf != 0 {
		if hf_sum != 0 {
			hf_sum = int(celt_udiv(uint32(int32(hf_sum)), uint32(int32(C*(4-m.NbEBands+end)))))
		}
		*hf_average = (*hf_average + hf_sum) >> 1
		hf_sum = *hf_average
		if *tapset_decision == 2 {
			hf_sum += 4
		} else if *tapset_decision == 0 {
			hf_sum -= 4
		}
		if hf_sum > 22 {
			*tapset_decision = 2
		} else if hf_sum > 18 {
			*tapset_decision = 1
		} else {
			*tapset_decision = 0
		}
	}
	sum = int(celt_udiv(uint32(int32(int(int32(sum))<<8)), uint32(int32(nbBands))))
	sum = (sum + *average) >> 1
	*average = sum
	sum = (sum*3 + (((3 - last_decision) << 7) + 64) + 2) >> 2
	if sum < 80 {
		decision = 3
	} else if sum < 256 {
		decision = 2
	} else if sum < 384 {
		decision = 1
	} else {
		decision = 0
	}
	return decision
}

var ordery_table [30]int = [30]int{1, 0, 3, 0, 2, 1, 7, 0, 4, 3, 6, 1, 5, 2, 15, 0, 8, 7, 12, 3, 11, 4, 14, 1, 9, 6, 13, 2, 10, 5}

func deinterleave_hadamard(X *celt_norm, N0 int, stride int, hadamard int) {
	var (
		i   int
		j   int
		tmp *celt_norm
		N   int
	)
	N = N0 * stride
	tmp = (*celt_norm)(libc.Malloc(N * int(unsafe.Sizeof(celt_norm(0)))))
	if hadamard != 0 {
		var ordery *int = (*int)(unsafe.Add(unsafe.Pointer(&ordery_table[stride]), -int(unsafe.Sizeof(int(0))*2)))
		for i = 0; i < stride; i++ {
			for j = 0; j < N0; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(celt_norm(0))*uintptr(*(*int)(unsafe.Add(unsafe.Pointer(ordery), unsafe.Sizeof(int(0))*uintptr(i)))*N0+j))) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j*stride+i)))
			}
		}
	} else {
		for i = 0; i < stride; i++ {
			for j = 0; j < N0; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(celt_norm(0))*uintptr(i*N0+j))) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j*stride+i)))
			}
		}
	}
	libc.MemCpy(unsafe.Pointer(X), unsafe.Pointer(tmp), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X))-uintptr(unsafe.Pointer(tmp))))*0))
}
func interleave_hadamard(X *celt_norm, N0 int, stride int, hadamard int) {
	var (
		i   int
		j   int
		tmp *celt_norm
		N   int
	)
	N = N0 * stride
	tmp = (*celt_norm)(libc.Malloc(N * int(unsafe.Sizeof(celt_norm(0)))))
	if hadamard != 0 {
		var ordery *int = (*int)(unsafe.Add(unsafe.Pointer(&ordery_table[stride]), -int(unsafe.Sizeof(int(0))*2)))
		for i = 0; i < stride; i++ {
			for j = 0; j < N0; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(celt_norm(0))*uintptr(j*stride+i))) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(*(*int)(unsafe.Add(unsafe.Pointer(ordery), unsafe.Sizeof(int(0))*uintptr(i)))*N0+j)))
			}
		}
	} else {
		for i = 0; i < stride; i++ {
			for j = 0; j < N0; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(tmp), unsafe.Sizeof(celt_norm(0))*uintptr(j*stride+i))) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(i*N0+j)))
			}
		}
	}
	libc.MemCpy(unsafe.Pointer(X), unsafe.Pointer(tmp), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X))-uintptr(unsafe.Pointer(tmp))))*0))
}
func haar1(X *celt_norm, N0 int, stride int) {
	var (
		i int
		j int
	)
	N0 >>= 1
	for i = 0; i < stride; i++ {
		for j = 0; j < N0; j++ {
			var (
				tmp1 opus_val32
				tmp2 opus_val32
			)
			tmp1 = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(stride*2*j+i)))) * opus_val32(0.70710678)
			tmp2 = opus_val32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(stride*(j*2+1)+i)))) * opus_val32(0.70710678)
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(stride*2*j+i))) = celt_norm(tmp1 + tmp2)
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(stride*(j*2+1)+i))) = celt_norm(tmp1 - tmp2)
		}
	}
}
func compute_qn(N int, b int, offset int, pulse_cap int, stereo int) int {
	var (
		exp2_table8 [8]int16 = [8]int16{16384, 17866, 19483, 21247, 23170, 25267, 27554, 30048}
		qn          int
		qb          int
		N2          int = N*2 - 1
	)
	if stereo != 0 && N == 2 {
		N2--
	}
	qb = int(celt_sudiv(int32(b+N2*offset), int32(N2)))
	if (b - pulse_cap - (int(4 << BITRES))) < qb {
		qb = b - pulse_cap - (int(4 << BITRES))
	} else {
		qb = qb
	}
	if (int(8 << BITRES)) < qb {
		qb = int(8 << BITRES)
	} else {
		qb = qb
	}
	if qb < (int(1<<BITRES) >> 1) {
		qn = 1
	} else {
		qn = int(exp2_table8[qb&0x7]) >> (14 - (qb >> BITRES))
		qn = (qn + 1) >> 1 << 1
	}
	return qn
}

type band_ctx struct {
	Encode            int
	Resynth           int
	M                 *OpusCustomMode
	I                 int
	Intensity         int
	Spread            int
	Tf_change         int
	Ec                *ec_ctx
	Remaining_bits    int32
	BandE             *celt_ener
	Seed              uint32
	Arch              int
	Theta_round       int
	Disable_inv       int
	Avoid_split_noise int
}
type split_ctx struct {
	Inv    int
	Imid   int
	Iside  int
	Delta  int
	Itheta int
	Qalloc int
}

func compute_theta(ctx *band_ctx, sctx *split_ctx, X *celt_norm, Y *celt_norm, N int, b *int, B int, B0 int, LM int, stereo int, fill *int) {
	var (
		qn        int
		itheta    int = 0
		delta     int
		imid      int
		iside     int
		qalloc    int
		pulse_cap int
		offset    int
		tell      int32
		inv       int = 0
		encode    int
		m         *OpusCustomMode
		i         int
		intensity int
		ec        *ec_ctx
		bandE     *celt_ener
	)
	encode = ctx.Encode
	m = ctx.M
	i = ctx.I
	intensity = ctx.Intensity
	ec = ctx.Ec
	bandE = ctx.BandE
	pulse_cap = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.LogN), unsafe.Sizeof(int16(0))*uintptr(i)))) + LM*(int(1<<BITRES))
	offset = (pulse_cap >> 1) - (func() int {
		if stereo != 0 && N == 2 {
			return QTHETA_OFFSET_TWOPHASE
		}
		return QTHETA_OFFSET
	}())
	qn = compute_qn(N, *b, offset, pulse_cap, stereo)
	if stereo != 0 && i >= intensity {
		qn = 1
	}
	if encode != 0 {
		itheta = stereo_itheta(X, Y, stereo, N, ctx.Arch)
	}
	tell = int32(ec_tell_frac(ec))
	if qn != 1 {
		if encode != 0 {
			if stereo == 0 || ctx.Theta_round == 0 {
				itheta = (itheta*int(int32(qn)) + 8192) >> 14
				if stereo == 0 && ctx.Avoid_split_noise != 0 && itheta > 0 && itheta < qn {
					var unquantized int = int(celt_udiv(uint32(int32(int(int32(itheta))*16384)), uint32(int32(qn))))
					imid = int(bitexact_cos(int16(unquantized)))
					iside = int(bitexact_cos(int16(16384 - unquantized)))
					delta = ((int(int32(int16((N-1)<<7))) * int(int16(bitexact_log2tan(iside, imid)))) + 16384) >> 15
					if delta > *b {
						itheta = qn
					} else if delta < -*b {
						itheta = 0
					}
				}
			} else {
				var (
					down int
					bias int
				)
				if itheta > 8192 {
					bias = math.MaxInt16 / qn
				} else {
					bias = int(-32767 / qn)
				}
				if (qn - 1) < (func() int {
					if 0 > ((itheta*int(int32(qn)) + bias) >> 14) {
						return 0
					}
					return (itheta*int(int32(qn)) + bias) >> 14
				}()) {
					down = qn - 1
				} else if 0 > ((itheta*int(int32(qn)) + bias) >> 14) {
					down = 0
				} else {
					down = (itheta*int(int32(qn)) + bias) >> 14
				}
				if ctx.Theta_round < 0 {
					itheta = down
				} else {
					itheta = down + 1
				}
			}
		}
		if stereo != 0 && N > 2 {
			var (
				p0 int = 3
				x  int = itheta
				x0 int = qn / 2
				ft int = p0*(x0+1) + x0
			)
			if encode != 0 {
				ec_encode((*ec_enc)(unsafe.Pointer(ec)), uint(func() int {
					if x <= x0 {
						return p0 * x
					}
					return (x - 1 - x0) + (x0+1)*p0
				}()), uint(func() int {
					if x <= x0 {
						return p0 * (x + 1)
					}
					return (x - x0) + (x0+1)*p0
				}()), uint(ft))
			} else {
				var fs int
				fs = int(ec_decode((*ec_dec)(unsafe.Pointer(ec)), uint(ft)))
				if fs < (x0+1)*p0 {
					x = fs / p0
				} else {
					x = x0 + 1 + (fs - (x0+1)*p0)
				}
				ec_dec_update((*ec_dec)(unsafe.Pointer(ec)), uint(func() int {
					if x <= x0 {
						return p0 * x
					}
					return (x - 1 - x0) + (x0+1)*p0
				}()), uint(func() int {
					if x <= x0 {
						return p0 * (x + 1)
					}
					return (x - x0) + (x0+1)*p0
				}()), uint(ft))
				itheta = x
			}
		} else if B0 > 1 || stereo != 0 {
			if encode != 0 {
				ec_enc_uint((*ec_enc)(unsafe.Pointer(ec)), uint32(int32(itheta)), uint32(int32(qn+1)))
			} else {
				itheta = int(ec_dec_uint((*ec_dec)(unsafe.Pointer(ec)), uint32(int32(qn+1))))
			}
		} else {
			var (
				fs int = 1
				ft int
			)
			ft = ((qn >> 1) + 1) * ((qn >> 1) + 1)
			if encode != 0 {
				var fl int
				if itheta <= (qn >> 1) {
					fs = itheta + 1
				} else {
					fs = qn + 1 - itheta
				}
				if itheta <= (qn >> 1) {
					fl = itheta * (itheta + 1) >> 1
				} else {
					fl = ft - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
				}
				ec_encode((*ec_enc)(unsafe.Pointer(ec)), uint(fl), uint(fl+fs), uint(ft))
			} else {
				var (
					fl int = 0
					fm int
				)
				fm = int(ec_decode((*ec_dec)(unsafe.Pointer(ec)), uint(ft)))
				if fm < ((qn >> 1) * ((qn >> 1) + 1) >> 1) {
					itheta = int((isqrt32(uint32(int32(int(uint32(int32(fm)))*8+1))) - 1) >> 1)
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					itheta = ((qn+1)*2 - int(isqrt32(uint32(int32(int(uint32(int32(ft-fm-1)))*8+1))))) >> 1
					fs = qn + 1 - itheta
					fl = ft - ((qn + 1 - itheta) * (qn + 2 - itheta) >> 1)
				}
				ec_dec_update((*ec_dec)(unsafe.Pointer(ec)), uint(fl), uint(fl+fs), uint(ft))
			}
		}
		itheta = int(celt_udiv(uint32(int32(int(int32(itheta))*16384)), uint32(int32(qn))))
		if encode != 0 && stereo != 0 {
			if itheta == 0 {
				intensity_stereo(m, X, Y, bandE, i, N)
			} else {
				stereo_split(X, Y, N)
			}
		}
	} else if stereo != 0 {
		if encode != 0 {
			inv = int(libc.BoolToInt(itheta > 8192 && ctx.Disable_inv == 0))
			if inv != 0 {
				var j int
				for j = 0; j < N; j++ {
					*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = -*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j)))
				}
			}
			intensity_stereo(m, X, Y, bandE, i, N)
		}
		if *b > int(2<<BITRES) && int(ctx.Remaining_bits) > int(2<<BITRES) {
			if encode != 0 {
				ec_enc_bit_logp((*ec_enc)(unsafe.Pointer(ec)), inv, 2)
			} else {
				inv = ec_dec_bit_logp((*ec_dec)(unsafe.Pointer(ec)), 2)
			}
		} else {
			inv = 0
		}
		if ctx.Disable_inv != 0 {
			inv = 0
		}
		itheta = 0
	}
	qalloc = int(ec_tell_frac(ec)) - int(tell)
	*b -= qalloc
	if itheta == 0 {
		imid = math.MaxInt16
		iside = 0
		*fill &= (1 << B) - 1
		delta = -16384
	} else if itheta == 16384 {
		imid = 0
		iside = math.MaxInt16
		*fill &= ((1 << B) - 1) << B
		delta = 16384
	} else {
		imid = int(bitexact_cos(int16(itheta)))
		iside = int(bitexact_cos(int16(16384 - itheta)))
		delta = ((int(int32(int16((N-1)<<7))) * int(int16(bitexact_log2tan(iside, imid)))) + 16384) >> 15
	}
	sctx.Inv = inv
	sctx.Imid = imid
	sctx.Iside = iside
	sctx.Delta = delta
	sctx.Itheta = itheta
	sctx.Qalloc = qalloc
}
func quant_band_n1(ctx *band_ctx, X *celt_norm, Y *celt_norm, lowband_out *celt_norm) uint {
	var (
		c      int
		stereo int
		x      *celt_norm = X
		encode int
		ec     *ec_ctx
	)
	encode = ctx.Encode
	ec = ctx.Ec
	stereo = int(libc.BoolToInt(Y != nil))
	c = 0
	for {
		{
			var sign int = 0
			if int(ctx.Remaining_bits) >= int(1<<BITRES) {
				if encode != 0 {
					sign = int(libc.BoolToInt(float32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_norm(0))*0))) < 0))
					ec_enc_bits((*ec_enc)(unsafe.Pointer(ec)), uint32(int32(sign)), 1)
				} else {
					sign = int(ec_dec_bits((*ec_dec)(unsafe.Pointer(ec)), 1))
				}
				ctx.Remaining_bits -= int32(int(1 << BITRES))
			}
			if ctx.Resynth != 0 {
				if sign != 0 {
					*(*celt_norm)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_norm(0))*0)) = -NORM_SCALING
				} else {
					*(*celt_norm)(unsafe.Add(unsafe.Pointer(x), unsafe.Sizeof(celt_norm(0))*0)) = NORM_SCALING
				}
			}
			x = Y
		}
		if func() int {
			p := &c
			*p++
			return *p
		}() >= stereo+1 {
			break
		}
	}
	if lowband_out != nil {
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(lowband_out), unsafe.Sizeof(celt_norm(0))*0)) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0))
	}
	return 1
}
func quant_partition(ctx *band_ctx, X *celt_norm, N int, b int, B int, lowband *celt_norm, LM int, gain opus_val16, fill int) uint {
	var (
		cache     *uint8
		q         int
		curr_bits int
		imid      int        = 0
		iside     int        = 0
		B0        int        = B
		mid       opus_val16 = 0
		side      opus_val16 = 0
		cm        uint       = 0
		Y         *celt_norm = nil
		encode    int
		m         *OpusCustomMode
		i         int
		spread    int
		ec        *ec_ctx
	)
	encode = ctx.Encode
	m = ctx.M
	i = ctx.I
	spread = ctx.Spread
	ec = ctx.Ec
	cache = (*uint8)(unsafe.Add(unsafe.Pointer(m.Cache.Bits), *(*int16)(unsafe.Add(unsafe.Pointer(m.Cache.Index), unsafe.Sizeof(int16(0))*uintptr((LM+1)*m.NbEBands+i)))))
	if LM != -1 && b > int(*(*uint8)(unsafe.Add(unsafe.Pointer(cache), *cache)))+12 && N > 2 {
		var (
			mbits         int
			sbits         int
			delta         int
			itheta        int
			qalloc        int
			sctx          split_ctx
			next_lowband2 *celt_norm = nil
			rebalance     int32
		)
		N >>= 1
		Y = (*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(N)))
		LM -= 1
		if B == 1 {
			fill = (fill & 1) | fill<<1
		}
		B = (B + 1) >> 1
		compute_theta(ctx, &sctx, X, Y, N, &b, B, B0, LM, 0, &fill)
		imid = sctx.Imid
		iside = sctx.Iside
		delta = sctx.Delta
		itheta = sctx.Itheta
		qalloc = sctx.Qalloc
		mid = opus_val16(float64(imid) * (1.0 / 32768))
		side = opus_val16(float64(iside) * (1.0 / 32768))
		if B0 > 1 && (itheta&0x3FFF) != 0 {
			if itheta > 8192 {
				delta -= delta >> (4 - LM)
			} else if 0 < (delta + (N << BITRES >> (5 - LM))) {
				delta = 0
			} else {
				delta = delta + (N << BITRES >> (5 - LM))
			}
		}
		if 0 > (func() int {
			if b < ((b - delta) / 2) {
				return b
			}
			return (b - delta) / 2
		}()) {
			mbits = 0
		} else if b < ((b - delta) / 2) {
			mbits = b
		} else {
			mbits = (b - delta) / 2
		}
		sbits = b - mbits
		ctx.Remaining_bits -= int32(qalloc)
		if lowband != nil {
			next_lowband2 = (*celt_norm)(unsafe.Add(unsafe.Pointer(lowband), unsafe.Sizeof(celt_norm(0))*uintptr(N)))
		}
		rebalance = ctx.Remaining_bits
		if mbits >= sbits {
			cm = quant_partition(ctx, X, N, mbits, B, lowband, LM, gain*mid, fill)
			rebalance = int32(mbits - (int(rebalance) - int(ctx.Remaining_bits)))
			if int(rebalance) > int(3<<BITRES) && itheta != 0 {
				sbits += int(rebalance) - (int(3 << BITRES))
			}
			cm |= quant_partition(ctx, Y, N, sbits, B, next_lowband2, LM, gain*side, fill>>B) << uint(B0>>1)
		} else {
			cm = quant_partition(ctx, Y, N, sbits, B, next_lowband2, LM, gain*side, fill>>B) << uint(B0>>1)
			rebalance = int32(sbits - (int(rebalance) - int(ctx.Remaining_bits)))
			if int(rebalance) > int(3<<BITRES) && itheta != 16384 {
				mbits += int(rebalance) - (int(3 << BITRES))
			}
			cm |= quant_partition(ctx, X, N, mbits, B, lowband, LM, gain*mid, fill)
		}
	} else {
		q = bits2pulses(m, i, LM, b)
		curr_bits = pulses2bits(m, i, LM, q)
		ctx.Remaining_bits -= int32(curr_bits)
		for int(ctx.Remaining_bits) < 0 && q > 0 {
			ctx.Remaining_bits += int32(curr_bits)
			q--
			curr_bits = pulses2bits(m, i, LM, q)
			ctx.Remaining_bits -= int32(curr_bits)
		}
		if q != 0 {
			var K int = get_pulses(q)
			if encode != 0 {
				cm = alg_quant(X, N, K, spread, B, (*ec_enc)(unsafe.Pointer(ec)), gain, ctx.Resynth, ctx.Arch)
			} else {
				cm = alg_unquant(X, N, K, spread, B, (*ec_dec)(unsafe.Pointer(ec)), gain)
			}
		} else {
			var j int
			if ctx.Resynth != 0 {
				var cm_mask uint
				cm_mask = uint(1<<B) - 1
				fill &= int(cm_mask)
				if fill == 0 {
					libc.MemSet(unsafe.Pointer(X), 0, N*int(unsafe.Sizeof(celt_norm(0))))
				} else {
					if lowband == nil {
						for j = 0; j < N; j++ {
							ctx.Seed = celt_lcg_rand(ctx.Seed)
							*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(int(int32(ctx.Seed)) >> 20)
						}
						cm = cm_mask
					} else {
						for j = 0; j < N; j++ {
							var tmp opus_val16
							ctx.Seed = celt_lcg_rand(ctx.Seed)
							tmp = 1.0 / 256
							if int(ctx.Seed)&0x8000 != 0 {
								tmp = tmp
							} else {
								tmp = -tmp
							}
							*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = *(*celt_norm)(unsafe.Add(unsafe.Pointer(lowband), unsafe.Sizeof(celt_norm(0))*uintptr(j))) + celt_norm(tmp)
						}
						cm = uint(fill)
					}
					renormalise_vector(X, N, gain, ctx.Arch)
				}
			}
		}
	}
	return cm
}
func quant_band(ctx *band_ctx, X *celt_norm, N int, b int, B int, lowband *celt_norm, LM int, lowband_out *celt_norm, gain opus_val16, lowband_scratch *celt_norm, fill int) uint {
	var (
		N0          int = N
		N_B         int = N
		N_B0        int
		B0          int = B
		time_divide int = 0
		recombine   int = 0
		longBlocks  int
		cm          uint = 0
		k           int
		encode      int
		tf_change   int
	)
	encode = ctx.Encode
	tf_change = ctx.Tf_change
	longBlocks = int(libc.BoolToInt(B0 == 1))
	N_B = int(celt_udiv(uint32(int32(N_B)), uint32(int32(B))))
	if N == 1 {
		return quant_band_n1(ctx, X, nil, lowband_out)
	}
	if tf_change > 0 {
		recombine = tf_change
	}
	if lowband_scratch != nil && lowband != nil && (recombine != 0 || (N_B&1) == 0 && tf_change < 0 || B0 > 1) {
		libc.MemCpy(unsafe.Pointer(lowband_scratch), unsafe.Pointer(lowband), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(lowband_scratch))-uintptr(unsafe.Pointer(lowband))))*0))
		lowband = lowband_scratch
	}
	for k = 0; k < recombine; k++ {
		var bit_interleave_table [16]uint8 = [16]uint8{0, 1, 1, 1, 2, 3, 3, 3, 2, 3, 3, 3, 2, 3, 3, 3}
		if encode != 0 {
			haar1(X, N>>k, 1<<k)
		}
		if lowband != nil {
			haar1(lowband, N>>k, 1<<k)
		}
		fill = int(bit_interleave_table[fill&0xF]) | int(bit_interleave_table[fill>>4])<<2
	}
	B >>= recombine
	N_B <<= recombine
	for (N_B&1) == 0 && tf_change < 0 {
		if encode != 0 {
			haar1(X, N_B, B)
		}
		if lowband != nil {
			haar1(lowband, N_B, B)
		}
		fill |= fill << B
		B <<= 1
		N_B >>= 1
		time_divide++
		tf_change++
	}
	B0 = B
	N_B0 = N_B
	if B0 > 1 {
		if encode != 0 {
			deinterleave_hadamard(X, N_B>>recombine, B0<<recombine, longBlocks)
		}
		if lowband != nil {
			deinterleave_hadamard(lowband, N_B>>recombine, B0<<recombine, longBlocks)
		}
	}
	cm = quant_partition(ctx, X, N, b, B, lowband, LM, gain, fill)
	if ctx.Resynth != 0 {
		if B0 > 1 {
			interleave_hadamard(X, N_B>>recombine, B0<<recombine, longBlocks)
		}
		N_B = N_B0
		B = B0
		for k = 0; k < time_divide; k++ {
			B >>= 1
			N_B <<= 1
			cm |= cm >> uint(B)
			haar1(X, N_B, B)
		}
		for k = 0; k < recombine; k++ {
			var bit_deinterleave_table [16]uint8 = [16]uint8{0x0, 0x3, 0xC, 0xF, 0x30, 0x33, 0x3C, 0x3F, 0xC0, 0xC3, 0xCC, 0xCF, 0xF0, 0xF3, 0xFC, math.MaxUint8}
			cm = uint(bit_deinterleave_table[cm])
			haar1(X, N0>>k, 1<<k)
		}
		B <<= recombine
		if lowband_out != nil {
			var (
				j int
				n opus_val16
			)
			n = opus_val16(float32(math.Sqrt(float64(N0))))
			for j = 0; j < N0; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(lowband_out), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = celt_norm(n * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*uintptr(j)))))
			}
		}
		cm &= uint((1 << B) - 1)
	}
	return cm
}
func quant_band_stereo(ctx *band_ctx, X *celt_norm, Y *celt_norm, N int, b int, B int, lowband *celt_norm, LM int, lowband_out *celt_norm, lowband_scratch *celt_norm, fill int) uint {
	var (
		imid      int        = 0
		iside     int        = 0
		inv       int        = 0
		mid       opus_val16 = 0
		side      opus_val16 = 0
		cm        uint       = 0
		mbits     int
		sbits     int
		delta     int
		itheta    int
		qalloc    int
		sctx      split_ctx
		orig_fill int
		encode    int
		ec        *ec_ctx
	)
	encode = ctx.Encode
	ec = ctx.Ec
	if N == 1 {
		return quant_band_n1(ctx, X, Y, lowband_out)
	}
	orig_fill = fill
	compute_theta(ctx, &sctx, X, Y, N, &b, B, B, LM, 1, &fill)
	inv = sctx.Inv
	imid = sctx.Imid
	iside = sctx.Iside
	delta = sctx.Delta
	itheta = sctx.Itheta
	qalloc = sctx.Qalloc
	mid = opus_val16(float64(imid) * (1.0 / 32768))
	side = opus_val16(float64(iside) * (1.0 / 32768))
	if N == 2 {
		var (
			c    int
			sign int = 0
			x2   *celt_norm
			y2   *celt_norm
		)
		mbits = b
		sbits = 0
		if itheta != 0 && itheta != 16384 {
			sbits = int(1 << BITRES)
		}
		mbits -= sbits
		c = int(libc.BoolToInt(itheta > 8192))
		ctx.Remaining_bits -= int32(qalloc + sbits)
		if c != 0 {
			x2 = Y
		} else {
			x2 = X
		}
		if c != 0 {
			y2 = X
		} else {
			y2 = Y
		}
		if sbits != 0 {
			if encode != 0 {
				sign = int(libc.BoolToInt(float32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(x2), unsafe.Sizeof(celt_norm(0))*0))**(*celt_norm)(unsafe.Add(unsafe.Pointer(y2), unsafe.Sizeof(celt_norm(0))*1))-*(*celt_norm)(unsafe.Add(unsafe.Pointer(x2), unsafe.Sizeof(celt_norm(0))*1))**(*celt_norm)(unsafe.Add(unsafe.Pointer(y2), unsafe.Sizeof(celt_norm(0))*0))) < 0))
				ec_enc_bits((*ec_enc)(unsafe.Pointer(ec)), uint32(int32(sign)), 1)
			} else {
				sign = int(ec_dec_bits((*ec_dec)(unsafe.Pointer(ec)), 1))
			}
		}
		sign = 1 - sign*2
		cm = quant_band(ctx, x2, N, mbits, B, lowband, LM, lowband_out, Q15ONE, lowband_scratch, orig_fill)
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(y2), unsafe.Sizeof(celt_norm(0))*0)) = celt_norm(float32(-sign) * float32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(x2), unsafe.Sizeof(celt_norm(0))*1))))
		*(*celt_norm)(unsafe.Add(unsafe.Pointer(y2), unsafe.Sizeof(celt_norm(0))*1)) = celt_norm(float32(sign) * float32(*(*celt_norm)(unsafe.Add(unsafe.Pointer(x2), unsafe.Sizeof(celt_norm(0))*0))))
		if ctx.Resynth != 0 {
			var tmp celt_norm
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0)) = celt_norm(mid * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0))))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*1)) = celt_norm(mid * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*1))))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*0)) = celt_norm(side * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*0))))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*1)) = celt_norm(side * opus_val16(*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*1))))
			tmp = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*0)) = tmp - (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*0)))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*0)) = tmp + (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*0)))
			tmp = *(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*1))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(X), unsafe.Sizeof(celt_norm(0))*1)) = tmp - (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*1)))
			*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*1)) = tmp + (*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*1)))
		}
	} else {
		var rebalance int32
		if 0 > (func() int {
			if b < ((b - delta) / 2) {
				return b
			}
			return (b - delta) / 2
		}()) {
			mbits = 0
		} else if b < ((b - delta) / 2) {
			mbits = b
		} else {
			mbits = (b - delta) / 2
		}
		sbits = b - mbits
		ctx.Remaining_bits -= int32(qalloc)
		rebalance = ctx.Remaining_bits
		if mbits >= sbits {
			cm = quant_band(ctx, X, N, mbits, B, lowband, LM, lowband_out, Q15ONE, lowband_scratch, fill)
			rebalance = int32(mbits - (int(rebalance) - int(ctx.Remaining_bits)))
			if int(rebalance) > int(3<<BITRES) && itheta != 0 {
				sbits += int(rebalance) - (int(3 << BITRES))
			}
			cm |= quant_band(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>B)
		} else {
			cm = quant_band(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>B)
			rebalance = int32(sbits - (int(rebalance) - int(ctx.Remaining_bits)))
			if int(rebalance) > int(3<<BITRES) && itheta != 16384 {
				mbits += int(rebalance) - (int(3 << BITRES))
			}
			cm |= quant_band(ctx, X, N, mbits, B, lowband, LM, lowband_out, Q15ONE, lowband_scratch, fill)
		}
	}
	if ctx.Resynth != 0 {
		if N != 2 {
			stereo_merge(unsafe.Slice(X, N), unsafe.Slice(Y, N), mid, N, ctx.Arch)
		}
		if inv != 0 {
			var j int
			for j = 0; j < N; j++ {
				*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = -*(*celt_norm)(unsafe.Add(unsafe.Pointer(Y), unsafe.Sizeof(celt_norm(0))*uintptr(j)))
			}
		}
	}
	return cm
}
func special_hybrid_folding(m *OpusCustomMode, norm *celt_norm, norm2 *celt_norm, start int, M int, dual_stereo int) {
	var (
		n1     int
		n2     int
		eBands *int16 = m.EBands
	)
	n1 = M * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start)))))
	n2 = M * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start+2)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start+1)))))
	libc.MemCpy(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(n1)))), unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(n1*2-n2)))), (n2-n1)*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(n1)))))-uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(n1*2-n2)))))))*0))
	if dual_stereo != 0 {
		libc.MemCpy(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(n1)))), unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(n1*2-n2)))), (n2-n1)*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(n1)))))-uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(n1*2-n2)))))))*0))
	}
}
func quant_all_bands(encode int, m *OpusCustomMode, start int, end int, X_ *celt_norm, Y_ *celt_norm, collapse_masks *uint8, bandE *celt_ener, pulses *int, shortBlocks int, spread int, dual_stereo int, intensity int, tf_res *int, total_bits int32, balance int32, ec *ec_ctx, LM int, codedBands int, seed *uint32, complexity int, arch int, disable_inv int) {
	var (
		i                int
		remaining_bits   int32
		eBands           *int16 = m.EBands
		norm             *celt_norm
		norm2            *celt_norm
		_norm            *celt_norm
		_lowband_scratch *celt_norm
		X_save           *celt_norm
		Y_save           *celt_norm
		X_save2          *celt_norm
		Y_save2          *celt_norm
		norm_save2       *celt_norm
		resynth_alloc    int
		lowband_scratch  *celt_norm
		B                int
		M                int
		lowband_offset   int
		update_lowband   int = 1
		C                int
	)
	if Y_ != nil {
		C = 2
	} else {
		C = 1
	}
	var norm_offset int
	var theta_rdo int = int(libc.BoolToInt(encode != 0 && Y_ != nil && dual_stereo == 0 && complexity >= 8))
	var resynth int = int(libc.BoolToInt(encode == 0 || theta_rdo != 0))
	var ctx band_ctx
	M = 1 << LM
	if shortBlocks != 0 {
		B = M
	} else {
		B = 1
	}
	norm_offset = M * int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start))))
	_norm = (*celt_norm)(libc.Malloc((C * (M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(m.NbEBands-1)))) - norm_offset)) * int(unsafe.Sizeof(celt_norm(0)))))
	norm = _norm
	norm2 = (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(m.NbEBands-1)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
	if encode != 0 && resynth != 0 {
		resynth_alloc = M * (int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(m.NbEBands)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(m.NbEBands-1)))))
	} else {
		resynth_alloc = ALLOC_NONE
	}
	_lowband_scratch = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	if encode != 0 && resynth != 0 {
		lowband_scratch = _lowband_scratch
	} else {
		lowband_scratch = (*celt_norm)(unsafe.Add(unsafe.Pointer(X_), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(m.NbEBands-1)))))))
	}
	X_save = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	Y_save = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	X_save2 = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	Y_save2 = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	norm_save2 = (*celt_norm)(libc.Malloc(resynth_alloc * int(unsafe.Sizeof(celt_norm(0)))))
	lowband_offset = 0
	ctx.BandE = bandE
	ctx.Ec = ec
	ctx.Encode = encode
	ctx.Intensity = intensity
	ctx.M = m
	ctx.Seed = *seed
	ctx.Spread = spread
	ctx.Arch = arch
	ctx.Disable_inv = disable_inv
	ctx.Resynth = resynth
	ctx.Theta_round = 0
	ctx.Avoid_split_noise = int(libc.BoolToInt(B > 1))
	for i = start; i < end; i++ {
		var (
			tell              int32
			b                 int
			N                 int
			curr_balance      int32
			effective_lowband int = -1
			X                 *celt_norm
			Y                 *celt_norm
			tf_change         int = 0
			x_cm              uint
			y_cm              uint
			last              int
		)
		ctx.I = i
		last = int(libc.BoolToInt(i == end-1))
		X = (*celt_norm)(unsafe.Add(unsafe.Pointer(X_), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))
		if Y_ != nil {
			Y = (*celt_norm)(unsafe.Add(unsafe.Pointer(Y_), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))
		} else {
			Y = nil
		}
		N = M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i+1)))) - M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))
		tell = int32(ec_tell_frac(ec))
		if i != start {
			balance -= tell
		}
		remaining_bits = int32(int(total_bits) - int(tell) - 1)
		ctx.Remaining_bits = remaining_bits
		if i <= codedBands-1 {
			curr_balance = celt_sudiv(balance, int32(func() int {
				if 3 < (codedBands - i) {
					return 3
				}
				return codedBands - i
			}()))
			if 0 > (func() int {
				if 16383 < (func() int {
					if (int(remaining_bits) + 1) < (*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)) {
						return int(remaining_bits) + 1
					}
					return *(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)
				}()) {
					return 16383
				}
				if (int(remaining_bits) + 1) < (*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)) {
					return int(remaining_bits) + 1
				}
				return *(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)
			}()) {
				b = 0
			} else if 16383 < (func() int {
				if (int(remaining_bits) + 1) < (*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)) {
					return int(remaining_bits) + 1
				}
				return *(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)
			}()) {
				b = 16383
			} else if (int(remaining_bits) + 1) < (*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)) {
				b = int(remaining_bits) + 1
			} else {
				b = *(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(curr_balance)
			}
		} else {
			b = 0
		}
		if resynth != 0 && (M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))-N >= M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(start)))) || i == start+1) && (update_lowband != 0 || lowband_offset == 0) {
			lowband_offset = i
		}
		if i == start+1 {
			special_hybrid_folding(m, norm, norm2, start, M, dual_stereo)
		}
		tf_change = *(*int)(unsafe.Add(unsafe.Pointer(tf_res), unsafe.Sizeof(int(0))*uintptr(i)))
		ctx.Tf_change = tf_change
		if i >= m.EffEBands {
			X = norm
			if Y_ != nil {
				Y = norm
			}
			lowband_scratch = nil
		}
		if last != 0 && theta_rdo == 0 {
			lowband_scratch = nil
		}
		if lowband_offset != 0 && (spread != 3 || B > 1 || tf_change < 0) {
			var (
				fold_start int
				fold_end   int
				fold_i     int
			)
			if 0 > (M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(lowband_offset)))) - norm_offset - N) {
				effective_lowband = 0
			} else {
				effective_lowband = M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(lowband_offset)))) - norm_offset - N
			}
			fold_start = lowband_offset
			for M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(func() int {
				p := &fold_start
				*p--
				return *p
			}())))) > effective_lowband+norm_offset {
			}
			fold_end = lowband_offset - 1
			for func() int {
				p := &fold_end
				*p++
				return *p
			}() < i && M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(fold_end)))) < effective_lowband+norm_offset+N {
			}
			x_cm = func() uint {
				y_cm = 0
				return y_cm
			}()
			fold_i = fold_start
			for {
				x_cm |= uint(*(*uint8)(unsafe.Add(unsafe.Pointer(collapse_masks), fold_i*C+0)))
				y_cm |= uint(*(*uint8)(unsafe.Add(unsafe.Pointer(collapse_masks), fold_i*C+C-1)))
				if func() int {
					p := &fold_i
					*p++
					return *p
				}() >= fold_end {
					break
				}
			}
		} else {
			x_cm = func() uint {
				y_cm = uint((1 << B) - 1)
				return y_cm
			}()
		}
		if dual_stereo != 0 && i == intensity {
			var j int
			dual_stereo = 0
			if resynth != 0 {
				for j = 0; j < M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i))))-norm_offset; j++ {
					*(*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(j))) = (*(*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(j))) + *(*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(j)))) * celt_norm(0.5)
				}
			}
		}
		if dual_stereo != 0 {
			x_cm = quant_band(&ctx, X, N, b/2, B, func() *celt_norm {
				if effective_lowband != -1 {
					return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
				}
				return nil
			}(), LM, func() *celt_norm {
				if last != 0 {
					return nil
				}
				return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
			}(), Q15ONE, lowband_scratch, int(x_cm))
			y_cm = quant_band(&ctx, Y, N, b/2, B, func() *celt_norm {
				if effective_lowband != -1 {
					return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
				}
				return nil
			}(), LM, func() *celt_norm {
				if last != 0 {
					return nil
				}
				return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm2), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
			}(), Q15ONE, lowband_scratch, int(y_cm))
		} else {
			if Y != nil {
				if theta_rdo != 0 && i < intensity {
					var (
						ec_save      ec_ctx
						ec_save2     ec_ctx
						ctx_save     band_ctx
						ctx_save2    band_ctx
						dist0        opus_val32
						dist1        opus_val32
						cm           uint
						cm2          uint
						nstart_bytes int
						nend_bytes   int
						save_bytes   int
						bytes_buf    *uint8
						bytes_save   [1275]uint8
						w            [2]opus_val16
					)
					compute_channel_weights(*(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i))), *(*celt_ener)(unsafe.Add(unsafe.Pointer(bandE), unsafe.Sizeof(celt_ener(0))*uintptr(i+m.NbEBands))), w)
					cm = x_cm | y_cm
					ec_save = *ec
					ctx_save = ctx
					libc.MemCpy(unsafe.Pointer(X_save), unsafe.Pointer(X), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X_save))-uintptr(unsafe.Pointer(X))))*0))
					libc.MemCpy(unsafe.Pointer(Y_save), unsafe.Pointer(Y), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(Y_save))-uintptr(unsafe.Pointer(Y))))*0))
					ctx.Theta_round = -1
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B, func() *celt_norm {
						if effective_lowband != -1 {
							return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
						}
						return nil
					}(), LM, func() *celt_norm {
						if last != 0 {
							return nil
						}
						return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
					}(), lowband_scratch, int(cm))
					dist0 = opus_val32(((w[0]) * opus_val16(func() opus_val32 {
						_ = arch
						return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(X_save)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), N)
					}())) + (w[1])*opus_val16(func() opus_val32 {
						_ = arch
						return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(Y_save)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(Y)), N), N)
					}()))
					cm2 = x_cm
					ec_save2 = *ec
					ctx_save2 = ctx
					libc.MemCpy(unsafe.Pointer(X_save2), unsafe.Pointer(X), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X_save2))-uintptr(unsafe.Pointer(X))))*0))
					libc.MemCpy(unsafe.Pointer(Y_save2), unsafe.Pointer(Y), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(Y_save2))-uintptr(unsafe.Pointer(Y))))*0))
					if last == 0 {
						libc.MemCpy(unsafe.Pointer(norm_save2), unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(norm_save2))-uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))))))*0))
					}
					nstart_bytes = int(ec_save.Offs)
					nend_bytes = int(ec_save.Storage)
					bytes_buf = (*uint8)(unsafe.Pointer(&ec_save.Buf[nstart_bytes]))
					save_bytes = nend_bytes - nstart_bytes
					libc.MemCpy(unsafe.Pointer(&bytes_save[0]), unsafe.Pointer(bytes_buf), save_bytes*int(unsafe.Sizeof(uint8(0)))+int((int64(uintptr(unsafe.Pointer(&bytes_save[0]))-uintptr(unsafe.Pointer(bytes_buf))))*0))
					*ec = ec_save
					ctx = ctx_save
					libc.MemCpy(unsafe.Pointer(X), unsafe.Pointer(X_save), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X))-uintptr(unsafe.Pointer(X_save))))*0))
					libc.MemCpy(unsafe.Pointer(Y), unsafe.Pointer(Y_save), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(Y))-uintptr(unsafe.Pointer(Y_save))))*0))
					if i == start+1 {
						special_hybrid_folding(m, norm, norm2, start, M, dual_stereo)
					}
					ctx.Theta_round = 1
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B, func() *celt_norm {
						if effective_lowband != -1 {
							return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
						}
						return nil
					}(), LM, func() *celt_norm {
						if last != 0 {
							return nil
						}
						return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
					}(), lowband_scratch, int(cm))
					dist1 = opus_val32(((w[0]) * opus_val16(func() opus_val32 {
						_ = arch
						return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(X_save)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(X)), N), N)
					}())) + (w[1])*opus_val16(func() opus_val32 {
						_ = arch
						return celt_inner_prod_c(unsafe.Slice((*opus_val16)(unsafe.Pointer(Y_save)), N), unsafe.Slice((*opus_val16)(unsafe.Pointer(Y)), N), N)
					}()))
					if dist0 >= dist1 {
						x_cm = cm2
						*ec = ec_save2
						ctx = ctx_save2
						libc.MemCpy(unsafe.Pointer(X), unsafe.Pointer(X_save2), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(X))-uintptr(unsafe.Pointer(X_save2))))*0))
						libc.MemCpy(unsafe.Pointer(Y), unsafe.Pointer(Y_save2), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer(Y))-uintptr(unsafe.Pointer(Y_save2))))*0))
						if last == 0 {
							libc.MemCpy(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))), unsafe.Pointer(norm_save2), N*int(unsafe.Sizeof(celt_norm(0)))+int((int64(uintptr(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))))-uintptr(unsafe.Pointer(norm_save2))))*0))
						}
						libc.MemCpy(unsafe.Pointer(bytes_buf), unsafe.Pointer(&bytes_save[0]), save_bytes*int(unsafe.Sizeof(uint8(0))))
					}
				} else {
					ctx.Theta_round = 0
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B, func() *celt_norm {
						if effective_lowband != -1 {
							return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
						}
						return nil
					}(), LM, func() *celt_norm {
						if last != 0 {
							return nil
						}
						return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
					}(), lowband_scratch, int(x_cm|y_cm))
				}
			} else {
				x_cm = quant_band(&ctx, X, N, b, B, func() *celt_norm {
					if effective_lowband != -1 {
						return (*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(effective_lowband)))
					}
					return nil
				}(), LM, func() *celt_norm {
					if last != 0 {
						return nil
					}
					return (*celt_norm)(unsafe.Add(unsafe.Pointer((*celt_norm)(unsafe.Add(unsafe.Pointer(norm), unsafe.Sizeof(celt_norm(0))*uintptr(M*int(*(*int16)(unsafe.Add(unsafe.Pointer(eBands), unsafe.Sizeof(int16(0))*uintptr(i)))))))), -int(unsafe.Sizeof(celt_norm(0))*uintptr(norm_offset))))
				}(), Q15ONE, lowband_scratch, int(x_cm|y_cm))
			}
			y_cm = x_cm
		}
		*(*uint8)(unsafe.Add(unsafe.Pointer(collapse_masks), i*C+0)) = uint8(x_cm)
		*(*uint8)(unsafe.Add(unsafe.Pointer(collapse_masks), i*C+C-1)) = uint8(y_cm)
		balance += int32(*(*int)(unsafe.Add(unsafe.Pointer(pulses), unsafe.Sizeof(int(0))*uintptr(i))) + int(tell))
		update_lowband = int(libc.BoolToInt(b > (N << BITRES)))
		ctx.Avoid_split_noise = 0
	}
	*seed = ctx.Seed
}
