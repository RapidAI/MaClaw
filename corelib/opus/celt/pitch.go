package celt

import (
	"math"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/cmath"
)

func xcorr_kernel_c(x []opus_val16, y []opus_val16, sum [4]opus_val32, len_ int) {
	var (
		j   int
		y_0 opus_val16
		y_1 opus_val16
		y_2 opus_val16
		y_3 opus_val16
	)
	y_3 = 0
	y_0 = y[0]
	y = y[1:]
	y_1 = y[0]
	y = y[1:]
	y_2 = y[0]
	y = y[1:]
	for j = 0; j < len_-3; j += 4 {
		tmp := x[0]
		x = x[1:]
		y_3 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_0)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_1)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_2)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_3)
		tmp = x[0]
		x = x[1:]
		y_0 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_1)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_2)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_3)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_0)
		tmp = x[0]
		x = x[1:]
		y_1 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_2)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_3)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_0)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_1)
		tmp = x[0]
		x = x[1:]
		y_2 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_3)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_0)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_1)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_2)
	}
	if func() int {
		p := &j
		x := *p
		*p++
		return x
	}() < len_ {
		tmp := x[0]
		x = x[1:]
		y_3 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_0)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_1)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_2)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_3)
	}
	if func() int {
		p := &j
		x := *p
		*p++
		return x
	}() < len_ {
		tmp := x[0]
		x = x[1:]
		y_0 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_1)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_2)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_3)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_0)
	}
	if j < len_ {
		tmp := x[0]
		x = x[1:]
		y_1 = y[0]
		y = y[1:]
		sum[0] = (sum[0]) + opus_val32(tmp)*opus_val32(y_2)
		sum[1] = (sum[1]) + opus_val32(tmp)*opus_val32(y_3)
		sum[2] = (sum[2]) + opus_val32(tmp)*opus_val32(y_0)
		sum[3] = (sum[3]) + opus_val32(tmp)*opus_val32(y_1)
	}
}
func dual_inner_prod_c(x []opus_val16, y01 []opus_val16, y02 []opus_val16, N int, xy1 *opus_val32, xy2 *opus_val32) {
	var (
		xy01 opus_val32
		xy02 opus_val32
	)
	for i := 0; i < N; i++ {
		xy01 = xy01 + opus_val32(x[i])*opus_val32(y01[i])
		xy02 = xy02 + opus_val32(x[i])*opus_val32(y02[i])
	}
	*xy1 = xy01
	*xy2 = xy02
}
func celt_inner_prod_c(x []opus_val16, y []opus_val16, N int) opus_val32 {
	var xy opus_val32
	for i := 0; i < N; i++ {
		xy = xy + opus_val32(x[i])*opus_val32(y[i])
	}
	return xy
}
func find_best_pitch(xcorr []opus_val32, y []opus_val16, len_ int, max_pitch int, best_pitch []int) {
	var (
		i        int
		j        int
		Syy      opus_val32 = 1
		best_num [2]opus_val16
		best_den [2]opus_val32
	)
	best_num[0] = opus_val16(-1)
	best_num[1] = opus_val16(-1)
	best_den[0] = 0
	best_den[1] = 0
	best_pitch[0] = 0
	best_pitch[1] = 1
	for j = 0; j < len_; j++ {
		Syy = Syy + opus_val32(y[j])*opus_val32(y[j])
	}
	for i = 0; i < max_pitch; i++ {
		if float32(xcorr[i]) > 0 {
			var (
				num     opus_val16
				xcorr16 opus_val32
			)
			xcorr16 = xcorr[i]
			xcorr16 *= opus_val32(1e-12)
			num = opus_val16(xcorr16 * xcorr16)
			if (num * opus_val16(best_den[1])) > ((best_num[1]) * opus_val16(Syy)) {
				if (num * opus_val16(best_den[0])) > ((best_num[0]) * opus_val16(Syy)) {
					best_num[1] = best_num[0]
					best_den[1] = best_den[0]
					best_pitch[1] = best_pitch[0]
					best_num[0] = num
					best_den[0] = Syy
					best_pitch[0] = i
				} else {
					best_num[1] = num
					best_den[1] = Syy
					best_pitch[1] = i
				}
			}
		}
		Syy += (opus_val32(y[i+len_]) * opus_val32(y[i+len_])) - opus_val32(y[i])*opus_val32(y[i])
		if 1 > float32(Syy) {
			Syy = 1
		} else {
			Syy = Syy
		}
	}
}
func celt_fir5(x []opus_val16, num []opus_val16, N int) {
	var (
		i    int
		num0 opus_val16
		num1 opus_val16
		num2 opus_val16
		num3 opus_val16
		num4 opus_val16
		mem0 opus_val32
		mem1 opus_val32
		mem2 opus_val32
		mem3 opus_val32
		mem4 opus_val32
	)
	num0 = num[0]
	num1 = num[1]
	num2 = num[2]
	num3 = num[3]
	num4 = num[4]
	mem0 = 0
	mem1 = 0
	mem2 = 0
	mem3 = 0
	mem4 = 0
	for i = 0; i < N; i++ {
		var sum opus_val32 = opus_val32(x[i])
		sum = sum + opus_val32(num0)*mem0
		sum = sum + opus_val32(num1)*mem1
		sum = sum + opus_val32(num2)*mem2
		sum = sum + opus_val32(num3)*mem3
		sum = sum + opus_val32(num4)*mem4
		mem4 = mem3
		mem3 = mem2
		mem2 = mem1
		mem1 = mem0
		mem0 = opus_val32(x[i])
		x[i] = opus_val16(sum)
	}
}
func pitch_downsample(x [][]celt_sig, x_lp []opus_val16, len_ int, C int, arch int) {
	var (
		i    int
		ac   [5]opus_val32
		tmp  opus_val16 = Q15ONE
		lpc  [4]opus_val16
		lpc2 [5]opus_val16
		c1   opus_val16 = 0.8
	)
	for i = 1; i < len_>>1; i++ {
		x_lp[i] = opus_val16(x[0][i*2-1]*celt_sig(0.25) + x[0][i*2+1]*celt_sig(0.25) + x[0][i*2]*celt_sig(0.5))
	}
	x_lp[0] = opus_val16(x[0][1]*celt_sig(0.25) + x[0][0]*celt_sig(0.5))
	if C == 2 {
		for i = 1; i < len_>>1; i++ {
			x_lp[i] += opus_val16(x[1][i*2-1]*celt_sig(0.25) + x[1][i*2+1]*celt_sig(0.25) + x[1][i*2]*celt_sig(0.5))
		}
		x_lp[0] += opus_val16(x[1][1]*celt_sig(0.25) + x[1][0]*celt_sig(0.5))
	}
	_celt_autocorr(x_lp, ac[:], nil, 0, 4, len_>>1, arch)
	ac[0] *= opus_val32(1.0001)
	for i = 1; i <= 4; i++ {
		ac[i] -= opus_val32(float64(ac[i]) * (float64(i) * 0.008) * (float64(i) * 0.008))
	}
	_celt_lpc(lpc[:], ac[:], 4)
	for i = 0; i < 4; i++ {
		tmp = tmp * 0.9
		lpc[i] = (lpc[i]) * tmp
	}
	lpc2[0] = lpc[0] + 0.8
	lpc2[1] = lpc[1] + c1*(lpc[0])
	lpc2[2] = lpc[2] + c1*(lpc[1])
	lpc2[3] = lpc[3] + c1*(lpc[2])
	lpc2[4] = c1 * (lpc[3])
	celt_fir5(x_lp, lpc2[:], len_>>1)
}
func PitchXcorrC(_x []opus_val16, _y []opus_val16, xcorr []opus_val32, len_ int, max_pitch int, arch int) {
	var i int
	for i = 0; i < max_pitch-3; i += 4 {
		var sum [4]opus_val32
		xcorr_kernel_c(_x, _y[i:], sum, len_)
		xcorr[i+0] = sum[0]
		xcorr[i+1] = sum[1]
		xcorr[i+2] = sum[2]
		xcorr[i+3] = sum[3]
	}
	for ; i < max_pitch; i++ {
		xcorr[i] = celt_inner_prod_c(_x, _y[i:], len_)
	}
}
func pitch_search(x_lp []opus_val16, y []opus_val16, len_ int, max_pitch int, pitch []int, arch int) {
	var (
		lag        int
		best_pitch [2]int
		x_lp4      []opus_val16
		y_lp4      []opus_val16
		xcorr      []opus_val32
		offset     int
	)
	lag = len_ + max_pitch
	x_lp4 = make([]opus_val16, len_>>2)
	y_lp4 = make([]opus_val16, lag>>2)
	xcorr = make([]opus_val32, max_pitch>>1)
	for j := 0; j < len_>>2; j++ {
		x_lp4[j] = x_lp[j*2]
	}
	for j := 0; j < lag>>2; j++ {
		y_lp4[j] = y[j*2]
	}
	PitchXcorrC(x_lp4, y_lp4, xcorr, len_>>2, max_pitch>>2, arch)
	find_best_pitch(xcorr, y_lp4, len_>>2, max_pitch>>2, best_pitch[:])
	for i := 0; i < max_pitch>>1; i++ {
		var sum opus_val32
		xcorr[i] = 0
		if cmath.Abs(int64(i-best_pitch[0]*2)) > 2 && cmath.Abs(int64(i-best_pitch[1]*2)) > 2 {
			continue
		}
		sum = func() opus_val32 {
			_ = arch
			return celt_inner_prod_c(x_lp, y[i:], len_>>1)
		}()
		if -1 > sum {
			xcorr[i] = -1
		} else {
			xcorr[i] = sum
		}
	}
	find_best_pitch(xcorr, y, len_>>1, max_pitch>>1, best_pitch[:])
	if best_pitch[0] > 0 && best_pitch[0] < (max_pitch>>1)-1 {
		a := xcorr[best_pitch[0]-1]
		b := xcorr[best_pitch[0]+0]
		c := xcorr[best_pitch[0]+1]
		if (c - a) > ((b - a) * opus_val32(0.7)) {
			offset = 1
		} else if (a - c) > ((b - c) * opus_val32(0.7)) {
			offset = -1
		} else {
			offset = 0
		}
	} else {
		offset = 0
	}
	pitch[0] = best_pitch[0]*2 - offset
}
func compute_pitch_gain(xy opus_val32, xx opus_val32, yy opus_val32) opus_val16 {
	return opus_val16(xy / opus_val32(float32(math.Sqrt(float64(float32(xx*yy)+1)))))
}

