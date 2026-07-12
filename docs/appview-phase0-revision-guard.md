# AppView Phase 0 — AgentView revision guard

**Status:** landed (2026-07)  
**Track:** AppView (post skill-lifecycle freeze)

## Goal

Before a full AppView shell exists, AgentView submits must not apply against a **superseded** panel after a newer open/update. Design requirement from `agent-dynamic-ui-runtime-design-zh.md`:

> 提交必须带 `schemaVersion`、`viewRevision`；后端拒绝过期 revision。

## What shipped

| Piece | Behavior |
| --- | --- |
| Open registry | `App.agentViewOpen[view_id]` stores monotic `viewRevision` + `schemaVersion` |
| Emit | `emitAgentView` calls `rememberAgentViewOpen` → `meta.viewRevision` + hidden `_agent_view_revision` |
| Clear / successful complete | `forgetAgentViewOpen` |
| Submit validation | Stale revision → `error=stale_view_revision`; schema mismatch → `schema_version_mismatch` |
| Payload | `AgentViewSubmitPayload.view_revision` / `schema_version` (+ data hidden fields as fallback) |
| Frontend | `submitAgentView` extracts tokens from visible view meta/hidden fields and sends them |

## Compatibility

- Clients that **omit** `view_revision` still succeed (legacy).
- Clients that send an **older** revision after re-open are rejected with a recoverable bilingual message.
- Disk / unknown view ids without an open record are not blocked.

## Next

1. ~~Full `type=app_view` workspace shell~~ → [appview-phase1-shell.md](./appview-phase1-shell.md)
2. ~~Require revision when `appId` present~~ → Phase 1 strict mode
3. Map DataSrv BusinessView / Report / Dashboard into AppView regions.
4. Wails E2E for right-pane stale submit.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "AgentViewRevision|RememberAgentView|ValidateAgentViewSubmit|ForgetAgentView"
```
