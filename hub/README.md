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

Hub can be configured with multiple Hub Center addresses via `center.base_urls`; see `../docs/hubcenter-ha-3nodes.md` for the 3-node HA example.


## Disaster backup and restore

Create a disaster-recovery archive from the Hub root directory:

```powershell
go run .\cmd\hub backup create --config .\configs\config.yaml --out .\data\backups\maclaw-hub-backup-2026-04-28-153012.tar.gz --json
```

The archive contains:

- `manifest.json` for AI/script inspection
- the Hub config file
- a consistent SQLite snapshot created with `VACUUM INTO`
- data-directory assets such as TLS/certificate files, skills, user/device/session/chat/IM/LLM data, and other local runtime state
- TLS cert/key files referenced by config when they live outside the data directory

Runtime `.log` files are skipped by default. Add `--include-logs` when incident forensics need them.

Inspect an archive before restoring:

```powershell
go run .\cmd\hub backup inspect --file .\data\backups\maclaw-hub-backup-2026-04-28-153012.tar.gz --json
```

Restore after stopping Hub:

```powershell
go run .\cmd\hub restore --file .\data\backups\maclaw-hub-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json
go run .\cmd\hub restore --file .\data\backups\maclaw-hub-backup-2026-04-28-153012.tar.gz --target-root . --force
```

Restore refuses to overwrite existing files unless `--force` is set. After restore, start Hub and check `/api/health`.

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
