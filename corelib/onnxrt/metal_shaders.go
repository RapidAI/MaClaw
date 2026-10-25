package onnxrt

// onnxMetalSrc holds the MSL kernels of the onnxrt Metal backend. They are
// compiled with fast math off so exp/div/erf stay IEEE-close to the CPU path.
const onnxMetalSrc = `
#include <metal_stdlib>
using namespace metal;

struct ConvP {
	int N, C, H, W, M, Cg, Mg, group;
	int kH, kW, sH, sW, pT, pL, dH, dW;
	int oH, oW, K, Kpad;
	int epi, hasBias; float alpha, beta;
};

static inline float erf_as(float x) {
	float ax = fabs(x);
	float t = 1.0f / (1.0f + 0.3275911f * ax);
	float p = ((((1.061405429f*t - 1.453152027f)*t + 1.421413741f)*t - 0.284496736f)*t + 0.254829592f)*t;
	float e = exp(max(-ax*ax, -80.0f));
	float r = 1.0f - p*e;
	return x < 0 ? -r : r;
}

static inline float epilogue(float v, int epi, float a, float b) {
	switch (epi) {
	case 1: return max(v, 0.0f);
	case 2: { float z = clamp(-v, -80.0f, 80.0f); return 1.0f / (1.0f + exp(z)); }
	case 3: return clamp(a*v + b, 0.0f, 1.0f);
	case 4: return v * clamp(a*v + b, 0.0f, 1.0f);
	case 5: return 0.5f * v * (1.0f + erf_as(v * 0.70710678118654752f));
	}
	return v;
}

// Implicit-GEMM conv: out[m][pix] = sum_k W[m][k] * im2col[k][pix].
// Tile 32 (m) x 64 (pix), K step 32, 128 threads = 4 simdgroups (each 32x16).
kernel void conv_gemm(device const float* x [[buffer(0)]],
		device const float* w [[buffer(1)]],
		device const float* bias [[buffer(2)]],
		device float* y [[buffer(3)]],
		constant ConvP& p [[buffer(4)]],
		uint3 tg [[threadgroup_position_in_grid]],
		uint t [[thread_index_in_threadgroup]],
		uint sg [[simdgroup_index_in_threadgroup]]) {
	threadgroup float As[32*32];
	threadgroup float Bs[32*64];
	int P = p.oH * p.oW;
	int pix0 = tg.x * 64, m0 = tg.y * 32;
	int ng = tg.z; int n = ng / p.group, g = ng % p.group;
	device const float* xg = x + ((long)n * p.C + (long)g * p.Cg) * p.H * p.W;
	device const float* wg = w + (long)g * p.Mg * p.Kpad;
	int khw = p.kH * p.kW;
	simdgroup_float8x8 acc[4][2];
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) acc[i][j] = simdgroup_float8x8(0);
	// B-load mapping: pixel bp, k range bk..bk+15
	int bp = t / 2, bk = (t % 2) * 16;
	int pix = pix0 + bp;
	int oh = pix / p.oW, ow = pix % p.oW;
	bool pv = pix < P;
	int ih0 = oh * p.sH - p.pT, iw0 = ow * p.sW - p.pL;
	int am = t / 4, ak = (t % 4) * 8;
	for (int k0 = 0; k0 < p.K; k0 += 32) {
		int mrow = m0 + am;
		for (int i = 0; i < 8; i++) {
			As[am*32 + ak + i] = mrow < p.Mg ? wg[(long)mrow * p.Kpad + k0 + ak + i] : 0.0f;
		}
		for (int i = 0; i < 16; i++) {
			int k = k0 + bk + i;
			float v = 0.0f;
			if (pv && k < p.K) {
				int c = k / khw, r = k % khw;
				int kh = r / p.kW, kw = r % p.kW;
				int ih = ih0 + kh * p.dH, iw = iw0 + kw * p.dW;
				if (ih >= 0 && ih < p.H && iw >= 0 && iw < p.W) v = xg[((long)c * p.H + ih) * p.W + iw];
			}
			Bs[(bk + i) * 64 + bp] = v;
		}
		threadgroup_barrier(mem_flags::mem_threadgroup);
		for (int kk = 0; kk < 32; kk += 8) {
			simdgroup_float8x8 a[4], b[2];
			for (int i = 0; i < 4; i++) simdgroup_load(a[i], As + (i*8)*32 + kk, 32);
			for (int j = 0; j < 2; j++) simdgroup_load(b[j], Bs + kk*64 + sg*16 + j*8, 64);
			for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) simdgroup_multiply_accumulate(acc[i][j], a[i], b[j], acc[i][j]);
		}
		threadgroup_barrier(mem_flags::mem_threadgroup);
	}
	// stage the 32x64 result through threadgroup memory for bounds-checked stores
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) simdgroup_store(acc[i][j], Bs + (i*8)*64 + sg*16 + j*8, 64);
	threadgroup_barrier(mem_flags::mem_threadgroup);
	device float* yg = y + ((long)n * p.M + (long)g * p.Mg) * P;
	for (int e = t; e < 32*64; e += 128) {
		int mi = e / 64, pj = e % 64;
		int m = m0 + mi, q = pix0 + pj;
		if (m < p.Mg && q < P) {
			float v = Bs[e];
			if (p.hasBias) v += bias[g * p.Mg + m];
			yg[(long)m * P + q] = epilogue(v, p.epi, p.alpha, p.beta);
		}
	}
}

// Pointwise conv (kH=kW=1, stride 1, no pad). Same 32x64 tile and the same
// simdgroup FMA order as conv_gemm, but activations are contiguous per
// channel so the B tile is float4 loads instead of a per-tap gather.
kernel void conv_1x1(device const float* x [[buffer(0)]],
		device const float* w [[buffer(1)]],
		device const float* bias [[buffer(2)]],
		device float* y [[buffer(3)]],
		constant ConvP& p [[buffer(4)]],
		uint3 tg [[threadgroup_position_in_grid]],
		uint t [[thread_index_in_threadgroup]],
		uint sg [[simdgroup_index_in_threadgroup]]) {
	threadgroup float As[32*32];
	threadgroup float Bs[32*64];
	int P = p.oH * p.oW;
	int pix0 = tg.x * 64, m0 = tg.y * 32;
	int ng = tg.z; int n = ng / p.group, g = ng % p.group;
	device const float* xg = x + ((long)n * p.C + (long)g * p.Cg) * p.H * p.W;
	device const float* wg = w + (long)g * p.Mg * p.Kpad;
	simdgroup_float8x8 acc[4][2];
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) acc[i][j] = simdgroup_float8x8(0);
	int am = t / 4, ak = (t % 4) * 8;
	for (int k0 = 0; k0 < p.K; k0 += 32) {
		int mrow = m0 + am;
		if (mrow < p.Mg) {
			device const float* wr = wg + (long)mrow * p.Kpad + k0 + ak;
			float4 a0 = *reinterpret_cast<device const float4*>(wr);
			float4 a1 = *reinterpret_cast<device const float4*>(wr + 4);
			As[am*32 + ak + 0] = a0.x; As[am*32 + ak + 1] = a0.y;
			As[am*32 + ak + 2] = a0.z; As[am*32 + ak + 3] = a0.w;
			As[am*32 + ak + 4] = a1.x; As[am*32 + ak + 5] = a1.y;
			As[am*32 + ak + 6] = a1.z; As[am*32 + ak + 7] = a1.w;
		} else {
			for (int i = 0; i < 8; i++) As[am*32 + ak + i] = 0.0f;
		}
		// 32 channels x 16 float4 pixel groups = 512 loads, 4 per thread.
		for (int rep = 0; rep < 4; rep++) {
			int e = (int)t + rep * 128;
			int kk = e >> 4;
			int p4 = e & 15;
			int pix = pix0 + p4 * 4;
			int k = k0 + kk;
			float4 v = float4(0.0f);
			// float4 requires a 4-float aligned address. Odd planes (the
			// self-check's 11x19) stay scalar; encoder maps are multiples of 4.
			long ix = (long)k * P + pix;
			if (k < p.K && pix + 3 < P && (ix & 3) == 0) {
				v = *reinterpret_cast<device const float4*>(xg + ix);
			} else if (k < p.K) {
				for (int i = 0; i < 4; i++) {
					if (pix + i < P) v[i] = xg[(long)k * P + pix + i];
				}
			}
			Bs[kk * 64 + p4 * 4 + 0] = v.x;
			Bs[kk * 64 + p4 * 4 + 1] = v.y;
			Bs[kk * 64 + p4 * 4 + 2] = v.z;
			Bs[kk * 64 + p4 * 4 + 3] = v.w;
		}
		threadgroup_barrier(mem_flags::mem_threadgroup);
		for (int kk = 0; kk < 32; kk += 8) {
			simdgroup_float8x8 a[4], b[2];
			for (int i = 0; i < 4; i++) simdgroup_load(a[i], As + (i*8)*32 + kk, 32);
			for (int j = 0; j < 2; j++) simdgroup_load(b[j], Bs + kk*64 + sg*16 + j*8, 64);
			for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) simdgroup_multiply_accumulate(acc[i][j], a[i], b[j], acc[i][j]);
		}
		threadgroup_barrier(mem_flags::mem_threadgroup);
	}
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) simdgroup_store(acc[i][j], Bs + (i*8)*64 + sg*16 + j*8, 64);
	threadgroup_barrier(mem_flags::mem_threadgroup);
	device float* yg = y + ((long)n * p.M + (long)g * p.Mg) * P;
	for (int e = t; e < 32*64; e += 128) {
		int mi = e / 64, pj = e % 64;
		int m = m0 + mi, q = pix0 + pj;
		if (m < p.Mg && q < P) {
			float v = Bs[e];
			if (p.hasBias) v += bias[g * p.Mg + m];
			yg[(long)m * P + q] = epilogue(v, p.epi, p.alpha, p.beta);
		}
	}
}

// Depthwise conv (multiplier 1): one thread per output element.
kernel void conv_dw(device const float* x [[buffer(0)]],
		device const float* w [[buffer(1)]],
		device const float* bias [[buffer(2)]],
		device float* y [[buffer(3)]],
		constant ConvP& p [[buffer(4)]],
		uint gid [[thread_position_in_grid]]) {
	int P = p.oH * p.oW;
	int total = p.N * p.C * P;
	if ((int)gid >= total) return;
	int q = gid % P, nc = gid / P, c = nc % p.C;
	int oh = q / p.oW, ow = q % p.oW;
	device const float* xc = x + (long)nc * p.H * p.W;
	device const float* wc = w + (long)c * p.kH * p.kW;
	float s = 0.0f;
	// 3x3 and 5x5 with unit dilation are the formula encoder's depthwise
	// layers. The row pointer stays live across the kernel width.
	if (p.dH == 1 && p.dW == 1 && p.kW == p.kH && (p.kH == 3 || p.kH == 5)) {
		int tap = 0;
		int iw0 = ow * p.sW - p.pL;
		for (int kh = 0; kh < p.kH; kh++) {
			int ih = oh * p.sH - p.pT + kh;
			if ((uint)ih >= (uint)p.H) {
				tap += p.kW;
				continue;
			}
			device const float* row = xc + ih * p.W;
			for (int kw = 0; kw < p.kW; kw++) {
				int iw = iw0 + kw;
				if ((uint)iw < (uint)p.W) s += row[iw] * wc[tap];
				tap++;
			}
		}
	} else {
		for (int kh = 0; kh < p.kH; kh++) {
			int ih = oh * p.sH - p.pT + kh * p.dH;
			if (ih < 0 || ih >= p.H) continue;
			for (int kw = 0; kw < p.kW; kw++) {
				int iw = ow * p.sW - p.pL + kw * p.dW;
				if (iw < 0 || iw >= p.W) continue;
				s += xc[ih * p.W + iw] * wc[kh * p.kW + kw];
			}
		}
	}
	if (p.hasBias) s += bias[c];
	y[gid] = epilogue(s, p.epi, p.alpha, p.beta);
}

struct BinP { int n, op; int od[4]; int as[4]; int bs[4]; };

kernel void binary4(device const float* a [[buffer(0)]],
		device const float* b [[buffer(1)]],
		device float* y [[buffer(2)]],
		constant BinP& p [[buffer(3)]],
		uint gid [[thread_position_in_grid]]) {
	if ((int)gid >= p.n) return;
	int r = gid, ia = 0, ib = 0;
	for (int d = 3; d >= 0; d--) {
		int i = r % p.od[d]; r /= p.od[d];
		ia += i * p.as[d]; ib += i * p.bs[d];
	}
	float u = a[ia], v = b[ib], o;
	switch (p.op) {
	case 0: o = u + v; break;
	case 1: o = u - v; break;
	case 2: o = u * v; break;
	default: o = u / v; break;
	}
	y[gid] = o;
}

struct UnP { int n, op; float alpha, beta; };

kernel void unary(device const float* x [[buffer(0)]],
		device float* y [[buffer(1)]],
		constant UnP& p [[buffer(2)]],
		uint gid [[thread_position_in_grid]]) {
	if ((int)gid >= p.n) return;
	float v = x[gid], o;
	switch (p.op) {
	case 0: o = max(v, 0.0f); break;
	case 1: { float z = clamp(-v, -80.0f, 80.0f); o = 1.0f / (1.0f + exp(z)); break; }
	case 2: o = clamp(p.alpha*v + p.beta, 0.0f, 1.0f); break;
	case 3: o = sqrt(v); break;
	default: o = erf_as(v); break;
	}
	y[gid] = o;
}

// Global average pool: one 256-thread group per (n,c).
kernel void gap(device const float* x [[buffer(0)]],
		device float* y [[buffer(1)]],
		constant int& hw [[buffer(2)]],
		uint tg [[threadgroup_position_in_grid]],
		uint t [[thread_index_in_threadgroup]]) {
	threadgroup float red[256];
	device const float* xc = x + (long)tg * hw;
	float s = 0.0f;
	for (int i = t; i < hw; i += 256) s += xc[i];
	red[t] = s;
	threadgroup_barrier(mem_flags::mem_threadgroup);
	for (int k = 128; k > 0; k >>= 1) {
		if ((int)t < k) red[t] += red[t + k];
		threadgroup_barrier(mem_flags::mem_threadgroup);
	}
	if (t == 0) y[tg] = red[0] / float(hw);
}
`

