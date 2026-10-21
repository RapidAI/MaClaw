# Desktop bot materials (file uploads)

Status: implemented, not deployed. 2026-10-21.

The person attaches files (pictures, video, documents) in the desktop bot
chat. They land on that bot's remote desktop, and the task the files came
with is told where they are.

## Where files land

`~/素材/<bot folder>/<file name>` in the **owner's** desktop container
(`/home/desktop/素材/…`). Thunar shows it, and the bot's Chromium file picker
can open it (type or paste the full path).

* `<bot folder>` is the bot's name, sanitized (`corelib/desktop.SafeMaterialName`:
  no `/ \ :`, control characters or leading dots, at most 120 runes).
  `MaterialsFolder` keeps a name that exactly one of this owner's bots wants.
  A shared name falls back to each bot's id. A bot whose name sanitizes away
  still occupies its id, so a later bot does not share that folder. If the id
  is taken too, a stable suffix is added.
* Hub derives the folder from the bot record. The client only sends a file
  name. The stored file name is that sanitized name plus `-<upload id>`
  (`MaterialStoredName`), capped at 120 runes, so two attachments that both
  arrive as `image.png` stay two files. The chip still shows the original
  name. The message uses the path Hub returns. desktopd refuses any upload
  path outside `~/素材/`.
* One file is at most 200 MB (`desktop.UploadMax`).

**Not a security boundary.** Every bot of one person, isolated or shared, runs
in that person's one container as the same Linux user `desktop`, and has a
`bash` tool there. Bot A can read bot B's folder. The folders keep things
tidy and keep each task pointed at its own files. Different people stay
separate: their containers are separate, and Hub checks that the bot belongs
to the caller.

## Flow

```
guiapp composer ──(Wails) UploadDesktopBotMaterialChunk, 4 MB base64 chunks──▶
Hub  POST /api/v1/bots/{id}/desktop/files      (the person's normal login; owner only, else 404)
     stages chunks in $TMPDIR/maclaw-bot-materials/<tenant>/<owner>/<bot>/<upload_id>.part
     last chunk ─▶ desktoppool.UploadFile ─▶
desktopd POST /v1/desktops/upload?tenant_id&user_id&path   (raw bytes, Content-Length)
     ─▶ docker exec -i … desktop_supervisor.py file write <path>.partial
     ─▶ docker exec … mv -f -- <path>.partial <path>
     ─▶ docker exec … chown desktop:desktop -- ~/素材 ~/素材/<bot> <file>
```

The supervisor write still truncates the path it is given, and a fifth
argument is rejected by the image that is already running. desktopd therefore
writes a sibling `.partial` (still under `~/素材/`) and `mv`s it onto the
final path only after the byte count matches. A short body or a failed write
runs `rm -f` on the partial and leaves the previous file alone. `mv` and `rm`
run inside the same per-user lock as the write. `chown` of the folders and
the finished file runs in that same lock, because `docker exec` is root and
the desktop user must be able to replace the file. Once the byte count
matches, `mv` and `chown` finish even if the caller has canceled, so a
disconnect cannot leave the file owned by root. A failed write still
`chown`s the folders it created, and does not `chown` the previous file.

Chunk request: `{"upload_id","name","offset","total","data":<base64>}`. The
answer is `{"upload_id","name","received","total","done","path"}`, and `path`
(`~/素材/…`) is set on the last chunk. A wrong offset answers 409 with the
usual error envelope (`code` `MATERIAL_OFFSET`, `message`) plus `received`,
`upload_id`, `name`, and `total`. The GUI returns that struct instead of
throwing, and the page sends the next chunk from `received`. A thrown error
retries the same chunk. Offset 0 replaces the staged bytes (it truncates
the part; a failed unlink must not leave the new chunk appended to the old
file). The name and
the total are pinned on the first chunk. The staged part is kept when the
desktop put fails, so the same last chunk retries the put. After the file is
on the desktop, the same upload id answers with that path again, including a
retry of offset 0, so a lost success response does not look like a different
upload. A new attachment uses a new upload id. One mutex per
upload id: a 200 MB put does not block every other upload. Staged parts
nobody finishes are swept after an hour.

## In the chat

