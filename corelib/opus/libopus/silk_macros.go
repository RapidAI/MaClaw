package libopus

func silk_CLZ16(in16 int16) int32 {
	return int32(32 - ec_ilog(uint32(int32(int(in16)<<16|0x8000))))
}
func silk_CLZ32(in32 int32) int32 {
	if int(in32) != 0 {
		return int32(32 - ec_ilog(uint32(in32)))
	}
	return 32
}
