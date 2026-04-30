package silk

func silk_inner_prod_aligned_scale(inVec1 []int16, inVec2 []int16, scale int, len_ int) int32 {
	var (
		i   int
		sum int32 = 0
	)
	for i = 0; i < len_; i++ {
		sum = int32(int(sum) + ((int(int32(inVec1[i])) * int(int32(inVec2[i]))) >> scale))
	}
	return sum
}
