# Admin Modules

This folder now uses a thin-shell structure.

## Runtime shell

- admin.js: compatibility shell, i18n, auth/navigation core, migration markers
- index.html: script load order only
- admin-bootstrap.js: startup and refresh orchestration
- admin-module-health.js: module availability checks

## Shared infrastructure

- admin-tabs.js: tab registry
- admin-ui.js: shared UI helpers; custom dialogs via `AdminUI.confirmDialog` / `AdminUI.promptDialog` (never `window.alert` / `prompt` / `confirm`). Unit tests: `admin-ui-dialog.test.js`

## Removed Mirror Tree

- The old `hub/web/admin/js/` mirror tree has been deleted.
- The live runtime loads only top-level assets from `hub/web/admin/*.js`.
- If a file seems to exist only in notes or history under `hub/web/admin/js/`, treat it as obsolete.

## Domain modules

- center-tab.js: Hub Center registration/status
- tenant-tab.js: tenant management, login tenant selection, and admin scope UI
- governance-tab.js: manual bind, blocklist, invites, content audit, smart route
- marketplace-tab.js: capability marketplace policy, approvals, imports, MCP editor
- security-tab.js: security management and org tree
- machines-tab.js: machine list and session inspection
- group-discussion-tab.js: current-Hub MaClaw expert list, discussions, and results
- im-tab.js: IM sub-pane routing and bridge integrations
- feishu-tab.js: Feishu settings and bindings
- invitation-tab.js: recharge/invitation code management
- pwa-tab.js: PWA approvals and pending logins
- system-tab.js: mail, TLS, admin profile/password
- compute-tab.js: compute placeholders
- llm-provider-tab.js: provider management
- llm-service-tabs.js: model service groups/cards/defaults
- usage-stats-tab.js: usage reporting

## Maintenance rule

When adding or changing admin behavior:

1. Prefer editing the owning module instead of admin.js.
2. Keep admin.js as a compatibility shell only.
3. Treat `hub/web/admin/*.js` as the runtime source of truth.
4. Do not recreate the removed `hub/web/admin/js/` mirror tree.
5. Keep script load order in index.html aligned with module dependencies.
6. Run syntax and ASCII checks after edits.
7. Run `node hub/web/admin/validate-admin-modules.js` after structural changes.
8. Run `powershell -ExecutionPolicy Bypass -File hub/web/admin/check-admin.ps1` before handing off larger admin changes.

## Removed legacy files

These files were intentionally deleted and should not be restored:

- llmproviders.js
- usagestats.js
- admin-check.js
- hub-admin-check.js
- hub-llm-tab.js
- _extra.js
