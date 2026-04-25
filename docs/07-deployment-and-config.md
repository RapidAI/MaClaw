# MaClaw Deployment And Config

## 1. Goal
This document defines how MaClaw services are laid out, configured, and deployed.

## 2. Monorepo Layout

Inside this repository:

- Desktop code stays at the root
- Hub lives under [hub](/D:/workprj/aicoder/hub)
- Hub Center lives under [hubcenter](/D:/workprj/aicoder/hubcenter)

## 3. Default Center

Default official Hub Center:

- `https://hubs.maclaw.top`

This should be the built-in default for:

- Desktop
- Hub
- Pocket

## 4. Hub Deployment

Recommended deployable structure:

```text
maclaw-hub/
  maclaw-hub(.exe)
  configs/config.yaml
  web/dist/
  data/
```

## 5. Hub Center Deployment

Recommended deployable structure:

```text
maclaw-hubcenter/
  maclaw-hubcenter(.exe)
  configs/config.yaml
  data/
```

## 6. SQLite Rules

Hub and Hub Center should use:

- SQLite
- WAL mode
- read/write split
- repository abstraction

## 7. PWA Hosting

PWA should be hosted directly by Hub under:

- `/app`

This keeps self-hosted deployment simple.

## 8. Important Config Values

### Desktop
- `RemoteEnabled`
- `RemoteHubURL`
- `RemoteHubCenterURL`
- `RemoteEmail`
- `RemoteSN`
- `RemoteMachineID`
- `RemoteMachineToken`

### Hub
- server settings
- database settings
- identity mode
- center settings
- hub identity and routing settings such as `hub.corporate_email_domain`
- mail settings

### Hub Center
- server settings
- database settings
- mail settings

### Corporate email routing
- Set `hub.corporate_email_domain` on a Hub when it should receive users from a specific company email domain.
- Leave `hub.corporate_email_domain` empty on the default catch-all Hub.
- Hub Center resolves exact domain matches first, then falls back to the empty-domain Hub.

## 9. Three-Node HA Examples

Recommended simplified HA config mode for Hub Center:

- Configure a per-node `ha.self_fqdn` that matches the node's public FQDN.
- Keep one shared `ha.nodes` catalog containing every Hub Center node.
- Let each node auto-resolve its own `node_id`, `advertise_url`, and peer list from that catalog.
- Keep a unique private key on each node via `ha.private_key_path`; do not share private keys across nodes.

For the lightweight 3-node Hub Center HA layout with local SQLite on each node, see:

- [docs/hubcenter-ha-3nodes.md](/D:/workprj/aicoder/docs/hubcenter-ha-3nodes.md)
- [docs/hubcenter-ha-go-live-checklist.md](/D:/workprj/aicoder/docs/hubcenter-ha-go-live-checklist.md)
- [deploy/check-hubcenter-ha.ps1](/D:/workprj/aicoder/deploy/check-hubcenter-ha.ps1)
- [deploy/hubcenter-ha.inventory.example.psd1](/D:/workprj/aicoder/deploy/hubcenter-ha.inventory.example.psd1)
- [deploy/render-hubcenter-ha-configs.ps1](/D:/workprj/aicoder/deploy/render-hubcenter-ha-configs.ps1)
- [hubcenter/configs/config.ha-hc1.example.yaml](/D:/workprj/aicoder/hubcenter/configs/config.ha-hc1.example.yaml)
- [hubcenter/configs/config.ha-hc2.example.yaml](/D:/workprj/aicoder/hubcenter/configs/config.ha-hc2.example.yaml)
- [hubcenter/configs/config.ha-hc3.example.yaml](/D:/workprj/aicoder/hubcenter/configs/config.ha-hc3.example.yaml)
- [hub/configs/config.ha-3centers.example.yaml](/D:/workprj/aicoder/hub/configs/config.ha-3centers.example.yaml)


## HA Quick Deploy

For the 3-node Hub Center HA rollout, use the repo root script:

```bat
deploy_all.cmd
```

Useful modes:

- `deploy_all.cmd`
  Full rollout for `hubcenter + hub`, then automatically runs HA smoke.
- `deploy_all.cmd hubcenter-only`
  Deploys only `hubcenter` to all 3 nodes. Use this for HA/UI/config fixes when `hub` does not need to change.
- `deploy_all.cmd hubcenter-only --no-check`
  Same as above, but skips the final HA smoke check.
- `deploy_hubcenter_only.cmd`
  Shortcut for `deploy_all.cmd hubcenter-only`. Recommended for routine Hub Center-only rollouts.
- `deploy_hubcenter_only_no_check.cmd`
  Shortcut for `deploy_all.cmd hubcenter-only --no-check`. Use only when you intentionally want to skip post-deploy validation.

The script defaults to:

- SSH user: `root`
- SSH password: `sunion123` when `REMOTE_PASS` is not set
- 3 HA nodes: `hubs.mypapers.top`, `hubs.maclaw.top`, `hubs2.maclaw.top`

After deployment, you can manually verify again with:

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls @('https://hubs.mypapers.top','https://hubs.maclaw.top','https://hubs2.maclaw.top') `
  -ClusterSecret '//8kbfllmLrjilq0gXkkJ84oEHcThxNi9uQP6mb5eOwcjV2DeQF0AMQNGh4k40S+'
```
