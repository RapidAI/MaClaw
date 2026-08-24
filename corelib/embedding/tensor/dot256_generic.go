//go:build !amd64

package tensor

func dot256(a, b []float32) float32 { return dot256Scalar(a, b) }
