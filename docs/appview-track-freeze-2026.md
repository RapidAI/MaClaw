# AppView track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should not extend AppView without a **named new goal** (e.g. Wails E2E suite, or a new enterprise sample).

## Shipped phases

| Phase | Doc | Summary |
| --- | --- | --- |
| 0 | [appview-phase0-revision-guard.md](./appview-phase0-revision-guard.md) | `viewRevision` / schema stale submit guard |
| 1 | [appview-phase1-shell.md](./appview-phase1-shell.md) | `type=app_view` shell + strict appId |
| 2 | [appview-phase2-datasrv-workspace.md](./appview-phase2-datasrv-workspace.md) | DataSrv op → AppView |
| 3 | [appview-phase3-multi-region-approval.md](./appview-phase3-multi-region-approval.md) | Multi preferred* nav + approval workspace open |
| 4 | [appview-phase4-decision-and-launch.md](./appview-phase4-decision-and-launch.md) | Approve/reject + install one-click |
| 5 | [appview-phase5-note-and-launch-feedback.md](./appview-phase5-note-and-launch-feedback.md) | Reject note + list hint |
| 6 | [appview-phase6-tile-badge-and-note-policy.md](./appview-phase6-tile-badge-and-note-policy.md) | Tile badge + `require_note_on_approve` |
| 7 | Harden | Pure launch-message helpers + Vitest (`appsWorkspaceLaunch.ts`) |

## Operator regression (backend)

```powershell
go test ./gui/ -count=1 -timeout 180s -run "DecideMaclaw|BuildMaclawApp|OpenMaclawApp|CollectMaclaw|StrictAppView|RememberAgentView|ValidateAgentViewSubmit"
```

## Frontend unit (launch messages)

```powershell
cd gui/frontend
npm test -- appsWorkspaceLaunch.test.ts
```

## Out of freeze (explicit goals only)

1. Full Wails/desktop E2E for tile → panel → decide.
2. New enterprise sample apps (expense/procurement) as product content.
3. Knowledge / Hub tracks — separate named goals.
