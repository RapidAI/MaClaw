# Linux desktop GUI artifacts

GitHub Actions builds one AppImage per Ubuntu/WebKit ABI. Download the file that matches the **host glibc**, not the machine you happened to compile on.

| Host | Artifact | WebKit ABI |
| --- | --- | --- |
| Ubuntu 22.04 | `*-u2204.AppImage` | webkit2gtk 4.0 |
| Ubuntu 24.04 / 26.04 (hub/VPS class) | `*-u2404.AppImage` | webkit2gtk 4.1 |

Brand prefixes stay `MaClaw-`, `TigerClaw-`, `MetaStaff-`. Architecture is `x86_64` or `aarch64`.

## Run on a remote Linux host

CI AppImages **bundle** WebKitGTK, JavaScriptCore, GTK, and `WebKitWebProcess`. You do **not** need `libwebkit2gtk` installed on the target.

```bash
chmod +x MaClaw-x86_64-u2404.AppImage
# Desktop session:
./MaClaw-x86_64-u2404.AppImage
# Headless / SSH (needs a virtual display):
sudo apt-get install -y xvfb
xvfb-run -a ./MaClaw-x86_64-u2404.AppImage
# Load-only smoke (no window):
./MaClaw-x86_64-u2404.AppImage --version
```

FUSE is optional. If the kernel blocks FUSE, extract and run:

```bash
./MaClaw-x86_64-u2404.AppImage --appimage-extract
./squashfs-root/AppRun
```

The standalone `*_linux_u2404` binary in the same workflow artifact is dynamically linked. On a host without the matching WebKit package it will not start; use the AppImage, or `sudo apt install libwebkit2gtk-4.1-0` (4.0-37 on Ubuntu 22.04).

## What was wrong before

`AppRun` only *checked* for the runner's `libwebkit2gtk-*.so` via `ldconfig` and `exit 1` if missing. The AppImage did not ship WebKit, so a typical Ubuntu 24.04 VPS could download the GHA artifact and still not run it. Packaging now copies those libraries into the AppImage and the workflow smokes `--version` plus an xvfb start.

## Rebuild locally (same as CI)

```bash
# Ubuntu 24.04
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev patchelf xvfb
CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 \
  -ldflags "-s -w -X main.version=dev" -o build/bin/MaClaw_amd64_linux ./gui/
build/linux/package_appimage.sh \
  --binary build/bin/MaClaw_amd64_linux \
  --app-name MaClaw \
  --ubuntu-label u2404
MACLAW_GUI_SMOKE_REQUIRE_XVFB=1 build/linux/smoke_gui.sh \
  --appimage dist/MaClaw-x86_64-u2404.AppImage \
  --binary build/bin/MaClaw_amd64_linux
```
