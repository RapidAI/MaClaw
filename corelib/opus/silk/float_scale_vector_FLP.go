package silk

func silk_scale_vector_FLP(data1 []float32, gain float32, dataSize int) {
	var (
		i         int
		dataSize4 int
	)
	dataSize4 = dataSize & 0xFFFC
	for i = 0; i < dataSize4; i += 4 {
		data1[i+0] *= gain
		data1[i+1] *= gain
		data1[i+2] *= gain
		data1[i+3] *= gain
	}
	for ; i < dataSize; i++ {
		data1[i] *= gain
	}
}
