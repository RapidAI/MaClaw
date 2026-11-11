# Build & test notes (guiapp)

## Building guiapp on Windows

- Official build is `build_win.bat` (compiles `./cmd/maclaw-gui/` with
  `-tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=<v>"`).
- `go build`/`go test` on package `guiapp` requires `guiapp/resource_windows_amd64.syso`
  (gitignored; build_win.bat generates then deletes it). To produce it for ad-hoc dev:

  ```bat
  powershell -NoProfile -ExecutionPolicy Bypass -File scripts\prepare_windows_resources.ps1 -Root . -Version 0.0.0 -AppName MaClaw
  go -C tools\windowsresgen run . -arch amd64 -ico build\windows\icon.ico -manifest build\windows\wails.exe.manifest.tmp -versioninfo build\windows\versioninfo.json.tmp -out guiapp\resource_windows_amd64.syso
  ```

- A runnable GUI binary needs the embedded frontend assets:

  ```sh
  go build -tags production -o dist/MaClaw_amd64.exe ./cmd/maclaw-gui/
  ```

  Without `-tags production` (or `-tags desktop`), `guiapp/assets_stub.go` supplies a
  stub FS and the app starts with a fake index.html — startup "works" but the UI never
  loads. Do not use a stub build for end-to-end checks.

## File-companion process

- `MaClaw.exe open-file <paths...>` runs the file-companion host. It uses its own
  single-instance lock (`maclaw-lock-file-companion`), `HideWindowOnClose`, and no tray.
- `MaClaw.exe open-file --quit` is a quit relay: it asks the running companion to exit
  through its normal exit controller; if no companion is running it self-terminates
  hidden. Main-process shutdown calls `app.signalFileCompanionQuit` to retire the
  companion (see `guiapp/file_companion_process.go`, `prepareProcessExit` in
  `guiapp/app.go`). Verified end-to-end on Windows via the Wails second-instance
  channel (WM_COPYDATA on the `wails-app-*-sic` message window).
- `maclaw-data-srv.exe` is an independent long-running service, unrelated to the
  desktop lifecycle.

## Logs

- `~/.maclaw/logs/maclaw.log` — shared by main app, companion, and quit relays.
  `[file-companion]` = companion-side; `[exit-lifecycle]` = shutdown lifecycle;
  `[boot]` = early startup markers.
- `~/.maclaw/logs/pet-companion.log`, `registration.log` for those subsystems.
