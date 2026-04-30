package silk

func silk_insertion_sort_decreasing_FLP(a []float32, idx []int, L int, K int) {
	var (
		value float32
		i     int
		j     int
	)
	for i = 0; i < K; i++ {
		idx[i] = i
	}
	for i = 1; i < K; i++ {
		value = a[i]
		for j = i - 1; j >= 0 && value > a[j]; j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
	for i = K; i < L; i++ {
		value = a[i]
		if value > a[K-1] {
			for j = K - 2; j >= 0 && value > a[j]; j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}
