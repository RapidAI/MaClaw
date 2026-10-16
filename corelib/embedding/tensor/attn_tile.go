package tensor

// attn_tile.go — 4-query tiled causal attention for head_dim 128 (arm64 NEON).
//
// Four consecutive query rows of one head share every K/V load: Q·Kᵀ runs as
// a 4×4 register tile (16 FMLA per 8 vector loads instead of 1:1), the
// softmax is vectorised (same Schraudolph exp as fastExp), and P·V runs as a
// 4-row × 16-dim tile with transposed probabilities. Causal masking: rows of
// the tile compute up to the last row's key count; entries past a row's own
// length are excluded from its softmax and get zero weight in P·V.

// AttnTile4Available reports whether AttnCausal4 has a kernel on this arch.
func AttnTile4Available() bool { return attnTile4Avail }

// AttnScratch4 returns the scratch length AttnCausal4 needs for nMax keys.
func AttnScratch4(nMax int) int { return 8*(nMax+4) + 8 }

// AttnCausal4 computes rows (len(nKeys) ≤ 4) of one attention head:
// out[r*outStride : +128] = softmax(scale·q_r·K[:nKeys[r]]ᵀ)·V[:nKeys[r]].
// q rows are at q[r*qStride], key/value s at k[s*kvStride], v[s*kvStride].
// nKeys must be non-decreasing. sc is scratch of AttnScratch4(nKeys[last]).
func AttnCausal4(out []float32, outStride int, q []float32, qStride int, k, v []float32, kvStride int, nKeys []int, scale float32, sc []float32) {
	R := len(nKeys)
	nMax := nKeys[R-1]
	ld := (nMax + 4) &^ 3
	S := sc[:4*ld]
	PT := sc[4*ld : 4*ld+4*ld]
	// Q·Kᵀ. Missing rows (R<4) reuse the last row's q (results ignored).
	var qbuf [4 * 128]float32
	qq, qs := q, qStride
	if R < 4 {
		for r := 0; r < 4; r++ {
			copy(qbuf[r*128:r*128+128], q[min(r, R-1)*qStride:min(r, R-1)*qStride+128])
		}
		qq, qs = qbuf[:], 128
	}
	nb := nMax / 4
	if nb > 0 {
		attnQK4x4(S, ld, qq, qs, k, kvStride, nb, scale)
	}
	for s := nb * 4; s < nMax; s++ {
		for r := 0; r < 4; r++ {
			S[r*ld+s] = Dot(qq[r*qs:r*qs+128], k[s*kvStride:s*kvStride+128]) * scale
		}
	}
	clear(PT[:4*nMax])
	for r := 0; r < R; r++ {
		row := S[r*ld : r*ld+nKeys[r]]
		mx := row[0]
		for _, x := range row[1:] {
			mx = max(mx, x)
		}
		sum := softmaxExpSum(row, mx)
		inv := float32(0)
		if sum != 0 {
			inv = 1 / sum
		}
		for s, p := range row {
			PT[s*4+r] = p * inv
		}
	}
	if R == 4 {
		attnPV4(out, outStride, PT, v, kvStride, nMax)
		return
	}
	var obuf [4 * 128]float32
	attnPV4(obuf[:], 128, PT, v, kvStride, nMax)
	for r := 0; r < R; r++ {
		copy(out[r*outStride:r*outStride+128], obuf[r*128:r*128+128])
	}
}
