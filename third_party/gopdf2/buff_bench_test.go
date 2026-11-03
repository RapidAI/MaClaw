package gopdf

import "testing"

func BenchmarkBuffWrite_SmallChunks(b *testing.B) {
	chunk := []byte("hello world! ")
	for i := 0; i < b.N; i++ {
		buf := &Buff{}
		for j := 0; j < 10000; j++ {
			buf.Write(chunk)
		}
	}
}

func BenchmarkBuffWrite_LargeChunk(b *testing.B) {
	chunk := make([]byte, 65536)
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}
	for i := 0; i < b.N; i++ {
		buf := &Buff{}
		for j := 0; j < 100; j++ {
			buf.Write(chunk)
		}
	}
}
