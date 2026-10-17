# HubCenter HA: lightweight replication

This note describes the replication and SQLite changes that stop a HubCenter node from pinning a multi-gigabyte op log on an 8GB host.

## What failed in production

At about 20:05 the kernel stalled while tearing down the `maclaw-hubcenter.service` memory cgroup (`ext4_inode_cache`, `radix_tree_node`, `dentry` reclaim). The machine froze for about 40 minutes and needed a hard reboot. The SQLite file was 2.4GB. The HA prune log showed `remaining_ops=57198` and `max_seq=149463026` while each pass deleted only about 320 ops. Right after start, RSS was about 960MB at 25% CPU. `llm-monitor` lease writes failed with `database is locked (SQLITE_BUSY)`.

The op log (`ha_sync_ops`) is append-only and keyed by `seq`. Prune used to delete a row only when a newer op existed for the same entity, plus `llm_usage_batch` rows past the age cutoff. After the superseded copies are gone, each remaining row is the newest op of its entity, so a pass deletes almost nothing and the seq counter (149 million) is just the historical high-water mark. The 2.4GB file is that retained payload plus free pages from the churn. SQLite's page cache of that file is charged to the service cgroup. Killing the process then asks the kernel to drop those caches, which is what stalled the host.

`SQLITE_BUSY` on the monitor lease is the same single writer being held by a large delete or by `BEGIN IMMEDIATE` on an HA append. `busy_timeout` waits, then the statement fails if the lock outlasts it.

## Replication model

The wire format is unchanged for old peers:

- `GET /api/internal/ha/ops?after_seq=&limit=` still returns `{node_id, ops, next_after_seq, has_more, max_seq, min_seq}`.
- `POST /api/internal/ha/ops/apply` still accepts `{"ops":[...]}` as plain JSON.
- New fields (`protocol`) are ignored by old decoders.
- Responses are gzip-compressed only when the client sends `Accept-Encoding: gzip`. Old clients do not, and get plain JSON.
- Push bodies stay plain JSON so a protocol-1 apply handler and the existing request signature (which hashes the raw body) keep working. The push queue already replaces an older queued op for the same entity; the sender also coalesces the batch before it is marshaled.

Protocol 2 (this build) adds:

1. **Ack.** Each pull sends `ack_seq` (the cursor it has applied) and `node_id`. The source stores them in `ha_peer_acks`. The ack only moves forward.
2. **Ack prune.** When every configured peer has a row, last-write-wins ops at or below `min(fresh ack, maxSeq - history_max_retained_ops)` are copied into `ha_entity_heads` (one row per entity, latest payload) and then deleted in autocommit batches of at most 2000 rows. The snapshot rows stay after the op-log delete. The retained window (default 4000) keeps the newest slice in `ha_sync_ops`, so a peer can rewind a short distance without reading the snapshot. Append-only ops are not deleted by ack.
3. **Lag grace.** A protocol-2 peer that has not pulled for `history_peer_lag_grace_minutes` (default 360) stops pinning the log. The truncation point is the minimum ack of the peers that are still fresh. The quiet peer catches up from `ha_entity_heads` plus the retained tail. Append-only entities (`llm_usage_batch`, `token_bank_ledger_batch`) stay in the op log until the age cutoff.
4. **Snapshot catch-up.** `GET /api/internal/ha/snapshot?after_seq=` pages those heads. A protocol-2 syncer that sees `min_seq` ahead of its cursor applies the heads, then continues the incremental pull. A protocol-1 peer gets 404 on that path and keeps the old behavior: it applies whatever ops are still in the log. Newest-op retention stays in force until that peer reports an ack, so a mixed cluster does not drop state an old peer has not pulled.
5. **Coalesce on the wire.** Inside one pull page, last-write-wins entities ship only their last op. Append-only ops are never collapsed. The highest seq in the page is always kept, so a cursor that advances to the last returned op does not skip a newer state.

Startup migration is `CREATE TABLE IF NOT EXISTS` for `ha_peer_acks` and `ha_entity_heads` plus one index. That writes sqlite_master only. It does not rewrite the 2.4GB table and it does not run `VACUUM`.

## What is not deleted

Prune and `--compact-db` write only `ha_sync_ops`, `ha_applied_ops`, and `ha_entity_heads`. Hub rows, skill-market rows, orders, ledger balances, and `system_settings` are not history and are not touched. `ha_entity_versions` is also kept: it is the conflict-resolution clock, not the op log.

Append-only usage and ledger ops inside the age window are kept even by `--force-window`.

## SQLite and memory

- WAL, `busy_timeout`, and a single writer connection stay as they were (`max_write_open_conns` must be 1).
- The background checkpointer still runs `wal_checkpoint(PASSIVE)` and, when `auto_vacuum=incremental` is already persisted, `incremental_vacuum(1024)`. Every 10th tick tries `wal_checkpoint(TRUNCATE)` with a 2s budget so the WAL file can shrink. `TRUNCATE` does not rewrite the main database.
- `incremental_vacuum` does nothing on a database that was created with `auto_vacuum=NONE` until an offline `VACUUM` rewrites the header. That is why the file stayed 2.4GB after deletes.
- Default page cache is 8MiB per connection and mmap is 64MiB, so startup does not map the whole file.
- `runtime.soft_memory_limit_mb` (default 1536) calls `debug.SetMemoryLimit`. On an 8GB host that leaves room for the kernel. Set it to `-1` to disable.
- The in-memory gossip snapshot is capped at 8000 posts.
- Lease writes (`system_settings` upserts, including the llm-monitor lease) retry `SQLITE_BUSY` with a short backoff.

