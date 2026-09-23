//go:build amd64

package yolo

import "sync"

// Winograd F(2,3) for 3x3 stride-1 padding-1 convolutions.
//
// The convolution is computed as Y = A^T (U ∘ V) A per 2x2 output tile:
//
//	U = G g G^T   — precomputed at load time (G is 4x3)
//	V = B^T x B   — input tiles of 4x4 (B is 4x4, entries ±1: no multiplies)
//	o = A^T Y A   — output transform (A is 2x4), fuses bias + SiLU
//
// The 16 ξ-slices reduce to 16 independent GEMMs of [OutC, InC] x [InC, T]
// (T = number of 2x2 tiles), batched through a single parallel dispatch so
// the per-GEMM goroutine overhead is paid once per layer. Total multiply
// count is 4/9 of im2col+GEMM (2.25x fewer MACs).
//
// B = [[1,0,-1,0],[0,1,1,0],[0,-1,1,0],[0,1,0,-1]]
// A = [[1,1,1,0],[0,1,-1,-1]]
// G = [[1,0,0],[.5,.5,.5],[.5,-.5,.5],[0,0,1]]

// winoB/winoA/winoG are the F(2,3) transform matrices (row-major).
var winoB = [16]float32{
	1, 0, -1, 0,
	0, 1, 1, 0,
	0, -1, 1, 0,
	0, 1, 0, -1,
}

// WinoFilter2x3 holds the load-time-transformed weights for one conv layer:
// U[ξ] is the [OutC][InC] GEMM matrix for transform position ξ.
type WinoFilter2x3 struct {
	U    [16][]float32
	InC  int
	OutC int
}

// initWinograd2x3 precomputes U = G g Gᵀ per (output channel, input channel).
// Call after BatchNorm folding so the transform sees the fused weights.
func (c *Conv2dBNSiLU) initWinograd2x3() {
	if c.KH != 3 || c.KW != 3 || c.Stride != 1 || c.Padding != 1 || c.Groups != 1 {
		return
	}
	wf := &WinoFilter2x3{InC: c.InC, OutC: c.OutC}
	g := c.Weight.Data // [OutC][InC][3][3]
	var t1 [4][3]float32 // G g  (4x3)
	var u [4][4]float32  // (G g) Gᵀ
	for m := 0; m < c.OutC; m++ {
		for ci := 0; ci < c.InC; ci++ {
			w := g[(m*c.InC+ci)*9:]
			// t1 = G(4x3) * g(3x3)
			for j := 0; j < 3; j++ {
				t1[0][j] = w[0*3+j]
				t1[1][j] = 0.5 * (w[0*3+j] + w[1*3+j] + w[2*3+j])
				t1[2][j] = 0.5 * (w[0*3+j] - w[1*3+j] + w[2*3+j])
				t1[3][j] = w[2*3+j]
			}
			// u = t1 * Gᵀ(3x4); Gᵀ columns: [1,.5,.5,0],[0,.5,-.5,0],[0,.5,.5,1]
			for i := 0; i < 4; i++ {
				u[i][0] = t1[i][0]
				u[i][1] = 0.5 * (t1[i][0] + t1[i][1] + t1[i][2])
				u[i][2] = 0.5 * (t1[i][0] - t1[i][1] + t1[i][2])
				u[i][3] = t1[i][2]
			}
			for xi := 0; xi < 16; xi++ {
				if wf.U[xi] == nil {
					wf.U[xi] = make([]float32, c.OutC*c.InC)
				}
				wf.U[xi][m*c.InC+ci] = u[xi/4][xi%4]
			}
		}
	}
	c.Wino2x3 = wf
}

// forwardWinograd2x3 computes the conv via Winograd F(2,3).
// Requires input H,W even (full 2x2 tiles). Returns nil if ineligible.
func (c *Conv2dBNSiLU) forwardWinograd2x3(input *Tensor) *Tensor {
	wf := c.Wino2x3
	if wf == nil || input.Shape[0] != 1 {
		return nil
	}
	H, W := input.Shape[2], input.Shape[3]
	if H%2 != 0 || W%2 != 0 {
		return nil
	}
	InC, OutC := wf.InC, wf.OutC
	TH, TW := H/2, W/2
	T := TH * TW
	Tpad := (T + 15) / 16 * 16

	// V[ξ][c][t]: per-ξ input slabs of [InC][Vldb]; the +16 row padding
	// avoids 4K store/load aliasing when Tpad*4 is a multiple of 4096.
	Vldb := Tpad + 16
	V := getBufUnzeroed(16 * InC * Vldb)
	defer putBuf(V)
	if Tpad != T {
		for r := 0; r < 16*InC; r++ {
			clear(V[r*Vldb+T : r*Vldb+Tpad])
		}
	}
	winoInputTransform(input.Data, InC, H, W, V, Tpad, Vldb)

	// Y[ξ][m][t]: per-ξ output slabs of [OutC][Tpad].
	Y := getBufUnzeroed(16 * OutC * Tpad)
	defer putBuf(Y)
	gemmWinograd16(wf, V, Y, OutC, InC, Tpad, Vldb)

	out := NewTensor(1, OutC, H, W)
	winoOutputTransform(Y, c.Bias, out.Data, OutC, H, W, Tpad, c.UseSiLU)
	return out
}

