# AppView Phase 3 — Multi-region DataSrv + approval workspace

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase2-datasrv-workspace.md](./appview-phase2-datasrv-workspace.md)

## Goal

1. When an enterprise app declares **multiple** DataSrv bindings (`preferredView` / `preferredReport` / `preferredDashboard` / `preferredAction`), open one AppView with **nav tabs** for each region.
2. Approval apps open a **workspace** (instance main + feedback side) after workflow start.

## Business multi-region

| Piece | Behavior |
| --- | --- |
| `collectMaclawAppRegionBindings` | Builds isolated inputs (one preferred* each) |
| Order | view → report → dashboard → action (browse-first) |
| Load | Execute each region; secondary soft-fail → error `result_browser` tab |
| `BuildMaclawAppBusinessMultiAppView` | `main[]` + `nav[]` with `targetViewId` |
| Install enrich | Fills empty preferred* from install package `datasrv` |

Return fields: `region_count`, `regions_loaded`, plus Phase 2 `app_view_*`.

## Approval workspace

| API | Behavior |
| --- | --- |
| `OpenMaclawAppApprovalWorkspace` | `StartMaclawAppApprovalWorkflow` + `BuildMaclawAppApprovalAppView` + emit |
| Main | `progress` steps when `progress_instances` exist, else instance `result_browser` |
| Side | feedback / sync / workflow_run package |

## Frontend

- AppsPage business run: still `OpenMaclawAppBusinessWorkspace` (now multi-region).
- AppsPage approval start / supplement: `OpenMaclawAppApprovalWorkspace` with fallback to `StartMaclawAppApprovalWorkflow`.
- Wails: `OpenMaclawAppApprovalWorkspace` in `App.js` / `App.d.ts`.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "CollectMaclaw|BuildMaclawAppBusiness|BuildMaclawAppApproval|OpenMaclawAppBusiness|HandleMaclawAppWorkspace|BuildAppView|StrictAppView"
```

## Next

1. ~~Approval in-panel approve/reject~~ → [appview-phase4-decision-and-launch.md](./appview-phase4-decision-and-launch.md)
2. ~~One-click open from install list~~ → Phase 4 `OpenMaclawAppWorkspaceFromInstall`
3. Wails E2E for multi-tab nav + stale revision + approve.
