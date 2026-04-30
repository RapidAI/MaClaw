package silk

func silk_bwexpander_FLP(ar []float32, d int, chirp float32) {
	var (
		i    int
		cfac float32 = chirp
	)
	for i = 0; i < d-1; i++ {
		ar[i] *= cfac
		cfac *= chirp
	}
	ar[d-1] *= cfac
}
