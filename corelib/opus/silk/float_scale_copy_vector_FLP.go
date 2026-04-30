package silk

func silk_scale_copy_vector_FLP(data_out []float32, data_in []float32, gain float32, dataSize int) {
	var (
		i         int
		dataSize4 int
	)
	dataSize4 = dataSize & 0xFFFC
	for i = 0; i < dataSize4; i += 4 {
		data_out[i+0] = gain * data_in[i+0]
		data_out[i+1] = gain * data_in[i+1]
		data_out[i+2] = gain * data_in[i+2]
		data_out[i+3] = gain * data_in[i+3]
	}
	for ; i < dataSize; i++ {
		data_out[i] = gain * data_in[i]
	}
}
