# MaClaw ACP for VS Code

MaClaw AI coding assistant chat, living in the **bottom panel** so it never hides your Explorer.

- Talks to `maclaw-acp-bridge` over stdio (ACP / NDJSON JSON-RPC).
- The bridge attaches to the running MaClaw GUI (Mode B) — the GUI is the only agent brain.
- Installed and upgraded automatically by the MaClaw desktop app ("Launch VS Code" in Utilities); no manual setup needed.

## Commands

- `MaClaw: Open Chat` — focus the chat view in the panel.
- `MaClaw: New Session` — start a fresh ACP session.
- `MaClaw: Cancel Current Turn` — cancel the in-flight prompt.

## Remote coding agent (Phase 1)

The sidebar **远程编程** card can attach to an existing MaClaw
`remote_coding_dev` task:

1. Start **MaClaw GUI** (agent brain + SSH sessions).
2. Create / open a remote coding task in MaClaw (host / user / workdir tags).
3. In VS Code sidebar: **刷新** → select the task → **附着远程**.
4. If SSH is down, you will be prompted for the password once (not stored).
5. Chat turns then use that task’s local path as ACP `cwd`, so sticky remote
   routing runs `RemoteCodingSubAgent` on the server.
6. **切回本地工作区** returns to the normal local workspace agent.

Notes:

- File edits land on the **remote host**; the local VS Code tree is unchanged
  unless you also open the remote folder (e.g. Remote-SSH) or sync files.
- Requires Mode B (running MaClaw GUI) and a current `maclaw-acp-bridge`.
- Tool path chips show `remote:/path` in remote mode; **click opens a read-only
  preview** (`maclaw-remote://`) fetched over the sticky SSH session.
- Sidebar **远端 ls work_dir** lists the remote project root.
- Attach state is remembered across VS Code reloads (re-arm SSH if needed).
- Commands: `Refresh Remote Coding Tasks`, `Detach Remote Coding`,
  `Open Attached Remote Task Folder`, `Open Remote Path Preview`,
  `List Remote Work Dir`, `Refresh Remote Previews`,
  `Open Work Dir in Remote-SSH`.
- After each successful remote turn, open `maclaw-remote://` previews auto-refresh.
- **Remote-SSH 打开** launches a new VS Code window on `user@host:work_dir`
  when `ms-vscode-remote.remote-ssh` is installed.
- **远端 ls** is a QuickPick: pick files to preview, folders to drill down.
- **远端↔本地** / editor title diff icon compares remote preview to a matching
  local workspace file (`work_dir`-relative path, then basename search).
- **远端搜索** runs `rg`/`grep` on the remote host (scoped to work_dir); pick
  hits (multi-select) to open previews.
- **远端 ls** supports multi-select open (up to 10 files) and in-folder search.
- Search hits **jump to line** in the remote preview (header-aware).
- **Find in Remote Preview** (editor title) lists matches in the open
  `maclaw-remote://` document and jumps to the selection.
- Sidebar **Search Results** tree groups hits by file; click a line to open
  the remote preview at that line. Status bar + first-line decoration show
  **远端只读预览** on `maclaw-remote://` documents.
- **Export Search Results** (tree title / command) writes Markdown or JSON.
- Open previews that match the current search get **line highlights + gutter
  border**; **F4 / Shift+F4** (or title icons) jump next / previous hit.
- **Copy Remote Path** (title / tab context) copies absolute or work_dir-relative
  path; **Open Recent Remote File** reopens the last ~24 previews.
- Sidebar **Remote Explorer** lazy-loads `work_dir` (expand folders, open files
  as previews; context: refresh / search in folder). **Hide dotfiles** is on by
  default (title eye icon toggles, sticky setting); **filter** by name is
  session-only (title funnel).
- Sidebar **Agent Changes** lists files the agent edited/deleted this session
  (from tool locations + File change cards). After a turn with writes, a toast
  offers **查看改动** / **全部打开**. Export as Markdown / JSON.
- Chat **File change** cards and tool path chips expose **打开预览 / Diff 本地**
  (and copy path in remote mode); relative paths resolve under `work_dir`.

## Pre-input queue

The composer stays usable while a turn is running: pressing Enter queues the
message instead of dropping it (the bridge rejects concurrent prompts), and
queued prompts fire FIFO, one turn at a time, as each turn ends.

- Queued prompts appear as chips above the composer. Click a chip's text to
  pull it back into the composer for editing (appended if the composer already
  has a draft); `▲` **steers it into the running turn** when the MaClaw host
  supports `session/steer` (the text is injected between agent iterations,
  like the GUI's 引导发射) — on older hosts it instead jumps to the front of
  the queue and leads the next turn; `✕` removes it; `Clear all` empties the queue.
- With the composer empty, `↑` pulls the newest queued prompt back for editing.
- If a turn fails (bridge down, quota, …), auto-fire pauses and the strip shows
  a hint — click `▲` on any chip to resume. Cancelling a turn is not an error:
  the queue keeps going.
- `MaClaw: New Session` discards the queue together with the old session.
- The queue holds at most 50 prompts; when full, the composer keeps your text.

## Settings

- `maclaw-acp.bridgePath` — optional explicit path to `maclaw-acp-bridge`; empty means auto-resolve (`MACLAW_ACP_BRIDGE` env, `<maclaw data dir>/bin`, then `PATH`).
