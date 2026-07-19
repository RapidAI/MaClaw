# MaClaw ACP for VS Code

MaClaw AI coding assistant chat, living in the **bottom panel** so it never hides your Explorer.

- Talks to `maclaw-acp-bridge` over stdio (ACP / NDJSON JSON-RPC).
- The bridge attaches to the running MaClaw GUI (Mode B) — the GUI is the only agent brain.
- Installed and upgraded automatically by the MaClaw desktop app ("Launch VS Code" in Utilities); no manual setup needed.

## Commands

- `MaClaw: Open Chat` — focus the chat view in the panel.
- `MaClaw: New Session` — start a fresh ACP session.
- `MaClaw: Cancel Current Turn` — cancel the in-flight prompt.

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
