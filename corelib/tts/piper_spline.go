package tts

import "math"

// rationalQuadraticSplineInverse computes the inverse of a piecewise rational quadratic spline.
// inputs: [N] values to transform (must be in [-tailBound, tailBound])
// W, H: [N, K] unnormalized widths and heights
// D: [N, K+1] unnormalized derivatives (already padded)
// Returns: [N] transformed values
func rationalQuadraticSplineInverse(inputs, W, H, D []float32, N, K int, tailBound float32) []float32 {
	outputs := make([]float32, N)

	for n := 0; n < N; n++ {
		inp := inputs[n]

		// Handle outside interval (linear tails)
		if inp < -tailBound || inp > tailBound {
			outputs[n] = inp
			continue
		}

		// Compute widths via softmax
		widths := make([]float64, K)
		cumwidths := make([]float64, K+1)
		{
			maxW := float64(W[n*K])
			for k := 1; k < K; k++ {
				if float64(W[n*K+k]) > maxW {
					maxW = float64(W[n*K+k])
				}
			}
			var sumExp float64
			for k := 0; k < K; k++ {
				widths[k] = math.Exp(float64(W[n*K+k]) - maxW)
				sumExp += widths[k]
			}
			for k := 0; k < K; k++ {
				widths[k] = 1e-3 + (1-1e-3*float64(K))*widths[k]/sumExp
			}
			cumwidths[0] = float64(-tailBound)
			for k := 0; k < K; k++ {
				cumwidths[k+1] = cumwidths[k] + widths[k]*2*float64(tailBound)
			}
			cumwidths[K] = float64(tailBound)
			for k := 0; k < K; k++ {
				widths[k] = cumwidths[k+1] - cumwidths[k]
			}
		}

		// Compute heights via softmax
		heights := make([]float64, K)
		cumheights := make([]float64, K+1)
		{
			maxH := float64(H[n*K])
			for k := 1; k < K; k++ {
				if float64(H[n*K+k]) > maxH {
					maxH = float64(H[n*K+k])
				}
			}
			var sumExp float64
			for k := 0; k < K; k++ {
				heights[k] = math.Exp(float64(H[n*K+k]) - maxH)
				sumExp += heights[k]
			}
			for k := 0; k < K; k++ {
				heights[k] = 1e-3 + (1-1e-3*float64(K))*heights[k]/sumExp
			}
			cumheights[0] = float64(-tailBound)
			for k := 0; k < K; k++ {
				cumheights[k+1] = cumheights[k] + heights[k]*2*float64(tailBound)
			}
			cumheights[K] = float64(tailBound)
			for k := 0; k < K; k++ {
				heights[k] = cumheights[k+1] - cumheights[k]
			}
		}

		// Compute derivatives via softplus
		derivatives := make([]float64, K+1)
		for k := 0; k <= K; k++ {
			derivatives[k] = 1e-3 + math.Log(1+math.Exp(float64(D[n*(K+1)+k])))
		}

		// Find bin (inverse: search in cumheights)
		binIdx := 0
		for k := 0; k < K; k++ {
			if float64(inp) >= cumheights[k+1] {
				binIdx = k + 1
			}
		}
		if binIdx >= K {
			binIdx = K - 1
		}

		// Gather bin parameters
		inputCumwidths := cumwidths[binIdx]
		inputBinWidths := widths[binIdx]
		inputCumheights := cumheights[binIdx]
		inputHeights := heights[binIdx]
		inputDelta := heights[binIdx] / widths[binIdx]
		inputDerivatives := derivatives[binIdx]
		inputDerivativesPlusOne := derivatives[binIdx+1]

		// Inverse quadratic
		a := (float64(inp)-inputCumheights)*(inputDerivatives+inputDerivativesPlusOne-2*inputDelta) +
			inputHeights*(inputDelta-inputDerivatives)
		b := inputHeights*inputDerivatives -
			(float64(inp)-inputCumheights)*(inputDerivatives+inputDerivativesPlusOne-2*inputDelta)
		c := -inputDelta * (float64(inp) - inputCumheights)

		discriminant := b*b - 4*a*c
		if discriminant < 0 {
			discriminant = 0
		}
		root := (2 * c) / (-b - math.Sqrt(discriminant))
		outputs[n] = float32(root*inputBinWidths + inputCumwidths)
	}

	return outputs
}
