package silk

func silk_corrVector_FLP(x []float32, t []float32, L int, Order int, Xt []float32) {
	for lag := 0; lag < Order; lag++ {
		Xt[lag] = float32(silk_inner_product_FLP(x[Order-lag-1:], t, L))
	}
}
func silk_corrMatrix_FLP(x []float32, L int, Order int, XX []float32) {
	var energy float64
	ptr1i := Order - 1
	energy = silk_energy_FLP(x[ptr1i:], L)
	XX[Order*0+0] = float32(energy)
	for j := 1; j < Order; j++ {
		energy += float64(x[ptr1i-j]*x[ptr1i-j] - x[ptr1i+L-j]*x[ptr1i+L-j])
		XX[j*Order+j] = float32(energy)
	}
	ptr2i := Order - 2
	for lag := 1; lag < Order; lag++ {
		energy = silk_inner_product_FLP(x[ptr1i:], x[ptr2i:], L)
		XX[lag*Order+0] = float32(energy)
		XX[Order*0+lag] = float32(energy)
		for j := 1; j < (Order - lag); j++ {
			energy += float64(x[ptr1i-j]*x[ptr2i-j] - x[ptr1i+L-j]*x[ptr2i+L-j])
			XX[(lag+j)*Order+j] = float32(energy)
			XX[j*Order+(lag+j)] = float32(energy)
		}
		ptr2i--
	}
}
