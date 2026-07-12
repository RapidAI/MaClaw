# AppView Phase 4 — In-panel decision + install one-click

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase3-multi-region-approval.md](./appview-phase3-multi-region-approval.md)

## Goal

1. Pending approval workspaces expose **type=approval** with Approve/Reject; decisions persist + sync + re-open AppView.
2. Clicking an enterprise app tile **opens the right-pane workspace** (DataSrv multi-region or latest approval instance) without an extra Run click.

## APIs

### `DecideMaclawAppApprovalInstance`

| Field | Meaning |
| --- | --- |
| `app_id` / `instance_id` / `approval_id` / `record_id` | Locate instance |
| `decision` | `approve`/`approved` or `reject`/`rejected` |
| `note` / `actor` | Optional audit |
| `open_app_view` | Default true — re-emit workspace after decision |

Terminal instances return `already_final=true` without changing status.

### `OpenMaclawAppWorkspaceFromInstall`

| Kind | Behavior |
| --- | --- |
| `enterprise_normal_app` | `OpenMaclawAppBusinessWorkspace` (install datasrv enrich) |
| `enterprise_approval_app` | Latest instance in pending_my_approval → my_requests → attention |

### Panel submit path

`AgentTaskPanel` approval buttons send `{ approved: true|false }`.  
`handleMaclawAppWorkspaceSubmit` detects this and calls `DecideMaclawAppApprovalInstance`.

`BuildMaclawAppApprovalAppView` uses `type=approval` while status is `pending` / `requires_input` / `attention`; otherwise result/progress.

## Frontend

- `AppsPage.openApp` best-effort calls `OpenMaclawAppWorkspaceFromInstall` for enterprise kinds.
- Wails: `DecideMaclawAppApprovalInstance`, `OpenMaclawAppWorkspaceFromInstall`.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "DecideMaclaw|BuildMaclawAppApproval|HandleMaclawAppApproval|OpenMaclawAppWorkspaceFromInstall|CollectMaclaw|BuildMaclawAppBusiness|OpenMaclawAppBusiness"
```

## Next

1. ~~Approval decision with required comment UI~~ → [appview-phase5-note-and-launch-feedback.md](./appview-phase5-note-and-launch-feedback.md)
2. ~~Install open failure feedback~~ → Phase 5 launch hint banner
3. Wails E2E: open tile → approve/reject-with-note → status handled.
