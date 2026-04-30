package cmath

import "math"

const TowardZero = 1

func FSetRound(r int32) int32 {
	return 0
}

func Abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func Modf(v float64, iptr *float64) float64 {
	intg, frac := math.Modf(v)
	*iptr = intg
	return frac
}

func Modff(v float32, iptr *float32) float32 {
	intg, frac := modf32(v)
	*iptr = intg
	return frac
}

// modf32 splits a float32 into integer and fractional parts.
// Replaces maze.io/x/math32.Modf to eliminate the external dependency.
func modf32(f float32) (float32, float32) {
	intg, frac := math.Modf(float64(f))
	return float32(intg), float32(frac)
}
