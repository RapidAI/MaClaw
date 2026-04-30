package libopus

func silk_interpolate(xi [16]int16, x0 [16]int16, x1 [16]int16, ifact_Q2 int, d int) {
	var i int
	for i = 0; i < d; i++ {
		xi[i] = int16(int(x0[i]) + ((int(int32(int16(int(x1[i])-int(x0[i])))) * int(int32(int16(ifact_Q2)))) >> 2))
	}
}
