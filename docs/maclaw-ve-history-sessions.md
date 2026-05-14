# MaClaw Digital Employee History Sessions

## Goal

MaClaw should show digital-employee group discussion history in the AI sidebar so users can reopen useful conversations without leaving the assistant workspace.

Two history types are supported:

- Discussions started by the local user. These can be reopened and continued while the Hub discussion remains open.
- Discussions where a local digital employee was invited by another user. These are available as read-only history for inspection and traceability.

Opening either item creates an AI assistant tab so the user can switch between local chat, digital employee chat, and group history without losing context.

## Local Cache

Hub remains the source of truth. The client may cache summaries for fast sidebar rendering and detail snapshots for offline inspection. Local persisted history is split by data type:

- Conversation summaries, messages, participant state, permission fields, sync cursors, hide/restore state, and attachment metadata are stored in the local database.
- Attachment file bytes are stored under the MaClaw data directory, grouped by discussion/session directory.
- The database stores the attachment's Hub identity, local file path, checksum/size when available, download status, and audit metadata. The file system path is treated as cache/storage, not as the source of truth for permissions.

Suggested fields:

- discussion_id
- local_relation
- readonly
- status
- topic / question
- participant_ids
- participant_summaries
- hub_updated_at
- last_synced_at
- message_count
- messages_snapshot
- attachment_index
- attachment_local_root
- attachment_download_state
- local_visibility
- hidden_at
- sync_state
- last_error

## Permissions

`local_relation` determines whether a tab is writable.

| local_relation | Meaning | Read | Send | Summarize | Hide |
|---|---|---:|---:|---:|---:|
| initiated_by_me | User started the discussion locally | yes | yes while open | yes | yes |
| owned_ve_invited | Local digital employee was invited by someone else | yes | no | no | yes |

Closed or archived discussions are read-only unless the Hub explicitly allows reopening.

## Sync Behavior

- Sidebar history refreshes from Hub when opened and can be refreshed manually.
- Client heartbeat can carry a compact history version marker so the client knows when to refresh.
- Detail fetch is lazy: load full messages only when the user opens a history tab.
- Failed sync keeps the last usable local snapshot and records the error.
- Hidden local entries remain hidden locally but are not deleted from Hub.

## Attachments

History messages should preserve attachment metadata in the database. File download should continue to use Hub relay authorization keyed by discussion/session and attachment identifiers.

Downloaded files are stored under the MaClaw data directory in a per-discussion directory, for example:

`<maclaw-data-dir>/group-discussions/<discussion_id>/attachments/<attachment_id-or-safe-filename>`

Implementation notes:

- Create the discussion attachment directory lazily when the first attachment is downloaded.
- Sanitize filenames and keep the Hub attachment ID in the database so duplicate names do not collide.
- Read-only history still allows attachment download for audit/review, but never allows sending new messages.
- Local hide does not delete downloaded attachment files by default. A separate local cache cleanup action may remove files while retaining database metadata and allowing re-download from Hub if still authorized.
- Admin or retention deletion should remove or invalidate the Hub-side attachment first; local clients should mark stale files as unavailable or purge them according to policy.

## AI Tab Behavior

- Double-clicking a history item opens a tab.
- Existing tabs are reused by `discussion_id` where possible.
- Read-only tabs show a visible read-only marker and disable sending.
- Writable tabs use the same message send path as group discussion history.
- Closing a tab only closes the UI tab; it does not delete the Hub discussion.

## Compatibility

The sidebar should keep the existing recent-task workflow intact. History and digital employee lists are separate middle-pane tabs so older users can continue using the tool selector and recent tasks as before.