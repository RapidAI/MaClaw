# xMemory-inspired Memory Improvements

This note tracks the MaClaw memory changes inspired by "Beyond RAG for Agent
Memory: Retrieval by Decoupling and Aggregation" (arXiv:2602.02007) and the
reference xMemory implementation.

## Implemented

- Theme hierarchy: `corelib/memory/theme.go` builds embedding-aware memory
  themes with tag fallback, split/merge behavior, and nearest-neighbor links.
- Store synchronization: the theme layer is maintained with the other derived
  indexes on save, update, delete, supersede, and full rebuild paths, so adaptive
  recall does not depend on a later maintenance cycle.
- Theme-aware diversity: `corelib/memory/diversity.go` reranks complex recall
  candidates so top results cover distinct themes instead of repeating near
  duplicates.
- Adaptive top-down recall: `corelib/memory/adaptive_recall.go` seeds from the
  existing dynamic recall path, selects related themes, expands adjacent
  evidence, and returns an optional debug plan.
- Query decoupling: complex recall queries are decomposed into deterministic
  facets such as entity, comparison, causal, temporal, and keyword facets.
  Adaptive recall can use those facets to select additional relevant themes
  even when the flat seed set misses a useful cluster.
- Evidence-aware facet theme selection: facet matching now considers not only a
  theme's summary and tags, but also the title, content, tags, and structured
  entities of member memories. This lets adaptive recall select opaque or
  weakly labeled themes when their raw evidence answers a query facet.
- Facet token matching uses word-boundary checks for ASCII alphanumeric tokens,
  preventing short query terms such as `go` from accidentally matching inside
  unrelated words such as `postgresql`, while preserving substring matching for
  non-ASCII text and phrases.
- Theme match evidence: selected themes now include compact match evidence for
  debug plans, showing the facet kind, matched token, representative entry id,
  source, and preview that justified the selection. When both theme metadata and
  member memory content match the same facet, the plan keeps both signals within
  a small cap instead of hiding the raw evidence behind the summary.
- Adaptive recall caches per-theme facet matches and match evidence during a
  single retrieval pass, avoiding repeated scans of the same theme member text
  while preserving the public debug output. Cache keys fall back to stable theme
  content when a caller supplies anonymous theme nodes without IDs.
- Adaptive diagnostics skip anonymous selected-theme IDs in facet coverage and
  deduplicate repeated seed/result/expanded entry ids inside theme aggregates,
  keeping token estimates and source counts stable even when theme membership
  contains duplicates.
- Recall evaluation deduplicates result evidence by entry id before computing
  source and seed-vs-expansion counts, so duplicate evidence rows cannot inflate
  strategy diagnostics.
- Adaptive debug/eval plans deduplicate result ids, result evidence, and theme
  aggregates by non-empty entry id while leaving the raw recall result slice
  unchanged. Anonymous entries remain distinct so temporary/debug evidence is
  not accidentally dropped.
- Match-evidence eval metrics: recall evaluation reports how much theme-match
  evidence was produced and whether it came from theme metadata or member
  memory content, and strategy summaries include average match evidence plus
  source totals so improvements to decoupled retrieval are visible in batch
  runs.
- Facet coverage tracing: adaptive debug plans report which selected themes
  matched each query facet and which expanded entries came through those
  themes, making missed sub-questions visible during eval and tool debugging.
- Theme aggregation: adaptive debug plans group seed, expanded, and final
  result entries by selected theme with short previews, source counts, matched
  facets, and token estimates. This mirrors xMemory's aggregate-first retrieval
  view without changing the raw entries returned to callers.
- Facet-aware theme expansion: when adaptive recall expands inside a selected
  theme, candidates are ranked by query facet token matches, theme-tag overlap,
  pinned status, access count, and recency before the top evidence is selected.
  Expanded evidence includes the ranking score for debugging, and that score
  contributes a bounded bonus during final adaptive reranking so high-signal
  theme evidence is less likely to be crowded out by seed results.
- Complexity-aware adaptive budget: complex and multi-facet queries receive a
  larger capped result and token budget than simple lookups. The selected budget
  is included in adaptive debug plans and eval output.
- Soft diversity caps: adaptive final selection applies soft per-theme and
  per-source caps before backfilling deferred high-score candidates. This keeps
  larger complex-query budgets from being monopolized by one theme or source
  while preserving result count when candidates are scarce.
- Diversity diagnostics: adaptive debug plans and eval output report the active
  theme/source caps, how many candidates were deferred by each cap, how many
  deferred candidates were backfilled, and final theme/source coverage.
