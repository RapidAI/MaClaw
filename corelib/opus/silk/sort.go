package silk

func silk_insertion_sort_increasing(a []int32, idx []int, L int, K int) {
	var (
		value int32
		i     int
		j     int
	)
	for i = 0; i < K; i++ {
		idx[i] = i
	}
	for i = 1; i < K; i++ {
		value = a[i]
		for j = i - 1; j >= 0 && int(value) < int(a[j]); j-- {
			a[j+1] = a[j]
			idx[j+1] = idx[j]
		}
		a[j+1] = value
		idx[j+1] = i
	}
	for i = K; i < L; i++ {
		value = a[i]
		if int(value) < int(a[K-1]) {
			for j = K - 2; j >= 0 && int(value) < int(a[j]); j-- {
				a[j+1] = a[j]
				idx[j+1] = idx[j]
			}
			a[j+1] = value
			idx[j+1] = i
		}
	}
}
func silk_insertion_sort_increasing_all_values_int16(a []int16, L int) {
	var (
		value int
		i     int
		j     int
	)
	for i = 1; i < L; i++ {
		value = int(a[i])
		for j = i - 1; j >= 0 && value < int(a[j]); j-- {
			a[j+1] = a[j]
		}
		a[j+1] = int16(value)
	}
}
