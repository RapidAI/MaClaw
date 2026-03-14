# MaClaw Remote Usage And Deployment Guide

## 1. Overview

MaClaw remote mode consists of four parts:

- `MaClaw Desktop`: hosts Claude Code locally and reports session state.
- `MaClaw Hub`: manages users, machines, sessions, PWA, and admin backend.
- `MaClaw Hub Center`: registers hubs and resolves entry URLs.
- `MaClaw PWA / Pocket`: mobile-facing remote control entry and session UI.

For local joint debugging in this repository, the default ports are:

- `Hub Center`: `9388`
- `Hub`: `9399`

Default local URLs:

- `Hub Center admin`: `http://127.0.0.1:9388/admin`
- `Hub admin`: `http://127.0.0.1:9399/admin`
- `Hub PWA`: `http://127.0.0.1:9399/app`

## 2. Repository Layout

- Desktop app root: [D:/workprj/aicoder](D:/workprj/aicoder)
- Hub service: [D:/workprj/aicoder/hub](D:/workprj/aicoder/hub)
- Hub Center service: [D:/workprj/aicoder/hubcenter](D:/workprj/aicoder/hubcenter)
- Frontend app: [D:/workprj/aicoder/frontend](D:/workprj/aicoder/frontend)
- Docs: [D:/workprj/aicoder/docs](D:/workprj/aicoder/docs)

## 3. Fast Local Demo

The fastest end-to-end local path is:

1. Run [reset_and_run_full_remote_demo.cmd](D:/workprj/aicoder/reset_and_run_full_remote_demo.cmd)
2. Check result with [show_last_remote_demo.cmd](D:/workprj/aicoder/show_last_remote_demo.cmd)
3. Open pages with [open_remote_demo_pages.cmd](D:/workprj/aicoder/open_remote_demo_pages.cmd)

One-click path that also opens pages:

- [run_full_remote_demo_auto_open.cmd](D:/workprj/aicoder/run_full_remote_demo_auto_open.cmd)

If you only want stack health and latest result:

- [remote_stack_status.cmd](D:/workprj/aicoder/remote_stack_status.cmd)

If you want to verify the user-side login and control API path from a script, use:

- [verify_remote_controls.cmd](D:/workprj/aicoder/verify_remote_controls.cmd)

## 4. What The Full Demo Does

The full demo script will:

1. Clean old local demo state.
2. Start `hubcenter` and `hub`.
3. Wait for `/healthz` on both services.
4. Initialize Hub Center admin.
5. Initialize Hub admin.
6. Configure Hub to point at local Hub Center.
7. Register Hub to Hub Center.
8. Run Claude remote smoke checks:
   - activation
   - ConPTY probe
   - Claude launch probe
   - real remote session start
   - hub visibility verification

Progress is written to:

- [D:/workprj/aicoder/.last_remote_demo.json](D:/workprj/aicoder/.last_remote_demo.json)

## 5. Expected Success Signals

When local demo succeeds, you should see:

- `Success: True`
- `Phase: completed`
- `Activation: approved`
- `ConPTY Ready: True`
- `Launch Ready: True`
- `Hub Visible: True`
- a non-empty `Session ID`

You can inspect these via:

- [show_last_remote_demo.cmd](D:/workprj/aicoder/show_last_remote_demo.cmd)
- [remote_stack_status.cmd](D:/workprj/aicoder/remote_stack_status.cmd)
- [verify_remote_controls.cmd](D:/workprj/aicoder/verify_remote_controls.cmd)

`verify_remote_controls.cmd` uses the latest demo progress file to:

1. request an email sign-in link from the Hub
2. confirm the one-time login token
3. fetch machines
4. fetch sessions
5. fetch the target session snapshot

Optional third argument sends a test input to the live session:

```bat
verify_remote_controls.cmd http://127.0.0.1:9399 D:\workprj\aicoder\.last_remote_demo.json "Summarize current status in one sentence."
```

## 6. Admin Initialization

Default local admin credentials used by helper scripts:

- Username: `admin`
- Password: `MaClaw123!`

If you need to override them for setup scripts, you can use environment variables:

- `HUBCENTER_ADMIN_USER`
- `HUBCENTER_ADMIN_PASS`
- `HUBCENTER_ADMIN_EMAIL`
- `HUB_ADMIN_USER`
- `HUB_ADMIN_PASS`
- `HUB_ADMIN_EMAIL`

Relevant helper:

- [setup_remote_stack.cmd](D:/workprj/aicoder/setup_remote_stack.cmd)

## 7. Hub Center Registration

For local debugging, Hub defaults to Hub Center:

- `http://127.0.0.1:9388`

Hub registration reports:

- advertised `base_url`
- `host`
- `port`

Registration prefers the domain/host parsed from `server.public_base_url`. If that is missing, it falls back to auto-detected local IP plus the configured listening port.

