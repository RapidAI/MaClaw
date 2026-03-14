# MaClaw Remote Quick Start

## Full Guide

- Full usage and deployment guide: [D:/workprj/aicoder/docs/REMOTE_USAGE_AND_DEPLOYMENT.md](D:/workprj/aicoder/docs/REMOTE_USAGE_AND_DEPLOYMENT.md)
- Mobile remote guide (Chinese): [D:/workprj/aicoder/docs/MOBILE_REMOTE_GUIDE_CN.md](D:/workprj/aicoder/docs/MOBILE_REMOTE_GUIDE_CN.md)

## Start the full local remote stack

Double-click:

- `run_remote_stack.cmd`

This starts:

- `MaClaw Hub Center` on `http://127.0.0.1:9388`
- `MaClaw Hub` on `http://127.0.0.1:9399`

Main URLs:

- Hub Center admin: `http://127.0.0.1:9388/admin`
- Hub admin: `http://127.0.0.1:9399/admin`
- Hub PWA: `http://127.0.0.1:9399/app`

After a successful demo run, prefer the auto-login style PWA URL:

- `http://127.0.0.1:9399/app?email=you@example.com&entry=app&autologin=1`

After a few seconds, verify the services with:

- `check_remote_stack.cmd`

Then initialize both admin backends and register the local Hub to the local Hub Center:

- `setup_remote_stack.cmd`

Or run the whole local demo chain in one step:

- `run_full_remote_demo.cmd`

If you want the demo to automatically open the local Hub admin, Hub PWA, and Hub Center admin after success:

- `run_full_remote_demo_auto_open.cmd`

To inspect the most recent demo result at any time:

- `show_last_remote_demo.cmd`

To print one combined status summary for:

- service health
- latest demo result
- local admin/PWA links

use:

- `remote_stack_status.cmd`

If the latest demo file contains an activation email, `remote_stack_status.cmd` will print the auto-login PWA link instead of the plain `/app` URL.

To open the main local pages after startup or demo completion:

- `open_remote_demo_pages.cmd`

If you want to reset the local Hub / Hub Center state and rerun from a clean slate:

- `clean_remote_stack.cmd`

If you want the shortest full reset-and-run path:

- `reset_and_run_full_remote_demo.cmd`

If you want to stop the local Hub / Hub Center console windows without deleting data:

- `stop_remote_stack.cmd`

The setup script now waits for both `/healthz` endpoints automatically before it initializes admins and performs Hub registration. If the local services are not running yet, it will also try to launch `run_remote_stack.cmd` once and then retry. On a cold first run, this can take a little longer because both services use `go run`.

## First-time setup

1. Run `setup_remote_stack.cmd`
2. Open `http://127.0.0.1:9399/admin`
3. Open `http://127.0.0.1:9388/admin`
4. Confirm the local Hub is already registered to the local Hub Center

If your local stack has stale admins, old registrations, or old smoke/demo state, reset it first:

```bat
clean_remote_stack.cmd
```

Default local admin credentials created by `setup_remote_stack.cmd`:

- Username: `admin`
- Password: `MaClaw123!`
- Email: `admin@local.maclaw`

If either admin backend has already been initialized with a different password, override the defaults before running `setup_remote_stack.cmd`:

```bat
set HUBCENTER_ADMIN_USER=admin
set HUBCENTER_ADMIN_PASS=YourExistingPassword
set HUBCENTER_ADMIN_EMAIL=admin@local.maclaw
set HUB_ADMIN_USER=admin
set HUB_ADMIN_PASS=YourExistingPassword
set HUB_ADMIN_EMAIL=admin@local.maclaw
setup_remote_stack.cmd
```

You can also pass explicit credentials as arguments:

```bat
setup_remote_stack.cmd http://127.0.0.1:9388 http://127.0.0.1:9399 admin YourPass admin@local.maclaw admin YourPass admin@local.maclaw
```

## Desktop remote activation

1. Open MaClaw Desktop
2. Open the `Claude Remote` panel
3. Set:
   - `Hub URL` to your Hub, for example `http://127.0.0.1:9399`
   - `Hub Center URL` to `http://127.0.0.1:9388` for local debugging, or your own Center
