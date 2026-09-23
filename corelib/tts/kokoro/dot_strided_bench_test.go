//go:build amd64

package kokoro

import "testing"

func benchDot4x2(b *testing.B, inC, taps, positions int) {
	if !useKokoroStridedFMA() {
		b.Skip()
	}
	x := make([]float32, (positions+taps+1)*inC)
	w := make([]float32, 4*taps*inC)
	b.SetBytes(int64(positions / 2 * 8 * taps * inC * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := float32(0)
		for it := 0; it+1 < positions; it += 2 {
			r0, r1, r2, r3, r4, r5, r6, r7 := dotStridedFMA4x2(x[it*inC:], inC, w, inC, taps*inC, taps, inC)
			acc += r0 + r1 + r2 + r3 + r4 + r5 + r6 + r7
		}
		if acc == 12345.678 {
			b.Fatal(acc)
		}
	}
}

func BenchmarkDotStridedFMA4x2K7C128(b *testing.B)  { benchDot4x2(b, 128, 7, 35041) }
func BenchmarkDotStridedFMA4x2K3C256(b *testing.B)  { benchDot4x2(b, 256, 3, 5841) }
func BenchmarkDotStridedFMA4x2K11C128(b *testing.B) { benchDot4x2(b, 128, 11, 35041) }
func BenchmarkDotStridedFMA4x2K7C1090(b *testing.B) { benchDot4x2(b, 1090, 7, 580) }

func benchDot2(b *testing.B, inC, taps, positions int) {
	if !useKokoroStridedFMA() {
		b.Skip()
	}
	x := make([]float32, (positions+taps+1)*inC)
	w := make([]float32, taps*inC)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := float32(0)
		for it := 0; it+1 < positions; it += 2 {
			r0, r1 := dotStridedFMA2(x[it*inC:], inC, w, inC, taps, inC)
			acc += r0 + r1
		}
		if acc == 12345.678 {
			b.Fatal(acc)
		}
	}
}

func BenchmarkDotStridedFMA2K7C128(b *testing.B) { benchDot2(b, 128, 7, 35041) }

func benchConv4x2G(b *testing.B, inC, taps, positions int) {
	if !useKokoroStridedFMA() {
		b.Skip()
	}
	x := make([]float32, (positions+taps+1)*inC)
	w := make([]float32, 4*taps*inC)
	bias := make([]float32, 4)
	out := make([]float32, 4*positions)
	b.SetBytes(int64(positions / 2 * 8 * taps * inC * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for it := 0; it+1 < positions; it += 2 {
			conv4x2G(x[it*inC:], inC, w, inC, taps*inC, taps, inC, inC,
				out[it:], positions, 1, bias, nil)
		}
	}
}

func BenchmarkConv4x2GK7C128(b *testing.B)  { benchConv4x2G(b, 128, 7, 35041) }
func BenchmarkConv4x2GK11C128(b *testing.B) { benchConv4x2G(b, 128, 11, 35041) }
func BenchmarkConv4x2GK3C256(b *testing.B)  { benchConv4x2G(b, 256, 3, 5841) }
