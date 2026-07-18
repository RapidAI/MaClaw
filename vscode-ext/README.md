# MaClaw ACP for VS Code

MaClaw AI coding assistant chat, living in the **bottom panel** so it never hides your Explorer.

- Talks to `maclaw-acp-bridge` over stdio (ACP / NDJSON JSON-RPC).
- The bridge attaches to the running MaClaw GUI (Mode B) — the GUI is the only agent brain.
- Installed and upgraded automatically by the MaClaw desktop app ("Launch VS Code" in Utilities); no manual setup needed.

## Commands

- `MaClaw: Open Chat` — focus the chat view in the panel.
- `MaClaw: New Session` — start a fresh ACP session.
- `MaClaw: Cancel Current Turn` — cancel the in-flight prompt.

## Settings

- `maclaw-acp.bridgePath` — optional explicit path to `maclaw-acp-bridge`; empty means auto-resolve (`MACLAW_ACP_BRIDGE` env, `<maclaw data dir>/bin`, then `PATH`).
