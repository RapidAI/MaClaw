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
  no `/ \ :`, control characters or leading dots, at most 120 runes). It falls
  back to the bot id when the name is unusable or another bot of the same
  owner sanitizes to the same folder (`MaterialsFolder`).
* Hub derives the folder from the bot record. The client only sends a file
  name, and that is sanitized too. desktopd refuses any upload path outside
  `~/素材/`.
* One file is at most 200 MB (`desktop.UploadMax`). A file with the same name
  is replaced.

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
     ─▶ docker exec -i … desktop_supervisor.py file write <path>   (existing supervisor op)
     ─▶ docker exec … chown desktop:desktop -- ~/素材 ~/素材/<bot> <file>   (exec runs as root)
```

Chunk request: `{"upload_id","name","offset","total","data":<base64>}`. The
answer is `{"upload_id","name","received","total","done","path"}`, and `path`
(`~/素材/…`) is set on the last chunk. A wrong offset answers 409 with
`received`, so a client can resume. Offset 0 restarts. Staged parts nobody
finishes are swept after an hour.

## In the chat

* There is a paperclip button in the composer, and files can be dropped on
  or pasted into the composer. Each file uploads at once and shows a
  percentage, its size when done, or an error ("超过 200 MB" / "上传失败"). ×
  removes it. Send waits while a file is uploading, and files can be sent
  without text.
* The sent message is the typed text plus a trailing list:

  ```
  用这些图发一篇小红书

  [素材]
  - ~/素材/小红书助手/封面.png
  - ~/素材/小红书助手/vlog.mp4
  ```

  The bubble hides the list and shows file chips instead (`splitMaterials`).
* **New task:** the understand prompt says a trailing `[素材]` list holds files
  already on the desktop. The work card must name every path and say to use
  them. The verbatim fallback carries the list too.
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

The image does not need a rebuild (the ownership fix is a desktopd-side chown): the upload reuses the supervisor's
existing `file write` op.

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
