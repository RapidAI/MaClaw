# HubCenter Admin Assets

`index.html` keeps the static page shell and tab markup. Behavior and styling are split by surface so future edits stay scoped.

## CSS

- `/pro-ui.css`: shared baseline styles. Keep this before admin-specific styles in `index.html`.
- `css/admin-shell.css`: base layout, navigation, panels, cards, and feature-specific desktop styling.
- `css/admin-responsive.css`: responsive overrides and viewport-specific adjustments.

## JavaScript

- `js/admin-core.js`: shared state, i18n, auth, tabs, hubs, routing, policies, failure logs, and console behavior.
- `js/user-management.js`: user migration, tenant selection, registration reports, and user dashboard.
- `js/profile-settings.js`: public URL, mail config, profile, and first-run setup checks.
- `js/gossip-admin.js`: gossip moderation and moderation provider settings.
- `js/skillmarket-admin.js`: SkillMarket review, purchases, trial config, and upload auth.
- `js/ha-news-admin.js`: HA cluster tools, SkillHub catalog, MCP market, and news management.

Keep script tags in the current order. `admin-core.js` must load first because feature modules depend on its shared helpers, i18n state, auth state, and tab functions.
