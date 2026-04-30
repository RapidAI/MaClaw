package libopus

func silk_schur_FLP(refl_coef []float32, auto_corr []float32, order int) float32 {
	var (
		k      int
		n      int
		C      [25][2]float64
		Ctmp1  float64
		Ctmp2  float64
		rc_tmp float64
	)
	k = 0
	for {
		C[k][0] = func() float64 {
			p := &C[k][1]
			C[k][1] = float64(auto_corr[k])
			return *p
		}()
		if func() int {
			p := &k
			*p++
			return *p
		}() > order {
			break
		}
	}
	for k = 0; k < order; k++ {
		rc_tmp = -C[k+1][0] / (func() float64 {
			if (C[0][1]) > 1e-09 {
				return C[0][1]
			}
			return 1e-09
		}())
		refl_coef[k] = float32(rc_tmp)
		for n = 0; n < order-k; n++ {
			Ctmp1 = C[n+k+1][0]
			Ctmp2 = C[n][1]
			C[n+k+1][0] = Ctmp1 + Ctmp2*rc_tmp
			C[n][1] = Ctmp2 + Ctmp1*rc_tmp
		}
	}
	return float32(C[0][1])
}
