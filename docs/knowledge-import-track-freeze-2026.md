# Knowledge import progress track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should **not** extend the import-progress track without a **named new goal** (e.g. persist job ids across app restart, global float outside settings already shipped).

## Shipped (design → product)

Source design: [knowledge-import-progress-design.md](./knowledge-import-progress-design.md)  
Phase notes: [knowledge-import-progress-phase.md](./knowledge-import-progress-phase.md)

| Item | Summary |
|------|---------|
| Async import jobs | `KnowledgeStartImportDirectory` / `KnowledgeStartImportFiles` + job status |
| Import dialog | Choose → configure/precheck → progress → done |
| Real-time progress | Wails `knowledge:import-progress` events |
| Throttle | ≥500ms / job; force on failed last-item; final never throttled |
| Toast | `show-toast` on complete / partial / fail |
| last_item_* | Path / status / reason from store progress |
| failed_items | Up to 20 failure details on finish + sanitize for API |
| ext_counts | Scan precheck extension histogram |
| Minimize + float | Dialog minimize; **global** `KnowledgeImportProvider` float on any page |
| Expand restore | `maclaw:open-settings` knowledge tab + dialog rehydrate |

## Operator regression

```powershell
go test ./corelib/knowledge/ -count=1 -run "TestLastItemProgressFields|TestImportProgressEmitsLastItemReasonAndFailedItems|TestImportFailedItemsFromScanFailure|TestScanDirectoryFiltersAndDedups|TestSQLiteStoreImportFiles"

go test ./gui/ -count=1 -timeout 120s -run "TestKnowledgeImportProgressShouldEmitThrottle|TestKnowledgeImportDoneToast"

go test ./MaClawSrv/ -count=1 -run "TestSanitizeKnowledgeDirectoryImportResultForAPIRedactsPaths"
```

Frontend:

```powershell
cd gui/frontend
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/knowledgeImportProgress.test.ts
```

## Out of freeze (explicit goals only)

1. Persist import job IDs / full processing log across **app restart**.
2. Trailing-edge timer emit (in addition to interval throttle).
3. Full i18n of toast/float copy beyond current zh product strings.
4. Separate tracks: export/share UX, structured knowledge v2, multi-turn auto-recall (only if named).

## Sibling freeze

**Knowledge auto-recall** is also frozen — see [knowledge-auto-recall-track-freeze-2026.md](./knowledge-auto-recall-track-freeze-2026.md).
