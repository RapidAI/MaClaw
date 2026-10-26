# Qwen3 embedding — pure-Go Vulkan backend

Status: implemented, Windows/Linux. macOS keeps the Metal backend.

## Why

The Qwen3-Embedding-0.6B forward pass is memory-bound: one embed streams the
whole 440 MB int8 weight blob. On the CPU that costs ~105 ms of CPU time per
embed spread over 3–4 cores, which on a laptop means fan noise and battery
drain for a latency the user barely notices. Moving the same work to the
integrated GPU drops the CPU cost by ~70–80% at comparable latency.

Measured on Ryzen 7 8745HS + Radeon 780M (iGPU), Qwen3-Embedding-0.6B Q8_0,
6–8 token CJK inputs, median of 3 interleaved rounds:

| backend | CPU time per embed | p50 latency | cores busy |
|---|---|---|---|
| CPU SIMD (Q8P, 4 threads) | 107.6 ms | 29.8 ms | 3.6 |
| Vulkan (780M) | 34.6 ms | 43.6 ms | 0.8 |

For a 27-token input the GPU is ~2x faster than the CPU. For very short inputs
the fixed per-submit cost makes the GPU slightly slower. The startup speed gate
below decides per machine.

## Constraints honoured

- **No cgo.** Every entry point is resolved by `syscall.LoadDLL` (Windows) or
  `purego.Dlopen` (Linux), so `CGO_ENABLED=0` builds keep working.
- **No sidecar process.** The forward runs in-process.
- **Operator fusion.** Six kernels cover the 28-layer forward (see below).

## Layout

| path | role |
|---|---|
| `corelib/vk/vk.go` | ABI structs and constants |
| `corelib/vk/device.go` | instance / physical device / device / queue / memory-type selection |
| `corelib/vk/buffer.go` | host-visible storage buffers, pipelines, descriptor-set caching |
| `corelib/vk/encoder.go` | one-command-buffer recording, barriers, single submit |
| `corelib/vk/shaders/*.comp` | GLSL compute kernels (the actual numerics) |
| `corelib/vk/shaders/gen_shaders.py` | compiles the kernels to SPIR-V and writes `shaders_gen.go` |
| `corelib/vk/shaders_gen.go` | generated: embedded SPIR-V bytes (checked in) |
| `corelib/embedding/qwen3_vulkan.go` | weight repacking, forward orchestration, self-check, speed gate |

## Kernels and fusion

| kernel | replaces | notes |
|---|---|---|
| `matmul` | QKV / Wo / gate+up / down projection | tiled int8 dequant GEMM, optional residual accumulate |
| `rmsnorm_rows` | pre-attention and pre-FFN norm | one workgroup per row, cooperative reduction |
| `qk_norm_rope` | per-head Q/K RMSNorm + NeoX RoPE | in place on the packed QKV buffer |
| `attention` | causal GQA attention | one workgroup per (row, head), online softmax, one read of K/V |
| `silu_mul` | SiLU(gate)·up | collapses the concatenated gate/up output |

Q, K, V and gate/up are concatenated into single weight matrices at load, so
each becomes one dispatch instead of two or three. The last-token hidden state
is the only output (`forward` returns it raw; the caller applies the output
norm), matching the Metal backend's contract.

## Numerics

The self-check compares the Vulkan embedding against the CPU path at model load
and refuses the GPU below cosine 0.999. Observed on the reference hardware:

| text | tokens | cos(vulkan, cpu) |
|---|---|---|
| 帮我把这个文件发给张三 | 8 | 0.999962 |
| 查询 api2 服务器状态 | 7 | 0.999991 |
| hello | 2 | 0.999999 |
| what is the weather like today | 7 | 0.999960 |

Batch isolation is exact: the same text embedded alone and packed with others
gives cosine 1.000000, so the causal mask and segment handling are right.

Activation maths is float32 with int8 weights dequantised in-kernel (per-32
scale), like the Metal backend. SiLU and softmax use the Schraudolph exp
approximation to stay close to the CPU kernels' `fastExp`.

## Pooling

