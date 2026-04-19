# Admin Modules

This folder now uses a thin-shell structure.

## Runtime shell

- admin.js: compatibility shell, i18n, auth/navigation core, migration markers
- index.html: script load order only
- admin-bootstrap.js: startup and refresh orchestration
- admin-module-health.js: module availability checks

## Shared infrastructure

- admin-tabs.js: tab registry
- admin-ui.js: shared UI helpers
- js/core.js: source-side core mirror

## Domain modules

- center-tab.js: Hub Center registration/status
- governance-tab.js: manual bind, blocklist, invites, content audit, smart route
- security-tab.js: security management and org tree
- machines-tab.js: machine list and session inspection
- im-tab.js: IM sub-pane routing and bridge integrations
- hub-llm-tab.js: legacy Hub LLM pane runtime
- feishu-tab.js: Feishu settings and bindings
- invitation-tab.js: recharge/invitation code management
- pwa-tab.js: PWA approvals and pending logins
- system-tab.js: mail, TLS, admin profile/password
- voiceprint-tab.js: voiceprint management
- compute-tab.js: compute placeholders
- llm-provider-tab.js: provider management
- llm-service-tabs.js: model service groups/cards/defaults
- usage-stats-tab.js: usage reporting

## Maintenance rule

When adding or changing admin behavior:

1. Prefer editing the owning module instead of admin.js.
2. Keep admin.js as a compatibility shell only.
3. Mirror source-side changes into hub/web/admin/js/ when applicable.
4. Keep script load order in index.html aligned with module dependencies.
5. Run syntax and ASCII checks after edits.
6. Run `node hub/web/admin/validate-admin-modules.js` after structural changes.

## Removed legacy files

These files were intentionally deleted and should not be restored:

- llmproviders.js
- usagestats.js
- admin-check.js
- hub-admin-check.js
- _extra.js