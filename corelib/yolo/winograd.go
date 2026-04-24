package yolo

// Winograd F(2×2, 3×3) convolution for stride=1, padding=1.
//
// Standard 3×3 conv on a 2×2 output tile: 9 multiplications per output element.
// Winograd: 4 multiplications per output element (2.25x reduction).
//
// Key insight for SIMD: the element-wise multiply M[oc,t,k] = sum_ic(G[oc,ic,k] * D[ic,t,k])
// can be decomposed into 16 independent matmuls (one per k=0..15):
//   M[:,:,k] = G[:,:,k] @ D[:,:,k]
// where G[:,:,k] is [OutC, InC] and D[:,:,k] is [InC, numTiles].
// Each matmul uses vek32.Dot for SIMD acceleration.

import (
	"runtime"
	"sync"

	"github.com/viterin/vek/vek32"
)

// WinogradFilter holds pre-transformed filter weights for F(2×2, 3×3).
// Layout: [16][OutC][InC] — transposed from the natural [OutC][InC][16]
// so that G[:,:,k] is contiguous for each k, enabling SIMD dot products.
type WinogradFilter struct {
	Data []float32 // [16 * OutC * InC]
	OutC int
	InC  int
}

// TransformFilters pre-computes Winograd filter transforms.
// Input g is [OutC, InC, 3, 3]. Output layout: [16][OutC][InC] for SIMD matmul.
func TransformFilters(g *Tensor) *WinogradFilter {
	outC := g.Shape[0]
	inC := g.Shape[1]
	wf := &WinogradFilter{
		Data: make([]float32, 16*outC*inC),
		OutC: outC,
		InC:  inC,
	}

	// First compute in natural layout [OutC][InC][16], then transpose to [16][OutC][InC]
	natural := make([]float32, outC*inC*16)
	for oc := 0; oc < outC; oc++ {
		for ic := 0; ic < inC; ic++ {
			srcOff := (oc*inC + ic) * 9
			dstOff := (oc*inC + ic) * 16
			transformFilter3x3(g.Data[srcOff:srcOff+9], natural[dstOff:dstOff+16])
		}
	}

	// Transpose: natural[oc*inC+ic][k] → wf.Data[k*outC*inC + oc*inC + ic]
	for oc := 0; oc < outC; oc++ {
		for ic := 0; ic < inC; ic++ {
			natOff := (oc*inC + ic) * 16
			for k := 0; k < 16; k++ {
				wf.Data[k*outC*inC+oc*inC+ic] = natural[natOff+k]
			}
		}
	}

	return wf
}

// transformFilter3x3 computes G = GgG^T for one 3×3 filter.
func transformFilter3x3(g []float32, G []float32) {
	// G matrix (4×3) applied as: tmp = G @ g, then result = tmp @ G^T
	var tmp [12]float32
	for i := 0; i < 3; i++ {
		g0, g1, g2 := g[0*3+i], g[1*3+i], g[2*3+i]
		tmp[0*3+i] = g0
		tmp[1*3+i] = (g0 + g1 + g2) * 0.5
		tmp[2*3+i] = (g0 - g1 + g2) * 0.5
		tmp[3*3+i] = g2
	}
	for i := 0; i < 4; i++ {
		t0, t1, t2 := tmp[i*3+0], tmp[i*3+1], tmp[i*3+2]
		G[i*4+0] = t0
		G[i*4+1] = (t0 + t1 + t2) * 0.5
		G[i*4+2] = (t0 - t1 + t2) * 0.5
		G[i*4+3] = t2
	}
}

