//go:build !amd64

package tensor

func q4Q8BlockVNNI(out *[8]int32, aQ, q4 []byte) {
	if len(aQ) < q4BlockSize || len(q4) < q4BlockBytes {
		return
	}
	for i, p := range q4[:q4BlockBytes] {
		out[i/4] += int32(int(p&0x0f)-8) * int32(aQ[i])
		out[4+i/4] += int32(int(p>>4)-8) * int32(aQ[q4BlockBytes+i])
	}
}

func q4Q8BlocksVNNI(out []int32, aQ, q4 []byte, nBlocks int) {
	if nBlocks <= 0 || len(out) < nBlocks*8 || len(aQ) < nBlocks*q4BlockSize || len(q4) < nBlocks*q4BlockBytes {
		return
	}
	for b := 0; b < nBlocks; b++ {
		var partial [8]int32
		q4Q8BlockVNNI(&partial, aQ[b*q4BlockSize:], q4[b*q4BlockBytes:])
		copy(out[b*8:], partial[:])
	}
}
