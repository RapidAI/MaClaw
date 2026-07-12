# AppView Phase 5 — Decision note + launch feedback

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase4-decision-and-launch.md](./appview-phase4-decision-and-launch.md)

## Goal

1. Approval panels collect a **decision note**; **reject requires note** (client + server).
2. Install-tile one-click open shows a **visible hint** when the workspace does not open.

## Approval note

| Layer | Behavior |
| --- | --- |
| `BuildMaclawAppApprovalAppView` | Sets `requireNoteOnReject`, `noteLabel`, `notePlaceholder` on `type=approval` |
| `AgentTaskPanel` | `ApprovalDecisionPanel` textarea; blocks reject without note |
| `DecideMaclawAppApprovalInstance` | Server rejects empty note on reject |
| Panel submit | `note_required_on_reject` recoverable error, `KeepPanel=true` |

Approve may omit note (defaults to system message).

## Launch feedback

`AppsPage.openApp` still opens the app tab; after `OpenMaclawAppWorkspaceFromInstall`:

- `app_view_opened !== true` → status banner (`apps-workspace-launch-hint`)
- Reasons: `no_approval_instances`, `app_view_error`, generic MIS/binding message
- Errors from the bridge also show a dismissible hint

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "DecideMaclaw|BuildMaclawAppApproval|HandleMaclawAppApproval|OpenMaclawAppWorkspaceFromInstall|CollectMaclaw|BuildMaclawAppBusiness"
```

## Next

1. ~~Optional require-note-on-approve policy~~ → [appview-phase6-tile-badge-and-note-policy.md](./appview-phase6-tile-badge-and-note-policy.md)
2. ~~Toast / badge on the tile~~ → Phase 6 tile `!` badge
3. Wails E2E for reject-with-note, launch-hint, and tile badge.
