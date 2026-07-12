# Knowledge export / Hub share track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should **not** extend export/share UX without a **named new goal** (e.g. dual export formats, bulk share management).

Related freezes:

- [knowledge-import-track-freeze-2026.md](./knowledge-import-track-freeze-2026.md)
- [knowledge-auto-recall-track-freeze-2026.md](./knowledge-auto-recall-track-freeze-2026.md)

Phase notes: [knowledge-export-share-phase.md](./knowledge-export-share-phase.md)

## Shipped (design → product)

| Item | Summary |
|------|---------|
| Export tab selection | Pick sources once → export file or share to Hub |
| Export success card | Path, counts, size; copy path; show in folder; share to Hub |
| Hub share dialog | Description required; visibility + TTL; scope hints |
| Share success actions | Copy ID / link; open share page; view shares |
| **My shares panel** | List via `GET /api/knowledge/shares/mine` |
| Share delete | Confirm + `DELETE /api/knowledge/shares/{id}` (local KB kept) |
| Share edit | Title / description / visibility via `PATCH /api/knowledge/shares/{id}` |
| Dual local formats (named increment) | `jsonl` full snapshot **or** `package` exchange JSON (`maclaw.knowledge.package`) |

### Desktop APIs

| Method | Hub |
|--------|-----|
| `KnowledgeShareToHub` | `POST /api/knowledge/shares` |
| `KnowledgeListMyHubShares` | `GET /api/knowledge/shares/mine` |
| `KnowledgeUpdateHubShare` | `PATCH /api/knowledge/shares/{id}` |
| `KnowledgeDeleteHubShare` | `DELETE /api/knowledge/shares/{id}` |

## Operator regression

```powershell
go test ./gui/ -count=1 -timeout 120s -run "TestKnowledgeListMyHubShares|TestKnowledgeDeleteHubShare|TestKnowledgeUpdateHubShare|TestKnowledgeShareToHubPostsPackageAndTTL"

cd gui/frontend
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx -t "Hub shares|export success|edits a Hub share"
```

## Named increment after freeze

| Goal | Status |
|------|--------|
| Dual local export formats | **Done** — `jsonl` + `package` (`.knowledge.json`); zip container still optional |

## Out of freeze (explicit goals only)

1. Zip container (`.mckb.zip`) wrapping package JSON if product still requires it  
2. Pagination / search inside My shares for large accounts  
3. Offline cache of share list  
4. Full i18n audit of result / share strings  

## Freeze rule

Do not stack more export/share micro-features on generic “继续”.  
Re-open only with an explicit goal name + acceptance criteria.
