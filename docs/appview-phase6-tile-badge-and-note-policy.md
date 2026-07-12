# AppView Phase 6 — Tile badge + approve-note policy

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase5-note-and-launch-feedback.md](./appview-phase5-note-and-launch-feedback.md)

## Goal

1. Per-app **tile badge** when one-click workspace open fails (in addition to list footer hint).
2. Optional install policy **`require_note_on_approve`** (reject remains always note-required).

## Tile badge

| State | UI |
| --- | --- |
| Launch failed | Red `!` on app icon; `has-workspace-issue` border; tooltip includes message |
| Launch succeeded | Badge cleared for that app id |
| Re-open click | Clears previous issue for that app, then re-evaluates |

Footer `workspaceLaunchHint` still shows the latest message (dismissible without clearing tile badges).

## Approve-note policy

Install package (any of):

```json
{
  "governance": {
    "approval_panel": { "require_note_on_approve": true }
  }
}
```

or `appview.approval.require_note_on_approve`.

Effects:

- `BuildMaclawAppApprovalAppView` sets `requireNote: true` when flag present on started result.
- `DecideMaclawAppApprovalInstance` rejects empty note on approve when policy set.
- Panel submit maps to `note_required_on_approve`.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "DecideMaclaw|BuildMaclawAppApproval|MaclawAppInstallApprovalPanelPolicy|HandleMaclawAppApproval|OpenMaclawAppWorkspaceFromInstall|CollectMaclaw|BuildMaclawAppBusiness"
```

## Next

1. ~~Harden launch message helpers~~ → [appview-track-freeze-2026.md](./appview-track-freeze-2026.md) + `appsWorkspaceLaunch.ts` Vitest
2. Wails E2E (open tile → badge / approve with note) — **named goal only**
3. **AppView track frozen** for observation unless a new product goal is named.
