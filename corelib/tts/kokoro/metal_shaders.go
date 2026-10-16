package kokoro

// kokoroMetalSrc: generator resblock kernels (AdaIN+snake, dilated conv1d as
// implicit GEMM with optional residual, accumulate). Compiled without fast
// math so sin/sqrt match the CPU closely.
const kokoroMetalSrc = `
#include <metal_stdlib>
using namespace metal;

struct AdaP { int C, T; };

// One 256-thread group per channel: instance norm with style affine, then
// snake: d + sin(a*d)^2 / a.
kernel void adain_snake(device const float* x [[buffer(0)]],
		device float* y [[buffer(1)]],
		device const float* gb [[buffer(2)]],   // [gamma(C), beta(C)]
		device const float* alpha [[buffer(3)]],
		constant AdaP& p [[buffer(4)]],
		uint c [[threadgroup_position_in_grid]],
		uint t [[thread_index_in_threadgroup]]) {
	threadgroup float rs[256], rq[256];
	device const float* row = x + (long)c * p.T;
	float s = 0, q = 0;
	for (int i = t; i < p.T; i += 256) { float v = row[i]; s += v; q += v*v; }
	rs[t] = s; rq[t] = q;
	threadgroup_barrier(mem_flags::mem_threadgroup);
	for (int k = 128; k > 0; k >>= 1) {
		if ((int)t < k) { rs[t] += rs[t+k]; rq[t] += rq[t+k]; }
		threadgroup_barrier(mem_flags::mem_threadgroup);
	}
	float mean = rs[0] / float(p.T);
	float var = rq[0] / float(p.T) - mean*mean;
	if (var < 0) var = 0;
	float inv = 1.0f / sqrt(var + 1e-5f);
	float scale = inv * (1.0f + gb[c]);
	float beta = gb[p.C + c];
	float a = alpha[c]; if (a == 0) a = 1;
	float ia = 1.0f / a;
	device float* out = y + (long)c * p.T;
	for (int i = t; i < p.T; i += 256) {
		float d = (row[i] - mean) * scale + beta;
		float sn = sin(a * d);
		out[i] = d + (sn*sn) * ia;
	}
}

struct ConvP { int C, M, T, kern, pad, dil, K, Kpad, res, Tin, stride; };

// out[m][t] = bias[m] + sum_k W[m][k] * x[c][t - pad + kk*dil] (+ res[m][t]).
// Tile 32 (m) x 64 (t), K step 32, 128 threads (4 simdgroups).
kernel void conv1d(device const float* x [[buffer(0)]],
		device const float* w [[buffer(1)]],
		device const float* bias [[buffer(2)]],
		device float* y [[buffer(3)]],
		constant ConvP& p [[buffer(4)]],
		uint3 tg [[threadgroup_position_in_grid]],
		uint t [[thread_index_in_threadgroup]],
		uint sg [[simdgroup_index_in_threadgroup]]) {
	threadgroup float As[32*32];
	threadgroup float Bs[32*64];
	int t0 = tg.x * 64, m0 = tg.y * 32;
	simdgroup_float8x8 acc[4][2];
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) acc[i][j] = simdgroup_float8x8(0);
	int bp = t / 2, bk = (t % 2) * 16;
	int tt = t0 + bp;
	int am = t / 4, ak = (t % 4) * 8;
	for (int k0 = 0; k0 < p.K; k0 += 32) {
		int mrow = m0 + am;
		for (int i = 0; i < 8; i++) As[am*32 + ak + i] = mrow < p.M ? w[(long)mrow * p.Kpad + k0 + ak + i] : 0.0f;
		for (int i = 0; i < 16; i++) {
			int k = k0 + bk + i;
			float v = 0.0f;
			if (tt < p.T && k < p.K) {
				int c = k / p.kern, kk = k % p.kern;
				int it = tt * p.stride - p.pad + kk * p.dil;
				if (it >= 0 && it < p.Tin) v = x[(long)c * p.Tin + it];
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
	for (int i = 0; i < 4; i++) for (int j = 0; j < 2; j++) simdgroup_store(acc[i][j], Bs + (i*8)*64 + sg*16 + j*8, 64);
	threadgroup_barrier(mem_flags::mem_threadgroup);
	for (int e = t; e < 32*64; e += 128) {
		int m = m0 + e / 64, q = t0 + e % 64;
		if (m < p.M && q < p.T) {
			float v = Bs[e] + bias[m];
			long o = (long)m * p.T + q;
			if (p.res) v += y[o];
			y[o] = v;
		}
	}
}

struct CTP { int inC, outC, inT, outT, kern, stride, pad, hasBias; };

// Transposed conv (groups=1), gather form: one thread per output element.
// w layout [inC][outC][kern].
kernel void convt1d(device const float* x [[buffer(0)]],
		device const float* w [[buffer(1)]],
		device const float* bias [[buffer(2)]],
		device float* y [[buffer(3)]],
		constant CTP& p [[buffer(4)]],
		uint2 gid [[thread_position_in_grid]]) {
	int ot = gid.x, oc = gid.y;
	if (ot >= p.outT || oc >= p.outC) return;
	int base = ot + p.pad;
	int k0 = base % p.stride;
	float s = 0.0f;
	for (int k = k0; k < p.kern; k += p.stride) {
		int it = (base - k) / p.stride;
		if (it < 0) break;
		if (it >= p.inT) continue;
		device const float* wk = w + (long)oc * p.kern + k;
		long ws = (long)p.outC * p.kern;
		for (int ic = 0; ic < p.inC; ic++) s += x[(long)ic * p.inT + it] * wk[ic * ws];
	}
	if (p.hasBias) s += bias[oc];
	y[(long)oc * p.outT + ot] = s;
}

struct AccP { int n, mode; float s; };
// mode 0: y = x; 1: y += x; 2: y = (y + x) * s
kernel void accum(device const float* x [[buffer(0)]],
		device float* y [[buffer(1)]],
		constant AccP& p [[buffer(2)]],
		uint i [[thread_position_in_grid]]) {
	if ((int)i >= p.n) return;
	if (p.mode == 0) y[i] = x[i];
	else if (p.mode == 1) y[i] += x[i];
	else y[i] = (y[i] + x[i]) * p.s;
}
`
