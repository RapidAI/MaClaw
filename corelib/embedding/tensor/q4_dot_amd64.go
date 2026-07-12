//go:build amd64

package tensor

//go:noescape
func dotQ4Q8BlockASM(q4, q8 *byte) int32

func dotQ4Q8Block(q4, q8 []byte) int32 {
	if len(q4) < q4BlockBytes || len(q8) < q4BlockSize {
		return 0
	}
	return dotQ4Q8BlockASM(&q4[0], &q8[0])
}

//go:noescape
func dotQ4Q8BlocksASM(out *int32, q4, q8 *byte, nBlocks int)

//go:noescape
func dotQ4Q8BlocksAVX2ASM(out *int32, q4, q8 *byte, nBlocks int)

func dotQ4Q8Blocks(out []int32, q4, q8 []byte, nBlocks int) {
	if nBlocks <= 0 || len(out) < nBlocks || len(q4) < nBlocks*q4BlockBytes || len(q8) < nBlocks*q8BlockBytes {
		return
	}
	if hasAVX2andFMA {
		dotQ4Q8BlocksAVX2ASM(&out[0], &q4[0], &q8[0], nBlocks)
		return
	}
	dotQ4Q8BlocksASM(&out[0], &q4[0], &q8[0], nBlocks)
}
