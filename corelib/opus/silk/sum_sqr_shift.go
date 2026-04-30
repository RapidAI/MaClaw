package silk

func silk_sum_sqr_shift(energy *int32, shift *int, x []int16, len_ int) {
	var (
		i       int
		shft    int
		nrg_tmp uint32
		nrg     int32
	)
	shft = 31 - int(silk_CLZ32(int32(len_)))
	nrg = int32(len_)
	for i = 0; i < len_-1; i += 2 {
		nrg_tmp = uint32(int32(int(int32(x[i])) * int(int32(x[i]))))
		nrg_tmp = uint32(int32(int(nrg_tmp) + int(uint32(int32(int(int32(x[i+1]))*int(int32(x[i+1])))))))
		nrg = int32(int(nrg) + (int(nrg_tmp) >> shft))
	}
	if i < len_ {
		nrg_tmp = uint32(int32(int(int32(x[i])) * int(int32(x[i]))))
		nrg = int32(int(nrg) + (int(nrg_tmp) >> shft))
	}
	shft = int(silk_max_32(0, int32(shft+3-int(silk_CLZ32(nrg)))))
	nrg = 0
	for i = 0; i < len_-1; i += 2 {
		nrg_tmp = uint32(int32(int(int32(x[i])) * int(int32(x[i]))))
		nrg_tmp = uint32(int32(int(nrg_tmp) + int(uint32(int32(int(int32(x[i+1]))*int(int32(x[i+1])))))))
		nrg = int32(int(nrg) + (int(nrg_tmp) >> shft))
	}
	if i < len_ {
		nrg_tmp = uint32(int32(int(int32(x[i])) * int(int32(x[i]))))
		nrg = int32(int(nrg) + (int(nrg_tmp) >> shft))
	}
	*shift = shft
	*energy = nrg
}
