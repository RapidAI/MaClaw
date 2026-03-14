# MaClaw Hub Center

MaClaw Hub Center is the directory and entry service for MaClaw Hubs.

## Responsibilities

- Register self-hosted hubs
- Track hub heartbeats and online status
- Resolve an email address to one or more hub PWA entry URLs
- Provide platform-level admin governance for hubs, emails, and IPs

## Run

```powershell
go run .\cmd\hubcenter
```

Use a custom config file:

```powershell
go run .\cmd\hubcenter --config .\configs\config.yaml
```

## Package

```powershell
.\scripts\build.ps1
.\scripts\package.ps1
```

Or use the Windows one-click wrapper:

```bat
build.cmd
build.cmd build
```

The packaged output is created under `.\package\MaClaw-hubcenter`.
