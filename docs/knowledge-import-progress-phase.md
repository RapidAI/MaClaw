# Knowledge import progress — phase notes (2026)

## Scope

Highest-value remaining gaps from `docs/knowledge-import-progress-design.md` after the import dialog / async job shell shipped:

1. Toast when an import job finishes (dialog may already be closed)
2. Per-file skip/fail **reason** on progress events (`last_item_reason`)
3. Failed-file detail list on finish (`failed_items`, max 20)
4. Scan precheck **ext_counts** for Step 2 type histogram

## Delivered

| Area | Change |
|------|--------|
| `corelib/knowledge/types.go` | `ImportFailedItem`; `DirectoryImportResult` gains `FailedItems`, `LastItemPath/Status/Reason`, `ExtCounts` |
| `corelib/knowledge/store.go` | `importScannedItems` fills last-item fields + collects `FailedItems`; progress callbacks carry them |
| `corelib/knowledge/scan.go` | Supported-extension histogram in `ExtCounts` during scan/precheck |
| `gui/app_knowledge.go` | Progress events prefer store `LastItem*`; finish event includes `failed_items` + `error`; `show-toast` on complete/fail/partial |
| `gui/frontend/.../KnowledgeImportDialog.tsx` | Merges finish `failed_items` into processing log when progress events were throttled/missed |
| `MaClawSrv/http_knowledge.go` | Sanitize/redact new path/error fields for API |

## Toast policy

| Outcome | Type | Duration |
|---------|------|----------|
| Hard error (`err != nil`) | `error` | 5000ms |
| Some imported + some failed | `warning` | 5000ms |
| Failed with no imports | `error` | 5000ms |
| Clean complete (optional skips) | `success` | 4000ms |

Frontend already listens on `show-toast` via `Toast.tsx`.

## Tests

- `TestLastItemProgressFields`, `TestImportProgressEmitsLastItemReasonAndFailedItems`, `TestImportFailedItemsFromScanFailure` (`corelib/knowledge`)
- ExtCounts assertions in `TestScanDirectoryFiltersAndDedups`
- `TestKnowledgeImportDoneToast` (`gui`)
- Sanitize coverage for `FailedItems` / `LastItem*` (`MaClawSrv`)

## Follow-up (this phase)

| Item | Status |
|------|--------|
| Progress event throttle (500ms / job, force on failed last-item + final) | Done |
| Minimized floating progress bar + Expand | Done |
| Minimize affordance on dialog close during run | Done |

### Throttle notes

- `knowledgeImportJobs` always updated (polling stays accurate).
- Wails `knowledge:import-progress` throttled to ≥500ms per job ID.
- Failed last-item events bypass throttle so the processing log keeps failure reasons.
- Finish event is never throttled; throttle slot cleared after finish.

### Floating bar notes

- Shown when dialog is closed and job is running **or** just finished (until Dismiss).
- Expand reopens the dialog with preserved step/log state.
- Dismiss on terminal jobs remounts the dialog for a clean next import.

## Global floating bar (app shell)

`KnowledgeImportProvider` (`gui/frontend/src/components/settings/KnowledgeImportContext.tsx`) is mounted in `main.tsx` above `App`:

- Listens to `knowledge:import-progress` + polls `KnowledgeImportJobStatus` even when Settings is unmounted
- Renders `KnowledgeImportFloatingBar` on any page while a job is active/finished
- **Expand** → `maclaw:open-settings` (tab=`knowledge`) + `maclaw:knowledge-import-expand`
- Dialog rehydrates via `restoreJob` + `KnowledgeImportJobStatus`
- Float hidden while the import dialog is open (`setDialogOpen`)

## Not in this slice

- Extending toast/copy i18n beyond Chinese product strings used by other desktop toasts
- Trailing-edge timer emit (current design: interval + force failed/final is enough)
- Restoring the full processing log after a full app restart (job id is in-memory only)

## Freeze

This track is **frozen** — see [knowledge-import-track-freeze-2026.md](./knowledge-import-track-freeze-2026.md).