// Conv3x3Winograd performs 3×3 stride=1 pad=1 convolution using Winograd F(2,3).
func Conv3x3Winograd(input *Tensor, wf *WinogradFilter, bias []float32, useSiLU bool) *Tensor {
	N := input.Shape[0]
	inC := input.Shape[1]
	H := input.Shape[2]
	W := input.Shape[3]
	outC := wf.OutC

	out := NewTensor(N, outC, H, W)

	tileH := (H + 1) / 2
	tileW := (W + 1) / 2
	numTiles := tileH * tileW

	nWorkers := runtime.NumCPU()

	for n := 0; n < N; n++ {
		// Step 1: Transform input tiles.
		// Layout: [16][InC][numTiles] — so that D[:,:,k] = D[k] is [InC, numTiles]
		// contiguous, enabling SIMD dot products in the matmul step.
		D := make([]float32, 16*inC*numTiles)

		// Parallel across input channels
		wkrs := nWorkers
		if wkrs > inC {
			wkrs = inC
		}
		var wg1 sync.WaitGroup
		icPerW := (inC + wkrs - 1) / wkrs
		for w := 0; w < wkrs; w++ {
			icStart := w * icPerW
			icEnd := icStart + icPerW
			if icEnd > inC {
				icEnd = inC
			}
			if icStart >= icEnd {
				break
			}
			wg1.Add(1)
			go func(ics, ice int) {
				defer wg1.Done()
				for ic := ics; ic < ice; ic++ {
					inOff := n*input.Stride[0] + ic*input.Stride[1]
					transformInputTilesTransposed(input.Data[inOff:], H, W, tileH, tileW, D, ic, inC, numTiles)
				}
			}(icStart, icEnd)
		}
		wg1.Wait()

		// Step 2: 16 independent matmuls.
		// For each k: M[k] = G[k] @ D[k]
		// G[k] is [OutC, InC] (row-major), D[k] is [InC, numTiles] (row-major).
		// Result M[k] is [OutC, numTiles].
		// We need M in layout [OutC][numTiles][16] for the inverse transform.
		// But computing 16 separate matmuls and then transposing is wasteful.
		// Instead, compute all 16 matmuls and write directly to M[oc][t][k].
		M := make([]float32, outC*numTiles*16)

		// Parallel across k (16 independent matmuls)
		var wg2 sync.WaitGroup
		for k := 0; k < 16; k++ {
			wg2.Add(1)
			go func(kk int) {
				defer wg2.Done()
				gSlice := wf.Data[kk*outC*inC : (kk+1)*outC*inC] // [OutC, InC]
				dSlice := D[kk*inC*numTiles : (kk+1)*inC*numTiles] // [InC, numTiles]

				// Transpose dSlice [InC, numTiles] → dT [numTiles, InC] for vek32.Dot
				dT := make([]float32, numTiles*inC)
				for r := 0; r < inC; r++ {
					srcOff := r * numTiles
					for c := 0; c < numTiles; c++ {
						dT[c*inC+r] = dSlice[srcOff+c]
					}
				}

				// Matmul: for each oc, dot(G[oc,:], dT[t,:]) for each t
				for oc := 0; oc < outC; oc++ {
					gRow := gSlice[oc*inC : oc*inC+inC]
					for t := 0; t < numTiles; t++ {
						val := vek32.Dot(gRow, dT[t*inC:t*inC+inC])
						M[(oc*numTiles+t)*16+kk] = val
					}
				}
			}(k)
		}
		wg2.Wait()

		// Step 3: Inverse transform M → output.
		// M layout: [OutC][numTiles][16]
		wkrs2 := nWorkers
		if wkrs2 > outC {
			wkrs2 = outC
		}
		var wg3 sync.WaitGroup
		ocPerW := (outC + wkrs2 - 1) / wkrs2
		for w := 0; w < wkrs2; w++ {
			ocStart := w * ocPerW
			ocEnd := ocStart + ocPerW
			if ocEnd > outC {
				ocEnd = outC
			}
			if ocStart >= ocEnd {
				break
			}
			wg3.Add(1)
			go func(ocs, oce int) {
				defer wg3.Done()
				for oc := ocs; oc < oce; oc++ {
					outOff := n*out.Stride[0] + oc*out.Stride[1]
					b := bias[oc]
					for t := 0; t < numTiles; t++ {
						mOff := (oc*numTiles + t) * 16
						inverseTransformOneTile(M[mOff:mOff+16], out.Data[outOff:], H, W, tileH, tileW, t, b)
					}
				}
			}(ocStart, ocEnd)
		}
		wg3.Wait()
	}

	if useSiLU {
		out.SiLU()
	}
	return out
}