* There is a paperclip button in the composer, and files can be dropped on
  or pasted into the composer. Paste and drop read clipboard or drag items
  of kind `file` first, then the file list, so a Windows screenshot that
  never appears in `clipboardData.files` is still attached. Each file uploads
  at once and shows a percentage, its size when done, or an error ("超过 200
  MB" / "上传失败"). × removes it and cancels that upload. Send waits while
  any chip is still uploading or has failed; a failed chip stays until it is
  dismissed. Files can be sent without text. A secret prompt blocks this
  send the same way it blocks a typed send.
* The sent message is the typed text plus a trailing list:

  ```
  用这些图发一篇小红书

  [素材]
  - ~/素材/小红书助手/封面-<upload id>.png
  - ~/素材/小红书助手/vlog-<upload id>.mp4
  ```

  The bubble hides the list and shows file chips instead (`splitMaterials`).
* **New task:** the understand prompt says a trailing `[素材]` list holds files
  already on the desktop. The work card must name every path and say to use
  them. When an order is clipped (the card, the verbatim fallback, or an
  earlier message in the prompt), the trailing `[素材]` list is kept and the
  prose in front of it is what gets shortened.
* **Running task:** `EvaluateDesktopTaskSupplement` treats a message with a
  `[素材]` list as a supplement and passes it whole, so the paths are not
  clipped. It then goes through `AppendDesktopBotSupplement` like any other
  supplement (docs/design/desktop-bot-supplement-and-preference-zh.md).

## Limits / follow-ups

* Hub stages chunks on local disk. With several Hub instances behind a load
  balancer, the chunks of one upload must reach the same instance (sticky
  routing), or `MaterialStageDir` must be shared storage.
* There is no quota per person and no cleanup of `~/素材/`. Files stay until
  someone deletes them.
* Only the main send path attaches files. Messages queued behind a busy bot
  carry the list in their text as usual.

## Deployment

The image does not need a rebuild for the upload path. desktopd still calls
the existing 4-argument `file write`, then `mv` and `chown` on the container.
An image rebuild is only needed for the supervisor to copy stdin in 1 MiB
pieces; until then that write still buffers the partial, not the live file.

1. **desktopd** (each Docker host): deploy the new binary. On the host's
   nginx, add the `location = /v1/desktops/upload` block from
   `desktopd/deploy/nginx-dockerd.conf.example` (`client_max_body_size 210m`,
   request buffering off, 600 s timeouts). The default `32m` refuses large
   files.
2. **Hub**: deploy after desktopd, because the new route calls the new
   desktopd endpoint. Check that whatever proxy sits in front of Hub accepts
   at least 6 MB request bodies on `/api/v1/bots/*/desktop/files`. Make sure
   `$TMPDIR` has room for in-flight uploads, or set `MaterialStageDir`.
3. **guiapp**: release last. An older Hub answers 404 on the new route, so
   the chips show "上传失败".

## Acknowledgements (what the bot says when it takes work or files)

Every acknowledgement sentence is written by a model; there are no stock
template sets.

- **Accept (new job, with or without files, with options or unclear parts).**
  The understand step's own `told` is kept when it is a real sentence: not
  empty, not a parrot of the message or the work card, and naming the files
  when files came along. `options`/`unclear`/`assumption` stay structured
  fields (the chips come from `options`); the prompt asks `told` to mention
  them in its own words. When `told` is unusable, the job is **sent at once**
  and the GUI calls `WriteDesktopBotAck` (kind `accept`) in the background;
  the line is placed above the working bubble when it arrives.
- **Files added to a running job.** The supplement is delivered first (Hub
  receipt `收到。`/`Got it.` is a placeholder), then `WriteDesktopBotAck`
  (kind `supplement`, with the running task text and file names) replaces it.
- **Files with no job.** The understand reply (`pace=reply`) writes the line.
- `WriteDesktopBotAck` attempts: the person's assistant model (8s), its
  lightweight sibling on the same provider (10s), then the Hub shared model
  (10s) only if both failed. If every attempt fails the GUI shows one
  neutral line: `好，我先做着。` / `收到。` (English `OK, I'm on it.` /
  `Got it.`). Nothing waits on these calls.

### Which model

GUI-side calls (understand, its fallback, acks) use the person's own model
settings (assistant profile; an unset profile resolves to the Hub service).
The understand fallback used to resolve role `background`, which inherits
the Hub's shared `auto` model even for a custom-model user (observed
2026-10-21: 78.6s on `hub.mypapers.top auto` after the custom
`deepseek-v4.1-flash` returned empty). It now stays on the person's provider
(lightweight sibling); the Hub `background` role is the third, last attempt.
The desktop run itself is resolved by MaClawSrv from the account's server-side
config (`agentservice.ResolveLLMConfig`), not from the GUI's local providers.
