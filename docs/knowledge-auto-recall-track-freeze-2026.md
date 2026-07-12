# Knowledge auto-recall track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should **not** extend auto-recall without a **named new goal** (e.g. multi-turn query expansion, TUI settings field, server-side embedder wiring).

Related: import progress is already frozen in [knowledge-import-track-freeze-2026.md](./knowledge-import-track-freeze-2026.md).

## Shipped (design → product)

Source design: [knowledge-auto-recall-design.md](./knowledge-auto-recall-design.md)

| Item | Summary |
|------|---------|
| Silent inject | FTS search on user message → score gate → system-prompt section |
| Shared policy | `corelib/agent` thresholds / inject ladder / header / NoMatchHint |
| Path parity | IM, VE, TUI, agentservice use the same inject policy |
| Toggle | `knowledge_auto_recall_enabled` (`*bool`, default **on**); GUI Knowledge Overview |
| Min score | `knowledge_auto_recall_min_score` (0 → default 0.3); `KnowledgeAutoRecallMaxInjectWithMin` |
| Patch API | `PatchConfigFields` whitelist for both fields |
| Embedding hybrid | `Search` fuses vector when FTS empty / low-confidence **and** store has embedder |
| Embedder wiring | Desktop `openKnowledgeStore` + auto-recall cache + `activateEmbedderAsync` |
| Multi-turn query (P3b) | `ExpandKnowledgeAutoRecallQuery` + prior user turns from history (IM/VE/TUI/agentservice) |

### Shared inject ladder (`prompt_blocks.go`)

| Top score | Max snippets |
|-----------|----------------|
| ≥ 3.0 | 5 |
| ≥ 1.0 | 3 |
| ≥ min score (default 0.3) | 2 |
| below min | 0 (+ optional NoMatchHint where KB is known non-empty) |

### Config keys

| JSON key | Meaning | Default |
|----------|---------|---------|
| `knowledge_auto_recall_enabled` | Auto-inject on/off | on (`nil`/true) |
| `knowledge_auto_recall_min_score` | Min FTS/hybrid score | 0 → 0.3 |

Turning **off** only disables auto-inject. Tools (`knowledge_search`, `knowledge_context_pack`, …) remain available.

## Operator regression

```powershell
go test ./corelib/ -count=1 -run "TestIsKnowledgeAutoRecallEnabledDefaultTrue|TestEffectiveKnowledgeAutoRecallMinScore"

go test ./corelib/agent/ -count=1 -run "TestKnowledgeAutoRecallMaxInjectWithMin"

go test ./corelib/knowledge/ -count=1 -run "TestSearchEmbeddingFallbackWhenFTSEmpty"

go test ./gui/ -count=1 -timeout 120s -run "TestPatchConfigFieldsKnowledgeAutoRecall|TestVEKnowledgeAutoRecallPolicyMatchesSharedAgentConstants|TestKnowledgeAutoRecallSnippetUsesBestContentText|TestKnowledgeAutoRecallHeaderAndNoMatchHintNonEmpty"

go test ./corelib/agent/ -count=1 -run "TestPriorUserMessagesFromHistory|TestExpandKnowledgeAutoRecallQuery"
```

Optional path checks (broader):

```powershell
go test ./tui/ -count=1 -run "TestProperty1_AutoRecallThresholdAndCount"
go test ./corelib/agentservice/ -count=1 -run "TestKnowledgeAutoRecall|TestCoreAgentBuildSystemPromptAutoRecall"
```

## Named increment after freeze

| Goal | Status |
|------|--------|
| Multi-turn query expansion (P3b) | **Done** — see design doc P3b section |

## Out of freeze (explicit goals only)

1. **TUI config UI** for the two keys (shared `config.json` already works via GUI).  
2. **agentservice / TUI embedder attach** when those runtimes load embedding outside the desktop App path.  
3. Feedback learning (raise/lower threshold from citation use) — design “future”.  

## Next named track (suggested)

Pick one **outside** this freeze, e.g.:

- Knowledge **export / Hub share** UX polish  
- Structured knowledge / quality maintenance (separate product line)

## Freeze rule

Do not keep stacking auto-recall micro-features on generic “继续”.  
Re-open only with an explicit goal name + acceptance criteria.
