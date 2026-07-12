# AppView Phase 1 — Workspace shell + strict identity

**Status:** landed (2026-07)  
**Track:** AppView  
**Depends on:** [appview-phase0-revision-guard.md](./appview-phase0-revision-guard.md)

## Goal

Ship a minimal controlled `type=app_view` workspace:

- Server builds `maclaw.appview.v1` with `appId`, `sessionId`, `regions.main` (+ optional nav/side/actions).
- Open registry marks **strict** when `app_view` / `appId` present.
- Strict submit requires: `view_revision` (exact), `schema_version`, matching `appId` (and `sessionId` when both set).
- Frontend `AppViewShell` renders chrome + nav; nested main/side reuse existing `AgentTaskPanel` types.

## API

### Build (Go)

```go
view, err := BuildAppView(AppViewBuildInput{
  AppID: "expense", SessionID: "u1", Title: "报销", Layout: "workspace",
  Main: map[string]interface{}{"type":"form","id":"expense:form","title":"填写","fields":[...]},
  Nav:  []map[string]interface{}{{"id":"form","label":"表单","targetViewId":"expense:form"}},
})
a.emitAgentView(view)
```

### Submit (strict)

```json
{
  "view_id": "app:expense:u1",
  "view_revision": 123,
  "schema_version": "…",
  "app_id": "expense",
  "session_id": "u1",
  "data": { "...": "form fields", "_inner_view_id": "expense:form" }
}
```

Errors: `missing_view_revision`, `stale_view_revision`, `missing_schema_version`, `schema_version_mismatch`, `missing_app_id`, `app_id_mismatch`, `session_id_mismatch`.

## Frontend

| File | Role |
| --- | --- |
| `agentViewTypes.ts` | `type: "app_view"` union + nav/action types |
| `AppViewShell.tsx` | header / nav / main / side / footer; submit wraps workspace id + tokens |
| `AgentTaskPanel.tsx` | routes `app_view` → shell before hooks; body in `AgentTaskPanelContent` |
| `useAIAssistant.ts` | sends `app_id` / `session_id` on submit |

Nested `app_view` is rejected by `BuildAppView`.

## Next

1. ~~DataSrv BusinessView / Report → `regions.main`~~ → [appview-phase2-datasrv-workspace.md](./appview-phase2-datasrv-workspace.md)
2. Footer action bar protocol + dry-run/approval stay in same workspace.
3. Strict-only mode for installed enterprise apps from App Panel.
4. Wails E2E: open app_view → re-open → stale submit rejected.

## Tests

```powershell
go test ./gui/ -count=1 -timeout 180s -run "BuildAppView|StrictAppView|RememberAgentView|ValidateAgentViewSubmit"
```