// winoInputTransform computes V = Bᵀ x B for every (channel, tile) and
// stores it ξ-slab-major. Pure ±1 transforms — adds only. Parallel over
// channel blocks. Organized by tile rows so the 16 ξ-streams are written as
// contiguous runs; interior tiles skip all bounds checks (border tiles are
// only the first/last row and column).
func winoInputTransform(data []float32, InC, H, W int, V []float32, Tpad, vstride int) {
	TW := W / 2
	TH := H / 2
	nWorkers := gemmWorkers
	if nWorkers > InC {
		nWorkers = InC
	}
	if InC < 8 {
		nWorkers = 1
	}
	var wg sync.WaitGroup
	per := (InC + nWorkers - 1) / nWorkers
	for wk := 0; wk < nWorkers; wk++ {
		c0 := wk * per
		c1 := c0 + per
		if c1 > InC {
			c1 = InC
		}
		if c0 >= c1 {
			break
		}
		wg.Add(1)
		go func(c0, c1 int) {
			defer wg.Done()
			locFlat := make([]float32, 16*TW)
			loc := func(xi, tx int) *float32 { return &locFlat[xi*TW+tx] }
			for c := c0; c < c1; c++ {
				chanOff := c * H * W
				for ty := 0; ty < TH; ty++ {
					rowOff := chanOff + (2*ty-1)*W
					yInterior := ty > 0 && ty < TH-1
					for tx := 0; tx < TW; tx++ {
						var x [4][4]float32
						if yInterior && tx > 0 && tx < TW-1 {
							base := rowOff + 2*tx - 1
							for i := 0; i < 4; i++ {
								r := data[base+i*W:]
								x[i][0] = r[0]
								x[i][1] = r[1]
								x[i][2] = r[2]
								x[i][3] = r[3]
							}
						} else {
							for i := 0; i < 4; i++ {
								ih := 2*ty + i - 1
								row := chanOff + ih*W
								for j := 0; j < 4; j++ {
									iw := 2*tx + j - 1
									if ih >= 0 && ih < H && iw >= 0 && iw < W {
										x[i][j] = data[row+iw]
									} else {
										x[i][j] = 0
									}
								}
							}
						}
						var t1 [4][4]float32
						for j := 0; j < 4; j++ {
							t1[0][j] = x[0][j] - x[2][j]
							t1[1][j] = x[1][j] + x[2][j]
							t1[2][j] = x[2][j] - x[1][j]
							t1[3][j] = x[1][j] - x[3][j]
						}
						*loc(0, tx) = t1[0][0] - t1[0][2]
						*loc(1, tx) = t1[0][1] + t1[0][2]
						*loc(2, tx) = t1[0][2] - t1[0][1]
						*loc(3, tx) = t1[0][1] - t1[0][3]
						*loc(4, tx) = t1[1][0] - t1[1][2]
						*loc(5, tx) = t1[1][1] + t1[1][2]
						*loc(6, tx) = t1[1][2] - t1[1][1]
						*loc(7, tx) = t1[1][1] - t1[1][3]
						*loc(8, tx) = t1[2][0] - t1[2][2]
						*loc(9, tx) = t1[2][1] + t1[2][2]
						*loc(10, tx) = t1[2][2] - t1[2][1]
						*loc(11, tx) = t1[2][1] - t1[2][3]
						*loc(12, tx) = t1[3][0] - t1[3][2]
						*loc(13, tx) = t1[3][1] + t1[3][2]
						*loc(14, tx) = t1[3][2] - t1[3][1]
						*loc(15, tx) = t1[3][1] - t1[3][3]
					}
					// Flush the tile row: 16 contiguous runs of TW floats.
					rowT := ty * TW
					for xi := 0; xi < 16; xi++ {
						dst := V[(xi*InC)*vstride+c*vstride+rowT:]
						copy(dst[:TW], locFlat[xi*TW:xi*TW+TW])
					}
				}
			}
		}(c0, c1)
	}
	wg.Wait()
}

