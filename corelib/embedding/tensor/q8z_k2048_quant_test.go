//go:build amd64

package tensor

import (
	"math"
	"math/rand"
	"testing"
)

// zmmQByte returns the ZMM-panel q byte for logical (row, block, i) given the
// interleaved layout q[g*512 + r*64 : +32] = block 2g of row r, q[+32:+64] =
// block 2g+1.
func zmmQByte(q *[8 * 2048]int8, r, b, i int) int8 {
	g := b / 2
	off := g*512 + r*64 + (b%2)*32 + i
	return q[off]
}

// TestQ8ZK2048QuantBitExact verifies quantizePanel8Q8UZmmK2048AVX512 produces
// bit-identical per-32-block values (q bytes as u8 + scale bit pattern) to the
// production quantizePanel8Q8UAVX512, across adversarial amax regimes.
func TestQ8ZK2048QuantBitExact(t *testing.T) {
	if !hasAVX512 {
		t.Skip("AVX-512 not available")
	}
	eps := math.Float32frombits(0x34000000) // ~1.19e-7, the in-kernel eps

	rng := rand.New(rand.NewSource(20260310))
	a := make([]float32, 8*2048)
	// Case fill helpers applied per 32-float block.
	fillers := []func(blk []float32, rng *rand.Rand){
		func(blk []float32, rng *rand.Rand) { // all zero
		},
		func(blk []float32, rng *rand.Rand) { // all exactly eps
			for i := range blk {
				blk[i] = eps
			}
		},
		func(blk []float32, rng *rand.Rand) { // amax just below eps
			for i := range blk {
				blk[i] = eps * 0.5
			}
			blk[7] = eps * 0.999
		},
		func(blk []float32, rng *rand.Rand) { // amax just above eps
			for i := range blk {
				blk[i] = eps * 0.001
			}
			blk[3] = eps * 1.001
		},
		func(blk []float32, rng *rand.Rand) { // ~1e-6
			for i := range blk {
				blk[i] = float32(rng.NormFloat64()) * 1e-6
				if blk[i] < 0 {
					blk[i] = -blk[i]
				}
			}
		},
		func(blk []float32, rng *rand.Rand) { // ~1e-8 (denormal-ish products)
			for i := range blk {
				blk[i] = float32(rng.NormFloat64()) * 1e-8
				if blk[i] < 0 {
					blk[i] = -blk[i]
				}
			}
		},
		func(blk []float32, rng *rand.Rand) { // normal range
			for i := range blk {
				blk[i] = float32(rng.NormFloat64())
				if blk[i] < 0 {
					blk[i] = -blk[i]
				}
			}
		},
		func(blk []float32, rng *rand.Rand) { // denormal inputs
			for i := range blk {
				blk[i] = math.Float32frombits(0x00000001 + uint32(rng.Intn(1000)))
			}
		},
		func(blk []float32, rng *rand.Rand) { // single spike
			for i := range blk {
				blk[i] = 0
			}
			blk[rng.Intn(32)] = 3.14159
		},
	}

	for trial := 0; trial < 20; trial++ {
		for b := 0; b < 64; b++ {
			blk := a[b*32 : (b+1)*32]
			for r := 1; r < 8; r++ {
				copy(a[r*2048+b*32:(r+1)*2048], blk[:0]) // no-op guard
			}
			fillers[(trial+b)%len(fillers)](blk, rng)
			for r := 1; r < 8; r++ {
				// Distinct content per row: perturb slightly.
				rb := a[r*2048+b*32 : r*2048+(b+1)*32]
				copy(rb, blk)
				if (trial+b+r)%3 == 0 {
					rb[rng.Intn(32)] += float32(rng.NormFloat64())
					if rb[rng.Intn(32)] < 0 {
						rb[0] = -rb[0]
					}
				}
			}
		}

		var py, pz q8APanel8
		var pz2 q8APanel8K2048Z
		quantizePanel8Q8U(&py, a)
		quantizePanel8Q8UZmmK2048(&pz2, a)
		_ = pz

		for r := 0; r < 8; r++ {
			for b := 0; b < 64; b++ {
				sY := py.s[b*8+r]
				sZ := pz2.s[b*8+r]
				if math.Float32bits(sY) != math.Float32bits(sZ) {
					t.Fatalf("trial %d row %d block %d: scale mismatch Y=%08x Z=%08x (%g vs %g)",
						trial, r, b, math.Float32bits(sY), math.Float32bits(sZ), sY, sZ)
				}
				for i := 0; i < 32; i++ {
					qY := py.q[b*256+r*32+i]
					qZ := zmmQByte(&pz2.q, r, b, i)
					if qY != qZ {
						t.Fatalf("trial %d row %d block %d i %d: q mismatch Y=%d Z=%d (scale %g)",
							trial, r, b, i, int8(qY), int8(qZ), sY)
					}
				}
			}
		}
	}
}

