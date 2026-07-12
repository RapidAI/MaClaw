# Knowledge export / Hub share UX — phase notes (2026-07)

## Named goal

**Export / Hub share UX polish** (suggested after knowledge import + auto-recall freezes).

Design references:

- [knowledge-export-share-frontend-interaction-design-zh.md](./knowledge-export-share-frontend-interaction-design-zh.md)
- [knowledge-export-share-and-hub-import-design-zh.md](./knowledge-export-share-and-hub-import-design-zh.md)

## Delivered this phase

| Item | Detail |
|------|--------|
| Export success card | Path, sources/nodes/cards/facts/size, dismiss |
| Export actions | Copy path · Show in folder (`OpenFileOrShowInFolder`) · Share to Hub |
| Visibility hints | Per-scope helper under Hub share dialog select |
| Share success actions | Copy knowledge ID · Copy share link · Open share page · View Shares |
| **My shares panel** | `KnowledgeListMyHubShares` + export-tab list (title, scope, stats) |
| Share delete | `KnowledgeDeleteHubShare` + confirm dialog (link only; local KB kept) |
| Share edit | `KnowledgeUpdateHubShare` (title / description / visibility) |
| Dual export formats | `jsonl` snapshot vs `package` exchange JSON; format radio + save dialog filters |

## Files

- `gui/frontend/src/components/settings/KnowledgeSettingsPanel.tsx`
- `gui/frontend/src/App.css`
- `gui/frontend/src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx`

## Backend APIs (desktop)

| Method | Hub |
|--------|-----|
| `KnowledgeListMyHubShares` | `GET /api/knowledge/shares/mine` |
| `KnowledgeUpdateHubShare` | `PATCH /api/knowledge/shares/{id}` |
| `KnowledgeDeleteHubShare` | `DELETE /api/knowledge/shares/{id}` |

Auth reuses Hub viewer token (same as share-to-Hub).

## Freeze

This track is **frozen** — see [knowledge-export-share-track-freeze-2026.md](./knowledge-export-share-track-freeze-2026.md).

## Still open (explicit goals only)

1. Optional `.mckb.zip` container around package JSON
2. Full i18n audit of new result strings
3. My shares pagination / search for large accounts

## Regression

```powershell
go test ./gui/ -count=1 -run "TestKnowledgeListMyHubShares|TestKnowledgeDeleteHubShare|TestKnowledgeUpdateHubShare|TestKnowledgeShareToHub"

cd gui/frontend
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx -t "Hub shares|export success|edits a Hub share"
```
