package silk

func silk_inner_product_FLP(data1 []float32, data2 []float32, dataSize int) float64 {
	var (
		i      int
		result float64
	)
	result = 0.0
	for i = 0; i < dataSize-3; i += 4 {
		result += float64(data1[i+0])*float64(data2[i+0]) + float64(data1[i+1])*float64(data2[i+1]) + float64(data1[i+2])*float64(data2[i+2]) + float64(data1[i+3])*float64(data2[i+3])
	}
	for ; i < dataSize; i++ {
		result += float64(data1[i]) * float64(data2[i])
	}
	return result
}
