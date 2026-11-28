package nnapi

import "math"

// LayerNormLast normalizes the last axis: (x - mean) / sqrt(var + eps) * gamma + beta.
// gamma and beta broadcast on the right. eps is the SenseVoice 1e-5 or Albert 1e-12.
func (b *Builder) LayerNormLast(x, gamma, beta int, eps float32) int {
	if !b.must(x) {
		return b.fail(errLayer("input"))
	}
	axis := len(b.tensors[x].shape) - 1
	mean := b.Mean(x, axis)
	centered := b.Sub(x, mean)
	variance := b.Mean(b.Mul(centered, centered), axis)
	inv := b.Rsqrt(b.Add(variance, b.ScalarF32(eps)))
	y := b.Mul(centered, inv)
	if gamma >= 0 {
		y = b.Mul(y, gamma)
	}
	if beta >= 0 {
		y = b.Add(y, beta)
	}
	return y
}

// Attention is scaled dot-product attention.
// q, k, v are [time, heads*headDim]. The scale is 1/sqrt(headDim).
func (b *Builder) Attention(q, k, v, hidden, heads int) int {
	if heads <= 0 || hidden <= 0 || hidden%heads != 0 {
		return b.fail(errLayer("attention shape"))
	}
	headDim := hidden / heads
	scale := float32(1 / math.Sqrt(float64(headDim)))
	qs := b.Mul(q, b.ScalarF32(scale))
	pack := func(t int) int {
		r := b.Reshape(t, []int{-1, heads, headDim})
		return b.Transpose(r, []int{1, 0, 2})
	}
	qt, kt, vt := pack(qs), pack(k), pack(v)
	scores := b.BatchMatMul(qt, kt, false, true)
	probs := b.Softmax(scores, 2)
	ctx := b.BatchMatMul(probs, vt, false, false)
	back := b.Transpose(ctx, []int{1, 0, 2})
	return b.Reshape(back, []int{-1, hidden})
}

// GELU is the tanh approximation used by Kokoro PL-BERT.
func (b *Builder) GELU(x int) int {
	x3 := b.Mul(b.Mul(x, x), x)
	inner := b.Add(x, b.Mul(x3, b.ScalarF32(0.044715)))
	inner = b.Mul(inner, b.ScalarF32(0.7978845608028654))
	one := b.Add(b.Tanh(inner), b.ScalarF32(1))
	return b.Mul(b.Mul(x, b.ScalarF32(0.5)), one)
}

func errLayer(what string) error {
	return errString("nnapi: " + what)
}

type errString string

func (e errString) Error() string { return string(e) }
