package mp3

/*
Functions for efficient multiplication operations with 32-bit integers. They rely on bitwise manipulation and shifting to perform multiplication and rounding to achieve fixed-point arithmetic.
*/

// mul safely multiples a and b and returns the result.
// By casting to int64 first, overflow is avoided.
func mul(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 32)
}

func mulR(a, b int32) int32 {
	return int32(((int64(a) * int64(b)) + 0x80000000) >> 32)
}

// mulSR is similar to mulS but with rounding.
func mulSR(a, b int32) int32 {
	return int32(((int64(a) * int64(b)) + 0x40000000) >> 31)
}

// cmuls multiples two complex numbers together.
func cmuls(aReal, aImag, bReal, bImag *int32) (int32, int32) {
	resReal := int32(((int64(*aReal)*int64(*bReal) - int64(*aImag)*int64(*bImag)) >> 31))
	resImag := int32(((int64(*aReal)*int64(*bImag) + int64(*aImag)*int64(*bReal)) >> 31))

	return resReal, resImag
}