// winoOutputTransform computes o = Aᵀ Y A + bias per tile and scatters into
// the [OutC][H*W] output. Tiles are processed in blocks so the 16 ξ-stream
// reads from Y advance in contiguous multi-KB runs; SiLU runs afterwards as
// a vectorized pass over each output plane (parallel over channels).
func winoOutputTransform(Y, bias []float32, out []float32, OutC, H, W, Tpad int, silu bool) {
	TW := W / 2
	T := (H / 2) * TW
	nWorkers := gemmWorkers
	if nWorkers > OutC {
		nWorkers = OutC
	}
	if OutC < 8 {
		nWorkers = 1
	}
	const blockT = 512
	var wg sync.WaitGroup
	per := (OutC + nWorkers - 1) / nWorkers
	for wk := 0; wk < nWorkers; wk++ {
		m0 := wk * per
		m1 := m0 + per
		if m1 > OutC {
			m1 = OutC
		}
		if m0 >= m1 {
			break
		}
		wg.Add(1)
		go func(m0, m1 int) {
			defer wg.Done()
			var loc [16][blockT]float32
			for m := m0; m < m1; m++ {
				yb := m * Tpad
				b := bias[m]
				od := out[m*H*W:]
				for t0 := 0; t0 < T; t0 += blockT {
					tn := blockT
					if t0+tn > T {
						tn = T - t0
					}
					// Gather the block: 16 contiguous runs of tn floats.
					for xi := 0; xi < 16; xi++ {
						src := Y[yb+xi*OutC*Tpad+t0:]
						copy(loc[xi][:tn], src[:tn])
					}
					for ti := 0; ti < tn; ti++ {
						t := t0 + ti
						ty, tx := t/TW, t%TW
						var tt [4][2]float32
						for i := 0; i < 4; i++ {
							y0 := loc[i*4+0][ti]
							y1 := loc[i*4+1][ti]
							y2 := loc[i*4+2][ti]
							y3 := loc[i*4+3][ti]
							tt[i][0] = y0 + y1 + y2
							tt[i][1] = y1 - y2 - y3
						}
						row0 := od[(2*ty)*W+2*tx:]
						row1 := od[(2*ty+1)*W+2*tx:]
						row0[0] = tt[0][0] + tt[1][0] + tt[2][0] + b
						row0[1] = tt[0][1] + tt[1][1] + tt[2][1] + b
						row1[0] = tt[1][0] - tt[2][0] - tt[3][0] + b
						row1[1] = tt[1][1] - tt[2][1] - tt[3][1] + b
					}
				}
				if silu {
					// NB: od extends to the end of the out tensor — restrict
					// the activation to this plane only (other workers own
					// the remaining planes).
					plane := od[:H*W]
					if hasAVX2FMA {
						body := len(plane) &^ 7
						if body >= 8 {
							siluAVX2(&plane[0], body)
						}
						for i := body; i < len(plane); i++ {
							plane[i] = plane[i] * sigmoid(plane[i])
						}
					} else {
						for i, v := range plane {
							plane[i] = v * sigmoid(v)
						}
					}
				}
			}
		}(m0, m1)
	}
	wg.Wait()
}

// gemmWinograd16 runs the 16 ξ-GEMMs (each [OutC,InC]×[InC,Tpad]) through a
// single parallel dispatch: every (panel × super-block) unit runs all 16 ξ
// sequentially, so goroutine setup is paid once per layer.
func gemmWinograd16(wf *WinoFilter2x3, V, Y []float32, OutC, InC, Tpad, Vldb int) {
	nPanels := (Tpad + gemmPanelWidth - 1) / gemmPanelWidth
	mFull := OutC / 6 * 6
	nSupers := (mFull + gemmSuper - 1) / gemmSuper
	nUnits := nPanels * (nSupers + 1) // +1 tail unit per panel
	zeroBias := make([]float32, OutC)
	nWorkers := gemmWorkers
	if nWorkers > nUnits {
		nWorkers = nUnits
	}
	if nWorkers <= 1 || int64(OutC)*int64(InC)*int64(Tpad)*16 < gemmSingleThreadMACs {
		gemmWinogradUnits(wf, V, Y, OutC, InC, Tpad, Vldb, zeroBias, nPanels, nSupers, 0, 1)
		return
	}
	var wg sync.WaitGroup
	for w := 0; w < nWorkers; w++ {
		wg.Add(1)
		go func(first int) {
			defer wg.Done()
			gemmWinogradUnits(wf, V, Y, OutC, InC, Tpad, Vldb, zeroBias, nPanels, nSupers, first, nWorkers)
		}(w)
	}
	wg.Wait()
}

func gemmWinogradUnits(wf *WinoFilter2x3, V, Y []float32, OutC, InC, Tpad, Vldb int, zeroBias []float32, nPanels, nSupers, first, step int) {
	aScratch := getBuf(6 * InC)
	defer putBuf(aScratch)
	biasScratch := make([]float32, 6)
	mFull := OutC / 6 * 6
	nUnits := nPanels * (nSupers + 1)
	for u := first; u < nUnits; u += step {
		p := u / (nSupers + 1)
		s := u % (nSupers + 1)
		if s == nSupers {
			// Tail rows, all 16 ξ.
			rem := OutC - mFull
			if rem == 0 {
				continue
			}
			for xi := 0; xi < 16; xi++ {
				gemmPanelTail(wf.U[xi], V[xi*InC*Vldb:], zeroBias, Y[xi*OutC*Tpad:], OutC, InC, Tpad, Vldb, p, false, aScratch, biasScratch)
			}
			continue
		}
		for xi := 0; xi < 16; xi++ {
			gemmSuperUnit(wf.U[xi], V[xi*InC*Vldb:], zeroBias, Y[xi*OutC*Tpad:], OutC, InC, Tpad, Vldb, p, s, mFull, false)
		}
	}
}
