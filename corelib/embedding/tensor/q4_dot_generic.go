//go:build !amd64

package tensor

func dotQ4Q8Block(q4, q8 []byte) int32 {
	if len(q4) < q4BlockBytes || len(q8) < q4BlockSize {
		return 0
	}
	var sum int32
	for i, packed := range q4[:q4BlockBytes] {
		sum += int32(int(packed&0x0f)-8) * int32(int8(q8[i]))
		sum += int32(int(packed>>4)-8) * int32(int8(q8[q4BlockBytes+i]))
	}
	return sum
}

func dotQ4Q8Blocks(out []int32, q4, q8 []byte, nBlocks int) {
	if nBlocks <= 0 || len(out) < nBlocks || len(q4) < nBlocks*q4BlockBytes || len(q8) < nBlocks*q8BlockBytes {
		return
	}
	for b := 0; b < nBlocks; b++ {
		out[b] = dotQ4Q8Block(q4[b*q4BlockBytes:], q8[b*q8BlockBytes+2:])
	}
}
