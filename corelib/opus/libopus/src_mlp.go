package libopus

import "math"

const WEIGHTS_SCALE = 0
const MAX_NEURONS = 32

type DenseLayer struct {
	Bias          []int8
	Input_weights []int8
	Nb_inputs     int
	Nb_neurons    int
	Sigmoid       int
}
type GRULayer struct {
	Bias              []int8
	Input_weights     []int8
	Recurrent_weights []int8
	Nb_inputs         int
	Nb_neurons        int
}

func tansig_approx(x float32) float32 {
	var (
		i    int
		y    float32
		dy   float32
		sign float32 = 1
	)
	if x >= 8 {
		return 1
	}
	if x <= float32(-8) {
		return float32(-1)
	}
	if x != x {
		return 0
	}
	if x < 0 {
		x = -x
		sign = float32(-1)
	}
	i = int(math.Floor(float64(x*25 + 0.5)))
	x -= float32(float64(i) * 0.04)
	y = tansig_table[i]
	dy = 1 - y*y
	y = y + x*dy*(1-y*x)
	return sign * y
}
func sigmoid_approx(x float32) float32 {
	return tansig_approx(x*0.5)*0.5 + 0.5
}
func gemm_accum(out []float32, weights []int8, rows int, cols int, col_stride int, x []float32) {
	var (
		i int
		j int
	)
	for i = 0; i < rows; i++ {
		for j = 0; j < cols; j++ {
			out[i] += float32(weights[j*col_stride+i]) * x[j]
		}
	}
}
func compute_dense(layer *DenseLayer, output []float32, input []float32) {
	var (
		i      int
		N      int
		M      int
		stride int
	)
	M = layer.Nb_inputs
	N = layer.Nb_neurons
	stride = N
	for i = 0; i < N; i++ {
		output[i] = float32(layer.Bias[i])
	}
	gemm_accum(output, layer.Input_weights, N, M, stride, input)
	for i = 0; i < N; i++ {
		output[i] *= 1.0 / 128
	}
	if layer.Sigmoid != 0 {
		for i = 0; i < N; i++ {
			output[i] = sigmoid_approx(output[i])
		}
	} else {
		for i = 0; i < N; i++ {
			output[i] = tansig_approx(output[i])
		}
	}
}
func compute_gru(gru *GRULayer, state []float32, input []float32) {
	var (
		i      int
		N      int
		M      int
		stride int
		tmp    [32]float32
		z      [32]float32
		r      [32]float32
		h      [32]float32
	)
	M = gru.Nb_inputs
	N = gru.Nb_neurons
	stride = N * 3
	for i = 0; i < N; i++ {
		z[i] = float32(gru.Bias[i])
	}
	gemm_accum(z[:], gru.Input_weights, N, M, stride, input)
	gemm_accum(z[:], gru.Recurrent_weights, N, N, stride, state)
	for i = 0; i < N; i++ {
		z[i] = sigmoid_approx(z[i] * (1.0 / 128))
	}
	for i = 0; i < N; i++ {
		r[i] = float32(gru.Bias[N+i])
	}
	gemm_accum(r[:], gru.Input_weights[N:], N, M, stride, input)
	gemm_accum(r[:], gru.Recurrent_weights[N:], N, N, stride, state)
	for i = 0; i < N; i++ {
		r[i] = sigmoid_approx(r[i] * (1.0 / 128))
	}
	for i = 0; i < N; i++ {
		h[i] = float32(gru.Bias[N*2+i])
	}
	for i = 0; i < N; i++ {
		tmp[i] = state[i] * r[i]
	}
	gemm_accum(h[:], gru.Input_weights[N*2:], N, M, stride, input)
	gemm_accum(h[:], gru.Recurrent_weights[N*2:], N, N, stride, tmp[:])
	for i = 0; i < N; i++ {
		h[i] = z[i]*state[i] + (1-z[i])*tansig_approx(h[i]*(1.0/128))
	}
	for i = 0; i < N; i++ {
		state[i] = h[i]
	}
}
