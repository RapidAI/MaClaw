# srvdemo

`srvdemo` is a small `Go + Wails` desktop client used to demonstrate how to call `MaClawSrv`.

## What it demonstrates

- Configure service address, admin secret, and API credential.
- Initialize demo data through admin APIs: create tenant, user, and credential, or use the one-click wizard.
- Inspect admin overview, dashboard, alerts, audit events, and tenant summary from the desktop demo so operator-facing APIs are visible without curl, including tenant quota headroom and per-user usage cards.
- Exchange `api_key + api_secret` for bearer token.
- Read current user info and health status.
- Discover config schema.
- Read, validate, test, and update shared user config.
- Run a quick end-to-end flow that saves config, validates it, creates an instance, and sends the first message.
- Query instances, sessions, messages, runs, and usage summary.
- Exercise skill APIs including search, install, import, export, validate, improve, upload, status lookup, and market account lookup.
- Use one-click skill source templates for GitHub, Zip archive, SkillHub, and SkillMarket install flows.
- Inspect installed skills through selectable registry cards with quick get, validate, export, and delete actions.
- Browse paginated admin, MCP, and skill registry APIs directly from the demo by setting `limit` and `before` cursors.
- Review skill search results as cards and either quick-fill the install form or install directly from a selected result.
- Exercise MCP APIs including create, update, start, stop, health-check, tool discovery, and template-based setup.
- Refresh the latest conversation state from `session / messages / run` endpoints.
- Surface the latest request status in a shared operation banner, which helps during demos and troubleshooting.
- Copy request examples and payload drafts from the UI with one click.
- Start from generated client playbooks for bootstrap, config preflight, and first-message flows.
- Follow a dynamic quick guide that reflects current login, config, instance, and session progress.

## Local settings

The demo stores local connection settings in:

- `%USERPROFILE%\.srvdemo\settings.json` on Windows
- `$HOME/.srvdemo/settings.json` on Unix-like systems

The file is written with owner-only intent.

## Run

```bash
go build ./srvdemo
```

If you use Wails tooling directly, the frontend is already prebuilt under `frontend/dist` and embedded by Go.

## Typical flow

1. Fill in `Base URL` and `Admin Secret`, then save settings.
2. In the admin panel, click `One-click bootstrap` to auto-create a demo tenant, user, and credential, or create them manually.
3. The wizard auto-fills `api_key` and `api_secret` back into the login form.
4. Use the `Admin Insights` panel when you want control-plane visibility such as global counts, dashboard trends, alert feeds, audit timelines, or tenant summary.
5. Click login, configure the user LLM settings, then use `Quick start` or manually create an instance and send a message.
6. Use `Refresh conversation`, `Get session`, `List messages`, and `Get run` to inspect the latest agent exchange.

Example service defaults:

- Base URL: `http://127.0.0.1:18080`
- Login endpoint: `POST /api/v1/auth/token`


## UX

- The demo UI now leans toward Chinese-language onboarding and operation guidance for local teams.
