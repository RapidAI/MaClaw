# AppView Phase 2 — DataSrv business operation → workspace

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase1-shell.md](./appview-phase1-shell.md)

## Goal

When an enterprise normal app runs a DataSrv binding (`preferredView` / `preferredReport` / `preferredDashboard` / `preferredAction`), open a right-pane **AppView** workspace with the result mapped to controlled AgentView regions (not only the AppsPage local card).

## API

### `OpenMaclawAppBusinessWorkspace(input)`

1. Calls `ExecuteMaclawAppBusinessOperation` (unchanged result package).
2. `BuildMaclawAppBusinessAppView` → `maclaw.appview.v1`.
3. `emitAgentView` for the task panel.
4. Returns operation result plus:
   - `app_view_id`, `view_revision`, `schema_version`, `app_view_opened`

### Result mapping

| Mode | Main region |
| --- | --- |
| `business_view` / `business_report` with rows | `table_editor` |
| otherwise | `result_browser` (outputs + payload + artifacts) |

Layout: `workspace` / `report` / `record` by mode.

Hidden launch binding fields (`_preferred_view`, …) allow AppView submit (`view_id` prefix `app:`) to **refresh** the same workspace.

## Frontend

- `AppsPage` business run uses `OpenMaclawAppBusinessWorkspace` (fallback to `ExecuteMaclawAppBusinessOperation`).
- Wails stubs: `App.js` / `App.d.ts`.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "BuildMaclawAppBusiness|OpenMaclawAppBusiness|HandleMaclawAppWorkspace|BuildAppView|StrictAppView"
```

## Next

1. ~~Nav multi-region~~ → [appview-phase3-multi-region-approval.md](./appview-phase3-multi-region-approval.md)
2. ~~Approval apps open AppView~~ → Phase 3 `OpenMaclawAppApprovalWorkspace`
3. App panel launch from install list (one-click open preferred view).
4. In-panel approve/reject actions.
