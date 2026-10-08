//go:build !amd64

package tensor

func geluMulBody(gate, up []float32) int { return 0 }