For virtual-host deployments, you should always set:

- `server.public_base_url`

in Hub config.

## 8. PWA Login Flow

PWA no longer asks the user to manually enter `SN`.

Current PWA login flow:

1. User opens PWA.
2. User enters email, or PWA receives `?email=...&autologin=1`.
3. Hub sends a sign-in email or, in local dev mode, returns a confirm URL.
4. User opens the sign-in link.
5. Hub returns viewer token and PWA becomes authenticated.

For local debugging, the PWA can render a direct `Open Sign-in Link` action when SMTP is not configured.

## 9. Desktop Remote Panel

Inside the Desktop app, the `Claude Remote` panel currently supports:

- remote enable switch
- Hub URL
- Hub Center URL
- remote email
- Claude readiness checks
- ConPTY probe
- Claude launch probe
- full smoke run
- session list
- send / interrupt / kill controls

Main file:

- [D:/workprj/aicoder/frontend/src/App.tsx](D:/workprj/aicoder/frontend/src/App.tsx)

## 10. Build And Package

### Hub

Directory:

- [D:/workprj/aicoder/hub](D:/workprj/aicoder/hub)

Quick build:

- [D:/workprj/aicoder/hub/build.cmd](D:/workprj/aicoder/hub/build.cmd) `build`

Quick package:

- [D:/workprj/aicoder/hub/build.cmd](D:/workprj/aicoder/hub/build.cmd)

### Hub Center

Directory:

- [D:/workprj/aicoder/hubcenter](D:/workprj/aicoder/hubcenter)

Quick build:

- [D:/workprj/aicoder/hubcenter/build.cmd](D:/workprj/aicoder/hubcenter/build.cmd) `build`

Quick package:

- [D:/workprj/aicoder/hubcenter/build.cmd](D:/workprj/aicoder/hubcenter/build.cmd)

## 11. Main Runtime Scripts

### Start stack

- [run_remote_stack.cmd](D:/workprj/aicoder/run_remote_stack.cmd)

### Stop stack

- [stop_remote_stack.cmd](D:/workprj/aicoder/stop_remote_stack.cmd)

### Health check

- [check_remote_stack.cmd](D:/workprj/aicoder/check_remote_stack.cmd)

### Clean state

- [clean_remote_stack.cmd](D:/workprj/aicoder/clean_remote_stack.cmd)

### Build services

- [build_remote_stack.cmd](D:/workprj/aicoder/build_remote_stack.cmd)

## 12. Local Config Defaults

### Hub defaults

- host: `0.0.0.0`
- port: `9399`
- center base url: `http://127.0.0.1:9388`

### Hub Center defaults

- host: `0.0.0.0`
- port: `9388`

### Desktop defaults for local debug

- Hub Center fallback: `http://127.0.0.1:9388`

## 13. Recommended Deployment Flow

For a self-hosted deployment:

1. Build or package Hub.
2. Build or package Hub Center if you want centralized entry resolution.
3. Configure Hub:
   - `server.public_base_url`
   - `center.base_url`
   - `identity.enrollment_mode`
   - SMTP, if mail-based login is needed
4. Initialize Hub admin.
5. Initialize Hub Center admin.
6. Register Hub to Hub Center.
7. Configure Desktop remote mode to point to the target Hub.

## 14. Troubleshooting

### Hub or Hub Center not healthy

Run:

- [check_remote_stack.cmd](D:/workprj/aicoder/check_remote_stack.cmd)

If not healthy:

- rerun [run_remote_stack.cmd](D:/workprj/aicoder/run_remote_stack.cmd)
- or clean first with [clean_remote_stack.cmd](D:/workprj/aicoder/clean_remote_stack.cmd)

### Demo failed

Inspect:

- [D:/workprj/aicoder/.last_remote_demo.json](D:/workprj/aicoder/.last_remote_demo.json)

Then use:

- [show_last_remote_demo.cmd](D:/workprj/aicoder/show_last_remote_demo.cmd)

### Claude cannot launch

Check Desktop diagnostics:

- Claude installed
- provider/model configured
- ConPTY probe result
- launch probe result

Main backend files:

- [D:/workprj/aicoder/remote_diagnostics.go](D:/workprj/aicoder/remote_diagnostics.go)
- [D:/workprj/aicoder/remote_pty_windows.go](D:/workprj/aicoder/remote_pty_windows.go)

### PWA cannot sign in locally

Check:

- Hub is healthy
- PWA is opened from Hub
- email-request returns a dev confirm URL or mail is configured

## 15. Current State

At the time of writing, the local remote-control chain is already runnable:

- Hub and Hub Center can start locally
- admin setup works
- Hub registration works
- Claude ConPTY probe works
- Claude launch probe works
- real remote smoke can start a Claude remote session
- Hub visibility verification succeeds

This document should be kept in sync as the remaining real-time PWA and long-session improvements land.