// TestQ8ZK2048ZmmVNNIBitwiseEquiv probes whether the ZMM K=2048 path
// (tryFusedK2048ZmmVNNI) is BITWISE identical to the production YMM path
// (tryFusedAccumVNNI) end-to-end. It reports mismatch statistics. The ZMM GEMM
// kernel accumulates even/odd block pairs into separate ZMM lane halves and
// groups the per-block scale product differently than the YMM kernel, so exact
// bitwise equivalence is NOT expected — this test documents the gap.
func TestQ8ZK2048ZmmVNNIBitwiseEquiv(t *testing.T) {
	if !hasAVX512VNNI {
		t.Skip("AVX-512 VNNI not available")
	}
	for _, M := range []int{8, 16, 17, 97} {
		for seed := int64(1); seed <= 3; seed++ {
			rng := rand.New(rand.NewSource(seed*1000 + int64(M)))
			a := make([]float32, M*2048)
			for i := range a {
				v := float32(rng.NormFloat64())
				if v < 0 {
					v = -v // ReLU activations are non-negative
				}
				a[i] = v
			}
			bData := make([]float32, 512*2048)
			for i := range bData {
				bData[i] = float32(rng.NormFloat64()) * 0.05
			}
			b := QuantizeToQ8(bData, 512, 2048)
			b.PrepareScales()
			bias := make([]float32, 512)
			for i := range bias {
				bias[i] = float32(rng.NormFloat64()) * 0.1
			}

			outY := make([]float32, M*512)
			outZ := make([]float32, M*512)
			enableQ8ZK2048VNNI = false
			matMulQ8RangeFusedAccumScaled(outY, a, b, bias, M, 512, 2048, 0, 512, 64)
			enableQ8ZK2048VNNI = true
			matMulQ8RangeFusedAccumScaled(outZ, a, b, bias, M, 512, 2048, 0, 512, 64)
			enableQ8ZK2048VNNI = false

			mismatch := 0
			maxULP := int64(0)
			var sumRel float64
			for i := range outY {
				y, z := math.Float32bits(outY[i]), math.Float32bits(outZ[i])
				if y != z {
					mismatch++
					d := int64(y) - int64(z)
					if d < 0 {
						d = -d
					}
					if d > maxULP {
						maxULP = d
					}
					den := math.Abs(float64(outY[i]))
					if den < 1e-30 {
						den = 1e-30
					}
					sumRel += math.Abs(float64(outZ[i])-float64(outY[i])) / den
				}
			}
			meanRel := 0.0
			if mismatch > 0 {
				meanRel = sumRel / float64(mismatch)
			}
			t.Logf("M=%d seed=%d: %d/%d mismatch, max ulp diff %d, mean rel err %.3e", M, seed, mismatch, len(outY), maxULP, meanRel)
			if mismatch > 0 {
				t.Skipf("ZMM K=2048 path NOT bitwise identical to YMM path: M=%d seed=%d %d/%d elements differ (max %d ulp) — GEMM kernel accumulation order differs; keeping enableQ8ZK2048VNNI=false", M, seed, mismatch, len(outY), maxULP)
			}
		}
	}
}
