# Local inference CPU budget: heads, routing, embeddings

What runs locally on every message, what it costs, and why each switch exists.
The Vulkan embedding backend is documented separately in
`docs/qwen3-vulkan-embedding.md`; this file covers the CPU-side budget and the
pieces that decide whether the GPU is used at all.

Reference hardware for every number below: Ryzen 7 8745HS (8C/16T, 16 MB L3),
Radeon 780M iGPU, Windows 11, Qwen3-Embedding-0.6B Q8_0 (1.5 GB file, 472 MB
GPU weights).

## Per-message stages

| stage | what it costs | notes |
|---|---|---|
| Layer 2 embedding (one forward) | ~100 ms wall on the GPU, CPU only normalises rows | runs on every message; a pooled head reuses it |
| Intent head (2560×2048) | 225 µs | int8 Q8P, auto-selected and verified |
| Tool head (49×1024) | 5.5 µs | one SIMD dot per tool |
| Interrupt relevance | 21 µs | MLP over the formatted vectors; the embedding itself dominates |
| Tool router usage scoring | 585 µs | 100 tools × 3 signals over 2000 records |
| Dynamic tool assembly | 84 µs (55 µs without usage history) | 100 tools, 400 records |

## Embedding threads

`corelib/embedding/threads.go` picks the worker count from physical cores and
the int8 ISA instead of using every logical CPU. Measured on the reference
machine (r13 intent text, per embed):

| threads | latency | process CPU |
|---|---|---|
| 1 | 51.4 ms | 74.2 ms |
| 2 | 37.1 ms | 87.5 ms |
| 4 | 29.7 ms | 99.2 ms |
| 8 | 28.6 ms | 114.5 ms |

Four threads is the knee: the last two halve latency at +15 % CPU, past that
the kernel is memory-bound. `MACLAW_EMBED_THREADS` overrides.

## GPU gate

At startup the backend is chosen by two gates, in this order:

1. correctness — every probe text must match the CPU vector within cosine
   0.999 and be finite;
2. speed — the GPU must be at least 0.5× as fast as the CPU on the same
   probe (measured 1.64× on the reference machine).

A machine that fails either gate keeps the CPU path for the session.
`MACLAW_QWEN3_VULKAN_MIN_RATIO`, `MACLAW_QWEN3_VULKAN=0` and the
hardware-acceleration toggle override it.

The pooled intent path is a deliberate trade: the Vulkan forward recomputes the
whole sequence, so it is slower in wall time than the CPU path (100 ms vs
67 ms), but it leaves the four cores free instead of occupying them for 67 ms
per message.
`embdoctor` prints both columns.

## One forward for Layer 2 and the head

The shipped intent head takes `input_blocks: ["last","mean"]`, and Layer 2
already embeds the same text with the same classification-role prompt. The
pooled forward returns both views, so Layer 2 uses it and keeps the pair for
the turn; `HeadQueryInput` assembles the head input from that pass instead of
embedding the message a second time. The last-token view is bit-identical to
the plain path (cos = 1.0, max difference ~1e-8 from summation order), verified
by `TestHeadQueryInputReusesLayer2Pass` (call counting) and by the shipped-head
integration tests.

The sharing works in both orders: when the composite-override probe builds the
head input first, Layer 2 takes its last-token view from that pass. A turn
therefore performs exactly one classification-role forward regardless of which
call runs first. The pair is keyed by (embedder, text), so a different message
or a different embedder re-embeds instead of reusing stale vectors.

Measured on the reference machine: Layer 2 classification 65 ms (one forward),
then the head input 0 ms; a cold pooled forward costs 67 ms.

## Heads: float32 → int8 without a quality regression

`corelib/intent/localhead.go` packs the hidden matrix into the int8 VNNI GEMM
kernel when the float32 copy no longer fits in the last-level cache. On the
reference machine (21.0 MB matrix, 16.8 MB L3):

| path | per prediction (benchmark) | allocs |
|---|---|---|
| float32 | 411 µs | 2 |
| int8 two-pass (auto) | 225 µs | 4 |

The decision is not left to the cache size alone. After loading, the head runs
a fixed 12-vector probe through both paths and compares probabilities:

- single-pass int8 is used only when the worst deviation is ≤ 1e-3;
- otherwise two-pass int8 is used when ≤ 1e-2 (measured 2.2e-3 on the shipped
  head);
- otherwise the float32 weights are kept and the backend is not used.

Explicit `MACLAW_HEAD_Q8P=0/1/2` skips the probe. On Apple Silicon there is no
`hw.l3cachesize`, so the P-core cluster L2 is used as the effective LLC.

The same probe design fixed a wrong initial threshold: `w1 > 2·L3` never fired
on the low-end machines the path exists for. Measured single-head,
cross-process block timing showed the crossover at ~1.0× L3, and the policy
now uses `w1 > L3`.

## Tool router usage scoring

The router asks for three signals per candidate tool: experience, contextual
outcome and routing-hint adjustment. Each used to scan the whole history and
allocate a Jaccard map per record.

| operation | before | after |
|---|---|---|
| 100 tools × 3 signals × 2000 records | 34.8 ms | 585 µs |
| record one outcome (2000-record cap) | 187 µs / 447 KB | 2.8 µs / 88 B |
| record one outcome incl. history write | 2.19 ms / 825 KB | debounced, off the caller |

Mechanisms:

- records carry at most 5 tokens, so Jaccard dedupes with a nested loop instead
  of a map;
- `byTool` / `byRecovery` index slices are maintained on write (in-place shift
  on trim) and fall back to a full scan when the record count does not match
  what the index was built from;
- history writes are coalesced to at most one per 400 ms, snapshot taken at
  flush time; `Save()` still writes synchronously and invalidations bypass the
  debounce.

## Reproducing

```bash
go test ./corelib/intent/ -bench 'LocalHeadPredict' -run XXX
go test ./corelib/tool/   -bench 'ToolHead|UsageTracker' -run XXX
go run ./corelib/embedding/cmd/embdoctor -model ~/.maclaw/models/Qwen3-Embedding-0.6B-Q8_0.gguf
```

The head benchmark loads the installed `~/.maclaw/models/intent_heads` head when
present and otherwise builds an equivalent synthetic one, so it runs on CI.

## Known limits

- Up to 400 ms of tool outcomes can be lost if the process dies before the
  coalesced writer fires (advisory routing data; `Save()` forces a write).
- The GPU has no prefix KV cache, so the pooled intent path pays ~35 % more wall
  time than the CPU in exchange for most of its CPU budget.
- Single sequences longer than `MACLAW_QWEN3_VULKAN_MAX_TOKENS` (192) run on the
  CPU: past that point the GPU loses to the CPU in wall time.