`EmbedPooledWithRole` (last-token + mean, used by the Layer 2 intent heads on
every message) also runs on the GPU. The extra dispatch normalises every row
with the output-norm weight and the mean is summed on the CPU, which is the same
value the CPU path builds from a cached prefix sum plus the suffix.

Measured on the reference hardware, per pooled embed (process CPU time):

| backend | CPU per embed |
|---|---|
| CPU (Q8P, 4 threads) | ~109 ms |
| Vulkan | ~7 ms |

That makes the intent path — the most frequent embedding call in the product —
cheap enough to run on every message.

## Startup gates: correctness first, then speed

A fast backend that computes the wrong vector is worthless, so the correctness
gate runs first and is absolute: if the candidate backend fails it, the device
is released and the CPU takes over regardless of speed.

One probe serves both gates, so the decision costs two forwards per engine
rather than four:

1. **numeric** — every probe text is compared, not just the first: the GPU packs
   them into one forward, so a broken causal mask or segment base would corrupt
   some sequences while leaving others correct. The *worst* cosine must reach
   0.999, and every output must be finite (NaN/Inf is rejected outright with a
   distinct reason). The failure message names the offending probe text;
2. **speed** — best-of-2 timing of the same two texts on both engines. The CPU
   side calls `EmbedBatch` (the production CPU path: parallel across texts,
   single-threaded kernels); the GPU side packs the same texts into one
   forward. Comparing a packed GPU forward against sequential single-text CPU
   forwards would flatter the GPU by a wide margin;
3. keep the GPU when `cpuTime / gpuTime >= 0.5` (Vulkan may be at most 2x slower
   than the CPU). Otherwise release the device and log why.

Metal uses the same shape of check (`metalSelfCheck`: both probe texts, finite
outputs, worst cosine >= 0.9999) before it is adopted.

`TestVulkanRejectsWrongBackend` exercises the gate with a deliberately corrupt
backend (zeroed, NaN, sign-flipped) and asserts the GPU is refused and the CPU
path still serves requests, so the gate cannot silently rot.

Observed on the reference hardware: `1.57x CPU speed: cpu 106.9ms vs vulkan
68.0ms`, stable across runs. The gate plus the ~450 MB weight upload add about
0.4–0.6 s to the one-time model load (663 ms CPU-only vs ~1.2 s with Vulkan).

The persisted hardware-acceleration preference is honoured before any of this
runs: when it is off, the device is never opened, so no upload or probe cost is
paid. Turning the setting back on re-runs the whole probe.

A low-memory guard runs before the upload: the weight blob (495.7 MB for this
model) lives in host-visible device-local memory, which on an integrated GPU is
ordinary system RAM, so the backend is skipped when available memory cannot
hold it plus a 256 MB margin. `MACLAW_EMBED_MEM_GUARD=0` disables both this and
the CPU-side guard.

## Environment variables

| var | effect |
|---|---|
| `MACLAW_QWEN3_VULKAN=0` | disable the Vulkan backend entirely |
| `MACLAW_QWEN3_VULKAN_MIN_RATIO` | speed gate threshold (default `0.5`; `0` accepts any GPU that passed the numeric check) |
| `MACLAW_QWEN3_VULKAN_MAX_ROWS` | activation arena / batch row cap (default `512`, min `32`) |
| `MACLAW_QWEN3_VULKAN_MAX_TOKENS` | longest single sequence kept on the GPU (default `128`) |

The hardware-acceleration toggle (`embedding.SetHWAccelPreferred`) governs both
GPU backends: off releases the device (waiting for in-flight forwards, and
without ever opening it at load), on re-runs the probe. macOS/Metal behaves the
same way but has no speed gate — Apple GPUs are not the weak-GPU case this was
built for.

CPU-side controls (unchanged): `MACLAW_EMBED_THREADS` caps the CPU worker count,
`MACLAW_Q8P_ISA` / `MACLAW_Q8P` select the int8 kernel.

## On-machine diagnostics

`embdoctor` prints everything needed to judge the backend on a specific
machine — cores and thread budget, ISA, the gate verdict with its measured
numbers, CPU-vs-GPU latency at several input lengths, and the memory footprint:

```bash
go run ./corelib/embedding/cmd/embdoctor -model ~/.maclaw/models/Qwen3-Embedding-0.6B-Q8_0.gguf
go run ./corelib/embedding/cmd/embdoctor -model … -json      # machine-readable
```

It measures the GPU first and then releases it with the hardware-acceleration
toggle, so both columns come from one process and one model load.

## Regenerating the shaders

SPIR-V is embedded so the shipped binary needs no shader compiler. After
editing a `.comp` file:

```bash
cd corelib/vk/shaders
python gen_shaders.py              # finds glslang on PATH
python gen_shaders.py /path/to/glslang   # or pass it explicitly
```

`glslang` (not `glslangValidator`) is required: the kernels use
`GL_EXT_shader_explicit_arithmetic_types_int8`, and Vulkan 1.1 SPIR-V is
emitted with `-V --target-env vulkan1.1`.

## Performance notes (things that actually mattered)

Three fixes account for almost the entire speedup (145 ms → 27 ms per forward):

1. **Shared-memory bank conflicts.** The weight tile was `shared float
   Wt[32][32]`: a 128-byte row stride, so every lane reading column `k` hit the
   same bank (32-way conflict). Padding to `[32][33]` fixed it.
2. **Weight offsets are absolute.** `wOff`/`sOff` are element offsets into the
   packed blob, not row indices. Multiplying them by `K` again read past the
   buffer and produced zeros.
3. **Descriptor sets are per buffer combination.** Vulkan reads descriptor
   contents at submit time, not at record time. Rebinding one set between
   recorded dispatches made every dispatch see the final binding (the whole
   forward produced zeros). `Pipeline.Bind` now caches one descriptor set per
   distinct buffer tuple.

Device selection requires the host-visible heap to hold the weight blob:
`NewDeviceWithMinHeap` skips any physical device whose heap is too small, so a
hybrid laptop falls back to the integrated GPU instead of failing on a discrete
GPU's small BAR heap.

The activation arena is allocated once at load (`MACLAW_QWEN3_VULKAN_MAX_ROWS`,
512 rows by default, ~35 MB) and never reallocated: releasing and recreating
mapped buffers while the driver still referenced them crashed the AMD driver
inside `vkCreateBuffer`, even with `vkDeviceWaitIdle` and correct
destroy-before-free ordering in place. A forward that would need more rows is
rejected and the CPU path serves it, which is why batch packing is capped at
the same row count.

Single sequences above `MACLAW_QWEN3_VULKAN_MAX_TOKENS` (default 128) also run
on the CPU. The GPU loses on long text because the 32-row matmul tile makes
every extra 32 rows re-read the whole weight blob, and attention grows
quadratically. Measured p50 on the reference hardware:

| tokens | CPU | Vulkan |
|---|---|---|
| 33 | 83 ms | 84 ms |
| 45 | 104 ms | 97 ms |
| 131 | 360 ms | 345 ms |
| 380 | 1045 ms | 1754 ms |

In a bulk batch, only the long texts are routed to the CPU; the rest still use
the GPU.

Also required: `vkGetDeviceProcAddr` (not `vkGetInstanceProcAddr`) for device
commands — the AMD loader returns NULL for `vkCreateComputePipeline` through the
instance path — and `vkCreateComputePipelines` (plural) as the primary entry
point.

Tried and rejected, with measurements on the reference hardware:

- a 64-wide N tile — register pressure made it ~30% slower;
- packing two M-tiles per workgroup so one dequantised weight tile serves 64
  rows — 512 rows took 525 ms vs 428 ms for one tile; the extra accumulators
  and the second barrier per K block cost more than the halved weight traffic;
- electing a non-device-local host-visible memory type — no measurable gain on
  UMA.

Per-row forward cost is ~0.85 ms from 32 rows upward, so the kernel is not
purely weight-bandwidth-bound: issue rate and the per-K-block barriers
dominate once the grid is large enough to fill the GPU.

## Known limits


- Head dim is fixed at 128 and the model must be Qwen3-Embedding; anything else
  falls back to the CPU.
- Windows and Linux only; macOS uses Metal (which has no pooled path yet).