// transformInputTilesTransposed transforms input tiles for one channel and writes
// to the transposed layout D[k][ic][t] = D[k*inC*numTiles + ic*numTiles + t].
func transformInputTilesTransposed(inData []float32, H, W, tileH, tileW int, D []float32, ic, inC, numTiles int) {
	tIdx := 0
	for th := 0; th < tileH; th++ {
		for tw := 0; tw < tileW; tw++ {
			var d [16]float32
			baseH := th*2 - 1
			baseW := tw*2 - 1
			for i := 0; i < 4; i++ {
				ih := baseH + i
				for j := 0; j < 4; j++ {
					iw := baseW + j
					if ih >= 0 && ih < H && iw >= 0 && iw < W {
						d[i*4+j] = inData[ih*W+iw]
					}
				}
			}

			// B^T d B transform
			// Step 1: tmp = B^T @ d (multiply rows of d by B^T rows)
			var tmp [16]float32
			for j := 0; j < 4; j++ {
				d0, d1, d2, d3 := d[0*4+j], d[1*4+j], d[2*4+j], d[3*4+j]
				tmp[0*4+j] = d0 - d2
				tmp[1*4+j] = d1 + d2
				tmp[2*4+j] = -d1 + d2
				tmp[3*4+j] = d1 - d3
			}
			// Step 2: result = tmp @ B (multiply columns of tmp by B columns)
			// B = transpose(B^T):
			// col 0: [1, 0, -1, 0]^T → result[i,0] = tmp[i,0] - tmp[i,2]
			// col 1: [0, 1, 1, 0]^T  → result[i,1] = tmp[i,1] + tmp[i,2]
			// col 2: [0, -1, 1, 0]^T → result[i,2] = -tmp[i,1] + tmp[i,2]
			// col 3: [0, 1, 0, -1]^T → result[i,3] = tmp[i,1] - tmp[i,3]
			// Wait — B columns are rows of B^T transposed. Let me recompute.
			// B^T[i,j] = B[j,i], so B[j,i] = B^T[i,j].
			// B[:,0] = B^T[0,:] = [1, 0, -1, 0] → result[i,0] = tmp[i,0]*1 + tmp[i,1]*0 + tmp[i,2]*(-1) + tmp[i,3]*0
			// B[:,1] = B^T[1,:] = [0, 1, 1, 0]  → result[i,1] = tmp[i,1] + tmp[i,2]
			// B[:,2] = B^T[2,:] = [0, -1, 1, 0] → result[i,2] = -tmp[i,1] + tmp[i,2]
			// B[:,3] = B^T[3,:] = [0, 1, 0, -1] → result[i,3] = tmp[i,1] - tmp[i,3]
			// This is IDENTICAL to what we had before! B^T is its own transpose for this matrix?
			// No — B^T rows = B columns. The operation tmp @ B means:
			// result[i,j] = sum_k(tmp[i,k] * B[k,j])
			// B[k,j] = B^T[j,k]
			// So result[i,j] = sum_k(tmp[i,k] * B^T[j,k])
			// For j=0: sum_k(tmp[i,k] * B^T[0,k]) = tmp[i,0]*1 + tmp[i,1]*0 + tmp[i,2]*(-1) + tmp[i,3]*0
			//        = tmp[i,0] - tmp[i,2]  ← SAME as before
			// Hmm, this IS the same. So the B transform is correct.
			var result [16]float32
			for i := 0; i < 4; i++ {
				t0, t1, t2, t3 := tmp[i*4+0], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3]
				result[i*4+0] = t0 - t2
				result[i*4+1] = t1 + t2
				result[i*4+2] = t2 - t1
				result[i*4+3] = t1 - t3
			}

			// Write to transposed layout: D[k][ic][t]
			for k := 0; k < 16; k++ {
				D[k*inC*numTiles+ic*numTiles+tIdx] = result[k]
			}
			tIdx++
		}
	}
}

// inverseTransformOneTile transforms one Winograd output tile back to spatial domain.
func inverseTransformOneTile(m []float32, outData []float32, H, W, tileH, tileW, tIdx int, bias float32) {
	th := tIdx / tileW
	tw := tIdx % tileW

	// A^T M A transform
	// A^T = [[1, 1, 1, 0], [0, 1, -1, -1]]
	// Note: A^T[1,3] = -1 (not +1 as in some references).
	// This is the correct sign for cross-correlation (PyTorch Conv2d convention).
	var tmp [8]float32
	for j := 0; j < 4; j++ {
		m0, m1, m2, m3 := m[0*4+j], m[1*4+j], m[2*4+j], m[3*4+j]
		tmp[0*4+j] = m0 + m1 + m2
		tmp[1*4+j] = m1 - m2 - m3
	}
	var y [4]float32
	for i := 0; i < 2; i++ {
		t0, t1, t2, t3 := tmp[i*4+0], tmp[i*4+1], tmp[i*4+2], tmp[i*4+3]
		y[i*2+0] = t0 + t1 + t2 + bias
		y[i*2+1] = t1 - t2 - t3 + bias
	}

	oh := th * 2
	ow := tw * 2
	if oh < H && ow < W {
		outData[oh*W+ow] = y[0]
	}
	if oh < H && ow+1 < W {
		outData[oh*W+ow+1] = y[1]
	}
	if oh+1 < H && ow < W {
		outData[(oh+1)*W+ow] = y[2]
	}
	if oh+1 < H && ow+1 < W {
		outData[(oh+1)*W+ow+1] = y[3]
	}
}
