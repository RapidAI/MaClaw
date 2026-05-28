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

For a 3-node Hub Center HA deployment with local SQLite on each node, see `../docs/hubcenter-ha-3nodes.md`.

## HA history maintenance

HA sync history is pruned automatically by default. For emergency disk cleanup, stop Hub Center first when using `--vacuum`, then run:

```powershell
go run .\cmd\hubcenter backup create --config .\configs\config.yaml --out .\data\backups\pre-ha-prune.tar.gz --json
go run .\cmd\hubcenter maintenance ha-prune --config .\configs\config.yaml --retention-days 0.5 --max-retained-ops 50000 --batch-size 20000 --vacuum --json
```


## Disaster backup and restore

Create a disaster-recovery archive from the Hub Center root directory:

```powershell
go run .\cmd\hubcenter backup create --config .\configs\config.yaml --out .\data\backups\maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --json
```

The archive contains:

- `manifest.json` for AI/script inspection
- the Hub Center config file
- a consistent SQLite snapshot created with `VACUUM INTO`
- data-directory assets such as RSA/certificate files, skills, skill-market workspaces, gossip cache, and other user data

Runtime `.log` files are skipped by default. Add `--include-logs` when incident forensics need them.

Inspect an archive before restoring:

```powershell
go run .\cmd\hubcenter backup inspect --file .\data\backups\maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --json
```

Restore after stopping Hub Center:

```powershell
go run .\cmd\hubcenter restore --file .\data\backups\maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json
go run .\cmd\hubcenter restore --file .\data\backups\maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --target-root . --force
```

Restore refuses to overwrite existing files unless `--force` is set. After restore, start Hub Center and check `/api/health`.

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

