package silk

func silk_autocorrelation_FLP(results []float32, inputData []float32, inputDataSize int, correlationCount int) {
	if correlationCount > inputDataSize {
		correlationCount = inputDataSize
	}
	for i := 0; i < correlationCount; i++ {
		results[i] = float32(silk_inner_product_FLP(inputData, inputData[i:], inputDataSize-i))
	}
}
