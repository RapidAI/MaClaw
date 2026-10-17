# MaClaw Hub Center

MaClaw Hub Center is the directory and entry service for MaClaw Hubs.

## Responsibilities

- Register self-hosted hubs
- Track hub heartbeats and online status
- Resolve an email address to one or more hub entry URLs
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

HA sync history is pruned automatically. Peers that run this build report how far they have applied, and acknowledged ops are deleted in small batches. For emergency disk cleanup, stop Hub Center first, then compact the local database. The command reads `database.dsn` from the config (a production host may use `data/codeclaw-hubcenter.db`). It does not contact other nodes.

```powershell
go run .\cmd\hubcenter backup create --config .\configs\config.yaml --out .\data\backups\pre-ha-prune.tar.gz --json
go run .\cmd\hubcenter --compact-db --config .\configs\config.yaml --json
```

`--compact-db` prunes replicated HA history and runs `VACUUM`. It refuses the vacuum when free disk is below the file size plus 512MiB. Do not pass `--force-window` or `--acked-through` unless you have read `docs/hubcenter-ha-lightweight-replication.md`.

The older prune command is still available:

```powershell
go run .\cmd\hubcenter maintenance ha-prune --config .\configs\config.yaml --retention-days 0.5 --max-retained-ops 4000 --batch-size 1000 --vacuum --json
```

If little is left to prune but the SQLite main file is still bloated by historical churn (free pages), reclaim the space with the standalone vacuum command (also stop Hub Center first — VACUUM briefly needs an exclusive lock):

```powershell
go run .\cmd\hubcenter maintenance vacuum --config .\configs\config.yaml --json
```

Both vacuum paths persist `auto_vacuum=INCREMENTAL` in the rebuilt database. A running service also does this itself: after each history prune batch it runs a small `PRAGMA incremental_vacuum`, and a legacy `auto_vacuum=NONE` file is rewritten once during the local maintenance window when disk space and replication are caught up. No external VACUUM timer is required. Set `database.maintenance.enabled: false` to turn that off. The window, stagger, and thresholds are documented in `docs/hubcenter-ha-lightweight-replication.md`.


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