// onnxMetalGridSrc is compiled on its own. A shader failure must not join
// the conv/elementwise pipeline list: that list's self-check disables every
// Metal op.
const onnxMetalGridSrc = `
#include <metal_stdlib>
using namespace metal;

struct GridP {
	int N, C, H, W, oH, oW;
	int alignCorners;
};

static inline float gs_at(device const float* plane, int w, int h, int y, int x) {
	if ((unsigned)x >= (unsigned)w || (unsigned)y >= (unsigned)h) return 0.0f;
	return plane[y * w + x];
}

// Bilinear zeros-padding GridSample. One thread per NCHW output element.
// Coordinate math is float32; the CPU path promotes the grid to float64.
// The separate self-check compares the 2×2 case (2.5 at the center, 0.25
// at the zeros corner) and disables only this kernel on mismatch.
kernel void grid_sample(device const float* x [[buffer(0)]],
		device const float* grid [[buffer(1)]],
		device float* y [[buffer(2)]],
		constant GridP& p [[buffer(3)]],
		uint gid [[thread_position_in_grid]]) {
	int oPlane = p.oH * p.oW;
	int total = p.N * p.C * oPlane;
	if ((int)gid >= total || p.H <= 0 || p.W <= 0) return;
	int pix = (int)gid % oPlane;
	int nc = (int)gid / oPlane;
	int c = nc % p.C;
	int n = nc / p.C;
	int oy = pix / p.oW;
	int ox = pix - oy * p.oW;
	int g = ((n * p.oH + oy) * p.oW + ox) * 2;
	float gx = grid[g];
	float gy = grid[g + 1];
	float ix, iy;
	if (p.alignCorners) {
		ix = (gx + 1.0f) * float(p.W - 1) * 0.5f;
		iy = (gy + 1.0f) * float(p.H - 1) * 0.5f;
	} else {
		ix = ((gx + 1.0f) * float(p.W) - 1.0f) * 0.5f;
		iy = ((gy + 1.0f) * float(p.H) - 1.0f) * 0.5f;
	}
	float x0 = floor(ix);
	float y0 = floor(iy);
	float wx = ix - x0;
	float wy = iy - y0;
	int X0 = int(x0);
	int Y0 = int(y0);
	device const float* plane = x + ((long)n * p.C + c) * p.H * p.W;
	float v00 = gs_at(plane, p.W, p.H, Y0, X0);
	float v01 = gs_at(plane, p.W, p.H, Y0, X0 + 1);
	float v10 = gs_at(plane, p.W, p.H, Y0 + 1, X0);
	float v11 = gs_at(plane, p.W, p.H, Y0 + 1, X0 + 1);
	y[gid] = v00 * (1.0f - wx) * (1.0f - wy) + v01 * wx * (1.0f - wy)
		+ v10 * (1.0f - wx) * wy + v11 * wx * wy;
}
`