- Selected-theme result reservation: after final scoring, adaptive recall
  reserves evidence coverage for selected themes when candidate evidence exists,
  replacing redundant same-theme tail results when needed.
- Selected-theme coverage diagnostics: adaptive diversity stats report how many
  selected themes were targeted, how many were covered before/after reservation,
  and how many final entries were reserved to restore selected-theme coverage.
- Recall provenance: adaptive debug plans now include final-result evidence and
  expanded-candidate evidence with reason, rank, theme id, source type, and
  source URL where available.
- Coverage reservation: complex adaptive recall reserves room for theme-expanded
  evidence when seed results would otherwise fill the whole budget, replacing a
  low-ranked seed with a representative expansion when token limits allow.
- Auto recall policy: `memory.ShouldUseAdaptiveRecall` routes complex
  multi-fact queries to adaptive recall while leaving simple fact lookups on the
  flat recall path.
- Evaluation harness: `corelib/memory/eval.go` compares hybrid and adaptive
  recall on JSON cases, including hit rate, token estimate, result count, and
  repeated-theme count. Adaptive scores also report fallback status, selected
  theme count, query facet count, adaptive budget, per-facet theme coverage,
  aggregated theme count, theme selection reason counts, expansion count,
  source counts, and how many final results came from seeds versus theme
  expansion. Strategy summaries include selected-theme coverage rate and
  average selected-theme reservations where applicable. Eval reports also include theme health,
  diagnostics, and a maintenance plan so recall quality can be interpreted
  alongside theme-layer coverage.
- Maintenance eval loop: `memory eval --maintenance` records recall metrics
  before and after safe theme maintenance, including deltas for theme coverage,
  diagnostic/action counts, hit rate, and repeated-theme counts.
- Tooling: `memory recall` and `memory eval` are exposed in the TUI; the agent
  and GUI memory tool accept `mode=dynamic|hybrid|adaptive|auto` and
  `debug=true`.
- Theme inspection: `memory themes` and `memory(action="themes")` expose the
  current theme layer for operations/debugging, including member count, tags,
  summary, and neighbors.
- Theme health diagnostics: `memory stats`, `memory themes --stats`, and
  `memory(action="themes", stats=true)` report theme coverage, uncovered active
  memories, average/max theme size, neighbor links, isolated themes, and
  duplicate entry references.
- Theme provenance explanations: `memory themes --evidence N` and
  `memory(action="themes", evidence=true)` return representative raw memories
  for each theme, including source type, source URL, similarity, access count,
  and content preview.
- Theme diagnostics: `memory themes --diagnose` and
  `memory(action="themes", diagnose=true)` report actionable issues such as low
  coverage, uncovered active memories, low-cohesion themes, isolated themes, and
  themes that reached capacity.
- Theme maintenance plans: `memory themes --plan` and
  `memory(action="themes", plan=true)` aggregate diagnostics into
  non-destructive actions such as `backfill_theme_inputs`,
  `review_split_theme`, `review_isolated_theme`, and
  `deduplicate_theme_membership`.
- Safe theme maintenance execution: `memory themes --apply` and
  `memory(action="themes", apply=true)` synchronously apply conservative
  actions only: backfill missing embeddings when a real embedder is active and
  rebuild the theme layer. Manual review actions are reported as skipped.

## Example commands

```powershell
maclaw-tui memory recall --mode auto --debug --query "compare react and vue migration decisions over time"
maclaw-tui memory recall --mode adaptive --json --debug --query "why did the project change database backup strategy"
maclaw-tui memory themes --limit 20 --json
maclaw-tui memory themes --limit 20 --stats
maclaw-tui memory themes --limit 20 --evidence 3
maclaw-tui memory themes --diagnose --evidence 2
maclaw-tui memory themes --plan --json
maclaw-tui memory themes --apply --json
maclaw-tui memory eval --cases .\testdata\memory_recall_cases.json --json
maclaw-tui memory eval --cases .\testdata\memory_recall_cases.json --maintenance --json
```

Eval cases can be either an array:

```json
[
  {
    "name": "migration",
    "query": "why compare react and vue migration decisions",
    "expected_contains": ["react migration", "vue migration"]
  }
]
```

or a wrapper object:

```json
{
  "cases": [
    {
      "name": "migration",
      "query": "why compare react and vue migration decisions",
      "expected_contains": ["react migration", "vue migration"]
    }
  ]
}
```

## Remaining Work

- Persist theme metadata if rebuild cost becomes visible on large stores.
- Add source/provenance expansion so adaptive results can explain which raw
  episode produced a semantic theme.
- Add product-level recall policy knobs for IM, coding sessions, and project
  recovery budgets.
