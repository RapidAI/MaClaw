package embedding

// qwen3MetalSource is the Metal Shading Language source for the Qwen3 GPU
// forward (qwen3_metal_darwin.go), compiled at runtime by the Metal driver.
// All arithmetic is float32; Q8_0 weights are dequantised in-kernel
// (int8 × per-32-block f32 scale), so results follow the CPU float path.
// softmax and SiLU use the same Schraudolph exp approximation as the CPU
// kernels (tensor.fastExp / siluMulASM) so the two backends agree closely.
const qwen3MetalSource = `
#include <metal_stdlib>
using namespace metal;

struct MMParams { int M; int N; int K; int accum; int lda; int ldc; int splitBlocks; long splitStride; };
struct NormParams { int M; int D; float eps; int ld; };
struct RopeParams { int M; int nH; int nKV; int hd; float eps; int ld; int kOff; };
struct AttnParams { int M; int nH; int nKV; int hd; float scale; int ld; int kOff; int vOff; int ldo; };
struct SiluParams { int M; int F; int ld; };

static inline float fast_exp(float x) {
    // tensor.fastExp: int32(a*x + b) as float bits; b rounded to float32.
    if (x < -88.0f) return 0.0f;
    if (x > 88.0f) return FLT_MAX;
    const float a = 12102203.0f;
    const float b = 1065292415.0f;
    return as_type<float>(int(a * x + b));
}

// out[r] = x[r] * rsqrt(mean(x^2)+eps) * w, one threadgroup (256) per row.
kernel void rmsnorm_rows(device const float* x [[buffer(0)]],
                         device const float* w [[buffer(1)]],
                         device float* out [[buffer(2)]],
                         constant NormParams& p [[buffer(3)]],
                         uint r [[threadgroup_position_in_grid]],
                         uint t [[thread_index_in_threadgroup]],
                         uint sg [[simdgroup_index_in_threadgroup]],
                         uint ln [[thread_index_in_simdgroup]]) {
    threadgroup float part[8];
    device const float* xr = x + (long)r * p.ld;
    float ss = 0.0f;
    for (int i = t; i < p.D; i += 256) { float v = xr[i]; ss += v * v; }
    ss = simd_sum(ss);
    if (ln == 0) part[sg] = ss;
    threadgroup_barrier(mem_flags::mem_threadgroup);
    float tot = 0.0f;
    for (int i = 0; i < 8; i++) tot += part[i];
    float sc = 1.0f / sqrt(tot / float(p.D) + p.eps);
    device float* o = out + (long)r * p.ld;
    for (int i = t; i < p.D; i += 256) o[i] = xr[i] * sc * w[i];
}

// C[M×N] (=|+=) A[M×K] · Wᵀ, W = int8 qs [N×K] with f32 scales [N×K/32].
// Threadgroup tile TMv×64, 4 simdgroups (2×2) of TMv/2×32, K step 32;
// (TMv=64 was tried: register pressure made it ~5x slower on M4.)
#define TN 64
#define TK 32
template<int TMv>
kernel void matmul_q8_t(device const float* A [[buffer(0)]],
                        device const char* Wq [[buffer(1)]],
                        device const float* Ws [[buffer(2)]],
                        device float* C [[buffer(3)]],
                        constant MMParams& p [[buffer(4)]],
                        uint3 tg [[threadgroup_position_in_grid]],
                        uint t [[thread_index_in_threadgroup]],
                        uint sg [[simdgroup_index_in_threadgroup]]) {
    constexpr int RA = TMv / 16; // 8-row fragments per simdgroup
    threadgroup float smem[TMv * TK + TK * TN];
    threadgroup float* As = smem;
    threadgroup float* Bs = smem + TMv * TK;
    const int n0 = tg.x * TN;
    const int m0 = tg.y * TMv;
    const int nb = p.K / 32;
    const int sm = (sg >> 1) * (TMv / 2);
    const int sn = (sg & 1) * 32;
    simdgroup_float8x8 c[RA][4];
    for (int i = 0; i < RA; i++) for (int j = 0; j < 4; j++) c[i][j] = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
    // A loader: thread t loads rows t/4 + 32*i, 8 floats at col (t%4)*8.
    const int ar = t >> 2, ac = (t & 3) * 8;
    // B loader: thread t dequantises column t/2, 16 k values at (t%2)*16.
    const int bn = t >> 1, bk = (t & 1) * 16;
    device const char* Wp = Wq + (long)(n0 + bn) * p.K + bk;
    device const float* Sp = Ws + (long)(n0 + bn) * nb;
    // Split-K: threadgroup z covers blocks [z*splitBlocks, (z+1)*splitBlocks)
    // and writes its partial sum to C + z*splitStride (reduced by mm_reduce).
    const int kbBeg = tg.z * p.splitBlocks;
    const int kbEnd = min(nb, kbBeg + p.splitBlocks);
    C += tg.z * p.splitStride;
    for (int kb = kbBeg; kb < kbEnd; kb++) {
        const int k0 = kb * TK;
        float4 a[TMv / 32][2];
        for (int i = 0; i < TMv / 32; i++) {
            a[i][0] = 0.0f; a[i][1] = 0.0f;
            if (m0 + ar + 32 * i < p.M) {
                device const float* ap = A + (long)(m0 + ar + 32 * i) * p.lda + ac + k0;
                a[i][0] = *(device const float4*)ap; a[i][1] = *(device const float4*)(ap + 4);
            }
        }
        float s = Sp[kb];
        char4 q0 = *(device const char4*)(Wp + k0);
        char4 q1 = *(device const char4*)(Wp + k0 + 4);
        char4 q2 = *(device const char4*)(Wp + k0 + 8);
        char4 q3 = *(device const char4*)(Wp + k0 + 12);
        threadgroup_barrier(mem_flags::mem_threadgroup);
        for (int i = 0; i < TMv / 32; i++) {
            *(threadgroup float4*)(As + (ar + 32 * i) * TK + ac) = a[i][0];
            *(threadgroup float4*)(As + (ar + 32 * i) * TK + ac + 4) = a[i][1];
        }
        float4 f0 = float4(q0) * s, f1 = float4(q1) * s, f2 = float4(q2) * s, f3 = float4(q3) * s;
        threadgroup float* bp = Bs + bk * TN + bn;
        bp[0*TN] = f0.x; bp[1*TN] = f0.y; bp[2*TN] = f0.z; bp[3*TN] = f0.w;
        bp[4*TN] = f1.x; bp[5*TN] = f1.y; bp[6*TN] = f1.z; bp[7*TN] = f1.w;
        bp[8*TN] = f2.x; bp[9*TN] = f2.y; bp[10*TN] = f2.z; bp[11*TN] = f2.w;
        bp[12*TN] = f3.x; bp[13*TN] = f3.y; bp[14*TN] = f3.z; bp[15*TN] = f3.w;
        threadgroup_barrier(mem_flags::mem_threadgroup);
        for (int kk = 0; kk < TK; kk += 8) {
            simdgroup_float8x8 af[RA], bf[4];
            for (int i = 0; i < RA; i++) simdgroup_load(af[i], As + (sm + i * 8) * TK + kk, TK);
            for (int j = 0; j < 4; j++) simdgroup_load(bf[j], Bs + kk * TN + sn + j * 8, TN);
            for (int i = 0; i < RA; i++) for (int j = 0; j < 4; j++) simdgroup_multiply_accumulate(c[i][j], af[i], bf[j], c[i][j]);
        }
    }
    threadgroup_barrier(mem_flags::mem_threadgroup);
    threadgroup float* Cs = smem; // TMv×64 staging
    for (int i = 0; i < RA; i++) for (int j = 0; j < 4; j++)
        simdgroup_store(c[i][j], Cs + (sm + i * 8) * TN + sn + j * 8, TN);
    threadgroup_barrier(mem_flags::mem_threadgroup);
    for (int e = t; e < TMv * TN; e += 128) {
        int r = e / TN, cc = e % TN;
        if (m0 + r < p.M) {
            device float* cp = C + (long)(m0 + r) * p.ldc + n0 + cc;
            float v = Cs[e];
            if (p.accum) v += *cp;
            *cp = v;
        }
    }
}
template [[host_name("matmul_q8")]] kernel void matmul_q8_t<32>(device const float*, device const char*, device const float*, device float*, constant MMParams&, uint3, uint, uint);

struct ReduceParams { int M; int N; int splits; int accum; long stride; };
// C[r][c] (=|+=) sum_z P[z][r][c] (deterministic split-K reduction).
kernel void mm_reduce(device const float* P [[buffer(0)]],
                      device float* C [[buffer(1)]],
                      constant ReduceParams& p [[buffer(2)]],
                      uint2 gid [[thread_position_in_grid]]) {
    const int c = gid.x * 4, r = gid.y;
    if (r >= p.M || c >= p.N) return;
    long o = (long)r * p.N + c;
    float4 v = *(device const float4*)(P + o);
    for (int z = 1; z < p.splits; z++) v += *(device const float4*)(P + z * p.stride + o);
    if (p.accum) v += *(device float4*)(C + o);
    *(device float4*)(C + o) = v;
}

// Per-head RMSNorm + NeoX RoPE on q (heads 0..nH-1) and k (nH..nH+nKV-1),
// in place in the fused qkv rows. One 64-thread group per (row, head).
kernel void qk_norm_rope(device float* qkv [[buffer(0)]],
                         device const float* qw [[buffer(1)]],
                         device const float* kw [[buffer(2)]],
                         device const float* cosT [[buffer(3)]],
                         device const float* sinT [[buffer(4)]],
                         device const int* pos [[buffer(5)]],
                         constant RopeParams& p [[buffer(6)]],
                         uint2 tg [[threadgroup_position_in_grid]],
                         uint t [[thread_index_in_threadgroup]],
                         uint sg [[simdgroup_index_in_threadgroup]],
                         uint ln [[thread_index_in_simdgroup]]) {
    threadgroup float part[2];
    const int r = tg.x, h = tg.y;
    const int half_ = p.hd / 2;
    device float* v;
    device const float* w;
    if (h < p.nH) { v = qkv + (long)r * p.ld + h * p.hd; w = qw; }
    else { v = qkv + (long)r * p.ld + p.kOff + (h - p.nH) * p.hd; w = kw; }
    float x0 = v[t], x1 = v[t + half_];
    float ss = simd_sum(x0 * x0 + x1 * x1);
    if (ln == 0) part[sg] = ss;
    threadgroup_barrier(mem_flags::mem_threadgroup);
    float sc = 1.0f / sqrt((part[0] + part[1]) / float(p.hd) + p.eps);
    x0 = x0 * sc * w[t];
    x1 = x1 * sc * w[t + half_];
    const int ps = pos[r];
    float c = cosT[ps * half_ + t], s = sinT[ps * half_ + t];
    v[t] = x0 * c - x1 * s;
    v[t + half_] = x0 * s + x1 * c;
}

// Causal GQA attention over packed sequences, head_dim 128. One simdgroup
// per (row, head); lane holds 4 dims. Rows attend keys [seg[r], r].
// Two passes (max, then exp-sum and weighted V) to match the CPU softmax.
kernel void attention(device const float* qkv [[buffer(0)]],
                      device float* out [[buffer(1)]],
                      device const int* seg [[buffer(2)]],
                      constant AttnParams& p [[buffer(3)]],
                      uint2 tg [[threadgroup_position_in_grid]],
                      uint sg [[simdgroup_index_in_threadgroup]],
                      uint ln [[thread_index_in_simdgroup]]) {
    const int r = tg.x;
    const int h = tg.y * 4 + sg;
    if (h >= p.nH) return;
    const int kvh = h / (p.nH / p.nKV);
    const int s0 = seg[r];
    float4 q = *(device const float4*)(qkv + (long)r * p.ld + h * p.hd + ln * 4);
    device const float* kb = qkv + p.kOff + kvh * p.hd + ln * 4;
    device const float* vb = qkv + p.vOff + kvh * p.hd + ln * 4;
    float mx = -FLT_MAX;
    for (int s = s0; s <= r; s++) {
        float d = simd_sum(dot(q, *(device const float4*)(kb + (long)s * p.ld))) * p.scale;
        mx = max(mx, d);
    }
    float sum = 0.0f;
    float4 acc = 0.0f;
    for (int s = s0; s <= r; s++) {
        float d = simd_sum(dot(q, *(device const float4*)(kb + (long)s * p.ld))) * p.scale;
        float e = fast_exp(d - mx);
        sum += e;
        acc += e * *(device const float4*)(vb + (long)s * p.ld);
    }
    *(device float4*)(out + (long)r * p.ldo + h * p.hd + ln * 4) = acc * (1.0f / sum);
}

// Tiled causal GQA attention (head_dim 128) with simdgroup matrices. One
// simdgroup per (8-row query block, head); K/V streamed in 8-key blocks.
// Pass 1 finds each row's max score, pass 2 accumulates fastExp(s-max)·V,
// mirroring the CPU softmax. Rows attend keys [seg[r], r].
kernel void attention_tiled(device const float* qkv [[buffer(0)]],
                            device float* out [[buffer(1)]],
                            device const int* seg [[buffer(2)]],
                            constant AttnParams& p [[buffer(3)]],
                            uint2 tg [[threadgroup_position_in_grid]],
                            uint sg [[simdgroup_index_in_threadgroup]],
                            uint ln [[thread_index_in_simdgroup]]) {
    threadgroup float Ss[4][64];
    threadgroup float Os[4][8 * 128];
    const int h = tg.y * 4 + sg;
    const int r0 = tg.x * 8;
    const int kvh = h / (p.nH / p.nKV);
    const int lr = ln >> 2, lc = (ln & 3) * 2;
    const int myRow = r0 + lr;
    const int rowLast = min(r0 + 7, p.M - 1);
    const bool rowOK = myRow < p.M;
    const int mySeg = rowOK ? seg[myRow] : 0;
    const int kmin = seg[r0];
    const int nkb = (rowLast - kmin + 8) / 8;
    device const float* qb = qkv + (long)r0 * p.ld + h * 128;
    device const float* kb = qkv + p.kOff + kvh * 128;
    device const float* vb = qkv + p.vOff + kvh * 128;
    threadgroup float* S = Ss[sg];
    simdgroup_float8x8 qf[16];
    for (int c = 0; c < 16; c++) simdgroup_load(qf[c], qb + c * 8, p.ld);
    float m = -FLT_MAX;
    for (int b = 0; b < nkb; b++) {
        const int s0 = kmin + b * 8;
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
            int s = s0 + lc + j;
            if (rowOK && s >= mySeg && s <= myRow) m = max(m, S[lr * 8 + lc + j] * p.scale);
        }
        simdgroup_barrier(mem_flags::mem_threadgroup);
    }
    m = max(m, simd_shuffle_xor(m, 1));
    m = max(m, simd_shuffle_xor(m, 2));
    simdgroup_float8x8 o[16];
    for (int c = 0; c < 16; c++) o[c] = make_filled_simdgroup_matrix<float, 8, 8>(0.0f);
    float l = 0.0f;
    for (int b = 0; b < nkb; b++) {
        const int s0 = kmin + b * 8;
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
            int s = s0 + lc + j;
            float e = 0.0f;
            if (rowOK && s >= mySeg && s <= myRow) e = fast_exp(S[lr * 8 + lc + j] * p.scale - m);
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

// g[r][j] = silu(gu[r][j]) * gu[r][F+j], SiLU as in siluMulASM.
kernel void silu_mul(device const float* gu [[buffer(0)]],
                     device float* g [[buffer(1)]],
                     constant SiluParams& p [[buffer(2)]],
                     uint2 gid [[thread_position_in_grid]]) {
    const int j = gid.x, r = gid.y;
    if (j >= p.F || r >= p.M) return;
    float v = gu[(long)r * p.ld + j];
    float u = gu[(long)r * p.ld + p.F + j];
    float nv = clamp(-v, -88.0f, 88.0f);
    float e = as_type<float>(int(12102203.0f * nv) + 0x3F7F127F);
    g[(long)r * p.F + j] = v / (1.0f + e) * u;
}
`