var second_check [16]int = [16]int{0, 0, 3, 2, 3, 2, 5, 2, 3, 2, 3, 2, 5, 2, 3, 2}

func remove_doubling(x []opus_val16, maxperiod int, minperiod int, N int, T0_ *int, prev_period int, prev_gain opus_val16, arch int) opus_val16 {
	var (
		k          int
		i          int
		T          int
		T0         int
		g          opus_val16
		g0         opus_val16
		pg         opus_val16
		xy         opus_val32
		xx         opus_val32
		yy         opus_val32
		xy2        opus_val32
		xcorr      [3]opus_val32
		best_xy    opus_val32
		best_yy    opus_val32
		offset     int
		minperiod0 int
		yy_lookup  []opus_val32
	)
	minperiod0 = minperiod
	maxperiod /= 2
	minperiod /= 2
	*T0_ /= 2
	prev_period /= 2
	N /= 2
	x = x[maxperiod:]
	if *T0_ >= maxperiod {
		*T0_ = maxperiod - 1
	}
	T = func() int {
		T0 = *T0_
		return T0
	}()
	yy_lookup = make([]opus_val32, maxperiod+1)
	_ = arch
	// FIXME
	dual_inner_prod_c(x, x, x[-T0:], N, &xx, &xy)
	yy_lookup[0] = xx
	yy = xx
	for i = 1; i <= maxperiod; i++ {
		yy = yy + opus_val32(x[-i])*opus_val32(x[-i]) - opus_val32(x[N-i])*opus_val32(x[N-i])
		if 0 > float32(yy) {
			yy_lookup[i] = 0
		} else {
			yy_lookup[i] = yy
		}
	}
	yy = yy_lookup[T0]
	best_xy = xy
	best_yy = yy
	g = func() opus_val16 {
		g0 = compute_pitch_gain(xy, xx, yy)
		return g0
	}()
	for k = 2; k <= 15; k++ {
		var (
			T1     int
			T1b    int
			g1     opus_val16
			cont   opus_val16 = 0
			thresh opus_val16
		)
		T1 = int(uint32(int32(T0*2+k)) / uint32(int32(k*2)))
		if T1 < minperiod {
			break
		}
		if k == 2 {
			if T1+T0 > maxperiod {
				T1b = T0
			} else {
				T1b = T0 + T1
			}
		} else {
			T1b = int(uint32(int32(second_check[k]*2*T0+k)) / uint32(int32(k*2)))
		}
		_ = arch
		// FIXME
		dual_inner_prod_c(x, x[-T1:], x[-T1b:], N, &xy, &xy2)
		xy = (xy + xy2) * 0.5
		yy = (yy_lookup[T1] + yy_lookup[T1b]) * 0.5
		g1 = compute_pitch_gain(xy, xx, yy)
		if cmath.Abs(int64(T1-prev_period)) <= 1 {
			cont = prev_gain
		} else if cmath.Abs(int64(T1-prev_period)) <= 2 && k*5*k < T0 {
			cont = prev_gain * 0.5
		} else {
			cont = 0
		}
		if 0.3 > ((g0 * 0.7) - cont) {
			thresh = 0.3
		} else {
			thresh = (g0 * 0.7) - cont
		}
		if T1 < minperiod*3 {
			if 0.4 > ((g0 * 0.85) - cont) {
				thresh = 0.4
			} else {
				thresh = (g0 * 0.85) - cont
			}
		} else if T1 < minperiod*2 {
			if 0.5 > ((g0 * 0.9) - cont) {
				thresh = 0.5
			} else {
				thresh = (g0 * 0.9) - cont
			}
		}
		if g1 > thresh {
			best_xy = xy
			best_yy = yy
			T = T1
			g = g1
		}
	}
	if 0 > float32(best_xy) {
		best_xy = 0
	} else {
		best_xy = best_xy
	}
	if best_yy <= best_xy {
		pg = Q15ONE
	} else {
		pg = opus_val16(float32(best_xy) / (float32(best_yy) + 1))
	}
	for k = 0; k < 3; k++ {
		xcorr[k] = func() opus_val32 {
			_ = arch
			// FIXME
			return celt_inner_prod_c(x, x[-(T+k-1):], N)
		}()
	}
	if (xcorr[2] - xcorr[0]) > ((xcorr[1] - xcorr[0]) * 0.7) {
		offset = 1
	} else if (xcorr[0] - xcorr[2]) > ((xcorr[1] - xcorr[2]) * 0.7) {
		offset = -1
	} else {
		offset = 0
	}
	if pg > g {
		pg = g
	}
	*T0_ = T*2 + offset
	if *T0_ < minperiod0 {
		*T0_ = minperiod0
	}
	return pg
}