4. Enter your email and activate remote access

## Run a local Claude remote smoke test

Use:

- `run_remote_smoke.cmd -- -project D:\path\to\your\repo -pty-probe -launch-probe`

This checks:

- Claude installation and provider/model configuration
- Windows ConPTY support
- Claude launch readiness under remote mode

To also activate against the current Hub and start a real remote Claude session:

- `run_remote_smoke.cmd -- -email you@example.com -activate -project D:\path\to\your\repo -start`

To force the smoke test to use the local stack on `9399/9388`:

- `run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\your\repo -start`

To verify that the started machine and session are actually visible from Hub:

- `run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\your\repo -start -verify-hub`

To keep the process alive for live Hub/PWA inspection:

- `run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\your\repo -start -verify-hub -hold-seconds 60`

To keep the process alive and continuously write JSON status snapshots while it runs:

- `run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\your\repo -start -verify-hub -hold-seconds 60 -progress-file D:\workprj\aicoder\.last_remote_smoke_live.json`

For machine-readable output:

- `run_remote_smoke.cmd -- -project D:\path\to\your\repo -pty-probe -launch-probe -json`

## Run the full local demo in one step

Use:

- `run_full_remote_demo.cmd`

This will:

1. Initialize `hubcenter`
2. Initialize `hub`
3. Register the local Hub to the local Hub Center
4. Activate Desktop remote identity with the local stack
5. Run:
   - PTY probe
   - Claude launch probe
   - real remote session start
   - hub visibility verification
6. Verify:
   - PWA email sign-in request
   - email confirm flow
   - viewer token issuance
   - machine/session/session-snapshot APIs

Defaults:

- email: `admin@local.maclaw`
- project: current repo root
- hold seconds: `60`
- progress file: `D:\workprj\aicoder\.last_remote_demo.json`

You can override them:

```bat
run_full_remote_demo.cmd you@example.com D:\workprj\aicoder 30 D:\workprj\aicoder\.my_remote_demo.json
```

Or run the same flow and automatically open the key local pages on success:

```bat
run_full_remote_demo_auto_open.cmd
```

You can also ask the full demo to send one test input through the viewer API, and optionally issue an `interrupt` or `kill` afterwards:

```bat
run_full_remote_demo.cmd admin@local.maclaw D:\workprj\aicoder 15 D:\workprj\aicoder\.last_remote_demo.json "" auto-open "Summarize current status in one sentence." interrupt
```

If you want to start from a clean local state and immediately run the full auto-open demo:

```bat
reset_and_run_full_remote_demo.cmd
```

You can also pass the same overrides to the auto-open wrapper:

```bat
run_full_remote_demo_auto_open.cmd you@example.com D:\workprj\aicoder 30 D:\workprj\aicoder\.my_remote_demo.json
```

After the script finishes, inspect the generated JSON file to confirm:

- activation succeeded
- ConPTY probe succeeded
- Claude launch probe succeeded
- a real remote session started
- the session is visible from Hub

Or use:

- `show_last_remote_demo.cmd`

It will print a short summary including:

- success / failure
- current phase
- activation status
- ConPTY readiness
- Claude launch readiness
- started session id
- hub visibility
- recommended next action

If you want to jump straight into the local pages after that, use:

- `open_remote_demo_pages.cmd`

If you do not pass an email, `open_remote_demo_pages.cmd` will try to reuse the activation email from the latest demo progress file automatically.

If you already have a demo result and just want the most useful local URLs and status in one place, use:

- `remote_stack_status.cmd`

If you want to script-verify the viewer login flow and session control API path against the latest demo result, use:

- `verify_remote_controls.cmd`

This validates:

- email sign-in request
- email confirm flow
- viewer token issuance
- machine list
- session list
- session snapshot

You can also send a test input in the same run:

```bat
verify_remote_controls.cmd http://127.0.0.1:9399 D:\workprj\aicoder\.last_remote_demo.json "Summarize current status in one sentence."
```

## Build the remote services

Double-click:

- `build_remote_stack.cmd`

This builds/packages:

- `hub`
- `hubcenter`
