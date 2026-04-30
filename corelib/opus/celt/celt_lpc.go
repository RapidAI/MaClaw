package celt

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

const LPC_ORDER = 24

func _celt_lpc(_lpc []opus_val16, ac []opus_val32, p int) {
	var (
		i     int
		j     int
		r     opus_val32
		error opus_val32 = ac[0]
		lpc   []float32  = ([]float32)(_lpc)
	)
	libc.MemSet(unsafe.Pointer(&lpc[0]), 0, p*int(unsafe.Sizeof(float32(0))))
	if ac[0] > 1e-10 {
		for i = 0; i < p; i++ {
			var rr opus_val32 = 0
			for j = 0; j < i; j++ {
				rr += opus_val32((lpc[j]) * float32(ac[i-j]))
			}
			rr += ac[i+1]
			r = opus_val32(-(float32(rr) / float32(error)))
			lpc[i] = float32(r)
			for j = 0; j < (i+1)>>1; j++ {
				var (
					tmp1 opus_val32
					tmp2 opus_val32
				)
				tmp1 = opus_val32(lpc[j])
				tmp2 = opus_val32(lpc[i-1-j])
				lpc[j] = float32(tmp1 + r*tmp2)
				lpc[i-1-j] = float32(tmp2 + r*tmp1)
			}
			error = error - (r*r)*error
			if error <= ac[0]*opus_val32(0.001) {
				break
			}
		}
	}
}
func celt_fir_c(x []opus_val16, num []opus_val16, y []opus_val16, N int, ord int, arch int) {
	var (
		i int
		j int
	)
	rnum := make([]opus_val16, ord)
	for i = 0; i < ord; i++ {
		rnum[i] = num[ord-i-1]
	}
	for i = 0; i < N-3; i += 4 {
		var sum [4]opus_val32
		sum[0] = opus_val32(x[i])
		sum[1] = opus_val32(x[i+1])
		sum[2] = opus_val32(x[i+2])
		sum[3] = opus_val32(x[i+3])
		_ = arch
		xcorr_kernel_c(rnum, x[i-ord:], sum, ord)
		y[i] = opus_val16(sum[0])
		y[i+1] = opus_val16(sum[1])
		y[i+2] = opus_val16(sum[2])
		y[i+3] = opus_val16(sum[3])
	}
	for ; i < N; i++ {
		var sum opus_val32 = opus_val32(x[i])
		for j = 0; j < ord; j++ {
			sum = sum + opus_val32(rnum[j])*opus_val32(x[i+j-ord])
		}
		y[i] = opus_val16(sum)
	}
}
func celt_iir(_x []opus_val32, den []opus_val16, _y []opus_val32, N int, ord int, mem []opus_val16, arch int) {
	var (
		i int
		j int
	)
	rden := make([]opus_val16, ord)
	y := make([]opus_val16, N+ord)
	for i = 0; i < ord; i++ {
		rden[i] = den[ord-i-1]
	}
	for i = 0; i < ord; i++ {
		y[i] = -mem[ord-i-1]
	}
	for ; i < N+ord; i++ {
		y[i] = 0
	}
	for i = 0; i < N-3; i += 4 {
		var sum [4]opus_val32
		sum[0] = _x[i]
		sum[1] = _x[i+1]
		sum[2] = _x[i+2]
		sum[3] = _x[i+3]
		_ = arch
		xcorr_kernel_c(rden, y[i:], sum, ord)
		y[i+ord] = opus_val16(-(sum[0]))
		_y[i] = sum[0]
		sum[1] = (sum[1]) + opus_val32(y[i+ord])*opus_val32(den[0])
		y[i+ord+1] = opus_val16(-(sum[1]))
		_y[i+1] = sum[1]
		sum[2] = (sum[2]) + opus_val32(y[i+ord+1])*opus_val32(den[0])
		sum[2] = (sum[2]) + opus_val32(y[i+ord])*opus_val32(den[1])
		y[i+ord+2] = opus_val16(-(sum[2]))
		_y[i+2] = sum[2]
		sum[3] = (sum[3]) + opus_val32(y[i+ord+2])*opus_val32(den[0])
		sum[3] = (sum[3]) + opus_val32(y[i+ord+1])*opus_val32(den[1])
		sum[3] = (sum[3]) + opus_val32(y[i+ord])*opus_val32(den[2])
		y[i+ord+3] = opus_val16(-(sum[3]))
		_y[i+3] = sum[3]
	}
	for ; i < N; i++ {
		var sum opus_val32 = _x[i]
		for j = 0; j < ord; j++ {
			sum -= opus_val32(rden[j]) * opus_val32(y[i+j])
		}
		y[i+ord] = opus_val16(sum)
		_y[i] = sum
	}
	for i = 0; i < ord; i++ {
		mem[i] = opus_val16(_y[N-i-1])
	}
}
func _celt_autocorr(x []opus_val16, ac []opus_val32, window []opus_val16, overlap int, lag int, n int, arch int) int {
	var (
		d     opus_val32
		i     int
		k     int
		fastN = n - lag
		shift int
		xptr  []opus_val16
	)
	xx := make([]opus_val16, n)
	if overlap == 0 {
		xptr = x
	} else {
		for i = 0; i < n; i++ {
			xx[i] = x[i]
		}
		for i = 0; i < overlap; i++ {
			xx[i] = (x[i]) * (window[i])
			xx[n-i-1] = (x[n-i-1]) * (window[i])
		}
		xptr = xx
	}
	shift = 0
	PitchXcorrC(xptr, xptr, ac, fastN, lag+1, arch)
	for k = 0; k <= lag; k++ {
		for func() opus_val32 {
			i = k + fastN
			return func() opus_val32 {
				d = 0
				return d
			}()
		}(); i < n; i++ {
			d = d + opus_val32(xptr[i])*opus_val32(xptr[i-k])
		}
		ac[k] += d
	}
	return shift
}
