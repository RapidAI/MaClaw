package silk

const SILK_MAX_ORDER_LPC = 24
const RAND_MULTIPLIER = 196314165
const RAND_INCREMENT = 907633515

func silk_ROR32(a32 int32, rot int) int32 {
	var (
		x uint32 = uint32(a32)
		r uint32 = uint32(int32(rot))
		m uint32 = uint32(int32(-rot))
	)
	if rot == 0 {
		return a32
	} else if rot < 0 {
		return int32((int(x) << int(m)) | int(x)>>(32-int(m)))
	} else {
		return int32((int(x) << (32 - int(r))) | int(x)>>int(r))
	}
}
func silk_min_int(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
func silk_min_16(a int16, b int16) int16 {
	if int(a) < int(b) {
		return a
	}
	return b
}
func silk_min_32(a int32, b int32) int32 {
	if int(a) < int(b) {
		return a
	}
	return b
}
func silk_min_64(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func silk_max_int(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
func silk_max_16(a int16, b int16) int16 {
	if int(a) > int(b) {
		return a
	}
	return b
}
func silk_max_32(a int32, b int32) int32 {
	if int(a) > int(b) {
		return a
	}
	return b
}
func silk_max_64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
