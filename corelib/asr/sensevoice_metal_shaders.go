package asr

// svMetalSource is the Metal Shading Language source for the SenseVoice
// encoder on the GPU (sensevoice_metal_darwin.go), compiled at runtime.
// float32 throughout; Q8_0 weights are dequantised in-kernel. The attention
// softmax uses the same Schraudolph exp as the CPU kernels.
const svMetalSource = `
#include <metal_stdlib>
using namespace metal;

struct MMParams { int M; int N; int K; int lda; int ldc; int accum; int relu; int hasBias; int splitBlocks; int _pad; long splitStride; };
struct LNParams { int M; int D; int ldx; int ldo; float eps; };
struct AttnParams { int M; int nH; float scale; int ld; int kOff; int vOff; int ldo; };
struct FSMNParams { int M; int D; int ld; int vOff; int ks; int residual; };
struct ReduceParams { int M; int N; int splits; int accum; int relu; int hasBias; long stride; };

static inline float fast_exp(float x) {
    if (x < -88.0f) return 0.0f;
    if (x > 88.0f) return FLT_MAX;
    return as_type<float>(int(12102203.0f * x + 1065292415.0f));
}

// LayerNorm with affine, one 256-thread group per row (in place allowed).
kernel void layernorm_rows(device const float* x [[buffer(0)]],
                           device const float* w [[buffer(1)]],
                           device const float* b [[buffer(2)]],
                           device float* out [[buffer(3)]],
                           constant LNParams& p [[buffer(4)]],
                           uint r [[threadgroup_position_in_grid]],
                           uint t [[thread_index_in_threadgroup]],
                           uint sg [[simdgroup_index_in_threadgroup]],
                           uint ln [[thread_index_in_simdgroup]]) {
    threadgroup float part[8];
    device const float* xr = x + (long)r * p.ldx;
    float s = 0.0f;
    for (int i = t; i < p.D; i += 256) s += xr[i];
    s = simd_sum(s);
    if (ln == 0) part[sg] = s;
    threadgroup_barrier(mem_flags::mem_threadgroup);
    float mean = 0.0f;
    for (int i = 0; i < 8; i++) mean += part[i];
    mean /= float(p.D);
    threadgroup_barrier(mem_flags::mem_threadgroup);
    float v = 0.0f;
    for (int i = t; i < p.D; i += 256) { float d = xr[i] - mean; v += d * d; }
    v = simd_sum(v);
    if (ln == 0) part[sg] = v;
    threadgroup_barrier(mem_flags::mem_threadgroup);
    float var = 0.0f;
    for (int i = 0; i < 8; i++) var += part[i];
    float inv = 1.0f / sqrt(var / float(p.D) + p.eps);
    device float* o = out + (long)r * p.ldo;
    for (int i = t; i < p.D; i += 256) o[i] = (xr[i] - mean) * inv * w[i] + b[i];
}

// C[M×N] (=|+=) epilogue(A[M×K] · Wᵀ + bias). W is Q8 (int8 qs [N×K] + f32
// scales [N×K/32]) or f32 [N×K]. Tile 32×64, 4 simdgroups of 16×32, K step 32.
#define TM 32
#define TN 64
#define TK 32
template<bool Q8>
kernel void matmul_t(device const float* A [[buffer(0)]],
                     device const void* W [[buffer(1)]],
                     device const float* Ws [[buffer(2)]],
                     device float* C [[buffer(3)]],
                     constant MMParams& p [[buffer(4)]],
                     device const float* bias [[buffer(5)]],
                     uint3 tg [[threadgroup_position_in_grid]],
                     uint t [[thread_index_in_threadgroup]],
                     uint sg [[simdgroup_index_in_threadgroup]]) {
    threadgroup float smem[TM * TK + TK * TN];
    threadgroup float* As = smem;
    threadgroup float* Bs = smem + TM * TK;
    const int n0 = tg.x * TN;
    const int m0 = tg.y * TM;
    const int nb = p.K / 32;
    const int sm = (sg >> 1) * 16;
    const int sn = (sg & 1) * 32;
    simdgroup_float8x8 c[2][4];
    for (int i = 0; i < 2; i++) for (int j = 0; j < 4; j++) c[i][j] = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
    const int ar = t >> 2, ac = (t & 3) * 8;
    const bool arow = (m0 + ar) < p.M;
    device const float* Ap = A + (long)(m0 + ar) * p.lda + ac;
    const int bn = t >> 1, bk = (t & 1) * 16;
    const int kbBeg = tg.z * p.splitBlocks;
    const int kbEnd = min(nb, kbBeg + p.splitBlocks);
    C += tg.z * p.splitStride;
    for (int kb = kbBeg; kb < kbEnd; kb++) {
        const int k0 = kb * TK;
        float4 a0 = 0.0f, a1 = 0.0f;
        if (arow) { a0 = *(device const float4*)(Ap + k0); a1 = *(device const float4*)(Ap + k0 + 4); }
        float4 f0, f1, f2, f3;
        if (Q8) {
            device const char* Wp = (device const char*)W + (long)(n0 + bn) * p.K + bk + k0;
            float s = Ws[(long)(n0 + bn) * nb + kb];
            f0 = float4(*(device const char4*)(Wp)) * s;
            f1 = float4(*(device const char4*)(Wp + 4)) * s;
            f2 = float4(*(device const char4*)(Wp + 8)) * s;
            f3 = float4(*(device const char4*)(Wp + 12)) * s;
        } else {
            device const float* Wp = (device const float*)W + (long)(n0 + bn) * p.K + bk + k0;
            f0 = *(device const float4*)(Wp); f1 = *(device const float4*)(Wp + 4);
            f2 = *(device const float4*)(Wp + 8); f3 = *(device const float4*)(Wp + 12);
        }
        threadgroup_barrier(mem_flags::mem_threadgroup);
        *(threadgroup float4*)(As + ar * TK + ac) = a0;
        *(threadgroup float4*)(As + ar * TK + ac + 4) = a1;
        threadgroup float* bp = Bs + bk * TN + bn;
        bp[0*TN] = f0.x; bp[1*TN] = f0.y; bp[2*TN] = f0.z; bp[3*TN] = f0.w;
        bp[4*TN] = f1.x; bp[5*TN] = f1.y; bp[6*TN] = f1.z; bp[7*TN] = f1.w;
        bp[8*TN] = f2.x; bp[9*TN] = f2.y; bp[10*TN] = f2.z; bp[11*TN] = f2.w;
        bp[12*TN] = f3.x; bp[13*TN] = f3.y; bp[14*TN] = f3.z; bp[15*TN] = f3.w;
        threadgroup_barrier(mem_flags::mem_threadgroup);
        for (int kk = 0; kk < TK; kk += 8) {
            simdgroup_float8x8 af[2], bf[4];
            simdgroup_load(af[0], As + (sm + 0) * TK + kk, TK);
            simdgroup_load(af[1], As + (sm + 8) * TK + kk, TK);
            for (int j = 0; j < 4; j++) simdgroup_load(bf[j], Bs + kk * TN + sn + j * 8, TN);
            for (int i = 0; i < 2; i++) for (int j = 0; j < 4; j++) simdgroup_multiply_accumulate(c[i][j], af[i], bf[j], c[i][j]);
        }
    }
    threadgroup_barrier(mem_flags::mem_threadgroup);
    threadgroup float* Cs = smem;
    for (int i = 0; i < 2; i++) for (int j = 0; j < 4; j++)
        simdgroup_store(c[i][j], Cs + (sm + i * 8) * TN + sn + j * 8, TN);
    threadgroup_barrier(mem_flags::mem_threadgroup);
    const bool raw = p.splitStride != 0; // split-K partial: epilogue in mm_reduce
    for (int e = t; e < TM * TN; e += 128) {
        int r = e / TN, cc = e % TN;
        if (m0 + r < p.M) {
            device float* cp = C + (long)(m0 + r) * p.ldc + n0 + cc;
            float v = Cs[e];
            if (!raw) {
                if (p.hasBias) v += bias[n0 + cc];
                if (p.relu) v = max(v, 0.0f);
                if (p.accum) v += *cp;
            }
            *cp = v;
        }
    }
}
template [[host_name("matmul_q8")]] kernel void matmul_t<true>(device const float*, device const void*, device const float*, device float*, constant MMParams&, device const float*, uint3, uint, uint);
template [[host_name("matmul_f32")]] kernel void matmul_t<false>(device const float*, device const void*, device const float*, device float*, constant MMParams&, device const float*, uint3, uint, uint);

kernel void mm_reduce(device const float* P [[buffer(0)]],
                      device float* C [[buffer(1)]],
                      constant ReduceParams& p [[buffer(2)]],
                      device const float* bias [[buffer(3)]],
                      uint2 gid [[thread_position_in_grid]]) {
    const int c = gid.x, r = gid.y;
    if (r >= p.M || c >= p.N) return;
    long o = (long)r * p.N + c;
    float v = P[o];
    for (int z = 1; z < p.splits; z++) v += P[z * p.stride + o];
    if (p.hasBias) v += bias[c];
    if (p.relu) v = max(v, 0.0f);
    if (p.accum) v += C[o];
    C[o] = v;
}

// out[f][c] += sum_k V[f+k-pad][c]*w[k][c] + V[f][c] (+ x[f][c] if residual).
kernel void fsmn_add(device const float* qkv [[buffer(0)]],
                     device const float* w [[buffer(1)]],
                     device const float* x [[buffer(2)]],
                     device float* out [[buffer(3)]],
                     constant FSMNParams& p [[buffer(4)]],
                     uint2 gid [[thread_position_in_grid]]) {
    const int c = gid.x, f = gid.y;
    if (c >= p.D || f >= p.M) return;
    const int pad = (p.ks - 1) / 2;
    device const float* v = qkv + p.vOff + c;
    float s = 0.0f;
    for (int k = 0; k < p.ks; k++) {
        int src = f + k - pad;
        if (src >= 0 && src < p.M) s += v[(long)src * p.ld] * w[k * p.D + c];
    }
    s += v[(long)f * p.ld];
    long o = (long)f * p.D + c;
    if (p.residual) s += x[o];
    out[o] += s;
}

// Full (bidirectional) multi-head attention, head_dim 128, simdgroup-tiled:
// one simdgroup per (8-query block, head); 4 heads per threadgroup.
kernel void attention_full(device const float* qkv [[buffer(0)]],
                           device float* out [[buffer(1)]],
                           constant AttnParams& p [[buffer(2)]],
                           uint2 tg [[threadgroup_position_in_grid]],
                           uint sg [[simdgroup_index_in_threadgroup]],
                           uint ln [[thread_index_in_simdgroup]]) {
    threadgroup float Ss[4][64];
    threadgroup float Os[4][8 * 128];
    const int h = tg.y * 4 + sg;
    if (h >= p.nH) return;
    const int r0 = tg.x * 8;
    const int lr = ln >> 2, lc = (ln & 3) * 2;
    const int myRow = r0 + lr;
    const bool rowOK = myRow < p.M;
    const int nkb = (p.M + 7) / 8;
    device const float* qb = qkv + (long)r0 * p.ld + h * 128;
    device const float* kb = qkv + p.kOff + h * 128;
    device const float* vb = qkv + p.vOff + h * 128;
    threadgroup float* S = Ss[sg];
    simdgroup_float8x8 qf[16];
    for (int c = 0; c < 16; c++) simdgroup_load(qf[c], qb + c * 8, p.ld);
    float m = -FLT_MAX;
    for (int b = 0; b < nkb; b++) {
        const int s0 = b * 8;
        simdgroup_float8x8 acc = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
        device const float* kp = kb + (long)s0 * p.ld;
        for (int c = 0; c < 16; c++) {
            simdgroup_float8x8 kt;
            simdgroup_load(kt, kp + c * 8, p.ld, ulong2(0, 0), true);
            simdgroup_multiply_accumulate(acc, qf[c], kt, acc);
        }
        simdgroup_store(acc, S, 8);
        simdgroup_barrier(mem_flags::mem_threadgroup);
        for (int j = 0; j < 2; j++) {
            if (s0 + lc + j < p.M) m = max(m, S[lr * 8 + lc + j] * p.scale);
        }
        simdgroup_barrier(mem_flags::mem_threadgroup);
    }
    m = max(m, simd_shuffle_xor(m, 1));
    m = max(m, simd_shuffle_xor(m, 2));
    simdgroup_float8x8 o[16];
    for (int c = 0; c < 16; c++) o[c] = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
    float l = 0.0f;
    for (int b = 0; b < nkb; b++) {
        const int s0 = b * 8;
        simdgroup_float8x8 acc = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
        device const float* kp = kb + (long)s0 * p.ld;
        for (int c = 0; c < 16; c++) {
            simdgroup_float8x8 kt;
            simdgroup_load(kt, kp + c * 8, p.ld, ulong2(0, 0), true);
            simdgroup_multiply_accumulate(acc, qf[c], kt, acc);
        }
        simdgroup_store(acc, S, 8);
        simdgroup_barrier(mem_flags::mem_threadgroup);
        for (int j = 0; j < 2; j++) {
            float e = 0.0f;
            if (s0 + lc + j < p.M) e = fast_exp(S[lr * 8 + lc + j] * p.scale - m);
            l += e;
            S[lr * 8 + lc + j] = e;
        }
        simdgroup_barrier(mem_flags::mem_threadgroup);
        simdgroup_float8x8 pm;
        simdgroup_load(pm, S, 8);
        device const float* vp = vb + (long)s0 * p.ld;
        for (int c = 0; c < 16; c++) {
            simdgroup_float8x8 vf;
            simdgroup_load(vf, vp + c * 8, p.ld);
            simdgroup_multiply_accumulate(o[c], pm, vf, o[c]);
        }
        simdgroup_barrier(mem_flags::mem_threadgroup);
    }
    l += simd_shuffle_xor(l, 1);
    l += simd_shuffle_xor(l, 2);
    threadgroup float* O = Os[sg];
    for (int c = 0; c < 16; c++) simdgroup_store(o[c], O + c * 8, 128);
    simdgroup_barrier(mem_flags::mem_threadgroup);
    if (rowOK) {
        float inv = 1.0f / l;
        device float* op = out + (long)myRow * p.ldo + h * 128 + (ln & 3) * 32;
        threadgroup float* src = O + lr * 128 + (ln & 3) * 32;
        for (int i = 0; i < 32; i++) op[i] = src[i] * inv;
    }
}
`