## Measured on a temp database

`TestAckPruneKeepsSnapshotAndShrinksFile` (modernc SQLite, this host) inserted 6000 ops: 2000 entities × 3 versions, each payload about 1KiB. After both peers were recorded as caught up, ack prune with a retained window of 200 deleted 5800 ops, copied 1800 last-write-wins heads into `ha_entity_heads`, and left 200 ops in the log. That prune took 169ms (about 34,000 deleted ops/s). `system_settings` was unchanged. The SQLite file went from 9,371,648 bytes to 3,194,880 bytes after `VACUUM` (34% of the pre-prune file). The remaining bytes are the snapshot heads, the retained tail, and SQLite's own pages. A 2.4GB production file shrinks by the same rule: `VACUUM` rewrites live pages, and the rewritten replication state is one head per entity plus the short tail, instead of every historical version and the free pages those versions left behind.

`TestCoalesceOpsBatchDropsDuplicates` collapsed 20,000 last-write-wins ops on 500 keys into 500 ops in 2.3ms (about 8.8 million ops/s) and allocated about 1.5MB. The highest seq in the batch is kept, so the puller's cursor does not skip a newer state. Wire gzip is opt-in (`Accept-Encoding: gzip`) and is not applied to signed push bodies.

The same package checks three cases the production prune was getting wrong:

- No ack row for a configured peer: unique entity heads are kept (rolling upgrade).
- A quiet protocol-2 peer: last-write-wins heads move to `ha_entity_heads`, the unacked usage batch stays in `ha_sync_ops`.
- `--force-window`: unacked last-write-wins ops outside the window are checkpointed, then removed; the usage batch stays.

## Upgrade order

Old and new nodes can run together. Do not compact a node until every peer is on this build and has pulled at least once.

1. Build `maclaw-hubcenter` for `linux/amd64` from this commit.
2. Upgrade one node at a time: stop it, replace the binary, start it. Leave `configs/config.yaml` in place unless you want the new defaults (`history_max_retained_ops: 4000`, `history_prune_batch_size: 1000`, `runtime.soft_memory_limit_mb: 1536`). Keys already set in the file or in the saved HA config win.
3. While any peer is still on the old binary it does not send `ack_seq`. The new nodes log `peer acks are incomplete` and keep newest ops. That is intentional.
4. After every peer is on this build, wait for one prune interval (default 10 minutes). The log line looks like `[hubcenter][ha] pruned history: ... ack_known=true ack_seq=<near max_seq>`. `remaining_ops` should fall toward `history_max_retained_ops` plus any append-only rows still inside the age window.
5. Row deletes do not return disk space until `VACUUM`. On each node, one at a time:
   - Confirm the other nodes are serving traffic.
   - `backup create` (or your existing backup) while the node is still running, or immediately after it stops.
   - Stop `maclaw-hubcenter`. Do not stop the other peers.
   - Check free disk. `VACUUM` needs about the current file size plus 512MiB. The command refuses to vacuum when that space is missing; the prune still commits.
   - Run the compact command below.
   - Start the node and confirm `/api/health` and that the other peers' sync errors clear.
6. Repeat for the other nodes. A peer that was compacted already has the applied state; the node you compact next still has the ops until its own compact.

### Compact command

Run on the host, with the service stopped. The DSN comes from `--config`. Production has used `data/codeclaw-hubcenter.db`; the example config uses `data/maclaw-hubcenter.db`. Use whichever path `database.dsn` names.

```bash
./maclaw-hubcenter --compact-db --config configs/config.yaml --json
```

Equivalent: `./maclaw-hubcenter maintenance compact --config configs/config.yaml --json`.

Optional, and easy to get wrong:

- `--acked-through <seq>` ignores `ha_peer_acks` and treats every peer as caught up through that seq. Only set it after you have checked, on each other node, `SELECT peer_node_id, last_pulled_seq FROM ha_peer_cursors` and taken the minimum cursor whose `peer_node_id` is the node you are compacting. A value that is too high deletes ops that peer has not applied.
- `--force-window` copies unacked last-write-wins heads into `ha_entity_heads` and deletes those ops so the file can shrink before acks exist. Protocol-2 peers catch up from the snapshot. Protocol-1 peers that have not pulled those ops will miss them. Do not use this until every peer is on this build.

Neither flag is required for the normal upgrade path above.

### What to watch after restart

- `[hubcenter] soft memory limit set to 1536 MiB`
- `[hubcenter][ha] pruned history: ... ack_known=true`
- No repeating `[llm-monitor] write monitor lease failed: ... SQLITE_BUSY` after the first minute
- SQLite file size after compact should drop by the historical versions and free pages. The steady state is one snapshot head per last-write-wins entity, the retained op-log tail, append-only ops still inside the age window, and the business tables. If `ack_known` is false, newest unacked ops stay in `ha_sync_ops` and `VACUUM` cannot remove them.
