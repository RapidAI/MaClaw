package libopus

// FLOAT2INT16 converts a float32 sample to int16 with clamping.
func FLOAT2INT16(x float32) int16 {
	v := x * 32768.0
	if v > 32767 {
		v = 32767
	}
	if v < -32768 {
		v = -32768
	}
	return int16(v)
}

// _opus_false is a helper that always returns 0 (false).
func _opus_false() int {
	return 0
}
