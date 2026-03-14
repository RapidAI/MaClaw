# MaClaw Hub

MaClaw Hub is the self-hosted remote control service for MaClaw Desktop.

## Responsibilities

- Manage hub-local identity and SN issuance
- Receive Desktop session summaries, previews, and important events
- Host the PWA entry at `/app`
- Provide admin setup/login and debug APIs

## Run

```powershell
go run .\cmd\hub
```

Use a custom config file:

```powershell
go run .\cmd\hub --config .\configs\config.yaml
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

The packaged output is created under `.\package\maclaw-hub`.
