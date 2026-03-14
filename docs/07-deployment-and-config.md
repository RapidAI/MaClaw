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

- `https://hubs.mypapers.top`

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
- mail settings

### Hub Center
- server settings
- database settings
- mail settings
