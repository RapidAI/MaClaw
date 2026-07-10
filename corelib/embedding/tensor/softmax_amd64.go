//go:build amd64

package tensor

//go:noescape
func softmaxExpSumAVX2(scores *float32, n int, max, a, b, neg88 float32) float32

//go:noescape
func softmaxHMaxAVX2(scores *float32, n int) float32

//go:noescape
func softmaxHMaxDualAVX2(sc0, sc1 *float32, n int, out *[2]float32)

//go:noescape
func softmaxExpSumDualAVX2(sc0, sc1 *float32, n int, max0, max1, a, b, neg88 float32, out *[2]float32)

func softmaxInplaceInvASM(scores []float32) float32 {
	n := len(scores)
	if n == 0 {
		return 0
	}
	if !hasAVX2andFMA || n < 8 {
		return softmaxInplaceInvScalar(scores)
	}
	// AVX2 max over body; scalar tail for n%8.
	body := n &^ 7
	max := softmaxHMaxAVX2(&scores[0], body)
	for i := body; i < n; i++ {
		if scores[i] > max {
			max = scores[i]
		}
	}
	const (
		a     = float32(12102203.0)
		b     = float32(1065353216.0 - 60801.0)
		neg88 = float32(-88.0)
	)
	sum := softmaxExpSumAVX2(&scores[0], body, max, a, b, neg88)
	for i := body; i < n; i++ {
		v := fastExp(scores[i] - max)
		scores[i] = v
		sum += v
	}
	if sum == 0 {
		return 0
	}
	return 1.0 / sum
}

// softmaxInplaceInvDualASM: two equal-length score rows, lockstep exp+sum.
func softmaxInplaceInvDualASM(sc0, sc1 []float32) (inv0, inv1 float32) {
	n := len(sc0)
	if n == 0 || n != len(sc1) {
		return softmaxInplaceInvASM(sc0), softmaxInplaceInvASM(sc1)
	}
	if !hasAVX2andFMA || n < 8 {
		return softmaxInplaceInvScalar(sc0), softmaxInplaceInvScalar(sc1)
	}
	body := n &^ 7
	var maxs [2]float32
	softmaxHMaxDualAVX2(&sc0[0], &sc1[0], body, &maxs)
	max0, max1 := maxs[0], maxs[1]
	for i := body; i < n; i++ {
		if sc0[i] > max0 {
			max0 = sc0[i]
		}
		if sc1[i] > max1 {
			max1 = sc1[i]
		}
	}
	const (
		a     = float32(12102203.0)
		b     = float32(1065353216.0 - 60801.0)
		neg88 = float32(-88.0)
	)
	var sums [2]float32
	softmaxExpSumDualAVX2(&sc0[0], &sc1[0], body, max0, max1, a, b, neg88, &sums)
	sum0, sum1 := sums[0], sums[1]
	for i := body; i < n; i++ {
		v0 := fastExp(sc0[i] - max0)
		v1 := fastExp(sc1[i] - max1)
		sc0[i], sc1[i] = v0, v1
		sum0 += v0
		sum1 += v1
	}
	if sum0 != 0 {
		inv0 = 1.0 / sum0
	}
	if sum1 != 0 {
		inv1 = 1.0 / sum1
	}
	return inv0, inv1
}
