package yolo

import (
	"fmt"
	"math"
	"testing"
)

// TestWinograd_vs_Im2col compares Winograd output against im2col reference
// on a small 3×3 conv with known weights and input.
func TestWinograd_vs_Im2col(t *testing.T) {
	inC, outC := 1, 1
	H, W := 4, 4

	input := NewTensor(1, inC, H, W)
	for i := range input.Data {
		input.Data[i] = float32(i + 1)
	}

	weight := NewTensor(outC, inC, 3, 3)
	for i := range weight.Data {
		weight.Data[i] = 1.0
	}

	bias := []float32{0.0}

	// Reference
	conv := &Conv2dBNSiLU{
		Weight: weight, Bias: bias,
		OutC: outC, InC: inC, KH: 3, KW: 3,
		Stride: 1, Padding: 1, Groups: 1, UseSiLU: false,
	}
	ref := conv.forwardNormal(input)

	// Brute-force Winograd for tile (0,0):
	// Input tile d (4x4 with padding):
	d := [4][4]float32{
		{0, 0, 0, 0},
		{0, 1, 2, 3},
		{0, 5, 6, 7},
		{0, 9, 10, 11},
	}

	// B^T matrix
	BT := [4][4]float32{
		{1, 0, -1, 0},
		{0, 1, 1, 0},
		{0, -1, 1, 0},
		{0, 1, 0, -1},
	}

	// G matrix
	G := [4][3]float32{
		{1, 0, 0},
		{0.5, 0.5, 0.5},
		{0.5, -0.5, 0.5},
		{0, 0, 1},
	}

	// A^T matrix — CORRECTED for cross-correlation: [1,3] = -1
	AT := [2][4]float32{
		{1, 1, 1, 0},
		{0, 1, -1, -1},
	}

	// Filter g (3x3 all ones)
	g := [3][3]float32{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}}

	// Step 1: V = B^T @ d @ B
	var Bd [4][4]float32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				Bd[i][j] += BT[i][k] * d[k][j]
			}
		}
	}
	// V = Bd @ B where B[k,j] = BT[j,k] (B is transpose of BT)
	var V [4][4]float32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				V[i][j] += Bd[i][k] * BT[j][k] // B[k,j] = BT[j,k]
			}
		}
	}

	// Step 2: U = G @ g @ G^T
	var Gg [4][3]float32
	for i := 0; i < 4; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				Gg[i][j] += G[i][k] * g[k][j]
			}
		}
	}
	var U [4][4]float32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 3; k++ {
				U[i][j] += Gg[i][k] * G[j][k] // G^T[k][j] = G[j][k]
			}
		}
	}

	// Step 3: M = U ⊙ V (element-wise)
	var M [4][4]float32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			M[i][j] = U[i][j] * V[i][j]
		}
	}

	// Step 4: Y = A^T @ M @ A
	var AM [2][4]float32
	for i := 0; i < 2; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				AM[i][j] += AT[i][k] * M[k][j]
			}
		}
	}
	// A = transpose(AT)
	var Y [2][2]float32
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			for k := 0; k < 4; k++ {
				Y[i][j] += AM[i][k] * AT[j][k] // A[k][j] = AT[j][k]
			}
		}
	}

	t.Logf("Brute-force Winograd tile (0,0):")
	t.Logf("  Y[0][0]=%.1f Y[0][1]=%.1f", Y[0][0], Y[0][1])
	t.Logf("  Y[1][0]=%.1f Y[1][1]=%.1f", Y[1][0], Y[1][1])
	t.Logf("Reference tile (0,0):")
	t.Logf("  ref[0][0]=%.1f ref[0][1]=%.1f", ref.At(0, 0, 0, 0), ref.At(0, 0, 0, 1))
	t.Logf("  ref[1][0]=%.1f ref[1][1]=%.1f", ref.At(0, 0, 1, 0), ref.At(0, 0, 1, 1))

	// Check brute-force matches reference
	if math.Abs(float64(Y[0][0]-ref.At(0, 0, 0, 0))) > 0.01 ||
		math.Abs(float64(Y[0][1]-ref.At(0, 0, 0, 1))) > 0.01 ||
		math.Abs(float64(Y[1][0]-ref.At(0, 0, 1, 0))) > 0.01 ||
		math.Abs(float64(Y[1][1]-ref.At(0, 0, 1, 1))) > 0.01 {
		t.Error("Brute-force Winograd doesn't match reference — matrices are wrong")
	} else {
		t.Log("Brute-force Winograd matches reference — bug is in the optimized implementation")
	}
}

// TestWinograd_vs_Im2col_MultiChannel tests with multiple input/output channels.
func TestWinograd_vs_Im2col_MultiChannel(t *testing.T) {
	inC, outC := 3, 2
	H, W := 6, 6

	input := NewTensor(1, inC, H, W)
	for i := range input.Data {
		input.Data[i] = float32(i%7-3) * 0.1
	}

	weight := NewTensor(outC, inC, 3, 3)
	for i := range weight.Data {
		weight.Data[i] = float32(i%5-2) * 0.1
	}

	bias := []float32{0.5, -0.3}

	conv := &Conv2dBNSiLU{
		Weight: weight, Bias: bias,
		OutC: outC, InC: inC, KH: 3, KW: 3,
		Stride: 1, Padding: 1, Groups: 1, UseSiLU: false,
	}
	ref := conv.forwardNormal(input)

	wf := TransformFilters(weight)
	wino := Conv3x3Winograd(input, wf, bias, false)

	maxDiff := float32(0)
	for i := range ref.Data {
		diff := float32(math.Abs(float64(ref.Data[i] - wino.Data[i])))
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	t.Logf("Max diff (multi-channel): %e", maxDiff)

	if maxDiff > 0.01 {
		t.Errorf("Multi-channel Winograd differs by %e", maxDiff)
		for oc := 0; oc < outC; oc++ {
			for h := 0; h < H; h++ {
				for w := 0; w < W; w++ {
					r := ref.At(0, oc, h, w)
					wi := wino.At(0, oc, h, w)
					if math.Abs(float64(r-wi)) > 0.01 {
						t.Logf("  [oc=%d,%d,%d] ref=%.4f wino=%.4f", oc, h, w, r, wi)
					}
				}
			}
		}
	}
}

// TestWinograd_FilterTransform verifies the filter transform G = GgG^T.
func TestWinograd_FilterTransform(t *testing.T) {
	// Identity-like filter: center=1, rest=0
	g := []float32{0, 0, 0, 0, 1, 0, 0, 0, 0}
	var G [16]float32
	transformFilter3x3(g, G[:])
	t.Logf("Identity filter transform:\n%s", format4x4(G[:]))

	// All-ones filter
	g2 := []float32{1, 1, 1, 1, 1, 1, 1, 1, 1}
	var G2 [16]float32
	transformFilter3x3(g2, G2[:])
	t.Logf("All-ones filter transform:\n%s", format4x4(G2[:]))
}

// ── helpers ──

func formatTensor2D(t *Tensor, n, c int) string {
	H, W := t.Shape[2], t.Shape[3]
	s := ""
	for h := 0; h < H; h++ {
		for w := 0; w < W; w++ {
			s += fmt.Sprintf("%8.3f", t.At(n, c, h, w))
		}
		s += "\n"
	}
	return s
}

func format4x4(data []float32) string {
	s := ""
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			s += fmt.Sprintf("%8.4f", data[i*4+j])
		}
		s += "\n"
	}
	return s
}
