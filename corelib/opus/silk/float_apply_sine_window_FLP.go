package silk

func silk_apply_sine_window_FLP(px_win []float32, px []float32, win_type int, length int) {
	var (
		k    int
		freq float32
		c    float32
		S0   float32
		S1   float32
	)
	freq = float32(3.1415926536 / float64(length+1))
	c = 2.0 - freq*freq
	if win_type < 2 {
		S0 = 0.0
		S1 = freq
	} else {
		S0 = 1.0
		S1 = c * 0.5
	}
	for k = 0; k < length; k += 4 {
		px_win[k+0] = px[k+0] * 0.5 * (S0 + S1)
		px_win[k+1] = px[k+1] * S1
		S0 = c*S1 - S0
		px_win[k+2] = px[k+2] * 0.5 * (S1 + S0)
		px_win[k+3] = px[k+3] * S0
		S1 = c*S0 - S1
	}
}
